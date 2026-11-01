package analyzer_test

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nexssp/ops/analyzer"
)

//go:embed testdata/generated-sdk-typecheck/package.json testdata/generated-sdk-typecheck/package-lock.json
var typecheckManifest embed.FS

func TestGeneratedPublicSDKCompiles(t *testing.T) {
	node := requireTestCommand(t, "node")
	npm := requireTestCommand(t, "npm")

	root := t.TempDir()
	fixtureDir := filepath.Join(root, "fixture")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}

	const fixture = `package sample

import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type CreateRequest struct {
	Name string ` + "`json:\"name\" validate:\"required\"`" + `
}

type CreateResponse struct {
	ID string ` + "`json:\"id\" validate:\"required\"`" + `
}

func Register() action.AnyAction {
	return action.New[CreateRequest, CreateResponse]("sample.create", nil).
		Route(thttp.POST("/api/sample")).
		Build()
}
`
	if err := os.WriteFile(filepath.Join(fixtureDir, "actions.go"), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := analyzer.Analyze(fixtureDir)
	if err != nil {
		t.Fatalf("analyze SDK fixture: %v", err)
	}
	if diagnostics := analyzer.ValidatePublicAPI(report); len(diagnostics) != 0 {
		t.Fatalf("SDK fixture has invalid public API diagnostics: %+v", diagnostics)
	}

	libDir := filepath.Join(root, "generated", "lib")
	files := analyzer.GenerateTypeScriptAPISDK(report)
	if len(files) == 0 {
		t.Fatal("API-only SDK generator returned no files")
	}
	for _, file := range files {
		path := filepath.Join(libDir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create generated SDK directory: %v", err)
		}
		if err := os.WriteFile(path, file.Content, 0o600); err != nil {
			t.Fatalf("write generated SDK file %q: %v", file.Path, err)
		}
	}

	endpointsPath := filepath.Join(libDir, "_gen", "api", "endpoints.ts")
	endpoints, err := os.ReadFile(endpointsPath)
	if err != nil {
		t.Fatalf("read generated endpoints: %v", err)
	}
	if !strings.Contains(string(endpoints), "sampleCreate:") {
		t.Fatalf("generated SDK omitted sample.create endpoint:\n%s", endpoints)
	}

	for _, name := range []string{"package.json", "package-lock.json"} {
		contents, err := typecheckManifest.ReadFile("testdata/generated-sdk-typecheck/" + name)
		if err != nil {
			t.Fatalf("read embedded type-check dependency %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(root, name), contents, 0o600); err != nil {
			t.Fatalf("write temporary npm file %s: %v", name, err)
		}
	}

	tsconfig := `{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "strict": true,
    "noEmit": true,
    "skipLibCheck": true,
    "lib": ["ES2022", "DOM"]
  },
  "include": ["generated/lib/_gen/api/**/*.ts"]
}
`
	configPath := filepath.Join(root, "tsconfig.json")
	if err := os.WriteFile(configPath, []byte(tsconfig), 0o600); err != nil {
		t.Fatalf("write TypeScript compiler config: %v", err)
	}

	installCtx, cancelInstall := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancelInstall()
	install, err := npmCommand(installCtx, npm, node, "ci", "--prefix", root, "--no-audit", "--no-fund", "--ignore-scripts")
	if err != nil {
		t.Fatal(err)
	}
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("install locked TypeScript SDK check dependencies: %v\n%s", err, output)
	}

	tscPath := filepath.Join(root, "node_modules", "typescript", "bin", "tsc")
	compileCtx, cancelCompile := context.WithTimeout(context.Background(), time.Minute)
	defer cancelCompile()
	compile := exec.CommandContext(compileCtx, node, tscPath, "--project", configPath)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile generated public TypeScript SDK: %v\n%s", err, output)
	}
}

func npmCommand(ctx context.Context, npm, node string, args ...string) (*exec.Cmd, error) {
	if runtime.GOOS != "windows" {
		return exec.CommandContext(ctx, npm, args...), nil
	}

	// Windows distributions expose npm through a .cmd shim, which Go cannot
	// execute directly. Invoke npm's bundled JavaScript CLI with node instead.
	for _, dir := range []string{filepath.Dir(npm), filepath.Dir(node)} {
		cli := filepath.Join(dir, "node_modules", "npm", "bin", "npm-cli.js")
		if info, err := os.Stat(cli); err == nil && !info.IsDir() {
			return exec.CommandContext(ctx, node, append([]string{cli}, args...)...), nil
		}
	}
	return nil, fmt.Errorf("could not locate npm-cli.js beside the npm or Node.js executable")
}

func requireTestCommand(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("%s is required for the generated SDK CI check: %v", name, err)
	}
	t.Skipf("%s is not installed; skipping generated SDK TypeScript check", name)
	return ""
}
