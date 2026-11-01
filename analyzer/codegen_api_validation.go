package analyzer

import "strings"

// ValidatePublicAPI reports contracts that the generated public SDK cannot
// type safely. It is intentionally separate from general analysis so generic
// OPS reports and non-SDK workflows keep their existing behavior.
func ValidatePublicAPI(report *Report) []Diagnostic {
	if report == nil {
		return nil
	}

	apiReport := reportWithFlowSchemas(report)
	colliding, tsStructs := analyzeCollisions(apiReport.Structs)
	var diagnostics []Diagnostic
	add := func(action, message, file string) {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: SeverityError,
			Rule:     "sdk/public-contract",
			Message:  action + ": " + message,
			File:     file,
		})
	}

	for _, action := range apiReport.Actions {
		if action.Scope != "public" || action.Path == "" {
			continue
		}
		if action.Streaming || action.Raw {
			continue
		}

		for _, request := range []bool{true, false} {
			label := "response"
			typeName := action.ResTypeName
			if request {
				label = "request"
				typeName = action.ReqTypeName
			}

			// An omitted request on a query route (or an NFlow route with no
			// request schema) is the intentional argument-free form.
			if request && ((action.FlowSchemaDriven && action.ReqSchemaName == "") ||
				(typeName == "" && (action.Method == "GET" || action.Method == "DELETE"))) {
				continue
			}
			if !request && action.FlowSchemaDriven && action.ResSchemaName == "" {
				add(action.Name, "has no response-schema; add an explicit NFlow response schema", "")
				continue
			}

			tsType := resolveActionTypeTS(action, request, tsStructs, apiReport.Structs, colliding)
			if tsType == "void" {
				if hasExplicitEmptyContract(action, request, apiReport.Structs) {
					continue
				}
				add(action.Name, "has no concrete "+label+" contract; define a type or an explicit empty struct", "")
				continue
			}
			if containsUnsupportedPublicType(tsType) {
				add(action.Name, "has an untyped "+label+" contract ("+tsType+"); use a supported concrete type", "")
			}
		}
	}

	// Check reachable DTOs as well as top-level request/response types so an
	// any/interface{} field cannot leak unknown through a nested public type.
	reachable := computeReachableStructs(apiReport, tsStructs, colliding, false)
	seenFields := make(map[string]bool)
	for _, definition := range reachable {
		for _, field := range definition.Fields {
			if !containsUnsupportedPublicType(field.Type) {
				continue
			}
			key := definition.Qualified + "." + field.Key
			if seenFields[key] {
				continue
			}
			seenFields[key] = true
			add(key, "uses unsupported TypeScript type "+field.Type+"; replace it with a concrete JSON type", definition.SourceFile)
		}
	}
	return diagnostics
}

func hasExplicitEmptyContract(action ActionContract, request bool, structs map[string]StructDefinition) bool {
	typeName := action.ResTypeName
	if request {
		typeName = action.ReqTypeName
	}
	if action.FlowSchemaDriven {
		schemaName := action.ResSchemaName
		if request {
			schemaName = action.ReqSchemaName
		}
		if schemaName == "" {
			return request
		}
	}
	if typeName == "struct{}" {
		return true
	}
	if definition, ok := resolveActionStruct(action, request, structs); ok {
		return len(definition.Fields) == 0
	}
	return false
}

func containsUnsupportedPublicType(tsType string) bool {
	for _, token := range strings.FieldsFunc(tsType, func(r rune) bool {
		return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) {
		if token == "unknown" || token == "void" || token == "any" {
			return true
		}
	}
	return false
}
