package cli

// US-029.1 in-process, the refusals. The codes are matched as the strings the
// story pins in internal/sdlcerr: SDLC-E0050 for a story another checkout
// holds, SDLC-E0051 for a holding checkout that is gone, SDLC-E0052 for a
// repository with no main checkout to keep the backlog in.

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

const (
	wtHeldElsewhere  = "SDLC-E0050"
	wtHolderMissing  = "SDLC-E0051"
	wtNoMainCheckout = "SDLC-E0052"
)

// wtRefused runs a command that has to be refused with code, and returns what
// it printed.
func wtRefused(t *testing.T, code, dir, session, stdin string, args ...string) string {
	t.Helper()
	r := wtRun(t, dir, session, stdin, args...)
	out := r.stdout + r.stderr
	if r.code == 0 {
		t.Errorf("sdlc %s in %s went through, want %s:\n%s", strings.Join(args, " "), dir, code, out)
	} else if !strings.Contains(out, code) {
		t.Errorf("sdlc %s in %s was refused with something other than %s:\n%s", strings.Join(args, " "), dir, code, out)
	}
	return out
}

// ---------------------------------------------------------------- AC-3

func TestAC3ACommandForAStoryAnotherCheckoutHoldsIsRefusedWithSDLCE0050(t *testing.T) {
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, main, wtSessionMain)

	refused := func(t *testing.T, session, stdin string, args ...string) {
		t.Helper()
		beforeMain, beforeWorktree := wtLoopState(t, main), wtLoopState(t, wt)
		out := wtRefused(t, wtHeldElsewhere, wt, session, stdin, args...)
		if !wtNames(out, main) {
			t.Errorf("the refusal does not name the checkout holding the story, %s:\n%s", main, out)
		}
		if !maps.Equal(beforeMain, wtLoopState(t, main)) {
			t.Error("the refusal changed the main checkout's backlog or records")
		}
		if !maps.Equal(beforeWorktree, wtLoopState(t, wt)) {
			t.Error("the refusal changed the worktree's loop state")
		}
	}

	t.Run("AC-3 start names the checkout that holds the story", func(t *testing.T) {
		refused(t, wtSessionWorktree, "", "start", "US-001")
	})
	t.Run("AC-3 a gate for the story", func(t *testing.T) {
		refused(t, wtSessionWorktree, "", "gate", "dor", "pass", "--story", "US-001", "--note", "from elsewhere")
	})
	t.Run("AC-3 a document for the story", func(t *testing.T) {
		refused(t, wtSessionWorktree, "# Analysis\n", "artifact", "write", "analysis", "--story", "US-001")
	})
	t.Run("AC-3 a review of the story", func(t *testing.T) {
		refused(t, wtSessionWorktree, "# architect\n", "review", "add", "design_review", "architect", "approve", "--story", "US-001")
	})
	t.Run("AC-3 a cost for the story", func(t *testing.T) {
		refused(t, wtSessionWorktree, "", "cost", "add", "--usd", "1.00", "--story", "US-001")
	})
	t.Run("AC-3 the session holding a story in the main checkout starting in the worktree", func(t *testing.T) {
		refused(t, wtSessionMain, "", "start")
	})
	t.Run("AC-3 an active in the worktree naming the held story", func(t *testing.T) {
		writeFile(t, wt, ".sdlc/state/active", "US-001\n")
		defer func() { _ = os.Remove(filepath.Join(wt, ".sdlc", "state", "active")) }()
		refused(t, wtSessionWorktree, "", "start")
	})
	t.Run("AC-3 approve for a story waiting in the main checkout", func(t *testing.T) {
		wtMust(t, main, wtSessionMain, "", "escalate", "spec_unclear", "--message", "which greeting?")
		refused(t, "", "", "approve", "US-001")
	})
}

// ---------------------------------------------------------------- AC-8

