package analyzer

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/native"
)

var (
	flowDirectivesOnce sync.Once
	flowDirectives     *compiler.DirectiveTable
	flowNameTokenRE    = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.-]*`)
)

func standardFlowDirectives() *compiler.DirectiveTable {
	flowDirectivesOnce.Do(func() { flowDirectives = native.Directives() })
	return flowDirectives
}

// scanNFlowPipelines uses Flow's own preprocessing and @pipeline directive
// contract, rather than attempting to parse pipeline headers as legacy DSL.
func (a *flowAnalyzer) scanNFlowPipelines(source, file string) {
	if !hasNFlowDirective(source) {
		return
	}
	prepared, err := compiler.PrepareSource(context.Background(), standardFlowDirectives(), source, file)
	if err != nil {
		if strings.Contains(source, "@pipeline") || strings.Contains(source, "@include") {
			a.diagnostics = append(a.diagnostics, Diagnostic{
				Severity: SeverityError,
				Rule:     "nflow/preprocess",
				File:     file,
				Message:  err.Error(),
			})
		}
		return
	}

	a.addFlowSchemas(prepared.Metadata(), file)
	for _, pipeline := range orderedFlowPipelines(source, prepared) {
		canonicalName := canonicalFlowPipelineName(pipeline.name)
		if a.seenNFlowPipelines[canonicalName] {
			continue
		}
		a.seenNFlowPipelines[canonicalName] = true
		if route := parseFlowHTTPRoute(pipeline.name, pipeline.modifiers, file, pipeline.line); route != nil {
			a.flowHTTPRoutes = append(a.flowHTTPRoutes, *route)
		}
		contract := parsePipelineRoute(pipeline.name, pipeline.body, pipeline.modifiers, a.actions, file, pipeline.line)
		if contract == nil {
			continue
		}
		a.applyFlowPipelineSchemas(contract, pipeline.modifiers, file, pipeline.line)
		a.mergePipelineContract(*contract, pipeline.name, file)
	}
}

type flowPipelineMetadata struct {
	name      string
	body      string
	modifiers []string
	line      int
}

// orderedFlowPipelines joins Flow's source-ordered declarations to metadata,
// whose pipeline keys retain the names as written in the source.
func orderedFlowPipelines(source string, prepared *compiler.PreparedSource) []flowPipelineMetadata {
	metadata := prepared.Metadata()
	pipelines, _ := metadata["pipelines"].(map[string]string)
	if len(pipelines) == 0 {
		return nil
	}
	modifiers, _ := metadata["pipeline_modifiers"].(map[string][]string)

	metadataNames := make([]string, 0, len(pipelines))
	for name := range pipelines {
		metadataNames = append(metadataNames, name)
	}
	sort.Strings(metadataNames)
	nameByCanonical := make(map[string]string, len(metadataNames))
	for _, name := range metadataNames {
		canonical := canonicalFlowPipelineName(name)
		if _, exists := nameByCanonical[canonical]; !exists {
			nameByCanonical[canonical] = name
		}
	}

	index := prepared.SourceIndex()
	ordered := make([]flowPipelineMetadata, 0, len(index.Declarations))
	for _, declaration := range index.Declarations {
		if declaration.Kind != "pipeline" {
			continue
		}

		canonical := canonicalFlowPipelineName(declaration.Name)
		name, ok := nameByCanonical[canonical]
		// Prefer the exact spelling at the declaration span if two raw metadata
		// keys happen to canonicalize to the same resolver name.
		if start, end := declaration.NameSpan.Start, declaration.NameSpan.End; start >= 0 && end >= start && end <= len(source) {
			if sourceName := source[start:end]; sourceName != "" {
				if _, exists := pipelines[sourceName]; exists {
					name, ok = sourceName, true
				}
			}
		}
		if !ok {
			continue
		}
		ordered = append(ordered, flowPipelineMetadata{
			name:      name,
			body:      pipelines[name],
			modifiers: modifiers[name],
			line:      sourceLineAt(source, declaration.NameSpan.Start),
		})
	}
	return ordered
}

func canonicalFlowPipelineName(name string) string {
	name = strings.TrimSpace(name)
	if !strings.Contains(name, ".") {
		return "pipeline." + name
	}
	return name
}

func parseFlowHTTPRoute(name string, rawModifiers []string, file string, line int) *FlowHTTPRoute {
	var routeValue string
	var status int
	for _, raw := range rawModifiers {
		key, value, hasValue := strings.Cut(strings.TrimSpace(raw), "=")
		if !hasValue {
			continue
		}
		switch strings.TrimSpace(compiler.ModifierName(key)) {
		case "route":
			routeValue = flowModifierValue(value)
		case "status":
			status, _ = strconv.Atoi(flowModifierValue(value))
		}
	}
	method, routePath := parseFlowRoute(routeValue)
	if method == "" || routePath == "" {
		return nil
	}
	return &FlowHTTPRoute{
		Pipeline:      flowResolverPipelineName(name),
		Method:        method,
		Path:          routePath,
		SuccessStatus: status,
		SourceFile:    file,
		SourceLine:    line,
	}
}

func flowResolverPipelineName(name string) string {
	return canonicalFlowPipelineName(name)
}

func hasNFlowDirective(source string) bool {
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@pipeline") || strings.HasPrefix(trimmed, "@include") {
			return true
		}
	}
	return false
}

func stripNFlowPipelineBlocks(source string) string {
	lines := strings.Split(source, "\n")
	out := make([]string, 0, len(lines))
	inPipeline := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@pipeline ") {
			inPipeline = true
			out = append(out, "")
			continue
		}
		if inPipeline {
			if trimmed == "@end" {
				inPipeline = false
			}
			out = append(out, "")
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func sourceLineAt(source string, offset int) int {
	if offset < 0 || offset > len(source) {
		offset = 0
	}
	return strings.Count(source[:offset], "\n") + 1
}

func parsePipelineRoute(name, body string, rawModifiers []string, actions []ActionContract, file string, line int) *ActionContract {
	attrs := make(map[string]string, len(rawModifiers))
	flags := make(map[string]bool)
	for _, raw := range rawModifiers {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		key, value, hasValue := strings.Cut(raw, "=")
		key = strings.TrimSpace(compiler.ModifierName(key))
		if hasValue {
			attrs[key] = flowModifierValue(value)
		} else {
			flags[key] = true
		}
	}
	method, path := parseFlowRoute(attrs["route"])
	if method == "" || path == "" {
		return nil
	}

	flowName := flowContractName(name)
	matched := flowActionsInBody(body, actions)
	var contract ActionContract
	if len(matched) == 1 {
		contract = actions[matched[0]]
	} else {
		contract = ActionContract{
			Name:        flowName,
			Scope:       "public",
			RouteSource: "nflow",
			TypeSource:  "go",
			SourceFile:  file,
			SourceLine:  line,
		}
		if len(matched) > 0 {
			first, last := actions[matched[0]], actions[matched[len(matched)-1]]
			contract.ReqTypeName, contract.ReqType = first.ReqTypeName, first.ReqType
			contract.ResTypeName, contract.ResType = last.ResTypeName, last.ResType
		}
	}
	contract.Name = flowName
	contract.Method = method
	contract.Path = path
	contract.RouteSource = "nflow"
	contract.SourceFile = file
	contract.SourceLine = line
	contract.Scope = "public"
	if value := attrs["scope"]; value != "" {
		contract.Scope = value
	}
	if value := attrs["desc"]; value != "" {
		contract.Description = value
	}
	if value := attrs["status"]; value != "" {
		contract.SuccessStatus, _ = strconv.Atoi(value)
	}
	if value := attrs["timeout"]; value != "" {
		contract.Timeout, _ = time.ParseDuration(value)
	}
	if value := attrs["retry"]; value != "" {
		contract.RetryMax, _ = strconv.Atoi(value)
	}
	contract.Idempotent = flags["idempotent"] || truthyFlow(attrs["idempotent"])
	if value := attrs["tag"]; value != "" {
		contract.Tags = splitFlowList(value)
	}
	if attrs["auth"] == "required" || truthyFlow(attrs["auth"]) {
		contract.RequiresAuth = true
	}
	contract.Transports = appendUniqueTransport(contract.Transports, TransportBinding{
		Kind: "http", Protocol: "flow", Target: method + " " + path, Method: method, Path: path,
	})
	return &contract
}

func (a *flowAnalyzer) mergePipelineContract(incoming ActionContract, pipelineName, file string) {
	for i := range a.actions {
		if a.actions[i].Name != incoming.Name {
			continue
		}
		if a.actions[i].Path != "" && (a.actions[i].Path != incoming.Path || a.actions[i].Method != incoming.Method) {
			a.diagnostics = append(a.diagnostics, Diagnostic{
				Severity: SeverityError,
				Rule:     "routes/conflict-go-nflow",
				Action:   incoming.Name,
				File:     file,
				Line:     incoming.SourceLine,
				Message:  fmt.Sprintf("route for pipeline %q conflicts with the existing %s %s route on action %q", pipelineName, a.actions[i].Method, a.actions[i].Path, incoming.Name),
			})
			return
		}
		// Ordinary NFlow routes retain the historical Go-derived contract.
		// When a boundary schema modifier is present, its schema bindings
		// (including an intentionally absent request schema) are authoritative.
		if !incoming.FlowSchemaDriven {
			goReq, goRes := a.actions[i].ReqTypeName, a.actions[i].ResTypeName
			goReqRef, goResRef := a.actions[i].ReqType, a.actions[i].ResType
			incoming.ReqTypeName, incoming.ResTypeName = goReq, goRes
			incoming.ReqType, incoming.ResType = goReqRef, goResRef
		}
		if a.actions[i].Description != "" && incoming.Description == "" {
			incoming.Description = a.actions[i].Description
		}
		incoming.Tags = unionStrings(a.actions[i].Tags, incoming.Tags)
		incoming.Roles = a.actions[i].Roles
		incoming.Permissions = a.actions[i].Permissions
		incoming.Features = a.actions[i].Features
		incoming.RequiresAuth = incoming.RequiresAuth || a.actions[i].RequiresAuth
		incoming.Idempotent = incoming.Idempotent || a.actions[i].Idempotent
		transports := append([]TransportBinding(nil), a.actions[i].Transports...)
		for _, binding := range incoming.Transports {
			transports = appendUniqueTransport(transports, binding)
		}
		incoming.Transports = transports
		a.actions[i] = incoming
		return
	}
	a.actions = append(a.actions, incoming)
}

func flowActionsInBody(body string, actions []ActionContract) []int {
	known := make(map[string]int, len(actions))
	for i := range actions {
		if _, exists := known[actions[i].Name]; !exists {
			known[actions[i].Name] = i
		}
	}
	seen := make(map[string]bool)
	var out []int
	for _, token := range flowNameTokenRE.FindAllString(stripFlowComments(body), -1) {
		if index, ok := known[token]; ok && !seen[token] {
			seen[token] = true
			out = append(out, index)
		}
	}
	return out
}

func stripFlowComments(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

func flowContractName(name string) string {
	if index := strings.IndexByte(name, '_'); index > 0 {
		return name[:index] + "." + name[index+1:]
	}
	if strings.Contains(name, ".") {
		return name
	}
	return "pipeline." + name
}

// ExcludeFlowPipelines removes named NFlow pipelines and their route-derived
// action contracts from an analysis report. This is useful when runtime flags
// conditionally mount a development pipeline that is not part of the default
// production API surface.
func ExcludeFlowPipelines(report *Report, names []string) {
	if report == nil || len(names) == 0 {
		return
	}
	excludedPipelines := make(map[string]bool, len(names))
	excludedActions := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "pipeline.")
		if name == "" {
			continue
		}
		excludedPipelines[name] = true
		excludedActions[flowContractName(name)] = true
	}

	routes := report.FlowHTTPRoutes[:0]
	for _, route := range report.FlowHTTPRoutes {
		name := strings.TrimPrefix(route.Pipeline, "pipeline.")
		if !excludedPipelines[name] {
			routes = append(routes, route)
		}
	}
	report.FlowHTTPRoutes = routes

	actions := report.Actions[:0]
	for _, action := range report.Actions {
		if action.RouteSource == "nflow" && excludedActions[action.Name] {
			continue
		}
		actions = append(actions, action)
	}
	report.Actions = actions

	report.Stats.TotalActions = len(actions)
	report.Stats.PublicActions = 0
	report.Stats.InternalActions = 0
	report.Stats.SystemActions = 0
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
}

func parseFlowRoute(value string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) < 2 {
		return "", ""
	}
	method := strings.ToUpper(fields[0])
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return method, strings.Join(fields[1:], " ")
	default:
		return "", ""
	}
}

func flowModifierValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1]
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}

func truthyFlow(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false", "no", "0":
		return false
	default:
		return true
	}
}

func splitFlowList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// ReadNFlowSource is an exported helper used by the SDK generation command's
// integration tests to exercise the exact Flow preprocessing path.
func ReadNFlowSource(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
