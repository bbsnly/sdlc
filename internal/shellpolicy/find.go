package shellpolicy

import (
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// find picks the files it changes by a name, and a name is no path the rules
// read: `find . -name active -delete` removed the loop's record, and
// `find . -path '*state/tests.lock' -delete` the freeze.

// stateFiles are the files the store keeps in .sdlc/state, which only a name
// can pick out of the directory.
var stateFiles = []string{
	".sdlc/state/active", ".sdlc/state/session", ".sdlc/state/tests.lock", ".sdlc/state/stop-blocks.json",
	".sdlc/state/acknowledged",
}

// findArguments are the directories find starts from, and the patterns it picks
// files with: a -name is matched against a file's name, a -path against the
// whole of the path find comes to it by.
func findArguments(args []string) (roots, names, paths []string) {
	i := 0
	// The options that come before the directories: -H, -L, -P, -O3, -D.
	for i < len(args) && (args[i] == "-H" || args[i] == "-L" || args[i] == "-P" || args[i] == "-D" ||
		strings.HasPrefix(args[i], "-O")) {
		i++
	}
	// Then the directories, up to the first option. A `(` or `!` taken for one
	// is no directory that reaches anything.
	for ; i < len(args) && !strings.HasPrefix(args[i], "-"); i++ {
		roots = append(roots, args[i])
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}
	for ; i+1 < len(args); i++ {
		switch args[i] {
		case "-name", "-iname":
			names = append(names, unquote(args[i+1]))
			i++
		case "-path", "-ipath", "-wholename", "-iwholename":
			paths = append(paths, unquote(args[i+1]))
			i++
		}
	}
	return roots, names, paths
}

// reaches reports whether find, started at root from dir, can come to the
// project's own files: from the project, from a directory above it, or from a
// directory this cannot place.
func reaches(root, dir string, s State, m *memo) bool {
	r := path.Join(dir, clean(root))
	if isAbsolute(clean(root)) {
		r = clean(root)
	}
	if strings.ContainsAny(r, "$%~*?[{") {
		return true
	}
	if isAbsolute(r) {
		if s.Resolve == nil {
			return true
		}
		// Outside is also above.
		rel := m.resolve(s, r)
		return rel == "" || rel == "."
	}
	for _, segment := range strings.Split(r, "/") {
		if segment != "." && segment != ".." {
			return false
		}
	}
	return true
}

// foundBy is the paths among shapes that find picks with these patterns from
// these roots: by the name a path ends in, or by the whole of it.
func foundBy(roots, names, patterns, shapes []string) []string {
	var out []string
	for _, shape := range shapes {
		for _, n := range names {
			if nameMatches(n, path.Base(shape)) {
				out = append(out, shape)
			}
		}
		for _, p := range patterns {
			re := findPattern(p)
			if re == nil {
				continue
			}
			for _, root := range roots {
				// The path find comes to it by starts at the root: `./x` from `.`,
				// and `internal/x` from `internal` for the project's internal/x.
				full := strings.TrimSuffix(clean(root), "/") + "/" + shape
				if r := clean(root); r != "." && strings.HasPrefix(shape, r+"/") {
					full = shape
				}
				if re.MatchString(pathrules.Fold(full)) {
					out = append(out, shape)
				}
			}
		}
	}
	return out
}

// literals is the patterns with a character of their own in them. One that is
// all wildcards is every file where find looks, and names none in particular.
func literals(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if hasLiteral(p) {
			out = append(out, p)
		}
	}
	return out
}

// nameMatches is find's -name: a glob against one name, where, unlike in the
// shell, `*` matches a leading dot.
func nameMatches(pattern, name string) bool {
	ok, err := path.Match(strings.ReplaceAll(pathrules.Fold(pattern), "[!", "[^"), pathrules.Fold(name))
	return ok && err == nil
}

// findPattern is find's -path as a regular expression: a glob in which `*`
// matches a `/` as well.
func findPattern(pattern string) *regexp.Regexp {
	p := pathrules.Fold(pattern)
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		switch c := p[i]; {
		case c == '*':
			b.WriteString(".*")
		case c == '?':
			b.WriteString(".")
		case c == '[':
			end := strings.IndexByte(p[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := p[i+1 : i+1+end]
			if rest, ok := strings.CutPrefix(class, "!"); ok {
				class = "^" + rest
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		case c >= utf8.RuneSelf:
			// A byte of a longer character, copied whole so the character stays.
			b.WriteByte(c)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}

// printedTo are the files find writes what it finds to: `-fprint FILE`,
// `-fprint0 FILE`, `-fprintf FILE FORMAT` and `-fls FILE`.
func printedTo(args []string) []string {
	var files []string
	for i, a := range args {
		switch a {
		case "-fprint", "-fprint0", "-fprintf", "-fls":
			if i+1 < len(args) {
				files = append(files, args[i+1])
			}
		}
	}
	return files
}
