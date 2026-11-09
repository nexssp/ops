package analyzer_test

import (
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
	"github.com/nexssp/ops/analyzer/core"
)

func TestNFlowBoundarySchemasDrivePublicTypesWithoutGoFallback(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "catalog/actions.go", `package catalog
import (
  "context"
  "github.com/nexssp/kernel/action"
)
type GoCreateRequest struct { Legacy string `+"`json:\"legacy\"`"+` }
type GoCreateResponse struct { LegacyID string `+"`json:\"legacy_id\"`"+` }
type GoListRequest struct { LegacyFilter string `+"`json:\"legacy_filter\"`"+` }
type GoListResponse struct { LegacyItems []string `+"`json:\"legacy_items\"`"+` }
func Create() action.AnyAction {
  return action.New("catalog.create", func(context.Context, GoCreateRequest) (GoCreateResponse, error) {
    return GoCreateResponse{}, nil
  }).Build()
}
func List() action.AnyAction {
  return action.New("catalog.list", func(context.Context, GoListRequest) (GoListResponse, error) {
    return GoListResponse{}, nil
  }).Build()
}
`)
	writeFlow(t, root, "routes.nflow", `@schema PublicDetail {
  Score int `+"`json:\"score\" validate:\"gte=0\"`"+`
}
@schema PublicCreateRequest {
  Name string `+"`json:\"name\" validate:\"required\"`"+`
  Detail PublicDetail `+"`json:\"detail\" validate:\"required\"`"+`
  Note *string `+"`json:\"note,omitempty\"`"+`
}
@schema PublicCreateResponse {
  ID string `+"`json:\"id\" validate:\"required\"`"+`
  Detail PublicDetail `+"`json:\"detail\" validate:\"required\"`"+`
}
@schema PublicListResponse {
  Items []PublicDetail `+"`json:\"items\" validate:\"required,dive\"`"+`
}
@pipeline catalog_create :route="POST /api/catalog" :request-schema=PublicCreateRequest :response-schema=PublicCreateResponse
  { name: .name, detail: .detail } -> catalog.create
@end
@pipeline catalog_list :route="GET /api/catalog" :response-schema=PublicListResponse
  {} -> catalog.list
@end
`)

	report, err := analyzer.Analyze(root)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Stats.ErrorCount != 0 {
		t.Fatalf("unexpected analyzer diagnostics: %+v", report.Diagnostics)
	}
	var create, list *analyzer.ActionContract
	for i := range report.Actions {
		switch report.Actions[i].Name {
		case "catalog.create":
			create = &report.Actions[i]
		case "catalog.list":
			list = &report.Actions[i]
		}
	}
	if create == nil || list == nil {
		t.Fatalf("NFlow route actions missing: %+v", report.Actions)
	}
	if !create.FlowSchemaDriven || create.ReqSchemaName != "PublicCreateRequest" || create.ResSchemaName != "PublicCreateResponse" {
		t.Errorf("create boundary schema binding = %+v", create)
	}
	if create.ReqTypeName != "PublicCreateRequest" || create.ResTypeName != "PublicCreateResponse" {
		t.Errorf("create Go types were not replaced by NFlow schemas: req=%q res=%q", create.ReqTypeName, create.ResTypeName)
	}
	if !list.FlowSchemaDriven || list.ReqSchemaName != "" || list.ReqTypeName != "" || len(list.ReqFields) != 0 {
		t.Errorf("schema-less GET request was inferred from its Go action: %+v", list)
	}
	if list.ResTypeName != "PublicListResponse" {
		t.Errorf("list response schema = %q, want PublicListResponse", list.ResTypeName)
	}
	if _, ok := report.Structs["nflow.PublicCreateRequest"]; !ok {
		t.Fatal("Flow request schema is missing from the returned report's Structs")
	}
	if len(create.ReqFields) != 3 || len(create.ResFields) != 2 {
		t.Errorf("Flow request/response fields were not correlated: request=%+v response=%+v", create.ReqFields, create.ResFields)
	}

	files := generatedByName(analyzer.GenerateTypeScriptAPISDK(report))
	types := string(files["_gen/api/types.ts"])
	endpoints := string(files["_gen/api/endpoints.ts"])
	if !strings.Contains(types, "export interface PublicCreateRequest") || !strings.Contains(types, "name: string;") {
		t.Errorf("NFlow request schema missing from generated types:\n%s", types)
	}
	if !strings.Contains(types, "detail: PublicDetail;") || !strings.Contains(types, "export interface PublicDetail") {
		t.Errorf("nested NFlow schema was not emitted recursively:\n%s", types)
	}
	if !strings.Contains(types, "note?: string | null;") {
		t.Errorf("NFlow pointer optionality/nullability was lost:\n%s", types)
	}
	if strings.Contains(types, "GoCreateRequest") || strings.Contains(types, "GoCreateResponse") || strings.Contains(types, "GoListRequest") || strings.Contains(types, "GoListResponse") {
		t.Errorf("Go-AST DTO leaked into schema-driven public types:\n%s", types)
	}
	if strings.Contains(types, "unknown") || strings.Contains(types, "z.unknown()") {
		t.Errorf("generated schema types contain an unknown fallback:\n%s", types)
	}
	if !strings.Contains(endpoints, "catalogCreate: (req: Types.PublicCreateRequest): Promise<Types.PublicCreateResponse>") {
		t.Errorf("create endpoint signature is not strongly schema-typed:\n%s", endpoints)
	}
	if !strings.Contains(endpoints, "catalogList: (): Promise<Types.PublicListResponse>") {
		t.Errorf("request-none GET endpoint did not retain its no-argument semantics:\n%s", endpoints)
	}
	if strings.Contains(endpoints, "Types.GoListRequest") || strings.Contains(endpoints, "Types.GoListResponse") {
		t.Errorf("Go DTO fallback leaked into NFlow endpoint signatures:\n%s", endpoints)
	}
	directCoreFiles := generatedByName(core.GenerateTypeScriptSDK(report))
	directCoreTypes := string(directCoreFiles["_gen/api/types.ts"])
	directCoreEndpoints := string(directCoreFiles["_gen/api/endpoints.ts"])
	if !strings.Contains(directCoreTypes, "export interface PublicCreateRequest") || !strings.Contains(directCoreEndpoints, "Types.PublicCreateRequest") {
		t.Errorf("direct core SDK generation dropped Flow DTOs or action types:\ntypes:\n%s\nendpoints:\n%s", directCoreTypes, directCoreEndpoints)
	}
	second := generatedByName(analyzer.GenerateTypeScriptAPISDK(report))
	for path, content := range files {
		if string(second[path]) != string(content) {
			t.Errorf("repeated generation changed %s", path)
		}
	}
}

func TestNFlowBoundarySchemaRejectsUntypedPublicField(t *testing.T) {
	root := t.TempDir()
	writeFlow(t, root, "routes.nflow", `@schema BadPublicResponse {
  Payload any `+"`json:\"payload\"`"+`
}
@pipeline bad :route="GET /api/bad" :response-schema=BadPublicResponse
  runtime.noop
@end
`)
	report, err := analyzer.Analyze(root)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if report.Stats.ErrorCount == 0 {
		t.Fatalf("an untyped public field was silently accepted: %+v", report.FlowSchemas)
	}
	found := false
	for _, diagnostic := range report.Diagnostics {
		found = found || strings.Contains(diagnostic.Message, "untyped schema value")
	}
	if !found {
		t.Fatalf("diagnostics = %+v; want an explicit untyped-field error", report.Diagnostics)
	}
}
