// Package backendgen emits a self-contained HTTP server bootstrap for Flow
// applications. The generated file depends only on Flow, Kernel, and transport;
// the application injects its own compiler bundles at startup.
package backendgen

import (
	"fmt"
	"go/format"
	"go/token"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/nexssp/ops/analyzer"
)

// Config controls the generated Go package and the set of mounted action routes.
type Config struct {
	PackageName      string
	FunctionName     string
	SelectorName     string
	ExcludePipelines []string
	ExcludeActions   []string
}

type generatedRoute struct {
	Action string
	Method string
	Path   string
	Source string
}

// Generate returns formatted Go source for a Flow/Kernel HTTP server bootstrap
// from NFlow pipeline routes. It remains available for callers that already
// have a FlowHTTPRoute manifest.
func Generate(routes []analyzer.FlowHTTPRoute, config Config) ([]byte, error) {
	selected := make([]generatedRoute, 0, len(routes))
	for _, route := range routes {
		selected = append(selected, generatedRoute{
			Action: normalizePipelineName(route.Pipeline),
			Method: route.Method,
			Path:   route.Path,
			Source: "pipeline",
		})
	}
	return generate(selected, config)
}

// GenerateFromActions returns a backend bootstrap for actions with explicitly
// declared HTTP routes. NFlow routes use their compiler resolver names from
// flowRoutes; Go action routes use their action names from the contracts. It
// does not invent paths or action implementations.
func GenerateFromActions(actions []analyzer.ActionContract, flowRoutes []analyzer.FlowHTTPRoute, config Config) ([]byte, error) {
	routes := make([]generatedRoute, 0, len(actions)+len(flowRoutes))
	for _, action := range actions {
		if action.RouteSource == "nflow" {
			continue
		}
		if action.Method == "" && action.Path == "" {
			continue
		}
		if strings.TrimSpace(action.Name) == "" {
			return nil, fmt.Errorf("routed action has an empty name")
		}
		if action.Method == "" || action.Path == "" {
			return nil, fmt.Errorf("action %q has an incomplete HTTP route: both method and path are required", action.Name)
		}
		source := action.RouteSource
		if source == "" {
			source = "action"
		}
		routes = append(routes, generatedRoute{Action: strings.TrimSpace(action.Name), Method: action.Method, Path: action.Path, Source: source})
	}
	for _, route := range flowRoutes {
		routes = append(routes, generatedRoute{
			Action: normalizePipelineName(route.Pipeline),
			Method: route.Method,
			Path:   route.Path,
			Source: "pipeline",
		})
	}
	return generate(routes, config)
}

