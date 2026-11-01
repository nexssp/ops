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

func TestIncludedFilesAppearInTreeAndCanBeOpenedAndSaved(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "app.nflow")
	first := filepath.Join(root, "modules", "normalize.nflow")
	second := filepath.Join(root, "modules", "respond.nflow")
	if err := os.MkdirAll(filepath.Dir(first), 0o700); err != nil {
		t.Fatal(err)
	}
	entrySource := "@include \"modules/normalize.nflow\" AS normalize\n@include \"modules/respond.nflow\" AS respond\n"
	moduleSource := "@pipeline normalize\n{} -> runtime.noop\n@end\n"
	if err := os.WriteFile(entry, []byte(entrySource), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte(moduleSource), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	studio, err := newStudio(entry, func(context.Context, []string) (commandOutput, error) {
		return commandOutput{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded := studioRequest(t, studio, http.MethodGet, "/api/project", "")
	var project struct {
		Tree       studioFile `json:"tree"`
		IndexError string     `json:"index_error"`
	}
	if err := json.Unmarshal(loaded.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	if loaded.Code != http.StatusOK || project.IndexError != "" {
		t.Fatalf("project status=%d index_error=%q body=%s", loaded.Code, project.IndexError, loaded.Body.String())
	}
	if len(project.Tree.Children) != 2 || project.Tree.Children[0].Path != "modules/normalize.nflow" || project.Tree.Children[1].Path != "modules/respond.nflow" {
		t.Fatalf("include tree = %#v", project.Tree)
	}
	opened := studioRequest(t, studio, http.MethodGet, "/api/file?path=modules%2Fnormalize.nflow", "")
	if opened.Code != http.StatusOK || !strings.Contains(opened.Body.String(), `"file":"modules/normalize.nflow"`) || !strings.Contains(opened.Body.String(), "runtime.noop") {
		t.Fatalf("included file open status=%d body=%s", opened.Code, opened.Body.String())
	}
	const edited = "@pipeline normalize\n{ value: 2 } -> runtime.noop\n@end\n"
	body, err := json.Marshal(map[string]string{"path": "modules/normalize.nflow", "source": edited})
	if err != nil {
		t.Fatal(err)
	}
	saved := studioRequest(t, studio, http.MethodPost, "/api/save", string(body))
	actual, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Code != http.StatusOK || string(actual) != edited {
		t.Fatalf("included file save status=%d body=%s source=%q", saved.Code, saved.Body.String(), actual)
	}
}

func TestExplainReturnsFlowPlanForEntrypointAndIncludedModules(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "app.nflow")
	child := filepath.Join(root, "included.nflow")
	if err := os.WriteFile(entry, []byte("@include \"included.nflow\" AS included\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte("@pipeline prepare\n{} -> runtime.noop\n@end\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	studio, err := newStudio(entry, func(_ context.Context, args []string) (commandOutput, error) {
		calls = append(calls, append([]string(nil), args...))
		return commandOutput{Stdout: `{"path":"plan.nflow","pipelines":[]}`}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response := studioRequest(t, studio, http.MethodGet, "/api/explain", "")
	if response.Code != http.StatusOK {
		t.Fatalf("Explain status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Entrypoint string       `json:"entrypoint"`
		Map        studioFile   `json:"map"`
		Modules    []modulePlan `json:"modules"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Entrypoint != "app.nflow" || len(result.Map.Children) != 1 || len(result.Modules) != 2 {
		t.Fatalf("Explain response = %#v", result)
	}
	if len(calls) != 2 {
		t.Fatalf("Flow Explain calls = %#v, want entrypoint and included module", calls)
	}
	for _, call := range calls {
		if len(call) != 5 || call[0] != "flow" || call[1] != "explain" || call[2] != "--plan" || call[3] != "--json" {
			t.Fatalf("Flow Explain command = %#v", call)
		}
	}
}

func TestMissingAndEscapingIncludesAreReportedWithoutExposingFiles(t *testing.T) {
	t.Run("missing include", func(t *testing.T) {
		root := t.TempDir()
		entry := filepath.Join(root, "app.nflow")
		if err := os.WriteFile(entry, []byte("@include \"missing.nflow\" AS missing\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		studio, err := newStudio(entry, func(context.Context, []string) (commandOutput, error) {
			t.Fatal("Explain must not run when the Flow include index has an error")
			return commandOutput{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		project := studioRequest(t, studio, http.MethodGet, "/api/project", "")
		if project.Code != http.StatusOK || !strings.Contains(project.Body.String(), "missing.nflow") {
			t.Fatalf("missing include was not visible in project: status=%d body=%s", project.Code, project.Body.String())
		}
		explained := studioRequest(t, studio, http.MethodGet, "/api/explain", "")
		if explained.Code != http.StatusUnprocessableEntity || !strings.Contains(explained.Body.String(), "missing.nflow") {
			t.Fatalf("missing include Explain status=%d body=%s", explained.Code, explained.Body.String())
		}
	})

	t.Run("traversal and symlink escape", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "project")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		entry := filepath.Join(root, "app.nflow")
		outside := filepath.Join(parent, "secret.nflow")
		if err := os.WriteFile(entry, []byte("@include \"linked.nflow\" AS linked\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		const secret = "do not expose this file\n"
		if err := os.WriteFile(outside, []byte(secret), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "linked.nflow")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		studio, err := newStudio(entry, func(context.Context, []string) (commandOutput, error) {
			return commandOutput{}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		project := studioRequest(t, studio, http.MethodGet, "/api/project", "")
		if !strings.Contains(project.Body.String(), "outside the Flow Studio project") || strings.Contains(project.Body.String(), secret) {
			t.Fatalf("symlink escape was not safely reported: %s", project.Body.String())
		}
		traversal := studioRequest(t, studio, http.MethodGet, "/api/file?path=..%2Fsecret.nflow", "")
		if traversal.Code != http.StatusBadRequest {
			t.Fatalf("traversal open status=%d body=%s", traversal.Code, traversal.Body.String())
		}
		body, err := json.Marshal(map[string]string{"path": "../secret.nflow", "source": "overwritten"})
		if err != nil {
			t.Fatal(err)
		}
		traversalSave := studioRequest(t, studio, http.MethodPost, "/api/save", string(body))
		actual, err := os.ReadFile(outside)
		if err != nil {
			t.Fatal(err)
		}
		if traversalSave.Code != http.StatusBadRequest || string(actual) != secret {
			t.Fatalf("traversal save status=%d outside source=%q", traversalSave.Code, actual)
		}
	})
}
