package parallel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// AC-7: sdlc itself makes no network call, runs no push and builds no process
// of its own: the one thing it starts is the session, through internal/shellx,
// which is what an interrupt can stop. Read from the source, because a launcher
// that did any of it would look the same from outside until the day it ran.
func TestAC7TheLauncherHasNoNetworkNoPushAndNoProcessOfItsOwn(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			switch path {
			case "net", "net/http", "net/rpc", "net/smtp":
				t.Errorf("%s imports %s", name, path)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if pkg, ok := n.X.(*ast.Ident); ok && pkg.Name == "exec" && (n.Sel.Name == "Command" || n.Sel.Name == "CommandContext") {
					t.Errorf("%s builds a process with exec.%s: sessions go through shellx.Command", name, n.Sel.Name)
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING && strings.Contains(strings.ToLower(n.Value), "git push") {
					t.Errorf("%s has a string that pushes: %s", name, n.Value)
				}
			}
			return true
		})
	}
	if checked == 0 {
		t.Error("the package has no source: internal/parallel is where the launcher is")
	}
}
