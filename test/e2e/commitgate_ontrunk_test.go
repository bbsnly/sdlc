// US-059 end to end: the commit gate run again on a story that was put onto a
// trunk that moved, through the built binary and the shipped hook.
//
// A session works a story in its worktree; another story lands meanwhile, in
// a commit that also changes the loop's own configuration. The story gets onto
// that trunk either by the stop's own replay, which then refuses because a
// configured command fails there (SDLC-E0055) and sends the session back
// through its gates, or by a rebase a person runs. `sdlc gate commit pass`
// then runs again: the range it prints and records is the story's own commits
// and none of trunk's (AC-3), it does not warn that the story changes the
// loop's files, which only trunk's commit did (AC-10), and the stop after it
// lands the story.
//
// Every commit of the session's is asked of the hook first, so the hook has
// watched the heads it was made on, as in a person's project; the commits the
// stop or the rebase made again are on heads it never saw. Helpers carry a
// gate prefix.

package e2e

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// gateNeedsFixed fails on a tree that has another story's CHANGES.md until
// the story adds FIXED.md.
const gateNeedsFixed = "test ! -f CHANGES.md || test -f FIXED.md"

// gateRepository is ontoRepository with a commands.test that another story's
// landing breaks for this one.
func gateRepository(t *testing.T, binary string) checkout {
	t.Helper()
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	setConfig(t, main.dir, "commands", "test", gateNeedsFixed)
	commitEverything(t, main.dir, "a test command another story's file fails")
	return main
}

// gateTrunkMoves is another story landing on trunk: a file of its own and, in
// the same commit, a change to the loop's configuration.
func gateTrunkMoves(t *testing.T, main checkout) string {
	t.Helper()
	writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "another story's work\n")
	setConfig(t, main.dir, "loop", "max_stop_blocks", 4)
	return commitPaths(t, main.dir, "another story: its work and the loop's configuration", "CHANGES.md", ".sdlc/config.json")
}

// gateReviewedAgain records the verifier's and the code reviews again, each
// asked of the hook first.
func gateReviewedAgain(s *ontoStory) {
	s.t.Helper()
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			if r.WhenDoRAdvocate {
				continue
			}
			s.do(r.Role, "Bash", running(s.at.in(s.tree, "sdlc review add "+string(gate)+" "+r.Role+" approve")))
			s.here().runWith(s.t, "# "+r.Role+", again\n", "review", "add", string(gate), r.Role, "approve")
		}
	}
}

// gateCommitAgain is the session running `sdlc gate commit pass` again in the
// worktree, which has to work, and what it printed on each stream.
func gateCommitAgain(s *ontoStory) (stdout, stderr string) {
	s.t.Helper()
	args := []string{"gate", "commit", "pass", "--note", "again"}
	s.do("", "Bash", running(s.at.in(s.tree, "sdlc "+strings.Join(args, " "))))
	stdout, stderr, code := s.here().outcome(s.t, "", args...)
	if code != 0 {
		s.t.Fatalf("sdlc gate commit pass, run again in %s, exited %d:\n%s%s", s.tree, code, stdout, stderr)
	}
	return stdout, stderr
}

// gateCommits is every commit in from..to.
func gateCommits(t *testing.T, dir, from, to string) []string {
	t.Helper()
	commits := strings.Fields(gitOut(t, dir, "rev-list", from+".."+to))
	slices.Sort(commits)
	return commits
}

// gateHolds holds what the gate printed and recorded to AC-3 and AC-10: the
// range is on..head, the trunk commit the story sits on and the story's tip,
// what git lists over the recorded pair is the story's own commits with
// trunk's commit not among them, and nothing warns of the loop's
// configuration, which only trunk's commit changed.
func gateHolds(t *testing.T, main checkout, tree, stdout, stderr, on, head string) {
	t.Helper()
	if want := "committed on story/US-001 in " + on[:12] + ".." + head[:12]; !strings.Contains(stdout, want) {
		t.Errorf("AC-3: the commit gate did not print %q:\n%s", want, stdout)
	}
	var record struct {
		CommitBase   string `json:"commit_base"`
		Commit       string `json:"commit"`
		RangeUnknown bool   `json:"range_unknown"`
	}
	readJSON(t, main.record("US-001"), &record)
	if record.CommitBase != on || record.Commit != head || record.RangeUnknown {
		t.Errorf("AC-3: the record names %q..%q (range_unknown %v), want %s..%s",
			record.CommitBase, record.Commit, record.RangeUnknown, on, head)
	}
	if record.CommitBase != "" && record.Commit != "" {
		own := gateCommits(t, tree, on, head)
		held := gateCommits(t, tree, record.CommitBase, record.Commit)
		if !slices.Equal(held, own) {
			t.Errorf("AC-3: the recorded range holds\n%s\nwant the story's own commits:\n%s",
				strings.Join(held, "\n"), strings.Join(own, "\n"))
		}
		if slices.Contains(held, on) {
			t.Errorf("AC-3: the recorded range holds %s, trunk's commit", on[:12])
		}
	}
	said := stdout + stderr
	if strings.Contains(said, "which the landing refuses") || strings.Contains(said, ".sdlc/config.json") {
		t.Errorf("AC-10: the commit gate warns of the loop's configuration, which only trunk's commit changed:\n%s", said)
	}
}

