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
//
// The segments are walked in place, a Cut at a time, rather than split into
// two slices: the freeze asks it twice per glob for every path a shell
// command names, and the two splits were a quarter of what the classifier
// allocated for a command naming many files (US-129, AC-7).
func MatchGlob(pattern, rel string) bool {
	return matchSegments(pattern, true, rel, true)
}

// matchSegments matches what is left of the pattern against what is left of
// the name, each a string of slash-separated segments; more says whether any
// segment is left at all, since the empty string is one empty segment where
// it follows a slash and none otherwise.
func matchSegments(pattern string, patternMore bool, name string, nameMore bool) bool {
	for patternMore {
		var segment string
		segment, pattern, patternMore = strings.Cut(pattern, "/")
		if segment == "**" {
			return matchAnyDepth(pattern, patternMore, name, nameMore)
		}
		if !nameMore {
			return false
		}
		var first string
		first, name, nameMore = strings.Cut(name, "/")
		if ok, err := path.Match(segment, first); err != nil || !ok {
			return false
		}
	}
	return !nameMore
}

// matchAnyDepth is a "**" segment followed by rest: rest has to match what is
// left of name after however many directories the "**" took.
func matchAnyDepth(rest string, restMore bool, name string, nameMore bool) bool {
	if !restMore {
		return nameMore
	}
	for {
		if matchSegments(rest, true, name, nameMore) {
			return true
		}
		if !nameMore {
			return false
		}
		_, name, nameMore = strings.Cut(name, "/")
	}
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

// checkConflictMarkers adds what is wrong with the paths where conflict markers
// are meant, one problem for each entry that cannot mean what it says. Pure
// slash logic, so that a glob loads the same on every platform. An entry that
// matched nothing would be a protection a person believed in that was not
// there, and doctor would list it as if it were. Past the size a refusal keeps
// an entry is only counted: kept in a slice, every problem took the load of
// 100,000 bad entries from 13 MB to 45 MB, and the hook loads the file on every
// tool call.
func (p Paths) checkConflictMarkers(found *problems) {
	for i, pattern := range p.ConflictMarkersIntended {
		why := badIntendedPath(pattern)
		switch {
		case why == "":
		case found.full():
			found.count(&found.otherFound, 1)
		default:
			found.addOther(fmt.Sprintf("paths.conflict_markers_intended[%d] is %s, which %s: "+
				"give a path or a glob from the repository root, slash-separated, as in \"docs/**\"", i, quoted(pattern), why))
		}
	}
}

// checkPausePaths adds what is wrong with the paths whose change pauses the
// commit gate, as checkConflictMarkers does for its list: a glob that cannot
// mean what it says matches nothing, and the pause a person listed would be
// no pause at all (US-133, THREATS T5).
func (h HumanGates) checkPausePaths(found *problems) {
	for i, pattern := range h.PreCommitPausePaths {
		why := badIntendedPath(pattern)
		switch {
		case why == "":
		case found.full():
			found.count(&found.otherFound, 1)
		default:
			found.addOther(fmt.Sprintf("human_gates.pre_commit_pause_paths[%d] is %s, which %s: "+
				"give a path or a glob from the repository root, slash-separated, as in \"internal/hook/**\"", i, quoted(pattern), why))
		}
	}
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
