package core

import (
	"strings"
	"testing"
)

func TestGenerateTypeScriptAPIEmitsFlatFilesAndTypedArrayResponses(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{{
			Name:        "widgets.list",
			Scope:       "public",
			Method:      "GET",
			Path:        "/api/widgets",
			ResTypeName: "[]Widget",
			ResType:     TypeRef{Domain: "nflow", Package: "nflow", Name: "Widget"},
		}},
		Structs: map[string]StructDefinition{
			"nflow.Widget": {
				Name: "Widget", Package: "nflow", Domain: "nflow", Qualified: "nflow.Widget",
				Fields: []FieldContract{{Name: "Name", Key: "name", Type: "string", Required: true}},
			},
		},
	}

	files := GenerateTypeScriptAPI(report)
	if len(files) != 4 {
		t.Fatalf("generated %d API files, want 4", len(files))
	}
	contents := make(map[string]string, len(files))
	for _, file := range files {
		contents[file.Path] = string(file.Content)
	}
	for _, name := range []string{"client.ts", "types.ts", "endpoints.ts", "index.ts"} {
		if _, ok := contents[name]; !ok {
			t.Errorf("missing flat API output %q", name)
		}
	}
	for _, name := range []string{"_gen/api/endpoints.ts", "stores/index.ts", "api.ts"} {
		if _, ok := contents[name]; ok {
			t.Errorf("API-only output unexpectedly contains %q", name)
		}
	}
	if !strings.Contains(contents["endpoints.ts"], "widgetsList: (): Promise<Types.Widget[]> ") {
		t.Errorf("DTO array response was not emitted with a Types reference:\n%s", contents["endpoints.ts"])
	}
	if !strings.Contains(contents["types.ts"], "export interface Widget {") {
		t.Errorf("array element DTO was not included in types.ts:\n%s", contents["types.ts"])
	}
}
