// US-091 end to end, in the sandbox: trunk's branch put on a commit of the
// story's own does not make the test files that commit added trunk's.
//
// A session works the story in its worktree through the gates, every command
// put to the hook first. A test file is added after the freeze by a route the
// hook did not see: the hook refuses the session's own write, so the file is
// written here, as a person or an unread spelling would write it. The session
// commits it. Then a person moves trunk to that commit -- a fast-forward in the
// main checkout, which is never put to the hook because no tool call is made --
// and the next gate and the stop are asked what they say. Before the fix the
// commit gate printed that it accepted the file as trunk's and the stop landed
// it; they now say what they said with trunk where the story began.
//
// The control is AC-2: a test file another story landed on trunk is still
// accepted as trunk's, and the stop lands the story.

package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// onLate is the test file added after the freeze.
const onLate = "late_test.go"

// onLateBody is what the late test holds.
const onLateBody = "package main\n\n// written after the freeze\n"

// onWorksUpTo takes the story through every gate before the one named.
func (sb sandbox) onWorksUpTo(t *testing.T, gate model.Gate) {
	t.Helper()
	for next := sb.nextGate(t); next != string(gate); next = sb.nextGate(t) {
		if next == "" {
			t.Fatalf("the story finished before it reached %s", gate)
		}
		sb.worksGate(t, model.Gate(next))
	}
}

// onReviewsAgain is the session recording the verifier's and the code reviews
// again on the tree as it stands, each put to the hook first.
func (sb sandbox) onReviewsAgain(t *testing.T) {
	t.Helper()
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			if !r.WhenDoRAdvocate {
				sb.works(t, sb.tree(), r.Role, "# "+r.Role+", again\n", "review", "add", string(gate), r.Role, "approve")
			}
		}
	}
}

// onSessionCommits is a commit of the session's: put to the hook, which lets it
// through once the code review has passed, and then made.
func (sb sandbox) onSessionCommits(t *testing.T, message string, paths ...string) string {
	t.Helper()
	add := []string{"add", "-A"}
	if len(paths) > 0 {
		add = append(add, "--")
		add = append(add, paths...)
	}
	sb.mustGit(t, sb.tree(), add...)
	sb.allows(t, sb.tree(), "", "Bash", map[string]any{"command": "git commit -m '" + message + "'"})
	sb.mustGit(t, sb.tree(), "commit", "--quiet", "-m", message)
	return sb.mustGit(t, sb.tree(), "rev-parse", "HEAD")
}

// onTrunk is where trunk's branch is.
func (sb sandbox) onTrunk(t *testing.T) string {
	t.Helper()
	return sb.mustGit(t, sb.repo, "rev-parse", "refs/heads/main")
}

// onUnheld holds a command to the refusal with SDLC-E0043 that names onLate,
// and returns what it printed.
func onUnheld(t *testing.T, what string, r result) string {
	t.Helper()
	const code = "SDLC-E0043"
	said := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(said, code) || !strings.Contains(said, onLate) {
		t.Fatalf("%s exited %d, want it refused with %s naming %s:\n%s", what, r.code, code, onLate, said)
	}
	return said
}

// onNothingTaken holds the story's record to taking nothing for trunk's.
func (sb sandbox) onNothingTaken(t *testing.T) {
	t.Helper()
	var record struct {
		TrunkTests *struct {
			Commit string   `json:"commit"`
			Files  []string `json:"files"`
		} `json:"trunk_tests"`
	}
	readJSON(t, filepath.Join(sb.repo, ".sdlc", "stories", "US-001", "gate-record.json"), &record)
	if record.TrunkTests != nil {
		t.Errorf("the record's trunk_tests is %+v, and the story's own test file was taken for trunk's", *record.TrunkTests)
	}
}

// AC-3, the next gate: trunk is moved to the story's first commit, which adds a
// test file after the freeze. The commit gate is refused with SDLC-E0043 naming
// the file, as it was before trunk moved, and says nothing of accepting it as
// trunk's. The record takes nothing for trunk's, the commit gate has not passed,
// and the story is still at the commit.
func TestAC3TrunkMovedToTheStorysFirstCommitStillGetsItsLateTestRefusedAtTheCommitGate(t *testing.T) {
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	sb.onWorksUpTo(t, model.GateVerifierReview)

	path := filepath.Join(tree, onLate)
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, "implementer", "Write",
		map[string]any{"file_path": path, "content": onLateBody}))
	if code != 0 || stdout == carryOn {
		t.Fatalf("the hook let the implementer write %s after the freeze: exit %d\n%s%s", onLate, code, stdout, stderr)
	}
	writeFile(t, path, onLateBody)

	sb.worksGate(t, model.GateVerifierReview)
	sb.worksGate(t, model.GateCodeReview)
	first := sb.onSessionCommits(t, "test: US-001", "invoice_test.go", onLate)
	sb.onSessionCommits(t, "feat: US-001")

	commitGate := []string{"gate", "commit", "pass", "--note", "committed"}
	before := sb.does(t, tree, "", "", commitGate...)
	onUnheld(t, "the commit gate with trunk where the story began", before)

	sb.mustGit(t, sb.repo, "merge", "--ff-only", "--quiet", first)
	if got := sb.onTrunk(t); got != first {
		t.Fatalf("precondition: trunk is at %s, want the story's first commit %s", got, first)
	}
	after := sb.does(t, tree, "", "", commitGate...)
	said := onUnheld(t, "the commit gate with trunk on the story's first commit", after)
	if strings.Contains(said, "accepted as") {
		t.Errorf("the commit gate says it accepted a test file as trunk's, and the file is the story's:\n%s", said)
	}
	sb.onNothingTaken(t)
	if gate := sb.nextGate(t); gate != string(model.GateCommit) {
		t.Errorf("the story's next gate is %q after the refusal, want the commit gate", gate)
	}
}

