package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// US-131 AC-3, AC-4 and AC-9, past the main rows in us131_test.go: what
// `sdlc decide` says with --json and refuses where its files cannot be read,
// how it keeps a decision an older sdlc saved, and how `sdlc status` counts a
// committed story's decisions against a person's ack.

// dcCommittedStory is a project whose US-001 holds the first decision and has
// passed every gate, with the iteration stopped and nothing acknowledged.
func dcCommittedStory(t *testing.T) string {
	t.Helper()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	mustRun(t, dcFirst()...)
	reach(t, root, "")
	mustRun(t, "stop")
	return root
}

// dcEditRecord changes US-001's record on disk, as an older sdlc or a hand edit
// would leave it.
func dcEditRecord(t *testing.T, root string, edit func(map[string]any)) {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal([]byte(dcRecordText(t, root)), &record); err != nil {
		t.Fatal(err)
	}
	edit(record)
	out, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".sdlc/stories/US-001/"+model.RecordFile, string(out)+"\n")
}

// dcDecisionsMd is US-001's DECISIONS.md, and whether it is there.
func dcDecisionsMd(t *testing.T, root string) (string, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", model.DecisionsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw), true
}

// dcStatus is decisions_unread and what `sdlc status --json` said on stderr.
func dcStatus(t *testing.T) (int, string) {
	t.Helper()
	r := mustRun(t, "status", "--json")
	got := decode[struct {
		DecisionsUnread *int `json:"decisions_unread"`
	}](t, r)
	if got.DecisionsUnread == nil {
		t.Fatal("AC-9 sdlc status --json has no decisions_unread")
	}
	return *got.DecisionsUnread, r.stderr
}

func TestAC3DecideWithJSONSaysWhatItRecordedAndNumbersEachDecision(t *testing.T) {
	t.Parallel()
	gitProjectInitialised(t)
	mustRun(t, "start")
	for i, args := range [][]string{dcFirst(), dcSecond()} {
		got := decode[decidePayload](t, mustRun(t, append(args, "--json")...))
		want := decidePayload{OK: true, Story: "US-001", Gate: string(model.GateDoR), By: args[len(args)-1], Number: i + 1,
			Path: ".sdlc/stories/US-001/DECISIONS.md"}
		if got != want {
			t.Errorf("AC-3 decide --json for decision %d = %+v, want %+v", i+1, got, want)
		}
	}
}

// A decision is checked whole before the store is opened, so a well-formed one
// is what reaches a directory that is no project; it is refused there with a
// code, and nothing is written.
func TestAC4DecideOutsideAProjectIsRefusedAndWritesNothing(t *testing.T) {
	t.Parallel()
	dir := wtTempDir(t)
	r := wtRun(t, dir, "", "", dcFirst()...)
	if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E") {
		t.Errorf("AC-4 decide outside a project was not refused with a code:\n%s%s", r.stdout, r.stderr)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("AC-4 the directory holds %d entries after the refusal (%v), want none", len(entries), err)
	}
}

// DECISIONS.md is written first and the record second, each read before it is
// written: where either cannot be read the decision is refused, and neither
// file changes.
func TestAC3ADecisionIsRefusedAndRecordsNothingWhereItsFilesCannotBeRead(t *testing.T) {
	t.Parallel()
	t.Run("AC-3 DECISIONS.md is a directory", func(t *testing.T) {
		t.Parallel()
		root := gitProjectInitialised(t)
		mustRun(t, "start")
		if err := os.MkdirAll(filepath.Join(root, ".sdlc", "stories", "US-001", "DECISIONS.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		before := dcRecordText(t, root)
		r := run(t, dcFirst()...)
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0005") || !strings.Contains(r.stderr, "DECISIONS.md") {
			t.Errorf("AC-3 a decision over a DECISIONS.md that cannot be read was not refused with SDLC-E0005 naming it:\n%s", r.stderr)
		}
		if after := dcRecordText(t, root); after != before {
			t.Error("AC-3 the refused decision changed the record")
		}
	})
	t.Run("AC-3 the record does not parse", func(t *testing.T) {
		t.Parallel()
		root := gitProjectInitialised(t)
		mustRun(t, "start")
		writeFile(t, root, ".sdlc/stories/US-001/"+model.RecordFile, "{ not json\n")
		r := run(t, dcFirst()...)
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0005") {
			t.Errorf("AC-3 a decision on a record that does not parse was not refused with SDLC-E0005:\n%s", r.stderr)
		}
		if _, ok := dcDecisionsMd(t, root); ok {
			t.Error("AC-3 the refused decision wrote DECISIONS.md")
		}
		if got := dcRecordText(t, root); got != "{ not json\n" {
			t.Errorf("AC-3 the refused decision rewrote the record: %q", got)
		}
	})
}

