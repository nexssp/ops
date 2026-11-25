package analyzer

import (
	"fmt"
	"strings"

	"github.com/nexssp/ops/analyzer/core"
)

// ContractManifest describes explicit wire-contract types for actions that the
// source analyzer cannot fully infer. A nil type means "not specified"; use
// the literal "struct{}" to explicitly declare an empty request or response.
type ContractManifest struct {
	Actions []ContractManifestAction `json:"actions"`
}

// ContractManifestAction supplies optional request and response types for one
// action name. Type references are optional disambiguators for Go DTOs.
type ContractManifestAction struct {
	Name       string   `json:"name"`
	ReqType    *string  `json:"req_type,omitempty"`
	ResType    *string  `json:"res_type,omitempty"`
	ReqTypeRef *TypeRef `json:"req_type_ref,omitempty"`
	ResTypeRef *TypeRef `json:"res_type_ref,omitempty"`
}

// ApplyContractManifest merges explicit wire types into analyzed actions.
// It only fills missing types (or confirms an identical inferred type), never
// changes action scope or route metadata, and refuses to override NFlow
// boundary schemas. The operation is atomic: report.Actions is unchanged on
// error. Action names may appear more than once in the report when one action
// has multiple route bindings; a manifest entry applies to each binding.
func ApplyContractManifest(report *Report, manifest ContractManifest) error {
	if report == nil {
		return fmt.Errorf("apply contract manifest: report is nil")
	}
	if len(manifest.Actions) == 0 {
		return fmt.Errorf("apply contract manifest: no actions were provided")
	}

	actionIndexes := make(map[string][]int, len(report.Actions))
	for i := range report.Actions {
		actionIndexes[report.Actions[i].Name] = append(actionIndexes[report.Actions[i].Name], i)
	}

	working := append([]ActionContract(nil), report.Actions...)
	seen := make(map[string]struct{}, len(manifest.Actions))
	for _, entry := range manifest.Actions {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			return fmt.Errorf("apply contract manifest: action name is required")
		}
		if name != entry.Name {
			return fmt.Errorf("apply contract manifest: action name %q has leading or trailing whitespace", entry.Name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("apply contract manifest: duplicate action %q", name)
		}
		seen[name] = struct{}{}

		indexes := actionIndexes[name]
		if len(indexes) == 0 {
			return fmt.Errorf("apply contract manifest: unknown action %q", name)
		}
		if entry.ReqType == nil && entry.ResType == nil && entry.ReqTypeRef == nil && entry.ResTypeRef == nil {
			return fmt.Errorf("apply contract manifest: action %q does not specify a request or response type", name)
		}

		for _, index := range indexes {
			action := &working[index]
			if action.FlowSchemaDriven {
				return fmt.Errorf("apply contract manifest: action %q is Flow-schema-driven; declare its boundary types in the Flow schema instead", name)
			}
			if err := applyManifestSide(report, action, true, entry.ReqType, entry.ReqTypeRef); err != nil {
				return fmt.Errorf("apply contract manifest: action %q request: %w", name, err)
			}
			if err := applyManifestSide(report, action, false, entry.ResType, entry.ResTypeRef); err != nil {
				return fmt.Errorf("apply contract manifest: action %q response: %w", name, err)
			}
		}
	}

	correlationReport := *report
	correlationReport.Actions = working
	core.CorrelateActionFields(&correlationReport)
	report.Actions = correlationReport.Actions
	return nil
}

func applyManifestSide(report *Report, action *ActionContract, request bool, declaredName *string, declaredRef *TypeRef) error {
	if declaredName == nil && declaredRef == nil {
		return nil
	}

	name, ref, err := normalizeManifestType(report, declaredName, declaredRef)
	if err != nil {
		return err
	}

	currentName, currentRef := action.ResTypeName, action.ResType
	if request {
		currentName, currentRef = action.ReqTypeName, action.ReqType
	}
	if currentName != "" && currentName != name && !equivalentEmptyContractTypes(currentName, name) {
		return fmt.Errorf("manifest type %q conflicts with inferred type %q", name, currentName)
	}
	if typeRefsConflict(currentRef, ref) {
		return fmt.Errorf("manifest type reference %q conflicts with inferred reference %q", ref.String(), currentRef.String())
	}

	if request {
		action.ReqTypeName = name
		if ref.Name != "" {
			action.ReqType = ref
		}
	} else {
		action.ResTypeName = name
		if ref.Name != "" {
			action.ResType = ref
		}
	}
	action.ContractSource = "manifest"
	return nil
}

