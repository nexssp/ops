package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type flowAnalyzer struct {
	actions            []ActionContract
	diagnostics        []Diagnostic
	flowSchemas        map[string]StructDefinition
	flowHTTPRoutes     []FlowHTTPRoute
	seenNFlowPipelines map[string]bool
	seenEmbeddedFiles  map[string]bool
	dslConfig          DSLConfig
}

func newFlowAnalyzer(actions []ActionContract) *flowAnalyzer {
	return &flowAnalyzer{
		actions:            append([]ActionContract(nil), actions...),
		diagnostics:        make([]Diagnostic, 0),
		flowSchemas:        make(map[string]StructDefinition),
		flowHTTPRoutes:     make([]FlowHTTPRoute, 0),
		seenNFlowPipelines: make(map[string]bool),
		seenEmbeddedFiles:  make(map[string]bool),
		dslConfig:          DSLConfig{Silent: make(map[string]bool)},
	}
}

func (a *flowAnalyzer) scanEmbeddedDSL(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "ui") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		clean := filepath.Clean(path)
		if a.seenEmbeddedFiles[clean] {
			return nil
		}
		a.seenEmbeddedFiles[clean] = true
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil || file == nil {
			return nil
		}
		for _, declaration := range file.Decls {
			genDecl, ok := declaration.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.CONST {
				continue
			}
			for _, spec := range genDecl.Specs {
				values, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, value := range values.Values {
					literal, ok := value.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					rawDSL := strings.Trim(literal.Value, "`\"")
					if strings.Contains(rawDSL, "->") || strings.Contains(rawDSL, ":route=") {
						a.mergeDSLManifest(rawDSL, path)
					}
				}
			}
		}
		return nil
	})
}

func (a *flowAnalyzer) finishReport(report *Report) {
	a.diagnostics = append(append([]Diagnostic(nil), report.Diagnostics...), a.diagnostics...)
	seen := make(map[string]bool, len(a.actions))
	actions := make([]ActionContract, 0, len(a.actions))
	for _, action := range a.actions {
		if action.Path == "" && len(action.Transports) == 0 {
			continue
		}
		sig := action.Name + "@" + action.Method + ":" + action.Path
		if action.Path == "" && len(action.Transports) > 0 {
			binding := action.Transports[0]
			sig = action.Name + "@" + binding.Kind + ":" + binding.Target
		}
		if seen[sig] {
			continue
		}
		seen[sig] = true
		actions = append(actions, action)
	}
	a.actions = actions
	a.detectDuplicateRoutes(actions)

	report.Diagnostics = a.filterDiagnostics(a.diagnostics)
	report.Actions = actions
	report.FlowSchemas = make(map[string]StructDefinition, len(a.flowSchemas))
	for name, schema := range a.flowSchemas {
		report.FlowSchemas[name] = schema
	}
	report.FlowHTTPRoutes = append([]FlowHTTPRoute(nil), a.flowHTTPRoutes...)
	sort.Slice(report.FlowHTTPRoutes, func(i, j int) bool {
		if report.FlowHTTPRoutes[i].Pipeline != report.FlowHTTPRoutes[j].Pipeline {
			return report.FlowHTTPRoutes[i].Pipeline < report.FlowHTTPRoutes[j].Pipeline
		}
		if report.FlowHTTPRoutes[i].Method != report.FlowHTTPRoutes[j].Method {
			return report.FlowHTTPRoutes[i].Method < report.FlowHTTPRoutes[j].Method
		}
		return report.FlowHTTPRoutes[i].Path < report.FlowHTTPRoutes[j].Path
	})
	correlateFlowActions(report)

	report.Stats.TotalActions = len(actions)
	report.Stats.PublicActions, report.Stats.InternalActions, report.Stats.SystemActions = 0, 0, 0
	report.Stats.ErrorCount, report.Stats.WarningCount = 0, 0
	for _, action := range actions {
		switch action.Scope {
		case "public":
			report.Stats.PublicActions++
		case "internal":
			report.Stats.InternalActions++
		case "system":
			report.Stats.SystemActions++
		}
	}
	for _, diagnostic := range report.Diagnostics {
		switch diagnostic.Severity {
		case SeverityError:
			report.Stats.ErrorCount++
		case SeverityWarning:
			report.Stats.WarningCount++
		}
	}
}
