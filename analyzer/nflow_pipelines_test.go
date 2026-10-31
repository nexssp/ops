package analyzer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
	"github.com/nexssp/ops/backendgen"
)

func TestNFlowPipelineGenerationForStandaloneApp(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "catalog/service.go", `package catalog
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type CreateReq struct { Name string `+"`json:\"name\" validate:\"required\"`"+` }
type Item struct { ID string `+"`json:\"id\"`"+` }
func Create() action.AnyAction {
	return action.New("catalog.create", func(ctx context.Context, req CreateReq) (Item, error) {
		return Item{}, nil
	}).Build()
}
`)
	if err := os.MkdirAll(filepath.Join(root, "flows"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFlow(t, root, "flows/routes.nflow", `@pipeline catalog_create :route="POST /api/catalog" :status=201
  { name: .name } -> catalog.create
@end
`)
	writeFlow(t, root, "flows/backend.nflow", `@include "routes.nflow"
`)

	report, err := analyzer.Analyze(root)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(report.FlowHTTPRoutes) != 1 {
		t.Fatalf("Flow HTTP routes = %+v, want one route", report.FlowHTTPRoutes)
	}
	flowRoute := report.FlowHTTPRoutes[0]
	if flowRoute.Pipeline != "pipeline.catalog_create" || flowRoute.Method != "POST" || flowRoute.Path != "/api/catalog" || flowRoute.SuccessStatus != 201 {
		t.Errorf("Flow route manifest = %+v, want pipeline.catalog_create POST /api/catalog status 201", flowRoute)
	}
	generatedBackend, err := backendgen.Generate(report.FlowHTTPRoutes, backendgen.Config{PackageName: "catalog"})
	if err != nil {
		t.Fatalf("Generate backend for independent app: %v", err)
	}
	if !strings.Contains(string(generatedBackend), "pipeline.catalog_create") || !strings.Contains(string(generatedBackend), "/api/catalog") {
		t.Errorf("generated reusable backend omitted the route:\n%s", generatedBackend)
	}
	var found *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "catalog.create" {
			found = &report.Actions[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("pipeline route did not bind to catalog.create: %+v", report.Actions)
	}
	if found.Method != "POST" || found.Path != "/api/catalog" || found.RouteSource != "nflow" {
		t.Errorf("route = %s %s source=%q; want POST /api/catalog from nflow", found.Method, found.Path, found.RouteSource)
	}
	if found.SuccessStatus != 201 {
		t.Errorf("success status = %d, want 201", found.SuccessStatus)
	}
	if !strings.Contains(found.ReqTypeName, "CreateReq") || !strings.Contains(found.ResTypeName, "Item") {
		t.Errorf("Go request/response types were not retained: req=%q res=%q", found.ReqTypeName, found.ResTypeName)
	}
	if len(found.Transports) != 1 || found.Transports[0].Kind != "http" {
		t.Errorf("HTTP transport = %+v, want one HTTP binding", found.Transports)
	}

	files := make(map[string][]byte)
	for _, generated := range analyzer.GenerateTypeScriptSDK(report) {
		files[generated.Path] = generated.Content
	}
	endpoints := string(files["_gen/api/endpoints.ts"])
	types := string(files["_gen/api/types.ts"])
	if !strings.Contains(endpoints, `POST`) || !strings.Contains(endpoints, `"/api/catalog"`) {
		t.Errorf("generated endpoints omit the nflow route:\n%s", endpoints)
	}
	if !strings.Contains(types, "export interface CreateReq") || !strings.Contains(types, "export interface Item") {
		t.Errorf("generated SDK omitted Go-derived DTOs:\n%s", types)
	}

	// A root @include and the included file are both encountered by the
	// workspace walk. The route and binding should be emitted only once.
	if len(report.Actions) != 1 {
		t.Errorf("an included pipeline was counted more than once: got %d actions", len(report.Actions))
	}
	if len(report.FlowHTTPRoutes) != 1 {
		t.Errorf("an included Flow route was counted more than once: got %d", len(report.FlowHTTPRoutes))
	}
	if _, err := os.Stat(filepath.Join(root, "flows", "backend.nflow")); err != nil {
		t.Fatal(err)
	}
}
