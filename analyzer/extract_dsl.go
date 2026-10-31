package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nexssp/ops/analyzer/dsl"
)

func (a *Analyzer) mergeDSLManifest(dslContent, sourceFile string) {
	lines := strings.Split(dslContent, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)

		if strings.HasPrefix(line, "@config:") {
			a.applyDSLConfigLine(line)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		a.parseAndMergeDSLLine(line, sourceFile)
	}
}

func (a *Analyzer) parseAndMergeDSLLine(line, sourceFile string) {
	pl, ok := dsl.ParseLine(line)
	if !ok {
		return
	}
	p := pl.Modifiers

	effectiveName := pl.RawName
	if p.CustomName != "" {
		effectiveName = p.CustomName
	} else if pl.IsComposite && p.Path != "" {
		cleanPath := strings.TrimPrefix(p.Path, "/api/")
		cleanPath = strings.TrimPrefix(cleanPath, "/")
		if cleanPath != "" && !strings.Contains(cleanPath, "{") {
			effectiveName = strings.ReplaceAll(cleanPath, "/", ".")
		}
	}

	var matchedAction *ActionContract
	for i := range a.actions {
		if a.actions[i].Name == effectiveName {
			matchedAction = &a.actions[i]
			break
		}
	}

	if matchedAction != nil {
		if matchedAction.RouteSource == "go" && matchedAction.Path != "" && p.Path != "" && matchedAction.Path != p.Path {
			a.diagnostics = append(a.diagnostics, Diagnostic{
				Severity: SeverityError,
				Rule:     "routes/conflict-go-dsl",
				Action:   effectiveName,
				File:     sourceFile,
				Message: fmt.Sprintf(
					"Ambiguous route definition for %q: route is defined both in Go code (%s %s) and in Flow DSL (%s %s). Choose one, cannot be both!",
					effectiveName, matchedAction.Method, matchedAction.Path, p.Method, p.Path,
				),
			})
			return
		}
		applyDSLModifiers(a, matchedAction, &p, effectiveName, sourceFile)
		return
	}

	if p.Path == "" && len(p.Transports) == 0 {
		return
	}

	a.diagnostics = append(a.diagnostics, Diagnostic{
		Severity: SeverityWarning,
		Rule:     "dsl/orphan-route",
		Action:   effectiveName,
		File:     sourceFile,
		Line:     1,
		Message: fmt.Sprintf(
			"DSL declares %s for %q but no matching Go action was found. "+
				"Silence rule 'dsl/orphan-route' via @config:silent if intentional.",
			describeDSLTargets(&p), effectiveName,
		),
	})

	reqType := inferReqTypeName(effectiveName)
	resType := inferResTypeName(effectiveName)

	if pl.IsComposite && p.ReqType == "" && p.ResType == "" {
		if pl.HeadAtom != "" {
			for _, act := range a.actions {
				if act.Name == pl.HeadAtom && act.ReqTypeName != "" {
					reqType = act.ReqTypeName
					break
				}
			}
		}
		for _, act := range a.actions {
			if act.Name == pl.RawName && act.ResTypeName != "" {
				resType = act.ResTypeName
				break
			}
		}
	}
	if p.ReqType != "" {
		reqType = p.ReqType
	}
	if p.ResType != "" {
		resType = p.ResType
	}

	typeSource := ""
	if p.ReqType != "" || p.ResType != "" {
		typeSource = "dsl"
	}

	newAct := ActionContract{
		Name:        effectiveName,
		Description: p.Description,
		Scope:       scopeOrDefault(p.Scope, "public"),
		RouteSource: "dsl",
		TypeSource:  typeSource,
		SourceFile:  sourceFile,
		SourceLine:  1,
		ReqTypeName: reqType,
		ResTypeName: resType,
	}
	applyDSLModifiers(a, &newAct, &p, effectiveName, sourceFile)
	a.actions = append(a.actions, newAct)
}

