package testifyhelpercheck

import (
	"go/ast"
	"go/types"
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
}

func importNames(file *ast.File) importSet {
	set := importSet{
		testing: make(map[string]struct{}),
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

type bodyAnalysis struct {
	pass                 *analysis.Pass
	tName                string
	assertHelper         helperState
	requireHelper        helperState
	assertCallPositions  []ast.Node
	requireCallPositions []ast.Node
}

func analyzeBody(pass *analysis.Pass, body *ast.BlockStmt, tName string, imports importSet) {
	if body == nil {
		return
	}

	analysis := &bodyAnalysis{pass: pass, tName: tName}
	for _, stmt := range body.List {
		analysis.visit(stmt)
	}
	analysis.report()
}

func (a *bodyAnalysis) visit(n ast.Node) {
	switch node := n.(type) {
	case *ast.FuncLit:
		return
	case *ast.AssignStmt:
		a.recordAssign(node)
	case *ast.ValueSpec:
		a.recordValueSpec(node)
	case *ast.CallExpr:
		a.recordCall(node)
	}

	ast.Inspect(n, func(child ast.Node) bool {
		if child == nil || child == n {
			return true
		}
		if _, ok := child.(*ast.FuncLit); ok {
			return false
		}
		a.visit(child)
		return false
	})
}

func (a *bodyAnalysis) recordAssign(node *ast.AssignStmt) {
	for i, rhs := range node.Rhs {
		if i >= len(node.Lhs) {
			continue
		}
		ident, ok := node.Lhs[i].(*ast.Ident)
		if !ok {
			continue
		}
		a.recordHelperIdent(ident, rhs)
	}
}

func (a *bodyAnalysis) recordValueSpec(node *ast.ValueSpec) {
	for i, value := range node.Values {
		if i >= len(node.Names) {
			continue
		}
		a.recordHelperIdent(node.Names[i], value)
	}
}

func (a *bodyAnalysis) recordHelperIdent(ident *ast.Ident, value ast.Expr) {
	if ident.Name == "assert" && isAssertNewCall(a.pass, value, a.tName) {
		a.assertHelper.add(identObject(a.pass, ident))
	}
	if ident.Name == "require" && isRequireNewCall(a.pass, value, a.tName) {
		a.requireHelper.add(identObject(a.pass, ident))
	}
}

func (a *bodyAnalysis) recordCall(node *ast.CallExpr) {
	switch callKind(a.pass, node, a.assertHelper, a.requireHelper) {
	case "assert":
		a.assertCallPositions = append(a.assertCallPositions, node)
	case "require":
		a.requireCallPositions = append(a.requireCallPositions, node)
	case "assert-helper":
		a.assertHelper.used = true
	case "require-helper":
		a.requireHelper.used = true
	}
}

func (a *bodyAnalysis) report() {
	total := len(a.assertCallPositions) + len(a.requireCallPositions)
	if total <= 3 {
		return
	}
	hasAssertHelper := len(a.assertHelper.objs) > 0 && a.assertHelper.used
	hasRequireHelper := len(a.requireHelper.objs) > 0 && a.requireHelper.used
	if len(a.assertCallPositions) >= 2 && !hasAssertHelper {
		a.pass.Reportf(a.assertCallPositions[len(a.assertCallPositions)-1].Pos(), assertDiagnosticMessage, total)
	}
	if len(a.requireCallPositions) >= 2 && !hasRequireHelper {
		a.pass.Reportf(a.requireCallPositions[len(a.requireCallPositions)-1].Pos(), requireDiagnosticMessage, total)
	}
}

type helperState struct {
	objs map[types.Object]struct{}
	used bool
}

func (s *helperState) add(obj types.Object) {
	if obj == nil {
		return
	}
	if s.objs == nil {
		s.objs = make(map[types.Object]struct{})
	}
	s.objs[obj] = struct{}{}
}

func (s helperState) has(obj types.Object) bool {
	if obj == nil {
		return false
	}
	_, ok := s.objs[obj]
	return ok
}

func identObject(pass *analysis.Pass, ident *ast.Ident) types.Object {
	if ident == nil {
		return nil
	}
	if obj := pass.TypesInfo.Defs[ident]; obj != nil {
		return obj
	}
	return pass.TypesInfo.Uses[ident]
}

func isAssertNewCall(pass *analysis.Pass, expr ast.Expr, tName string) bool {
	return isHelperNewCall(pass, expr, tName, "github.com/stretchr/testify/assert")
}

func isRequireNewCall(pass *analysis.Pass, expr ast.Expr, tName string) bool {
	return isHelperNewCall(pass, expr, tName, "github.com/stretchr/testify/require")
}

func isHelperNewCall(pass *analysis.Pass, expr ast.Expr, tName string, importPath string) bool {
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
	pkgObj, ok := pass.TypesInfo.Uses[pkg].(*types.PkgName)
	if !ok || pkgObj.Imported().Path() != importPath {
		return false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	return ok && arg.Name == tName
}

func callKind(pass *analysis.Pass, call *ast.CallExpr, assertHelper, requireHelper helperState) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	obj := pass.TypesInfo.Uses[pkg]
	switch {
	case assertHelper.has(obj):
		return "assert-helper"
	case requireHelper.has(obj):
		return "require-helper"
	}

	pkgObj, ok := obj.(*types.PkgName)
	if !ok || sel.Sel.Name == "New" {
		return ""
	}

	switch pkgObj.Imported().Path() {
	case "github.com/stretchr/testify/assert":
		return "assert"
	case "github.com/stretchr/testify/require":
		return "require"
	default:
		return ""
	}
}
