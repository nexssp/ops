package analyzer_test

import (
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestTypeOverride_AppliesWhenSilent(t *testing.T) {
	tmp := t.TempDir()

	// CreateReq lives in internal.go (not a convention file) so the analyzer
	// does NOT auto-expose it. PublicUserCreateReq lives in types.go so it IS
	// auto-exposed and therefore reachable via the type override.
	writeGo(t, tmp, "users/internal.go", `package users

type CreateReq struct {
	Email       string `+"`json:\"email\" validate:\"required,email\"`"+`
	Name        string `+"`json:\"name\"  validate:\"required,min=2\"`"+`
	TenantID    string `+"`json:\"tenant_id\"`"+`
	AuditUserID string `+"`json:\"audit_user_id\"`"+`
}
`)
	writeGo(t, tmp, "users/types.go", `package users

type PublicUserCreateReq struct {
	Email string `+"`json:\"email\" validate:\"required,email\"`"+`
	Name  string `+"`json:\"name\"  validate:\"required,min=2\"`"+`
}

type User struct {
	ID string `+"`json:\"id\"`"+`
}
`)
	writeGo(t, tmp, "users/service.go", `package users
import (
	"context"
	"github.com/nexssp/kernel/action"
)
func CreateAction() action.AnyAction {
	return action.New[CreateReq, User]("users.create", nil).Build()
}
`)
	writeFlow(t, tmp, "app.nflow", `@config:silent=dsl/type-override-mismatch
users.create:type="PublicUserCreateReq -> User":route="POST /api/users":status=201
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/type-override-mismatch" {
			t.Fatalf("dsl/type-override-mismatch should have been silenced; got %+v", d)
		}
	}

	var act *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "users.create" {
			act = &report.Actions[i]
			break
		}
	}
	if act == nil {
		t.Fatalf("users.create not found; got %+v", report.Actions)
	}
	if act.ReqTypeName != "PublicUserCreateReq" {
		t.Errorf("ReqTypeName = %q, want PublicUserCreateReq", act.ReqTypeName)
	}
	if act.TypeSource != "dsl-override" {
		t.Errorf("TypeSource = %q, want dsl-override", act.TypeSource)
	}

	ts := string(generatedByName(analyzer.GenerateTypeScriptSDK(report))["_gen/api/types.ts"])
	if !strings.Contains(ts, "export interface PublicUserCreateReq {") {
		t.Errorf("PublicUserCreateReq interface missing from types.ts")
	}
	if strings.Contains(ts, "tenant_id") {
		t.Errorf("tenant_id should be excluded from generated types; got:\n%s", ts)
	}
	if strings.Contains(ts, "audit_user_id") {
		t.Errorf("audit_user_id should be excluded from generated types; got:\n%s", ts)
	}
}

func TestTypeOverride_EmitsMismatchError(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "users/types.go", `package users

type CreateReq struct {
	Email string `+"`json:\"email\" validate:\"required,email\"`"+`
}
type PublicUserCreateReq struct {
	Email string `+"`json:\"email\" validate:\"required,email\"`"+`
}
type User struct {
	ID string `+"`json:\"id\"`"+`
}
`)
	writeGo(t, tmp, "users/service.go", `package users
import (
	"context"
	"github.com/nexssp/kernel/action"
)
func CreateAction() action.AnyAction {
	return action.New[CreateReq, User]("users.create", nil).Build()
}
`)
	writeFlow(t, tmp, "app.nflow", `users.create:type="PublicUserCreateReq -> User":route="POST /api/users":status=201
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	var hits []analyzer.Diagnostic
	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/type-override-mismatch" {
			hits = append(hits, d)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly 1 dsl/type-override-mismatch diagnostic, got %d: %+v", len(hits), hits)
	}
	if hits[0].Severity != analyzer.SeverityError {
		t.Errorf("severity = %s, want ERROR", hits[0].Severity)
	}
	msg := hits[0].Message
	if !strings.Contains(msg, "PublicUserCreateReq") || !strings.Contains(msg, "CreateReq") {
		t.Errorf("message should name both DTOs; got %q", msg)
	}

	var act *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "users.create" {
			act = &report.Actions[i]
			break
		}
	}
	if act == nil {
		t.Fatal("users.create missing")
	}
	if act.ReqTypeName != "PublicUserCreateReq" {
		t.Errorf("ReqTypeName = %q, want PublicUserCreateReq (override still applied)", act.ReqTypeName)
	}
	if act.TypeSource != "dsl-override" {
		t.Errorf("TypeSource = %q, want dsl-override", act.TypeSource)
	}
}

func TestTypeOverride_NoConflictWhenGoIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc/types.go", `package svc

type Envelope struct {
	Payload string `+"`json:\"payload\" validate:\"required\"`"+`
}
`)
	writeGo(t, tmp, "svc/service.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
func A() action.AnyAction {
	return action.New[struct{}, struct{}]("svc.emit", nil).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `svc.emit:type="Envelope -> Envelope":route="POST /api/emit"
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/type-override-mismatch" {
			t.Fatalf("no conflict expected when Go DTOs are struct{}; got %+v", d)
		}
	}

	var act *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "svc.emit" {
			act = &report.Actions[i]
			break
		}
	}
	if act == nil {
		t.Fatal("svc.emit missing")
	}
	if act.ReqTypeName != "Envelope" || act.ResTypeName != "Envelope" {
		t.Errorf("req=%q res=%q, want both Envelope", act.ReqTypeName, act.ResTypeName)
	}
	if act.TypeSource != "dsl" {
		t.Errorf("TypeSource = %q, want dsl (no override conflict)", act.TypeSource)
	}
}

func TestTypeOverride_NoConflictWhenMatch(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc/types.go", `package svc

type SubmitReq struct {
	V string `+"`json:\"v\"`"+`
}
type SubmitRes struct {
	OK bool `+"`json:\"ok\"`"+`
}
`)
	writeGo(t, tmp, "svc/service.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
func A() action.AnyAction {
	return action.New[SubmitReq, SubmitRes]("svc.submit", nil).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `svc.submit:type="SubmitReq -> SubmitRes":route="POST /api/submit"
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "dsl/type-override-mismatch" {
			t.Fatalf("no conflict expected when DSL matches Go; got %+v", d)
		}
	}

	var act *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "svc.submit" {
			act = &report.Actions[i]
			break
		}
	}
	if act == nil {
		t.Fatal("svc.submit missing")
	}
	if act.TypeSource == "dsl-override" {
		t.Errorf("TypeSource = dsl-override, want empty (no override occurred)")
	}
}

func TestTypeOverride_PartialReq(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "svc/types.go", `package svc

type FullReq struct {
	V string `+"`json:\"v\"`"+`
}
type MiniReq struct {
	V string `+"`json:\"v\"`"+`
}
type Res struct {
	OK bool `+"`json:\"ok\"`"+`
}
`)
	writeGo(t, tmp, "svc/service.go", `package svc
import (
	"context"
	"github.com/nexssp/kernel/action"
)
func A() action.AnyAction {
	return action.New[FullReq, Res]("svc.mix", nil).Build()
}
`)
	writeFlow(t, tmp, "svc.nflow", `@config:silent=dsl/type-override-mismatch
svc.mix:type="MiniReq":route="POST /api/mix"
`)

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatal(err)
	}

	var act *analyzer.ActionContract
	for i := range report.Actions {
		if report.Actions[i].Name == "svc.mix" {
			act = &report.Actions[i]
			break
		}
	}
	if act == nil {
		t.Fatal("svc.mix missing")
	}
	if act.ReqTypeName != "MiniReq" {
		t.Errorf("ReqTypeName = %q, want MiniReq", act.ReqTypeName)
	}
	// Go extraction stores the fully-qualified name ("svc.Res"); the bare
	// name is never assigned because the DSL does not override the response.
	if !strings.HasSuffix(act.ResTypeName, "Res") || strings.HasSuffix(act.ResTypeName, "MiniReq") {
		t.Errorf("ResTypeName = %q, want qualified *Res (unchanged)", act.ResTypeName)
	}
	if act.TypeSource != "dsl-override" {
		t.Errorf("TypeSource = %q, want dsl-override (req was overridden)", act.TypeSource)
	}
}
