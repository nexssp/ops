package analyzer

import (
	"sort"
	"strings"
)

type GeneratedFile struct {
	Path      string
	Content   []byte
	Overwrite bool
}

type StoreCluster struct {
	Noun         string
	List         *ActionContract
	Get          *ActionContract
	Create       *ActionContract
	Update       *ActionContract
	Delete       *ActionContract
	EntityType   string
	EntityStruct StructDefinition
	IDField      string
}

var crudVerbs = map[string]string{
	"list": "list", "all": "list", "index": "list",
	"get": "get", "find": "get", "show": "get", "fetch": "get", "read": "get",
	"create": "create", "add": "create", "new": "create", "store": "create",
	"update": "update", "patch": "update", "edit": "update", "put": "update",
	"delete": "delete", "remove": "delete", "destroy": "delete", "del": "delete",
}

func clusterStores(actions []ActionContract, structs map[string]StructDefinition) []StoreCluster {
	byNoun := map[string]*StoreCluster{}

	for i := range actions {
		act := &actions[i]
		if act.Scope != "public" || act.Streaming || act.Raw {
			continue
		}
		parts := strings.SplitN(act.Name, ".", 2)
		if len(parts) != 2 {
			continue
		}
		noun, verb := parts[0], parts[1]
		slot, ok := crudVerbs[verb]
		if !ok {
			continue
		}

		c, exists := byNoun[noun]
		if !exists {
			c = &StoreCluster{Noun: noun}
			byNoun[noun] = c
		}
		switch slot {
		case "list":
			if c.List == nil {
				c.List = act
			}
		case "get":
			if c.Get == nil {
				c.Get = act
			}
		case "create":
			if c.Create == nil {
				c.Create = act
			}
		case "update":
			if c.Update == nil {
				c.Update = act
			}
		case "delete":
			if c.Delete == nil {
				c.Delete = act
			}
		}
	}

	out := make([]StoreCluster, 0, len(byNoun))
	for _, c := range byNoun {
		hasRead := c.List != nil || c.Get != nil
		hasWrite := c.Create != nil || c.Update != nil || c.Delete != nil
		if !hasRead || !hasWrite {
			continue
		}

		var sample *ActionContract
		for _, cand := range []*ActionContract{c.Get, c.List, c.Create, c.Update} {
			if cand != nil {
				sample = cand
				break
			}
		}
		if sample == nil {
			continue
		}
		typeName := sample.ResTypeName
		if typeName == "" || typeName == "struct{}" || typeName == "any" {
			continue
		}

		sDef, ok := lookupStruct(structs, typeName)
		if !ok {
			continue
		}

		c.EntityType = sDef.Name
		c.EntityStruct = sDef
		c.IDField = detectIDField(sDef)
		out = append(out, *c)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Noun < out[j].Noun })
	return out
}

func lookupStruct(structs map[string]StructDefinition, typeName string) (StructDefinition, bool) {
	clean := lastSegment(typeName)
	for _, s := range structs {
		if s.Name == clean || s.Qualified == typeName {
			return s, true
		}
	}
	return StructDefinition{}, false
}

