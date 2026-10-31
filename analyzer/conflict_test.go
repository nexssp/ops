package analyzer_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestConflictArbiter_RejectsDoubleRouteDefinition(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	// 1. Action defines Route in Go code
	goCode := `package domain
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type Req struct { Val string }
type Res struct { Out string }

func Action() action.AnyAction {
	return action.New("test.route", func(ctx context.Context, r Req) (Res, error) {
		return Res{Out: r.Val}, nil
	}).
		Route(thttp.POST("/api/conflict/go")).
		Build()
}
`
	if err := os.WriteFile(filepath.Join(tmp, "service.go"), []byte(goCode), 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. DSL defines different route for the same action
	flowContent := `test.route:route="POST /api/conflict/dsl"`
	if err := os.WriteFile(filepath.Join(tmp, "app.nflow"), []byte(flowContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// 3. Analyzer must detect the conflict and produce an ERROR diagnostic
	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatalf("analyzer error: %v", err)
	}

	foundConflict := false
	for _, d := range report.Diagnostics {
		if d.Rule == "routes/conflict-go-dsl" {
			foundConflict = true
			t.Logf("Diagnostic caught as expected: %s", d.Message)
			break
		}
	}

	if !foundConflict {
		t.Fatal("CONFLICT ARBITER FAILED: Double route declaration was not rejected by analyzer")
	}
}