// gateLands is the session passing the retro again, which the commit gate
// recorded again reopened, and stopping: the story lands on the trunk commit
// it sits on, by fast-forward to its tip.
func gateLands(s *ontoStory, main checkout, head string) {
	s.t.Helper()
	if gate := s.nextGate(); gate != string(model.GateRetro) {
		s.t.Fatalf("after the commit gate the next gate is %q, want the retro", gate)
	}
	s.stores("bookkeeper", "retro", "# Retro\n\nPut onto a trunk that moved.\n")
	s.run("", "gate", "retro", "pass", "--note", "worked by the session")
	s.run("", "stop")
	if got := landTrunk(s.t, main); got != head {
		s.t.Errorf("trunk is at %s after the stop, want the story's tip %s", got, head)
	}
	if got := statuses(s.t, main.backlog())["US-001"]; got != "done" {
		s.t.Errorf("US-001 is %s after the stop, want done", got)
	}
}

// AC-3 and AC-10, the stop's own replay: the stop puts the story onto the
// moved trunk and refuses with SDLC-E0055, because commands.test fails there.
// The session does what the fix says -- the implementation recorded as failed,
// the story put right in a commit of its own, every gate from there again --
// and the commit gate names the story's three commits, says nothing of the
// loop's configuration, and the next stop lands the story.
func TestAC3AndAC10TheCommitGateRunAgainAfterTheStopsOwnReplayNamesOnlyTheStorysCommits(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := gateRepository(t, binary)
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	s.worked()
	landBookkeeping(t, s.treeSession, true)
	on := gateTrunkMoves(t, main)

	s.do("", "Bash", running(s.at.in(s.tree, "sdlc stop")))
	out, code := s.here().try(t, "stop")
	if code == 0 || !strings.Contains(out, "SDLC-E0055") || !strings.Contains(out, "commands.test") {
		t.Fatalf("the stop exited %d, want SDLC-E0055 naming commands.test:\n%s", code, out)
	}
	if got := landTrunk(t, main); got != on {
		t.Fatalf("the refused stop moved trunk to %s", got)
	}
	if gitOut(t, s.tree, "merge-base", on, "HEAD") != on {
		t.Fatalf("precondition: the refused stop did not leave the story on trunk at %s", on[:12])
	}

	s.run("", "gate", "implementation", "fail", "--note", "commands.test fails on trunk as it is now")
	var stdout, stderr string
	for gate := s.nextGate(); gate != "" && gate != string(model.GateRetro); gate = s.nextGate() {
		switch model.Gate(gate) {
		case model.GateImplementation:
			s.writes("implementer", "FIXED.md", "what trunk's change asked of the story\n")
		case model.GateVerification:
			s.stores("verifier", "verification", "# Verification\n\nEvery command green on trunk as it is.\n")
		case model.GateCommit:
			git(t, s.tree, "add", "-A")
			s.do("", "Bash", running(s.at.in(s.tree, "git commit -m rework")))
			git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "rework")
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
		if model.Gate(gate) == model.GateCommit {
			stdout, stderr = gateCommitAgain(s)
			continue
		}
		s.run("", "gate", gate, "pass", "--note", "worked again by the session")
	}
	head := headOf(t, s.tree)
	if got := len(gateCommits(t, s.tree, on, head)); got != 3 {
		t.Fatalf("precondition: the story has %d commits on trunk's, want its work, its bookkeeper's and the rework", got)
	}

	gateHolds(t, main, s.tree, stdout, stderr, on, head)
	gateLands(s, main, head)
}

// AC-3 and AC-10, a rebase by hand: a person rebases the finished story onto
// the moved trunk, the reviews are recorded again, and the commit gate run
// again names the story's two commits, says nothing of the loop's
// configuration, and the stop lands the story.
func TestAC3AndAC10TheCommitGateRunAgainAfterARebaseByHandNamesOnlyTheStorysCommits(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	s.worked()
	landBookkeeping(t, s.treeSession, true)
	on := gateTrunkMoves(t, main)

	git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--quiet", "main")
	head := headOf(t, s.tree)
	if got := len(gateCommits(t, s.tree, on, head)); got != 2 {
		t.Fatalf("precondition: the story has %d commits on trunk's, want its work and its bookkeeper's", got)
	}
	gateReviewedAgain(s)

	stdout, stderr := gateCommitAgain(s)
	gateHolds(t, main, s.tree, stdout, stderr, on, head)
	gateLands(s, main, head)
}
