// Package testscan reads this repository's tests and command tree without
// running either. It is development tooling: test/e2e uses it to hold that every
// command has a case that ran it, and internal/tools/testreport prints what no
// such case runs. Nothing the released binary links imports it.
package testscan

import (
	"cmp"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/cli"
)

// atRoot are directories left alone only at the top of the module: .sdlc holds
// whole older copies of the repository under worktrees/, and the others are
// output or vendored code. Further down the same names are real packages, whose
// tests have to be seen.
var atRoot = []string{".git", ".sdlc", "node_modules", "dist"}

// files lists every file under root, slash-separated and relative to it. A
// directory that cannot be read is an error: a scan that skipped it would pass
// on exactly the files it could not see. testdata is skipped at any depth, as
// the go tool does.
func files(root string) ([]string, error) {
	var out []string
	err := fs.WalkDir(os.DirFS(root), ".", func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == "testdata" || slices.Contains(atRoot, path)):
			return fs.SkipDir
		case !d.IsDir():
			out = append(out, path)
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

// parse reads and parses one file of root. The read error comes back ahead of
// the parse error it causes (an empty source), without a branch of its own that
// no test can reach.
func parse(root, rel string) (*ast.File, error) {
	src, readErr := fs.ReadFile(os.DirFS(root), rel)
	f, parseErr := parser.ParseFile(token.NewFileSet(), rel, string(src), parser.SkipObjectResolution)
	return f, cmp.Or(readErr, parseErr)
}

// isTestFile reports whether rel is a Go test file.
func isTestFile(rel string) bool { return strings.HasSuffix(rel, "_test.go") }

// tree is the command tree a person sees, built fresh because Find sets up
// flags on the commands it passes and tests may call Resolve at the same time.
func tree() *cobra.Command {
	return cli.New(strings.NewReader(""), io.Discard, io.Discard)
}
