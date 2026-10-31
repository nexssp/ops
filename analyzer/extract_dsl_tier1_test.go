package analyzer_test

import (
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTier1_TagsRolesPermissionsFeatures(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("users.create", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `users.create:route="POST /api/users":tag="users,write,public":role="admin,operator":perm="users:write,users:create":feature="beta_access,audit_trail"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "users.create")

	if !equalSlices(act.Tags, []string{"users", "write", "public"}) {
		t.Errorf("Tags = %v", act.Tags)
	}
	if !equalSlices(act.Roles, []string{"admin", "operator"}) {
		t.Errorf("Roles = %v", act.Roles)
	}
	if !equalSlices(act.Permissions, []string{"users:write", "users:create"}) {
		t.Errorf("Permissions = %v", act.Permissions)
	}
	if !equalSlices(act.Features, []string{"beta_access", "audit_trail"}) {
		t.Errorf("Features = %v", act.Features)
	}
}

func TestTier1_AuthScopeStatus(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("admin.ban", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `admin.ban:route="POST /api/admin/ban":auth=required:scope=internal:status=202`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "admin.ban")

	if !act.RequiresAuth {
		t.Errorf("RequiresAuth = false")
	}
	if act.Scope != "internal" {
		t.Errorf("Scope = %q, want internal", act.Scope)
	}
	if act.SuccessStatus != 202 {
		t.Errorf("SuccessStatus = %d, want 202", act.SuccessStatus)
	}
}

func TestTier1_BudgetDollarAndMicros(t *testing.T) {
	cases := []struct {
		name string
		flow string
		want int64
	}{
		{"dollar with decimal", `autonomous.solve:route="POST /api/x":budget=$0.50`, 500_000},
		{"bare decimal", `autonomous.solve:route="POST /api/x":budget=0.25`, 250_000},
		{"explicit micros", `autonomous.solve:route="POST /api/x":budget_micros=750000`, 750_000},
		{"bare integer is micros", `autonomous.solve:route="POST /api/x":budget=1000000`, 1_000_000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeGo(t, dir, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("autonomous.solve", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
			writeFlow(t, dir, "svc.nflow", c.flow)

			report, err := analyzer.Analyze(dir)
			if err != nil {
				t.Fatal(err)
			}
			act := findAction(t, report, "autonomous.solve")
			if act.BudgetMicros != c.want {
				t.Errorf("BudgetMicros = %d, want %d", act.BudgetMicros, c.want)
			}
		})
	}
}

func TestTier1_RateLimitAndConcurrency(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("expensive.query", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `expensive.query:route="POST /api/q":rate_limit="100/s":burst=200:concurrency=10`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "expensive.query")

	if act.RateLimit != "100/s" {
		t.Errorf("RateLimit = %q", act.RateLimit)
	}
	if act.RateLimitBurst != 200 {
		t.Errorf("RateLimitBurst = %d", act.RateLimitBurst)
	}
	if act.ConcurrencyLimit != 10 {
		t.Errorf("ConcurrencyLimit = %d", act.ConcurrencyLimit)
	}
}

func TestTier1_CircuitBreaker(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("flaky.api", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `flaky.api:route="POST /api/x":breaker="failures=5,cooldown=30s"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "flaky.api")

	if act.BreakerFailures != 5 {
		t.Errorf("BreakerFailures = %d", act.BreakerFailures)
	}
	if act.BreakerCooldown.String() != "30s" {
		t.Errorf("BreakerCooldown = %v", act.BreakerCooldown)
	}
}

func TestTier1_HITL(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("dangerous.delete", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `dangerous.delete:route="POST /api/del":hitl="Delete production data?":hitl_options="approve,reject":hitl_trigger="all,danger"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "dangerous.delete")

	if act.HITLPrompt != "Delete production data?" {
		t.Errorf("HITLPrompt = %q", act.HITLPrompt)
	}
	if !equalSlices(act.HITLOptions, []string{"approve", "reject"}) {
		t.Errorf("HITLOptions = %v", act.HITLOptions)
	}
	if !equalSlices(act.HITLTriggers, []string{"all", "danger"}) {
		t.Errorf("HITLTriggers = %v", act.HITLTriggers)
	}
}

func TestTier1_MergeGoAndDSLListFields(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("users.create", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).
		Tag("go-tag-1", "go-tag-2").
		RequireRole("admin").
		RequirePermission("users:read").
		Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `users.create:route="POST /api/users":tag="dsl-tag-1":role="operator":perm="users:write"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "users.create")

	// Tags merged, no duplicates.
	if len(act.Tags) != 3 {
		t.Errorf("Tags = %v, want 3 entries", act.Tags)
	}
	// Roles merged with admin from Go.
	if !equalSlices(act.Roles, []string{"admin", "operator"}) {
		t.Errorf("Roles = %v", act.Roles)
	}
	// Permissions merged with users:read from Go.
	if !equalSlices(act.Permissions, []string{"users:read", "users:write"}) {
		t.Errorf("Permissions = %v", act.Permissions)
	}
}

func TestTier1_MultiTransportFromDSL(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("order.create", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow",
		`order.create:http="POST /orders":cli="order:create":cron="every 5s":worker="every 1s":nats="orders.created":topic="order.internal"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "order.create")

	want := map[string]bool{
		"http":        false,
		"cli":         false,
		"cron":        false,
		"worker":      false,
		"nats-pubsub": false,
		"topic":       false,
	}
	for _, b := range act.Transports {
		if _, ok := want[b.Kind]; ok {
			want[b.Kind] = true
		}
	}
	for kind, found := range want {
		if !found {
			t.Errorf("missing transport kind %q; got %+v", kind, act.Transports)
		}
	}

	if act.Method != "POST" || act.Path != "/orders" {
		t.Errorf("legacy HTTP fields wrong: %s %s", act.Method, act.Path)
	}
}

func TestTier1_SSEWithChannelFromDSL(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"github.com/nexssp/kernel/action"
)
func A() action.AnyAction {
	return action.New[any, any]("console.stream", nil).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `console.stream:sse="/api/events":channel="events"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "console.stream")

	b := findBinding(act.Transports, "sse")
	if b == nil {
		t.Fatalf("sse binding missing; got %+v", act.Transports)
	}
	if b.Path != "/api/events" || !b.Stream {
		t.Errorf("sse binding wrong: %+v", b)
	}
	if b.Meta["channel"] != "events" {
		t.Errorf("channel meta = %q, want events", b.Meta["channel"])
	}
}

func TestTier1_CLIAliasesFromDSL(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("pack.run", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `pack.run:cli="pack":cli_alias="p,pk":cli_desc="Package sources"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "pack.run")

	b := findBinding(act.Transports, "cli")
	if b == nil {
		t.Fatalf("cli binding missing")
	}
	if !equalSlices(b.Aliases, []string{"p", "pk"}) {
		t.Errorf("Aliases = %v", b.Aliases)
	}
	if b.Meta["description"] != "Package sources" {
		t.Errorf("description = %q", b.Meta["description"])
	}
}

func TestTier1_A2AWithDescription(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("support.reply", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `support.reply:a2a="assistant":a2a_desc="Assistant agent role":a2a_example="hello,hi"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "support.reply")

	b := findBinding(act.Transports, "a2a")
	if b == nil {
		t.Fatalf("a2a binding missing")
	}
	if b.Target != "assistant" {
		t.Errorf("Target = %q", b.Target)
	}
	if b.Meta["description"] != "Assistant agent role" {
		t.Errorf("description meta = %q", b.Meta["description"])
	}
}

func TestTier1_NATSKVFromDSL(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("config.watch", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `config.watch:nats_kv="DYNAMIC_CONFIGS/runtime.settings"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "config.watch")

	b := findBinding(act.Transports, "nats-kv")
	if b == nil {
		t.Fatalf("nats-kv binding missing")
	}
	if b.Meta["bucket"] != "DYNAMIC_CONFIGS" || b.Meta["key"] != "runtime.settings" {
		t.Errorf("meta = %+v", b.Meta)
	}
}

func TestTier1_NATSDurableFromDSL(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("payments.process", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `payments.process:nats_durable="PAYMENTS_STREAM/payments.process/processor/payments.dlq"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "payments.process")

	b := findBinding(act.Transports, "nats-durable")
	if b == nil {
		t.Fatalf("nats-durable binding missing")
	}
	if b.Meta["stream"] != "PAYMENTS_STREAM" || b.Meta["durable"] != "processor" || b.Meta["dlq"] != "payments.dlq" {
		t.Errorf("meta = %+v", b.Meta)
	}
}

func TestTier1_NATSServiceFromDSL(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("pricing.get", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `pricing.get:nats_svc="pricing-svc/1.0.0/calculate/pricing.v1.calc"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "pricing.get")

	b := findBinding(act.Transports, "nats-service")
	if b == nil {
		t.Fatalf("nats-service binding missing")
	}
	if b.Subject != "pricing.v1.calc" {
		t.Errorf("Subject = %q", b.Subject)
	}
	if b.Meta["service"] != "pricing-svc" || b.Meta["version"] != "1.0.0" || b.Meta["endpoint"] != "calculate" {
		t.Errorf("meta = %+v", b.Meta)
	}
}

func TestTier1_OrphanWithTransportsStillWarns(t *testing.T) {
	tmp := t.TempDir()
	writeFlow(t, tmp, "svc.nflow", `nobody.home:http="GET /api/nobody":cron="every 1h"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/orphan-route" && d.Action == "nobody.home" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dsl/orphan-route warning; got %+v", report.Diagnostics)
	}

	act := findAction(t, report, "nobody.home")
	if len(act.Transports) != 2 {
		t.Errorf("expected 2 transports on orphan action, got %d", len(act.Transports))
	}
}
