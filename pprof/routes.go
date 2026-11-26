// Package pprof exposes Go runtime profiling endpoints as Kernel actions.
package pprof

import (
	"net/http"
	stdpprof "net/http/pprof"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

// Actions returns named system actions for the standard Go runtime profiling
// endpoints. actionNamePrefix is prepended to each profile name; when it is
// empty, the prefix defaults to "ops.pprof". The supplied standard-library
// handlers are mounted directly, without changing their responses.
func Actions(actionNamePrefix string) []action.AnyAction {
	if actionNamePrefix == "" {
		actionNamePrefix = "ops.pprof"
	}

	makeAction := func(name, path string, handler http.HandlerFunc) action.AnyAction {
		return action.New[any, any](actionNamePrefix+"."+name, nil).
			System().
			Description("Go runtime profiling: "+name).
			Tag("infra", "debug").
			Route(thttp.RawHandler(http.MethodGet, path, handler)).
			Build()
	}

	return []action.AnyAction{
		makeAction("index", "/debug/pprof/", stdpprof.Index),
		makeAction("cmdline", "/debug/pprof/cmdline", stdpprof.Cmdline),
		makeAction("profile", "/debug/pprof/profile", stdpprof.Profile),
		makeAction("symbol", "/debug/pprof/symbol", stdpprof.Symbol),
		makeAction("trace", "/debug/pprof/trace", stdpprof.Trace),
		makeAction("goroutine", "/debug/pprof/goroutine", stdpprof.Handler("goroutine").ServeHTTP),
		makeAction("heap", "/debug/pprof/heap", stdpprof.Handler("heap").ServeHTTP),
		makeAction("threadcreate", "/debug/pprof/threadcreate", stdpprof.Handler("threadcreate").ServeHTTP),
		makeAction("block", "/debug/pprof/block", stdpprof.Handler("block").ServeHTTP),
		makeAction("mutex", "/debug/pprof/mutex", stdpprof.Handler("mutex").ServeHTTP),
		makeAction("allocs", "/debug/pprof/allocs", stdpprof.Handler("allocs").ServeHTTP),
	}
}
