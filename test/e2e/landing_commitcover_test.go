// US-092 end to end: what a person's approval at Gate 8 covers, driven through
// the built binary and the plugin launcher in a real git repository.
//
// A story in a tier that waits for a person is approved before any commit of
// it exists, then committed, then passed at the commit gate, which records
// the commits. A session can rewrite those commits afterwards with commands
// the hook lets through, and `sdlc stop` used to land the rewrite on the
// approval. Now the stop refuses with SDLC-E0037 and trunk's ref stays where
// it was (AC-1), unless the stop's own replay made the commits again, which
// keeps each one's message and author and leaves the approval standing, and
// does not launder a tip that was rewritten before it (AC-2). A tier that does
// not wait for a person is judged as before.
//
// The story is worked as the runbook directs, in its worktree, with two
// commits of its own. Helpers carry a cc prefix, for commit cover.

package e2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// ccRepository is ontoRepository with US-001 in the risk tier given.
func ccRepository(t *testing.T, binary, tier string) checkout {
	t.Helper()
	main := ontoRepository(t, binary, spec{id: "US-001", priority: 1})
	if tier != "" {
		var backlog map[string]any
		readJSON(t, main.backlog(), &backlog)
		stories, _ := backlog["stories"].([]any)
		for _, s := range stories {
			if story, ok := s.(map[string]any); ok && story["id"] == "US-001" {
				story["risk_tier"] = tier
			}
		}
		writeJSON(t, main.backlog(), backlog)
		commitEverything(t, main.dir, "US-001 is in tier "+tier)
	}
	return main
}

// ccStory is US-001 worked by a session, with the commits it made.
type ccStory struct {
	*ontoStory
	main          checkout
	cut           string
	first, second string
}

