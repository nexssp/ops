package analyzer_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer"
)

func TestDuplicateStructNamesAcrossPackages(t *testing.T) {
	tmp := t.TempDir()

	// Package 1: ai
	aiDir := filepath.Join(tmp, "ai")
	if err := os.MkdirAll(aiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aiCode := `package ai

import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type DockerExecRecord struct {
	ID        string ` + "`json:\"id\" validate:\"required\"`" + `
	ModelName string ` + "`json:\"model_name\"`" + `
}

type DockerExecResponse struct {
	Success bool ` + "`json:\"success\"`" + `
}

func RegisterActions() action.AnyAction {
	return action.New[DockerExecRecord, DockerExecResponse]("ai.exec", nil).
		Route(thttp.POST("/api/ai/exec")).
		Build()
}
`
	if err := os.WriteFile(filepath.Join(aiDir, "types.go"), []byte(aiCode), 0o600); err != nil {
		t.Fatal(err)
	}

	// Package 2: jumalu
	jumaluDir := filepath.Join(tmp, "jumalu")
	if err := os.MkdirAll(jumaluDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jumaluCode := `package jumalu

import (
	"github.com/nexssp/kernel/action"
	"github.com/nexssp/transport/thttp"
)

type DockerExecRecord struct {
	ContainerID string ` + "`json:\"container_id\" validate:\"required\"`" + `
	ExitCode    int    ` + "`json:\"exit_code\"`" + `
}

type DockerExecResponse struct {
	Output string ` + "`json:\"output\"`" + `
}

func RegisterActions() action.AnyAction {
	return action.New[DockerExecRecord, DockerExecResponse]("jumalu.exec", nil).
		Route(thttp.POST("/api/jumalu/exec")).
		Build()
}
`
	if err := os.WriteFile(filepath.Join(jumaluDir, "types.go"), []byte(jumaluCode), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Verify both structs were retained without collision
	if report.Stats.TotalStructs != 4 {
		t.Fatalf("expected 4 structs, got %d", report.Stats.TotalStructs)
	}

	aiStruct, ok := report.Structs["ai.DockerExecRecord"]
	if !ok {
		t.Fatalf("expected ai.DockerExecRecord to be registered")
	}
	if len(aiStruct.Fields) != 2 || aiStruct.Fields[0].Key != "id" {
		t.Errorf("ai.DockerExecRecord fields corrupted: %+v", aiStruct.Fields)
	}

	jumaluStruct, ok := report.Structs["jumalu.DockerExecRecord"]
	if !ok {
		t.Fatalf("expected jumalu.DockerExecRecord to be registered")
	}
	if len(jumaluStruct.Fields) != 2 || jumaluStruct.Fields[0].Key != "container_id" {
		t.Errorf("jumalu.DockerExecRecord fields corrupted: %+v", jumaluStruct.Fields)
	}

	// Verify generated TypeScript SDK prefixing
	sdkFiles := generatedByName(analyzer.GenerateTypeScriptSDK(report))
	sdk := string(sdkFiles["_gen/api/types.ts"]) + "\n" + string(sdkFiles["_gen/api/endpoints.ts"])

	if !strings.Contains(sdk, "export interface AiDockerExecRecord {") {
		t.Errorf("missing AiDockerExecRecord interface in SDK")
	}
	if !strings.Contains(sdk, "export interface JumaluDockerExecRecord {") {
		t.Errorf("missing JumaluDockerExecRecord interface in SDK")
	}
	if !strings.Contains(sdk, "export const AiDockerExecRecordSchema = z.object({") {
		t.Errorf("missing AiDockerExecRecordSchema in SDK")
	}
	if !strings.Contains(sdk, "export const JumaluDockerExecRecordSchema = z.object({") {
		t.Errorf("missing JumaluDockerExecRecordSchema in SDK")
	}
}

func TestUnknownVsAnyGeneration(t *testing.T) {
	tmp := t.TempDir()

	// Uses the documented @nexss:expose directive so that these structs are
	// candidates for generation regardless of filename. This isolates the
	// test to the type-mapping logic it verifies (any/interface{}/error →
	// unknown, z.unknown() not z.any()).
	code := `package svc

// @nexss:expose
type GenericPayload struct {
	Metadata any         ` + "`json:\"metadata\"`" + `
	Raw      interface{} ` + "`json:\"raw\"`" + `
	ErrData  error       ` + "`json:\"err_data\"`" + `
}

// @nexss:expose
type GenericResponse struct {
	Result any ` + "`json:\"result\"`" + `
}
`
	if err := os.WriteFile(filepath.Join(tmp, "svc.go"), []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := analyzer.Analyze(tmp)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	sdkFiles := generatedByName(analyzer.GenerateTypeScriptSDK(report))
	sdk := string(sdkFiles["_gen/api/types.ts"]) + "\n" + string(sdkFiles["_gen/api/endpoints.ts"])

	if strings.Contains(sdk, "metadata?: any;") {
		t.Errorf("expected metadata to be unknown, found any")
	}
	if !strings.Contains(sdk, "metadata?: unknown;") {
		t.Errorf("expected metadata?: unknown; in interface")
	}
	if !strings.Contains(sdk, "z.unknown()") {
		t.Errorf("expected z.unknown() in Zod schema")
	}
	if strings.Contains(sdk, "z.any()") {
		t.Errorf("z.any() should not be generated")
	}
}
