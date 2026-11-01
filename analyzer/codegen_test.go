package analyzer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestGenerateSDKFromSourceTree(t *testing.T) {
	root := t.TempDir()
	source := `package sampleapi

import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type CreateWidgetRequest struct {
	Name string ` + "`json:\"name\" validate:\"required\"`" + `
}

type Widget struct {
	ID string ` + "`json:\"id\"`" + `
}

func RegisterActions() action.AnyAction {
	return action.New[CreateWidgetRequest, Widget]("widgets.create", nil).
		Route(thttp.POST("/api/widgets")).
		Build()
}
`
	if err := os.WriteFile(filepath.Join(root, "api.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := analyzer.Analyze(root)
	if err != nil {
		t.Fatalf("analyze source tree: %v", err)
	}

	var apiTypes, endpoints string
	for _, file := range analyzer.GenerateTypeScriptAPISDK(report) {
		switch file.Path {
		case "_gen/api/types.ts":
			apiTypes = string(file.Content)
		case "_gen/api/endpoints.ts":
			endpoints = string(file.Content)
		}
	}
	if !strings.Contains(apiTypes, "export interface CreateWidgetRequest") || !strings.Contains(apiTypes, "export interface Widget {") {
		t.Fatalf("generated types do not reflect the scanned source:\n%s", apiTypes)
	}
	if !strings.Contains(endpoints, "widgetsCreate") || !strings.Contains(endpoints, "/api/widgets") {
		t.Fatalf("generated endpoints do not reflect the scanned source:\n%s", endpoints)
	}
}
