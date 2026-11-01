package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
	"github.com/nexssp/ops/deploygen"
)

func TestScaffoldServiceNameUsesProjectDirectory(t *testing.T) {
	tests := map[string]string{
		"/work/Inventory API": "inventory-api",
		"/work/project_01":    "project-01",
		"/work/___":           "app",
	}
	for input, want := range tests {
		if got := scaffoldServiceName(input); got != want {
			t.Errorf("scaffoldServiceName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFreshGenericScaffoldUsesSourceSDKWithoutAuthPreset(t *testing.T) {
	root := t.TempDir()
	_, err := deploygen.ScaffoldSvelteKitFromTemplates(context.Background(), root, deploygen.UIConfig{
		ServiceName: "generic-sample",
		HTTPPort:    "8080",
		ObsPort:     "9090",
	}, true)
	if err != nil {
		t.Fatalf("scaffold generic UI: %v", err)
	}

	page, err := os.ReadFile(filepath.Join(root, "ui", "src", "routes", "+page.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "from '#lib/api.js'") || strings.Contains(string(page), "$lib/api") {
		t.Fatalf("fresh page must use the SvelteKit 3 #lib package import:\n%s", page)
	}

	viteConfig, err := os.ReadFile(filepath.Join(root, "ui", "vite.config.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(viteConfig), "/auth") {
		t.Fatalf("generic Vite proxy must not assume an auth route:\n%s", viteConfig)
	}
	for _, relative := range []string{
		filepath.Join("ui", "svelte.config.js"),
		filepath.Join("ui", "src", "routes", "login", "+page.svelte"),
		filepath.Join("ui", "src", "lib", "client.ts"),
		filepath.Join("ui", "src", "lib", "api.ts"),
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); !os.IsNotExist(err) {
			t.Errorf("generic scaffold unexpectedly wrote %s (stat error: %v)", relative, err)
		}
	}

	var client, apiBarrel string
	files := analyzer.GenerateTypeScriptSDK(&analyzer.Report{Structs: map[string]analyzer.StructDefinition{}})
	for _, file := range files {
		switch file.Path {
		case "_gen/api/client.ts":
			client = string(file.Content)
		case "api.ts":
			apiBarrel = string(file.Content)
		}
	}
	if client == "" || apiBarrel == "" {
		t.Fatal("source-derived SDK did not emit its client and app API barrel")
	}
	for _, forbidden := range []string{"localStorage", "access_token", "refresh_token", "/auth/refresh", "credentials: \"include\""} {
		if strings.Contains(client, forbidden) {
			t.Errorf("generated client contains authentication preset %q", forbidden)
		}
	}
	for _, expected := range []string{"headers?: HeadersInit", "credentials?: RequestCredentials", "export function setFetcher(fn: Fetcher)"} {
		if !strings.Contains(client, expected) {
			t.Errorf("generated client is missing caller-configurable request behavior %q", expected)
		}
	}
	if !strings.Contains(apiBarrel, "./_gen/api/index.js") || !strings.Contains(apiBarrel, "./custom/api.js") {
		t.Fatalf("generated API barrel does not merge the source SDK with custom extensions:\n%s", apiBarrel)
	}
}
