package core

import (
	"strings"
	"testing"
)

func TestValidatePublicAPIRejectsUntypedReachableFields(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{{
			Name: "records.create", Scope: "public", Method: "POST", Path: "/records",
			ReqTypeName: "CreateRequest", ResTypeName: "CreateResponse",
		}},
		Structs: map[string]StructDefinition{
			"records.CreateRequest": {
				Name: "CreateRequest", Package: "records", Qualified: "records.CreateRequest", SourceFile: "records/types.go",
				Fields: []FieldContract{{Key: "metadata", Type: "Record<string, unknown>"}},
			},
			"records.CreateResponse": {
				Name: "CreateResponse", Package: "records", Qualified: "records.CreateResponse",
				Fields: []FieldContract{{Key: "id", Type: "string"}},
			},
		},
	}

	diagnostics := ValidatePublicAPI(report)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "records.CreateRequest.metadata") {
		t.Fatalf("diagnostics = %+v; want one error for the nested unknown field", diagnostics)
	}
	if diagnostics[0].Rule != "sdk/public-contract" || diagnostics[0].File != "records/types.go" {
		t.Fatalf("diagnostic = %+v; want public-contract rule and source file", diagnostics[0])
	}
}

func TestValidatePublicAPIRejectsMissingResponseEvenWhenForced(t *testing.T) {
	// Force is deliberately not an input to this validator: it cannot make an
	// absent public response contract safe to publish.
	report := &Report{Actions: []ActionContract{{
		Name: "records.list", Scope: "public", Method: "GET", Path: "/records",
		ReqTypeName: "", ResTypeName: "",
	}}, Structs: map[string]StructDefinition{}}

	diagnostics := ValidatePublicAPI(report)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "untyped response contract") {
		t.Fatalf("diagnostics = %+v; want a non-bypassable response contract error", diagnostics)
	}
}

func TestValidatePublicAPIAllowsArgumentFreeQueryRequest(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{{
			Name: "records.list", Scope: "public", Method: "GET", Path: "/records",
			ReqTypeName: "", ResTypeName: "ListResponse",
		}},
		Structs: map[string]StructDefinition{
			"records.ListResponse": {
				Name: "ListResponse", Package: "records", Qualified: "records.ListResponse",
				Fields: []FieldContract{{Key: "items", Type: "string[]"}},
			},
		},
	}

	if diagnostics := ValidatePublicAPI(report); len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics for a bodyless GET: %+v", diagnostics)
	}
}

func TestValidatePublicAPIStillChecksRawBlobRequestDTO(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{{
			Name: "reports.download", Scope: "public", Method: "GET", Path: "/reports/{id}/pdf",
			ReqTypeName: "DownloadRequest", ResTypeName: "[]byte", Raw: true, BlobResponse: true,
		}},
		Structs: map[string]StructDefinition{
			"reports.DownloadRequest": {
				Name: "DownloadRequest", Package: "reports", Qualified: "reports.DownloadRequest",
				Fields: []FieldContract{{Key: "filter", Type: "Record<string, unknown>"}},
			},
		},
	}

	diagnostics := ValidatePublicAPI(report)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "DownloadRequest.filter") {
		t.Fatalf("diagnostics = %+v; want the raw blob request's unsupported field to be rejected", diagnostics)
	}
}
