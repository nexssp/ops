package analyzer_test

import (
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func findAction(t *testing.T, report *analyzer.Report, name string) *analyzer.ActionContract {
	t.Helper()
	for i := range report.Actions {
		if report.Actions[i].Name == name {
			return &report.Actions[i]
		}
	}
	t.Fatalf("action %q not found in report", name)
	return nil
}

func findBinding(bindings []analyzer.TransportBinding, kind string) *analyzer.TransportBinding {
	for i := range bindings {
		if bindings[i].Kind == kind {
			return &bindings[i]
		}
	}
	return nil
}

func TestTransports_HTTPBindingDetected(t *testing.T) {
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
	return action.New[Req, Res]("svc.x", nil).Route(thttp.POST("/api/x")).Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "svc.x")
	b := findBinding(act.Transports, "http")
	if b == nil {
		t.Fatalf("http binding missing; transports=%+v", act.Transports)
	}
	if b.Method != "POST" || b.Path != "/api/x" {
		t.Errorf("route = %s %s, want POST /api/x", b.Method, b.Path)
	}
	if b.Target != "POST /api/x" {
		t.Errorf("target = %q, want POST /api/x", b.Target)
	}
}

func TestTransports_SSEWithChannel(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
type B struct{}
func (b *B) Stream() action.AnyAction {
	return action.New[any, any]("console.stream", nil).
		Route(thttp.Stream("/api/events").WithChannel("events").WithBroadcaster(b)).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "console.stream")
	b := findBinding(act.Transports, "sse")
	if b == nil {
		t.Fatalf("sse binding missing; transports=%+v", act.Transports)
	}
	if b.Path != "/api/events" || !b.Stream {
		t.Errorf("path=%q stream=%v", b.Path, b.Stream)
	}
	if b.Meta["channel"] != "events" {
		t.Errorf("channel meta = %q, want events", b.Meta["channel"])
	}
}

func TestTransports_RawHandler(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"net/http"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)
func V() action.AnyAction {
	h := func(w http.ResponseWriter, r *http.Request) {}
	return action.New[struct{}, struct{}]("workspace.view", nil).
		Route(thttp.RawHandler(http.MethodGet, "/api/view", h)).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "workspace.view")
	b := findBinding(act.Transports, "raw")
	if b == nil || b.Method != "GET" || b.Path != "/api/view" || !b.Raw {
		t.Fatalf("raw binding wrong: %+v", b)
	}
}

func TestTransports_CLIWithAliases(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/tcli"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("order.create", nil).
		Route(tcli.Command("order:create", "Create order").WithAliases("oc", "new")).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "order.create")
	b := findBinding(act.Transports, "cli")
	if b == nil {
		t.Fatalf("cli binding missing")
	}
	if b.Target != "order:create" {
		t.Errorf("target = %q", b.Target)
	}
	if len(b.Aliases) != 2 || b.Aliases[0] != "oc" || b.Aliases[1] != "new" {
		t.Errorf("aliases = %v", b.Aliases)
	}
	if b.Meta["description"] != "Create order" {
		t.Errorf("description = %q", b.Meta["description"])
	}
}

func TestTransports_CronEvery(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"time"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/cron"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("cleanup.job", nil).
		Route(cron.Every(5 * time.Second)).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "cleanup.job")
	b := findBinding(act.Transports, "cron")
	if b == nil {
		t.Fatalf("cron binding missing")
	}
	if b.Interval.String() != "5s" {
		t.Errorf("interval = %v", b.Interval)
	}
}

func TestTransports_CronSchedule(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/cron"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("nightly.job", nil).
		Route(cron.Cron("0 0 * * *")).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "nightly.job")
	b := findBinding(act.Transports, "cron")
	if b == nil || b.Schedule != "0 0 * * *" {
		t.Fatalf("cron schedule wrong: %+v", b)
	}
}

func TestTransports_Worker(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"time"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/tworker"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("sync.worker", nil).
		Route(tworker.Every(1 * time.Second)).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "sync.worker")
	b := findBinding(act.Transports, "worker")
	if b == nil || b.Interval.String() != "1s" {
		t.Fatalf("worker binding wrong: %+v", b)
	}
}

func TestTransports_TBusTopic(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/tbus"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("user.created.listener", nil).
		Route(tbus.Topic("user.created")).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "user.created.listener")
	b := findBinding(act.Transports, "topic")
	if b == nil || b.Subject != "user.created" {
		t.Fatalf("topic binding wrong: %+v", b)
	}
}

