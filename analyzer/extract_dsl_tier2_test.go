package analyzer_test

import (
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestTier2_Backoff(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("flaky.call", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `flaky.call:route="POST /api/x":backoff="exponential,base=100ms,max=5s"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "flaky.call")

	if act.BackoffStrategy != "exponential" {
		t.Errorf("BackoffStrategy = %q", act.BackoffStrategy)
	}
	if act.BackoffBase.String() != "100ms" {
		t.Errorf("BackoffBase = %v", act.BackoffBase)
	}
	if act.BackoffMax.String() != "5s" {
		t.Errorf("BackoffMax = %v", act.BackoffMax)
	}
}

func TestTier2_BackoffSeparateKeys(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("flaky.call", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `flaky.call:route="POST /api/x":backoff="linear":backoff_base=50ms:backoff_max=2s`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "flaky.call")

	if act.BackoffStrategy != "linear" {
		t.Errorf("BackoffStrategy = %q", act.BackoffStrategy)
	}
	if act.BackoffBase.String() != "50ms" {
		t.Errorf("BackoffBase = %v", act.BackoffBase)
	}
	if act.BackoffMax.String() != "2s" {
		t.Errorf("BackoffMax = %v", act.BackoffMax)
	}
}

func TestTier2_RetryPredicate(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("api.call", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `api.call:route="POST /api/x":retry=3:retry_if=transient`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "api.call")

	if act.RetryPredicate != "transient" {
		t.Errorf("RetryPredicate = %q", act.RetryPredicate)
	}
	if act.RetryMax != 3 {
		t.Errorf("RetryMax = %d", act.RetryMax)
	}
}

func TestTier2_Priority(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("health.check", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `health.check:route="GET /health":priority=critical`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "health.check")

	if act.Priority != "critical" {
		t.Errorf("Priority = %q", act.Priority)
	}
}

func TestTier2_CacheKey(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("users.get", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `users.get:route="GET /api/users/{id}":cache=1h:cache_key="user.id"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "users.get")

	if act.CacheKey != "user.id" {
		t.Errorf("CacheKey = %q", act.CacheKey)
	}
	if act.CacheTTL.String() != "1h0m0s" {
		t.Errorf("CacheTTL = %v", act.CacheTTL)
	}
}

func TestTier2_Audit(t *testing.T) {
	cases := []struct {
		name string
		flow string
		want bool
	}{
		{"bare keyword", `users.delete:route="POST /api/users/delete":audit`, true},
		{"audit=true", `users.delete:route="POST /api/users/delete":audit=true`, true},
		{"audit=yes", `users.delete:route="POST /api/users/delete":audit=yes`, true},
		{"audit=1", `users.delete:route="POST /api/users/delete":audit=1`, true},
		{"audit=false", `users.delete:route="POST /api/users/delete":audit=false`, false},
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
	return action.New("users.delete", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
			writeFlow(t, dir, "svc.nflow", c.flow)

			report, err := analyzer.Analyze(dir)
			if err != nil {
				t.Fatal(err)
			}
			act := findAction(t, report, "users.delete")
			if act.Audit != c.want {
				t.Errorf("Audit = %v, want %v", act.Audit, c.want)
			}
		})
	}
}

func TestTier2_IdempotencyHeader(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("payments.charge", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `payments.charge:route="POST /api/pay":idempotent:idempotency_header="X-Tenant-Idem-Key"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "payments.charge")

	if !act.Idempotent {
		t.Errorf("Idempotent = false")
	}
	if act.IdempotencyHeader != "X-Tenant-Idem-Key" {
		t.Errorf("IdempotencyHeader = %q", act.IdempotencyHeader)
	}
}

func TestTier2_Deprecated(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("users.create.v1", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `users.create.v1:route="POST /api/v1/users":deprecated=true:since="2.0":use="v2.users.create"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "users.create.v1")

	if !act.Deprecated {
		t.Errorf("Deprecated = false")
	}
	if act.DeprecatedSince != "2.0" {
		t.Errorf("DeprecatedSince = %q", act.DeprecatedSince)
	}
	if act.DeprecatedUse != "v2.users.create" {
		t.Errorf("DeprecatedUse = %q", act.DeprecatedUse)
	}
}

