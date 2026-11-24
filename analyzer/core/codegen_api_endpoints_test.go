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

func TestMultipartEndpointOmitsAbsentOptionalFile(t *testing.T) {
	request := StructDefinition{
		Name: "UploadRequest", Package: "uploads", Domain: "uploads", Qualified: "uploads.UploadRequest",
		Fields: []FieldContract{
			{Name: "Certificate", Key: "certificate", Type: "File", Required: true, Multipart: true},
			{Name: "PrivateKey", Key: "private_key", Type: "File", Multipart: true},
			{Name: "Password", Key: "password", Type: "string", Required: true},
		},
	}
	response := StructDefinition{
		Name: "UploadResponse", Package: "uploads", Domain: "uploads", Qualified: "uploads.UploadResponse",
		Fields: []FieldContract{{Name: "ID", Key: "id", Type: "string", Required: true}},
	}
	raw := map[string]StructDefinition{
		"uploads.UploadRequest":  request,
		"uploads.UploadResponse": response,
	}
	ts := map[string]StructDefinition{
		"UploadRequest":  request,
		"UploadResponse": response,
	}
	var out bytes.Buffer
	emitEndpointMethod(&out, ActionContract{
		Name: "uploads.create", Scope: "public", Method: "POST", Path: "/api/uploads",
		ReqTypeName: "UploadRequest", ResTypeName: "UploadResponse", Multipart: true,
	}, ts, raw, map[string]bool{})
	generated := out.String()
	for _, want := range []string{
		`fd.append("certificate", req.certificate as unknown as Blob);`,
		`if (req.private_key != null) fd.append("private_key", req.private_key as unknown as Blob);`,
	} {
		if !strings.Contains(generated, want) {
			t.Errorf("multipart endpoint is missing %q:\n%s", want, generated)
		}
	}
	if strings.Contains(generated, `
    fd.append("private_key"`) {
		t.Errorf("optional file was emitted unconditionally:\n%s", generated)
	}
}
