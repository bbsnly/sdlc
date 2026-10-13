package cli

// US-019, AC-4: docs/enforcement.md states the answer to "does the loop govern
// a path outside the repository?" once, and every refusal that turns on the
// answer says what to do instead. The page is held to the code the way
// docs_test.go holds it: by what the code does, not by a second copy of it.
// A sentence is not pinned; what the page has to say is, as sets of words that
// one paragraph must hold together, so that a rewording does not fail it and a
// page that says nothing does.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/policy"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// paragraphsOf is the blocks of text in a page, each with its whitespace
// collapsed, separated by blank lines.
func paragraphsOf(text string) []string {
	var out []string
	for block := range strings.SplitSeq(text, "\n\n") {
		if flat := strings.Join(strings.Fields(block), " "); flat != "" {
			out = append(out, flat)
		}
	}
	return out
}

// ruleSection is the body of a rule's section on the enforcement page, up to
// the next heading of its own level or above.
func ruleSection(t *testing.T, id string) string {
	t.Helper()
	_, rest, ok := strings.Cut(page(t, "enforcement.md"), "\n### `"+id+"`\n")
	if !ok {
		t.Fatalf("docs/enforcement.md has no section for %s", id)
	}
	body, _, _ := strings.Cut(rest, "\n### ")
	body, _, _ = strings.Cut(body, "\n## ")
	return body
}

// holdsAll reports whether one paragraph of text holds every pattern.
func holdsAll(text string, patterns ...string) (string, bool) {
	for _, p := range paragraphsOf(text) {
		all := true
		for _, pattern := range patterns {
			if !regexp.MustCompile("(?i)" + pattern).MatchString(p) {
				all = false
				break
			}
		}
		if all {
			return p, true
		}
	}
	return "", false
}

// AC-4: the page says what "outside" is measured against, and that the file
// tools refuse an outside path while the shell rules let a path the hook can
// place go. One paragraph says it, naming the rule that refuses and the shell.
func TestAC4TheEnforcementPageStatesWhatOutsideTheRepositoryMeans(t *testing.T) {
	t.Parallel()
	doc := page(t, "enforcement.md")
	if _, ok := holdsAll(doc,
		`outside the repository`,
		`write-outside-repository`,
		`shell`,
		`checkout`,
		`not (the project's|governed|refused|protected)|does not govern|let(s)? (it|them|a path) go|allowed`,
	); !ok {
		t.Error("docs/enforcement.md has no paragraph that says what outside the repository is measured against " +
			"(the checkout the session is held in) and that the file tools refuse an outside path under " +
			"write-outside-repository while the shell rules let a path the hook can place outside it go")
	}
}

// AC-4: the page says that a word the hook cannot place is judged by its name,
// and what to do about it: write the path out in full.
func TestAC4TheEnforcementPageSaysAWordTheHookCannotPlaceIsJudgedByName(t *testing.T) {
	t.Parallel()
	doc := page(t, "enforcement.md")
	if p, ok := holdsAll(doc,
		`(cannot|can't|could not|unable to|does not know) (place|tell where)`,
		`by (its |the )?name`,
		`(out|spell\w*|writ\w+) .{0,40}in full|in full`,
	); !ok {
		t.Error("docs/enforcement.md has no paragraph that says a word the hook cannot place, such as a variable, is " +
			"judged by its name and that writing the path out in full is the way on")
	} else if !strings.Contains(p, "$") {
		t.Errorf("the paragraph on a word the hook cannot place gives no example of one (a variable such as \"$S\"): %q", p)
	}
}

// AC-4: each rule whose answer turns on a path being outside the repository
// says so in its own section, and says what to do instead. The freeze's rule,
// the new-test rule and the implementer's rule are the three that read an
// outside path as a test today.
func TestAC4EveryShellRuleThatTurnsOnAPathBeingOutsideSaysSoAndSaysWhatToDoInstead(t *testing.T) {
	t.Parallel()
	for _, id := range []string{
		"frozen-test-through-the-tool",
		"no-new-test-after-the-freeze",
		"implementer-does-not-write-tests",
	} {
		body := ruleSection(t, id)
		if !regexp.MustCompile(`(?i)outside the (repository|project)`).MatchString(body) {
			t.Errorf("the %s section does not say what a path outside the repository does to it", id)
		}
	}
	for _, id := range []string{
		"write-outside-repository",
		"loop-state-through-the-tool",
		"protected-path-through-the-tool",
		"frozen-test-through-the-tool",
		"no-new-test-after-the-freeze",
		"implementer-does-not-write-tests",
	} {
		if body := ruleSection(t, id); !strings.Contains(body, "*Instead:*") {
			t.Errorf("the %s section does not say what to do instead", id)
		}
	}
}

// AC-4: the refusal that turns on it says what to do instead in the refusal
// itself, not only on the page: the file tools' for an outside path, and the
// freeze's for a word it cannot place.
func TestAC4EveryRefusalThatTurnsOnItSaysWhatToDoInstead(t *testing.T) {
	t.Parallel()
	for _, r := range policy.Rules {
		if r.ID == "write-outside-repository" && (r.Route == "" || !strings.Contains(r.Route, "inside the repository")) {
			t.Errorf("write-outside-repository's route does not say to work inside the repository: %q", r.Route)
		}
	}
	s := shellpolicy.State{Frozen: []string{"internal/x_test.go"}}
	for _, command := range []string{
		`find "$S" -name '*_test*' -delete`,
		`rm -f "$S/internal/x_test.go"`,
		`rm -f "$S"/internal/*_test.go`,
		`find . -name '*_test*' -delete`,
	} {
		f, ok := shellpolicy.Inspect(command, s)
		if !ok || f.Rule != "frozen-test-through-the-tool" {
			t.Errorf("%s was not refused under the freeze: %+v", command, f)
			continue
		}
		if !strings.Contains(f.Route, "sdlc unfreeze") {
			t.Errorf("%s: the refusal does not say what to do instead: %q", command, f.Route)
		}
	}
}
