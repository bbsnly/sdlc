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
		".sdlc", ".sdlc/state", ".sdlc/config.json", ".sdlc/claude-progress.json",
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
			// build/*` was refused. The shape has to be asked for by name.
			if i == 0 && !hasLiteral(p) || !segmentMatches(p, name) {
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

// segmentMatches reports whether one segment of a glob matches a name, folded
// as the filesystem folds it. The shell's `*` and `?` do not match a leading
// dot, so `rm -rf ?git` leaves .git where it is.
func segmentMatches(pattern, name string) bool {
	pattern, name = pathrules.Fold(pattern), pathrules.Fold(name)
	if strings.HasPrefix(name, ".") && !strings.HasPrefix(pattern, ".") {
		return false
	}
	// The shell negates a class with `[!`, and path.Match with `[^`.
	ok, err := path.Match(strings.ReplaceAll(pattern, "[!", "[^"), name)
	return ok && err == nil
}

// hasLiteral reports whether a glob segment has a character of its own, outside
// `*`, `?` and a `[...]` class.
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
		expanded := make([]string, len(parts))
		for j, p := range parts {
			expanded[j] = w[:open] + p + w[end+1:]
		}
		out = append(out[:i], append(expanded, out[i+1:]...)...)
	}
	return out
}

// braceGroup finds the first group in w with a comma at its own depth, and
// returns where it opens and closes and what is between the commas.
func braceGroup(w string) (int, int, []string) {
	for open := 0; open < len(w); open++ {
		if w[open] != '{' || open > 0 && w[open-1] == '$' {
			continue
		}
		depth, start := 0, open+1
		var parts []string
	group:
		for i := open; i < len(w); i++ {
			switch w[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth > 0 {
					continue
				}
				if parts != nil {
					return open, i, append(parts, w[start:i])
				}
				break group
			case ',':
				if depth == 1 {
					parts = append(parts, w[start:i])
					start = i + 1
				}
			}
		}
	}
	return -1, -1, nil
}