func detectIDField(s StructDefinition) string {
	for _, f := range s.Fields {
		if f.Key == "id" {
			return "id"
		}
	}
	for _, f := range s.Fields {
		if strings.HasSuffix(f.Key, "_id") {
			return f.Key
		}
	}
	if len(s.Fields) > 0 {
		return s.Fields[0].Key
	}
	return "id"
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

// isFormAction decides whether a mutating action should get a standalone form
// store under _gen/stores/forms/.
//
// An action is a form candidate when it is a public POST/PUT/PATCH that is
// not a CRUD verb AND has a meaningful request payload.
//
// The analyzer stores request type names with their package prefix, so a
// handler that takes struct{} arrives here as "jumalu.struct{}". Normalize
// before deciding so anonymous-empty request types never produce a form
// store with an unresolvable type reference.
func isFormAction(act *ActionContract) bool {
	if act == nil || act.Scope != "public" || act.Streaming || act.Raw {
		return false
	}
	switch act.Method {
	case "POST", "PUT", "PATCH":
	default:
		return false
	}

	// Normalize "pkg.struct{}" → "struct{}" before the payload check.
	clean := lastSegment(act.ReqTypeName)
	switch clean {
	case "", "struct{}", "struct{...}", "void", "any":
		return false
	}

	parts := strings.SplitN(act.Name, ".", 2)
	if len(parts) == 2 {
		if _, ok := crudVerbs[parts[1]]; ok {
			return false
		}
	}
	return true
}

func actionSlug(name string) string {
	return strings.ReplaceAll(name, ".", "-")
}

func validateClusters(clusters []StoreCluster, diags *[]Diagnostic) {
	for _, c := range clusters {
		check := func(act *ActionContract, expectedMethod, wantPathSuffix string) {
			if act == nil {
				return
			}
			if act.Method != expectedMethod {
				*diags = append(*diags, Diagnostic{
					Severity: SeverityWarning,
					Rule:     "stores/rest-convention",
					Action:   act.Name,
					File:     act.SourceFile,
					Line:     act.SourceLine,
					Message:  "CRUD verb expects " + expectedMethod + " but action uses " + act.Method,
					Suggestion: "Rename the action, or change the HTTP verb, or move it out of the " +
						c.Noun + ".* cluster so the generated entity store doesn't mis-classify it.",
				})
				return
			}
			if wantPathSuffix == "/{id}" && !strings.Contains(act.Path, "{") {
				*diags = append(*diags, Diagnostic{
					Severity:   SeverityWarning,
					Rule:       "stores/rest-convention",
					Action:     act.Name,
					File:       act.SourceFile,
					Line:       act.SourceLine,
					Message:    "CRUD verb expects an item path like /" + c.Noun + "/{id} but got " + act.Path,
					Suggestion: "Point the route at /" + c.Noun + "/{id} or rename the verb to `list`/`create`.",
				})
			}
		}
		check(c.List, "GET", "")
		check(c.Get, "GET", "/{id}")
		check(c.Create, "POST", "")
		if c.Update != nil {
			check(c.Update, c.Update.Method, "/{id}")
		}
		check(c.Delete, "DELETE", "/{id}")
	}
}

// generateStoreFiles produces the entire _gen/stores/ tree plus the
// app-level stores barrel at ui/src/lib/stores/index.ts.
func generateStoreFiles(report *Report, colliding map[string]bool) []GeneratedFile {
	var out []GeneratedFile

	// ── Runtime support layer — write once, editable by hand ──
	out = append(out,
		GeneratedFile{Path: "_gen/stores/runtime/entity.svelte.ts", Content: []byte(runtimeEntityStoreTS), Overwrite: false},
		GeneratedFile{Path: "_gen/stores/runtime/form.svelte.ts", Content: []byte(runtimeFormStoreTS), Overwrite: false},
		GeneratedFile{Path: "_gen/stores/runtime/stream.svelte.ts", Content: []byte(runtimeStreamStoreTS), Overwrite: false},
		GeneratedFile{Path: "_gen/stores/runtime/index.ts", Content: []byte(runtimeIndexTS), Overwrite: false},
	)

	// ── Per-resource entity stores ──
	clusters := clusterStores(report.Actions, report.Structs)

	for _, c := range clusters {
		out = append(out, GeneratedFile{
			Path:      "_gen/stores/entities/" + c.Noun + ".svelte.ts",
			Content:   emitEntityStore(c, report, colliding),
			Overwrite: true,
		})
	}

	// ── Form stores ──
	formNames := map[string]bool{}
	for i := range report.Actions {
		act := &report.Actions[i]
		if !isFormAction(act) {
			continue
		}
		slug := actionSlug(act.Name)
		if formNames[slug] {
			continue
		}
		formNames[slug] = true
		out = append(out, GeneratedFile{
			Path:      "_gen/stores/forms/" + slug + ".svelte.ts",
			Content:   emitFormStore(*act, report),
			Overwrite: true,
		})
	}

	// ── Stream stores ──
	for i := range report.Actions {
		act := &report.Actions[i]
		if !act.Streaming {
			continue
		}
		out = append(out, GeneratedFile{
			Path:      "_gen/stores/streams/" + actionSlug(act.Name) + ".svelte.ts",
			Content:   emitStreamStore(*act, report),
			Overwrite: true,
		})
	}

	// ── System stores ──
	out = append(out, emitSystemStores(report)...)

	// ── Generated store barrel ──
	out = append(out, GeneratedFile{
		Path:      "_gen/stores/index.ts",
		Content:   emitStoresIndex(clusters, report, formNames),
		Overwrite: true,
	})

	// ── App-level store barrel (write-once) ──
	out = append(out, GeneratedFile{
		Path:      "stores/index.ts",
		Content:   generateAppStoresBarrel(),
		Overwrite: false,
	})

	return out
}

// generateAppStoresBarrel emits ui/src/lib/stores/index.ts — the merge
// point between generated stores and hand-written custom stores.
//
// Written once, then preserved. Delete to reset.
func generateAppStoresBarrel() []byte {
	return []byte(`/**
 * App-level stores barrel.
 *
 * Written once by 'nexssp sdk', then preserved on subsequent runs.
 *
 * Ownership:
 *   _gen/stores/**    generated — do not edit
 *   custom/stores.ts  yours — add app-owned stores there
 *   stores/*.svelte.ts yours — app-specific stores (jumalu.svelte.ts, etc.)
 *   stores/index.ts   this file — the merge point
 */

export * from '../_gen/stores';
export * from '../custom/stores';

// App-specific stores are added below, one line per file. They are not
// managed by codegen — add them manually and they will survive every
// 'nexssp sdk' run.
//
//   export * from './jumalu.svelte';
//   export * from './auth.svelte';
`)
}