// ccCommit is the session committing the paths, the hook asked first, as the
// author given, or the repository's own where there is none.
func (s *ccStory) commit(message, author string, paths ...string) string {
	s.t.Helper()
	git(s.t, s.tree, append([]string{"add", "--"}, paths...)...)
	s.do("", "Bash", running(s.at.in(s.tree, "git commit -m '"+message+"'")))
	args := []string{"-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message}
	if author != "" {
		args = append(args, "--author", author)
	}
	git(s.t, s.tree, args...)
	return headOf(s.t, s.tree)
}

// ccWorked takes the story through every gate as the runbook directs. In a
// tier that waits, the session hands the story over before the commit, a
// person approves it, and the session resumes; the story then has two commits,
// the source file's and the rest, the first as firstAuthor where one is given.
func ccWorked(t *testing.T, binary string, main checkout, firstAuthor string) *ccStory {
	t.Helper()
	s := &ccStory{ontoStory: ontoBegun(t, binary, main, sessionFirst, "US-001"), main: main, cut: landTrunk(t, main)}
	for gate := s.nextGate(); gate != ""; gate = s.nextGate() {
		switch model.Gate(gate) {
		case model.GateDoR, model.GateDesignReview, model.GateVerifierReview, model.GateCodeReview:
		case model.GateAnalysis:
			s.stores("researcher", "analysis", "# Analysis\n\nAC-1 is testable.\n")
			s.stores("researcher", "threats", "# Threats\n\nNothing crosses a trust boundary.\n")
		case model.GateTestsFrozen:
			s.writes("sdet", s.test, "package main\n\n// AC-1 of "+s.story+"\n")
			s.stores("sdet", "test_plan", "# Test plan\n\nAC-1 -> TestRejectsZero\n")
			s.run("sdet", "freeze")
		case model.GatePlan:
			s.stores("implementer", "plan", "# Plan\n\nOne step.\n")
		case model.GateImplementation:
			s.writes("implementer", s.source, "package main\n\n// "+s.story+"\n")
		case model.GateVerification:
			s.stores("verifier", "verification", "# Verification\n\nEvery command green.\n")
		case model.GateCommit:
			s.run("", "escalate", "pre_commit_approval", "--message", "ready to commit")
			if out := s.here().as("").run(t, "approve", s.story); !strings.Contains(out, s.story) {
				t.Fatalf("approve printed %q, want it to name %s", out, s.story)
			}
			s.do("", "Bash", running(s.at.in(s.tree, "sdlc start --json")))
			s.here().run(t, "start", "--json")
			s.first = s.commit("feat: the source, with a trailer\n\nCo-Authored-By: Dave <dave@example.org>", firstAuthor, s.source)
			s.second = s.commit("feat: the test", "", s.test)
		case model.GateRetro:
			s.stores("bookkeeper", "retro", "# Retro\n\nNo deviations.\n")
		}
		for _, r := range model.ReviewersFor(model.Gate(gate)) {
			if r.WhenDoRAdvocate {
				continue
			}
			s.do(r.Role, "Bash", running(s.at.in(s.tree, "sdlc review add "+gate+" "+r.Role+" approve")))
			s.here().runWith(t, "# "+r.Role+"\n", "review", "add", gate, r.Role, "approve")
		}
		s.run("", "gate", gate, "pass", "--note", "worked by the session")
	}
	return s
}

// refusedAndTrunkStays is the session's stop refused with SDLC-E0037 and a fix
// a person can act on, with trunk's ref where it was, and the story's branch
// and worktree still there.
func (s *ccStory) refusedAndTrunkStays(want string) {
	s.t.Helper()
	out, code := s.here().try(s.t, "stop")
	refusedWith(s.t, out, code != 0, "SDLC-E0037")
	if got := landTrunk(s.t, s.main); got != want {
		s.t.Errorf("trunk is at %s after the refused stop, want it where it was, %s", got, want)
	}
	if _, ok := branchesOf(s.t, s.main.dir)["story/"+s.story]; !ok {
		s.t.Errorf("story/%s is gone after a refused stop", s.story)
	}
	if !exists(s.tree) {
		s.t.Error("the refused stop took the story's worktree away")
	}
}

// ccApprovalCount is the number of approvals on the story's record.
func ccApprovalCount(t *testing.T, main checkout) int {
	t.Helper()
	var record struct {
		Approvals []any `json:"approvals"`
	}
	readJSON(t, main.record("US-001"), &record)
	return len(record.Approvals)
}

// ccTamperings are the ways a session changes the story's commits after the
// commit gate, with commands the hook lets through or git alone does.
var ccTamperings = []struct {
	name   string
	tamper func(t *testing.T, s *ccStory)
}{
	{"AC-1 the tip's message amended", func(t *testing.T, s *ccStory) {
		t.Helper()
		git(t, s.tree, "commit", "--quiet", "--amend", "-m", "feat: something else")
	}},
	{"AC-1 the tip's author amended", func(t *testing.T, s *ccStory) {
		t.Helper()
		git(t, s.tree, "commit", "--quiet", "--amend", "--no-edit", "--author", "Mallory <mallory@example.com>")
	}},
	{"AC-1 the middle commit reworded", func(t *testing.T, s *ccStory) {
		t.Helper()
		git(t, s.tree, "reset", "--quiet", "--hard", s.cut)
		git(t, s.tree, "cherry-pick", "--no-commit", s.first)
		git(t, s.tree, "commit", "--quiet", "-m", "feat: the source, reworded")
		git(t, s.tree, "cherry-pick", s.second)
	}},
	{"AC-1 the commits squashed into one with the same tree", func(t *testing.T, s *ccStory) {
		t.Helper()
		git(t, s.tree, "reset", "--quiet", "--soft", s.cut)
		git(t, s.tree, "commit", "--quiet", "-m", "feat: both")
	}},
}

// ---------------------------------------------------------------- AC-1

// AC-1: a commit of the story changed after the commit gate, in a tier that
// waits for a person, is refused by `sdlc stop` with SDLC-E0037, and trunk's
// ref is where it was.
func TestAC1AStopRefusesAStoryWhoseCommitsWereChangedAfterTheCommitGateAndLandsNothing(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	for _, tc := range ccTamperings {
		t.Run(tc.name, func(t *testing.T) {
			main := ccRepository(t, binary, "high")
			s := ccWorked(t, binary, main, "")
			tc.tamper(t, s)

			s.refusedAndTrunkStays(s.cut)
		})
	}
}

// AC-1: the runbook's own Gate 9 docs commit, which changes the root
// CODEMAP.md alone, still lands above the recorded range. Passes before the
// fix as well; it is here so that the fix cannot close the runbook.
func TestAC1ACodemapOnlyCommitAboveTheRecordedRangeStillLandsThroughTheBinary(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ccRepository(t, binary, "high")
	s := ccWorked(t, binary, main, "")
	docs := landBookkeeping(t, s.treeSession, true)

	s.run("", "stop")
	if got := landTrunk(t, main); got != docs {
		t.Errorf("trunk is at %s after the stop, want the story's tip, the docs commit, %s", got, docs)
	}
	if got := statuses(t, main.backlog())["US-001"]; got != "done" {
		t.Errorf("US-001 is %s after the stop, want done", got)
	}
}

// AC-1 non-goal: a story in a tier that does not wait for a person is judged
// as before. The amended tip lands. Passes before the fix.
func TestAC1AStoryInATierThatDoesNotWaitLandsAnAmendedTipAsBefore(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ccRepository(t, binary, "low")
	s := ontoBegun(t, binary, main, sessionFirst, "US-001")
	s.worked()
	git(t, s.tree, "commit", "--quiet", "--amend", "-m", "feat: something else", "--author", "Mallory <mallory@example.com>")
	amended := headOf(t, s.tree)

	s.run("", "stop")
	if got := landTrunk(t, main); got != amended {
		t.Errorf("trunk is at %s after the stop, want the amended tip %s: a tier that does not wait is judged as before", got, amended)
	}
}

// ---------------------------------------------------------------- AC-2

// AC-2: trunk moved, so the stop itself makes the story's commits again. Each
// keeps its message, trailers included, and its author, the first being
// another person's; the approval stands and nobody is asked again. Passes
// before the fix: it is the half that must stay true.
func TestAC2TheStopsOwnReplayKeepsEachCommitsMessageAndAuthorAndTheApprovalStands(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := ccRepository(t, binary, "high")
	s := ccWorked(t, binary, main, "Carol <carol@example.org>")
	landBookkeeping(t, s.treeSession, true)
	const format = "--format=%an <%ae>%n%B%x00"
	before := gitOut(t, s.tree, "log", "--reverse", format, s.cut+"..HEAD")
	approvals := ccApprovalCount(t, main)
	writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "another story's work\n")
	moved := commitPaths(t, main.dir, "another story lands", "CHANGES.md")

	s.run("", "stop")
	tip := landTrunk(t, main)
	if tip == moved || gitOut(t, main.dir, "merge-base", moved, tip) != moved {
		t.Fatalf("trunk is at %s after the stop, want the story's commits on top of %s", tip, moved)
	}
	if after := gitOut(t, main.dir, "log", "--reverse", format, moved+".."+tip); after != before {
		t.Errorf("the replay changed a message or an author:\nbefore\n%s\nafter\n%s", before, after)
	}
	if !strings.Contains(before, "Carol <carol@example.org>") || !strings.Contains(before, "Co-Authored-By: Dave") {
		t.Fatalf("precondition: the story's first commit does not carry Carol and the trailer:\n%s", before)
	}
	if got := ccApprovalCount(t, main); got != approvals {
		t.Errorf("the record has %d approvals after the landing, want the %d it had: nobody is asked again", got, approvals)
	}
}

// AC-2: a tip changed before the replay is not laundered by it. Trunk moved,
// and the tip's message or author was changed after the commit gate: the stop
// is refused with SDLC-E0037, trunk stays at the commit that moved it, and the
// story's branch is not replayed.
func TestAC2ATamperedTipIsRefusedWhenTrunkMovedAndTheReplayDoesNotLaunderIt(t *testing.T) {
	parallel(t)
	skipWithoutTheLauncher(t)
	binary := build(t)
	for _, tc := range ccTamperings[:2] {
		t.Run(strings.Replace(tc.name, "AC-1", "AC-2", 1), func(t *testing.T) {
			main := ccRepository(t, binary, "high")
			s := ccWorked(t, binary, main, "")
			tc.tamper(t, s)
			tampered := headOf(t, s.tree)
			writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "another story's work\n")
			moved := commitPaths(t, main.dir, "another story lands", "CHANGES.md")

			s.refusedAndTrunkStays(moved)
			if got := headOf(t, s.tree); got != tampered {
				t.Errorf("the story's branch is at %s after the refused stop, want it as the session left it, %s: it was replayed", got, tampered)
			}
		})
	}
}
