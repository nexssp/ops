package flowstudio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStudio(t *testing.T, run commandRunner) (*Studio, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.nflow")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	studio, err := newStudio(path, run)
	if err != nil {
		t.Fatal(err)
	}
	return studio, path
}

func studioRequest(t *testing.T, studio *Studio, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "http://127.0.0.1:54321"+path, strings.NewReader(body))
	request.Host = "127.0.0.1:54321"
	request.AddCookie(&http.Cookie{Name: "flowstudio", Value: studio.token})
	if method != http.MethodGet {
		request.Header.Set("Origin", "http://"+request.Host)
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	studio.Handler().ServeHTTP(response, request)
	return response
}

func TestProjectLoadAndSave(t *testing.T) {
	studio, path := newTestStudio(t, func(context.Context, []string) (commandOutput, error) {
		return commandOutput{}, nil
	})
	loaded := studioRequest(t, studio, http.MethodGet, "/api/project", "")
	if loaded.Code != http.StatusOK || !strings.Contains(loaded.Body.String(), `"file":"app.nflow"`) {
		t.Fatalf("load status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	const source = "@description \"edited in Studio\"\n{}\n"
	requestBody, err := json.Marshal(map[string]string{"source": source})
	if err != nil {
		t.Fatal(err)
	}
	saved := studioRequest(t, studio, http.MethodPost, "/api/save", string(requestBody))
	if saved.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", saved.Code, saved.Body.String())
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != source {
		t.Fatalf("saved source=%q, want %q", actual, source)
	}
}

func TestValidateAndRunUseFlowCLI(t *testing.T) {
	var calls [][]string
	studio, _ := newTestStudio(t, func(_ context.Context, args []string) (commandOutput, error) {
		calls = append(calls, append([]string(nil), args...))
		return commandOutput{Stdout: `{"ok":true}`}, nil
	})
	validated := studioRequest(t, studio, http.MethodPost, "/api/validate", `{}`)
	if validated.Code != http.StatusOK {
		t.Fatalf("validate status=%d body=%s", validated.Code, validated.Body.String())
	}
	if len(calls) != 1 || len(calls[0]) != 4 || calls[0][0] != "flow" || calls[0][2] != "--info" || calls[0][3] != "--json" {
		t.Fatalf("validation command args = %#v", calls)
	}

	run := studioRequest(t, studio, http.MethodPost, "/api/run", `{"payload":{"name":"Ada","count":2}}`)
	if run.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", run.Code, run.Body.String())
	}
	if len(calls) != 2 || len(calls[1]) != 4 || calls[1][0] != "flow" || calls[1][3] != "--json" {
		t.Fatalf("run command args = %#v", calls)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(calls[1][2]), &payload); err != nil || payload["name"] != "Ada" {
		t.Fatalf("run payload arg = %q, err=%v", calls[1][2], err)
	}
}

func TestFailuresAndUnsupportedOperationsAreVisible(t *testing.T) {
	studio, _ := newTestStudio(t, func(context.Context, []string) (commandOutput, error) {
		return commandOutput{Stderr: "compile: unknown action"}, errors.New("exit status 1")
	})
	failed := studioRequest(t, studio, http.MethodPost, "/api/validate", `{}`)
	if failed.Code != http.StatusUnprocessableEntity || !strings.Contains(failed.Body.String(), "compile: unknown action") {
		t.Fatalf("validation failure was not visible: status=%d body=%s", failed.Code, failed.Body.String())
	}
	unsupported := studioRequest(t, studio, http.MethodPost, "/api/delete", `{}`)
	if unsupported.Code != http.StatusNotImplemented || !strings.Contains(unsupported.Body.String(), "Not implemented") {
		t.Fatalf("unsupported operation was not visible: status=%d body=%s", unsupported.Code, unsupported.Body.String())
	}
}

func TestStudioRejectsForeignHostsAndOrigins(t *testing.T) {
	studio, path := newTestStudio(t, func(context.Context, []string) (commandOutput, error) {
		t.Fatal("foreign requests must not invoke Flow")
		return commandOutput{}, nil
	})
	foreignHost := httptest.NewRequest(http.MethodGet, "http://example.test/api/project", nil)
	foreignHost.Host = "example.test"
	foreignHost.AddCookie(&http.Cookie{Name: "flowstudio", Value: studio.token})
	response := httptest.NewRecorder()
	studio.Handler().ServeHTTP(response, foreignHost)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign Host status=%d body=%s", response.Code, response.Body.String())
	}

	// A foreign Origin must not modify the source.
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:54321/api/save", strings.NewReader(`{"source":"changed"}`))
	request.Host = "127.0.0.1:54321"
	request.AddCookie(&http.Cookie{Name: "flowstudio", Value: studio.token})
	request.Header.Set("Origin", "http://evil.test")
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	studio.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign Origin status=%d body=%s", response.Code, response.Body.String())
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "{}\n" {
		t.Fatalf("foreign request changed source to %q", actual)
	}
}

func TestNewRequiresExistingNFlowFile(t *testing.T) {
	if _, err := newStudio(filepath.Join(t.TempDir(), "missing.nflow"), func(context.Context, []string) (commandOutput, error) {
		return commandOutput{}, nil
	}); err == nil {
		t.Fatal("New accepted a missing Flow file")
	}
	path := filepath.Join(t.TempDir(), "app.txt")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newStudio(path, func(context.Context, []string) (commandOutput, error) {
		return commandOutput{}, nil
	}); err == nil || !strings.Contains(err.Error(), ".nflow") {
		t.Fatalf("New error = %v, want an .nflow restriction", err)
	}
}
