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
