package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSDKContractManifestPreservesExplicitEmptyType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contracts.json")
	if err := os.WriteFile(path, []byte(`{"actions":[{"name":"records.create","req_type":"struct{}"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadSDKContractManifest(path)
	if err != nil {
		t.Fatalf("loadSDKContractManifest() error = %v", err)
	}
	if len(manifest.Actions) != 1 || manifest.Actions[0].ReqType == nil || *manifest.Actions[0].ReqType != "struct{}" {
		t.Fatalf("explicit empty contract was not preserved: %#v", manifest)
	}
	if manifest.Actions[0].ResType != nil {
		t.Fatalf("omitted response type should remain absent: %#v", manifest.Actions[0])
	}
}

func TestLoadSDKContractManifestRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	for _, content := range []string{
		`{"actions":[{"name":"records.create","request":"struct{}"}]}`,
		`{"actions":[]} {"actions":[]}`,
	} {
		path := filepath.Join(t.TempDir(), "contracts.json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSDKContractManifest(path); err == nil {
			t.Fatalf("loadSDKContractManifest(%q) unexpectedly succeeded", content)
		}
	}
}

func TestSDKAPIArgsPreserveManifestAndFacadeValues(t *testing.T) {
	outputDir, scanArgs := parseSDKAPIArgs([]string{
		"--contract-manifest", "api-contracts.json",
		"--api-facade", "src/lib/api.generated.ts",
		"src/lib/_gen/api",
		"--scan", "actions",
	})
	if outputDir != "src/lib/_gen/api" {
		t.Fatalf("output directory = %q", outputDir)
	}
	options := parseScanArgs(scanArgs, true)
	if options.ContractManifest != "api-contracts.json" || options.APIFacade != "src/lib/api.generated.ts" {
		t.Fatalf("manifest/facade options were lost: %#v", options)
	}
	if len(options.Roots) != 1 || options.Roots[0] != "actions" {
		t.Fatalf("scan roots = %#v", options.Roots)
	}
}

func TestWriteAPICompatibilityFacadeUsesRelativeESMImports(t *testing.T) {
	root := t.TempDir()
	outputDir := filepath.Join(root, "src", "lib", "_gen", "api")
	facadePath := filepath.Join(root, "src", "lib", "api.generated.ts")
	if err := writeAPICompatibilityFacade(facadePath, outputDir); err != nil {
		t.Fatalf("writeAPICompatibilityFacade() error = %v", err)
	}
	content, err := os.ReadFile(facadePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, expected := range []string{
		`export * from "./_gen/api/index.js";`,
		`export { api as generatedApi } from "./_gen/api/endpoints.js";`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("facade missing %q:\n%s", expected, text)
		}
	}
}
