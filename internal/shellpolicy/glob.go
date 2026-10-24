package shellpolicy

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// The shell expands a glob and a brace before the command sees them, so
// `rm .sdl?/state/active` and `echo x > CLAUDE.{md,}` reached the files the
// rules protect under words no rule matched. Nothing here lists a directory:
// each glob is matched against the shapes of the protected paths instead, and
// every path it can name is checked as though it had been written out.

// maxBraces bounds brace expansion, which doubles with every group:
// `{a,b}{a,b}...` is not thousands of paths to check.
const maxBraces = 64

// protectedShapes are the paths loopState and protectedPath refuse, as shapes:
// a `*` segment is any one name, as a story's id is.
func protectedShapes(backlog string) []string {
	shapes := []string{
		".sdlc", loopStateDir, loopConfigFile, ".sdlc/claude-progress.json",
		".sdlc/stories", ".sdlc/stories/*/reviews", ".sdlc/stories/*/" + model.RecordFile,
	}
	for _, a := range model.Artifacts {
		shapes = append(shapes, ".sdlc/stories/*/"+a.File)
	}
	shapes = append(shapes, protectedShellPaths...)
	if backlog != "" {
		shapes = append(shapes, backlog)
	}
	return shapes
}

// frozenShapes are the frozen tests.
func frozenShapes(frozen []string) []string {
	shapes := make([]string, len(frozen))
	for i, f := range frozen {
		shapes[i] = filepath.ToSlash(f)
	}
	return shapes
}

// withGlobs is words and, for each with a glob or a brace in it, every path
// among the shapes it can name. The shapes are only built for a command that
// has one.
func withGlobs(words []string, shapes func() []string) []string {
	out := words
	var all []string
	for _, w := range words {
		if !strings.ContainsAny(w, "*?[{") {
			continue
		}
		if all == nil {
			all = shapes()
		}
		out = append(out, globbed(w, all)...)
	}
	return out
}

// globbed is what one word can name among the shapes, each written out.
func globbed(word string, shapes []string) []string {
	var out []string
	for _, w := range braces(strings.TrimPrefix(clean(word), "./")) {
		if !strings.ContainsAny(w, "*?[") {
			out = append(out, w)
			continue
		}
		pattern := strings.Split(w, "/")
		for _, s := range shapes {
			out = append(out, matchShape(pattern, s)...)
		}
	}
	return out
}

// matchShape is the paths a pattern names as the shape, wherever in the pattern
// the shape starts. What comes before it is the directory it is in, and what
// comes after is kept as written: whether that is still protected is for the
// rules to say, as it is for a path with no glob in it.
func matchShape(pattern []string, shape string) []string {
	want := strings.Split(shape, "/")
	var out []string
	for at := 0; at+len(want) <= len(pattern); at++ {
		named := append([]string{}, pattern[:at]...)
		matched := true
		for i, name := range want {
			p := pattern[at+i]
			if name == "*" {
				named = append(named, p)
				continue
			}
			// A segment with nothing written in it names everything, and
			// CLAUDE.md is protected wherever it is: read as naming it, `rm -rf
			// build/*` was refused. The shape has to be asked for by name,
			// which namedFirst decides for the first segment.
			if i == 0 && !namedFirst(p, name) || i > 0 && !segmentMatches(p, name) {
				matched = false
				break
			}
			named = append(named, name)
		}
		if matched {
			out = append(out, strings.Join(append(named, pattern[at+len(want):]...), "/"))
		}
	}
	return out
}

