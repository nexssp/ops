package analyzer

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"time"
)

func (a *Analyzer) extractActions(f *ast.File) {
	filePath := a.fset.Position(f.Pos()).Filename
	pkgInfo := getPackageInfo(filePath, f.Name.Name)
	fileImports := collectFileImports(f)

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isBuildCall(call) {
			return true
		}

		contract := a.inspectPipeline(call, filePath, pkgInfo, fileImports)
		if contract.Name != "" {
			a.actions = append(a.actions, contract)
		}
		return true
	})

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, val := range vs.Values {
				lit, ok := val.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				rawDSL := strings.Trim(lit.Value, "`\"")
				if strings.Contains(rawDSL, "->") || strings.Contains(rawDSL, ":route=") {
					a.mergeDSLManifest(rawDSL, filePath)
				}
			}
		}
	}
}

func (a *Analyzer) inspectPipeline(rootCall *ast.CallExpr, filePath string, pkgInfo PackageInfo, fileImports map[string]string) ActionContract {
	contract := ActionContract{Scope: "public", SourceFile: filePath, SourceLine: a.fset.Position(rootCall.Pos()).Line}
	curr := rootCall

	for curr != nil {
		var sel *ast.SelectorExpr
		var typeIndices []ast.Expr

		switch fun := curr.Fun.(type) {
		case *ast.SelectorExpr:
			sel = fun
			if idxList, ok := sel.X.(*ast.IndexListExpr); ok {
				typeIndices = idxList.Indices
			}
		case *ast.IndexListExpr:
			if s, ok := fun.X.(*ast.SelectorExpr); ok {
				sel = s
				typeIndices = fun.Indices
			}
		}

		if sel == nil {
			break
		}

		args := curr.Args
		switch sel.Sel.Name {
		case "New":
			if len(args) > 0 {
				contract.Name = cleanLiteral(exprToString(args[0]))
			}
			if len(args) > 1 {
				if funcLit, ok := args[1].(*ast.FuncLit); ok {
					var reqRef, resRef TypeRef
					if funcLit.Type.Params != nil {
						for _, p := range funcLit.Type.Params.List {
							tStr := exprToString(p.Type)
							if tStr != "context.Context" && tStr != "Context" {
								reqRef = parseTypeRef(p.Type, pkgInfo, fileImports)
								break
							}
						}
					}
					if funcLit.Type.Results != nil && len(funcLit.Type.Results.List) > 0 {
						resRef = parseTypeRef(funcLit.Type.Results.List[0].Type, pkgInfo, fileImports)
					}
					contract.ReqType = reqRef
					contract.ResType = resRef
					contract.ReqTypeName = reqRef.String()
					contract.ResTypeName = resRef.String()
				}
			}
			if len(typeIndices) >= 2 {
				contract.ReqType = parseTypeRef(typeIndices[0], pkgInfo, fileImports)
				contract.ResType = parseTypeRef(typeIndices[1], pkgInfo, fileImports)
				contract.ReqTypeName = contract.ReqType.String()
				contract.ResTypeName = contract.ResType.String()
			}

			// Blob response: the declared response type is []byte.
			// The endpoint generator emits a Blob-returning client method
			// instead of the default JSON method.
			if contract.ResTypeName == "[]byte" {
				contract.BlobResponse = true
			}
		case "Description":
			if len(args) > 0 {
				contract.Description = cleanLiteral(exprToString(args[0]))
			}
		case "Tag":
			for _, arg := range args {
				contract.Tags = append(contract.Tags, cleanLiteral(exprToString(arg)))
			}
		case "Idempotent":
			contract.Idempotent = true
		case "Timeout":
			if len(args) > 0 {
				tStr := exprToString(args[0])
				if d, err := time.ParseDuration(cleanLiteral(tStr)); err == nil {
					contract.Timeout = d
				}
			}
		case "Retry", "RetryAll":
			if len(args) > 0 {
				if n, err := strconv.Atoi(exprToString(args[0])); err == nil {
					contract.RetryMax = n
				}
			}
		case "ConcurrencyLimit":
			if len(args) > 0 {
				if n, err := strconv.ParseInt(exprToString(args[0]), 10, 32); err == nil {
					contract.ConcurrencyLimit = int32(n)
				}
			}
		case "RequireRole":
			if len(args) > 0 {
				contract.Roles = append(contract.Roles, cleanLiteral(exprToString(args[0])))
			}
		case "RequirePermission":
			if len(args) > 0 {
				contract.Permissions = append(contract.Permissions, cleanLiteral(exprToString(args[0])))
			}
		case "RequireFeature":
			for _, arg := range args {
				contract.Features = append(contract.Features, cleanLiteral(exprToString(arg)))
			}
		case "Route":
			for _, arg := range args {
				for _, b := range detectBindings(arg) {
					contract.Transports = append(contract.Transports, b)
					applyLegacyHTTPFields(&contract, b)
				}
			}
		case "Public":
			contract.Scope = "public"
		case "Internal":
			contract.Scope = "internal"
		case "System":
			contract.Scope = "system"
		}

		if nextCall, ok := sel.X.(*ast.CallExpr); ok {
			curr = nextCall
		} else if idxList, ok := sel.X.(*ast.IndexListExpr); ok {
			if nextCall, ok := idxList.X.(*ast.CallExpr); ok {
				curr = nextCall
			} else {
				break
			}
		} else {
			break
		}
	}

	if len(contract.Transports) > 0 && !hasHTTPLikeBinding(contract.Transports) {
		contract.Method = ""
	}

	return contract
}

// applyLegacyHTTPFields mirrors HTTP/SSE/Raw bindings onto the legacy
// Method/Path/Streaming/Raw fields used by the TypeScript SDK generator. Last
// HTTP-like binding wins, matching the historical single-route semantics.
func applyLegacyHTTPFields(c *ActionContract, b TransportBinding) {
	switch b.Kind {
	case "http":
		c.Method = b.Method
		c.Path = b.Path
		c.RouteSource = "go"
	case "sse":
		c.Method = "SSE"
		c.Path = b.Path
		c.RouteSource = "go"
		c.Streaming = true
	case "raw":
		c.Method = b.Method
		c.Path = b.Path
		c.RouteSource = "go"
		c.Raw = true
	}
}

func hasHTTPLikeBinding(bindings []TransportBinding) bool {
	for _, b := range bindings {
		switch b.Kind {
		case "http", "sse", "raw":
			return true
		}
	}
	return false
}
