package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStudioWorkBenchAndDemoEndpoints(t *testing.T) {
	handler := newHandler()

	ui := httptest.NewRecorder()
	handler.ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "/studio/", nil))
	if ui.Code != http.StatusOK || !strings.Contains(ui.Body.String(), "Action Workbench") {
		t.Fatalf("GET /studio/: status=%d body=%q", ui.Code, ui.Body.String())
	}

	catalog := httptest.NewRecorder()
	handler.ServeHTTP(catalog, httptest.NewRequest(http.MethodGet, "/studio/api/actions", nil))
	if catalog.Code != http.StatusOK {
		t.Fatalf("GET action catalog: status=%d body=%q", catalog.Code, catalog.Body.String())
	}
	var actions []map[string]any
	if err := json.Unmarshal(catalog.Body.Bytes(), &actions); err != nil {
		t.Fatalf("decode action catalog: %v", err)
	}
	if len(actions) != 5 {
		t.Fatalf("catalog has %d operations, want four Go actions and one NFlow route", len(actions))
	}
	goCount, flowCount := 0, 0
	for _, item := range actions {
		if strings.HasPrefix(item["name"].(string), "studio.") {
			t.Fatalf("internal Studio action leaked into business catalog: %v", item["name"])
		}
		switch item["kind"] {
		case "action":
			goCount++
		case "nflow":
			flowCount++
			if item["name"] != "pipeline.demo_nflow_echo" || item["method"] != "POST" || item["path"] != "/api/demo/nflow-echo" {
				t.Errorf("NFlow route identity does not match app.nflow: %+v", item)
			}
			if item["source_file"] == "" || item["request_schema"] != "DemoFlowRequest" || item["response_schema"] != "DemoFlowResponse" {
				t.Errorf("NFlow source/schema metadata missing: %+v", item)
			}
		default:
			t.Errorf("operation has unknown source kind: %+v", item)
		}
	}
	if goCount != 4 || flowCount != 1 {
		t.Fatalf("catalog has %d Go actions and %d NFlow routes, want 4 and 1", goCount, flowCount)
	}

	security := httptest.NewRecorder()
	handler.ServeHTTP(security, httptest.NewRequest(http.MethodGet, "/system/security", nil))
	var universe map[string]any
	if err := json.Unmarshal(security.Body.Bytes(), &universe); err != nil {
		t.Fatalf("decode security universe: %v", err)
	}
	if len(universe["roles"].([]any)) != 1 || universe["roles"].([]any)[0] != "operator" {
		t.Fatalf("secured action metadata missing from claims matrix: %v", universe)
	}

	scenario := postJSON(t, handler, "/studio/api/ai/scenario", map[string]any{
		"action_name": "demo.calculate", "scenario": "happy",
	})
	var generated struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(scenario.Body.Bytes(), &generated); err != nil {
		t.Fatalf("decode generated scenario: %v", err)
	}
	if generated.Payload["operator"] != "+" || generated.Payload["left"] != float64(10) {
		t.Fatalf("happy-path payload does not match action contract: %v", generated.Payload)
	}

	payload := map[string]any{"action_name": "demo.echo", "payload": map[string]any{"message": "hello", "repeat": 2}}
	execution := postJSON(t, handler, "/studio/api/execute", payload)
	if execution.Code != http.StatusOK {
		t.Fatalf("execute: status=%d body=%q", execution.Code, execution.Body.String())
	}
	var record map[string]any
	if err := json.Unmarshal(execution.Body.Bytes(), &record); err != nil {
		t.Fatalf("decode execution record: %v", err)
	}
	if record["success"] != true {
		t.Fatalf("execution did not succeed: %s", execution.Body.String())
	}
	response, ok := record["response"].(map[string]any)
	if !ok || response["message"] != "hello" || response["repetitions"] != float64(2) {
		t.Fatalf("unexpected response: %#v", record["response"])
	}
	spans, ok := record["waterfall"].([]any)
	if !ok || len(spans) < 2 {
		t.Fatalf("trace waterfall missing decode/action spans: %#v", record["waterfall"])
	}

	fail := postJSON(t, handler, "/studio/api/execute", map[string]any{
		"action_name": "demo.validate", "payload": map[string]any{"title": "reject"},
	})
	var failed map[string]any
	if err := json.Unmarshal(fail.Body.Bytes(), &failed); err != nil {
		t.Fatalf("decode failed execution: %v", err)
	}
	if failed["success"] != false || failed["status"] != float64(http.StatusBadRequest) {
		t.Fatalf("error response was not captured: %s", fail.Body.String())
	}
	if _, ok := failed["error"].(map[string]any); !ok {
		t.Fatalf("expected structured error taxonomy, got %#v", failed["error"])
	}

	history := httptest.NewRecorder()
	handler.ServeHTTP(history, httptest.NewRequest(http.MethodGet, "/studio/api/history?limit=50", nil))
	var records []map[string]any
	if err := json.Unmarshal(history.Body.Bytes(), &records); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("history has %d records, want 2", len(records))
	}
}

