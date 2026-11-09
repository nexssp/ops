package core

import (
	"go/ast"
	"strconv"
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
}

func (a *Analyzer) inspectPipeline(rootCall *ast.CallExpr, filePath string, pkgInfo PackageInfo, fileImports map[string]string) ActionContract {
	contract := ActionContract{Scope: "public", SourceFile: filePath, SourceLine: a.fset.Position(rootCall.Pos()).Line}
	curr := rootCall

	for curr != nil {
		method, receiver, typeIndices := actionCall(curr)
		if method == "" {
			break
		}

		args := curr.Args
		switch method {
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
				for _, binding := range detectBindings(arg) {
					contract.Transports = append(contract.Transports, binding)
					applyLegacyHTTPFields(&contract, binding)
				}
			}
		case "Public":
			contract.Scope = "public"
		case "Internal":
			contract.Scope = "internal"
		case "System":
			contract.Scope = "system"
		}

		if receiverCall, ok := receiver.(*ast.CallExpr); ok {
			curr = receiverCall
		} else if indexList, ok := receiver.(*ast.IndexListExpr); ok {
			if receiverCall, ok := indexList.X.(*ast.CallExpr); ok {
				curr = receiverCall
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

func actionCall(call *ast.CallExpr) (string, ast.Expr, []ast.Expr) {
	switch function := call.Fun.(type) {
	case *ast.SelectorExpr:
		if indexList, ok := function.X.(*ast.IndexListExpr); ok {
			return function.Sel.Name, indexList, indexList.Indices
		}
		return function.Sel.Name, function.X, nil
	case *ast.IndexExpr:
		if selector, ok := function.X.(*ast.SelectorExpr); ok {
			return selector.Sel.Name, selector.X, []ast.Expr{function.Index}
		}
		if identifier, ok := function.X.(*ast.Ident); ok && identifier.Name == "New" {
			return identifier.Name, nil, []ast.Expr{function.Index}
		}
	case *ast.IndexListExpr:
		if selector, ok := function.X.(*ast.SelectorExpr); ok {
			return selector.Sel.Name, selector.X, function.Indices
		}
		if identifier, ok := function.X.(*ast.Ident); ok && identifier.Name == "New" {
			return identifier.Name, nil, function.Indices
		}
	case *ast.Ident:
		if function.Name == "New" {
			return function.Name, nil, nil
		}
	}
	return "", nil, nil
}

func applyLegacyHTTPFields(contract *ActionContract, binding TransportBinding) {
	switch binding.Kind {
	case "http":
		contract.Method = binding.Method
		contract.Path = binding.Path
		contract.RouteSource = "go"
	case "sse":
		contract.Method = "SSE"
		contract.Path = binding.Path
		contract.RouteSource = "go"
		contract.Streaming = true
	case "raw":
		contract.Method = binding.Method
		contract.Path = binding.Path
		contract.RouteSource = "go"
		contract.Raw = true
	}
}

func hasHTTPLikeBinding(bindings []TransportBinding) bool {
	for _, binding := range bindings {
		switch binding.Kind {
		case "http", "sse", "raw":
			return true
		}
	}
	return false
}
