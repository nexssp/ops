package analyzer

import (
	"testing"

	"github.com/nexssp/ops/analyzer/core"
)

func TestFlowSchemaActionTypeUsesQualifiedSchemaReference(t *testing.T) {
	report := &Report{
		FlowSchemas: map[string]StructDefinition{
			"billing.PublicDetail": {
				Name:      "PublicDetail",
				Package:   "nflow",
				Domain:    "nflow",
				Qualified: "nflow.billing.PublicDetail",
				Fields:    []FieldContract{{Name: "InvoiceID", Key: "invoice_id", Type: "string"}},
			},
			"support.PublicDetail": {
				Name:      "PublicDetail",
				Package:   "nflow",
				Domain:    "nflow",
				Qualified: "nflow.support.PublicDetail",
				Fields:    []FieldContract{{Name: "TicketID", Key: "ticket_id", Type: "string"}},
			},
		},
	}

	typeName, ref := flowSchemaActionType(report, "support.PublicDetail")
	if typeName != "PublicDetail" || ref.Package != "nflow" || ref.Name != "support.PublicDetail" {
		t.Fatalf("Flow schema reference = %q, %+v; want PublicDetail and nflow.support.PublicDetail", typeName, ref)
	}
	flowAction := flowAwareAction(report, ActionContract{FlowSchemaDriven: true, ReqSchemaName: "support.PublicDetail"})
	if !flowAction.FlowSchemaDriven || flowAction.ReqType.Name != "support.PublicDetail" {
		t.Fatalf("Flow-aware action copy lost schema semantics: %+v", flowAction)
	}

	correlationReport := &Report{
		Actions: []ActionContract{{Name: "api.read", ReqTypeName: typeName, ReqType: ref}},
		Structs: map[string]StructDefinition{
			"nflow.billing.PublicDetail": report.FlowSchemas["billing.PublicDetail"],
			"nflow.support.PublicDetail": report.FlowSchemas["support.PublicDetail"],
		},
	}
	core.CorrelateActionFields(correlationReport)
	fields := correlationReport.Actions[0].ReqFields
	if len(fields) != 1 || fields[0].Key != "ticket_id" {
		t.Fatalf("correlated request fields = %+v, want the support schema's ticket_id field", fields)
	}
}
