package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/nexssp/flow/compiler"
	flowschema "github.com/nexssp/flow/extensions/schema"

	"github.com/nexssp/ops/analyzer/core"
)

// flowSchemaDefinitions converts the schema values produced by Flow's public
// directive APIs into OPS DTOs. It intentionally does not inspect Go ASTs or
// re-parse @schema source text.
func flowSchemaDefinitions(meta map[string]any, file string) (map[string]StructDefinition, error) {
	schemas := flowschema.VisibleSchemasFromMeta(meta)
	known := make(map[string]bool, len(schemas))
	generatedNames := make(map[string]string, len(schemas))
	for _, schema := range schemas {
		known[schema.Name] = true
		tsName := flowSchemaTSName(schema.Name)
		if prior, exists := generatedNames[tsName]; exists && prior != schema.Name {
			return nil, fmt.Errorf("Flow schemas %q and %q both map to TypeScript type %q", prior, schema.Name, tsName)
		}
		generatedNames[tsName] = schema.Name
	}

	definitions := make(map[string]StructDefinition, len(schemas))
	for _, schema := range schemas {
		fields := make([]FieldContract, 0, len(schema.Fields))
		for _, field := range schema.Fields {
			tsType, nullable, err := flowSchemaType(field.Type, known)
			if err != nil {
				line := field.Position.Line
				return nil, fmt.Errorf("@schema %s field %s at line %d: %w", schema.Name, field.Name, line, err)
			}
			if field.JSONName == "" {
				return nil, fmt.Errorf("@schema %s field %s has no JSON name", schema.Name, field.Name)
			}
			validation := field.Tags["validate"]
			fields = append(fields, FieldContract{
				Name:          field.Name,
				Key:           field.JSONName,
				Type:          tsType,
				Required:      hasFlowValidatorRule(validation, "required"),
				Nullable:      nullable,
				JSONOmitEmpty: hasJSONTagOption(field.Tags["json"], "omitempty"),
				Validation:    validation,
				RawTags:       flowSchemaRawTags(field.Tags),
			})
		}
		tsName := flowSchemaTSName(schema.Name)
		definitions[schema.Name] = StructDefinition{
			Name:       tsName,
			Package:    "nflow",
			Domain:     "nflow",
			Qualified:  "nflow." + schema.Name,
			SourceFile: file,
			Fields:     fields,
			CustomName: tsName,
		}
	}
	return definitions, nil
}

func flowSchemaRawTags(tags map[string]string) string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(key)
		b.WriteByte(':')
		b.WriteString(strconv.Quote(tags[key]))
	}
	return b.String()
}

func hasFlowValidatorRule(raw, expected string) bool {
	return core.HasValidatorRule(raw, expected)
}

func hasJSONTagOption(raw, expected string) bool {
	parts := strings.Split(raw, ",")
	for _, option := range parts[1:] {
		if strings.TrimSpace(option) == expected {
			return true
		}
	}
	return false
}

func flowSchemaTSName(name string) string {
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		name = name[dot+1:]
	}
	return toPascalCase(name)
}

// flowSchemaType maps the Flow schema type grammar into a closed TypeScript
// type set. Unsupported or unresolved types are errors; they never widen to
// unknown/any.
func flowSchemaType(raw string, knownSchemas map[string]bool) (string, bool, error) {
	t := strings.TrimSpace(raw)
	nullable := false
	for strings.HasPrefix(t, "*") {
		nullable = true
		t = strings.TrimSpace(strings.TrimPrefix(t, "*"))
	}
	if t == "" {
		return "", nullable, fmt.Errorf("empty Flow schema type")
	}
	if strings.HasPrefix(t, "[]") {
		elemType, elemNullable, err := flowSchemaType(strings.TrimSpace(strings.TrimPrefix(t, "[]")), knownSchemas)
		if err != nil {
			return "", nullable, err
		}
		if elemNullable {
			elemType = "(" + elemType + " | null)"
		}
		return elemType + "[]", nullable, nil
	}
	if strings.HasPrefix(t, "map[") {
		close := strings.IndexByte(t, ']')
		if close < 0 || close == len(t)-1 {
			return "", nullable, fmt.Errorf("unsupported map type %q", raw)
		}
		keyType := strings.TrimSpace(t[len("map["):close])
		if keyType != "string" {
			return "", nullable, fmt.Errorf("map key type %q is not JSON-object compatible", keyType)
		}
		valueType, valueNullable, err := flowSchemaType(strings.TrimSpace(t[close+1:]), knownSchemas)
		if err != nil {
			return "", nullable, err
		}
		if valueNullable {
			valueType += " | null"
		}
		return "Record<string, " + valueType + ">", nullable, nil
	}
	switch t {
	case "string":
		return "string", nullable, nil
	case "bool":
		return "boolean", nullable, nil
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "duration", "time.Duration":
		return "number", nullable, nil
	case "any", "interface{}":
		return "", nullable, fmt.Errorf("untyped schema value %q is not allowed in generated public types", t)
	}
	if resolved, err := resolveFlowSchemaName(t, knownSchemas); err != nil {
		return "", nullable, err
	} else if resolved != "" {
		return flowSchemaTSName(resolved), nullable, nil
	}
	return "", nullable, fmt.Errorf("type %q is neither a supported scalar nor a declared Flow schema", t)
}

