package parallel

// US-026: an unattended session works a story with the staffing on only if it
// may run what the project manager runs, and reads the staffing with. The
// three are named one by one: `Bash(sdlc staffing:*)` would pre-approve
// `sdlc staffing assign`, which is a person's (decision 8), and leave only the
// hook and the command's own check between a session and it (T4).

import (
	"slices"
	"strings"
	"testing"
)

func TestAC1AnUnattendedSessionMayRecordAmendAndReadTheStaffing(t *testing.T) {
	list := Allowlist()
	for _, want := range []string{
		"Bash(sdlc staffing set:*)",
		"Bash(sdlc staffing amend:*)",
		"Bash(sdlc staffing show:*)",
	} {
		if !slices.Contains(list, want) {
			t.Errorf("the built-in list has no %s: a story with the staffing on stops at the analysis gate", want)
		}
	}
}

func TestAC8TheUnattendedListNeverPreApprovesAPersonsAssignment(t *testing.T) {
	if len(Allowlist()) == 0 {
		t.Fatal("the unattended flag grants nothing")
	}
	// The command is the string and the entry's prefix is what it is tested
	// for, in that order: the question is whether the command starts with it.
	assign := "sdlc staffing assign security --model opus --reason x"
	if !strings.HasPrefix(assign, "sdlc staffing") {
		t.Fatal("the check below cannot see an entry that pre-approves every staffing command")
	}
	for _, e := range Allowlist() {
		name, scope, _ := strings.Cut(e, "(")
		if name != "Bash" {
			continue
		}
		// What the entry lets through is every command that starts with its
		// prefix: one that `sdlc staffing assign` starts with pre-approves it.
		prefix := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(scope, ")"), "*"), ":")
		if strings.HasPrefix(assign, prefix) {
			t.Errorf("%q pre-approves `sdlc staffing assign`, which only a person runs", e)
		}
	}
}
