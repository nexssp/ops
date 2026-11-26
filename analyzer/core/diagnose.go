package core

import (
	"fmt"

	"github.com/nexssp/ops/analyzer/dsl"
)

func (a *Analyzer) applyDSLConfigLine(line string) {
	dsl.ApplyConfigLine(&a.dslConfig, line)
}

func (a *Analyzer) filterDiagnostics() []Diagnostic {
	out := make([]Diagnostic, 0, len(a.diagnostics))
	for _, d := range a.diagnostics {
		if a.dslConfig.Silent[d.Rule] {
			continue
		}
		if a.dslConfig.Strict && d.Severity == SeverityWarning {
			d.Severity = SeverityError
		}
		out = append(out, d)
	}
	return out
}

func (a *Analyzer) detectDuplicateActionNames(actions []ActionContract) {
	firstByName := make(map[string]ActionContract, len(actions))
	for _, action := range actions {
		if action.Name == "" {
			continue
		}

		first, exists := firstByName[action.Name]
		if !exists {
			firstByName[action.Name] = action
			continue
		}

		a.diagnostics = append(a.diagnostics, Diagnostic{
			Severity:   SeverityError,
			Rule:       "actions/duplicate-name",
			Action:     action.Name,
			File:       action.SourceFile,
			Line:       action.SourceLine,
			Message:    fmt.Sprintf("Action name %q is declared more than once; first declaration is at %s:%d and this declaration is at %s:%d.", action.Name, first.SourceFile, first.SourceLine, action.SourceFile, action.SourceLine),
			Suggestion: "Give each action contract a unique name, or declare its transports on a single action.",
		})
	}
}

func (a *Analyzer) detectDuplicateRoutes(actions []ActionContract) {
	byRoute := make(map[string]string)

	for _, act := range actions {
		if act.Method == "" || act.Path == "" {
			continue
		}
		key := act.Method + " " + act.Path
		if prev, dup := byRoute[key]; dup && prev != act.Name {
			a.diagnostics = append(a.diagnostics, Diagnostic{
				Severity: SeverityError,
				Rule:     "routes/duplicate",
				Action:   act.Name,
				File:     act.SourceFile,
				Line:     act.SourceLine,
				Message: fmt.Sprintf(
					"Route %q claimed by both %q and %q. Two distinct actions cannot share the same HTTP method and path.",
					key, prev, act.Name,
				),
			})
			continue
		}
		byRoute[key] = act.Name
	}
}