func resolveFlowSchemaName(name string, knownSchemas map[string]bool) (string, error) {
	if knownSchemas[name] {
		return name, nil
	}
	var match string
	for candidate := range knownSchemas {
		if dot := strings.LastIndexByte(candidate, '.'); dot >= 0 && candidate[dot+1:] == name {
			if match != "" && match != candidate {
				return "", fmt.Errorf("schema type %q is ambiguous across included modules %q and %q", name, match, candidate)
			}
			match = candidate
		}
	}
	return match, nil
}

func (a *flowAnalyzer) addFlowSchemas(meta map[string]any, file string) map[string]StructDefinition {
	definitions, err := flowSchemaDefinitions(meta, file)
	if err != nil {
		a.diagnostics = append(a.diagnostics, Diagnostic{
			Severity: SeverityError,
			Rule:     "nflow/schema-type",
			File:     file,
			Message:  err.Error(),
		})
		return nil
	}
	names := make([]string, 0, len(definitions))
	for name := range definitions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		definition := definitions[name]
		if prior, exists := a.flowSchemas[name]; exists && !sameSchemaDefinition(prior, definition) {
			a.diagnostics = append(a.diagnostics, Diagnostic{
				Severity: SeverityError,
				Rule:     "nflow/schema-duplicate",
				File:     file,
				Message:  fmt.Sprintf("Flow schema %q has conflicting definitions in %s and %s", name, prior.SourceFile, file),
			})
			continue
		}
		nameCollision := false
		for priorName, prior := range a.flowSchemas {
			if priorName != name && prior.CustomName == definition.CustomName && !sameSchemaDefinition(prior, definition) {
				a.diagnostics = append(a.diagnostics, Diagnostic{
					Severity: SeverityError,
					Rule:     "nflow/schema-name-collision",
					File:     file,
					Message:  fmt.Sprintf("Flow schemas %q and %q map to the same public TypeScript name %q with different shapes", priorName, name, definition.CustomName),
				})
				nameCollision = true
				break
			}
		}
		if nameCollision {
			continue
		}
		a.flowSchemas[name] = definition
	}
	return definitions
}

func sameSchemaDefinition(a, b StructDefinition) bool {
	if a.Name != b.Name || len(a.Fields) != len(b.Fields) {
		return false
	}
	for i := range a.Fields {
		x, y := a.Fields[i], b.Fields[i]
		if x.Name != y.Name || x.Key != y.Key || x.Type != y.Type || x.Required != y.Required || x.Nullable != y.Nullable || x.JSONOmitEmpty != y.JSONOmitEmpty || x.Validation != y.Validation {
			return false
		}
	}
	return true
}

func (a *flowAnalyzer) applyFlowPipelineSchemas(contract *ActionContract, modifiers []string, file string, line int) {
	var requestName, responseName string
	requestDeclared, responseDeclared := false, false
	for _, raw := range modifiers {
		key, value, hasValue := strings.Cut(strings.TrimSpace(raw), "=")
		if !hasValue {
			continue
		}
		key = strings.TrimSpace(compiler.ModifierName(key))
		name := strings.TrimSpace(flowModifierValue(value))
		name = strings.TrimPrefix(name, "!")
		switch key {
		case "request-schema", "in-schema":
			requestDeclared, requestName = true, name
		case "response-schema", "out-schema":
			responseDeclared, responseName = true, name
		}
	}
	if !requestDeclared && !responseDeclared {
		return
	}

	contract.FlowSchemaDriven = true
	contract.TypeSource = "nflow-schema"
	// Clearing both sides first is deliberate: an absent schema modifier is
	// not permission to infer a request or response DTO from a Go action.
	contract.ReqTypeName, contract.ReqType = "", TypeRef{}
	contract.ResTypeName, contract.ResType = "", TypeRef{}
	contract.ReqFields, contract.ResFields = nil, nil
	contract.Multipart, contract.BlobResponse = false, false

	bind := func(side, name string, declared bool) (string, string, TypeRef) {
		if !declared {
			return "", "", TypeRef{}
		}
		if name == "" {
			a.addFlowSchemaReferenceError(file, line, contract.Name, side, name)
			return "", "", TypeRef{}
		}
		resolved, definition, err := a.findFlowSchema(name)
		if err != nil || resolved == "" {
			a.addFlowSchemaReferenceError(file, line, contract.Name, side, name)
			return "", "", TypeRef{}
		}
		return resolved, definition.Name, TypeRef{Domain: "nflow", Package: "nflow", Name: resolved}
	}
	contract.ReqSchemaName, contract.ReqTypeName, contract.ReqType = bind("request", requestName, requestDeclared)
	contract.ResSchemaName, contract.ResTypeName, contract.ResType = bind("response", responseName, responseDeclared)
}

func (a *flowAnalyzer) findFlowSchema(name string) (string, StructDefinition, error) {
	if definition, ok := a.flowSchemas[name]; ok {
		return name, definition, nil
	}
	known := make(map[string]bool, len(a.flowSchemas))
	for schemaName := range a.flowSchemas {
		known[schemaName] = true
	}
	resolved, err := resolveFlowSchemaName(name, known)
	if err != nil || resolved == "" {
		return "", StructDefinition{}, err
	}
	definition, ok := a.flowSchemas[resolved]
	if !ok {
		return "", StructDefinition{}, nil
	}
	return resolved, definition, nil
}

func (a *flowAnalyzer) addFlowSchemaReferenceError(file string, line int, action, side, name string) {
	a.diagnostics = append(a.diagnostics, Diagnostic{
		Severity: SeverityError,
		Rule:     "nflow/schema-reference",
		Action:   action,
		File:     file,
		Line:     line,
		Message:  fmt.Sprintf("NFlow route %s declares %s schema %q, but no matching @schema declaration was provided", action, side, name),
	})
}
