package pprof_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"

	"github.com/nexssp/ops/pprof"
)

func TestActionsExposeNamedGETRoutes(t *testing.T) {
	wantRoutes := map[string]string{
		"plugin.pprof.index":        "/debug/pprof/",
		"plugin.pprof.cmdline":      "/debug/pprof/cmdline",
		"plugin.pprof.profile":      "/debug/pprof/profile",
		"plugin.pprof.symbol":       "/debug/pprof/symbol",
		"plugin.pprof.trace":        "/debug/pprof/trace",
		"plugin.pprof.goroutine":    "/debug/pprof/goroutine",
		"plugin.pprof.heap":         "/debug/pprof/heap",
		"plugin.pprof.threadcreate": "/debug/pprof/threadcreate",
		"plugin.pprof.block":        "/debug/pprof/block",
		"plugin.pprof.mutex":        "/debug/pprof/mutex",
		"plugin.pprof.allocs":       "/debug/pprof/allocs",
	}

	actions := pprof.Actions("plugin.pprof")
	if len(actions) != len(wantRoutes) {
		t.Fatalf("Actions() returned %d actions, want %d", len(actions), len(wantRoutes))
	}
	for _, act := range actions {
		meta := act.Describe()
		if meta == nil {
			t.Fatal("pprof action has nil metadata")
		}
		wantPath, ok := wantRoutes[meta.Name]
		if !ok {
			t.Errorf("unexpected pprof action %q", meta.Name)
			continue
		}
		delete(wantRoutes, meta.Name)
		if meta.Scope != action.ScopeSystem {
			t.Errorf("%s scope = %q, want system", meta.Name, meta.Scope)
		}
		if !reflect.DeepEqual(meta.Tags, []string{"infra", "debug"}) {
			t.Errorf("%s tags = %v, want [infra debug]", meta.Name, meta.Tags)
		}
		profileName := strings.TrimPrefix(meta.Name, "plugin.pprof.")
		if got, want := meta.Description, "Go runtime profiling: "+profileName; got != want {
			t.Errorf("%s description = %q, want %q", meta.Name, got, want)
		}
		bindings := act.GetBindings()
		if len(bindings) != 1 {
			t.Errorf("%s has %d bindings, want one", meta.Name, len(bindings))
			continue
		}
		route, ok := bindings[0].(thttp.RawHTTPHandler)
		if !ok {
			t.Errorf("%s binding has type %T, want thttp.RawHTTPHandler", meta.Name, bindings[0])
			continue
		}
		if route.Method != http.MethodGet || route.Path != wantPath {
			t.Errorf("%s route = %s %s, want GET %s", meta.Name, route.Method, route.Path, wantPath)
		}
	}
	for name, path := range wantRoutes {
		t.Errorf("missing pprof action %s for GET %s", name, path)
	}
}

func TestActionsDefaultNamePrefix(t *testing.T) {
	actions := pprof.Actions("")
	if len(actions) != 11 {
		t.Fatalf("Actions(\"\") returned %d actions, want 11", len(actions))
	}
	for _, act := range actions {
		if name := act.Describe().Name; !strings.HasPrefix(name, "ops.pprof.") {
			t.Errorf("default action name = %q, want ops.pprof prefix", name)
		}
	}
}

func TestFastProfileEndpointsServeInProcess(t *testing.T) {
	transport := thttp.New(":0")
	transport.Mount(pprof.Actions("plugin.pprof"))
	handler := transport.Handler()

	for _, path := range []string{
		"/debug/pprof/",
		"/debug/pprof/goroutine",
		"/debug/pprof/heap",
		"/debug/pprof/allocs",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d; body=%s", path, response.Code, http.StatusOK, response.Body.String())
			}
			if response.Body.Len() == 0 {
				t.Errorf("GET %s returned an empty profile response", path)
			}
			if got := response.Header().Get("Content-Type"); got == "" {
				t.Errorf("GET %s did not set Content-Type", path)
			}
			if path == "/debug/pprof/" {
				for _, profile := range []string{"goroutine", "heap", "allocs"} {
					if !strings.Contains(response.Body.String(), profile) {
						t.Errorf("pprof index does not list %q", profile)
					}
				}
			}
		})
	}
}
