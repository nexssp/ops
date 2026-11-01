package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestFieldIsRequiredUsesJSONAndValidationTags(t *testing.T) {
	source := "package dto\ntype Request struct {\n" +
		"Required string `json:\"required\"`\n" +
		"OptionalJSON string `json:\"optional_json,omitempty\"`\n" +
		"OptionalValidation string `json:\"optional_validation\" validate:\"omitempty\"`\n" +
		"RequiredOverride string `json:\"required_override,omitempty\" validate:\"required\"`\n" +
		"}\n"
	file, err := parser.ParseFile(token.NewFileSet(), "dto.go", source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	decl := file.Decls[0].(*ast.GenDecl)
	structType := decl.Specs[0].(*ast.TypeSpec).Type.(*ast.StructType)
	fields := (&Analyzer{}).extractStructFields(structType)

	want := map[string]bool{
		"required":            true,
		"optional_json":       false,
		"optional_validation": false,
		"required_override":   true,
	}
	if len(fields) != len(want) {
		t.Fatalf("got %d fields, want %d", len(fields), len(want))
	}
	for _, field := range fields {
		if field.Required != want[field.Key] {
			t.Errorf("field %q required=%v, want %v", field.Key, field.Required, want[field.Key])
		}
	}
}

func TestClusterStoresSkipDomainSpecificListEnvelope(t *testing.T) {
	actions := []ActionContract{
		{Name: "records.list", Scope: "public", Method: "GET", ResTypeName: "RecordList"},
		{Name: "records.create", Scope: "public", Method: "POST", ResTypeName: "Record"},
		{Name: "records.update", Scope: "public", Method: "PUT", ResTypeName: "Record"},
		{Name: "records.delete", Scope: "public", Method: "DELETE", ResTypeName: "DeleteRecordResult"},
	}
	structs := map[string]StructDefinition{
		"RecordList": {Name: "RecordList", Fields: []FieldContract{{Key: "records", Type: "Record[]"}}},
		"Record":     {Name: "Record", Fields: []FieldContract{{Key: "id", Type: "string"}}},
		"DeleteRecordResult": {Name: "DeleteRecordResult", Fields: []FieldContract{
			{Key: "id", Type: "string"}, {Key: "deleted", Type: "boolean"},
		}},
	}
	if got := clusterStores(actions, structs); len(got) != 0 {
		t.Fatalf("resource-specific {records: []Record} envelope should not generate a generic entity store: %#v", got)
	}

	structs["RecordList"] = StructDefinition{Name: "RecordList", Fields: []FieldContract{{Key: "items", Type: "Record[]"}}}
	got := clusterStores(actions, structs)
	if len(got) != 1 || got[0].EntityType != "Record" || got[0].IDField != "id" {
		t.Fatalf("canonical {items: []Agent} envelope should generate a valid entity store, got %#v", got)
	}
}
