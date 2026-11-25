package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestParseBackendGenArgs(t *testing.T) {
	opts, err := parseBackendGenArgs([]string{
		"--scan", "../catalog",
		"--out", "app/backend_server_gen.go",
		"--package", "app",
		"--function", "RunCatalogBackend",
		"--selector", "SelectCatalogActions",
		"--route-manifest", "routes.json",
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
	if opts.Output != "app/backend_server_gen.go" || opts.PackageName != "app" || opts.FunctionName != "RunCatalogBackend" || opts.SelectorName != "SelectCatalogActions" || opts.RouteManifest != "routes.json" {
		t.Errorf("parsed output settings = %+v", opts)
	}
	if len(opts.ExcludePipelines) != 1 || opts.ExcludePipelines[0] != "catalog_debug" || len(opts.ExcludeActions) != 1 || opts.ExcludeActions[0] != "catalog.rebuild" || !opts.Force {
		t.Errorf("parsed route settings = %+v", opts)
	}
}

func TestParseBackendGenArgsRejectsMissingValue(t *testing.T) {
	for _, flag := range []string{"--out", "--selector", "--route-manifest"} {
		if _, err := parseBackendGenArgs([]string{flag}); err == nil {
			t.Errorf("expected missing %s value to fail", flag)
		}
	}
}

func TestLoadBackendRouteManifest(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "routes.json")
	data := `{"routes":[{"action":"customer.patch","method":"PATCH","path":"/api/v1/customers/{id}"}]}`
	if err := os.WriteFile(filename, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	actions, err := loadBackendRouteManifest(filename)
	if err != nil {
		t.Fatalf("loadBackendRouteManifest: %v", err)
	}
	if len(actions) != 1 || actions[0].Name != "customer.patch" || actions[0].Method != "PATCH" || actions[0].Path != "/api/v1/customers/{id}" || actions[0].RouteSource != "manifest" {
		t.Fatalf("manifest routes = %+v", actions)
	}
}

func TestLoadBackendRouteManifestRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "missing routes", data: `{}`},
		{name: "unknown field", data: `{"routes":[],"other":true}`},
		{name: "trailing value", data: `{"routes":[]} {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "routes.json")
			if err := os.WriteFile(filename, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadBackendRouteManifest(filename); err == nil {
				t.Fatal("expected invalid manifest to fail")
			}
		})
	}
}

func TestSelectedBackendRouteCount(t *testing.T) {
	actions := []analyzer.ActionContract{
		{Name: "catalog.list", Method: "GET", Path: "/api/items", RouteSource: "go"},
		{Name: "customer.patch", Method: "PATCH", Path: "/admin/customers/{id}", RouteSource: "manifest"},
		{Name: "pipeline.items_list", Method: "GET", Path: "/api/flow-items", RouteSource: "nflow"},
		{Name: "catalog.rebuild"},
	}
	flowRoutes := []analyzer.FlowHTTPRoute{{Pipeline: "items_list", Method: "GET", Path: "/api/flow-items"}}
	if got := selectedBackendRouteCount(actions, flowRoutes, []string{"items_list"}, []string{"catalog.list"}); got != 1 {
		t.Fatalf("selectedBackendRouteCount = %d, want 1 manifest route", got)
	}
	if got := selectedBackendRouteCount(actions, flowRoutes, nil, []string{"catalog.list"}); got != 2 {
		t.Fatalf("selectedBackendRouteCount with one excluded action = %d, want 2", got)
	}
}
