package hook

// US-029.1: the loop's evidence -- the backlog and the story records -- is kept
// in the repository's main checkout, and a session is held in whichever
// checkout it started its story. Before this story the hook judged a session
// only inside the checkout that held it, so from a worktree the main
// checkout's records were "another project" and every rule stopped at its
// edge. These are the threats THREATS.md lists for that move: T1, T2 (the
// routes of AC-7), T3 and T12.
//
// The repositories are real, because the main checkout is found through git's
// own files, and the worktree is made with `git worktree add`. State is set
// down as `sdlc start` in the worktree leaves it: the worktree's own
// .sdlc/state names the story and the session, and the story's record and
// status are in the main checkout. No claim is written here; the hook finds
// the session through the worktree's own .sdlc/state/session.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

const (
	wtHeld   = "session-in-the-worktree"
	wtNobody = "session-holding-nothing"
)

func wtGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func wtTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

// wtCheckouts is a repository with the loop in it, A-1 and B-2 in its backlog,
// and a linked worktree in which session wtHeld holds A-1. The main checkout
// holds nothing.
func wtCheckouts(t *testing.T) (main, wt string) {
	t.Helper()
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	main = wtTempDir(t)
	wtGit(t, main, "init", "--quiet", "--initial-branch=main")
	write(t, main, ".sdlc/config.json", `{"version":1}`)
	write(t, main, "user_stories.json", `{"stories":[`+
		`{"id":"A-1","title":"Invoices","status":"ready"},`+
		`{"id":"B-2","title":"Refunds","status":"ready"}]}`+"\n")
	write(t, main, "invoice.go", "package invoice\n")
	wtGit(t, main, "add", "-A")
	wtGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "the project")

	wt = filepath.Join(wtTempDir(t), "wt")
	wtGit(t, main, "worktree", "add", "--quiet", "-b", "wt", wt)
	write(t, wt, ".sdlc/state/active", "A-1\n")
	write(t, wt, ".sdlc/state/session", wtHeld+"\n")
	write(t, main, "user_stories.json", `{"stories":[`+
		`{"id":"A-1","title":"Invoices","status":"in_progress"},`+
		`{"id":"B-2","title":"Refunds","status":"ready"}]}`+"\n")
	wtRecord(t, main, "A-1")
	return main, wt
}

// wtRecord writes id's record in root with the gates given passed.
func wtRecord(t *testing.T, root, id string, gates ...model.Gate) {
	t.Helper()
	at := time.Now()
	record := model.NewRecord(id, at)
	for _, g := range gates {
		record.SetGate(g, model.GatePass, "", at)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/"+id+"/gate-record.json", string(raw))
}

// wtAsk is a tool call from session, in a session whose project is dir.
func wtAsk(t *testing.T, session, dir, tool string, input map[string]any, getenv func(string) string) reply {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": session,
		"tool_name": tool, "cwd": dir, "tool_input": input,
	})
	if err != nil {
		t.Fatal(err)
	}
	return call(t, string(raw), getenv)
}

// wtWrites are the ways into the main checkout's backlog and A-1's record that
// a single checkout refuses today: file tools and the shell, by absolute path,
// through `git -C`, and after changing directory.
func wtWrites(main string) []struct {
	name  string
	tool  string
	input map[string]any
} {
	backlog := filepath.Join(main, "user_stories.json")
	record := filepath.Join(main, ".sdlc", "stories", "A-1", "gate-record.json")
	return []struct {
		name  string
		tool  string
		input map[string]any
	}{
		{"Write the backlog", "Write", map[string]any{"file_path": backlog, "content": "{}"}},
		{"Write the record", "Write", map[string]any{"file_path": record, "content": "{}"}},
		{"redirect into the backlog", "Bash", map[string]any{"command": "echo '{}' > " + backlog}},
		{"delete the record", "Bash", map[string]any{"command": "rm -f " + record}},
		{"restore the backlog with git -C", "Bash", map[string]any{"command": "git -C " + main + " checkout -- user_stories.json"}},
		{"change directory and redirect", "Bash", map[string]any{"command": "cd " + main + " && echo '{}' > user_stories.json"}},
	}
}

// ---------------------------------------------------------------- T2

// T2, the routes of AC-7: a session held in a worktree writes the main
// checkout's evidence. From the worktree, and from a call made wholly inside
// the main checkout, where it holds nothing.
func TestAC7AndT2ASessionHeldInAWorktreeIsRefusedTheMainCheckoutsRecordsByEveryRoute(t *testing.T) {
	main, wt := wtCheckouts(t)
	for _, w := range wtWrites(main) {
		t.Run("T2 "+w.name, func(t *testing.T) {
			if got := wtAsk(t, wtHeld, wt, w.tool, w.input, noEnv); !denied(got) {
				t.Errorf("the session held in the worktree may %s in the main checkout", w.name)
			}
			if got := wtAsk(t, wtHeld, main, w.tool, w.input, noEnv); !denied(got) {
				t.Errorf("the session held in the worktree may %s from inside the main checkout", w.name)
			}
		})
	}
}

// ---------------------------------------------------------------- T1