// An sdlc older than the decision event keeps a decision's type and message and
// drops its choice. DECISIONS.md is written again from the record with each
// decision, so such a one is kept there, by what its message says.
func TestAC3ADecisionAnOlderSdlcSavedWithoutItsChoiceStaysInDecisionsMd(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	const old = "later researcher: an old question"
	dcEditRecord(t, root, func(record map[string]any) {
		events, _ := record["events"].([]any)
		record["events"] = append(events, map[string]any{"at": "2026-11-20T09:00:00Z", "type": model.EventDecision, "message": old})
	})
	got := decode[decidePayload](t, mustRun(t, append(dcFirst(), "--json")...))
	if got.Number != 2 {
		t.Errorf("AC-3 the decision after an older one is D-%d, want D-2", got.Number)
	}
	body, ok := dcDecisionsMd(t, root)
	if !ok {
		t.Fatal("AC-3 decide wrote no DECISIONS.md")
	}
	for _, want := range []string{
		"## D-1 at unknown gate, 2026-11-20T09:00:00Z\n", "- Recorded as: " + old + "\n",
		"## D-2 at dor, by researcher, ", "- Question: " + dcQ1 + "\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("AC-3 DECISIONS.md does not hold %q:\n%s", want, body)
		}
	}
}

// AC-9 and security F-7 of the design review: a committed story's decisions
// count until a person's ack covers its commit, and a decision on a story whose
// commit an ack already covers is refused, since nothing would count or list it.
func TestAC9ACommittedStorysDecisionsCountUntilAnAckCoversItsCommit(t *testing.T) {
	t.Parallel()
	root := dcCommittedStory(t)
	first := wtGit(t, root, "rev-list", "--max-parents=0", "HEAD")
	mustRun(t, "ack", "--through", first)
	if n, _ := dcStatus(t); n != 1 {
		t.Errorf("AC-9 decisions_unread is %d with an ack from before the story's commit, want 1", n)
	}
	// That ack does not cover the commit, so a late decision is taken.
	mustRun(t, dcSecond()...)
	if n, _ := dcStatus(t); n != 2 {
		t.Errorf("AC-9 decisions_unread is %d after a decision the ack does not cover, want 2", n)
	}

	mustRun(t, "ack", "--through", "HEAD")
	if n, _ := dcStatus(t); n != 0 {
		t.Errorf("AC-9 decisions_unread is %d once the ack covers the story's commit, want 0", n)
	}
	before := dcRecordText(t, root)
	file, _ := dcDecisionsMd(t, root)
	r := run(t, dcFirst()...)
	if r.code == 0 || !strings.Contains(r.stderr, dcCode) || !strings.Contains(r.stderr, "acknowledged") {
		t.Errorf("AC-3 a decision on a story whose commit is acknowledged was not refused with %s:\n%s", dcCode, r.stderr)
	}
	if after, _ := dcDecisionsMd(t, root); dcRecordText(t, root) != before || after != file {
		t.Error("AC-3 the refused decision changed the record or DECISIONS.md")
	}
}

