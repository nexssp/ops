package core

import (
	"strings"
	"testing"
)

func TestAPIManifestEmitsUnreferencedManifestTypesOnly(t *testing.T) {
	report := &Report{
		APISurfaceManifest: true,
		Structs: map[string]StructDefinition{
			"manifest.AdminUserDTO": {
				Name: "AdminUserDTO", Package: "manifest", Qualified: "manifest.AdminUserDTO",
				SourceFile: "api-surface.manifest.json", IsExposed: true,
				Fields: []FieldContract{{Key: "email", Type: "string", Required: true}},
			},
			"scanned.InternalDTO": {
				Name: "InternalDTO", Package: "scanned", Qualified: "scanned.InternalDTO",
				SourceFile: "internal.go", IsExposed: true,
				Fields: []FieldContract{{Key: "secret", Type: "string", Required: true}},
			},
		},
	}
	var generated string
	for _, file := range GenerateTypeScriptAPI(report) {
		if file.Path == "types.ts" {
			generated = string(file.Content)
			break
		}
	}
	if !strings.Contains(generated, "export interface AdminUserDTO") {
		t.Fatalf("unreferenced manifest DTO was omitted from API output:\n%s", generated)
	}
	if strings.Contains(generated, "export interface InternalDTO") {
		t.Fatalf("unrelated exposed scanned DTO leaked into API output:\n%s", generated)
	}
}