// applyDSLModifiers folds a parsed DSL Modifiers block onto an ActionContract.
// List fields (roles, permissions, features, tags) merge with what Go source
// already declared; scalar fields are overridden only when DSL set them.
func applyDSLModifiers(a *Analyzer, act *ActionContract, p *dsl.Modifiers, effectiveName, sourceFile string) {
	for _, b := range p.Transports {
		act.Transports = appendUniqueTransport(act.Transports, b)
		switch b.Kind {
		case "http":
			act.Method = b.Method
			act.Path = b.Path
			act.RouteSource = "dsl"
		case "sse":
			act.Method = "SSE"
			act.Path = b.Path
			act.RouteSource = "dsl"
			act.Streaming = true
		case "raw":
			act.Method = b.Method
			act.Path = b.Path
			act.RouteSource = "dsl"
			act.Raw = true
		}
	}

	if p.Path != "" && act.Path == "" {
		act.Method = p.Method
		act.Path = p.Path
		act.RouteSource = "dsl"
	}

	if p.Description != "" {
		act.Description = p.Description
	}
	if p.Timeout > 0 {
		act.Timeout = p.Timeout
	}
	if p.CacheTTL > 0 {
		act.CacheTTL = p.CacheTTL
	}
	if p.RetryMax > 0 {
		act.RetryMax = p.RetryMax
	}
	if p.Idempotent {
		act.Idempotent = true
	}

	act.Roles = unionStrings(act.Roles, p.Roles)
	act.Permissions = unionStrings(act.Permissions, p.Permissions)
	act.Features = unionStrings(act.Features, p.Features)
	act.Tags = unionStrings(act.Tags, p.Tags)

	if p.RequiresAuth {
		act.RequiresAuth = true
	}
	if p.Scope != "" {
		act.Scope = p.Scope
	}
	if p.SuccessStatus > 0 {
		act.SuccessStatus = p.SuccessStatus
	}
	if p.BudgetMicros > 0 {
		act.BudgetMicros = p.BudgetMicros
	}
	if p.RateLimit != "" {
		act.RateLimit = p.RateLimit
	}
	if p.RateLimitBurst > 0 {
		act.RateLimitBurst = p.RateLimitBurst
	}
	if p.ConcurrencyLimit > 0 {
		act.ConcurrencyLimit = p.ConcurrencyLimit
	}
	if p.BreakerFailures > 0 {
		act.BreakerFailures = p.BreakerFailures
	}
	if p.BreakerCooldown > 0 {
		act.BreakerCooldown = p.BreakerCooldown
	}
	if p.HITLPrompt != "" {
		act.HITLPrompt = p.HITLPrompt
	}
	if len(p.HITLOptions) > 0 {
		act.HITLOptions = p.HITLOptions
	}
	if len(p.HITLTriggers) > 0 {
		act.HITLTriggers = p.HITLTriggers
	}
	if p.BackoffStrategy != "" {
		act.BackoffStrategy = p.BackoffStrategy
	}
	if p.BackoffBase > 0 {
		act.BackoffBase = p.BackoffBase
	}
	if p.BackoffMax > 0 {
		act.BackoffMax = p.BackoffMax
	}
	if p.RetryPredicate != "" {
		act.RetryPredicate = p.RetryPredicate
	}
	if p.Priority != "" {
		act.Priority = p.Priority
	}
	if p.CacheKey != "" {
		act.CacheKey = p.CacheKey
	}
	if p.Audit {
		act.Audit = true
	}
	if p.IdempotencyHeader != "" {
		act.IdempotencyHeader = p.IdempotencyHeader
	}
	if p.Deprecated {
		act.Deprecated = true
	}
	if p.DeprecatedSince != "" {
		act.DeprecatedSince = p.DeprecatedSince
	}
	if p.DeprecatedUse != "" {
		act.DeprecatedUse = p.DeprecatedUse
	}

	if p.ReqType != "" || p.ResType != "" {
		a.applyTypeOverride(act, p.ReqType, p.ResType, effectiveName, sourceFile)
	}
}

