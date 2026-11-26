package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeUnqualifiedNewActionBuilder(t *testing.T) {
	root := t.TempDir()
	source := `package api

import (
	"context"
	"github.com/nexssp/transport/thttp"
)

type CreateRequest struct {
	Name string ` + "`json:\"name\" validate:\"required\"`" + `
}
type CreateResponse struct {
	ID string ` + "`json:\"id\"`" + `
}

func Register() {
	_ = New("customers.create", func(_ context.Context, req CreateRequest) (CreateResponse, error) {
		return CreateResponse{}, nil
	}).Description("Create a customer").Route(thttp.POST("/customers")).Build()
}
`
	if err := os.WriteFile(filepath.Join(root, "actions.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := AnalyzeGo(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 1 {
		t.Fatalf("got %d actions, want 1: %#v", len(report.Actions), report.Actions)
	}
	action := report.Actions[0]
	if action.Name != "customers.create" || action.Method != "POST" || action.Path != "/customers" {
		t.Fatalf("unexpected action contract: %#v", action)
	}
	if action.ReqTypeName != "api.CreateRequest" || action.ResTypeName != "api.CreateResponse" {
		t.Fatalf("unqualified constructor lost handler types: request=%q response=%q", action.ReqTypeName, action.ResTypeName)
	}
	if len(action.ReqFields) != 1 || action.ReqFields[0].Key != "name" {
		t.Fatalf("request fields = %#v, want the tagged name field", action.ReqFields)
	}
}

func TestAnalyzeDynamicActionAndRouteValuesAreNotReportedAsLiterals(t *testing.T) {
	root := t.TempDir()
	source := `package api

import (
	"net/http"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

func Register(name, path string) {
	_ = action.New(name, nil).Route(thttp.RawHandler("GET", path, nil)).Build()
	_ = action.New("route.dynamic", nil).Route(thttp.GET(path)).Build()
	_ = action.New("route.static", nil).Route(thttp.GET("/static")).Build()
	_ = action.New("route.raw", nil).Route(thttp.RawHandler(http.MethodGet, "/raw", nil)).Build()
}
`
	if err := os.WriteFile(filepath.Join(root, "actions.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := AnalyzeGo(root)
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]ActionContract, len(report.Actions))
	for _, action := range report.Actions {
		byName[action.Name] = action
	}
	if _, ok := byName["name"]; ok {
		t.Fatal("dynamic action name was reported as the literal name \"name\"")
	}
	dynamicRoute, ok := byName["route.dynamic"]
	if !ok {
		t.Fatal("action with a dynamic route path was not retained as a route-less action contract")
	}
	if dynamicRoute.Method != "" || dynamicRoute.Path != "" {
		t.Fatalf("dynamic route was reported as %s %s", dynamicRoute.Method, dynamicRoute.Path)
	}
	staticRoute, ok := byName["route.static"]
	if !ok || staticRoute.Method != "GET" || staticRoute.Path != "/static" {
		t.Fatalf("static route contract = %#v, want GET /static", staticRoute)
	}
	rawRoute, ok := byName["route.raw"]
	if !ok || rawRoute.Method != "GET" || rawRoute.Path != "/raw" {
		t.Fatalf("raw route contract = %#v, want GET /raw", rawRoute)
	}
}