func TestTier2_BareDeprecatedKeyword(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("legacy.action", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `legacy.action:route="GET /api/legacy":deprecated`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "legacy.action")

	if !act.Deprecated {
		t.Errorf("bare :deprecated should set Deprecated=true")
	}
}

func TestTier2_EverythingTogether(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New("orders.submit", func(ctx context.Context, r Req) (Res, error) {
		return Res{}, nil
	}).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow",
		`orders.submit:http="POST /api/orders":cli="orders:submit":status=201:auth=required:role="admin,customer":perm="orders:write":feature="checkout_v2":budget=$1.00:rate_limit="500/s":burst=1000:concurrency=50:breaker="failures=10,cooldown=60s":timeout=10s:retry=3:backoff="exponential,base=200ms,max=10s":retry_if=transient:priority=high:cache=5m:cache_key="order.id":audit=true:idempotent:idempotency_header="X-Order-Idem-Key":tag="orders,write,checkout"`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "orders.submit")

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Method", act.Method, "POST"},
		{"Path", act.Path, "/api/orders"},
		{"SuccessStatus", act.SuccessStatus, 201},
		{"RequiresAuth", act.RequiresAuth, true},
		{"BudgetMicros", act.BudgetMicros, int64(1_000_000)},
		{"RateLimit", act.RateLimit, "500/s"},
		{"RateLimitBurst", act.RateLimitBurst, 200*5 + 0}, // guard against typos; see below
	}
	_ = checks

	if act.Method != "POST" || act.Path != "/api/orders" {
		t.Errorf("route = %s %s", act.Method, act.Path)
	}
	if act.SuccessStatus != 201 {
		t.Errorf("SuccessStatus = %d", act.SuccessStatus)
	}
	if !act.RequiresAuth {
		t.Errorf("RequiresAuth = false")
	}
	if act.BudgetMicros != 1_000_000 {
		t.Errorf("BudgetMicros = %d, want 1000000", act.BudgetMicros)
	}
	if act.RateLimit != "500/s" || act.RateLimitBurst != 1000 {
		t.Errorf("rate = %q burst = %d", act.RateLimit, act.RateLimitBurst)
	}
	if act.ConcurrencyLimit != 50 {
		t.Errorf("ConcurrencyLimit = %d", act.ConcurrencyLimit)
	}
	if act.BreakerFailures != 10 || act.BreakerCooldown.String() != "1m0s" {
		t.Errorf("breaker = %d / %v", act.BreakerFailures, act.BreakerCooldown)
	}
	if act.Timeout.String() != "10s" {
		t.Errorf("Timeout = %v", act.Timeout)
	}
	if act.RetryMax != 3 {
		t.Errorf("RetryMax = %d", act.RetryMax)
	}
	if act.BackoffStrategy != "exponential" || act.BackoffBase.String() != "200ms" || act.BackoffMax.String() != "10s" {
		t.Errorf("backoff = %q base=%v max=%v", act.BackoffStrategy, act.BackoffBase, act.BackoffMax)
	}
	if act.RetryPredicate != "transient" {
		t.Errorf("RetryPredicate = %q", act.RetryPredicate)
	}
	if act.Priority != "high" {
		t.Errorf("Priority = %q", act.Priority)
	}
	if act.CacheTTL.String() != "5m0s" {
		t.Errorf("CacheTTL = %v", act.CacheTTL)
	}
	if act.CacheKey != "order.id" {
		t.Errorf("CacheKey = %q", act.CacheKey)
	}
	if !act.Audit {
		t.Errorf("Audit = false")
	}
	if !act.Idempotent {
		t.Errorf("Idempotent = false")
	}
	if act.IdempotencyHeader != "X-Order-Idem-Key" {
		t.Errorf("IdempotencyHeader = %q", act.IdempotencyHeader)
	}
	if !equalSlices(act.Tags, []string{"orders", "write", "checkout"}) {
		t.Errorf("Tags = %v", act.Tags)
	}
	if !equalSlices(act.Roles, []string{"admin", "customer"}) {
		t.Errorf("Roles = %v", act.Roles)
	}
	if !equalSlices(act.Permissions, []string{"orders:write"}) {
		t.Errorf("Permissions = %v", act.Permissions)
	}
	if !equalSlices(act.Features, []string{"checkout_v2"}) {
		t.Errorf("Features = %v", act.Features)
	}
}
