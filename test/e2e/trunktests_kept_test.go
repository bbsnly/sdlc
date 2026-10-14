// US-094 end to end: the record keeps the trunk test files a gate accepted
// until a later gate judges them again, through the built binary and the
// shipped hook.
//
// A session finishes a story in its worktree; another story's test file lands
// on trunk; the story is rebased onto it and reworked from the
// implementation, every command of the session's asked of the hook first.
// The first gate of the rework to check the freeze says it took the file for
// trunk's, and from then on the record names the trunk commit and the file
// after every gate, with the one trunk_tests event (AC-1, AC-2). The commit
// gate recorded as failed takes the field with the rest it recorded (AC-3);
// passed again it names the file once more, and the stop lands the story with
// the field on the landed record.
//
// Helpers are landing_ontrunk_test.go's and landing_conditions_test.go's; the
// ones added here carry a kept prefix.

package e2e

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// keptTest is the test file another story lands on trunk.
const keptTest = "ledger_test.go"

// keptRecord is what the story's record says of the test files taken for
// trunk's: the field, and the message of every trunk_tests event.
type keptRecord struct {
	TrunkTests *struct {
		Commit string   `json:"commit"`
		Files  []string `json:"files"`
		Gone   []string `json:"gone"`
	} `json:"trunk_tests"`
	Replay *struct {
		To string `json:"to"`
	} `json:"replay"`
	Commit string `json:"commit"`
	Change string `json:"change"`
	Gates  map[string]struct {
		Status string `json:"status"`
	} `json:"gates"`
	Events []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"events"`
}

// keptHolds holds the record in the main checkout to naming keptTest against
// commit in its trunk_tests, or to having none where commit is empty, and to
// that many trunk_tests events.
func keptHolds(t *testing.T, main checkout, when, commit string, events int) keptRecord {
	t.Helper()
	var r keptRecord
	readJSON(t, main.record("US-001"), &r)
	switch {
	case commit == "" && r.TrunkTests != nil:
		t.Errorf("%s the record's trunk_tests is %+v, want none", when, *r.TrunkTests)
	case commit != "" && r.TrunkTests == nil:
		t.Errorf("%s the record has no trunk_tests, want %s taken for trunk's against %s", when, keptTest, commit)
	case commit != "" && (r.TrunkTests.Commit != commit || !slices.Equal(r.TrunkTests.Files, []string{keptTest}) || len(r.TrunkTests.Gone) != 0):
		t.Errorf("%s the record's trunk_tests is %+v, want only %s against %s", when, *r.TrunkTests, keptTest, commit)
	}
	var said []string
	for _, e := range r.Events {
		if e.Type == "trunk_tests" {
			said = append(said, e.Message)
		}
	}
	if len(said) != events {
		t.Errorf("%s the record has %d trunk_tests event(s), want %d:\n%s", when, len(said), events, strings.Join(said, "\n"))
	}
	return r
}

// keptGate is the session passing a gate in the worktree, asked of the hook
// first, which has to work; it returns what the gate printed.
func keptGate(s *ontoStory, gate string) string {
	s.t.Helper()
	args := []string{"gate", gate, "pass", "--note", "reworked"}
	s.do("", "Bash", running(s.at.in(s.tree, "sdlc "+strings.Join(args, " "))))
	stdout, stderr, code := s.here().outcome(s.t, "", args...)
	if code != 0 {
		s.t.Fatalf("sdlc gate %s pass in %s exited %d:\n%s%s", gate, s.tree, code, stdout, stderr)
	}
	return stdout + stderr
}

// AC-1, AC-2 and AC-3 through the binary and the hook.
func TestAC1AC2AndAC3AReworkOnAMovedTrunkKeepsWhatItsFirstGateAcceptedOnTheRecordWithOneEvent(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1})
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	s.worked()
	keptHolds(t, main, "with the story finished on the cut,", "", 0)

	writeFile(t, filepath.Join(main.dir, keptTest), "package main\n\n// another story's test\n")
	moved := commitPaths(t, main.dir, "another story's test", keptTest)
	git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--quiet", "main")

	s.run("", "gate", "implementation", "fail", "--note", "reworked")
	keptHolds(t, main, "after the implementation was recorded as failed,", "", 0)

	// The rework, gate by gate as the session works them.
	for gate := s.nextGate(); gate != ""; gate = s.nextGate() {
		switch model.Gate(gate) {
		case model.GateImplementation:
			s.writes("implementer", "FIXED.md", "what the rework changed\n")
		case model.GateVerification:
			s.stores("verifier", "verification", "# Verification\n\nEvery command green again.\n")
		case model.GateCommit:
			git(t, s.tree, "add", "-A")
			s.do("", "Bash", running(s.at.in(s.tree, "git commit -m rework")))
			git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "rework")
		case model.GateRetro:
			s.stores("bookkeeper", "retro", "# Retro\n\nReworked on a trunk that moved.\n")
		default:
			// Reviews only, below.
		}
		for _, r := range model.ReviewersFor(model.Gate(gate)) {
			if r.WhenDoRAdvocate {
				continue
			}
			s.do(r.Role, "Bash", running(s.at.in(s.tree, "sdlc review add "+gate+" "+r.Role+" approve")))
			s.here().runWith(t, "# "+r.Role+", again\n", "review", "add", gate, r.Role, "approve")
		}
		said := keptGate(s, gate)
		switch model.Gate(gate) {
		case model.GateImplementation, model.GateVerification, model.GateCommit:
			if !strings.Contains(said, "accepted as main's") || !strings.Contains(said, keptTest) {
				t.Errorf("the %s gate does not say it took %s for trunk's:\n%s", gate, keptTest, said)
			}
		default:
			if strings.Contains(said, "accepted as") {
				t.Errorf("the %s gate speaks of test files, and it does not check the freeze:\n%s", gate, said)
			}
		}
		keptHolds(t, main, "AC-1, AC-2: after the "+gate+" gate passed in the rework,", moved, 1)
	}

	// AC-3: the commit gate opened again takes the field with the rest.
	s.run("", "gate", "commit", "fail", "--note", "reworked")
	r := keptHolds(t, main, "AC-3: after the commit gate was recorded as failed,", "", 1)
	if r.Commit != "" || r.Change != "" || r.Replay != nil || r.Gates["commit"].Status == "pass" {
		t.Errorf("AC-3: the record still holds what the commit gate recorded: commit %q, change %q, replay %+v, gate %q",
			r.Commit, r.Change, r.Replay, r.Gates["commit"].Status)
	}

	// The same file after the field was emptied adds one event, and the
	// landed record still names it once the stop has lifted the freeze.
	if said := keptGate(s, "commit"); !strings.Contains(said, keptTest) {
		t.Errorf("the commit gate, passed again, does not name %s:\n%s", keptTest, said)
	}
	keptHolds(t, main, "after the commit gate passed again,", moved, 2)
	s.stores("bookkeeper", "retro", "# Retro\n\nReworked on a trunk that moved.\n")
	keptGate(s, "retro")
	head := headOf(t, s.tree)
	s.run("", "stop")
	if got := landTrunk(t, main); got != head {
		t.Fatalf("trunk is at %s after the stop, want the reworked story's tip %s", got, head)
	}
	keptHolds(t, main, "on the landed record,", moved, 2)
}