// generate returns the shared runtime bootstrap for an analyzed action route
// manifest. The generated function accepts application bundles, an optional
// mux configurator (for a UI or other app-owned handlers), and middleware.
func generate(routes []generatedRoute, config Config) ([]byte, error) {
	if config.PackageName == "" {
		config.PackageName = "main"
	}
	if config.FunctionName == "" {
		config.FunctionName = "RunGeneratedBackendServer"
	}
	if config.SelectorName == "" {
		config.SelectorName = "SelectGeneratedBackendActions"
	}
	if !validIdentifier(config.PackageName) {
		return nil, fmt.Errorf("invalid Go package name %q", config.PackageName)
	}
	if !validIdentifier(config.FunctionName) || !unicode.IsUpper([]rune(config.FunctionName)[0]) {
		return nil, fmt.Errorf("function name %q must be an exported Go identifier", config.FunctionName)
	}
	if !validIdentifier(config.SelectorName) || !unicode.IsUpper([]rune(config.SelectorName)[0]) {
		return nil, fmt.Errorf("selector name %q must be an exported Go identifier", config.SelectorName)
	}

	excludedPipelines := make(map[string]bool, len(config.ExcludePipelines))
	for _, name := range config.ExcludePipelines {
		name = normalizePipelineName(name)
		if name == "pipeline." {
			return nil, fmt.Errorf("empty pipeline name in exclusions")
		}
		excludedPipelines[name] = true
	}
	excludedActions := make(map[string]bool, len(config.ExcludeActions))
	for _, name := range config.ExcludeActions {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("empty action name in exclusions")
		}
		excludedActions[name] = true
	}

	selected := make([]generatedRoute, 0, len(routes))
	foundPipelines := make(map[string]bool, len(excludedPipelines))
	foundActions := make(map[string]bool, len(excludedActions))
	seenActions := make(map[string]bool, len(routes))
	seenEndpoints := make(map[string]string, len(routes))
	for _, route := range routes {
		route.Action = strings.TrimSpace(route.Action)
		if route.Source == "pipeline" || route.Source == "nflow" {
			route.Action = normalizePipelineName(route.Action)
		}
		if route.Action == "" || route.Action == "pipeline." {
			return nil, fmt.Errorf("route has an empty action name")
		}
		if route.Source == "pipeline" || route.Source == "nflow" {
			if excludedPipelines[route.Action] {
				foundPipelines[route.Action] = true
				continue
			}
		} else if excludedActions[route.Action] {
			foundActions[route.Action] = true
			continue
		}
		if err := validateRoute(route); err != nil {
			return nil, err
		}
		if seenActions[route.Action] {
			return nil, fmt.Errorf("duplicate HTTP action route %q", route.Action)
		}
		seenActions[route.Action] = true
		endpoint := route.Method + " " + route.Path
		if previous, ok := seenEndpoints[endpoint]; ok {
			return nil, fmt.Errorf("duplicate HTTP route %q declared by %q and %q", endpoint, previous, route.Action)
		}
		seenEndpoints[endpoint] = route.Action
		selected = append(selected, route)
	}
	for name := range excludedPipelines {
		if !foundPipelines[name] {
			return nil, fmt.Errorf("excluded pipeline %q was not found among .nflow HTTP routes", name)
		}
	}
	for name := range excludedActions {
		if !foundActions[name] {
			return nil, fmt.Errorf("excluded action %q was not found among routed actions", name)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no routed actions remain for backend generation")
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Action != selected[j].Action {
			return selected[i].Action < selected[j].Action
		}
		if selected[i].Method != selected[j].Method {
			return selected[i].Method < selected[j].Method
		}
		return selected[i].Path < selected[j].Path
	})

	var source strings.Builder
	fmt.Fprintf(&source, "// Code generated by nexssp OPS. DO NOT EDIT.\npackage %s\n\n", config.PackageName)
	source.WriteString(generatedImports)
	source.WriteString("\n// opsGeneratedBackendRoute is an HTTP route bound to a registered application action.\ntype opsGeneratedBackendRoute struct {\n\tAction string\n\tMethod string\n\tPath string\n}\n\nvar opsGeneratedBackendRoutes = []opsGeneratedBackendRoute{\n")
	for _, route := range selected {
		fmt.Fprintf(&source, "\t{Action: %s, Method: %s, Path: %s},\n",
			strconv.Quote(route.Action), strconv.Quote(route.Method), strconv.Quote(route.Path))
	}
	source.WriteString("}\n")
	runtime := strings.NewReplacer(
		"@@FUNCTION_NAME@@", config.FunctionName,
		"@@SELECTOR_NAME@@", config.SelectorName,
	).Replace(generatedRuntime)
	source.WriteString(runtime)

	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, fmt.Errorf("format generated backend source: %w", err)
	}
	return formatted, nil
}

func validIdentifier(name string) bool {
	return token.IsIdentifier(name) && !token.Lookup(name).IsKeyword()
}

func normalizePipelineName(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "pipeline.") {
		return name
	}
	return "pipeline." + name
}

func validateRoute(route generatedRoute) error {
	switch route.Method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace:
	case "SSE":
		if route.Source == "pipeline" || route.Source == "nflow" {
			return fmt.Errorf("Flow pipeline action %q uses unsupported HTTP method %q; SSE bindings must be supplied as app-owned actions", route.Action, route.Method)
		}
	default:
		return fmt.Errorf("action %q uses unsupported HTTP method %q; generated backends support CONNECT, DELETE, GET, HEAD, OPTIONS, PATCH, POST, PUT, and TRACE", route.Action, route.Method)
	}
	if !validRoutePath(route.Path) {
		return fmt.Errorf("action %q has unsafe route path %q; paths must be canonical absolute paths without query, fragment, backslash, or whitespace", route.Action, route.Path)
	}
	return nil
}

func validRoutePath(routePath string) bool {
	if routePath == "" || !strings.HasPrefix(routePath, "/") || strings.HasPrefix(routePath, "//") {
		return false
	}
	canonical := path.Clean(routePath) == routePath
	if !canonical && strings.HasSuffix(routePath, "/") {
		trimmed := strings.TrimSuffix(routePath, "/")
		canonical = trimmed != "" && path.Clean(trimmed) == trimmed
	}
	if !canonical || strings.ContainsAny(routePath, "?#\\") || strings.IndexFunc(routePath, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) >= 0 {
		return false
	}
	return true
}