// AC-3, the stop: a finished story, a commit a person made by hand on its branch
// that adds a test file nobody froze, the bookkeeper's commit on top, and trunk
// moved by hand to the person's commit. The stop is refused with SDLC-E0043
// naming the file -- not with SDLC-E0029 asking for the reviews, which the
// stop answered before the fix and then landed the story once they were
// recorded again -- and recording the reviews again does not let it land.
func TestAC3TrunkMovedToAStoryCommitThatAddsATestFileIsRefusedAtTheStopAndNothingLands(t *testing.T) {
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	sb.worksEveryGate(t)

	writeFile(t, filepath.Join(tree, onLate), onLateBody)
	sb.mustGit(t, tree, "add", "--", onLate)
	sb.mustGit(t, tree, "commit", "--quiet", "-m", "test by hand")
	byHand := sb.mustGit(t, tree, "rev-parse", "HEAD")
	// The person's commit changed the work after its reviews, so the hook holds
	// the session's next commit until they are recorded again.
	sb.onReviewsAgain(t)
	sb.writesFile(t, "bookkeeper", "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices.\n")
	tip := sb.onSessionCommits(t, "docs: map US-001", "CODEMAP.md")

	before := sb.does(t, tree, "", "", "stop")
	onUnheld(t, "the stop with trunk where the story began", before)

	sb.mustGit(t, sb.repo, "merge", "--ff-only", "--quiet", byHand)
	after := sb.does(t, tree, "", "", "stop")
	said := onUnheld(t, "the stop with trunk on the person's commit", after)
	if !strings.Contains(said, "Nothing landed") || strings.Contains(said, "accepted as") {
		t.Errorf("the stop does not say nothing landed, or says it accepted a test file as trunk's:\n%s", said)
	}

	sb.onReviewsAgain(t)
	again := sb.does(t, tree, "", "", "stop")
	onUnheld(t, "the stop with the reviews recorded again", again)

	if got := sb.onTrunk(t); got != byHand {
		t.Errorf("trunk is at %s after the refused stops, want it left at %s", got, byHand)
	}
	if got := sb.mustGit(t, sb.repo, "rev-parse", "refs/heads/story/US-001"); got != tip {
		t.Errorf("story/US-001 is at %s after the refused stops, want it left at %s", got, tip)
	}
	if got := storiesOf(t, sb.repo)["US-001"]; got == "done" {
		t.Error("a story that did not land is done")
	}
	sb.onNothingTaken(t)
}

// AC-2 and AC-3: the control. Another story lands a test file on trunk, which
// the story never touched, and the stop puts the story onto it and lands it,
// saying that it accepted the file as trunk's. It passes before the fix and has
// to go on passing.
func TestAC2AndAC3ATestFileAnotherStoryLandedIsStillAcceptedAsTrunksAtTheStop(t *testing.T) {
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	sb.worksEveryGate(t)

	writeFile(t, filepath.Join(sb.repo, "foreign_test.go"), "package main\n\n// another story's test\n")
	sb.mustGit(t, sb.repo, "add", "--", "foreign_test.go")
	sb.mustGit(t, sb.repo, "commit", "--quiet", "-m", "another story's test")
	moved := sb.onTrunk(t)

	r := sb.does(t, tree, "", "", "stop")
	said := r.stdout + r.stderr
	if r.code != 0 {
		t.Fatalf("the stop exited %d on a trunk that gained another story's test file:\n%s", r.code, said)
	}
	if !strings.Contains(said, "1 test file accepted as main's") || !strings.Contains(said, "foreign_test.go") {
		t.Errorf("the stop does not say it accepted foreign_test.go as trunk's:\n%s", said)
	}
	if got := sb.onTrunk(t); got == moved {
		t.Errorf("trunk is still at %s after the stop", moved)
	}
	if got := storiesOf(t, sb.repo)["US-001"]; got != "done" {
		t.Errorf("US-001 is %s after the stop, want done", got)
	}
}
