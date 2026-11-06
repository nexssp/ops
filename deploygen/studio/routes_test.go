package studio

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"

	"github.com/nexssp/ops/analyzer"
)

type routeTestRequest struct {
	Name string `json:"name" validate:"required"`
}

type routeTestResponse struct {
	Greeting string `json:"greeting"`
}

func TestCatalogMergesGoActionsAndAnalyzerNFlowRoutes(t *testing.T) {
	goAction := action.New("demo.ping", func(_ context.Context, req routeTestRequest) (routeTestResponse, error) {
		return routeTestResponse{Greeting: "hello " + req.Name}, nil
	}).Route(thttp.POST("/api/demo/ping")).Build()

	report := &analyzer.Report{
		Actions: []analyzer.ActionContract{{
			Name:             "flow.ping",
			Method:           "POST",
			Path:             "/api/flow/ping",
			RouteSource:      "nflow",
			FlowSchemaDriven: true,
			ReqSchemaName:    "PingRequest",
			ResSchemaName:    "PingResponse",
			ReqTypeName:      "PingRequest",
			ResTypeName:      "PingResponse",
			ReqFields: []analyzer.FieldContract{{
				Name: "Name", Key: "name", Type: "string", Required: true, Validation: "required",
			}},
			ResFields: []analyzer.FieldContract{{
				Name: "Greeting", Key: "greeting", Type: "string",
			}},
		}},
		FlowHTTPRoutes: []analyzer.FlowHTTPRoute{{
			Pipeline: "pipeline.flow_ping", Method: "POST", Path: "/api/flow/ping",
			SuccessStatus: 201, SourceFile: "routes.nflow", SourceLine: 17,
		}},
	}

	service := NewService([]action.AnyAction{goAction}, report)
	catalog, err := service.handleActions(context.Background(), struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 2 {
		t.Fatalf("catalog has %d entries, want one Go action and one NFlow route: %+v", len(catalog), catalog)
	}

	var goEntry, flowEntry *ActionDTO
	for i := range catalog {
		switch catalog[i].Name {
		case "demo.ping":
			goEntry = &catalog[i]
		case "pipeline.flow_ping":
			flowEntry = &catalog[i]
		}
	}
	if goEntry == nil || goEntry.Kind != "action" || goEntry.Method != "POST" || goEntry.Path != "/api/demo/ping" {
		t.Fatalf("registered Go action metadata was not retained: %+v", goEntry)
	}
	if flowEntry == nil {
		t.Fatalf("analyzer-declared NFlow route was not included: %+v", catalog)
	}
	if flowEntry.Kind != "nflow" || flowEntry.Method != "POST" || flowEntry.Path != "/api/flow/ping" {
		t.Errorf("NFlow route identity = %+v", flowEntry)
	}
	if flowEntry.SourceFile != "routes.nflow" || flowEntry.SourceLine != 17 || flowEntry.SuccessStatus != 201 {
		t.Errorf("NFlow source/status metadata = %+v", flowEntry)
	}
	if !flowEntry.FlowSchemaDriven || flowEntry.ReqSchemaName != "PingRequest" || flowEntry.ResSchemaName != "PingResponse" {
		t.Errorf("NFlow boundary schemas = %+v", flowEntry)
	}
	if flowEntry.ReqType != "PingRequest" || len(flowEntry.ReqFields) != 1 || flowEntry.ReqFields[0].Key != "name" || !flowEntry.ReqFields[0].Required {
		t.Errorf("NFlow request fields = %+v", flowEntry.ReqFields)
	}
	if len(flowEntry.ResFields) != 1 || flowEntry.ResFields[0].Key != "greeting" {
		t.Errorf("NFlow response fields = %+v", flowEntry.ResFields)
	}
}
