package analyzer_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer/core"
)

func TestCorePackageDoesNotDependOnFlow(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not return the test source path")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), ".."))
	command := exec.Command("go", "list", "-deps", "./analyzer/core")
	command.Dir = moduleRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list analyzer/core dependencies: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "github.com/nexssp/flow") {
		t.Fatalf("analyzer/core unexpectedly depends on Flow:\n%s", output)
	}
}

func TestAnalyzeGoAllIgnoresFlowInputs(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/actions.go", `package api

import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type CreateReq struct {
	Name string `+"`json:\"name\" validate:\"required\"`"+`
}

type CreateRes struct {
	ID string `+"`json:\"id\"`"+`
}

const legacyDSL = `+"`users.legacy:route=\"GET /legacy\"`"+`

func Register() action.AnyAction {
	return action.New[CreateReq, CreateRes]("users.create", nil).
		Route(thttp.POST("/api/users")).
		Public().
		Build()
}
`)
	writeFlow(t, root, "routes.nflow", `@pipeline flow_only :route="GET /from-flow"
	runtime.noop
@end
`)

	report, err := core.AnalyzeGoAll(root)
	if err != nil {
		t.Fatalf("AnalyzeGoAll: %v", err)
	}
	if len(report.Actions) != 1 || report.Actions[0].Name != "users.create" {
		t.Fatalf("Go-only actions = %+v, want only users.create", report.Actions)
	}
	if report.Actions[0].Method != "POST" || report.Actions[0].Path != "/api/users" {
		t.Fatalf("Go action route = %s %s, want POST /api/users", report.Actions[0].Method, report.Actions[0].Path)
	}
	if len(report.FlowHTTPRoutes) != 0 || len(report.FlowSchemas) != 0 {
		t.Fatalf("Go-only report contains Flow data: routes=%+v schemas=%+v", report.FlowHTTPRoutes, report.FlowSchemas)
	}

	files := make(map[string][]byte)
	for _, generated := range core.GenerateTypeScriptAPISDK(report) {
		files[filepath.ToSlash(generated.Path)] = generated.Content
	}
	endpoints := string(files["_gen/api/endpoints.ts"])
	if !strings.Contains(endpoints, `"/api/users"`) {
		t.Fatalf("generated SDK omitted Go route:\n%s", endpoints)
	}
	if strings.Contains(endpoints, "/from-flow") || strings.Contains(endpoints, "/legacy") {
		t.Fatalf("generated SDK included a Flow route:\n%s", endpoints)
	}
}
