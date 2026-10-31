package analyzer_test

import (
	"testing"

	"github.com/nexssp/ops/analyzer"
)

// TestDiagnose_OrphanRouteWarns — DSL declares a route for an action that
// has no matching Go action.
func TestDiagnose_OrphanRouteWarns(t *testing.T) {
	tmp := t.TempDir()
	writeFlow(t, tmp, "app.nflow", `nobody.home:route="GET /api/nobody"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/orphan-route" && d.Severity == analyzer.SeverityWarning {
			found = true
			if d.Action != "nobody.home" {
				t.Errorf("diagnostic action = %q, want nobody.home", d.Action)
			}
		}
	}
	if !found {
		t.Fatalf("expected dsl/orphan-route WARNING; got %+v", report.Diagnostics)
	}

	// Phantom action still created: DSL is authoritative for its own routes.
	if len(report.Actions) != 1 || report.Actions[0].Name != "nobody.home" {
		t.Errorf("expected phantom action nobody.home, got %+v", report.Actions)
	}
}

// TestDiagnose_SilentMutesRule — @config:silent suppresses the matching rule.
func TestDiagnose_SilentMutesRule(t *testing.T) {
	tmp := t.TempDir()
	writeFlow(t, tmp, "app.nflow", `@config:silent=dsl/orphan-route
nobody.home:route="GET /api/nobody"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/orphan-route" {
			t.Fatalf("dsl/orphan-route should have been muted; got %+v", d)
		}
	}
	if report.Stats.WarningCount != 0 {
		t.Errorf("WarningCount = %d, want 0", report.Stats.WarningCount)
	}
}

// TestDiagnose_SilentMultipleRules — comma-separated list.
func TestDiagnose_SilentMultipleRules(t *testing.T) {
	tmp := t.TempDir()
	writeFlow(t, tmp, "app.nflow", `@config:silent=dsl/orphan-route,routes/conflict-go-dsl
nobody.home:route="GET /api/nobody"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/orphan-route" {
			t.Errorf("dsl/orphan-route not muted: %+v", d)
		}
	}
}

// TestDiagnose_StrictPromotesWarnings — @config:strict=true upgrades every
// WARNING to ERROR.
func TestDiagnose_StrictPromotesWarnings(t *testing.T) {
	tmp := t.TempDir()
	writeFlow(t, tmp, "app.nflow", `@config:strict=true
nobody.home:route="GET /api/nobody"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	if report.Stats.WarningCount != 0 {
		t.Errorf("WarningCount = %d, want 0 (all promoted)", report.Stats.WarningCount)
	}
	if report.Stats.ErrorCount == 0 {
		t.Errorf("ErrorCount = 0, want >= 1")
	}

	foundErr := false
	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/orphan-route" && d.Severity == analyzer.SeverityError {
			foundErr = true
		}
	}
	if !foundErr {
		t.Errorf("expected dsl/orphan-route as ERROR under strict; got %+v", report.Diagnostics)
	}
}

// TestDiagnose_SilentWinsOverStrict — silenced rule stays silenced even
// under strict mode.
func TestDiagnose_SilentWinsOverStrict(t *testing.T) {
	tmp := t.TempDir()
	writeFlow(t, tmp, "app.nflow", `@config:strict=true
@config:silent=dsl/orphan-route
nobody.home:route="GET /api/nobody"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/orphan-route" {
			t.Errorf("dsl/orphan-route should be muted even under strict; got %+v", d)
		}
	}
}
