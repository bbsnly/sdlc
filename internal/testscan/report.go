package testscan

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/types"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
)

// e2eDir is where the tests that drive the built binary are.
const e2eDir = "test/e2e"

// counts is the tests of one directory; js is whether any are JavaScript.
type counts struct {
	tests int
	js    bool
}

// class says how a directory's tests are told apart in the report.
func (c counts) class(dir string) string {
	switch {
	case dir == e2eDir:
		return "e2e"
	case c.js || strings.HasPrefix(dir, "test/"):
		return "other"
	}
	return "unit"
}

// Report writes what no test in test/e2e runs, from the sources alone: a line
// "command <path>" for each command, "gate <key>" for each gate record key, and
// then "<class> <count> <package>" for the tests of every directory. Nothing is
// run, and nothing is written when the sources cannot be read.
//
// A test runs a command when the command's words stand as separate string
// literals among the arguments of a call, or as the elements of a []string
// literal; the arguments of a call to something named refus... are left out,
// because a helper of that name expects the command to be refused. This reads
// the syntax tree, so it can credit a string that merely looks like a run
// (strings.Contains(out, "status")); the guard in test/e2e, which records real
// runs, is the check, and this is the picture.
func Report(root string, w io.Writer) error {
	names, err := files(root)
	if err != nil {
		return err
	}
	sc := &scan{
		commands: tree(),
		ran:      map[string]bool{},
		passed:   map[model.Gate]bool{},
		dirs:     map[string]counts{},
	}
	for _, rel := range names {
		dir := path.Dir(rel)
		c := sc.dirs[dir]
		switch {
		case strings.HasSuffix(rel, ".test.js"):
			c = sc.countJS(root, rel, c)
		case isTestFile(rel):
			f, err := parse(root, rel)
			if err != nil {
				return err
			}
			c = sc.countGo(f, dir, c)
		default:
			continue
		}
		sc.dirs[dir] = c
	}
	_, werr := io.WriteString(w, sc.render())
	return cmp.Or(sc.jsErr, werr)
}

// scan is what Report has read so far: the commands and gate keys the
// end-to-end tests run, and the tests counted per directory.
type scan struct {
	commands *cobra.Command
	ran      map[string]bool
	passed   map[model.Gate]bool
	dirs     map[string]counts
	jsErr    error
}

// note takes one list of literals: the command it names is the deepest one
// from the first word that starts one, so leading words (a role, a document)
// do not hide it, and the word after "gate" is a gate record key.
func (sc *scan) note(words []string) {
	for i := range words {
		if c, ok := resolve(sc.commands, words[i:]); ok {
			sc.ran[c] = true
			break
		}
	}
	for i, word := range words[:max(len(words)-1, 0)] {
		if word == "gate" {
			sc.passed[model.Gate(words[i+1])] = true
		}
	}
}

// countJS adds the tests of one JavaScript file to c.
func (sc *scan) countJS(root, rel string, c counts) counts {
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	sc.jsErr = cmp.Or(sc.jsErr, err)
	for line := range strings.SplitSeq(string(src), "\n") {
		if strings.HasPrefix(line, "test(") {
			c.tests++
		}
	}
	c.js = true
	return c
}

// countGo adds the tests of one parsed Go test file to c, and, for the
// end-to-end directory, notes the commands the file runs.
func (sc *scan) countGo(f *ast.File, dir string, c counts) counts {
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && isTest(fn.Name.Name) {
			c.tests++
		}
	}
	if dir == e2eDir {
		sc.noteRuns(f)
	}
	return c
}

// noteRuns notes the commands one end-to-end test file runs.
func (sc *scan) noteRuns(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if strings.HasPrefix(calleeName(n), "refus") {
				return false
			}
			sc.note(literals(n.Args))
		case *ast.CompositeLit:
			if isStringSlice(n.Type) {
				sc.note(literals(n.Elts))
			}
		}
		return true
	})
}

// render is the report: the commands and gates no test runs, then the count
// per directory.
func (sc *scan) render() string {
	var out strings.Builder
	for _, c := range Commands() {
		if !sc.ran[c] {
			fmt.Fprintf(&out, "command %s\n", c)
		}
	}
	for _, g := range model.Gates {
		if !sc.passed[g] {
			fmt.Fprintf(&out, "gate %s\n", g)
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(sc.dirs)) {
		fmt.Fprintf(&out, "%s %d %s\n", sc.dirs[dir].class(dir), sc.dirs[dir].tests, dir)
	}
	return out.String()
}

// isTest reports whether a top-level function name is a Go test: Test followed
// by nothing or by something that is not a lower-case letter, and not TestMain.
func isTest(name string) bool {
	rest, ok := strings.CutPrefix(name, "Test")
	r, _ := utf8.DecodeRuneInString(rest)
	return ok && name != "TestMain" && !unicode.IsLower(r)
}

// calleeName is the name a call is made by, "" for a callee that has none (a
// function literal, an index expression). Such a call is read like any other,
// never taken for a refusal.
func calleeName(call *ast.CallExpr) string {
	name := ""
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		name = fn.Name
	case *ast.SelectorExpr:
		name = fn.Sel.Name
	}
	return name
}

// isStringSlice reports whether a composite literal's type is []string.
func isStringSlice(t ast.Expr) bool {
	return t != nil && types.ExprString(t) == "[]string"
}

// literals are the string literals among exprs, unquoted, in order.
func literals(exprs []ast.Expr) []string {
	var out []string
	for _, e := range exprs {
		if lit, ok := e.(*ast.BasicLit); ok {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, s)
			}
		}
	}
	return out
}
