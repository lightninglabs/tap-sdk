package tapsdk

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const taprootAssetsModule = "github.com/lightninglabs/taproot-assets"

// TestExportedSurfaceAvoidsTaprootAssetsTypes ensures the public SDK
// surface only exposes SDK-owned types, even if internal adapters still
// translate taproot-assets types behind the boundary.
func TestExportedSurfaceAvoidsTaprootAssetsTypes(t *testing.T) {
	repoRoot := testRepoRoot(t)

	violations, err := exportedSurfaceViolations(repoRoot)
	require.NoError(t, err)
	require.Empty(t, violations, strings.Join(violations, "\n"))
}

func testRepoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Dir(file)
}

func exportedSurfaceViolations(root string) ([]string, error) {
	fset := token.NewFileSet()
	var violations []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry,
		err error) error {

		if err != nil {
			return err
		}

		if d.IsDir() {
			if shouldSkipDir(root, path, d.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(path) != ".go" ||
			strings.HasSuffix(path, "_test.go") {

			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		aliases, importViolations := importAliases(file)
		violations = append(
			violations, formatViolations(fset, path, importViolations)...,
		)

		for _, decl := range file.Decls {
			violations = append(
				violations, declViolations(fset, path, decl, aliases)...,
			)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(violations)

	return violations, nil
}

func shouldSkipDir(root, path, name string) bool {
	if path == root {
		return false
	}

	if strings.HasPrefix(name, ".") {
		return true
	}

	switch name {
	case "docs", "itest", "testdata":
		return true
	default:
		return false
	}
}

func importAliases(file *ast.File) (map[string]string, []token.Pos) {
	aliases := make(map[string]string)
	var dotImportViolations []token.Pos

	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"")

		if imp.Name != nil {
			switch imp.Name.Name {
			case "_":
				continue
			case ".":
				if strings.HasPrefix(path, taprootAssetsModule) {
					dotImportViolations = append(
						dotImportViolations, imp.Pos(),
					)
				}
				continue
			default:
				aliases[imp.Name.Name] = path
				continue
			}
		}

		aliases[filepath.Base(path)] = path
	}

	return aliases, dotImportViolations
}

func declViolations(fset *token.FileSet, path string, decl ast.Decl,
	aliases map[string]string) []string {

	var violations []string

	switch d := decl.(type) {
	case *ast.FuncDecl:
		if !d.Name.IsExported() {
			return nil
		}

		positions := exprViolations(d.Type, aliases)
		return formatViolations(fset, path, positions)

	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				if !s.Name.IsExported() {
					continue
				}

				positions := exprViolations(s.Type, aliases)
				violations = append(
					violations,
					formatViolations(fset, path, positions)...,
				)

			case *ast.ValueSpec:
				if !hasExportedName(s.Names) || s.Type == nil {
					continue
				}

				positions := exprViolations(s.Type, aliases)
				violations = append(
					violations,
					formatViolations(fset, path, positions)...,
				)
			}
		}
	}

	return violations
}

func hasExportedName(names []*ast.Ident) bool {
	for _, name := range names {
		if name.IsExported() {
			return true
		}
	}

	return false
}

func exprViolations(node ast.Node, aliases map[string]string) []token.Pos {
	var positions []token.Pos

	ast.Inspect(node, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}

		importPath, ok := aliases[pkg.Name]
		if !ok {
			return true
		}

		if strings.HasPrefix(importPath, taprootAssetsModule) {
			positions = append(positions, sel.Pos())
		}

		return true
	})

	return positions
}

func formatViolations(fset *token.FileSet, path string,
	positions []token.Pos) []string {

	violations := make([]string, 0, len(positions))
	for _, pos := range positions {
		location := fset.Position(pos)
		violations = append(violations, fmt.Sprintf(
			"%s:%d:%d references taproot-assets in an exported "+
				"declaration",
			path, location.Line, location.Column,
		))
	}

	return violations
}
