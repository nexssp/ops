package analyzer

import (
	"sort"
	"strings"

	"github.com/nexssp/ops/analyzer/core"
)

func flowAwareReport(report *Report) *Report {
	if report == nil {
		return nil
	}
	copyReport := *report
	usage := flowSchemaUsage(report)
	copyReport.Structs = flowSchemaStructs(report, usage)
	copyReport.Actions = append([]ActionContract(nil), report.Actions...)
	for i := range copyReport.Actions {
		copyReport.Actions[i] = flowAwareAction(report, copyReport.Actions[i])
	}
	core.CorrelateActionFields(&copyReport)
	return &copyReport
}

func flowSchemaStructs(report *Report, usage map[string]*flowSchemaSides) map[string]StructDefinition {
	structs := make(map[string]StructDefinition, len(report.Structs)+len(report.FlowSchemas))
	for key, definition := range report.Structs {
		structs[key] = definition
	}
	names := make([]string, 0, len(report.FlowSchemas))
	for name := range report.FlowSchemas {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		iQualified := strings.Contains(names[i], ".")
		jQualified := strings.Contains(names[j], ".")
		if iQualified != jQualified {
			return !iQualified
		}
		return names[i] < names[j]
	})
	seenTypes := make(map[string]StructDefinition)
	for _, name := range names {
		definition := report.FlowSchemas[name]
		typeName := flowSchemaTypeName(definition)
		if sides := usage[typeName]; sides != nil {
			definition.Fields = append([]FieldContract(nil), definition.Fields...)
			for i := range definition.Fields {
				field := &definition.Fields[i]
				if sides.request && !sides.response {
					field.Required = core.HasValidatorRule(field.Validation, "required")
				} else if sides.response && !sides.request {
					field.Required = core.HasValidatorRule(field.Validation, "required") || (!core.HasValidatorRule(field.Validation, "omitempty") && !field.JSONOmitEmpty)
				}
			}
		}
		if _, exists := seenTypes[typeName]; exists {
			continue
		}
		seenTypes[typeName] = definition
		structs[definition.Qualified] = definition
	}
	return structs
}

func flowAwareAction(report *Report, action ActionContract) ActionContract {
	if action.FlowSchemaDriven {
		action.ReqTypeName, action.ReqType = flowSchemaActionType(report, action.ReqSchemaName)
		action.ResTypeName, action.ResType = flowSchemaActionType(report, action.ResSchemaName)
	}
	return action
}

func flowSchemaActionType(report *Report, schemaName string) (string, TypeRef) {
	if schemaName == "" {
		return "struct{}", TypeRef{}
	}
	typeName, ref, err := flowSchemaContractType(report.FlowSchemas, schemaName)
	if err == nil {
		return typeName, ref
	}
	return "unknown", TypeRef{}
}

func findFlowSchema(report *Report, name string) (StructDefinition, bool) {
	name = strings.TrimSpace(name)
	for strings.HasPrefix(name, "[]") {
		name = strings.TrimSpace(strings.TrimPrefix(name, "[]"))
	}
	name = strings.TrimPrefix(name, "*")
	if definition, ok := report.FlowSchemas[name]; ok {
		return definition, true
	}
	target := flowSchemaTSName(name)
	for _, definition := range report.FlowSchemas {
		if flowSchemaTypeName(definition) == target {
			return definition, true
		}
	}
	return StructDefinition{}, false
}

func flowSchemaTypeName(definition StructDefinition) string {
	if definition.CustomName != "" {
		return definition.CustomName
	}
	return definition.Name
}

type flowSchemaSides struct {
	request  bool
	response bool
}

type flowSchemaQueueEntry struct {
	typeName string
	request  bool
}

func flowSchemaUsage(report *Report) map[string]*flowSchemaSides {
	byType := make(map[string]StructDefinition, len(report.FlowSchemas))
	for _, definition := range report.FlowSchemas {
		typeName := flowSchemaTypeName(definition)
		if _, exists := byType[typeName]; !exists {
			byType[typeName] = definition
		}
	}
	usage := make(map[string]*flowSchemaSides)
	queue := make([]flowSchemaQueueEntry, 0, len(report.Actions)*2)
	mark := func(definition StructDefinition, request bool) {
		typeName := flowSchemaTypeName(definition)
		sides := usage[typeName]
		if sides == nil {
			sides = &flowSchemaSides{}
			usage[typeName] = sides
		}
		if request {
			if sides.request {
				return
			}
			sides.request = true
		} else {
			if sides.response {
				return
			}
			sides.response = true
		}
		queue = append(queue, flowSchemaQueueEntry{typeName: typeName, request: request})
	}
	for _, action := range report.Actions {
		if !action.FlowSchemaDriven {
			continue
		}
		if definition, ok := findFlowSchema(report, action.ReqSchemaName); ok && action.ReqSchemaName != "" {
			mark(definition, true)
		}
		if definition, ok := findFlowSchema(report, action.ResSchemaName); ok && action.ResSchemaName != "" {
			mark(definition, false)
		}
	}
	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]
		definition, ok := byType[entry.typeName]
		if !ok {
			continue
		}
		for _, field := range definition.Fields {
			nestedName := flowBaseType(field.Type)
			if nested, ok := byType[nestedName]; ok {
				mark(nested, entry.request)
			}
		}
	}
	return usage
}

func flowBaseType(typeName string) string {
	typeName = strings.TrimSpace(strings.TrimSuffix(typeName, "[]"))
	if strings.HasPrefix(typeName, "Record<") {
		if idx := strings.LastIndex(typeName, ", "); idx > 0 && strings.HasSuffix(typeName, ">") {
			typeName = typeName[idx+2 : len(typeName)-1]
		}
	}
	if idx := strings.LastIndexByte(typeName, '.'); idx >= 0 {
		typeName = typeName[idx+1:]
	}
	return strings.TrimSpace(typeName)
}

func correlateFlowActions(report *Report) {
	if report == nil {
		return
	}
	structs := flowSchemaStructs(report, flowSchemaUsage(report))
	report.Structs = structs
	report.Stats.TotalStructs = uniqueStructCount(structs)
	for i := range report.Actions {
		correlateFlowAction(report, structs, &report.Actions[i])
	}
}

func uniqueStructCount(structs map[string]StructDefinition) int {
	qualified := make(map[string]struct{}, len(structs))
	for key, definition := range structs {
		name := definition.Qualified
		if name == "" {
			name = key
		}
		qualified[name] = struct{}{}
	}
	return len(qualified)
}

func correlateFlowAction(report *Report, structs map[string]StructDefinition, action *ActionContract) {
	correlationReport := Report{
		Actions: []ActionContract{flowAwareAction(report, *action)},
		Structs: structs,
	}
	core.CorrelateActionFields(&correlationReport)
	correlated := correlationReport.Actions[0]
	action.ReqFields = correlated.ReqFields
	action.ResFields = correlated.ResFields
	action.Multipart = correlated.Multipart
	action.BlobResponse = correlated.BlobResponse
}

func validateFlowPublicAPI(report *Report) []Diagnostic {
	if report == nil {
		return nil
	}
	diagnostics := core.ValidatePublicAPI(flowAwareReport(report))
	for _, action := range report.Actions {
		if action.Scope != "public" || action.Path == "" || action.Streaming || action.Raw || !action.FlowSchemaDriven || action.ResSchemaName != "" {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Severity: SeverityError,
			Rule:     "sdk/public-contract",
			Message:  action.Name + ": has no response-schema; add an explicit NFlow response schema",
		})
	}
	return diagnostics
}
