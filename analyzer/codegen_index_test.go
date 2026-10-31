package analyzer_test

import (
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestCodegen_VoidReqZeroArgMethod(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "ws.go", `package ws
import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
type FileListRes struct { Files []string `+"`json:\"files\"`"+` }
func Files() action.AnyAction {
	return action.New[struct{}, FileListRes]("workspace.files", nil).
		Route(thttp.GET("/api/workspace/files")).Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	sdk := string(generatedByName(analyzer.GenerateTypeScriptSDK(report))["_gen/api/endpoints.ts"])

	if !strings.Contains(sdk, "workspaceFiles: (): Promise<Types.FileListRes>") {
		t.Errorf("expected zero-arg signature; got:\n%s", extractApiObject(sdk))
	}
	if strings.Contains(sdk, "req?: void") || strings.Contains(sdk, "req: void") {
		t.Error("void req should not appear as a parameter")
	}
}

func TestCodegen_SSEAndRawExcludedFromApiButInMeta(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"net/http"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
type B struct{}
func (b *B) StreamAction() action.AnyAction {
	return action.New[any, any]("console.stream", nil).
		Route(thttp.Stream("/api/events").WithChannel("events")).Build()
}
func ViewAction() action.AnyAction {
	h := func(w http.ResponseWriter, r *http.Request) {}
	return action.New[struct{}, struct{}]("workspace.view", nil).
		Route(thttp.RawHandler(http.MethodGet, "/api/view", h)).Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	sdk := string(generatedByName(analyzer.GenerateTypeScriptSDK(report))["_gen/api/endpoints.ts"])

	if !strings.Contains(sdk, `consoleStream: { name: "console.stream", method: "SSE", path: "/api/events", streaming: true, raw: false`) {
		t.Errorf("consoleStream missing from endpointMeta:\n%s", sdk)
	}
	if !strings.Contains(sdk, `workspaceView: { name: "workspace.view", method: "GET", path: "/api/view", streaming: false, raw: true`) {
		t.Errorf("workspaceView missing from endpointMeta:\n%s", sdk)
	}

	// api object should NOT contain methods for SSE/Raw
	apiBody := extractApiObject(sdk)
	if strings.Contains(apiBody, "consoleStream:") {
		t.Errorf("consoleStream should be excluded from api object")
	}
	if strings.Contains(apiBody, "workspaceView:") {
		t.Errorf("workspaceView should be excluded from api object")
	}
}

// extractApiObject returns the substring of index.ts between
// `export const api = {` and the closing `};`.
func extractApiObject(sdk string) string {
	const marker = "export const api = {"
	i := strings.Index(sdk, marker)
	if i < 0 {
		return ""
	}
	body := sdk[i+len(marker):]
	end := strings.Index(body, "\n};")
	if end < 0 {
		return body
	}
	return body[:end]
}
