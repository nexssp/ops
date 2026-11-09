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
