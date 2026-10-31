package analyzer

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type PackageInfo struct {
	ImportPath string
	Domain     string
	Package    string
}

var (
	moduleCache   = make(map[string]PackageInfo)
	moduleCacheMu sync.RWMutex
)

func getPackageInfo(filePath, pkgName string) PackageInfo {
	dir := filepath.Dir(filePath)
	moduleCacheMu.RLock()
	if info, ok := moduleCache[dir]; ok {
		moduleCacheMu.RUnlock()
		return info
	}
	moduleCacheMu.RUnlock()

	domain, importPath := pkgName, filepath.ToSlash(dir)
	curr := dir

	for {
		if data, err := os.ReadFile(filepath.Join(curr, "go.mod")); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					modPath := strings.TrimSpace(strings.TrimPrefix(line, "module"))
					rel, _ := filepath.Rel(curr, dir)
					if rel != "." && rel != "" {
						importPath = modPath + "/" + filepath.ToSlash(rel)
					} else {
						importPath = modPath
					}
					domain = filepath.Base(modPath)
					break
				}
			}
			break
		}
		parent := filepath.Dir(curr)
		if parent == curr || parent == "." {
			break
		}
		curr = parent
	}

	info := PackageInfo{ImportPath: importPath, Domain: domain, Package: pkgName}
	moduleCacheMu.Lock()
	moduleCache[dir] = info
	moduleCacheMu.Unlock()
	return info
}

func findLocalDependencies(baseDir string) []string {
	var dirs []string
	seen := map[string]bool{filepath.Clean(baseDir): true}

	if data, err := os.ReadFile(filepath.Join(baseDir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "replace ") {
				parts := strings.Fields(line)
				for i, p := range parts {
					if p == "=>" && i+1 < len(parts) {
						target := parts[i+1]
						if strings.HasPrefix(target, ".") || strings.HasPrefix(target, "/") {
							absTarget, _ := filepath.Abs(filepath.Join(baseDir, target))
							if !seen[absTarget] {
								if fi, err := os.Stat(absTarget); err == nil && fi.IsDir() {
									seen[absTarget] = true
									dirs = append(dirs, absTarget)
								}
							}
						}
					}
				}
			}
		}
	}
	return dirs
}

func collectFileImports(f *ast.File) map[string]string {
	imports := make(map[string]string)
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, "`\"")
		alias := filepath.Base(path)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		imports[alias] = path
	}
	return imports
}

func parseTypeRef(e ast.Expr, pkgInfo PackageInfo, imports map[string]string) TypeRef {
	if e == nil {
		return TypeRef{}
	}
	switch v := e.(type) {
	case *ast.StarExpr:
		return parseTypeRef(v.X, pkgInfo, imports)
	case *ast.Ident:
		if isBasicType(v.Name) {
			return TypeRef{Name: v.Name, Package: pkgInfo.Package, Domain: pkgInfo.Domain}
		}
		return TypeRef{Domain: pkgInfo.Domain, Package: pkgInfo.Package, ImportPath: pkgInfo.ImportPath, Name: v.Name}
	case *ast.SelectorExpr:
		pkgName := exprToString(v.X)
		importPath, domain := "", pkgName
		if imp, ok := imports[pkgName]; ok {
			importPath, domain = imp, filepath.Base(imp)
		}
		return TypeRef{ImportPath: importPath, Domain: domain, Package: pkgName, Name: v.Sel.Name}
	}
	s := exprToString(e)
	idx := strings.LastIndex(s, ".")
	if idx >= 0 {
		return TypeRef{Domain: s[:idx], Package: s[:idx], Name: s[idx+1:]}
	}
	return TypeRef{Name: strings.TrimPrefix(s, "*"), Package: pkgInfo.Package, Domain: pkgInfo.Domain}
}

func isBasicType(name string) bool {
	switch name {
	case "string", "int", "int32", "int64", "bool", "byte", "any", "error", "float32", "float64":
		return true
	}
	return false
}

func isBuildCall(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fun.Sel.Name == "Build"
	case *ast.IndexListExpr:
		if sel, ok := fun.X.(*ast.SelectorExpr); ok {
			return sel.Sel.Name == "Build"
		}
	}
	return false
}

func exprToString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.BasicLit:
		return v.Value
	case *ast.SelectorExpr:
		return exprToString(v.X) + "." + v.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(v.X)
	case *ast.ArrayType:
		return "[]" + exprToString(v.Elt)
	case *ast.MapType:
		return fmt.Sprintf("map[%s]%s", exprToString(v.Key), exprToString(v.Value))
	case *ast.CallExpr:
		var args []string
		for _, a := range v.Args {
			args = append(args, exprToString(a))
		}
		return fmt.Sprintf("%s(%s)", exprToString(v.Fun), strings.Join(args, ", "))
	case *ast.StructType:
		if v.Fields == nil || len(v.Fields.List) == 0 {
			return "struct{}"
		}
		return "struct{...}"
	}
	return ""
}

func cleanLiteral(s string) string { return strings.Trim(s, "\"`'") }
