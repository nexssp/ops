package analyzer

import (
	"strings"
	"testing"
)

func TestApplyContractManifestExplicitEmptyRequest(t *testing.T) {
	response := StructDefinition{
		Name:      "CreateResponse",
		Package:   "records",
		Domain:    "records",
		Qualified: "records.CreateResponse",
		Fields:    []FieldContract{{Key: "id", Type: "string", Required: true}},
	}
	report := &Report{
		Actions: []ActionContract{{
			Name:        "records.create",
			ResTypeName: "CreateResponse",
			ResType:     TypeRef{Package: "records", Name: "CreateResponse"},
			Method:      "POST",
			Path:        "/records",
			Scope:       "public",
		}},
		Structs: map[string]StructDefinition{response.Qualified: response},
	}
	empty := "struct{}"
	if err := ApplyContractManifest(report, ContractManifest{Actions: []ContractManifestAction{{Name: "records.create", ReqType: &empty}}}); err != nil {
		t.Fatalf("ApplyContractManifest() error = %v", err)
	}

	action := report.Actions[0]
	if action.ReqTypeName != "struct{}" || action.ContractSource != "manifest" {
		t.Fatalf("manifest request was not recorded: %#v", action)
	}
	if len(action.ResFields) != 1 || action.ResFields[0].Key != "id" {
		t.Fatalf("response fields were not re-correlated: %#v", action.ResFields)
	}
	if diagnostics := ValidatePublicAPI(report); len(diagnostics) != 0 {
		t.Fatalf("explicit empty request did not pass public validation: %#v", diagnostics)
	}

	var endpoints string
	for _, file := range GenerateTypeScriptAPI(report) {
		if file.Path == "endpoints.ts" {
			endpoints = string(file.Content)
			break
		}
	}
	if !strings.Contains(endpoints, "recordsCreate: (req?: EmptyStruct): Promise") {
		t.Fatalf("empty request did not preserve the optional object-call shape:\n%s", endpoints)
	}
	if strings.Contains(endpoints, "recordsCreate: (): Promise") {
		t.Fatalf("empty request lost compatibility with callers that pass {}:\n%s", endpoints)
	}
}

func TestApplyContractManifestNormalizesQualifiedEmptyStructPlaceholder(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{{
			Name:        "records.list",
			ReqTypeName: "records.struct{}",
			ResTypeName: "[]string",
			Method:      "GET",
			Path:        "/records",
			Scope:       "public",
		}},
		Structs: map[string]StructDefinition{},
	}
	empty := "struct{}"
	manifest := ContractManifest{Actions: []ContractManifestAction{{Name: "records.list", ReqType: &empty}}}
	if err := ApplyContractManifest(report, manifest); err != nil {
		t.Fatalf("ApplyContractManifest() error = %v", err)
	}
	if got := report.Actions[0].ReqTypeName; got != "struct{}" {
		t.Fatalf("normalized request type = %q, want struct{}", got)
	}
	if diagnostics := ValidatePublicAPI(report); len(diagnostics) != 0 {
		t.Fatalf("normalized empty request did not pass public validation: %#v", diagnostics)
	}
}

func TestApplyContractManifestResolvesQualifiedDTOReference(t *testing.T) {
	request := StructDefinition{
		Name:      "CreateRequest",
		Package:   "requestdto",
		Domain:    "requestdto",
		Qualified: "requestdto.CreateRequest",
		Fields:    []FieldContract{{Key: "title", Type: "string", Required: true}},
	}
	other := StructDefinition{
		Name:      "CreateRequest",
		Package:   "otherdto",
		Domain:    "otherdto",
		Qualified: "otherdto.CreateRequest",
		Fields:    []FieldContract{{Key: "wrong", Type: "string"}},
	}
	report := &Report{
		Actions: []ActionContract{{Name: "records.create", Method: "POST", Path: "/records", Scope: "public"}},
		Structs: map[string]StructDefinition{
			request.Qualified: request,
			other.Qualified:   other,
		},
	}
	requestName := "CreateRequest"
	requestRef := TypeRef{Package: "requestdto", Name: "CreateRequest"}
	responseName := "struct{}"
	manifest := ContractManifest{Actions: []ContractManifestAction{{
		Name:       "records.create",
		ReqType:    &requestName,
		ReqTypeRef: &requestRef,
		ResType:    &responseName,
	}}}
	if err := ApplyContractManifest(report, manifest); err != nil {
		t.Fatalf("ApplyContractManifest() error = %v", err)
	}
	action := report.Actions[0]
	if action.ReqType.Package != "requestdto" || len(action.ReqFields) != 1 || action.ReqFields[0].Key != "title" {
		t.Fatalf("manifest DTO reference resolved incorrectly: %#v", action)
	}
}

func TestApplyContractManifestRejectsUnsafeOrAmbiguousEntriesAtomically(t *testing.T) {
	first := "struct{}"
	cases := []struct {
		name     string
		manifest ContractManifest
		want     string
	}{
		{
			name:     "unknown action",
			manifest: ContractManifest{Actions: []ContractManifestAction{{Name: "records.create", ReqType: &first}, {Name: "missing.action", ReqType: &first}}},
			want:     "unknown action",
		},
		{
			name:     "duplicate manifest action",
			manifest: ContractManifest{Actions: []ContractManifestAction{{Name: "records.create", ReqType: &first}, {Name: "records.create", ReqType: &first}}},
			want:     "duplicate action",
		},
		{
			name:     "inferred type conflict",
			manifest: ContractManifest{Actions: []ContractManifestAction{{Name: "records.create", ReqType: stringPointer("RequestDTO")}}},
			want:     "conflicts with inferred type",
		},
		{
			name:     "Flow schema precedence",
			manifest: ContractManifest{Actions: []ContractManifestAction{{Name: "records.create", ReqType: &first}}},
			want:     "Flow-schema-driven",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action := ActionContract{Name: "records.create", ReqTypeName: "", Method: "POST", Path: "/records", Scope: "public"}
			if tc.name == "inferred type conflict" {
				action.ReqTypeName = "ExistingRequest"
			}
			if tc.name == "Flow schema precedence" {
				action.FlowSchemaDriven = true
			}
			report := &Report{Actions: []ActionContract{action}, Structs: map[string]StructDefinition{}}
			err := ApplyContractManifest(report, tc.manifest)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ApplyContractManifest() error = %v, want substring %q", err, tc.want)
			}
			if report.Actions[0].ReqTypeName != action.ReqTypeName {
				t.Fatalf("failed manifest partially mutated report: before=%q after=%q", action.ReqTypeName, report.Actions[0].ReqTypeName)
			}
		})
	}
}

func TestApplyContractManifestRejectsAmbiguousUnqualifiedDTO(t *testing.T) {
	left := StructDefinition{Name: "RequestDTO", Package: "left", Domain: "left", Qualified: "left.RequestDTO"}
	right := StructDefinition{Name: "RequestDTO", Package: "right", Domain: "right", Qualified: "right.RequestDTO"}
	report := &Report{
		Actions: []ActionContract{{Name: "records.create", Method: "POST", Path: "/records", Scope: "public"}},
		Structs: map[string]StructDefinition{left.Qualified: left, right.Qualified: right},
	}
	request := "RequestDTO"
	err := ApplyContractManifest(report, ContractManifest{Actions: []ContractManifestAction{{Name: "records.create", ReqType: &request}}})
	if err == nil || !strings.Contains(err.Error(), "ambiguous across Go packages") {
		t.Fatalf("ApplyContractManifest() error = %v, want ambiguous DTO error", err)
	}
}

func stringPointer(value string) *string { return &value }
