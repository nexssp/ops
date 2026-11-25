package core

import (
	"strings"
	"testing"
)

func TestGenerateEmptyStructRequestsPreservesOptionalObjectCallShape(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{
			{
				Name:        "records.list",
				ReqTypeName: "struct{}",
				ResTypeName: "[]string",
				Method:      "GET",
				Path:        "/records",
				Scope:       "public",
			},
			{
				Name:        "records.create",
				ReqTypeName: "struct{}",
				ResTypeName: "string",
				Method:      "POST",
				Path:        "/records",
				Scope:       "public",
			},
		},
		Structs: map[string]StructDefinition{},
	}

	var endpoints, client string
	for _, file := range GenerateTypeScriptAPI(report) {
		switch file.Path {
		case "endpoints.ts":
			endpoints = string(file.Content)
		case "client.ts":
			client = string(file.Content)
		}
	}
	for _, expected := range []string{
		`reqType":"EmptyStruct"`,
		`recordsList: (req?: EmptyStruct): Promise<string[]>`,
		`recordsCreate: (req?: EmptyStruct): Promise<string>`,
		`body: req`,
	} {
		if !strings.Contains(endpoints, expected) {
			t.Fatalf("endpoint output missing %q:\n%s", expected, endpoints)
		}
	}
	if !strings.Contains(client, "export type EmptyStruct = Record<string, never>;") {
		t.Fatalf("client output missing the EmptyStruct type:\n%s", client)
	}
	if !strings.Contains(endpoints, "import type { EmptyStruct } from './client';") {
		t.Fatalf("endpoint output missing the EmptyStruct type import:\n%s", endpoints)
	}
}

func TestGenerateEmptyStructRequestWithPathParameterKeepsRequiredPathArgs(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{{
			Name:        "records.delete",
			ReqTypeName: "struct{}",
			ResTypeName: "string",
			Method:      "DELETE",
			Path:        "/records/{id}",
			Scope:       "public",
		}},
		Structs: map[string]StructDefinition{},
	}

	var endpoints string
	for _, file := range GenerateTypeScriptAPI(report) {
		if file.Path == "endpoints.ts" {
			endpoints = string(file.Content)
			break
		}
	}
	if !strings.Contains(endpoints, `recordsDelete: (req: { id: string | number }): Promise<string>`) {
		t.Fatalf("empty request lost its required path parameter:\n%s", endpoints)
	}
}
