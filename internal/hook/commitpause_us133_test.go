package hook

// US-133 through the hook: `git commit` is asked before it runs whether the
// story's change waits for a person, as the commit gate asks after it (AC-2:
// "as pre_commit_pause_tiers makes it pause today", and the tier's pause is
// asked here today). A low story, in a tier no setting pauses, whose change
// touches a path trunk's configuration lists, is refused, and the refusal
// names the file and the glob and says how to hand the story to a person.
//
// The repository is real and the story is worked in its own worktree, on
// story/A-1, cut from trunk, which lists internal/hook/** and
// plugin/hooks/*.json and holds two files under internal/hook/. What the
// story changes is everything from where its branch left trunk: its commits,
// what is staged, what is changed and not staged, and what git does not track
// yet, with both names of a rename and a deleted file (THREATS T3), and a name
// that differs from a glob only in case (T4). The story's own copy of the
// configuration cannot take a pause away (AC-3, T1), and neither can a flag
// of git's that skips git's own hooks (T7). Each refusal row has a control
// beside it that is let through, so a refusal is the pause and not the
// reviews or the freeze.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	cphHookGlob   = "internal/hook/**"
	cphPluginGlob = "plugin/hooks/*.json"
	cphBody       = "package hook\n\n// Written by the story.\n"
	cphConfig     = `{"version":1,"human_gates":{"pre_commit_pause_tiers":["high"],` +
		`"pre_commit_pause_paths":["` + cphHookGlob + `","` + cphPluginGlob + `"]}}`
	cphNarrowed = `{"version":1,"human_gates":{"pre_commit_pause_tiers":[],"pre_commit_pause_paths":[]}}`
)

func cphBacklog(status string) string {
	return `{"stories":[{"id":"A-1","title":"Invoices","status":"` + status + `","risk_tier":"low"}]}` + "\n"
}

// cphCheckouts is a main checkout whose trunk lists the globs, with A-1, a low
// story, started in its worktree on story/A-1 and its test frozen there.
func cphCheckouts(t *testing.T) (main, wt string) {
	t.Helper()
	main = wtTempDir(t)
	wtGit(t, main, "init", "--quiet", "--initial-branch=main")
	write(t, main, ".gitignore", "/.sdlc/state/\n")
	write(t, main, ".sdlc/config.json", cphConfig)
	write(t, main, "user_stories.json", cphBacklog("ready"))
	write(t, main, "invoice.go", "package invoice\n")
	write(t, main, "internal/hook/guard.go", "package hook\n\n// Guard is on trunk.\n")
	write(t, main, "internal/hook/old.go", "package hook\n\n// Old is on trunk.\n")
	wtGit(t, main, "add", "-A")
	wtGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "the project")

	wt = filepath.Join(main, ".sdlc", "worktrees", "A-1")
	wtGit(t, main, "worktree", "add", "--quiet", "-b", "story/A-1", wt, "main")
	write(t, wt, ".sdlc/state/active", "A-1\n")
	write(t, wt, ".sdlc/state/session", swHeld+"\n")
	freeze(t, wt, "A-1", "invoice_test.go", "package invoice\n\n// AC-1\n")
	write(t, main, "user_stories.json", cphBacklog("in_progress"))
	swClaim(t, wt, "A-1")
	return main, wt
}

// cphReady records every gate before the commit as passed for A-1, with the
// reviews, and where approved is set a person's pre-commit approval, stamped
// against the worktree as it stands.
func cphReady(t *testing.T, main, wt string, approved bool) {
	t.Helper()
	cfg, err := config.Load(wt)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: wt, Config: cfg}).ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range model.GateCommit.Before() {
		record.SetGate(g, model.GatePass, "", at)
	}
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			record.AddReview(model.Review{Gate: gate, Role: r.Role, Verdict: model.VerdictApprove, Subject: tree}, at)
		}
	}
	if approved {
		record.Approvals = []model.Approval{{At: model.Timestamp(at), Decision: model.DecisionApproved,
			Type: "pre_commit_approval", TreeHash: tree}}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, main, ".sdlc/stories/A-1/gate-record.json", string(raw))
}

// cphCommit is the session, opened in the story's worktree, asking to run
// command there.
func cphCommit(t *testing.T, wt, command string) reply {
	t.Helper()
	return swAsk(t, swHeld, swOpening{"opened in the story worktree", wt, wt}, "Bash",
		map[string]any{"command": command}, nil)
}

func cphCommitted(t *testing.T, wt, message string) {
	t.Helper()
	wtGit(t, wt, "add", "-A")
	wtGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
}