const generatedImports = `import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/nexssp/flow/compiler"
	"github.com/nexssp/flow/extensions/require"
	"github.com/nexssp/flow/native"
	"github.com/nexssp/flow/runner"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
`

const generatedRuntime = `
// @@SELECTOR_NAME@@ selects app-owned actions with the generated HTTP routes.
// Use this with an app's own transport when it already owns server lifecycle.
func @@SELECTOR_NAME@@(appActions []action.AnyAction) ([]action.AnyAction, error) {
	byName := make(map[string]action.AnyAction, len(appActions))
	for _, appAction := range appActions {
		if appAction == nil || appAction.Describe() == nil {
			continue
		}
		name := appAction.Describe().Name
		if _, exists := byName[name]; exists {
			return nil, fmt.Errorf("duplicate registered application action %q", name)
		}
		byName[name] = appAction
	}

	selected := make([]action.AnyAction, 0, len(opsGeneratedBackendRoutes))
	for _, route := range opsGeneratedBackendRoutes {
		if strings.HasPrefix(route.Action, "pipeline.") {
			continue
		}
		appAction, ok := byName[route.Action]
		if !ok {
			return nil, fmt.Errorf("generated backend action %q was not registered by the application", route.Action)
		}
		if !opsGeneratedBackendActionHasRoute(appAction, route.Method, route.Path) {
			return nil, fmt.Errorf("registered action %q no longer declares generated route %s %s; regenerate the backend", route.Action, route.Method, route.Path)
		}
		selected = append(selected, appAction)
	}
	return selected, nil
}

func opsGeneratedBackendActionHasRoute(appAction action.AnyAction, method, routePath string) bool {
	for _, binding := range appAction.GetBindings() {
		switch route := binding.(type) {
		case thttp.HTTPRoute:
			if route.Method == method && route.Path == routePath {
				return true
			}
			case thttp.RawHTTPHandler:
				if route.Method == method && route.Path == routePath {
					return true
				}
			case thttp.SSERoute:
				if method == "SSE" && route.Path == routePath {
					return true
				}
			}
	}
	return false
}

// @@FUNCTION_NAME@@ compiles workflowPath once and mounts only the generated
// HTTP routes for analyzed actions. appBundles supplies application actions/extensions;
// configureMux can attach a UI handler, and middleware follows net/http shape.
// Binding is intentionally loopback-only by default to avoid accidental public
// exposure. An app-provided bundle that already owns :route is reused.
func @@FUNCTION_NAME@@(ctx context.Context, workflowPath, addr string, appBundles []compiler.Bundle, configureMux func(*http.ServeMux), middleware ...func(http.Handler) http.Handler) error {
	if !opsGeneratedBackendIsLoopback(addr) {
		return fmt.Errorf("unsafe HTTP bind address %q: generated backend accepts loopback addresses only", addr)
	}
	source, err := os.ReadFile(workflowPath)
	if err != nil {
		return fmt.Errorf("read backend workflow %q: %w", workflowPath, err)
	}

	host := runner.NewHost()
	err = host.Run(ctx, func(runCtx context.Context) error {
		nativeBundles := native.Bundles()
		if err := host.OwnAll(nativeBundles); err != nil {
			return fmt.Errorf("own native Flow bundles: %w", err)
		}

		registeredAppBundles := append([]compiler.Bundle(nil), appBundles...)
		if !opsGeneratedBackendHasRouteModifier(registeredAppBundles) {
			registeredAppBundles = append(registeredAppBundles, opsGeneratedBackendRouteBundle())
		}
		if err := host.OwnAll(registeredAppBundles); err != nil {
			return fmt.Errorf("own application Flow bundles: %w", err)
		}

		directives := make([]compiler.Directive, 0)
		for i := range nativeBundles {
			directives = append(directives, nativeBundles[i].Directives...)
		}
		_, meta, err := compiler.Preprocess(runCtx, compiler.NewDirectiveTable(directives...), string(source), workflowPath)
		if err != nil {
			return fmt.Errorf("preprocess backend workflow: %w", err)
		}

		requirements, err := require.FromMeta(meta)
		if err != nil {
			return fmt.Errorf("derive backend requirements: %w", err)
		}
		var ownershipErr error
		requiredBundles, err := require.ResolveBundlesWith(requirements, func(bundle compiler.Bundle) {
			if ownershipErr == nil {
				ownershipErr = host.Own(bundle)
			}
		})
		if err != nil {
			return fmt.Errorf("resolve backend workflow bundles: %w", err)
		}
		if ownershipErr != nil {
			return fmt.Errorf("own required backend bundle: %w", ownershipErr)
		}

		bundles := make([]compiler.Bundle, 0, len(nativeBundles)+len(registeredAppBundles)+len(requiredBundles))
		seen := make(map[string]bool, cap(bundles))
		appendUnique := func(group []compiler.Bundle) {
			for _, bundle := range group {
				if seen[bundle.ID] {
					continue
				}
				seen[bundle.ID] = true
				bundles = append(bundles, bundle)
			}
		}
		appendUnique(nativeBundles)
		appendUnique(registeredAppBundles)
		appendUnique(requiredBundles)

		cfg, err := runner.BuildConfig(bundles)
		if err != nil {
			return fmt.Errorf("build backend Flow config: %w", err)
		}
		compiled, err := runner.Compile(runCtx, cfg, string(source), workflowPath)
		if err != nil {
			return fmt.Errorf("compile backend Flow: %w", err)
		}

			endpoints := make([]action.AnyAction, 0, len(opsGeneratedBackendRoutes))
			for _, route := range opsGeneratedBackendRoutes {
				endpoint, ok := compiled.Resolver.Action(route.Action)
				if !ok {
					return fmt.Errorf("backend endpoint action %q was not materialized", route.Action)
				}
				if len(endpoint.GetBindings()) == 0 {
					return fmt.Errorf("backend endpoint action %q has no Kernel route binding", route.Action)
			}
			endpoints = append(endpoints, endpoint)
		}

		httpTransport := thttp.New(addr,
			thttp.WithReadTimeout(10*time.Second),
			thttp.WithWriteTimeout(130*time.Second),
		)
		httpTransport.Mount(endpoints)
		if configureMux != nil {
			configureMux(httpTransport.Mux())
		}
		httpTransport.Use(middleware...)
		_, runErr := httpTransport.Do(runCtx, nil)
		return runErr
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func opsGeneratedBackendRouteBundle() compiler.Bundle {
	return compiler.Bundle{
		ID: "nexssp-generated-http-routes",
		Modifiers: []compiler.Modifier{{
			Name: "route",
			Owner: compiler.OwnerBundle,
			ValueKind: compiler.ModifierKindString,
			Unique: true,
			Example: ` + "`" + `:route="GET /api/health"` + "`" + `,
			Apply: opsGeneratedBackendApplyHTTPRoute,
		}},
	}
}

func opsGeneratedBackendHasRouteModifier(bundles []compiler.Bundle) bool {
	for _, bundle := range bundles {
		for _, modifier := range bundle.Modifiers {
			if compiler.ModifierName(modifier.Name) == "route" {
				return true
			}
		}
	}
	return false
}

func opsGeneratedBackendApplyHTTPRoute(builder *action.Builder[any, any], raw string) error {
	if builder == nil {
		return fmt.Errorf("route modifier requires an action builder")
	}
	parts := strings.Fields(raw)
	if len(parts) != 2 {
		return fmt.Errorf("expected METHOD /api/path")
	}
	method, routePath := strings.ToUpper(parts[0]), parts[1]
	if !opsGeneratedBackendSafeRoutePath(routePath) {
		return fmt.Errorf("route must be a canonical absolute path without query, fragment, backslash, or whitespace")
	}
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace:
		builder.Route(thttp.HTTPRoute{Method: method, Path: routePath})
	default:
		return fmt.Errorf("method %q is not a supported standard HTTP method", method)
	}
	return nil
}

func opsGeneratedBackendSafeRoutePath(routePath string) bool {
	if routePath == "" || !strings.HasPrefix(routePath, "/") || strings.HasPrefix(routePath, "//") {
		return false
	}
	canonical := path.Clean(routePath) == routePath
	if !canonical && strings.HasSuffix(routePath, "/") {
		trimmed := strings.TrimSuffix(routePath, "/")
		canonical = trimmed != "" && path.Clean(trimmed) == trimmed
	}
	return canonical && !strings.ContainsAny(routePath, "?#\\") && strings.IndexFunc(routePath, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) < 0
}

func opsGeneratedBackendIsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
`