func TestTransports_TA2ARole(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transportai/ta2a"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("support.reply", nil).
		Route(ta2a.Role("assistant").WithDescription("Assistant agent role").WithExamples("one", "two")).
		Build()
}
`)

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
		t.Errorf("target = %q", b.Target)
	}
	if b.Meta["description"] != "Assistant agent role" {
		t.Errorf("description = %q", b.Meta["description"])
	}
}

func TestTransports_NATSTopic(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transportnats/tnats"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("orders.process", nil).
		Route(tnats.Topic("orders.created", "workers")).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "orders.process")
	b := findBinding(act.Transports, "nats-pubsub")
	if b == nil {
		t.Fatalf("nats-pubsub missing")
	}
	if b.Subject != "orders.created" || b.QueueGroup != "workers" {
		t.Errorf("subject=%q queue=%q", b.Subject, b.QueueGroup)
	}
}

func TestTransports_NATSRequest(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"time"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transportnats/tnats"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("order.query", nil).
		Route(tnats.Request("order.query", 2 * time.Second)).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "order.query")
	b := findBinding(act.Transports, "nats-rpc")
	if b == nil || b.Subject != "order.query" {
		t.Fatalf("nats-rpc wrong: %+v", b)
	}
	if b.Meta["timeout"] != "2s" {
		t.Errorf("timeout = %q, want 2s", b.Meta["timeout"])
	}
}

func TestTransports_NATSKV(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transportnats/tnats"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("config.watch", nil).
		Route(tnats.KV("DYNAMIC_CONFIGS", "runtime.settings")).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "config.watch")
	b := findBinding(act.Transports, "nats-kv")
	if b == nil || b.Meta["bucket"] != "DYNAMIC_CONFIGS" || b.Meta["key"] != "runtime.settings" {
		t.Fatalf("nats-kv wrong: %+v", b)
	}
}

func TestTransports_NATSDurableWork(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transportnats/tnats"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("payments.process", nil).
		Route(tnats.DurableWork("PAYMENTS_STREAM", "payments.process", "payment-processor", "payments.dlq")).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "payments.process")
	b := findBinding(act.Transports, "nats-durable")
	if b == nil {
		t.Fatalf("nats-durable missing")
	}
	if b.Subject != "payments.process" {
		t.Errorf("subject = %q", b.Subject)
	}
	if b.Meta["stream"] != "PAYMENTS_STREAM" || b.Meta["durable"] != "payment-processor" {
		t.Errorf("meta = %+v", b.Meta)
	}
}

func TestTransports_NATSConsumer(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transportnats/tnats"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("inventory.reserve", nil).
		Route(tnats.Consumer("COMMERCE_STREAM", "orders.event.created", "inventory-consumer").NewOnly()).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "inventory.reserve")
	b := findBinding(act.Transports, "nats-consumer")
	if b == nil || b.Subject != "orders.event.created" {
		t.Fatalf("nats-consumer wrong: %+v", b)
	}
	if b.Meta["stream"] != "COMMERCE_STREAM" || b.Meta["durable"] != "inventory-consumer" {
		t.Errorf("meta = %+v", b.Meta)
	}
}

func TestTransports_NATSObjectStore(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transportnats/tnats"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("objects.ingest", nil).
		Route(tnats.ObjectStore("ASSETS_BUCKET", "reports.>").WithData(1024)).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "objects.ingest")
	b := findBinding(act.Transports, "nats-objectstore")
	if b == nil || b.Meta["bucket"] != "ASSETS_BUCKET" || b.Meta["pattern"] != "reports.>" {
		t.Fatalf("nats-objectstore wrong: %+v", b)
	}
}

func TestTransports_NATSService(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transportnats/tnats"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("pricing.get", nil).
		Route(tnats.Service("pricing-svc", "1.0.0", "calculate", "pricing.v1.calc")).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "pricing.get")
	b := findBinding(act.Transports, "nats-service")
	if b == nil || b.Subject != "pricing.v1.calc" {
		t.Fatalf("nats-service wrong: %+v", b)
	}
	if b.Meta["service"] != "pricing-svc" || b.Meta["version"] != "1.0.0" || b.Meta["endpoint"] != "calculate" {
		t.Errorf("meta = %+v", b.Meta)
	}
}

func TestTransports_MultiProtocolSingleAction(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"time"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/cron"
	"github.com/nexssp/transport/tbus"
	"github.com/nexssp/transport/tcli"
	"github.com/nexssp/transport/thttp"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("order.create", nil).
		Route(thttp.POST("/orders")).
		Route(tcli.Command("order:create", "Create order").WithAliases("oc")).
		Route(tbus.Topic("order.create")).
		Route(cron.Every(5 * time.Second)).
		Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "order.create")
	if len(act.Transports) != 4 {
		t.Fatalf("expected 4 transports, got %d: %+v", len(act.Transports), act.Transports)
	}
	for _, kind := range []string{"http", "cli", "topic", "cron"} {
		if findBinding(act.Transports, kind) == nil {
			t.Errorf("missing binding kind=%q", kind)
		}
	}

	// Legacy HTTP fields populated from the HTTP binding (codegen compat).
	if act.Method != "POST" || act.Path != "/orders" {
		t.Errorf("legacy fields wrong: %s %s", act.Method, act.Path)
	}
}

func TestTransports_NonHTTPActionHasNoLegacyMethod(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc.go", `package svc
import (
	"context"
	"time"
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/cron"
)
type Req struct{}
type Res struct{}
func A() action.AnyAction {
	return action.New[Req, Res]("cleanup.job", nil).Route(cron.Every(2 * time.Second)).Build()
}
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	act := findAction(t, report, "cleanup.job")

	// Must not leak the historical POST default when there is no HTTP binding.
	if act.Method != "" {
		t.Errorf("Method = %q, want empty for cron-only action", act.Method)
	}
	if act.Path != "" {
		t.Errorf("Path = %q, want empty", act.Path)
	}
	if len(act.Transports) != 1 || act.Transports[0].Kind != "cron" {
		t.Errorf("transports = %+v", act.Transports)
	}
}