func TestAC2AndAC3TheHookRefusesACommitWhoseChangeTouchesAListedPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, file, glob string
		change           func(t *testing.T, wt string)
		command          string
	}{
		{"AC_2 a new file under the glob, staged", "internal/hook/new.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			write(t, wt, "internal/hook/new.go", cphBody)
			wtGit(t, wt, "add", "-A")
		}, "git commit -m A-1"},
		{"AC_2 a new file in a directory git does not track yet", "internal/hook/fresh/new.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			write(t, wt, "invoice.go", "package invoice\n\n// A-1.\n")
			wtGit(t, wt, "add", "--", "invoice.go")
			write(t, wt, "internal/hook/fresh/new.go", cphBody)
		}, "git commit -m A-1"},
		{"AC_2 a file trunk holds, changed and not staged", "internal/hook/guard.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			write(t, wt, "internal/hook/guard.go", "package hook\n")
		}, "git commit -am A-1"},
		{"AC_2 a file trunk holds, deleted and not staged", "internal/hook/old.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			if err := os.Remove(filepath.Join(wt, "internal", "hook", "old.go")); err != nil {
				t.Fatal(err)
			}
		}, "git commit -am A-1"},
		{"AC_2 a file trunk holds, renamed away from the glob and staged", "internal/hook/old.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			wtGit(t, wt, "mv", "internal/hook/old.go", "internal/old.go")
		}, "git commit -m A-1"},
		// git status reports this one in the worktree column, " R elsewhere.go"
		// then the old name, and nothing else changes (code review F-3).
		{"AC_3 a file trunk holds, moved away from the glob and intent-to-add", "internal/hook/old.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			if err := os.Rename(filepath.Join(wt, "internal", "hook", "old.go"), filepath.Join(wt, "elsewhere.go")); err != nil {
				t.Fatal(err)
			}
			wtGit(t, wt, "add", "-N", "elsewhere.go")
		}, "git commit -am A-1"},
		{"AC_2 a file under the glob in a commit the story already made", "internal/hook/early.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			write(t, wt, "internal/hook/early.go", cphBody)
			cphCommitted(t, wt, "A-1, the first part")
			write(t, wt, "invoice.go", "package invoice\n\n// A-1.\n")
			wtGit(t, wt, "add", "--", "invoice.go")
		}, "git commit -m A-1"},
		{"AC_2 a name that differs from the glob only in case", "Plugin/Hooks/Hooks.JSON", cphPluginGlob, func(t *testing.T, wt string) {
			t.Helper()
			write(t, wt, "Plugin/Hooks/Hooks.JSON", "{}\n")
			wtGit(t, wt, "add", "-A")
		}, "git commit -m A-1"},
		{"AC_3 the story's own copy of the configuration emptied", "internal/hook/new.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			write(t, wt, "internal/hook/new.go", cphBody)
			write(t, wt, ".sdlc/config.json", cphNarrowed)
			wtGit(t, wt, "add", "-A")
		}, "git commit -m A-1"},
		{"AC_3 the story's own copy of the configuration emptied and committed", "internal/hook/new.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			write(t, wt, ".sdlc/config.json", cphNarrowed)
			cphCommitted(t, wt, "A-1, the configuration")
			write(t, wt, "internal/hook/new.go", cphBody)
			wtGit(t, wt, "add", "-A")
		}, "git commit -m A-1"},
		{"AC_3 git's own hooks skipped", "internal/hook/new.go", cphHookGlob, func(t *testing.T, wt string) {
			t.Helper()
			write(t, wt, "internal/hook/new.go", cphBody)
			wtGit(t, wt, "add", "-A")
		}, "git commit --no-verify -m A-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt := cphCheckouts(t)
			tc.change(t, wt)
			cphReady(t, main, wt, false)

			r := cphCommit(t, wt, tc.command)
			reason := swReason(r)
			if !denied(r) {
				t.Fatalf("%s went through on a low story whose change touches %s", tc.command, tc.file)
			}
			for _, want := range []string{tc.file, tc.glob, "pre_commit_approval"} {
				if !strings.Contains(reason, want) {
					t.Errorf("the refusal does not name %q: %s", want, reason)
				}
			}
		})
	}
}

// The controls: the same story, with a change no glob matches, or with a
// person's approval of the change as it stands, is committed as it always was.
func TestAC2TheHookLetsThroughACommitNoGlobPausesOrAPersonApproved(t *testing.T) {
	t.Parallel()
	t.Run("AC_2 a change no glob matches", func(t *testing.T) {
		t.Parallel()
		main, wt := cphCheckouts(t)
		write(t, wt, "invoice.go", "package invoice\n\n// A-1.\n")
		write(t, wt, "plugin/hooks/README.md", "not a .json\n")
		wtGit(t, wt, "add", "-A")
		cphReady(t, main, wt, false)
		if r := cphCommit(t, wt, "git commit -m A-1"); denied(r) {
			t.Errorf("a change no glob matches was refused: %s", swReason(r))
		}
	})
	t.Run("AC_2 a change a glob matches, approved by a person as it stands", func(t *testing.T) {
		t.Parallel()
		main, wt := cphCheckouts(t)
		write(t, wt, "internal/hook/new.go", cphBody)
		wtGit(t, wt, "add", "-A")
		cphReady(t, main, wt, true)
		if r := cphCommit(t, wt, "git commit -m A-1"); denied(r) {
			t.Errorf("the change a person approved was refused: %s", swReason(r))
		}
	})
}
