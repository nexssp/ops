package analyzer_test

import (
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestRoutesDuplicate_TwoActionsSameMethodAndPath(t *testing.T) {
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
	return action.New("svc.alpha", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/dup")).Build()
}
func B() action.AnyAction {
	return action.New("svc.beta", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/dup")).Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	var hits []analyzer.Diagnostic
	for _, d := range report.Diagnostics {
		if d.Rule == "routes/duplicate" {
			hits = append(hits, d)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly 1 routes/duplicate diagnostic, got %d: %+v", len(hits), hits)
	}
	if hits[0].Severity != analyzer.SeverityError {
		t.Errorf("severity = %s, want ERROR", hits[0].Severity)
	}
	msg := hits[0].Message
	if !strings.Contains(msg, "svc.alpha") || !strings.Contains(msg, "svc.beta") {
		t.Errorf("message should name both actions; got %q", msg)
	}
	if !strings.Contains(msg, "POST /api/dup") {
		t.Errorf("message should name the route; got %q", msg)
	}

	if report.Stats.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", report.Stats.ErrorCount)
	}
}

func TestRoutesDuplicate_DifferentPaths_NoFlag(t *testing.T) {
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
	return action.New("svc.alpha", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/a")).Build()
}
func B() action.AnyAction {
	return action.New("svc.beta", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/b")).Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "routes/duplicate" {
			t.Errorf("unexpected routes/duplicate: %+v", d)
		}
	}
}

func TestRoutesDuplicate_DifferentMethods_NoFlag(t *testing.T) {
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
	return action.New("svc.alpha", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.GET("/api/x")).Build()
}
func B() action.AnyAction {
	return action.New("svc.beta", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/x")).Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "routes/duplicate" {
			t.Errorf("unexpected routes/duplicate: %+v", d)
		}
	}
}

func TestRoutesDuplicate_SameActionTwice_NoFlag(t *testing.T) {
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
	return action.New("svc.same", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/x")).Build()
}
func B() action.AnyAction {
	return action.New("svc.same", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/x")).Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "routes/duplicate" {
			t.Errorf("same action name should be deduped silently; got %+v", d)
		}
	}
}

func TestRoutesDuplicate_Silenceable(t *testing.T) {
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
	return action.New("svc.alpha", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/dup")).Build()
}
func B() action.AnyAction {
	return action.New("svc.beta", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Route(thttp.POST("/api/dup")).Build()
}
`)
	writeFlow(t, tmp, "app.nflow", `@config:silent=routes/duplicate`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "routes/duplicate" {
			t.Errorf("routes/duplicate should have been silenced; got %+v", d)
		}
	}
	if report.Stats.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0", report.Stats.ErrorCount)
	}
}
