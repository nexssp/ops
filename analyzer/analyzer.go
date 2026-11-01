package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Analyzer struct {
	fset           *token.FileSet
	structs        map[StructKey]StructDefinition
	flowSchemas    map[string]StructDefinition
	enums          map[StructKey]EnumDefinition
	actions        []ActionContract
	flowHTTPRoutes []FlowHTTPRoute
	diagnostics    []Diagnostic
	fileCount      int
	dslConfig      DSLConfig

	// seenFiles dedupes AST parses across multiple scan roots. Keys are
	// cleaned absolute paths.
	seenFiles map[string]bool

	// aliases maps a source type alias (name in a convention file) to the
	// target type expression as written, e.g. "docker.DockerExecRecord".
	// Populated from convention files, consumed by propagateAliasExposure.
	aliases map[StructKey]string

	// seenNFlowPipelines deduplicates declarations encountered through an
	// @include and again during the source-root walk.
	seenNFlowPipelines map[string]bool
}

func New() *Analyzer {
	return &Analyzer{
		fset:               token.NewFileSet(),
		structs:            make(map[StructKey]StructDefinition),
		flowSchemas:        make(map[string]StructDefinition),
		enums:              make(map[StructKey]EnumDefinition),
		actions:            make([]ActionContract, 0),
		flowHTTPRoutes:     make([]FlowHTTPRoute, 0),
		diagnostics:        make([]Diagnostic, 0),
		dslConfig:          DSLConfig{Silent: make(map[string]bool)},
		seenFiles:          make(map[string]bool),
		aliases:            make(map[StructKey]string),
		seenNFlowPipelines: make(map[string]bool),
	}
}

// Analyze scans a single root. Kept for callers that pass exactly one path.
func Analyze(root string) (*Report, error) {
	return AnalyzeAll(root)
}

// AnalyzeAll scans one or more roots. Extra roots are additive; each root's
// local-dependency expansion (`replace` directives in go.mod) still runs, so
// multi-module workspaces work out of the box.
//
// Files visited under more than one root are parsed exactly once (path-based
// dedupe via seenFiles), and Flow DSL files found under any root are merged
// in scan order.
func AnalyzeAll(roots ...string) (*Report, error) {
	if len(roots) == 0 {
		roots = []string{"."}
	}

	a := New()
	for _, root := range roots {
		if root == "" {
			root = "."
		}

		absRoot, err := filepath.Abs(root)
		if err != nil {
			absRoot = filepath.Clean(root)
		}

		info, err := os.Stat(absRoot)
		if err != nil {
			return nil, fmt.Errorf("scan root %q: %w", root, err)
		}

		scanDir := absRoot
		if !info.IsDir() {
			scanDir = filepath.Dir(absRoot)
		}

		if err := a.walkAndParse(scanDir); err != nil {
			return nil, err
		}

		for _, extra := range findLocalDependencies(scanDir) {
			_ = a.walkAndParse(extra)
		}

		a.ScanFlowFiles(scanDir)
	}

	// Alias propagation must run before correlation: exposing a struct via
	// an alias can change which DTOs end up in the generated SDK, and
	// correlation is what fills in per-action Req/Res fields.
	a.propagateAliasExposure()
	a.correlateAndValidate()

	return a.buildReport(), nil
}

func (a *Analyzer) walkAndParse(scanDir string) error {
	var files []*ast.File

	err := filepath.WalkDir(scanDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			if path != scanDir && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "ui") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		clean := filepath.Clean(path)
		if a.seenFiles[clean] {
			return nil
		}
		a.seenFiles[clean] = true

		fileNode, parseErr := parser.ParseFile(a.fset, path, nil, parser.ParseComments)
		if parseErr == nil && fileNode != nil {
			files = append(files, fileNode)
			a.fileCount++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directory %s: %w", scanDir, err)
	}

	for _, f := range files {
		a.extractStructsAndFuncs(f)
	}
	for _, f := range files {
		a.extractEnums(f)
	}
	for _, f := range files {
		a.extractActions(f)
	}

	return nil
}

func (a *Analyzer) buildReport() *Report {
	seen := make(map[string]bool, len(a.actions))
	filteredActions := make([]ActionContract, 0, len(a.actions))

	for _, act := range a.actions {
		if act.Path == "" && len(act.Transports) == 0 {
			continue
		}

		// Dedup key covers both routing and transport surface. Actions with
		// only non-HTTP bindings get a stable key derived from their first
		// transport target.
		sig := act.Name + "@" + act.Method + ":" + act.Path
		if act.Path == "" && len(act.Transports) > 0 {
			b := act.Transports[0]
			sig = act.Name + "@" + b.Kind + ":" + b.Target
		}

		if seen[sig] {
			continue
		}
		seen[sig] = true
		filteredActions = append(filteredActions, act)
	}

	a.detectDuplicateRoutes(filteredActions)

	filteredDiags := a.filterDiagnostics()

	errCount, warnCount, pub, internal, sys := 0, 0, 0, 0, 0
	for _, d := range filteredDiags {
		switch d.Severity {
		case SeverityError:
			errCount++
		case SeverityWarning:
			warnCount++
		}
	}
	for _, act := range filteredActions {
		switch act.Scope {
		case "public":
			pub++
		case "internal":
			internal++
		case "system":
			sys++
		}
	}

	structs := make(map[string]StructDefinition, len(a.structs))
	for _, s := range a.structs {
		structs[s.Qualified] = s
		if s.ImportPath != "" {
			structs[s.ImportPath+"."+s.Name] = s
		}
	}
	flowSchemas := make(map[string]StructDefinition, len(a.flowSchemas))
	for name, schema := range a.flowSchemas {
		flowSchemas[name] = schema
	}

	enums := make(map[string]EnumDefinition, len(a.enums))
	for _, e := range a.enums {
		enums[e.Qualified] = e
	}
	flowHTTPRoutes := append([]FlowHTTPRoute(nil), a.flowHTTPRoutes...)
	sort.Slice(flowHTTPRoutes, func(i, j int) bool {
		if flowHTTPRoutes[i].Pipeline != flowHTTPRoutes[j].Pipeline {
			return flowHTTPRoutes[i].Pipeline < flowHTTPRoutes[j].Pipeline
		}
		if flowHTTPRoutes[i].Method != flowHTTPRoutes[j].Method {
			return flowHTTPRoutes[i].Method < flowHTTPRoutes[j].Method
		}
		return flowHTTPRoutes[i].Path < flowHTTPRoutes[j].Path
	})

	return &Report{
		ScannedFiles:   a.fileCount,
		Actions:        filteredActions,
		FlowHTTPRoutes: flowHTTPRoutes,
		FlowSchemas:    flowSchemas,
		Structs:        structs,
		Enums:          enums,
		Diagnostics:    filteredDiags,
		Stats: ReportStats{
			TotalActions:    len(filteredActions),
			PublicActions:   pub,
			InternalActions: internal,
			SystemActions:   sys,
			TotalStructs:    len(a.structs),
			TotalEnums:      len(a.enums),
			ErrorCount:      errCount,
			WarningCount:    warnCount,
		},
	}
}