// A record that names its commit other than in full is placed as `sdlc log`
// places it: read once an ack covers the commit git reads it as, and not read
// where git has no such commit, which is the safe direction.
func TestAC9ACommitTheRecordNamesShortIsPlacedAsTheLogPlacesIt(t *testing.T) {
	t.Parallel()
	root := dcCommittedStory(t)
	mustRun(t, "ack", "--through", "HEAD")
	full := readRecord(t, root, "US-001").Commit
	if len(full) < 40 {
		t.Fatalf("the committed record names %q, want its full commit", full)
	}
	dcEditRecord(t, root, func(record map[string]any) { record["commit"] = full[:12] })
	if n, _ := dcStatus(t); n != 0 {
		t.Errorf("AC-9 decisions_unread is %d for a short name of an acknowledged commit, want 0", n)
	}
	dcEditRecord(t, root, func(record map[string]any) { record["commit"] = "abc1234" })
	if n, _ := dcStatus(t); n != 1 {
		t.Errorf("AC-9 decisions_unread is %d for a commit git does not have, want 1", n)
	}
}

// An ack that cannot be read covers nothing, and says so: counting a story
// read that nobody read is the unsafe direction.
func TestAC9AnAckThatCannotBeReadCountsEveryCommittedStorysDecisions(t *testing.T) {
	t.Parallel()
	root := dcCommittedStory(t)
	mustRun(t, "ack", "--through", "HEAD")
	if n, _ := dcStatus(t); n != 0 {
		t.Fatalf("AC-9 decisions_unread is %d after an ack of HEAD, want 0", n)
	}
	ack := filepath.Join(root, filepath.FromSlash(store.AcknowledgedFile))

	missing := strings.Repeat("0", 39) + "1"
	writeFile(t, root, store.AcknowledgedFile, missing+"\n")
	if n, said := dcStatus(t); n != 1 || !strings.Contains(said, "counted as not acknowledged") || !strings.Contains(said, missing) {
		t.Errorf("AC-9 with an ack naming no commit git has, decisions_unread is %d and stderr is:\n%s", n, said)
	}

	if err := os.Remove(ack); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ack, 0o755); err != nil {
		t.Fatal(err)
	}
	if n, said := dcStatus(t); n != 1 || !strings.Contains(said, "counted as not acknowledged") {
		t.Errorf("AC-9 with an ack that cannot be read, decisions_unread is %d and stderr is:\n%s", n, said)
	}
}

// The status line names up to three stories under way, each with its
// DECISIONS.md, and counts the rest; a count short of a record that could not
// be read says "at least".
func TestAC9TheStatusLineNamesThreeStoriesUnderWayAndCountsTheRest(t *testing.T) {
	t.Parallel()
	if got := (unreadDecisions{}).line(); got != "" {
		t.Errorf("AC-9 with nothing unread the line is %q, want none", got)
	}
	u := unreadDecisions{
		total: 7, committed: 1,
		underWay: []storyDecisions{{"US-001", 2}, {"US-002", 1}, {"US-003", 2}, {"US-004", 1}},
		unknown:  []string{"US-009"},
	}
	want := "Decisions: at least 7 not yet acknowledged (1 in stories committed since your last ack, listed by sdlc log; " +
		"2 in US-001 (.sdlc/stories/US-001/DECISIONS.md), 1 in US-002 (.sdlc/stories/US-002/DECISIONS.md), " +
		"2 in US-003 (.sdlc/stories/US-003/DECISIONS.md) and 1 more, under way; " +
		"not counted: US-009, whose records could not be read)"
	if got := u.line(); got != want {
		t.Errorf("AC-9 the status line is\n%q\nwant\n%q", got, want)
	}
	two := unreadDecisions{total: 3, underWay: []storyDecisions{{"US-001", 2}, {"US-002", 1}}}
	if got := two.line(); !strings.Contains(got, "Decisions: 3 not yet acknowledged (2 in US-001 (.sdlc/stories/US-001/DECISIONS.md) and 1 in US-002") {
		t.Errorf("AC-9 the status line for two stories is %q", got)
	}
}