func appendUniqueTransport(bindings []TransportBinding, b TransportBinding) []TransportBinding {
	for _, existing := range bindings {
		if existing.Kind == b.Kind && existing.Target == b.Target {
			return bindings
		}
	}
	return append(bindings, b)
}

func unionStrings(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	for _, s := range b {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

func scopeOrDefault(scope, def string) string {
	if scope != "" {
		return scope
	}
	return def
}

func describeDSLTargets(p *dsl.Modifiers) string {
	if len(p.Transports) == 0 {
		return "binding"
	}
	parts := make([]string, 0, len(p.Transports))
	for _, b := range p.Transports {
		parts = append(parts, b.Kind+" "+b.Target)
	}
	return strings.Join(parts, ", ")
}

func (a *Analyzer) applyTypeOverride(act *ActionContract, dslReq, dslRes, effectiveName, sourceFile string) {
	if dslReq != "" {
		goReq := lastTypeSegment(act.ReqTypeName)
		dslSeg := lastTypeSegment(dslReq)
		goHasReal := goReq != "" && goReq != "struct{}" && goReq != "void"

		switch {
		case !goHasReal:
			act.TypeSource = "dsl"
			act.ReqTypeName = dslReq
			act.ReqType = TypeRef{Name: dslReq}
		case goReq != dslSeg:
			a.diagnostics = append(a.diagnostics, Diagnostic{
				Severity: SeverityError,
				Rule:     "dsl/type-override-mismatch",
				Action:   effectiveName,
				File:     sourceFile,
				Message: fmt.Sprintf(
					"DSL :type= declares request %q for %q but Go source has %q. "+
						"Silence rule 'dsl/type-override-mismatch' via @config:silent to force the override.",
					dslReq, effectiveName, goReq,
				),
			})
			act.TypeSource = "dsl-override"
			act.ReqTypeName = dslReq
			act.ReqType = TypeRef{Name: dslReq}
		}
	}

	if dslRes != "" {
		goRes := lastTypeSegment(act.ResTypeName)
		dslSeg := lastTypeSegment(dslRes)
		goHasReal := goRes != "" && goRes != "struct{}" && goRes != "void"

		switch {
		case !goHasReal:
			act.TypeSource = "dsl"
			act.ResTypeName = dslRes
			act.ResType = TypeRef{Name: dslRes}
		case goRes != dslSeg:
			a.diagnostics = append(a.diagnostics, Diagnostic{
				Severity: SeverityError,
				Rule:     "dsl/type-override-mismatch",
				Action:   effectiveName,
				File:     sourceFile,
				Message: fmt.Sprintf(
					"DSL :type= declares response %q for %q but Go source has %q. "+
						"Silence rule 'dsl/type-override-mismatch' via @config:silent to force the override.",
					dslRes, effectiveName, goRes,
				),
			})
			act.TypeSource = "dsl-override"
			act.ResTypeName = dslRes
			act.ResType = TypeRef{Name: dslRes}
		}
	}
}

func lastTypeSegment(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

func inferReqTypeName(actName string) string {
	parts := strings.Split(actName, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + toPascalCase(parts[1]) + "Req"
	}
	return toPascalCase(actName) + "Req"
}

func inferResTypeName(actName string) string {
	parts := strings.Split(actName, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + toPascalCase(parts[1]) + "Res"
	}
	return toPascalCase(actName) + "Res"
}

func (a *Analyzer) ScanFlowFiles(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".nflow") {
			//nolint:gosec // G703: prompt file path is user-supplied on the CLI, by design.
			if data, err := os.ReadFile(path); err == nil {
				source := string(data)
				a.scanNFlowPipelines(source, path)
				a.mergeDSLManifest(stripNFlowPipelineBlocks(source), path)
			}
		}
		return nil
	})
}
