package analyzer

import (
	"strings"
	"testing"
)

func TestAPISDKIncludesRouteDTOsAndOmitsUnrelatedExposedDTOs(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{{
			Name:        "widgets.create",
			Scope:       "public",
			Method:      "POST",
			Path:        "/api/widgets",
			ReqTypeName: "CreateWidgetRequest",
			ResTypeName: "Widget",
			ReqFields:   []FieldContract{{Name: "Name", Key: "name", Type: "string", Required: true}},
		}},
		Structs: map[string]StructDefinition{
			"widgets.CreateWidgetRequest": {
				Name: "CreateWidgetRequest", Package: "widgets", Domain: "widgets", Qualified: "widgets.CreateWidgetRequest",
				Fields: []FieldContract{{Name: "Name", Key: "name", Type: "string", Required: true}},
			},
			"widgets.Widget": {
				Name: "Widget", Package: "widgets", Domain: "widgets", Qualified: "widgets.Widget",
				Fields: []FieldContract{{Name: "ID", Key: "id", Type: "string", Required: true}},
			},
			"metadata.RegistryInfo": {
				Name: "RegistryInfo", Package: "metadata", Domain: "metadata", Qualified: "metadata.RegistryInfo", IsExposed: true,
				Fields: []FieldContract{{Name: "Mode", Key: "mode", Type: "string"}},
			},
		},
	}

	var apiTypes string
	for _, file := range GenerateTypeScriptAPISDK(report) {
		if file.Path == "_gen/api/types.ts" {
			apiTypes = string(file.Content)
			break
		}
	}
	if apiTypes == "" {
		t.Fatal("API-only SDK did not emit _gen/api/types.ts")
	}
	if !strings.Contains(apiTypes, "export interface CreateWidgetRequest") || !strings.Contains(apiTypes, "export interface Widget {") {
		t.Fatalf("API-only SDK omitted route DTOs:\n%s", apiTypes)
	}
	if strings.Contains(apiTypes, "export interface RegistryInfo {") {
		t.Fatalf("API-only SDK included unrelated convention-exposed DTO RegistryInfo:\n%s", apiTypes)
	}

	var fullTypes string
	for _, file := range GenerateTypeScriptSDK(report) {
		if file.Path == "_gen/api/types.ts" {
			fullTypes = string(file.Content)
			break
		}
	}
	if !strings.Contains(fullTypes, "export interface RegistryInfo {") {
		t.Fatal("full SDK mode should preserve convention-exposed DTOs")
	}
}
