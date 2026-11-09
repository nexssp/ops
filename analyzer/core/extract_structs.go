package core

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
)

var (
	reDirective = regexp.MustCompile(`@nexss:(?:sdk|expose)\s*(.*)`)
	reParam     = regexp.MustCompile(`(\w+):"([^"]+)"`)
)

func parseDirectiveParams(docText string) (isExposed bool, customName, targetFile string) {
	if strings.Contains(docText, "@nexss:expose") {
		isExposed = true
	}

	matches := reDirective.FindStringSubmatch(docText)
	if len(matches) > 1 {
		isExposed = true
		for _, pMatch := range reParam.FindAllStringSubmatch(matches[1], -1) {
			if pMatch[1] == "name" {
				customName = pMatch[2]
			}
			if pMatch[1] == "file" {
				targetFile = pMatch[2]
			}
		}
	}
	return isExposed, customName, targetFile
}

func (a *Analyzer) extractStructsAndFuncs(f *ast.File) {
	filePath := a.fset.Position(f.Pos()).Filename
	pkgInfo := getPackageInfo(filePath, f.Name.Name)

	baseName := filepath.Base(filePath)
	isConventionFile := strings.HasSuffix(baseName, "events.go") ||
		strings.HasSuffix(baseName, "dto.go") ||
		strings.HasSuffix(baseName, "types.go") ||
		strings.HasSuffix(baseName, "service.go") ||
		strings.HasSuffix(baseName, "contracts.go")

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !ast.IsExported(ts.Name.Name) {
				continue
			}

			// Type alias:  type Foo = pkg.Bar
			//
			// Recorded only from convention files. The alias itself is
			// treated as a declaration of exposure intent for its target
			// (see propagateAliasExposure) — that's how a struct from a
			// library package enters codegen without the library carrying
			// any tooling-specific directive.
			if ts.Assign.IsValid() && isConventionFile {
				target := exprToString(ts.Type)
				if target != "" {
					a.aliases[StructKey{
						Domain:  pkgInfo.Domain,
						Package: pkgInfo.Package,
						Name:    ts.Name.Name,
					}] = target
				}
				continue
			}

			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}

			docText := ""
			if genDecl.Doc != nil {
				docText += genDecl.Doc.Text()
			}
			if ts.Doc != nil {
				docText += ts.Doc.Text()
			}

			isExposed, customName, targetFile := parseDirectiveParams(docText)

			if isConventionFile {
				isExposed = true
			}
			if strings.Contains(docText, "@nexss:ignore") {
				isExposed = false
			}

			key := StructKey{Domain: pkgInfo.Domain, Package: pkgInfo.Package, Name: ts.Name.Name}
			qualified := pkgInfo.Package + "." + ts.Name.Name

			if existing, dup := a.structs[key]; dup {
				if !sameFields(existing.Fields, a.extractStructFields(st)) {
					a.diagnostics = append(a.diagnostics, Diagnostic{
						Severity: SeverityError,
						Rule:     "structs/duplicate-name",
						Message: fmt.Sprintf(
							"Struct %q declared twice in package %q with different shapes:\n"+
								"      • %s\n      • %s",
							ts.Name.Name, pkgInfo.Package,
							existing.SourceFile, filePath,
						),
						Suggestion: "Delete one declaration. Go cannot compile a " +
							"package that declares the same struct twice.",
						File: filePath,
					})
				}
				continue
			}

			a.structs[key] = StructDefinition{
				Name:       ts.Name.Name,
				Package:    pkgInfo.Package,
				Domain:     pkgInfo.Domain,
				ImportPath: pkgInfo.ImportPath,
				Qualified:  qualified,
				SourceFile: filePath,
				Fields:     a.extractStructFields(st),
				IsExposed:  isExposed,
				IsIgnored:  strings.Contains(docText, "@nexss:ignore"),
				CustomName: customName,
				TargetFile: targetFile,
			}
		}
	}
}

