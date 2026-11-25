package analyzer

import (
	"strings"
	"testing"
)

func TestApplyAPISurfaceManifestProjectsRoutesAndPreservesMetadata(t *testing.T) {
	report := &Report{
		Actions: []ActionContract{
			{Name: "metrics.prometheus", Scope: "public", Method: "GET", Path: "/obs/metrics"},
			{Name: "metrics.system", Scope: "system", Method: "GET", Path: "/obs/metrics"},
			{Name: "admin.backup", Scope: "internal", Method: "POST", Path: "/admin/backup"},
		},
		Diagnostics: []Diagnostic{
			{Severity: SeverityError, Rule: "routes/duplicate", Message: "duplicate route from scanned server actions"},
			{Severity: SeverityWarning, Rule: "analysis/example", Message: "unrelated warning remains visible"},
			{Severity: SeverityError, Rule: "routes/conflict-go-dsl", Action: "admin.backup", Message: "unselected route conflict"},
			{Severity: SeverityWarning, Rule: "analysis/selected", Action: "records.list", Message: "selected action warning remains visible"},
		},
		Stats: ReportStats{TotalActions: 3, PublicActions: 1, InternalActions: 1, SystemActions: 1, ErrorCount: 1, WarningCount: 1},
	}
	manifest := APISurfaceManifest{Actions: []ActionContract{
		{
			Name: "records.list", SDKMethodName: "recordsList", Description: "List public records",
			Method: "GET", Path: "/api/records", Scope: "public", ReqTypeName: "struct{}", ResTypeName: "[]string",
			Node: "records", Roles: []string{"reader"}, SuccessStatus: 200, Deduplicated: true, Coalesced: true,
			ReqFields: []FieldContract{{Name: "Limit", Key: "limit", Type: "number", Required: false}},
		},
		{
			Name: "reports.download", SDKMethodName: "downloadReport", Description: "Download a report",
			Method: "GET", Path: "/api/reports/{id}/pdf", Scope: "public", ReqTypeName: "struct{}", ResTypeName: "[]byte",
			Raw: true,
		},
		{
			Name: "reports.stream", SDKMethodName: "streamReport", Description: "Stream a report",
			Method: "GET", Path: "/api/reports/{id}/stream", Scope: "public", ReqTypeName: "struct{}", ResTypeName: "string",
			Raw: true, Streaming: true,
		},
	}}

	if err := ApplyAPISurfaceManifest(report, manifest); err != nil {
		t.Fatalf("ApplyAPISurfaceManifest() error = %v", err)
	}
	if !report.APISurfaceManifest {
		t.Fatal("manifest application did not mark the report as an authoritative API surface")
	}
	if len(report.Actions) != 3 || report.Actions[0].Name != "records.list" || report.Actions[1].Name != "reports.download" || report.Actions[2].Name != "reports.stream" {
		t.Fatalf("projected actions = %#v, want only the three manifest routes", report.Actions)
	}
	if report.Stats.TotalActions != 3 || report.Stats.PublicActions != 3 || report.Stats.InternalActions != 0 || report.Stats.SystemActions != 0 {
		t.Fatalf("projected action stats = %#v", report.Stats)
	}
	if report.Stats.ErrorCount != 0 || report.Stats.WarningCount != 2 || len(report.Diagnostics) != 2 || report.Diagnostics[0].Rule != "analysis/example" || report.Diagnostics[1].Rule != "analysis/selected" {
		t.Fatalf("diagnostics were not refreshed correctly: stats=%#v diagnostics=%#v", report.Stats, report.Diagnostics)
	}
	if got := report.Actions[0].ReqFields; len(got) != 1 || got[0].Key != "limit" {
		t.Fatalf("explicit request metadata was not preserved: %#v", got)
	}

	files := GenerateTypeScriptAPI(report)
	var endpoints string
	for _, file := range files {
		if file.Path == "endpoints.ts" {
			endpoints = string(file.Content)
		}
	}
	for _, want := range []string{
		`recordsList: {"name":"records.list","methodName":"recordsList"`,
		`"node":"records"`,
		`"successStatus":200`,
		`"deduplicated":true,"coalesced":true`,
		`downloadReport: {"name":"reports.download","methodName":"downloadReport"`,
		`"raw":true,"blob":true`,
		`streamReport: {"name":"reports.stream","methodName":"streamReport"`,
		`recordsList: (req?: EmptyStruct): Promise<string[]>`,
	} {
		if !strings.Contains(endpoints, want) {
			t.Errorf("generated API endpoints are missing %q:\n%s", want, endpoints)
		}
	}
	apiMethods := strings.SplitN(endpoints, "export const api = {", 2)
	if len(apiMethods) != 2 {
		t.Fatalf("generated endpoints have no api object:\n%s", endpoints)
	}
	if !strings.Contains(apiMethods[1], "downloadReport: (req: { id: string | number }): Promise<Blob>") ||
		!strings.Contains(apiMethods[1], "_fetcherBlob({ method: 'GET', path: resolvePath('/api/reports/{id}/pdf', req) })") {
		t.Errorf("raw blob action did not become a typed, path-aware SDK method:\n%s", apiMethods[1])
	}
	if strings.Contains(apiMethods[1], "streamReport") || strings.Contains(apiMethods[1], "adminBackup") || strings.Contains(apiMethods[1], "metricsSystem") {
		t.Errorf("streaming or non-surface actions unexpectedly became typed SDK methods:\n%s", apiMethods[1])
	}
}

func TestApplyAPISurfaceManifestRejectsDuplicateRoutesAndMethodNamesAtomically(t *testing.T) {
	tests := []struct {
		name     string
		actions  []ActionContract
		wantPart string
	}{
		{
			name: "duplicate route",
			actions: []ActionContract{
				{Name: "records.list", SDKMethodName: "recordsList", Method: "GET", Path: "/records"},
				{Name: "records.all", SDKMethodName: "recordsAll", Method: "GET", Path: "/records"},
			},
			wantPart: `route "GET /records" is shared`,
		},
		{
			name: "duplicate method name",
			actions: []ActionContract{
				{Name: "records.list", SDKMethodName: "records", Method: "GET", Path: "/records"},
				{Name: "records.all", SDKMethodName: "records", Method: "GET", Path: "/all-records"},
			},
			wantPart: `SDK method name "records" is shared`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			report := &Report{Actions: []ActionContract{{Name: "existing.route", Scope: "public", Method: "GET", Path: "/existing"}}}
			if err := ApplyAPISurfaceManifest(report, APISurfaceManifest{Actions: tc.actions}); err == nil || !strings.Contains(err.Error(), tc.wantPart) {
				t.Fatalf("ApplyAPISurfaceManifest() error = %v, want substring %q", err, tc.wantPart)
			}
			if report.APISurfaceManifest || len(report.Actions) != 1 || report.Actions[0].Name != "existing.route" {
				t.Fatalf("failed manifest changed the report: %#v", report)
			}
		})
	}
}
