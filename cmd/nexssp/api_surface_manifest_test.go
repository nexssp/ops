package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const apiSurfaceFixtureEnv = "NEXSSP_API_SURFACE_FIXTURE"

func TestLoadSDKAPISurfaceManifestAndRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sdk-api.json")
	content := `{"actions":[{"name":"records.list","sdk_method_name":"recordsList","method":"GET","path":"/records","scope":"public","req_type":"struct{}","res_type":"[]string"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadSDKAPISurfaceManifest(path)
	if err != nil {
		t.Fatalf("loadSDKAPISurfaceManifest() error = %v", err)
	}
	if len(manifest.Actions) != 1 || manifest.Actions[0].SDKMethodName != "recordsList" || manifest.Actions[0].Path != "/records" {
		t.Fatalf("loaded API surface = %#v", manifest)
	}

	for _, invalid := range []string{
		`{"actions":[{"name":"records.list","method":"GET","path":"/records","unknown":true}]}`,
		`{"actions":[]} {"actions":[]}`,
	} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadSDKAPISurfaceManifest(path); err == nil {
			t.Fatalf("loadSDKAPISurfaceManifest(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestSDKAPIArgsPreserveAPISurfaceManifest(t *testing.T) {
	outputDir, scanArgs := parseSDKAPIArgs([]string{
		"--api-manifest", "sdk-api.json",
		"--scan", "actions",
		"api-out",
	})
	if outputDir != "api-out" {
		t.Fatalf("output directory = %q, want api-out", outputDir)
	}
	options := parseScanArgs(scanArgs, true)
	if options.APISurfaceManifest != "sdk-api.json" || len(options.Roots) != 1 || options.Roots[0] != "actions" {
		t.Fatalf("API manifest/source options were lost: %#v", options)
	}
}

func TestSDKAPIManifestRelatedFlagsRequireValues(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		option string
	}{
		{name: "api manifest missing", args: []string{"--api-manifest"}, option: "--api-manifest"},
		{name: "api manifest before another option", args: []string{"--api-manifest", "--quiet"}, option: "--api-manifest"},
		{name: "empty api manifest assignment", args: []string{"--api-manifest="}, option: "--api-manifest"},
		{name: "contract manifest missing", args: []string{"--contract-manifest"}, option: "--contract-manifest"},
		{name: "facade missing", args: []string{"--api-facade"}, option: "--api-facade"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, scanArgs := parseSDKAPIArgs(tc.args)
			options := parseScanArgs(scanArgs, true)
			if !strings.Contains(options.ParseError, "missing value for "+tc.option) {
				t.Fatalf("ParseError = %q, want missing value for %s", options.ParseError, tc.option)
			}
			if tc.name == "api manifest before another option" && !options.Quiet {
				t.Fatal("following --quiet option was consumed as the manifest path")
			}
		})
	}
}

func TestSDKAPISurfaceManifestSelectsPublicRoutesBeforeGeneration(t *testing.T) {
	if root := os.Getenv(apiSurfaceFixtureEnv); root != "" {
		if err := os.Chdir(root); err != nil {
			t.Fatal(err)
		}
		handleSDKGen([]string{"api", filepath.Join(root, "out", "api"), "--scan", "src", "--api-manifest", "sdk-api.json", "--api-facade", filepath.Join(root, "api.generated.ts"), "--quiet"})
		return
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	source := `package metrics
import (
    "github.com/nexssp/kernel/action"
    "github.com/nexssp/transport/thttp"
)
type Request struct{}
type Response struct { Value string ` + "`json:\"value\"`" + ` }
func Prometheus() action.AnyAction {
    return action.New[Request, Response]("metrics.prometheus", nil).Route(thttp.GET("/obs/metrics")).Build()
}
func System() action.AnyAction {
    return action.New[Request, Response]("metrics.system", nil).Route(thttp.GET("/obs/metrics")).Build()
}
`
	if err := os.WriteFile(filepath.Join(root, "src", "actions.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"actions":[
  {"name":"metrics.prometheus","sdk_method_name":"scrapeMetrics","description":"Prometheus metrics","method":"GET","path":"/obs/metrics","scope":"public","req_type":"struct{}","res_type":"Response","node":"observability","success_status":200},
  {"name":"catalog.dynamic_list","sdk_method_name":"catalogList","description":"List dynamic catalog rows","method":"GET","path":"/api/catalog","scope":"public","req_type":"struct{}","res_type":"[]Response"}
]}`
	if err := os.WriteFile(filepath.Join(root, "sdk-api.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSDKAPISurfaceManifestSelectsPublicRoutesBeforeGeneration$")
	cmd.Env = append(os.Environ(), apiSurfaceFixtureEnv+"="+root)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sdk api manifest fixture failed: %v\n%s", err, output)
	}
	endpointsPath := filepath.Join(root, "out", "api", "endpoints.ts")
	endpoints, err := os.ReadFile(endpointsPath)
	if err != nil {
		t.Fatalf("read generated endpoints: %v", err)
	}
	text := string(endpoints)
	for _, want := range []string{"scrapeMetrics: {", `"path":"/obs/metrics"`, `"node":"observability"`, "catalogList: {", `"path":"/api/catalog"`} {
		if !strings.Contains(text, want) {
			t.Errorf("generated surface is missing %q:\n%s", want, text)
		}
	}
	facade, err := os.ReadFile(filepath.Join(root, "api.generated.ts"))
	if err != nil {
		t.Fatalf("read generated compatibility facade: %v", err)
	}
	for _, want := range []string{
		`export * from "./out/api/index.js";`,
		`export { api as generatedApi } from "./out/api/endpoints.js";`,
	} {
		if !strings.Contains(string(facade), want) {
			t.Errorf("generated compatibility facade is missing %q:\n%s", want, facade)
		}
	}
	if strings.Contains(text, "metricsSystem") || strings.Contains(text, "adminBackup") {
		t.Errorf("unselected server routes leaked into SDK output:\n%s", text)
	}
}
