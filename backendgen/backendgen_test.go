package backendgen

import (
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestGenerateReusableBackendAndExcludesOptionalRoute(t *testing.T) {
	routes := []analyzer.FlowHTTPRoute{
		{Pipeline: "items_list", Method: "GET", Path: "/api/items"},
		{Pipeline: "pipeline.chaos_probe", Method: "POST", Path: "/api/dev/chaos"},
	}

	source, err := Generate(routes, Config{
		PackageName:      "catalog",
		FunctionName:     "RunCatalogBackend",
		ExcludePipelines: []string{"chaos_probe"},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	generated := string(source)
	for _, want := range []string{
		"package catalog",
		"func RunCatalogBackend(",
		"pipeline.items_list",
		"/api/items",
		"opsGeneratedBackendHasRouteModifier",
		"configureMux func(*http.ServeMux)",
	} {
		if !strings.Contains(generated, want) {
			t.Errorf("generated source missing %q", want)
		}
	}
	if strings.Contains(generated, "pipeline.chaos_probe") || strings.Contains(generated, "/api/dev/chaos") {
		t.Errorf("excluded route leaked into generated source:\n%s", generated)
	}
	if strings.Contains(generated, "github.com/nexssp/ops/") {
		t.Errorf("generated runtime must not require OPS at application runtime")
	}
}

func TestGenerateRejectsUnsafeOrUnsupportedRoutes(t *testing.T) {
	tests := []struct {
		name  string
		route analyzer.FlowHTTPRoute
	}{
		{name: "non-api path", route: analyzer.FlowHTTPRoute{Pipeline: "pipeline.x", Method: "GET", Path: "/admin"}},
		{name: "non-canonical path", route: analyzer.FlowHTTPRoute{Pipeline: "pipeline.x", Method: "GET", Path: "/api/a/../b"}},
		{name: "query string", route: analyzer.FlowHTTPRoute{Pipeline: "pipeline.x", Method: "GET", Path: "/api/items?all=1"}},
		{name: "unsupported method", route: analyzer.FlowHTTPRoute{Pipeline: "pipeline.x", Method: "PATCH", Path: "/api/items"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Generate([]analyzer.FlowHTTPRoute{tt.route}, Config{}); err == nil {
				t.Fatal("Generate unexpectedly accepted invalid route")
			}
		})
	}
}

func TestGenerateRejectsDuplicateEndpointsAndUnknownExclusions(t *testing.T) {
	routes := []analyzer.FlowHTTPRoute{
		{Pipeline: "pipeline.first", Method: "GET", Path: "/api/items"},
		{Pipeline: "pipeline.second", Method: "GET", Path: "/api/items"},
	}
	if _, err := Generate(routes, Config{}); err == nil || !strings.Contains(err.Error(), "duplicate HTTP route") {
		t.Fatalf("duplicate route error = %v", err)
	}
	if _, err := Generate(routes[:1], Config{ExcludePipelines: []string{"missing"}}); err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("unknown exclusion error = %v", err)
	}
}

func TestGenerateFromActionsMountsDeclaredRoutesOnly(t *testing.T) {
	actions := []analyzer.ActionContract{
		{Name: "catalog.list", Method: "GET", Path: "/api/items", RouteSource: "go"},
		{Name: "flow.catalog.list", Method: "GET", Path: "/api/wrong-path", RouteSource: "nflow"},
		{Name: "catalog.rebuild", Scope: "internal"},
		{Name: "debug.trace", Method: "POST", Path: "/api/debug", RouteSource: "go"},
	}
	flowRoutes := []analyzer.FlowHTTPRoute{
		{Pipeline: "catalog_list", Method: "GET", Path: "/api/flow-items"},
		{Pipeline: "items_list", Method: "GET", Path: "/api/skip"},
	}

	source, err := GenerateFromActions(actions, flowRoutes, Config{
		PackageName:      "catalog",
		FunctionName:     "RunCatalogBackend",
		ExcludePipelines: []string{"items_list"},
		ExcludeActions:   []string{"debug.trace"},
	})
	if err != nil {
		t.Fatalf("GenerateFromActions: %v", err)
	}
	generated := string(source)
	for _, want := range []string{
		`Action: "catalog.list"`,
		`Action: "pipeline.catalog_list"`,
		`compiled.Resolver.Action(route.Action)`,
		"RunCatalogBackend(",
	} {
		if !strings.Contains(generated, want) {
			t.Errorf("generated source missing %q\n%s", want, generated)
		}
	}
	for _, omitted := range []string{"pipeline.items_list", "flow.catalog.list", "/api/wrong-path", "catalog.rebuild", "debug.trace", "/api/debug"} {
		if strings.Contains(generated, omitted) {
			t.Errorf("excluded or unrouted action %q leaked into generated source:\n%s", omitted, generated)
		}
	}
}

func TestGenerateFromActionsRejectsIncompleteRouteAndUnknownExclusion(t *testing.T) {
	if _, err := GenerateFromActions([]analyzer.ActionContract{{Name: "catalog.get", Method: "GET"}}, nil, Config{}); err == nil || !strings.Contains(err.Error(), "incomplete HTTP route") {
		t.Fatalf("incomplete route error = %v", err)
	}
	actions := []analyzer.ActionContract{{Name: "catalog.list", Method: "GET", Path: "/api/items", RouteSource: "go"}}
	if _, err := GenerateFromActions(actions, nil, Config{ExcludeActions: []string{"missing.action"}}); err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("unknown action exclusion error = %v", err)
	}
}
