package analyzer

import (
	"strings"
	"testing"

	"github.com/nexssp/ops/analyzer/core"
)

func TestApplyAPISurfaceManifestMergesTypesAndClientConfig(t *testing.T) {
	required := false
	report := &Report{
		Structs: map[string]StructDefinition{
			"legacy.InvoiceRequest": {
				Name: "InvoiceRequest", Package: "legacy", Domain: "legacy", Qualified: "legacy.InvoiceRequest",
				Fields: []FieldContract{{Key: "old", Type: "string", Required: true}},
			},
		},
	}
	manifest := APISurfaceManifest{
		Actions: []ActionContract{{
			Name: "invoices.upload", Scope: "public", Method: "POST", Path: "/api/invoices/upload",
			ReqTypeName: "InvoiceRequest", ResTypeName: "InvoiceResponse", Multipart: true,
			ValidateMultipartRequest: true, RequestOptional: &required,
		}},
		Types: []core.APISurfaceType{
			{Name: "InvoiceRequest", Fields: []core.FieldContract{
				{Key: "document", Type: "File", Required: true, Multipart: true, PreserveMultipartType: true, Zod: &core.ZodFieldConfig{Base: "file"}},
				{Key: "metadata", Type: "Record<string, unknown>", Required: false},
			}},
			{Name: "InvoiceResponse", Fields: []core.FieldContract{{Key: "id", Type: "string", Required: true}}},
		},
		Client: &core.APIClientConfig{
			Transport: "single-fetcher", ErrorKinds: []string{"BadRequest", "Internal"},
			ScalarAliases:          []core.APIScalarAlias{{Name: "IsoDateTimeString", Type: "string"}},
			EndpointMetadataFields: []string{"name", "reqType"}, FieldMetadataFields: []string{"key", "type"},
			FacadeExportName: "generatedApi", FactoryExportName: "createGeneratedApi",
			EmptyStructSchema: true, PathParameterEncoding: "none",
		},
	}
	if err := ApplyAPISurfaceManifest(report, manifest); err != nil {
		t.Fatalf("ApplyAPISurfaceManifest() error = %v", err)
	}
	if report.APIClientConfig == nil || report.APIClientConfig.Transport != "single-fetcher" || report.APIClientConfig.PathParameterEncoding != "none" {
		t.Fatalf("API client config not preserved: %#v", report.APIClientConfig)
	}
	if len(report.Actions) != 1 || report.Actions[0].RequestOptional == nil || *report.Actions[0].RequestOptional || !report.Actions[0].ValidateMultipartRequest {
		t.Fatalf("explicit action flags not preserved: %#v", report.Actions)
	}
	if _, exists := report.Structs["legacy.InvoiceRequest"]; exists {
		t.Fatal("manifest DTO did not replace the scanned DTO with the same name")
	}
	request, ok := report.Structs["manifest.InvoiceRequest"]
	if !ok || len(request.Fields) != 2 || request.Fields[1].Type != "Record<string, unknown>" {
		t.Fatalf("manifest request DTO not merged: %#v", request)
	}
	if len(report.Actions[0].ReqFields) != 2 || !report.Actions[0].ReqFields[0].Multipart {
		t.Fatalf("request fields were not correlated from manifest DTO: %#v", report.Actions[0].ReqFields)
	}
}

func TestApplyAPISurfaceManifestRejectsUnsafeTypeAndInvalidZodAtomically(t *testing.T) {
	tests := []struct {
		name  string
		types []core.APISurfaceType
		want  string
	}{
		{
			name:  "unsafe type expression",
			types: []core.APISurfaceType{{Name: "Unsafe", Fields: []core.FieldContract{{Key: "value", Type: "Record<string, unknown>; throw"}}}},
			want:  "invalid TypeScript type",
		},
		{
			name:  "unsupported zod base",
			types: []core.APISurfaceType{{Name: "BadSchema", Fields: []core.FieldContract{{Key: "value", Type: "string", Zod: &core.ZodFieldConfig{Base: "execute"}}}}},
			want:  "unsupported Zod base",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := &Report{Actions: []ActionContract{{Name: "existing.route", Scope: "public", Method: "GET", Path: "/existing"}}}
			err := ApplyAPISurfaceManifest(report, APISurfaceManifest{
				Actions: []ActionContract{{Name: "new.route", Scope: "public", Method: "GET", Path: "/new"}},
				Types:   tc.types,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ApplyAPISurfaceManifest() error = %v, want substring %q", err, tc.want)
			}
			if report.APISurfaceManifest || len(report.Actions) != 1 || report.Actions[0].Name != "existing.route" {
				t.Fatalf("invalid manifest changed the report: %#v", report)
			}
		})
	}
}

func TestManifestTypeExpressionAllowsSafeRecordUnknown(t *testing.T) {
	if !validAPISurfaceTypeExpression("Record<string, unknown>") {
		t.Fatal("safe Record<string, unknown> expression was rejected")
	}
	for _, expression := range []string{"Record<string, any>", "string; alert(1)", "Record<string, unknown> |", "Foo<>", "File[];"} {
		if validAPISurfaceTypeExpression(expression) {
			t.Errorf("unsafe or malformed type expression %q was accepted", expression)
		}
	}
}
