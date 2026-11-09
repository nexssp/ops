package core

import (
	"go/ast"
	"go/token"
	"strings"
)

// EnumDefinition captures a Go string-backed enum type and its values.
//
// Convention recognised by the analyzer:
//
//	type TaskStatus string
//	const (
//	    StatusPending TaskStatus = "PENDING"
//	    StatusRunning            = "RUNNING"  // inherits the type
//	)
//
// Only exported type names and exported const names are captured. The
// captured TypeName is the Go type; ConstName is the identifier; Value is
// the literal string. Order of Values matches declaration order.
type EnumDefinition struct {
	TypeName   string
	Package    string
	Domain     string
	Qualified  string
	SourceFile string
	Values     []EnumValue
}

type EnumValue struct {
	ConstName string
	Value     string
}

func (a *Analyzer) extractEnums(f *ast.File) {
	filePath := a.fset.Position(f.Pos()).Filename
	pkgInfo := getPackageInfo(filePath, f.Name.Name)

	// Pass 1 — string-backed exported types declared in this file.
	stringTypes := map[string]bool{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !ast.IsExported(ts.Name.Name) {
				continue
			}
			if isStringUnderlying(ts.Type) {
				stringTypes[ts.Name.Name] = true
			}
		}
	}
	if len(stringTypes) == 0 {
		return
	}

	// Pass 2 — const groups that reference one of those types.
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}

		currentType := ""
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if vs.Type != nil {
				if id, ok := vs.Type.(*ast.Ident); ok {
					currentType = id.Name
				} else {
					currentType = ""
				}
			}
			if currentType == "" || !stringTypes[currentType] {
				continue
			}

			for i, nameIdent := range vs.Names {
				if !ast.IsExported(nameIdent.Name) {
					continue
				}
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val := strings.Trim(lit.Value, "`\"")

				key := StructKey{Domain: pkgInfo.Domain, Package: pkgInfo.Package, Name: currentType}
				enum, exists := a.enums[key]
				if !exists {
					enum = EnumDefinition{
						TypeName:   currentType,
						Package:    pkgInfo.Package,
						Domain:     pkgInfo.Domain,
						Qualified:  pkgInfo.Package + "." + currentType,
						SourceFile: filePath,
					}
				}
				enum.Values = append(enum.Values, EnumValue{ConstName: nameIdent.Name, Value: val})
				a.enums[key] = enum
			}
		}
	}
}

// isStringUnderlying reports whether t is the identifier "string".
func isStringUnderlying(t ast.Expr) bool {
	id, ok := t.(*ast.Ident)
	return ok && id.Name == "string"
}