// wtNamedCommands is every sdlc command a text quotes, in "..." or `...`, as
// the arguments a person would type: a placeholder for a value becomes a
// reason, and <ID> or ID the story.
func wtNamedCommands(text, id string) [][]string {
	var out [][]string
	for _, delim := range []byte{'"', '`'} {
		rest := text
		for {
			i := strings.Index(rest, string(delim)+"sdlc ")
			if i < 0 {
				break
			}
			body := rest[i+len("sdlc ")+1:]
			end := len(body)
			for j := 0; j < len(body); j++ {
				if body[j] == '\n' || (body[j] == delim && (delim != '"' || j == 0 || body[j-1] != '\\')) {
					end = j
					break
				}
			}
			out = append(out, wtCommandArgs(body[:end], id))
			if end >= len(body) {
				break
			}
			rest = body[end+1:]
		}
	}
	return out
}

func wtCommandArgs(line, id string) []string {
	line = strings.ReplaceAll(line, `\"`, `"`)
	var args []string
	for {
		line = strings.TrimLeft(line, " ")
		if line == "" {
			return args
		}
		var word string
		if q := line[0]; q == '"' || q == '\'' {
			end := strings.IndexByte(line[1:], q)
			if end < 0 {
				word, line = line[1:], ""
			} else {
				word, line = line[1:1+end], line[2+end:]
			}
		} else {
			end := strings.IndexByte(line, ' ')
			if end < 0 {
				word, line = line, ""
			} else {
				word, line = line[:end], line[end:]
			}
		}
		switch {
		case word == "..." || word == "…" || word == "":
			word = "the checkout that held it is gone"
		case word == "ID" || (strings.HasPrefix(word, "<") && strings.HasSuffix(word, ">")):
			word = id
		}
		args = append(args, word)
	}
}

func TestAC8AStoryWhoseCheckoutIsGoneIsRefusedWithSDLCE0051UntilAPersonReleasesIt(t *testing.T) {
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	gone := wtWorktree(t, main, "gone")
	wtStarted(t, gone, wtSessionWorktree, "US-001")
	t.Chdir(main)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	before := wtLoopState(t, main)
	out := wtRefused(t, wtHolderMissing, main, wtSessionMain, "", "start", "US-001")
	if !wtNames(out, gone) {
		t.Errorf("the refusal does not name the missing checkout %s:\n%s", gone, out)
	}
	if !maps.Equal(before, wtLoopState(t, main)) {
		t.Error("the refused start changed the main checkout's loop state")
	}

	var release [][]string
	for _, args := range wtNamedCommands(out, "US-001") {
		if len(args) == 0 || slices.Contains([]string{"start", "status", "doctor", "log", "help", "version"}, args[0]) ||
			(args[0] == "story" && len(args) > 1 && args[1] == "list") {
			continue
		}
		release = append(release, args)
	}
	if len(release) == 0 {
		t.Fatalf("the refusal names no command that releases the claim:\n%s", out)
	}
	released := false
	for _, args := range release {
		if r := wtRun(t, main, "", "", args...); r.code == 0 {
			released = true
		}
	}
	if !released {
		t.Fatalf("no command the refusal names worked for a person: %v", release)
	}
	if got := wtStarted(t, main, wtSessionMain, "US-001"); got.Story != "US-001" {
		t.Errorf("after the release start in the main checkout gave %q, want US-001", got.Story)
	}
}

// ---------------------------------------------------------------- AC-9

// wtBareWorktree is a linked worktree of a bare repository, cloned from a
// project with the loop set up in it when withLoop is set.
func wtBareWorktree(t *testing.T, withLoop bool) string {
	t.Helper()
	wtNoGitEnv(t)
	src := wtTempDir(t)
	wtGit(t, src, "init", "--quiet", "--initial-branch=main")
	writeFile(t, src, "README.md", "a project\n")
	if withLoop {
		wtMust(t, src, "", "", "init")
	}
	wtCommit(t, src, "the project")
	bare := filepath.Join(wtTempDir(t), "repo.git")
	wtGit(t, src, "clone", "--quiet", "--bare", src, bare)
	wt := filepath.Join(wtTempDir(t), "wt")
	wtGit(t, bare, "worktree", "add", "--quiet", "-b", "wt", wt, "main")
	wtTrunkIs(t, wt, "wt")
	return wt
}

