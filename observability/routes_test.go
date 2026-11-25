package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

func TestHealthActionsMountStandardRoutesWithoutChangingHandlers(t *testing.T) {
	liveness := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("live:" + r.URL.Path))
	})
	readiness := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write([]byte("ready:" + r.URL.Path))
	})

	actions := HealthActions(liveness, readiness)
	if len(actions) != 2 {
		t.Fatalf("HealthActions() returned %d actions, want 2", len(actions))
	}
	if got := actions[0].Describe().Name; got != "obs.health" {
		t.Errorf("liveness action name = %q, want obs.health", got)
	}
	if got := actions[1].Describe().Name; got != "system.health" {
		t.Errorf("readiness action name = %q, want system.health", got)
	}

	handler := mountActions(actions)
	for _, test := range []struct {
		path       string
		wantStatus int
		wantType   string
		wantBody   string
	}{
		{path: "/healthz", wantStatus: http.StatusAccepted, wantType: "application/problem+json", wantBody: "live:/healthz"},
		{path: "/readyz", wantStatus: http.StatusMultiStatus, wantType: "application/json; charset=utf-8", wantBody: "ready:/readyz"},
		{path: "/health", wantStatus: http.StatusMultiStatus, wantType: "application/json; charset=utf-8", wantBody: "ready:/health"},
	} {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if got := response.Header().Get("Content-Type"); got != test.wantType {
				t.Errorf("Content-Type = %q, want %q", got, test.wantType)
			}
			if got := response.Body.String(); got != test.wantBody {
				t.Errorf("body = %q, want %q", got, test.wantBody)
			}
		})
	}
}

func TestMetricsActionPreservesPrometheusHandlerOutput(t *testing.T) {
	metrics := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte("sample_metric 1\n"))
	})

	metricsAction := MetricsAction("obs.prometheus", "/obs/metrics", metrics, "observability")
	if got := metricsAction.Describe().Name; got != "obs.prometheus" {
		t.Fatalf("metrics action name = %q, want obs.prometheus", got)
	}

	response := httptest.NewRecorder()
	mountActions([]action.AnyAction{metricsAction}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/obs/metrics", nil))
	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got, want := response.Header().Get("Content-Type"), "text/plain; version=0.0.4; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "sample_metric 1\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func mountActions(actions []action.AnyAction) http.Handler {
	transport := thttp.New("")
	transport.Mount(actions)
	return transport.Handler()
}
