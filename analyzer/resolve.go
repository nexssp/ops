package analyzer

import "strings"

func (a *Analyzer) FindStruct(nameOrQualified string) (StructDefinition, bool) {
	for _, s := range a.structs {
		if s.Qualified == nameOrQualified || s.Name == nameOrQualified {
			return s, true
		}
	}
	return StructDefinition{}, false
}

func (a *Analyzer) resolveActionStruct(act ActionContract, isReq bool) (StructDefinition, bool) {
	ref := act.ReqType
	typeName := act.ReqTypeName
	if !isReq {
		ref = act.ResType
		typeName = act.ResTypeName
	}
	if act.FlowSchemaDriven {
		schemaName := act.ReqSchemaName
		if !isReq {
			schemaName = act.ResSchemaName
		}
		if schemaName == "" {
			return StructDefinition{}, false
		}
		schema, ok := a.flowSchemas[schemaName]
		return schema, ok
	}

	if ref.Package != "" && ref.Name != "" {
		if s, ok := a.structs[StructKey{Domain: ref.Domain, Package: ref.Package, Name: ref.Name}]; ok {
			return s, true
		}
		for k, s := range a.structs {
			if k.Package == ref.Package && k.Name == ref.Name {
				return s, true
			}
		}
	}

	if idx := strings.IndexByte(act.Name, '.'); idx > 0 {
		pkg := act.Name[:idx]
		cleanType := typeName
		if dotIdx := strings.LastIndex(cleanType, "."); dotIdx >= 0 {
			cleanType = cleanType[dotIdx+1:]
		}
		for k, s := range a.structs {
			if (k.Package == pkg || s.Package == pkg) && (k.Name == cleanType || s.Name == cleanType) {
				return s, true
			}
		}
	}

	clean := typeName
	if idx := strings.LastIndex(clean, "."); idx >= 0 {
		clean = clean[idx+1:]
	}
	for _, s := range a.structs {
		if s.Name == clean || s.Qualified == typeName {
			return s, true
		}
	}

	return StructDefinition{}, false
}

func (a *Analyzer) correlateAndValidate() {
	for i := range a.actions {
		act := &a.actions[i]
		if s, ok := a.resolveActionStruct(*act, true); ok {
			act.ReqFields = s.Fields

			// Propagate the multipart flag onto the action so the endpoint
			// generator can switch the client method shape.
			for _, f := range s.Fields {
				if f.Multipart {
					act.Multipart = true
					break
				}
			}
		}
		if s, ok := a.resolveActionStruct(*act, false); ok {
			act.ResFields = s.Fields
		}
	}

	// Store convention checks run here, in the analysis pipeline, so the
	// resulting diagnostics are counted in the report summary and can
	// abort 'nexssp sdk' before any file is written. The clustering logic
	// is shared with codegen — running it twice is idempotent and cheap.
	a.validateStoreConventions()
}

// validateStoreConventions clusters actions and emits WARNING diagnostics
// when a CRUD-verb action violates the REST convention used by the
// generated entity stores (e.g. `create` bound to GET, `get` missing an
// id path segment). Non-blocking: the store is still generated, but the
// operator is told exactly what to fix.
func (a *Analyzer) validateStoreConventions() {
	structs := make(map[string]StructDefinition, len(a.structs))
	for _, s := range a.structs {
		structs[s.Qualified] = s
		if s.ImportPath != "" {
			structs[s.ImportPath+"."+s.Name] = s
		}
	}

	clusters := clusterStores(a.actions, structs)
	validateClusters(clusters, &a.diagnostics)
}
