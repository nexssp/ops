package main

import (
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestParseBackendGenArgs(t *testing.T) {
	opts, err := parseBackendGenArgs([]string{
		"--scan", "../catalog",
		"--out", "app/backend_server_gen.go",
		"--package", "app",
		"--function", "RunCatalogBackend",
		"--exclude-pipeline", "catalog_debug",
		"--exclude-action", "catalog.rebuild",
		"--force",
	})
	if err != nil {
		t.Fatalf("parseBackendGenArgs: %v", err)
	}
	if len(opts.Roots) != 1 || opts.Roots[0] != "../catalog" {
		t.Errorf("Roots = %v, want [../catalog]", opts.Roots)
	}
	if opts.Output != "app/backend_server_gen.go" || opts.PackageName != "app" || opts.FunctionName != "RunCatalogBackend" {
		t.Errorf("parsed output settings = %+v", opts)
	}
	if len(opts.ExcludePipelines) != 1 || opts.ExcludePipelines[0] != "catalog_debug" || len(opts.ExcludeActions) != 1 || opts.ExcludeActions[0] != "catalog.rebuild" || !opts.Force {
		t.Errorf("parsed route settings = %+v", opts)
	}
}

func TestParseBackendGenArgsRejectsMissingValue(t *testing.T) {
	if _, err := parseBackendGenArgs([]string{"--out"}); err == nil {
		t.Fatal("expected missing --out value to fail")
	}
}

func TestSelectedBackendRouteCount(t *testing.T) {
	actions := []analyzer.ActionContract{
		{Name: "catalog.list", Method: "GET", Path: "/api/items", RouteSource: "go"},
		{Name: "pipeline.items_list", Method: "GET", Path: "/api/flow-items", RouteSource: "nflow"},
		{Name: "catalog.rebuild"},
	}
	flowRoutes := []analyzer.FlowHTTPRoute{{Pipeline: "items_list", Method: "GET", Path: "/api/flow-items"}}
	if got := selectedBackendRouteCount(actions, flowRoutes, []string{"items_list"}, []string{"catalog.list"}); got != 0 {
		t.Fatalf("selectedBackendRouteCount = %d, want 0", got)
	}
	if got := selectedBackendRouteCount(actions, flowRoutes, nil, []string{"catalog.list"}); got != 1 {
		t.Fatalf("selectedBackendRouteCount with one excluded action = %d, want 1", got)
	}
}