func (a *Analyzer) extractStructFields(st *ast.StructType) []FieldContract {
	var fields []FieldContract
	if st == nil || st.Fields == nil {
		return fields
	}

	for _, field := range st.Fields.List {
		if len(field.Names) == 0 || !ast.IsExported(field.Names[0].Name) {
			continue
		}
		name := field.Names[0].Name

		rawTag := ""
		if field.Tag != nil {
			rawTag = strings.Trim(field.Tag.Value, "`")
		}

		jsonKey := name
		tagStruct := reflect.StructTag(rawTag)
		if jt := tagStruct.Get("json"); jt != "" {
			parts := strings.Split(jt, ",")
			if parts[0] == "-" {
				continue
			}
			if parts[0] != "" {
				jsonKey = parts[0]
			}
		}

		valRule := tagStruct.Get("validate")
		typeStr := exprToString(field.Type)

		// File field detection: []byte with a `form:"..."` tag means the
		// field is a multipart file part, not a JSON base64 string. The
		// client generator switches to multipart/form-data for the whole
		// request when any field carries this flag.
		isMultipart := false
		if typeStr == "[]byte" {
			if formKey := tagStruct.Get("form"); formKey != "" {
				isMultipart = true
				jsonKey = formKey
			}
		}

		fields = append(fields, FieldContract{
			Name:       name,
			Key:        jsonKey,
			Type:       mapGoTypeToTS(typeStr),
			Required:   fieldIsRequired(tagStruct, valRule),
			Validation: valRule,
			RawTags:    rawTag,
			Multipart:  isMultipart,
		})
	}
	return fields
}

// fieldIsRequired combines explicit validator rules with JSON encoding rules.
// A field is required when validation says so or when JSON always emits it;
// omitempty in either tag keeps a field optional unless validation explicitly
// requires it.
func fieldIsRequired(tag reflect.StructTag, validation string) bool {
	if strings.Contains(validation, "required") {
		return true
	}
	if strings.Contains(validation, "omitempty") {
		return false
	}
	jsonTag := tag.Get("json")
	if jsonTag == "" {
		return false
	}
	for _, option := range strings.Split(jsonTag, ",")[1:] {
		if option == "omitempty" {
			return false
		}
	}
	return true
}

// sameFields compares two field slices on the shape that matters to codegen:
// JSON key, TS type, and required flag. Declaration order is significant —
// different order means a different DTO to TypeScript.
func sameFields(a, b []FieldContract) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].Type != b[i].Type || a[i].Required != b[i].Required {
			return false
		}
	}
	return true
}

// propagateAliasExposure walks every alias recorded from a convention file
// and marks the target struct exposed. The alias *is* the exposure
// declaration — no additional struct definition is required at the alias
// site.
//
// This is how `type DockerExecRecord = docker.DockerExecRecord` in a
// dto.go-named file pulls the library's DockerExecRecord into the SDK
// without the library carrying any tooling directive.
//
// Runs after all files have been parsed and before validation so codegen
// sees the final exposure state.
func (a *Analyzer) propagateAliasExposure() {
	type aliasEntry struct {
		src StructKey
		tgt string
	}
	var entries []aliasEntry
	for k, v := range a.aliases {
		entries = append(entries, aliasEntry{src: k, tgt: v})
	}

	for _, e := range entries {
		pkg, name := splitQualified(e.tgt)
		if name == "" {
			continue
		}

		for k, s := range a.structs {
			if k.Name != name {
				continue
			}
			if pkg != "" && k.Package != pkg && s.Qualified != e.tgt {
				continue
			}
			if !s.IsExposed {
				s.IsExposed = true
				a.structs[k] = s
			}
		}
	}
}

// splitQualified parses "pkg.Name" into ("pkg", "Name"). Bare names return
// ("", name).
func splitQualified(s string) (string, string) {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}
