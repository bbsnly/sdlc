package shellpolicy

import (
	"path"
	"slices"
	"strings"
	"testing"
)

// A glob or a brace is expanded by the shell, and the file it names is the file
// the command changes: each of these reached a protected path under a word no
// rule matched.
func TestAGlobNamesTheFilesItMatches(t *testing.T) {
	s := ready
	s.Backlog = "docs/user_stories.json"
	for _, command := range []string{
		"rm -rf .sdlc/*",
		"rm .sdlc/*/active",
		"rm .sdl?/state/active",
		"rm .sdlc/stat?/active",
		"rm -rf .s*",
		"rm -rf .sdlc/stor*",
		"rm .sdlc/config.js??",
		"rm .sdlc/claude-progress.js?n",
		"rm .sdlc/{state,x}/active",
		"rm -rf .{sdlc,nothing}",
		"rm .sdlc/stories/*/*",
		"rm .sdlc/stories/A-1/TEST-PLAN.m?",
		"rm .sdlc/stories/A-?/PLAN.m?",
		"rm .sdlc/stories/A-1/gate-record.js?n",
		"rm -rf .sdlc/stories/A-1/revie?s",
		"rm /home/dev/repo/.sdl[c]/config.json",
		"cd .sdlc && rm stat?/active",
	} {
		refused(t, command, s, "loop-state-through-the-tool")
	}
	for _, command := range []string{
		"rm -rf .git*",
		"rm CLAUDE.m?",
		"rm [C]LAUDE.md",
		"rm [!x]LAUDE.md",
		"echo x > CLAUDE.{md,}",
		"rm .claude/settings.*",
		"rm docs/user_stor?es.json",
	} {
		refused(t, command, s, "protected-path-through-the-tool")
	}
	for _, command := range []string{
		"rm -rf *",
		"rm -rf ./*",
		"rm -rf [!.]*",
		"rm -rf ?git",
		"rm *sdlc/config.json",
		"rm -rf build/*",
		"cd build && rm -rf *",
		"rm -rf dist/*/*",
		"rm build/*.o",
		"rm .sdlc/stories/*/notes.md",
		"rm -rf node_modules/{a,b}",
		"find . -name '*.pyc' -exec rm {} +",
		"rm -rf ${TMPDIR}/build",
	} {
		allowed(t, command, s)
	}

	frozen := ready
	frozen.Frozen = []string{"internal/calc/add_test.go"}
	frozen.IsTest = func(p string) bool {
		ok, _ := path.Match("*_test.go", path.Base(p))
		return ok
	}
	for _, command := range []string{
		"rm internal/calc/add_tes?.go",
		"echo x > internal/calc/add_test.{go,}",
		"cd internal && rm calc/add_te[s]t.go",
		"rm -f internal/calc/*",
	} {
		refused(t, command, frozen, "frozen-test-through-the-tool")
	}
	allowed(t, "rm internal/calc/*.txt", frozen)
}

func TestBracesExpandAsTheShellExpandsThem(t *testing.T) {
	for word, want := range map[string][]string{
		"a{b,c}d{e,f}": {"abde", "abdf", "acde", "acdf"},
		"{a,{b,c}}":    {"a", "b", "c"},
		"x{,.md}":      {"x", "x.md"},
		"${name,,}/a":  {"${name,,}/a"},
		"{}":           {"{}"},
		"{a}{b,c":      {"{a}{b,c"},
	} {
		if got := braces(word); !slices.Equal(got, want) {
			t.Errorf("braces(%q) = %q, want %q", word, got, want)
		}
	}
	if got := braces(strings.Repeat("{a,b}", 20)); len(got) > maxBraces {
		t.Errorf("twenty groups were %d words", len(got))
	}
}

func TestOnlyALiteralNamesAProtectedPath(t *testing.T) {
	for pattern, want := range map[string]bool{
		"*": false, "?*": false, "[!.]*": false, "[a-z]": false,
		".s*": true, "[C]LAUDE.md": true, "*.md": true, "[abc": true,
	} {
		if got := hasLiteral(pattern); got != want {
			t.Errorf("hasLiteral(%q) = %v, want %v", pattern, got, want)
		}
	}
}
