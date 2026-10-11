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
	commands := tree()
	ran := map[string]bool{}
	passed := map[model.Gate]bool{}
	dirs := map[string]counts{}
	var jsErr error

	// note takes one list of literals: the command it names is the deepest one
	// from the first word that starts one, so leading words (a role, a document)
	// do not hide it, and the word after "gate" is a gate record key.
	note := func(words []string) {
		for i := range words {
			if c, ok := resolve(commands, words[i:]); ok {
				ran[c] = true
				break
			}
		}
		for i, word := range words[:max(len(words)-1, 0)] {
			if word == "gate" {
				passed[model.Gate(words[i+1])] = true
			}
		}
	}

	for _, rel := range names {
		dir := path.Dir(rel)
		c := dirs[dir]
		switch {
		case strings.HasSuffix(rel, ".test.js"):
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			jsErr = cmp.Or(jsErr, err)
			for _, line := range strings.Split(string(src), "\n") {
				if strings.HasPrefix(line, "test(") {
					c.tests++
				}
			}
			c.js = true
		case isTestFile(rel):
			f, err := parse(root, rel)
			if err != nil {
				return err
			}
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && isTest(fn.Name.Name) {
					c.tests++
				}
			}
			if dir == e2eDir {
				ast.Inspect(f, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.CallExpr:
						if strings.HasPrefix(calleeName(n), "refus") {
							return false
						}
						note(literals(n.Args))
					case *ast.CompositeLit:
						if isStringSlice(n.Type) {
							note(literals(n.Elts))
						}
					}
					return true
				})
			}
		default:
			continue
		}
		dirs[dir] = c
	}

	var out strings.Builder
	for _, c := range Commands() {
		if !ran[c] {
			fmt.Fprintf(&out, "command %s\n", c)
		}
	}
	for _, g := range model.Gates {
		if !passed[g] {
			fmt.Fprintf(&out, "gate %s\n", g)
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		fmt.Fprintf(&out, "%s %d %s\n", dirs[dir].class(dir), dirs[dir].tests, dir)
	}
	_, werr := io.WriteString(w, out.String())
	return cmp.Or(jsErr, werr)
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
