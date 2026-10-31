package analyzer_test

import (
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestActions_SSEBinding(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "webconsole/service.go", `package webconsole
import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
type Service struct{ b *thttp.InMemoryBroadcaster }
func (s *Service) StreamAction() action.AnyAction {
	return action.New[any, any]("console.stream", nil).
		Route(thttp.Stream("/api/events").WithChannel("events").WithBroadcaster(s.b)).
		Public().
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	var found *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "console.stream" {
			found = &report.Actions[i]
		}
	}
	if found == nil {
		t.Fatalf("console.stream not extracted; got %+v", report.Actions)
	}
	if found.Method != "SSE" {
		t.Errorf("method = %q, want SSE", found.Method)
	}
	if found.Path != "/api/events" {
		t.Errorf("path = %q, want /api/events", found.Path)
	}
	if !found.Streaming {
		t.Error("Streaming = false, want true")
	}
}

func TestActions_RawHandlerBinding(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "ws/service.go", `package ws
import (
	"net/http"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
func Files() action.AnyAction {
	h := func(w http.ResponseWriter, r *http.Request) {}
	return action.New[struct{}, struct{}]("workspace.view", nil).
		Route(thttp.RawHandler(http.MethodGet, "/api/workspace/view", h)).
		Public().
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	var found *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "workspace.view" {
			found = &report.Actions[i]
		}
	}
	if found == nil {
		t.Fatalf("workspace.view not extracted")
	}
	if found.Method != "GET" {
		t.Errorf("method = %q, want GET", found.Method)
	}
	if found.Path != "/api/workspace/view" {
		t.Errorf("path = %q", found.Path)
	}
	if !found.Raw {
		t.Error("Raw = false, want true")
	}
}

func TestActions_AllHTTPVerbs(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "crud.go", `package crud
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
type Req struct{}
type Res struct{}
func mk(name string, fn func(context.Context, Req) (Res, error)) action.AnyAction {
	return action.New(name, fn).Build()
}
func All() []action.AnyAction {
	return []action.AnyAction{
		action.New[Req, Res]("crud.get", nil).Route(thttp.GET("/api/x")).Build(),
		action.New[Req, Res]("crud.post", nil).Route(thttp.POST("/api/x")).Build(),
		action.New[Req, Res]("crud.put", nil).Route(thttp.PUT("/api/x")).Build(),
		action.New[Req, Res]("crud.del", nil).Route(thttp.DELETE("/api/x")).Build(),
		action.New[Req, Res]("crud.patch", nil).Route(thttp.PATCH("/api/x")).Build(),
	}
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"crud.get":   "GET",
		"crud.post":  "POST",
		"crud.put":   "PUT",
		"crud.del":   "DELETE",
		"crud.patch": "PATCH",
	}
	got := map[string]string{}
	for _, a := range report.Actions {
		got[a.Name] = a.Method
	}
	for name, method := range want {
		if got[name] != method {
			t.Errorf("%s: method = %q, want %q", name, got[name], method)
		}
	}
}
