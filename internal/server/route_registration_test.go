package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIRoutesUseHumaRegistration(t *testing.T) {
	require := require.New(t)

	matches, err := rawAPIRouteRegistrations()
	require.NoError(err)
	require.Empty(
		matches,
		"raw API routes should be registered through Huma: "+
			strings.Join(matches, ", "),
	)
}

func rawAPIRouteRegistrations() ([]string, error) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		pathMatches, err := rawAPIRouteRegistrationsInFile(path)
		if err != nil {
			return nil, err
		}
		matches = append(matches, pathMatches...)
	}
	return matches, nil
}

func rawAPIRouteRegistrationsInFile(path string) ([]string, error) {
	source, err := os.ReadFile(filepath.Join(path))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(path), source, 0)
	if err != nil {
		return nil, err
	}
	var matches []string
	ast.Inspect(file, func(node ast.Node) bool {
		match, ok := rawAPIRouteRegistration(path, node)
		if ok {
			matches = append(matches, match)
		}
		return true
	})
	return matches, nil
}

func rawAPIRouteRegistration(path string, node ast.Node) (string, bool) {
	call, ok := node.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !isRawMuxRegistration(selector) {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	route, err := strconv.Unquote(lit.Value)
	if err != nil || !strings.Contains(route, "/api/") {
		return "", false
	}
	return path + ":" + route, true
}

func isRawMuxRegistration(selector *ast.SelectorExpr) bool {
	if selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc" {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == "mux"
}
