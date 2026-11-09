package analyzer

import (
	"fmt"
	"strings"

	"github.com/nexssp/ops/analyzer/dsl"
)

func (a *flowAnalyzer) applyDSLConfigLine(line string) {
	dsl.ApplyConfigLine(&a.dslConfig, line)
}

func (a *flowAnalyzer) filterDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if a.dslConfig.Silent[diagnostic.Rule] {
			continue
		}
		if a.dslConfig.Strict && diagnostic.Severity == SeverityWarning {
			diagnostic.Severity = SeverityError
		}
		out = append(out, diagnostic)
	}
	return out
}

func (a *flowAnalyzer) detectDuplicateRoutes(actions []ActionContract) {
	byRoute := make(map[string]string)
	for _, action := range actions {
		if action.Method == "" || action.Path == "" {
			continue
		}
		key := action.Method + " " + action.Path
		previous, exists := byRoute[key]
		if !exists {
			byRoute[key] = action.Name
			continue
		}
		if previous == action.Name || a.hasDuplicateRouteDiagnostic(action.Name, key) {
			continue
		}
		a.diagnostics = append(a.diagnostics, Diagnostic{
			Severity: SeverityError,
			Rule:     "routes/duplicate",
			Action:   action.Name,
			File:     action.SourceFile,
			Line:     action.SourceLine,
			Message: fmt.Sprintf(
				"Route %q claimed by both %q and %q. Two distinct actions cannot share the same HTTP method and path.",
				key, previous, action.Name,
			),
		})
	}
}

func (a *flowAnalyzer) hasDuplicateRouteDiagnostic(action, route string) bool {
	for _, diagnostic := range a.diagnostics {
		if diagnostic.Rule == "routes/duplicate" && diagnostic.Action == action && diagnostic.Message != "" && containsRoute(diagnostic.Message, route) {
			return true
		}
	}
	return false
}

func containsRoute(message, route string) bool {
	return len(route) > 0 && len(message) >= len(route) && strings.Contains(message, route)
}
