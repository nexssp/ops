package main

import "testing"

func TestParseBackendGenArgs(t *testing.T) {
	opts, err := parseBackendGenArgs([]string{
		"--scan", "../catalog",
		"--out", "app/backend_server_gen.go",
		"--package", "app",
		"--function", "RunCatalogBackend",
		"--exclude-pipeline", "catalog_debug",
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
	if len(opts.ExcludePipelines) != 1 || opts.ExcludePipelines[0] != "catalog_debug" || !opts.Force {
		t.Errorf("parsed route settings = %+v", opts)
	}
}

func TestParseBackendGenArgsRejectsMissingValue(t *testing.T) {
	if _, err := parseBackendGenArgs([]string{"--out"}); err == nil {
		t.Fatal("expected missing --out value to fail")
	}
}