func TestDemoBusinessEndpoint(t *testing.T) {
	body := []byte(`{"message":"direct","repeat":3}`)
	recorder := httptest.NewRecorder()
	newHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/demo/echo", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST /api/demo/echo: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	var response EchoResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode echo response: %v", err)
	}
	if response.Message != "direct" || response.Repetitions != 3 || response.GeneratedAt == "" {
		t.Fatalf("unexpected echo result: %+v", response)
	}

	zeroOperand := httptest.NewRecorder()
	newHandler().ServeHTTP(zeroOperand, httptest.NewRequest(http.MethodPost, "/api/demo/calculate", strings.NewReader(`{"left":0,"right":3,"operator":"+"}`)))
	if zeroOperand.Code != http.StatusOK {
		t.Fatalf("zero should be a valid arithmetic operand: status=%d body=%q", zeroOperand.Code, zeroOperand.Body.String())
	}
}

func TestLiveNFlowRouteAndRegisteredGoAction(t *testing.T) {
	baseURL := startDemoBackend(t)

	catalogResponse, err := http.Get(baseURL + "/studio/api/actions")
	if err != nil {
		t.Fatal(err)
	}
	defer catalogResponse.Body.Close()
	var catalog []map[string]any
	if err := json.NewDecoder(catalogResponse.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode live catalog: %v", err)
	}
	foundFlowRoute, foundGoAction := false, false
	for _, entry := range catalog {
		if entry["kind"] == "nflow" && entry["path"] == "/api/demo/nflow-echo" {
			foundFlowRoute = true
		}
		if entry["kind"] == "action" && entry["name"] == "demo.calculate" {
			foundGoAction = true
		}
	}
	if !foundFlowRoute || !foundGoAction {
		t.Fatalf("live Studio catalog missing NFlow route or Go action: %+v", catalog)
	}

	flowResponse := postURLJSON(t, baseURL+"/api/demo/nflow-echo", map[string]any{"message": "through nflow", "repeat": 3})
	defer flowResponse.Body.Close()
	if flowResponse.StatusCode != http.StatusOK {
		t.Fatalf("NFlow endpoint returned HTTP %d", flowResponse.StatusCode)
	}
	var echo EchoResponse
	if err := json.NewDecoder(flowResponse.Body).Decode(&echo); err != nil {
		t.Fatalf("decode NFlow pipeline response: %v", err)
	}
	if echo.Message != "through nflow" || echo.Repetitions != 3 || echo.GeneratedAt == "" {
		t.Fatalf("NFlow pipeline did not execute demo.echo: %+v", echo)
	}

	goResponse := postURLJSON(t, baseURL+"/studio/api/execute", map[string]any{
		"action_name": "demo.calculate",
		"payload":     map[string]any{"left": 7, "right": 5, "operator": "+"},
	})
	defer goResponse.Body.Close()
	var execution struct {
		Success  bool              `json:"success"`
		Response CalculateResponse `json:"response"`
	}
	if err := json.NewDecoder(goResponse.Body).Decode(&execution); err != nil {
		t.Fatalf("decode Go action execution: %v", err)
	}
	if !execution.Success || execution.Response.Result != 12 {
		t.Fatalf("registered Go action did not execute through Studio: %+v", execution)
	}
}

func startDemoBackend(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var runErr error
	t.Cleanup(func() {
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
		cancel()
		select {
		case <-done:
			if runErr != nil {
				t.Errorf("generated demo backend stopped with error: %v", runErr)
			}
		case <-time.After(15 * time.Second):
			t.Error("generated demo backend did not stop after cancellation")
		}
	})
	go func() {
		service, bundles := demoRuntime()
		configureMux := func(mux *http.ServeMux) { mux.Handle("/", service.Handler()) }
		runErr = RunStudioDemoBackend(ctx, demoFlowFile(), address, bundles, configureMux)
		close(done)
	}()

	baseURL := "http://" + address
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + "/studio/api/actions")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return baseURL
			}
		}
		select {
		case <-done:
			t.Fatalf("generated demo backend exited before serving HTTP: %v", runErr)
		default:
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("generated demo backend did not become ready at %s", baseURL)
	return ""
}

func postURLJSON(t *testing.T, url string, payload any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestStudioIdentityOverrideRequiresDevelopmentOptIn(t *testing.T) {
	handler := newHandler()
	request := map[string]any{
		"action_name": "demo.secure-preview",
		"payload":     map[string]any{"document_id": "doc-42"},
		"identity": map[string]any{
			"user_id": "dev-user", "tenant_id": "ws-dev", "roles": []string{"operator"},
			"permissions": []string{"demo.write"}, "features": []string{"preview-v2"},
		},
	}
	t.Setenv("APP_ENV", "production")
	t.Setenv("NEXSS_STUDIO_ALLOW_IDENTITY_OVERRIDE", "true")
	production := postJSON(t, handler, "/studio/api/execute", request)
	var productionRecord map[string]any
	if err := json.Unmarshal(production.Body.Bytes(), &productionRecord); err != nil {
		t.Fatal(err)
	}
	if productionRecord["success"] != false {
		t.Fatal("client-supplied claims were honored in production")
	}

	t.Setenv("APP_ENV", "development")
	development := postJSON(t, handler, "/studio/api/execute", request)
	var developmentRecord map[string]any
	if err := json.Unmarshal(development.Body.Bytes(), &developmentRecord); err != nil {
		t.Fatal(err)
	}
	if developmentRecord["success"] != true {
		t.Fatalf("explicit development identity override was not applied: %s", development.Body.String())
	}
}

func postJSON(t *testing.T, handler http.Handler, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
	return recorder
}