func TestAC9AWorktreeOfARepositoryWithNoMainCheckoutIsRefusedWithSDLCE0052(t *testing.T) {
	wt := wtBareWorktree(t, true)

	for _, args := range [][]string{
		{"status"},
		{"story", "list"},
		{"start"},
		{"start", "US-001"},
		{"gate", "dor", "pass", "--story", "US-001", "--note", "anywhere"},
		{"approve", "US-001"},
	} {
		t.Run("AC-9 "+strings.Join(args, " "), func(t *testing.T) {
			if out := wtRefused(t, wtNoMainCheckout, wt, wtSessionWorktree, "", args...); !strings.Contains(strings.ToLower(out), "bare") {
				t.Errorf("the refusal does not say the repository is bare:\n%s", out)
			}
			if wtExists(filepath.Join(wt, ".sdlc", "stories")) {
				t.Error("a record was written in the worktree")
			}
		})
	}

	t.Run("AC-9 init", func(t *testing.T) {
		fresh := wtBareWorktree(t, false)
		wtRefused(t, wtNoMainCheckout, fresh, "", "", "init")
		if wtExists(filepath.Join(fresh, "user_stories.json")) || wtExists(filepath.Join(fresh, ".sdlc", "config.json")) {
			t.Error("init wrote the loop's files into a worktree of a bare repository")
		}
	})

	t.Run("AC-9 stop ends the iteration and says the backlog is out of reach", func(t *testing.T) {
		writeFile(t, wt, ".sdlc/state/active", "US-001\n")
		writeFile(t, wt, ".sdlc/state/session", wtSessionWorktree+"\n")
		r := wtRun(t, wt, "", "", "stop")
		if r.code != 0 {
			t.Fatalf("stop exited %d:\n%s", r.code, r.stderr)
		}
		if wtExists(filepath.Join(wt, ".sdlc", "state", "active")) {
			t.Error("stop left .sdlc/state/active behind")
		}
		if wtExists(filepath.Join(wt, ".sdlc", "stories")) {
			t.Error("stop wrote a record in the worktree")
		}
		if !strings.Contains(r.stdout+r.stderr, "backlog") {
			t.Errorf("stop does not say the backlog could not be reached:\n%s%s", r.stdout, r.stderr)
		}
	})

	t.Run("AC-9 doctor reports it as a failing check", func(t *testing.T) {
		r := wtRun(t, wt, "", "", "doctor", "--json")
		reported := false
		for _, check := range decode[doctorPayload](t, r).Checks {
			if check.State == stateProblem && strings.Contains(strings.ToLower(check.Detail+" "+check.Fix), "bare") {
				reported = true
			}
		}
		if r.code == 0 || !reported {
			t.Errorf("doctor exited %d and named no bare repository:\n%s", r.code, r.stdout)
		}
	})

	t.Run("AC-9 a worktree of a repository with a separate git directory", func(t *testing.T) {
		main, gitDir := wtTempDir(t), filepath.Join(wtTempDir(t), "gitdir")
		wtGit(t, main, "init", "--quiet", "--initial-branch=main", "--separate-git-dir="+gitDir)
		wtMust(t, main, "", "", "init")
		wtCommit(t, main, "the project")
		linked := wtWorktree(t, main, "wt")
		for _, args := range [][]string{{"status"}, {"start"}} {
			if out := wtRefused(t, wtNoMainCheckout, linked, wtSessionWorktree, "", args...); !strings.Contains(strings.ToLower(out), "separate") {
				t.Errorf("the refusal does not cover a separate git directory:\n%s", out)
			}
		}
		if r := wtRun(t, main, "", "", "status"); r.code != 0 {
			t.Errorf("the main checkout of a separate git directory stopped working:\n%s", r.stderr)
		}
	})
}

