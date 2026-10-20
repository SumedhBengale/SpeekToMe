package privacylog

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestServerRuntimeCannotBypassPrivacyLogger(t *testing.T) {
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "privacylog" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		tree, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, item := range tree.Imports {
			value, _ := strconv.Unquote(item.Path.Value)
			if value == "log" || value == "log/slog" {
				t.Errorf("unfiltered logger import in %s", path)
			}
		}
		if strings.Contains(filepath.ToSlash(path), "/handlers/") {
			ast.Inspect(tree, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				base, ok := sel.X.(*ast.Ident)
				if ok && base.Name == "fmt" && (sel.Sel.Name == "Print" || sel.Sel.Name == "Printf" || sel.Sel.Name == "Println") {
					t.Errorf("unfiltered output in %s", path)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
