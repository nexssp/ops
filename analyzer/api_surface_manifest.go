package analyzer

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/nexssp/ops/analyzer/core"
)

// APISurfaceManifest is the complete public HTTP API emitted by `nexssp sdk api`.
// Its actions use the same wire-contract and metadata fields as analyzer reports.
type APISurfaceManifest struct {
	Actions []ActionContract `json:"actions"`
}

// ApplyAPISurfaceManifest replaces the scanned route set with an explicit public
// surface. This supports routes created dynamically at runtime and prevents
// server-only routes or duplicate scanner declarations from leaking into the SDK.
func ApplyAPISurfaceManifest(report *Report, manifest APISurfaceManifest) error {
	if report == nil {
		return fmt.Errorf("apply API surface manifest: report is nil")
	}
	if len(manifest.Actions) == 0 {
		return fmt.Errorf("apply API surface manifest: no actions were provided")
	}

	actions := append([]ActionContract(nil), manifest.Actions...)
	seenRoutes := make(map[string]string, len(actions))
	seenMethodNames := make(map[string]string, len(actions))
	for i := range actions {
		action := &actions[i]
		if action.Name == "" || strings.TrimSpace(action.Name) != action.Name {
			return fmt.Errorf("apply API surface manifest: action name is required and must not have surrounding whitespace")
		}
		if action.Scope != "" && action.Scope != "public" {
			return fmt.Errorf("apply API surface manifest: action %q has scope %q; only public actions belong in the SDK surface", action.Name, action.Scope)
		}
		if !validAPISurfaceMethod(action.Method) {
			return fmt.Errorf("apply API surface manifest: action %q has unsupported or non-canonical HTTP method %q", action.Name, action.Method)
		}
		if !validAPISurfacePath(action.Path) {
			return fmt.Errorf("apply API surface manifest: action %q has invalid HTTP path %q", action.Name, action.Path)
		}
		if action.FlowSchemaDriven {
			return fmt.Errorf("apply API surface manifest: action %q must declare resolved request/response types instead of Flow schema names", action.Name)
		}

		action.Scope = "public"
		action.FlowSchemaDriven = false
		action.ReqSchemaName = ""
		action.ResSchemaName = ""
		if action.RouteSource == "" {
			action.RouteSource = "manifest"
		}
		if action.ContractSource == "" {
			action.ContractSource = "manifest"
		}
		if action.ReqTypeName == "" && action.ReqType.Name != "" {
			action.ReqTypeName = action.ReqType.String()
		}
		if action.ResTypeName == "" && action.ResType.Name != "" {
			action.ResTypeName = action.ResType.String()
		}

		methodName := core.SDKMethodName(*action)
		if !validSDKMethodIdentifier(methodName) {
			return fmt.Errorf("apply API surface manifest: action %q has invalid SDK method name %q", action.Name, methodName)
		}
		if previous, exists := seenMethodNames[methodName]; exists {
			return fmt.Errorf("apply API surface manifest: SDK method name %q is shared by actions %q and %q", methodName, previous, action.Name)
		}
		seenMethodNames[methodName] = action.Name
		action.SDKMethodName = methodName

		routeKey := action.Method + " " + action.Path
		if previous, exists := seenRoutes[routeKey]; exists {
			return fmt.Errorf("apply API surface manifest: route %q is shared by actions %q and %q", routeKey, previous, action.Name)
		}
		seenRoutes[routeKey] = action.Name
	}

	working := *report
	working.Structs = flowSchemaStructs(report, flowSchemaUsage(report))
	working.Actions = actions
	selectedActions := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		selectedActions[action.Name] = struct{}{}
	}
	requestFields := make([][]FieldContract, len(actions))
	responseFields := make([][]FieldContract, len(actions))
	multipart := make([]bool, len(actions))
	blobResponse := make([]bool, len(actions))
	for i, action := range actions {
		requestFields[i] = action.ReqFields
		responseFields[i] = action.ResFields
		multipart[i] = action.Multipart
		blobResponse[i] = action.BlobResponse
	}
	core.CorrelateActionFields(&working)
	for i := range working.Actions {
		if requestFields[i] != nil {
			working.Actions[i].ReqFields = requestFields[i]
		}
		if responseFields[i] != nil {
			working.Actions[i].ResFields = responseFields[i]
		}
		working.Actions[i].Multipart = working.Actions[i].Multipart || multipart[i]
		working.Actions[i].BlobResponse = working.Actions[i].BlobResponse || blobResponse[i]
	}

	working.Diagnostics = make([]Diagnostic, 0, len(report.Diagnostics))
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Rule == "routes/duplicate" {
			continue
		}
		if diagnostic.Action != "" {
			if _, selected := selectedActions[diagnostic.Action]; !selected {
				continue
			}
		}
		working.Diagnostics = append(working.Diagnostics, diagnostic)
	}
	working.APISurfaceManifest = true
	recountReportStats(&working)
	*report = working
	return nil
}

func validAPISurfaceMethod(method string) bool {
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
		http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut,
		http.MethodTrace:
		return true
	default:
		return false
	}
}

func validAPISurfacePath(route string) bool {
	if !strings.HasPrefix(route, "/") || strings.Contains(route, "//") || strings.ContainsAny(route, "?#\\ \t\r\n") {
		return false
	}
	for _, segment := range strings.Split(route, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validSDKMethodIdentifier(value string) bool {
	if value == "" || !isSDKIdentifierStart(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isSDKIdentifierStart(value[i]) && (value[i] < '0' || value[i] > '9') {
			return false
		}
	}
	return true
}

func isSDKIdentifierStart(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func recountReportStats(report *Report) {
	report.Stats.TotalActions = len(report.Actions)
	report.Stats.PublicActions = 0
	report.Stats.InternalActions = 0
	report.Stats.SystemActions = 0
	report.Stats.ErrorCount = 0
	report.Stats.WarningCount = 0
	for _, action := range report.Actions {
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