// ---------------------------------------------------------------- AC-10

// D4: an iteration that was running when this release was installed has no
// claim. It carries on as before, and holds its story from then on.
func TestAC10AnIterationRunningAtUpgradeCarriesOnAndHoldsItsStory(t *testing.T) {
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wtSetStatus(t, main, "US-001", model.StatusInProgress)
	writeFile(t, main, ".sdlc/state/active", "US-001\n")
	writeFile(t, main, ".sdlc/state/session", wtSessionMain+"\n")

	if got := wtStarted(t, main, wtSessionMain); got.Story != "US-001" || !got.Resume {
		t.Fatalf("start in the upgraded checkout gave %+v, want US-001 resumed", got)
	}
	wt := wtWorktree(t, main, "wt")
	if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-002" {
		t.Errorf("start in a new worktree gave %q, want US-002: the upgraded checkout holds US-001", got.Story)
	}
}

// D10: an active naming a story nobody claims, whose status a person set by
// hand, is resumed as today in a single checkout and ignored where the
// repository has a linked worktree.
func TestAC10AnActiveAPersonSetAsideIsResumedOnlyInASingleCheckout(t *testing.T) {
	for _, layout := range []struct {
		name   string
		linked bool
		want   string
	}{
		{"AC-10 in a single checkout it is resumed as today", false, "US-001"},
		{"AC-10 with a linked worktree start picks afresh", true, "US-002"},
	} {
		t.Run(layout.name, func(t *testing.T) {
			main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			wtSetStatus(t, main, "US-001", model.StatusBlocked)
			writeFile(t, main, ".sdlc/state/active", "US-001\n")
			writeFile(t, main, ".sdlc/state/session", wtSessionMain+"\n")
			if layout.linked {
				wtWorktree(t, main, "wt")
			}
			if got := wtStarted(t, main, wtSessionMain); got.Story != layout.want {
				t.Errorf("start gave %q, want %s", got.Story, layout.want)
			}
		})
	}
}

