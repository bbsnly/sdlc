package config

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode"
)

// MatchGlob is path.Match with "**" added: a segment of its own that stands for
// any number of directories. path.Match reads "**" as "*", which stops at a
// slash, so "src/**/*.spec.ts" found a spec one directory down and not two, and
// the deeper one was not a test as far as the freeze knew.
//
// A trailing "**" needs something beneath it: "e2e/**" is what is inside e2e/,
// not a file called e2e. A malformed pattern matches nothing, as it did. The
// match is exact: nothing here folds case, which is the callers' to decide.
func MatchGlob(pattern, rel string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			return matchAnyDepth(pattern[1:], name)
		}
		if len(name) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], name[0]); err != nil || !ok {
			return false
		}
		pattern, name = pattern[1:], name[1:]
	}
	return len(name) == 0
}

// matchAnyDepth is a "**" segment followed by rest: rest has to match what is
// left of name after however many directories the "**" took.
func matchAnyDepth(rest, name []string) bool {
	if len(rest) == 0 {
		return len(name) > 0
	}
	for i := 0; i <= len(name); i++ {
		if matchSegments(rest, name[i:]) {
			return true
		}
	}
	return false
}

// IntendsConflictMarkers reports whether the project says that the lines of an
// unresolved conflict are meant to be in the file rel, a slash path from the
// repository root as git names it. A pattern is matched against the whole path
// and exactly, with no folding of case and no reading of a bare name as a file
// anywhere: an exemption that matched more than it names would let a story add
// a conflict where nobody listed one. A path with an empty, "." or ".." segment
// is none git has in a commit that anyone checks out, and is never exempt.
func (p Paths) IntendsConflictMarkers(rel string) bool {
	if rel == "" {
		return false
	}
	for segment := range strings.SplitSeq(rel, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	for _, pattern := range p.ConflictMarkersIntended {
		if MatchGlob(pattern, rel) {
			return true
		}
	}
	return false
}

// conflictMarkerProblems is what is wrong with the paths where conflict markers
// are meant, one problem for each entry that cannot mean what it says. Pure
// slash logic, so that a glob loads the same on every platform. An entry that
// matched nothing would be a protection a person believed in that was not
// there, and doctor would list it as if it were.
func (p Paths) conflictMarkerProblems() []string {
	var problems []string
	for i, pattern := range p.ConflictMarkersIntended {
		if why := badIntendedPath(pattern); why != "" {
			problems = append(problems, fmt.Sprintf("paths.conflict_markers_intended[%d] is %q, which %s: "+
				"give a path or a glob from the repository root, slash-separated, as in \"docs/**\"", i, pattern, why))
		}
	}
	return problems
}

// badIntendedPath says why pattern is not a path or a glob from the root, or
// returns "" where it is one.
func badIntendedPath(pattern string) string {
	switch {
	case pattern == "":
		return "is empty"
	case strings.HasPrefix(pattern, "/"):
		return "starts with a slash"
	case strings.Contains(pattern, `\`):
		return "has a backslash in it"
	case strings.IndexFunc(pattern, unicode.IsControl) >= 0:
		return "has a control character in it"
	case slices.Contains(strings.Split(pattern, "/"), ".."):
		return "leaves the repository"
	case path.Clean(pattern) != pattern || pattern == ".":
		return "is not written in its plainest form (no \".\" or empty segment, no slash at the end)"
	}
	for segment := range strings.SplitSeq(pattern, "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, ""); err != nil {
			return "is not a pattern path.Match reads (" + err.Error() + ")"
		}
	}
	return ""
}
