// Package observability adapts application-owned HTTP handlers into Kernel
// actions for standard health and metrics endpoints.
//
// The adapters only bind routes; they do not wrap or alter handler responses.
package observability

import (
	"net/http"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

// HealthActions returns the standard liveness and readiness actions. The
// supplied handlers remain responsible for response status, headers, and body.
// Liveness is exposed at GET /healthz; readiness is exposed at GET /readyz and
// the backwards-compatible GET /health alias.
func HealthActions(livenessHandler, readinessHandler http.Handler) []action.AnyAction {
	return []action.AnyAction{
		handlerAction(
			"obs.health",
			livenessHandler,
			[]string{"infra", "observability"},
			route{method: http.MethodGet, path: "/healthz"},
		),
		handlerAction(
			"system.health",
			readinessHandler,
			[]string{"infra", "observability"},
			route{method: http.MethodGet, path: "/readyz"},
			route{method: http.MethodGet, path: "/health"},
		),
	}
}

// MetricsAction returns a system action that serves the supplied handler at one
// GET endpoint. The handler is mounted directly so Prometheus content
// negotiation and exposition output are preserved.
func MetricsAction(name, path string, handler http.Handler, tags ...string) action.AnyAction {
	return handlerAction(name, handler, tags, route{method: http.MethodGet, path: path})
}

type route struct {
	method string
	path   string
}

func handlerAction(name string, handler http.Handler, tags []string, routes ...route) action.AnyAction {
	builder := action.New[any, any](name, nil).System()
	if len(tags) > 0 {
		builder.Tag(tags...)
	}
	for _, route := range routes {
		builder.Route(thttp.RawHandler(route.method, route.path, handler.ServeHTTP))
	}
	return builder.Build()
}
