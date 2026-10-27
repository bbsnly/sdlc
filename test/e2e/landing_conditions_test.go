// US-090 end to end, its AC-6: a configured command fails on the tree a stop
// made when it put a story onto a trunk that moved. The refusal leaves the
// story with its session, and this follows the fix through the built binary
// and the shipped hook, every command of the session's asked of the hook
// first: the command run on trunk as it is, the implementation recorded as
// failed, the story put right, its gates again, and the stop that lands it.
//
// The AC number is US-090's. Helpers are landing_ontrunk_test.go's and
// commitgate_ontrunk_test.go's; the ones added here carry a held prefix.

package e2e

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// heldNeedsFixed passes on trunk, with another story's CHANGES.md and without
// this story's source, and fails on the tree that has both until the story
// adds FIXED.md: a failure that is the story's to put right and not trunk's.
const heldNeedsFixed = "test ! -f CHANGES.md || test ! -f us001.go || test -f FIXED.md"

// heldRecord is what the story's record says of a replay and of a question to
// a person, read from the main checkout.
type heldRecord struct {
	Replay *struct {
		To   string `json:"to"`
		Onto string `json:"onto"`
	} `json:"replay"`
	Escalations []struct {
		Type     string `json:"type"`
		Resolved bool   `json:"resolved"`
	} `json:"escalations"`
	Events []struct {
		Type string `json:"type"`
	} `json:"events"`
}

func heldRecordOf(t *testing.T, main checkout) heldRecord {
	t.Helper()
	var r heldRecord
	readJSON(t, main.record("US-001"), &r)
	return r
}

// heldWithItsSession holds the story to being its session's still: in
// progress on the backlog, the iteration running in the worktree, and nothing
// on the record that hands it to a person.
func heldWithItsSession(t *testing.T, s *ontoStory, main checkout, when string) {
	t.Helper()
	if got := statuses(t, main.backlog())["US-001"]; got != "in_progress" {
		t.Errorf("%s the backlog has US-001 as %q, want in_progress: the story is the session's to rework", when, got)
	}
	if got := s.here().document(t, "status")["active"]; got != "US-001" {
		t.Errorf("%s the worktree's iteration is on %v, want US-001 still running", when, got)
	}
	r := heldRecordOf(t, main)
	if len(r.Escalations) != 0 {
		t.Errorf("%s the record holds a question to a person: %+v", when, r.Escalations)
	}
	for _, e := range r.Events {
		if strings.Contains(e.Type, "escalat") {
			t.Errorf("%s the record has a %s event", when, e.Type)
		}
	}
}

