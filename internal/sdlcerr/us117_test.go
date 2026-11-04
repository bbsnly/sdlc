package sdlcerr

import (
	"strings"
	"testing"
)

// US-117 AC-2 to AC-5, and a ledger an older sdlc dropped (its design review): a
// refusal of its own for each, found by number so that
// this package keeps compiling until each is registered. Every fix says what to
// do next in words a person can act on, and every code has a live section in
// docs/troubleshooting.md that says the same.
func TestAC2AC3AC4AC5TheFindingCodesAreDocumentedAndSayWhatToDo(t *testing.T) {
	t.Parallel()
	documented := documentedCodes(t)
	for _, c := range []struct {
		id, refuses string
		// fix is every word the fix has to hold, lower case.
		fix []string
		// section is every word the troubleshooting section has to hold.
		section []string
	}{
		{"SDLC-E0089", "AC-2 findings that do not agree with --important and --nits",
			[]string{"--finding", "--important", "--nits"},
			[]string{"--finding", "--important", "--nits"}},
		{"SDLC-E0090", "AC-3 --declined or --referred with no reason",
			[]string{"--reason", "--fixed"},
			[]string{"--declined", "--referred", "--reason"}},
		{"SDLC-E0091", "AC-4 --referred of a finding in a file the story changes",
			[]string{"fixed", "declined", "reason"},
			[]string{"--referred", "changes", "declined"}},
		{"SDLC-E0092", "AC-5 gate verification or gate commit pass with a finding open",
			[]string{"sdlc finding close"},
			[]string{"sdlc finding close", "verification", "commit", "open"}},
		// The design review's I-1 (security) and I-3 (red team): the reviewer it
		// names lists its findings again, and every sdlc that saves the record is
		// one that keeps them.
		{"SDLC-E0093", "AC-5 gate verification or gate commit pass on a record whose findings an older sdlc dropped",
			[]string{"sdlc review add", "--finding"},
			[]string{"sdlc review add", "--finding", "verification", "commit", "older"}},
	} {
		t.Run(c.refuses, func(t *testing.T) {
			t.Parallel()
			code := registered(c.id)
			if code.String() != c.id {
				t.Fatalf("the catalogue has no %s", c.id)
			}
			fix := strings.ToLower(fixFor(code))
			for _, word := range c.fix {
				if !strings.Contains(fix, word) {
					t.Errorf("the fix for %s does not say %q: %s", c.id, word, fix)
				}
			}
			body, ok := documented[c.id]
			if !ok || isRetired(body) {
				t.Fatalf("%s has no live `### %s` section in %s", c.id, c.id, troubleshootingPath)
			}
			flat := strings.ToLower(strings.Join(strings.Fields(body), " "))
			for _, word := range c.section {
				if !strings.Contains(flat, word) {
					t.Errorf("the section for %s does not say %q:\n%s", c.id, word, body)
				}
			}
		})
	}
}