func equivalentEmptyContractTypes(left, right string) bool {
	return isEmptyContractTypeName(left) && isEmptyContractTypeName(right)
}

func isEmptyContractTypeName(name string) bool {
	return name == "struct{}" || strings.HasSuffix(name, ".struct{}")
}

func normalizeManifestType(report *Report, declaredName *string, declaredRef *TypeRef) (string, TypeRef, error) {
	name := ""
	if declaredName != nil {
		name = strings.TrimSpace(*declaredName)
		if name == "" {
			return "", TypeRef{}, fmt.Errorf("empty type names are ambiguous; use \"struct{}\" for an explicit empty contract")
		}
	}

	var ref TypeRef
	if declaredRef != nil {
		ref = *declaredRef
		if strings.TrimSpace(ref.Name) == "" {
			return "", TypeRef{}, fmt.Errorf("type reference name is required")
		}
		if name == "" {
			name = ref.String()
		} else if name != ref.Name && name != ref.String() {
			return "", TypeRef{}, fmt.Errorf("type name %q does not match type reference %q", name, ref.String())
		}
	}

	if name == "" {
		return "", TypeRef{}, fmt.Errorf("type name is required")
	}
	if name == "struct{}" {
		if ref.Name != "" {
			return "", TypeRef{}, fmt.Errorf("struct{} cannot have a named type reference")
		}
		return name, TypeRef{}, nil
	}

	if ref.Name == "" {
		inferred, err := inferManifestTypeRef(report, name)
		if err != nil {
			return "", TypeRef{}, err
		}
		ref = inferred
	}
	return name, ref, nil
}

func inferManifestTypeRef(report *Report, typeName string) (TypeRef, error) {
	clean := strings.TrimSpace(typeName)
	for strings.HasPrefix(clean, "[]") {
		clean = strings.TrimSpace(strings.TrimPrefix(clean, "[]"))
	}
	clean = strings.TrimPrefix(clean, "*")

	var exact *StructDefinition
	var matches []StructDefinition
	for _, definition := range report.Structs {
		if definition.Qualified == typeName || (definition.ImportPath != "" && definition.ImportPath+"."+definition.Name == typeName) {
			copyDefinition := definition
			exact = &copyDefinition
			break
		}
		if !strings.Contains(clean, ".") && definition.Name == clean {
			matches = append(matches, definition)
		}
	}
	if exact != nil {
		return typeRefForStruct(*exact), nil
	}
	if strings.Contains(clean, ".") {
		return TypeRef{}, nil
	}
	if len(matches) > 1 {
		return TypeRef{}, fmt.Errorf("type %q is ambiguous across Go packages; provide a type reference", typeName)
	}
	if len(matches) == 1 {
		return typeRefForStruct(matches[0]), nil
	}
	return TypeRef{}, nil
}

func typeRefForStruct(definition StructDefinition) TypeRef {
	return TypeRef{
		ImportPath: definition.ImportPath,
		Domain:     definition.Domain,
		Package:    definition.Package,
		Name:       definition.Name,
	}
}

func typeRefsConflict(current, declared TypeRef) bool {
	if current.Name == "" || declared.Name == "" {
		return false
	}
	return current.Name != declared.Name ||
		(current.Package != "" && declared.Package != "" && current.Package != declared.Package) ||
		(current.Domain != "" && declared.Domain != "" && current.Domain != declared.Domain) ||
		(current.ImportPath != "" && declared.ImportPath != "" && current.ImportPath != declared.ImportPath)
}
