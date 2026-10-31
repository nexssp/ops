package analyzer

import (
	"context"
	"fmt"
	"os"
	"regexp"
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
func (a *Analyzer) scanNFlowPipelines(source, file string) {
	if !hasNFlowDirective(source) {
		return
	}
	_, meta, err := compiler.Preprocess(context.Background(), standardFlowDirectives(), source, file)
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

	pipelines, _ := meta["pipelines"].(map[string]string)
	modifiers, _ := meta["pipeline_modifiers"].(map[string][]string)
	for name, body := range pipelines {
		if a.seenNFlowPipelines[name] {
			continue
		}
		a.seenNFlowPipelines[name] = true
		line := pipelineLine(source, name)
		if route := parseFlowHTTPRoute(name, modifiers[name], file, line); route != nil {
			a.flowHTTPRoutes = append(a.flowHTTPRoutes, *route)
		}
		contract := parsePipelineRoute(name, body, modifiers[name], a.actions, file, line)
		if contract == nil {
			continue
		}
		a.mergePipelineContract(*contract, name, file)
	}
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
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "pipeline.") {
		return name
	}
	return "pipeline." + name
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

func pipelineLine(source, name string) int {
	prefix := "@pipeline " + name
	for i, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return i + 1
		}
	}
	return 1
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

func (a *Analyzer) mergePipelineContract(incoming ActionContract, pipelineName, file string) {
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
		// Keep the Go-derived request/response contracts, fields and metadata;
		// the nflow declaration supplies only the transport binding and route.
		goReq, goRes := a.actions[i].ReqTypeName, a.actions[i].ResTypeName
		goReqRef, goResRef := a.actions[i].ReqType, a.actions[i].ResType
		incoming.ReqTypeName, incoming.ResTypeName = goReq, goRes
		incoming.ReqType, incoming.ResType = goReqRef, goResRef
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