// AC-6: commands.test fails on the tree the stop made and passes on trunk.
// The stop lands nothing and says SDLC-E0055; the story is not handed to a
// person, and its fix names the way back in. Each of the session's next
// commands is put to the hook, which lets it through, and run: the command on
// trunk in the main checkout, `sdlc gate implementation fail --story US-001`
// in the worktree, the rework and its commit, the gates again, and `sdlc
// stop`, which lands the story with nobody but the session having acted.
func TestAC6ACommandThatFailsOnTheTreeAStopMadeLeavesTheStoryWithItsSessionToReworkAndLand(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1})
	setConfig(t, main.dir, "commands", "test", heldNeedsFixed)
	commitEverything(t, main.dir, "a test command this story fails on another story's file")
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	s.worked()
	landBookkeeping(t, s.treeSession, true)
	writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "another story's work\n")
	moved := commitPaths(t, main.dir, "another story lands", "CHANGES.md")

	s.do("", "Bash", running(s.at.in(s.tree, "sdlc stop")))
	out, code := s.here().try(t, "stop")
	if code == 0 || !strings.Contains(out, "SDLC-E0055") || !strings.Contains(out, "commands.test") {
		t.Fatalf("the stop exited %d, want SDLC-E0055 naming commands.test:\n%s", code, out)
	}
	if got := landTrunk(t, main); got != moved {
		t.Fatalf("the refused stop moved trunk to %s", got)
	}
	made := headOf(t, s.tree)
	if gitOut(t, s.tree, "merge-base", moved, made) != moved {
		t.Fatalf("precondition: the refused stop did not leave the story on trunk at %s", moved[:12])
	}
	if r := heldRecordOf(t, main); r.Replay == nil || r.Replay.To != made || r.Replay.Onto != moved {
		t.Fatalf("precondition: the record's replay mark is %+v, want the tree this stop made, %s on %s", r.Replay, made, moved)
	}
	heldWithItsSession(t, s, main, "after the command failed,")

	// What the refusal tells the session to do next, and nothing that sends
	// the story to a person.
	said := strings.Join(strings.Fields(out), " ")
	route := "sdlc gate implementation fail --story US-001 --note"
	for _, want := range []string{"the story is still the session's", "run that same command on main as it is", route} {
		if !strings.Contains(said, want) {
			t.Fatalf("the refusal does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(said, "sdlc escalate") {
		t.Errorf("the refusal hands the story to a person, and a command that failed is the session's to rework:\n%s", out)
	}

	// The fix's first command: the same command on trunk, in the checkout
	// that has trunk checked out. It passes there, so the failure is the
	// story's and the fix's second route is the one to take.
	inMain := "cd " + strconv.Quote(main.dir) + " && "
	s.do("", "Bash", running(inMain+"git status"))
	s.do("", "Bash", running(inMain+heldNeedsFixed))
	onTrunk := exec.CommandContext(t.Context(), "sh", "-c", heldNeedsFixed)
	onTrunk.Dir = main.dir
	if err := onTrunk.Run(); err != nil {
		t.Fatalf("precondition: commands.test fails on trunk too, so the failure is not the story's: %v", err)
	}

	// The fix's second: the implementation recorded as failed, by the route
	// it names.
	note := "commands.test failed on the tree the stop made"
	s.do("", "Bash", running(s.at.in(s.tree, route+" "+strconv.Quote(note))))
	s.here().run(t, "gate", "implementation", "fail", "--story", "US-001", "--note", note)
	if gate := s.nextGate(); gate != string(model.GateImplementation) {
		t.Fatalf("after the implementation was recorded as failed the next gate is %q, want the implementation", gate)
	}
	heldWithItsSession(t, s, main, "after the implementation was recorded as failed,")
	if r := heldRecordOf(t, main); r.Replay != nil {
		t.Errorf("the record still carries the replay mark %+v once the story is reopened: no later stop may take a tip for the reviewed change on it", r.Replay)
	}

	// The rework, every gate from the implementation on, as the session works
	// them: each command and each write asked of the hook first.
	for gate := s.nextGate(); gate != ""; gate = s.nextGate() {
		switch model.Gate(gate) {
		case model.GateImplementation:
			s.writes("implementer", "FIXED.md", "what trunk's change asked of the story\n")
		case model.GateVerification:
			s.stores("verifier", "verification", "# Verification\n\nEvery command green on trunk as it is.\n")
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
		s.run("", "gate", gate, "pass", "--note", "worked again by the session")
	}
	heldWithItsSession(t, s, main, "with every gate passed again,")
	head := headOf(t, s.tree)

	s.do("", "Bash", running(s.at.in(s.tree, "sdlc stop")))
	out, code = s.here().try(t, "stop")
	if code != 0 {
		t.Fatalf("the stop after the rework exited %d, want the story landed:\n%s", code, out)
	}
	if got := landTrunk(t, main); got != head {
		t.Errorf("trunk is at %s after the stop, want the reworked story's tip %s", got, head)
	}
	for _, f := range []string{s.source, s.test, "FIXED.md", "CHANGES.md"} {
		if !ontoHolds(t, main, f) {
			t.Errorf("trunk does not hold %s after the stop", f)
		}
	}
	if got := statuses(t, main.backlog())["US-001"]; got != "done" {
		t.Errorf("US-001 is %s after the stop, want done", got)
	}
	if r := heldRecordOf(t, main); len(r.Escalations) != 0 {
		t.Errorf("the story reached trunk with a question to a person on its record: %+v", r.Escalations)
	}
}