// T1: an escalation ends the iteration, so only the record says the story
// waits, and the record is in the main checkout. From the worktree the story
// escalated in, a session held to nothing is refused the approval, as it is
// in a single checkout today.
func TestT1ApproveIsRefusedInAWorktreeWhileItsStoryWaitsInTheMainCheckout(t *testing.T) {
	main, wt := wtCheckouts(t)
	at := time.Now()
	record := model.NewRecord("A-1", at)
	record.Escalate("spec_unclear", "which greeting?", "", at)
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, main, ".sdlc/stories/A-1/gate-record.json", string(raw))
	write(t, main, "user_stories.json", `{"stories":[`+
		`{"id":"A-1","title":"Invoices","status":"awaiting_human"},`+
		`{"id":"B-2","title":"Refunds","status":"ready"}]}`+"\n")
	for _, name := range []string{"active", "session"} {
		if err := os.Remove(filepath.Join(wt, ".sdlc", "state", name)); err != nil {
			t.Fatal(err)
		}
	}

	for _, command := range []string{"sdlc approve A-1", "sdlc approve"} {
		if got := wtAsk(t, wtNobody, wt, "Bash", map[string]any{"command": command}, noEnv); !denied(got) {
			t.Errorf("a session in the worktree ran %q while A-1 waits for a person", command)
		}
	}
}

// ---------------------------------------------------------------- T3

// T3: the main checkout is the one the worktree's own .git leads to. Git's
// variables, in the environment Claude Code runs the hook in, name a directory
// anybody can make, and must not move what the hook protects to it.
func TestT3GitsVariablesDoNotMoveTheMainCheckoutTheHookProtects(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE"} {
		t.Run("T3 "+name, func(t *testing.T) {
			main, wt := wtCheckouts(t)
			fake := wtTempDir(t)
			wtGit(t, fake, "init", "--quiet", "--initial-branch=main")
			write(t, fake, ".sdlc/config.json", `{"version":1}`)
			write(t, fake, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"ready"}]}`+"\n")
			value := filepath.Join(fake, ".git")
			if name == "GIT_WORK_TREE" {
				value = fake
			}
			t.Setenv(name, value)
			getenv := env(map[string]string{name: value})

			for _, w := range wtWrites(main) {
				if got := wtAsk(t, wtHeld, wt, w.tool, w.input, getenv); !denied(got) {
					t.Errorf("with %s at a fake, the session held in the worktree may %s in the main checkout", name, w.name)
				}
				if got := wtAsk(t, wtHeld, main, w.tool, w.input, getenv); !denied(got) {
					t.Errorf("with %s at a fake, the session held in the worktree may %s from inside the main checkout", name, w.name)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- T12

// T12: a session held in a worktree makes the main checkout unfindable, and
// the hook fails open over what it cannot read. Failing open must never mean a
// gate counts as passed: the story's record is the main checkout's, and the
// copy git checked out in the worktree, which says every gate has passed, is
// not it (D5).
func TestT12AMainCheckoutMadeUnfindableLeavesTheCommitRefusedAndTheFreezeHeld(t *testing.T) {
	for _, damage := range []struct {
		name string
		do   func(t *testing.T, main, wt string)
	}{
		{"T12 with the main checkout intact the worktree's copy does not count", func(*testing.T, string, string) {}},
		{"T12 core.bare set in the common configuration", func(t *testing.T, main, _ string) {
			t.Helper()
			wtGit(t, main, "config", "core.bare", "true")
		}},
		{"T12 the worktree's commondir rewritten", func(t *testing.T, main, _ string) {
			t.Helper()
			write(t, main, ".git/worktrees/wt/commondir", filepath.Join(wtTempDir(t), "nowhere")+"\n")
		}},
	} {
		t.Run(damage.name, func(t *testing.T) {
			main, wt := wtCheckouts(t)
			// The freeze first: the reviews are stamped against the tree as it
			// stands, and a test written after them would make them stale, which
			// refuses the commit for another reason.
			freeze(t, wt, "A-1", "invoice_test.go", "package invoice\n\n// AC-1\n")
			recordPastEveryGate(t, wt)
			damage.do(t, main, wt)

			if got := wtAsk(t, wtHeld, wt, "Bash", map[string]any{"command": `git commit -m "done"`}, noEnv); !denied(got) {
				t.Error("the commit went through on the worktree's copy of the record")
			}
			// Held by design today, and it has to stay held.
			if got := wtAsk(t, wtHeld, wt, "Write", map[string]any{"file_path": "invoice_test.go", "content": "x"}, noEnv); !denied(got) {
				t.Error("a frozen test was written")
			}
		})
	}
}

// ---------------------------------------------------------------- AC-5

// The Stop guard counts in the checkout the session is held in, and reads
// where the story has got to from the story's one record.
func TestAC5TheStopGuardInAWorktreeCountsThereAndReadsTheStoryFromTheMainCheckout(t *testing.T) {
	main, wt := wtCheckouts(t)
	wtRecord(t, main, "A-1", model.GateDoR, model.GateAnalysis)

	raw, err := json.Marshal(map[string]any{
		"hook_event_name": "Stop", "cwd": wt, "stop_hook_active": false, "session_id": wtHeld,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Run([]string{"Stop"}, bytes.NewReader(raw), &out, io.Discard, noEnv); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var got stopOutcome
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
	}
	if got.Decision != "block" || !strings.Contains(got.Reason, string(model.GateTestsFrozen)) {
		t.Errorf("the stop guard said %q (%s), want the session sent back to work %s", got.Reason, got.Decision, model.GateTestsFrozen)
	}
	if _, err := os.Stat(filepath.Join(wt, ".sdlc", "state", "stop-blocks.json")); err != nil {
		t.Errorf("the stop was not counted in the worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(main, ".sdlc", "state", "stop-blocks.json")); err == nil {
		t.Error("the stop was counted in the main checkout, which holds nothing")
	}
}
