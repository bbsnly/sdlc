package shellpolicy

import (
	"strings"
	"testing"
)

// find picks what it deletes by a name, and each of these deleted the loop's
// own files, or a frozen test, by a name no path rule read.
func TestFindPicksItsFilesByName(t *testing.T) {
	s := ready
	s.Backlog = "docs/user_stories.json"
	for _, command := range []string{
		"find . -name active -delete",
		"find . -iname ACTIVE -delete",
		"find . -name '[!x]ctive' -delete",
		"find . -name tests.lock -exec rm {} +",
		"find -L . -name stop-blocks.json -delete",
		"find . -path '*state/active' -delete",
		"find . -path './.sdlc/sta[t]e' -exec rm -rf {} +",
		"find -name active -delete",
		"find .. -name active -delete",
		"cd build && find .. -name active -delete",
		`find "$ROOT" -name active -delete`,
		"find . -name 'activ?' -delete",
		"find . -type d -name reviews -exec rm -rf {} +",
		"find . -name gate-record.json -delete",
		"find . -name PLAN.md -delete",
		"find . -name config.json -delete",
		"find /work/project -name config.json -delete",
		// Everything there is the loop's files as well.
		"find . -name '*' -delete",
		"find . -path '*' -delete",
	} {
		refused(t, command, s, "loop-state-through-the-tool")
	}
	for _, command := range []string{
		"find . -name user_stories.json -delete",
		"find . -name '.gi?' -exec rm -rf {} +",
	} {
		refused(t, command, s, "protected-path-through-the-tool")
	}
	for _, command := range []string{
		"find build -name config.json -delete",
		"find -L build -name config.json -delete",
		"cd build && find . -name config.json -delete",
		"find . -name '*.pyc' -delete",
		"find . -name active -print",
		"find . -name '*' -print",
		"find internal -path '*state/active' -delete",
	} {
		allowed(t, command, s)
	}

	// A directory outside the project can be one above it.
	placed := s
	placed.Resolve = func(word string) string {
		if word == "/work/project" {
			return "."
		}
		if rel, ok := strings.CutPrefix(word, "/work/project/"); ok {
			return rel
		}
		if strings.HasPrefix(word, "/") {
			return ""
		}
		return word
	}
	refused(t, "find /work -name active -delete", placed, "loop-state-through-the-tool")
	refused(t, "find /work/project -name active -delete", placed, "loop-state-through-the-tool")
	refused(t, "cd build && find /work -name active -delete", placed, "loop-state-through-the-tool")
	allowed(t, "find /work/project/internal -name active -delete", placed)

	frozen := ready
	frozen.Frozen = []string{"internal/load/testdata/config.json"}
	for _, command := range []string{
		"find internal -name 'confi?.json' -delete",
		"find internal -path '*testdata/config.json' -delete",
		"find internal -path 'internal/load/*/config.json' -delete",
		// The path find comes to it by starts at internal, not below it.
		"find internal -path 'internal/load*config.json' -delete",
	} {
		refused(t, command, frozen, "frozen-test-through-the-tool")
	}
	allowed(t, "find build -name '*.txt' -delete", frozen)
	allowed(t, "find build -name '*' -delete", frozen)
}

func TestFindPatternsMatchAsFindMatchesThem(t *testing.T) {
	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		{"*state/active", "./.sdlc/state/active", true},
		{"./.sdlc/*", "./.sdlc/stories/a-1/plan.md", true},
		{"./.sdlc/[!s]tate", "./.sdlc/state", false},
		{"./.sdlc/[!x]tate", "./.sdlc/state", true},
		{"a.b", "axb", false},
		{"[abc", "[abc", true},
		{"ü*", "über", true},
	} {
		re := findPattern(c.pattern)
		if got := re != nil && re.MatchString(c.path); got != c.want {
			t.Errorf("-path %q against %q = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
	if !nameMatches("*sdlc", ".sdlc") || nameMatches("x*", ".sdlc") {
		t.Error("find's -name matches a leading dot with *")
	}
}
