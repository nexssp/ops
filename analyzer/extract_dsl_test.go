package analyzer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func writeFlow(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeGo(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDSL_ArrowInsideQuotesIsNotPipeline — the regression test for the
// `users.create:type="UsersCreateReq -> User"` bug that produced phantom
// actions named `users` / `user"`.
func TestDSL_ArrowInsideQuotesIsNotPipeline(t *testing.T) {
	tmp := t.TempDir()

	writeGo(t, tmp, "users/service.go", `package users
import (
	"context"
	"github.com/nexssp/kernel/action"
)

type CreateReq struct { Email string `+"`json:\"email\" validate:\"required,email\"`"+` }
type User      struct { ID string `+"`json:\"id\"`"+` }

func CreateAction() action.AnyAction {
	return action.New("users.create", func(ctx context.Context, r CreateReq) (User, error) {
		return User{}, nil
	}).Description("create user").Build()
}
`)

	writeFlow(t, tmp, "app.nflow", `
users.create:type="CreateReq -> User":route="POST /api/users":status=201
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if len(report.Actions) != 1 {
		for _, a := range report.Actions {
			t.Logf("action: %q method=%s path=%s", a.Name, a.Method, a.Path)
		}
		t.Fatalf("expected exactly 1 action, got %d (phantom from quoted arrow?)", len(report.Actions))
	}

	got := report.Actions[0]
	if got.Name != "users.create" {
		t.Errorf("name = %q, want users.create", got.Name)
	}
	if got.Method != "POST" || got.Path != "/api/users" {
		t.Errorf("route = %s %s, want POST /api/users", got.Method, got.Path)
	}
	if got.RouteSource != "dsl" {
		t.Errorf("RouteSource = %q, want dsl", got.RouteSource)
	}
}

// TestDSL_ModifiersParsedCorrectly — every DSL modifier field.
func TestDSL_ModifiersParsedCorrectly(t *testing.T) {
	tmp := t.TempDir()

	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("svc.do", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)

	writeFlow(t, tmp, "svc.nflow", `
svc.do:route="PUT /api/svc/do":desc="does the thing":timeout=5s:cache=30m:retry=3:idempotent
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(report.Actions))
	}
	a := report.Actions[0]

	if a.Method != "PUT" || a.Path != "/api/svc/do" {
		t.Errorf("route = %s %s", a.Method, a.Path)
	}
	if a.Description != "does the thing" {
		t.Errorf("desc = %q", a.Description)
	}
	if a.Timeout.String() != "5s" {
		t.Errorf("timeout = %v", a.Timeout)
	}
	if a.CacheTTL.String() != "30m0s" {
		t.Errorf("cache = %v", a.CacheTTL)
	}
	if a.RetryMax != 3 {
		t.Errorf("retry = %d", a.RetryMax)
	}
	if !a.Idempotent {
		t.Error("idempotent = false")
	}
}

// TestDSL_OrphanRouteFromFlow — a route declared in DSL with no matching Go
// action. Currently produces a phantom action (pre-diagnostic-behavior).
// Post-feature, this should ALSO emit dsl/orphan-route diagnostic.
func TestDSL_OrphanRouteFromFlow(t *testing.T) {
	tmp := t.TempDir()
	writeFlow(t, tmp, "app.nflow", `nobody.home:route="GET /api/nobody"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	// Today: phantom action is created. After @config feature: warning added.
	if len(report.Actions) != 1 || report.Actions[0].Name != "nobody.home" {
		t.Fatalf("expected phantom action nobody.home, got %+v", report.Actions)
	}

	// Assertion for POST-feature:
	// found := false
	// for _, d := range report.Diagnostics {
	// 	if d.Rule == "dsl/orphan-route" { found = true }
	// }
	// if !found { t.Error("expected dsl/orphan-route diagnostic") }
}

// TestDSL_ConflictGoVsDSL_EmitsError — regression for the existing rule.
func TestDSL_ConflictGoVsDSL_EmitsError(t *testing.T) {
	tmp := t.TempDir()

	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("svc.x", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/go-route")).Build()
}
`)

	writeFlow(t, tmp, "app.nflow", `svc.x:route="POST /api/dsl-route"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, d := range report.Diagnostics {
		if d.Rule == "routes/conflict-go-dsl" && d.Severity == analyzer.SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("expected routes/conflict-go-dsl ERROR diagnostic; got %+v", report.Diagnostics)
	}
}

// TestDSL_PipelineHeadTailTypeInference — composite pipeline infers req from
// head node, res from tail node.
func TestDSL_PipelineHeadTailTypeInference(t *testing.T) {
	tmp := t.TempDir()

	writeGo(t, tmp, "agents.go", `package agents
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type PlanReq struct{}
type PlanRes struct{}
type CodeRes struct{}

func Planner() action.AnyAction {
	return action.New("agent.planner", func(ctx context.Context, r PlanReq) (PlanRes, error) {
		return PlanRes{}, nil
	}).Build()
}

func Architect() action.AnyAction {
	return action.New("agent.architect", func(ctx context.Context, r PlanRes) (CodeRes, error) {
		return CodeRes{}, nil
	}).Build()
}
`)

	writeFlow(t, tmp, "pipeline.nflow", `agent.planner -> agent.architect:route="POST /api/pipeline"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	var pipeline *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "pipeline" || strings.Contains(report.Actions[i].Path, "/api/pipeline") {
			pipeline = &report.Actions[i]
		}
	}
	if pipeline == nil {
		t.Fatalf("pipeline action not found; got %+v", report.Actions)
	}

	// Req from head (agent.planner → agents.PlanReq)
	if !strings.Contains(pipeline.ReqTypeName, "PlanReq") {
		t.Errorf("ReqTypeName = %q, want *PlanReq", pipeline.ReqTypeName)
	}
	// Res from tail (agent.architect → agents.CodeRes)
	if !strings.Contains(pipeline.ResTypeName, "CodeRes") {
		t.Errorf("ResTypeName = %q, want *CodeRes", pipeline.ResTypeName)
	}
}
