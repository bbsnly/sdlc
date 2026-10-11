// US-029.3 end to end: a session working a story in its worktree, as the
// runbook directs and through the shipped launcher, lands the finished story on
// trunk itself -- the bookkeeper's CODEMAP.md committed on the story's branch
// after the retro, then `sdlc stop` -- with no person running git; a story it
// rebased onto a trunk that moved lands too; and while it holds the story, the
// hook refuses it the commands that would move trunk or a story branch
// instead.
//
// The project ignores the loop's own state, so that what a story commits is
// its work and nothing of .sdlc/state is carried to trunk. Helpers carry a
// land prefix.

package e2e

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// landRepository is storyRepository with the loop's state ignored, and two
// stories.
func landRepository(t *testing.T, binary string) checkout {
	t.Helper()
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	git(t, main.dir, "rm", "-r", "--cached", "--quiet", "--ignore-unmatch", "--", ".sdlc/state")
	writeFile(t, filepath.Join(main.dir, ".gitignore"), "/.sdlc/state/\n")
	commitEverything(t, main.dir, "ignore the loop's state")
	return main
}

// landFinished is US-001 worked through every gate by a session opened in the
// main checkout, as the runbook directs.
func landFinished(t *testing.T, binary string, main checkout) *treeSession {
	t.Helper()
	s := &treeSession{t: t, binary: binary, id: sessionFirst, at: opening{"opened in the main checkout", main.dir, main.dir}}
	s.do("", "Bash", running("sdlc start --json"))
	s.tree = startIn(t, main, main.as(sessionFirst)).dir
	for gate := s.nextGate(); gate != ""; gate = s.nextGate() {
		s.workGate(model.Gate(gate))
	}
	return s
}

// landBookkeeping is the bookkeeper's CODEMAP.md change committed on the
// story's branch after the retro, the hook asked first where ask is set, and
// the story's tip it makes.
func landBookkeeping(t *testing.T, s *treeSession, ask bool) string {
	t.Helper()
	s.writes("bookkeeper", "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices.\n")
	if ask {
		s.do("", "Bash", running(s.at.in(s.tree, "git add CODEMAP.md && git commit -m 'docs: map US-001'")))
	}
	git(t, s.tree, "add", "--", "CODEMAP.md")
	git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "docs: map US-001")
	return headOf(t, s.tree)
}

func landTrunk(t *testing.T, main checkout) string {
	t.Helper()
	return gitOut(t, main.dir, "rev-parse", "refs/heads/main")
}

// landLogged is US-001's entry in `sdlc log --json` in dir.
func landLogged(t *testing.T, c checkout) map[string]any {
	t.Helper()
	var payload struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(c.run(t, "log", "--json")), &payload); err != nil {
		t.Fatal(err)
	}
	for _, e := range payload.Entries {
		if e["story"] == "US-001" {
			return e
		}
	}
	t.Fatalf("sdlc log in %s does not list US-001", c.dir)
	return nil
}

// AC-1, AC-2, AC-7 and AC-10 as a session meets them: the finished story lands
// when the session stops it, nothing is left behind, the story is done, and
// sdlc log prints what reached trunk in the main checkout and in the next
// story's worktree.
func TestAC1TheRunbooksSessionLandsItsFinishedStoryWithoutAPersonRunningGit(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := landRepository(t, binary)
	cut := landTrunk(t, main)
	s := landFinished(t, binary, main)
	tip := landBookkeeping(t, s, true)

	s.run("", "stop")
	if got := landTrunk(t, main); got != tip {
		t.Fatalf("trunk is at %s after the stop, want the story's tip %s", got, tip)
	}
	if merges := gitOut(t, main.dir, "rev-list", "--merges", cut+"..main"); merges != "" {
		t.Errorf("trunk has merge commits: %s", merges)
	}
	if exists(s.tree) {
		t.Error("the landed story's worktree is still there")
	}
	if _, ok := branchesOf(t, main.dir)["story/US-001"]; ok {
		t.Error("story/US-001 is still there")
	}

	next := startIn(t, main, main.as(sessionSecond))
	if next.story != "US-002" {
		t.Fatalf("the next start gave %s, want US-002", next.story)
	}
	for _, c := range []checkout{main, next.checkout} {
		e := landLogged(t, c)
		if e["commit_base"] != cut || e["commit"] != tip || e["missing"] == true || e["off_branch"] == true {
			t.Errorf("sdlc log in %s gives %v, want %s..%s on the branch", c.dir, e, cut, tip)
		}
	}
}

// AC-4 as a session meets it: a story the session rebased onto the trunk that
// moved, which the hook lets it do on its own branch, lands as rebased. That
// it need not rebase at all is US-088's, in landing_ontrunk_test.go.
func TestAC4AStoryTheSessionRebasedOntoTheTrunkThatMovedLands(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := landRepository(t, binary)
	// The rebased tree has the configured commands run on it (AC-4). sdlc
	// init's defaults for this project's Go files fail on it -- it has no main
	// function, and no coverage -- so it is given commands that pass.
	for key, command := range map[string]string{
		"build": "exit 0", "test": "exit 0", "lint": "exit 0", "fmt_check": "exit 0",
		"coverage": "echo 100", "mutation": "echo 100",
	} {
		setConfig(t, main.dir, "commands", key, command)
	}
	commitEverything(t, main.dir, "commands that pass on this project")
	s := landFinished(t, binary, main)
	landBookkeeping(t, s, false)
	writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "another story's work\n")
	git(t, main.dir, "add", "--", "CHANGES.md")
	git(t, main.dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "another story lands")

	s.do("", "Bash", running(s.at.in(s.tree, "git rebase main")))
	git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--quiet", "main")
	rebased := headOf(t, s.tree)
	s.run("", "stop")
	if got := landTrunk(t, main); got != rebased {
		t.Errorf("trunk is at %s after the stop, want the rebased tip %s", got, rebased)
	}
}

// AC-9 through the launcher: from every place the session can have been
// opened, a sample of each class that moves trunk or a story branch is
// refused. internal/hook/landing_test.go holds the full tables.
func TestAC9TheHookRefusesTheSessionEveryWayOfMovingTrunkOrAStoryBranch(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := landRepository(t, binary)
	s := landFinished(t, binary, main)
	landBookkeeping(t, s, false)

	for _, o := range openingsOf(main.dir, s.tree) {
		for _, command := range []string{
			"git update-ref refs/heads/main HEAD",
			"git branch -f main story/US-001",
			"git branch -D story/US-001",
			"git push . HEAD:main",
			"git checkout --ignore-other-worktrees main",
			"git -C '" + main.dir + "' reset --hard story/US-001",
		} {
			if r := askAt(t, binary, sessionFirst, o, "", "Bash", running(o.in(s.tree, command))); !denied(r) {
				t.Errorf("%s: %s was allowed", o.name, command)
			}
		}
	}
}
