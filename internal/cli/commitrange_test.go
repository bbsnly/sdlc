package cli

import (
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// landedStory is the ordinary case: a story that has been through every gate
// before the commit, whose work is one commit, and that commit the only one
// since the base. The tree is clean, so `sdlc gate commit pass` is the next
// thing that happens.
func landedStory(t *testing.T) (root, base, commit string) {
	t.Helper()
	root = startedAt(t, model.GateCommit)
	base = headOf(t, root)
	commitEverything(t, root)
	return root, base, headOf(t, root)
}

// revList is what a range holds, newest first. It is the meaning of the pair
// rather than the pair itself: git leaves the base out and takes the end in.
func revList(t *testing.T, root, base, end string) []string {
	t.Helper()
	if base == "" || end == "" {
		return nil
	}
	return strings.Fields(gitOut(t, root, "rev-list", base+".."+end))
}

// AC-5. Everything else in this story narrows the range; this is the case it
// must not narrow. One commit on a trunk nobody else touched is what almost
// every story is, and what the gate writes down for it -- the commit code
// review passed on, up to the commit that carries the work -- does not change.
func TestAC5TheOrdinaryOneCommitStoryRecordsTheRangeItAlwaysDid(t *testing.T) {
	t.Parallel()
	root, base, commit := landedStory(t)

	passed := decode[gatePayload](t, mustRun(t, "gate", "commit", "pass", "--note", "on trunk", "--json"))
	if passed.CommitBase != base || passed.Commit != commit {
		t.Errorf("the pass reported %q..%q, want %s..%s", passed.CommitBase, passed.Commit, base, commit)
	}
	if r := readRecord(t, root, "US-001"); r.CommitBase != base || r.Commit != commit {
		t.Errorf("the record names %q..%q, want %s..%s", r.CommitBase, r.Commit, base, commit)
	}
	if held := revList(t, root, base, commit); len(held) != 1 || held[0] != commit {
		t.Errorf("the range holds %v, want the story's one commit %s", held, commit)
	}

	// AC-2 for the same story: one commit's work reads back as one commit.
	e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json")))
	if e.RangeSource != rangeFromRecord || e.CommitBase != base || e.Commit != commit {
		t.Errorf("sdlc log places the story at %q..%q from %q, want %s..%s from the record",
			e.CommitBase, e.Commit, e.RangeSource, base, commit)
	}
	if out := mustRun(t, "log").stdout; !strings.Contains(out, base[:12]+".."+commit[:12]) {
		t.Errorf("sdlc log does not print the story's range:\n%s", out)
	}
}

// AC-4. The note is prose a session writes; the range is what the loop watched
// happen. US-004's record holds both and they disagree -- its note names
// d841f4f, the commit that carried the work, and its field names fad697d, a
// chore made afterwards. One record cannot say two things.
//
// What counts as naming a commit is drawn narrowly on purpose, because this
// refusal falls at the last gate, on work already committed and, in a paused
// tier, already approved: seven hexadecimal characters or more that git
// resolves to a commit in this repository, and nothing else. Anything shorter,
// and anything git has never heard of, is prose. Several of them are compared
// as a set, and any of them may name any commit inside the range, not only its
// end.
func TestAC4TheGateRefusesANoteNamingACommitItsRangeDoesNotHold(t *testing.T) {
	t.Parallel()
	root, base, commit := landedStory(t)

	for _, tc := range []struct {
		name, note string
		refused    bool
	}{
		{"AC-4 a note with no commit in it, which is what the runbook asks for", "on trunk", false},
		{"AC-4 the range's own end, in full", "landed in " + commit, false},
		{"AC-4 the range's own end, in seven characters", "landed in " + commit[:7], false},
		{"AC-4 forty hexadecimal characters git has never heard of", "reverts " + strings.Repeat("a", 40), false},
		{"AC-4 six characters of a commit outside the range are prose, not a sha", "see " + base[:6], false},
		{"AC-4 a hexadecimal word git would read as an option", "see -" + strings.Repeat("a", 40), false},
		{"AC-4 a commit the range does not hold", "landed in " + base, true},
		{"AC-4 two commits, one of them outside the range", commit + " and " + base, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The gate's own pass writes the record, so the tree the next note
			// is tried against is put back the way the commit left it.
			defer gitOut(t, root, "checkout", "--", ".sdlc")

			r := run(t, "gate", "commit", "pass", "--note", tc.note)
			if !tc.refused {
				if r.code != 0 {
					t.Fatalf("a note naming no commit outside the range was refused (%d):\n%s", r.code, r.stderr)
				}
				return
			}
			if r.code == 0 {
				t.Fatalf("the gate recorded a note naming a commit its range does not hold:\n%s", r.stdout)
			}
			// A person reading this has work already committed and a gate that
			// will not record, so the refusal has to say which sha is which.
			for _, want := range []string{"SDLC-E0049", base[:12], commit[:12]} {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
				}
			}
			record := readRecord(t, root, "US-001")
			if record.Pass(model.GateCommit) {
				t.Error("the gate was recorded as passed and the disagreement only reported")
			}
		})
	}
}
