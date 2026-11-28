package analyzer

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nexssp/ops/analyzer/core"
)

func validateAPIClientConfig(config *core.APIClientConfig) error {
	if config == nil {
		return nil
	}
	if config.Transport != "" && config.Transport != "single-fetcher" && config.Transport != "split-fetcher" {
		return fmt.Errorf("apply API surface manifest: unsupported client transport %q", config.Transport)
	}
	if config.PathParameterEncoding != "" && config.PathParameterEncoding != "none" && config.PathParameterEncoding != "component" {
		return fmt.Errorf("apply API surface manifest: unsupported path parameter encoding %q", config.PathParameterEncoding)
	}
	if err := validateUniqueIdentifiers("error kind", config.ErrorKinds); err != nil {
		return err
	}
	if err := validateUniqueIdentifiers("endpoint metadata field", config.EndpointMetadataFields); err != nil {
		return err
	}
	if err := validateUniqueIdentifiers("field metadata field", config.FieldMetadataFields); err != nil {
		return err
	}
	seenAliases := make(map[string]struct{}, len(config.ScalarAliases))
	for _, alias := range config.ScalarAliases {
		if !validSDKMethodIdentifier(alias.Name) || !validAPISurfaceTypeExpression(alias.Type) {
			return fmt.Errorf("apply API surface manifest: invalid scalar alias %q", alias.Name)
		}
		if _, exists := seenAliases[alias.Name]; exists {
			return fmt.Errorf("apply API surface manifest: scalar alias %q is declared more than once", alias.Name)
		}
		seenAliases[alias.Name] = struct{}{}
	}
	for _, name := range []string{config.FacadeExportName, config.FactoryExportName} {
		if name != "" && !validSDKMethodIdentifier(name) {
			return fmt.Errorf("apply API surface manifest: client export name %q is not a valid TypeScript identifier", name)
		}
	}
	return nil
}

func validateUniqueIdentifiers(label string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validSDKMethodIdentifier(value) {
			return fmt.Errorf("apply API surface manifest: %s %q is not a valid TypeScript identifier", label, value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("apply API surface manifest: %s %q is declared more than once", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateAPISurfaceTypes(types []core.APISurfaceType) error {
	seenTypes := make(map[string]struct{}, len(types))
	for _, apiType := range types {
		if !validSDKMethodIdentifier(apiType.Name) {
			return fmt.Errorf("apply API surface manifest: type name %q is not a valid TypeScript identifier", apiType.Name)
		}
		if _, exists := seenTypes[apiType.Name]; exists {
			return fmt.Errorf("apply API surface manifest: type %q is declared more than once", apiType.Name)
		}
		seenTypes[apiType.Name] = struct{}{}
		seenFields := make(map[string]struct{}, len(apiType.Fields))
		for _, field := range apiType.Fields {
			if !validSDKMethodIdentifier(field.Key) {
				return fmt.Errorf("apply API surface manifest: type %q has invalid field key %q", apiType.Name, field.Key)
			}
			if _, exists := seenFields[field.Key]; exists {
				return fmt.Errorf("apply API surface manifest: type %q declares field %q more than once", apiType.Name, field.Key)
			}
			seenFields[field.Key] = struct{}{}
			if !validAPISurfaceTypeExpression(field.Type) {
				return fmt.Errorf("apply API surface manifest: type %q field %q has invalid TypeScript type %q", apiType.Name, field.Key, field.Type)
			}
			if err := validateZodFieldConfig(apiType.Name, field); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateZodFieldConfig(typeName string, field core.FieldContract) error {
	if field.Zod == nil {
		return nil
	}
	switch field.Zod.Base {
	case "", "iso-datetime", "number-or-string", "file":
	default:
		return fmt.Errorf("apply API surface manifest: type %q field %q has unsupported Zod base %q", typeName, field.Key, field.Zod.Base)
	}
	for _, refinement := range field.Zod.Refinements {
		switch refinement.Kind {
		case "min", "max":
			if _, err := strconv.ParseFloat(refinement.Value, 64); err != nil || strings.TrimSpace(refinement.Value) != refinement.Value {
				return fmt.Errorf("apply API surface manifest: type %q field %q has invalid %s refinement value %q", typeName, field.Key, refinement.Kind, refinement.Value)
			}
		case "email":
		case "regex":
			if refinement.Value == "" {
				return fmt.Errorf("apply API surface manifest: type %q field %q has an empty regex refinement", typeName, field.Key)
			}
		default:
			return fmt.Errorf("apply API surface manifest: type %q field %q has unsupported Zod refinement %q", typeName, field.Key, refinement.Kind)
		}
	}
	return nil
}

func mergeAPISurfaceTypes(structs map[string]core.StructDefinition, types []core.APISurfaceType) {
	for _, apiType := range types {
		for key, existing := range structs {
			if existing.Name == apiType.Name {
				delete(structs, key)
			}
		}
		definition := core.StructDefinition{
			Name:       apiType.Name,
			Package:    "manifest",
			Domain:     "manifest",
			Qualified:  "manifest." + apiType.Name,
			SourceFile: "api-surface.manifest.json",
			Fields:     append([]core.FieldContract(nil), apiType.Fields...),
			IsExposed:  true,
		}
		structs[definition.Qualified] = definition
	}
}

func validAPISurfaceTypeExpression(value string) bool {
	if value == "Record<string, unknown>" {
		return true
	}
	parser := apiTypeExpressionParser{value: value}
	if strings.TrimSpace(value) != value || value == "" || !parser.parseUnion() {
		return false
	}
	parser.skipSpaces()
	return parser.offset == len(parser.value)
}

type apiTypeExpressionParser struct {
	value  string
	offset int
}

func (p *apiTypeExpressionParser) parseUnion() bool {
	if !p.parsePostfix() {
		return false
	}
	for {
		p.skipSpaces()
		if !p.consume('|') {
			return true
		}
		if !p.parsePostfix() {
			return false
		}
	}
}

func (p *apiTypeExpressionParser) parsePostfix() bool {
	if !p.parsePrimary() {
		return false
	}
	for {
		p.skipSpaces()
		if p.offset+1 >= len(p.value) || p.value[p.offset] != '[' || p.value[p.offset+1] != ']' {
			return true
		}
		p.offset += 2
	}
}

func (p *apiTypeExpressionParser) parsePrimary() bool {
	p.skipSpaces()
	if p.offset >= len(p.value) || !isTypeIdentifierStart(p.value[p.offset]) {
		return false
	}
	start := p.offset
	for p.offset < len(p.value) && isTypeIdentifierPart(p.value[p.offset]) {
		p.offset++
	}
	identifier := p.value[start:p.offset]
	if identifier == "any" || identifier == "unknown" {
		return false
	}
	p.skipSpaces()
	if !p.consume('<') {
		return true
	}
	for {
		if !p.parseUnion() {
			return false
		}
		p.skipSpaces()
		if p.consume('>') {
			return true
		}
		if !p.consume(',') {
			return false
		}
	}
}

func (p *apiTypeExpressionParser) skipSpaces() {
	for p.offset < len(p.value) && (p.value[p.offset] == ' ' || p.value[p.offset] == '\t') {
		p.offset++
	}
}

func (p *apiTypeExpressionParser) consume(expected byte) bool {
	if p.offset < len(p.value) && p.value[p.offset] == expected {
		p.offset++
		return true
	}
	return false
}

func isTypeIdentifierStart(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isTypeIdentifierPart(value byte) bool {
	return isTypeIdentifierStart(value) || value >= '0' && value <= '9' || value == '.'
}
