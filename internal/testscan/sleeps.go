package testscan

import (
	"cmp"
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

// Sleep is one function that calls time.Sleep, and when it is on a list, why.
type Sleep struct{ File, Func, Reason string }

// Sleeps is every function in a test file under root that calls time.Sleep,
// once each. The files are parsed, so a sleep in a comment or a string is not a
// call; a closure counts as the declaration it is written in.
func Sleeps(root string) ([]Sleep, error) {
	names, err := files(root)
	if err != nil {
		return nil, err
	}
	var out []Sleep
	for _, rel := range names {
		if !isTestFile(rel) {
			continue
		}
		f, err := parse(root, rel)
		if err != nil {
			return nil, err
		}
		alias := ""
		for _, imp := range f.Imports {
			if imp.Path.Value == `"time"` {
				alias = "time"
				if imp.Name != nil {
					alias = imp.Name.Name
				}
			}
		}
		for _, decl := range f.Decls {
			name := "(package level)"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				name = fn.Name.Name
				if fn.Recv != nil {
					recv, _, _ := strings.Cut(strings.TrimLeft(types.ExprString(fn.Recv.List[0].Type), "*"), "[")
					name = recv + "." + name
				}
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && calls(call, alias, "Sleep") {
					out = append(out, Sleep{File: rel, Func: name})
				}
				return true
			})
		}
	}
	slices.SortFunc(out, func(a, b Sleep) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Func, b.Func))
	})
	return slices.Compact(out), nil
}

// calls reports whether call is pkg.name(...).
func calls(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// CheckSleeps is what is wrong with the sleeps under root against the list
// given: a sleep that is not on it, an entry that matches none, and an entry
// without a reason. Nothing is returned when the list is the sleeps there are.
func CheckSleeps(root string, allowed []Sleep) ([]string, error) {
	found, err := Sleeps(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range found {
		if !slices.ContainsFunc(allowed, func(a Sleep) bool { return a.File == s.File && a.Func == s.Func }) {
			out = append(out, s.File+": "+s.Func+" calls time.Sleep and is not on the list")
		}
	}
	for _, a := range allowed {
		switch {
		case !slices.ContainsFunc(found, func(s Sleep) bool { return a.File == s.File && a.Func == s.Func }):
			out = append(out, a.File+": "+a.Func+" is on the list but calls no time.Sleep")
		case strings.TrimSpace(a.Reason) == "":
			out = append(out, a.File+": "+a.Func+" is on the list without a reason")
		}
	}
	slices.Sort(out)
	return out, nil
}
