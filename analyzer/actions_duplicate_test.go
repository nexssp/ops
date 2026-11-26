package analyzer_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestActionsDuplicateNameDifferentRoutes(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "svc.go", duplicateActionSource("/api/second"))

	report, err := analyzer.Analyze(root)
	if err != nil {
		t.Fatal(err)
	}

	var hits []analyzer.Diagnostic
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Rule == "actions/duplicate-name" {
			hits = append(hits, diagnostic)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("duplicate-name diagnostics = %+v, want exactly one", hits)
	}

	diagnostic := hits[0]
	if diagnostic.Severity != analyzer.SeverityError {
		t.Errorf("severity = %s, want ERROR", diagnostic.Severity)
	}
	if diagnostic.Action != "svc.shared" || diagnostic.File == "" || diagnostic.Line == 0 {
		t.Errorf("diagnostic location/action = %+v, want svc.shared with source location", diagnostic)
	}
	if !strings.Contains(diagnostic.Message, "first declaration is at") {
		t.Errorf("diagnostic message omits the first declaration location: %q", diagnostic.Message)
	}
	if report.Stats.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", report.Stats.ErrorCount)
	}
}

func TestActionsDuplicateNameIdenticalDeclarationIsDeduplicated(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "svc.go", duplicateActionSource("/api/first"))

	report, err := analyzer.Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Actions) != 1 {
		t.Fatalf("action count = %d, want one deduplicated action", len(report.Actions))
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Rule == "actions/duplicate-name" {
			t.Fatalf("identical declarations should remain deduplicated: %+v", diagnostic)
		}
	}
}

func TestActionsDuplicateNameCanBeSilenced(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "svc.go", duplicateActionSource("/api/second"))
	writeFlow(t, root, "app.nflow", "@config:silent=actions/duplicate-name\n")

	report, err := analyzer.Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Rule == "actions/duplicate-name" {
			t.Fatalf("duplicate-name should be silenced: %+v", diagnostic)
		}
	}
	if report.Stats.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0 after silencing", report.Stats.ErrorCount)
	}
}

func duplicateActionSource(secondPath string) string {
	return fmt.Sprintf(`package svc

import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type Request struct{}
type Response struct{}

func First() action.AnyAction {
	return action.New("svc.shared", func(ctx context.Context, request Request) (Response, error) {
		return Response{}, nil
	}).Route(thttp.POST("/api/first")).Build()
}

func Second() action.AnyAction {
	return action.New("svc.shared", func(ctx context.Context, request Request) (Response, error) {
		return Response{}, nil
	}).Route(thttp.POST(%q)).Build()
}
`, secondPath)
}