// D10: a finished story keeps today's handling in every layout. Between the
// retro and `sdlc stop`, start refuses with "run sdlc stop"; stop ends the
// iteration and lifts the finished story's freeze on its one record.
func TestAC10AFinishedStoryInAWorktreeIsPutDownWithStop(t *testing.T) {
	main := wtRepository(t, wtStory{id: "US-001"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, wt, wtSessionWorktree)
	reach(t, wt, "")

	if r := wtRun(t, wt, wtSessionWorktree, "", "start"); r.code == 0 ||
		!strings.Contains(r.stderr, "SDLC-E0033") || !strings.Contains(r.stderr, "sdlc stop") {
		t.Errorf("start between the retro and stop exited %d, want SDLC-E0033 sending you to sdlc stop:\n%s", r.code, r.stderr)
	}
	if out := wtMust(t, wt, "", "", "stop").stdout; !strings.Contains(out, "Every gate passed") {
		t.Errorf("stop did not end a finished iteration:\n%s", out)
	}
	if wtExists(filepath.Join(wt, ".sdlc", "state", "tests.lock")) {
		t.Error("stop left the finished story's freeze in the worktree")
	}
	var record model.Record
	wtReadJSON(t, wtRecordPath(main, "US-001"), &record)
	if n := len(record.Events); n == 0 || record.Events[n-1].Type != "unfreeze" {
		t.Errorf("the story's record in the main checkout does not end with the freeze lifted: %+v", record.Events)
	}
	if got := wtStatuses(t, main)["US-001"]; got != "done" {
		t.Errorf("the main checkout's backlog has US-001 %q, want done", got)
	}
}

// ---------------------------------------------------------------- T3

// wtFakeMain is a directory made up to look like a main checkout, with the
// loop's configuration and a backlog of its own in which nothing is held.
func wtFakeMain(t *testing.T, like string) string {
	t.Helper()
	fake := wtTempDir(t)
	wtGit(t, fake, "init", "--quiet", "--initial-branch=main")
	writeFile(t, fake, ".sdlc/config.json", wtRead(t, filepath.Join(like, ".sdlc", "config.json")))
	writeFile(t, fake, "user_stories.json", wtRead(t, filepath.Join(like, "user_stories.json")))
	wtSetStatus(t, fake, "US-001", model.StatusReady)
	return fake
}

// The main checkout is the one the worktree's own .git leads to. Git's
// variables name a directory anybody can make, and a claim read from there
// would be whatever its maker wrote.
func TestT3GitsVariablesCannotPointAWorktreeAtAFakeMainCheckout(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_COMMON_DIR"} {
		t.Run("T3 "+name, func(t *testing.T) {
			main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			wt := wtWorktree(t, main, "wt")
			wtStarted(t, main, wtSessionMain)
			fake := wtFakeMain(t, main)
			writeFile(t, wt, ".sdlc/state/active", "US-001\n")
			t.Setenv(name, filepath.Join(fake, ".git"))

			before := wtLoopState(t, main)
			wtRefused(t, wtHeldElsewhere, wt, wtSessionWorktree, "", "start")
			wtRefused(t, wtHeldElsewhere, wt, wtSessionWorktree, "", "gate", "dor", "pass", "--story", "US-001", "--note", "x")
			if !maps.Equal(before, wtLoopState(t, main)) {
				t.Error("the main checkout's loop state changed")
			}
			for _, dir := range []string{fake, wt} {
				if wtExists(filepath.Join(dir, ".sdlc", "stories")) {
					t.Errorf("a record was written in %s", dir)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- T12

// A worktree whose main checkout a session has made unfindable records no
// gate: there is nowhere to record it, and the worktree's own files are not the
// story's record.
func TestT12AWorktreeWhoseMainCheckoutWasMadeUnfindableRecordsNoGate(t *testing.T) {
	for _, damage := range []struct {
		name string
		// code is the refusal the story pins, or "" where any refusal will do.
		code string
		do   func(t *testing.T, main string)
	}{
		// The repository now reads as bare, and D6 gives a bare repository
		// AC-9's code.
		{"T12 core.bare set in the common configuration", wtNoMainCheckout, func(t *testing.T, main string) {
			wtGit(t, main, "config", "core.bare", "true")
		}},
		// Git itself can no longer read this worktree, so which code refuses
		// it is not this test's to pin; that nothing is recorded is. The
		// released tool already refuses it, so this one passes today: it is
		// the guard against a main checkout that, not found, falls back to the
		// worktree's own files.
		{"T12 the worktree's commondir rewritten", "", func(t *testing.T, main string) {
			writeFile(t, main, ".git/worktrees/wt/commondir", filepath.Join(wtTempDir(t), "nowhere")+"\n")
		}},
	} {
		t.Run(damage.name, func(t *testing.T) {
			main := wtRepository(t, wtStory{id: "US-001"})
			wt := wtWorktree(t, main, "wt")
			wtStarted(t, wt, wtSessionWorktree)
			damage.do(t, main)

			args := []string{"gate", "dor", "pass", "--note", "x"}
			if damage.code != "" {
				wtRefused(t, damage.code, wt, wtSessionWorktree, "", args...)
			} else if r := wtRun(t, wt, wtSessionWorktree, "", args...); r.code == 0 {
				t.Errorf("sdlc %s went through in a worktree whose main checkout is gone:\n%s", strings.Join(args, " "), r.stdout)
			}
			for _, root := range []string{main, wt} {
				path := wtRecordPath(root, "US-001")
				if !wtExists(path) {
					continue
				}
				var record model.Record
				wtReadJSON(t, path, &record)
				if got, ok := record.Gates[model.GateDoR]; ok && got.Status == model.GatePass {
					t.Errorf("the gate was recorded as passed in %s", root)
				}
			}
		})
	}
}
