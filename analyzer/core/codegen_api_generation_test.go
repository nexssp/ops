package core

import (
	"strings"
	"testing"
	"time"
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

func TestGenerateEndpointsFileEmitsInspectorMetadata(t *testing.T) {
	report := &Report{Actions: []ActionContract{{
		Name:             "users.create",
		Description:      "Create a user",
		ReqTypeName:      "CreateUserReq",
		ResTypeName:      "User",
		Method:           "POST",
		Path:             "/api/users",
		Scope:            "public",
		RequiresAuth:     true,
		Roles:            []string{"admin"},
		Permissions:      []string{"users:create"},
		Features:         []string{"billing"},
		Tags:             []string{"users"},
		RetryMax:         2,
		Timeout:          1500 * time.Millisecond,
		ConcurrencyLimit: 4,
		RateLimit:        "per-minute",
		CacheTTL:         30 * time.Second,
		Idempotent:       true,
		ReqFields: []FieldContract{{
			Name: "Email", Key: "email", Type: "string", Required: true,
			Validation: "required,email", RawTags: `json:"email" validate:"required,email"`,
		}},
		ResFields: []FieldContract{{Name: "ID", Key: "id", Type: "string", Required: true}},
	}}}
	generated := string(generateEndpointsFile(report, map[string]StructDefinition{}, map[string]bool{}))
	for _, want := range []string{
		`usersCreate: {"name":"users.create","methodName":"usersCreate"`,
		`"description":"Create a user"`,
		`"reqType":"CreateUserReq","resType":"User"`,
		`"requiresAuth":true,"requiredRoles":["admin"],"requiredPermissions":["users:create"],"requiredFeatures":["billing"],"tags":["users"]`,
		`"fields":[{"key":"email","type":"string","required":true,"label":"Email","validation":"required,email","raw_tags":"json:\"email\" validate:\"required,email\""}]`,
		`"resFields":[{"key":"id","type":"string","required":true,"label":"ID"}]`,
		`"retryMax":2,"timeoutMs":1500,"concurrencyLimit":4,"rateLimit":"per-minute","cacheTtlMs":30000,"idempotent":true`,
	} {
		if !strings.Contains(generated, want) {
			t.Errorf("generated endpoint metadata is missing %q:\n%s", want, generated)
		}
	}
}
