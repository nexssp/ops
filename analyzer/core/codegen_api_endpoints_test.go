package core

import (
	"bytes"
	"strings"
	"testing"
)

func TestEndpointQueryArgumentOptionalityFollowsDTOFields(t *testing.T) {
	deleteRequest := StructDefinition{
		Name: "DeleteRecordRequest", Package: "records", Domain: "records", Qualified: "records.DeleteRecordRequest",
		Fields: []FieldContract{{Name: "ID", Key: "id", Type: "string", Required: true}},
	}
	deleteResult := StructDefinition{
		Name: "DeleteRecordResult", Package: "records", Domain: "records", Qualified: "records.DeleteRecordResult",
		Fields: []FieldContract{{Name: "ID", Key: "id", Type: "string", Required: true}},
	}
	listRequest := StructDefinition{
		Name: "RecordListRequest", Package: "records", Domain: "records", Qualified: "records.RecordListRequest",
		Fields: []FieldContract{{Name: "Page", Key: "page", Type: "number", Required: false}},
	}
	listResult := StructDefinition{
		Name: "RecordList", Package: "records", Domain: "records", Qualified: "records.RecordList",
		Fields: []FieldContract{{Name: "Total", Key: "total", Type: "number", Required: true}},
	}
	raw := map[string]StructDefinition{
		"records.DeleteRecordRequest": deleteRequest,
		"records.DeleteRecordResult":  deleteResult,
		"records.RecordListRequest":   listRequest,
		"records.RecordList":          listResult,
	}
	ts := map[string]StructDefinition{
		"DeleteRecordRequest": deleteRequest,
		"DeleteRecordResult":  deleteResult,
		"RecordListRequest":   listRequest,
		"RecordList":          listResult,
	}
	colliding := map[string]bool{}

	var out bytes.Buffer
	emitEndpointMethod(&out, ActionContract{
		Name: "records.delete", Scope: "public", Method: "DELETE", Path: "/api/records",
		ReqTypeName: "DeleteRecordRequest", ResTypeName: "DeleteRecordResult",
		ReqFields: []FieldContract{{Key: "id", Type: "string", Required: true}},
	}, ts, raw, colliding)
	emitEndpointMethod(&out, ActionContract{
		Name: "records.list", Scope: "public", Method: "GET", Path: "/api/records",
		ReqTypeName: "RecordListRequest", ResTypeName: "RecordList",
		ReqFields: []FieldContract{{Key: "page", Type: "number", Required: false}},
	}, ts, raw, colliding)

	generated := out.String()
	if !strings.Contains(generated, "recordsDelete: (req: Types.DeleteRecordRequest)") {
		t.Fatalf("required DELETE query DTO was made optional:\n%s", generated)
	}
	if strings.Contains(generated, "recordsDelete: (req?: Types.DeleteRecordRequest)") {
		t.Fatalf("required DELETE query DTO was made optional:\n%s", generated)
	}
	if !strings.Contains(generated, "recordsList: (req?: Types.RecordListRequest)") {
		t.Fatalf("all-optional GET filter DTO should remain optional:\n%s", generated)
	}
}