// climbsPastGlobstar reports whether a word, as clean leaves it (which keeps
// `..`), has a segment that holds `**` and a later segment that is `..`; it
// is asked of the word with its backslashes taken out as well, since clean
// reads `.\.` as two names. zsh, and bash with globstar, let `**` stand for
// no directories, so `/tmp/**/..` can be `/` and what follows it any path;
// filepath.Clean (inside pathrules.Rel), path.Join and path.Dir each read
// `**/..` as one name and clean it away, and `/tmp/**/../work/project` was
// read as /tmp/work/project, outside.
func climbsPastGlobstar(word string) bool {
	for _, w := range []string{word, strings.ReplaceAll(word, `\`, "")} {
		star := false
		for seg := range strings.SplitSeq(clean(w), "/") {
			if star && seg == ".." {
				return true
			}
			star = star || strings.Contains(seg, "**")
		}
	}
	return false
}

// segmentMatches reports whether one segment of a glob matches a name, folded
// as the filesystem folds it. The shell reads a class as find does, so it is
// the same reader as find's -name. The shell's `*` and `?` do not match a
// leading dot, so `rm -rf ?git` leaves .git where it is.
func segmentMatches(pattern, name string) bool {
	g := findPattern(pattern)
	if g == nil {
		// A segment the reader cannot read is judged as before the reader.
		// Counted as matching everything, such a segment would name
		// CLAUDE.md, and a find from a directory holding only a frozen test
		// would be refused for a file it did not name.
		return pathMatches(pattern, name)
	}
	if strings.HasPrefix(pathrules.Fold(name), ".") && !strings.HasPrefix(pathrules.Fold(pattern), ".") {
		return false
	}
	return g.MatchString(name)
}

// pathMatches is segmentMatches as it was before the reader, by path.Match.
func pathMatches(pattern, name string) bool {
	pattern, name = pathrules.Fold(pattern), pathrules.Fold(name)
	if strings.HasPrefix(name, ".") && !strings.HasPrefix(pattern, ".") {
		return false
	}
	// The shell negates a class with `[!`, and path.Match with `[^`.
	ok, err := path.Match(strings.ReplaceAll(pattern, "[!", "[^"), name)
	return ok && err == nil
}

// ownRune reports whether the reader finds a character of the glob's own in
// it, outside `*`, `?` and every class.
func ownRune(pattern string) bool {
	g := findPattern(pattern)
	return g != nil && g.literal
}

// namedFirst reports whether the first segment of a glob names name. One in
// which the reader finds a character of its own is read as find reads it, so
// `[]C]LAUDE.md` names CLAUDE.md; any other is judged by hasLiteral and
// path.Match, as before the reader.
func namedFirst(pattern, name string) bool {
	if ownRune(pattern) {
		return segmentMatches(pattern, name)
	}
	return hasLiteral(pattern) && pathMatches(pattern, name)
}

// hasLiteral reports whether a glob segment has a character of its own, outside
// `*`, `?` and a `[...]` class.
//
// It ends a class at its first `]`, as it did before find's reader, which is
// not how find reads one. It is kept as it was because `literals` and
// `namedFirst` ask it beside the reader rather than in its place, so that a
// glob it counted as having a character of its own still counts as one.
func hasLiteral(pattern string) bool {
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*', '?':
		case '[':
			end := strings.IndexByte(pattern[i+1:], ']')
			if end < 0 {
				return true
			}
			i += end + 1
		default:
			return true
		}
	}
	return false
}

// braces is a word as the shell expands its braces: `CLAUDE.{md,txt}` is two
// words. A group without a comma is left as it is, and so is `${name,,}`,
// which is a variable.
func braces(word string) []string {
	out := []string{word}
	for i := 0; i < len(out); {
		open, end, parts := braceGroup(out[i])
		if open < 0 || len(out)+len(parts)-1 > maxBraces {
			i++
			continue
		}
		w := out[i]
		expanded := make([]string, 0, len(parts))
		for _, p := range parts {
			expanded = append(expanded, w[:open]+p+w[end+1:])
		}
		out = append(out[:i], append(expanded, out[i+1:]...)...)
	}
	return out
}

// braceGroup finds the first group in w with a comma at its own depth, and
// returns where it opens and closes and what is between the commas.
//
// It is one pass, with the groups still open on a stack. Scanning on from each
// `{` in turn was quadratic, and a word of a hundred thousand braces that
// never closed took seconds, past the time Claude Code gives the hook.
func braceGroup(w string) (int, int, []string) {
	var stack []braceOpen
	best, bestEnd := -1, -1
	var bestCommas []int
	for i := range len(w) {
		switch w[i] {
		case '{':
			stack = append(stack, braceOpen{open: i, name: i > 0 && w[i-1] == '$'})
		case ',':
			noteComma(stack, i)
		case '}':
			if len(stack) == 0 {
				continue
			}
			g := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if g.list() && (best < 0 || g.open < best) {
				best, bestEnd, bestCommas = g.open, i, g.commas
			}
		}
	}
	if best < 0 {
		return -1, -1, nil
	}
	return best, bestEnd, betweenCommas(w, best, bestEnd, bestCommas)
}

// braceOpen is a `{` braceGroup has read and not yet seen closed.
type braceOpen struct {
	open   int
	commas []int
	name   bool // `${`, a variable and never a list
}

// noteComma records a comma at i in the innermost group open, where one is.
func noteComma(stack []braceOpen, i int) {
	if len(stack) > 0 {
		stack[len(stack)-1].commas = append(stack[len(stack)-1].commas, i)
	}
}

// list is whether the group, now closed, is a list the shell expands: not a
// variable, and with a comma at its own depth.
func (g braceOpen) list() bool {
	return !g.name && len(g.commas) > 0
}

// betweenCommas is what a group of w holds between its braces, cut at the
// commas at its own depth.
func betweenCommas(w string, open, end int, commas []int) []string {
	parts := make([]string, 0, len(commas)+1)
	from := open + 1
	for _, c := range commas {
		parts = append(parts, w[from:c])
		from = c + 1
	}
	return append(parts, w[from:end])
}
