package testifyhelpercheck

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "testifyhelpercheck",
	Doc:  "checks testify helper usage in tests",
	Run:  run,
}

const assertDiagnosticMessage = "test has %d direct testify package calls; create a local assert helper with assert := Assert.New(t) and use it for repeated checks"
const requireDiagnosticMessage = "test has %d direct testify package calls; create a local require helper with require := require.New(t) and use it for repeated checks"

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if !strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}

		imports := importNames(file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !isTestFuncDecl(fn, imports.testing) {
				continue
			}
			analyzeBody(pass, fn.Body, fn.Type.Params.List[0].Names[0].Name, imports)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if !ok || !isTestLikeFuncType(lit.Type, imports.testing) {
				return true
			}
			paramName := lit.Type.Params.List[0].Names[0].Name
			analyzeBody(pass, lit.Body, paramName, imports)
			return false
		})
	}

	return nil, nil
}

type importSet struct {
	testing map[string]struct{}
	assert  map[string]struct{}
	require map[string]struct{}
}

func importNames(file *ast.File) importSet {
	set := importSet{
		testing: make(map[string]struct{}),
		assert:  make(map[string]struct{}),
		require: make(map[string]struct{}),
	}

	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, "\"")
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		switch path {
		case "testing":
			if name == "" {
				name = "testing"
			}
			set.testing[name] = struct{}{}
		case "github.com/stretchr/testify/assert":
			if name == "" {
				name = "assert"
			}
			set.assert[name] = struct{}{}
		case "github.com/stretchr/testify/require":
			if name == "" {
				name = "require"
			}
			set.require[name] = struct{}{}
		}
	}

	return set
}

func isTestFuncDecl(fn *ast.FuncDecl, testingImports map[string]struct{}) bool {
	if fn.Body == nil || fn.Name == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
		return false
	}
	return isTestLikeFuncType(fn.Type, testingImports)
}

func isTestLikeFuncType(fnType *ast.FuncType, testingImports map[string]struct{}) bool {
	if fnType == nil || fnType.Params == nil || len(fnType.Params.List) != 1 {
		return false
	}
	field := fnType.Params.List[0]
	if len(field.Names) != 1 {
		return false
	}
	return isTestingT(field.Type, testingImports)
}

func isTestingT(expr ast.Expr, testingImports map[string]struct{}) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "T" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = testingImports[pkg.Name]
	return ok
}

func analyzeBody(pass *analysis.Pass, body *ast.BlockStmt, tName string, imports importSet) {
	if body == nil {
		return
	}

	hasAssertHelper := false
	hasRequireHelper := false
	var assertCallPositions []ast.Node
	var requireCallPositions []ast.Node

	var visit func(ast.Node)
	visit = func(n ast.Node) {
		switch node := n.(type) {
		case *ast.FuncLit:
			return
		case *ast.AssignStmt:
			for i, rhs := range node.Rhs {
				if i >= len(node.Lhs) {
					continue
				}
				if ident, ok := node.Lhs[i].(*ast.Ident); ok {
					if ident.Name == "assert" && isAssertNewCall(rhs, tName, imports.assert) {
						hasAssertHelper = true
					}
					if ident.Name == "require" && isRequireNewCall(rhs, tName, imports.require) {
						hasRequireHelper = true
					}
				}
			}
		case *ast.ValueSpec:
			for i, value := range node.Values {
				if i >= len(node.Names) {
					continue
				}
				if node.Names[i].Name == "assert" && isAssertNewCall(value, tName, imports.assert) {
					hasAssertHelper = true
				}
				if node.Names[i].Name == "require" && isRequireNewCall(value, tName, imports.require) {
					hasRequireHelper = true
				}
			}
		case *ast.CallExpr:
			switch directCallKind(node, imports.assert, imports.require) {
			case "assert":
				assertCallPositions = append(assertCallPositions, node)
			case "require":
				requireCallPositions = append(requireCallPositions, node)
			}
		}

		ast.Inspect(n, func(child ast.Node) bool {
			if child == nil || child == n {
				return true
			}
			if _, ok := child.(*ast.FuncLit); ok {
				return false
			}
			visit(child)
			return false
		})
	}

	for _, stmt := range body.List {
		visit(stmt)
	}

	total := len(assertCallPositions) + len(requireCallPositions)
	if total <= 3 {
		return
	}

	hasAnyHelper := hasAssertHelper || hasRequireHelper

	if len(assertCallPositions) >= 2 && !hasAssertHelper {
		pass.Reportf(assertCallPositions[len(assertCallPositions)-1].Pos(), assertDiagnosticMessage, total)
		return
	}

	if !hasAnyHelper {
		pos := requireCallPositions[len(requireCallPositions)-1].Pos()
		msg := requireDiagnosticMessage
		if len(requireCallPositions) == 0 {
			pos = assertCallPositions[len(assertCallPositions)-1].Pos()
			msg = assertDiagnosticMessage
		}
		pass.Reportf(pos, msg, total)
		return
	}
}

func isAssertNewCall(expr ast.Expr, tName string, assertImports map[string]struct{}) bool {
	return isHelperNewCall(expr, tName, assertImports)
}

func isRequireNewCall(expr ast.Expr, tName string, requireImports map[string]struct{}) bool {
	return isHelperNewCall(expr, tName, requireImports)
}

func isHelperNewCall(expr ast.Expr, tName string, imports map[string]struct{}) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "New" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if _, ok := imports[pkg.Name]; !ok {
		return false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	return ok && arg.Name == tName
}

func directCallKind(call *ast.CallExpr, assertImports, requireImports map[string]struct{}) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	if _, ok := assertImports[pkg.Name]; ok {
		if sel.Sel.Name == "New" {
			return ""
		}
		return "assert"
	}
	if _, ok := requireImports[pkg.Name]; ok {
		if sel.Sel.Name == "New" {
			return ""
		}
		return "require"
	}
	return ""
}
