package hook

// US-029.2: `sdlc start` makes each story a worktree of its own, at
// .sdlc/worktrees/<ID> inside the main checkout, on the branch story/<ID>, and
// the session working the story is held there (decision 3). AC-6 asks that the
// session be held to every rule a single checkout holds it to -- the freeze,
// the protected paths, the commit gate -- for what is in its worktree, whether
// Claude Code was opened in the worktree or in the main checkout. Decisions 4
// to 6 close what a worktree adds: a session must not end its own hold by
// taking its worktree away (T1), take another story's away (T2), make a
// worktree inside a checkout's protected paths (T5), or commit another
// checkout's work by pointing git at it (T4). T7 is the other direction: a
// session held in the main checkout must not reach into a story worktree with
// the file tools.
//
// The repositories are real and the worktrees are made by git, so each rule is
// judged exactly as the hook judges a live session: through Run, from the
// payload and the files on disk. State is set down as `sdlc start` leaves it
// with worktrees on: the story worktree's own .sdlc/state names the story and
// the session, the claim names the worktree, and the story's status and record
// are in the main checkout.

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	swHeld  = "session-held-in-the-story-worktree"
	swOther = "session-held-in-the-other-story-worktree"
	swMain  = "session-held-in-the-main-checkout"
)

// swLayout is a main checkout with two stories started in worktrees: A-1,
// held by swHeld with invoice_test.go frozen, and B-2, held by swOther with
// refund_test.go frozen. C-3 is in the backlog and held nowhere.
type swLayout struct {
	main, wt, other string
}

func swBacklog(a1, b2, c3 string) string {
	return `{"stories":[` +
		`{"id":"A-1","title":"Invoices","status":"` + a1 + `"},` +
		`{"id":"B-2","title":"Refunds","status":"` + b2 + `"},` +
		`{"id":"C-3","title":"Credit notes","status":"` + c3 + `"}]}` + "\n"
}

// GIT_DIR, GIT_WORK_TREE and GIT_COMMON_DIR are unset for the whole binary by
// TestMain, so the layout is built as a person's shell without them builds it;
// a test that sets one does so itself, and is then not parallel.
func swCheckouts(t *testing.T) swLayout {
	t.Helper()
	main := wtTempDir(t)
	wtGit(t, main, "init", "--quiet", "--initial-branch=main")
	write(t, main, ".sdlc/config.json", `{"version":1}`)
	write(t, main, "user_stories.json", swBacklog("ready", "ready", "ready"))
	write(t, main, "invoice.go", "package invoice\n")
	write(t, main, "refund.go", "package invoice\n")
	wtGit(t, main, "add", "-A")
	wtGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "the project")

	l := swLayout{
		main:  main,
		wt:    filepath.Join(main, ".sdlc", "worktrees", "A-1"),
		other: filepath.Join(main, ".sdlc", "worktrees", "B-2"),
	}
	wtGit(t, main, "worktree", "add", "--quiet", "-b", "story/A-1", l.wt, "main")
	wtGit(t, main, "worktree", "add", "--quiet", "-b", "story/B-2", l.other, "main")
	write(t, l.wt, ".sdlc/state/active", "A-1\n")
	write(t, l.wt, ".sdlc/state/session", swHeld+"\n")
	write(t, l.other, ".sdlc/state/active", "B-2\n")
	write(t, l.other, ".sdlc/state/session", swOther+"\n")
	freeze(t, l.wt, "A-1", "invoice_test.go", "package invoice\n\n// AC-1\n")
	freeze(t, l.other, "B-2", "refund_test.go", "package invoice\n\n// AC-1\n")
	write(t, main, "user_stories.json", swBacklog("in_progress", "in_progress", "ready"))
	wtRecord(t, main, "A-1")
	wtRecord(t, main, "B-2")
	swClaim(t, l.wt, "A-1")
	swClaim(t, l.other, "B-2")
	return l
}

// swClaim writes the claim `sdlc start` writes, naming checkout.
func swClaim(t *testing.T, checkout, id string) {
	t.Helper()
	cfg, err := config.Load(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(&config.Project{Root: checkout, Config: cfg}).Claim(id); err != nil {
		t.Fatal(err)
	}
}

// swReady records every gate before the commit as passed for A-1, in the main
// checkout where its record is kept, with the reviews stamped against A-1's
// worktree as it stands.
func swReady(t *testing.T, l swLayout) {
	t.Helper()
	cfg, err := config.Load(l.wt)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: l.wt, Config: cfg}).ReviewSubject(t.Context())
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
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, l.main, ".sdlc/stories/A-1/gate-record.json", string(raw))
}

// swOpening is where Claude Code was opened, and where the call runs from.
type swOpening struct {
	name, project, cwd string
}

// swOpenings are AC-6's two ways of opening the session working A-1, and the
// second with the shell moved into the worktree.
func swOpenings(l swLayout) []swOpening {
	return []swOpening{
		{"opened in the story worktree", l.wt, l.wt},
		{"opened in the main checkout", l.main, l.main},
		{"opened in the main checkout with the shell in the story worktree", l.main, l.wt},
	}
}

// swAsk is a tool call from session, opened as o, with anything else the
// session's environment carries.
func swAsk(t *testing.T, session string, o swOpening, tool string, input map[string]any, carried map[string]string) reply {
	t.Helper()
	vars := map[string]string{"CLAUDE_PROJECT_DIR": o.project}
	maps.Copy(vars, carried)
	return wtAsk(t, session, o.cwd, tool, input, env(vars))
}

// swRow is one call, where it is made from, and the agent making it: empty
// for the main conversation.
type swRow struct {
	name        string
	o           swOpening
	tool, agent string
	input       map[string]any
}

func swBash(name string, o swOpening, command string) swRow {
	return swRow{name, o, "Bash", "", map[string]any{"command": command}}
}

func swPowerShell(name string, o swOpening, command string) swRow {
	return swRow{name, o, "PowerShell", "", map[string]any{"command": command}}
}

func swFile(name string, o swOpening, tool, agent, path string) swRow {
	return swRow{name, o, tool, agent, map[string]any{"file_path": path, "content": "x", "old_string": "a", "new_string": "b"}}
}

// swCall makes the row's call from session, with anything else the session's
// environment carries.
func swCall(t *testing.T, session string, r swRow, carried map[string]string) reply {
	t.Helper()
	vars := map[string]string{"CLAUDE_PROJECT_DIR": r.o.project}
	maps.Copy(vars, carried)
	e := map[string]any{
		"hook_event_name": "PreToolUse", "session_id": session,
		"tool_name": r.tool, "cwd": r.o.cwd, "tool_input": r.input,
	}
	if r.agent != "" {
		e["agent_type"] = r.agent
	}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return call(t, string(raw), env(vars))
}

func swReason(r reply) string { return r.HookSpecificOutput.PermissionDecisionReason }

// swRefused holds every row to a refusal from session, by rule where one is
// named, with a reason that says what to do instead.
func swRefused(t *testing.T, session string, rows []swRow, rule string, carried map[string]string) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			got := swCall(t, session, r, carried)
			if !denied(got) {
				t.Fatalf("allowed, from %s: %v", r.o.name, r.input)
			}
			if rule != "" && !strings.Contains(swReason(got), "["+rule+"]") {
				t.Errorf("refused, but not as %s: %s", rule, swReason(got))
			}
			if !strings.Contains(swReason(got), "Instead: ") {
				t.Errorf("the refusal does not say what to do instead: %s", swReason(got))
			}
		})
	}
}

// swAllowed holds every row to going through.
func swAllowed(t *testing.T, session string, rows []swRow, carried map[string]string) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			if got := swCall(t, session, r, carried); denied(got) {
				t.Errorf("refused, from %s: %s", r.o.name, swReason(got))
			}
		})
	}
}

// swLink makes a link at name to target, or skips where the platform will not.
func swLink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
}

// ---------------------------------------------------------------- AC-6

// AC-6, the freeze: the story's frozen test, and a new test after the freeze,
// are refused to the file tools and to the shell, by an absolute path and by
// the path relative to wherever the call runs.
func TestAC6ASessionHeldInItsStoryWorktreeIsHeldToTheFreezeWhereverItWasOpened(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	frozen := filepath.Join(l.wt, "invoice_test.go")
	for _, o := range swOpenings(l) {
		t.Run("AC-6 "+o.name, func(t *testing.T) {
			swRefused(t, swHeld, []swRow{
				swFile("Edit the frozen test", o, "Edit", "sdlc:implementer", frozen),
				swFile("Write the frozen test", o, "Write", "sdlc:sdet", frozen),
				swFile("Edit the frozen test by the main conversation", o, "Edit", "", frozen),
			}, "frozen-test-is-not-edited", nil)
			swRefused(t, swHeld, []swRow{
				swFile("Write a new test after the freeze", o, "Write", "sdlc:sdet", filepath.Join(l.wt, "credit_test.go")),
			}, "no-new-test-after-the-freeze", nil)
			swRefused(t, swHeld, []swRow{
				swBash("append to the frozen test", o, "echo cheat >> "+frozen),
				swBash("remove the frozen test", o, "rm -f "+frozen),
				swBash("rewrite the frozen test in place", o, "sed -i.bak s/AC-1/AC-2/ "+frozen),
			}, "frozen-test-through-the-tool", nil)
		})
	}

	fromMain := swOpenings(l)[1]
	swRefused(t, swHeld, []swRow{
		swFile("AC-6 Edit the frozen test relative to the main checkout", fromMain, "Edit", "sdlc:implementer",
			filepath.Join(".sdlc", "worktrees", "A-1", "invoice_test.go")),
		swBash("AC-6 append to the frozen test relative to the main checkout", fromMain,
			"echo cheat >> .sdlc/worktrees/A-1/invoice_test.go"),
		swBash("AC-6 change into the worktree and append", fromMain,
			"cd .sdlc/worktrees/A-1 && echo cheat >> invoice_test.go"),
	}, "", nil)
	inWorktree := swOpenings(l)[2]
	swRefused(t, swHeld, []swRow{
		swBash("AC-6 append to the frozen test by its own name", inWorktree, "echo cheat >> invoice_test.go"),
	}, "frozen-test-through-the-tool", nil)
}

// AC-6, the protected paths: the worktree's own loop state, configuration and
// session settings, and the story's backlog and record in the main checkout.
func TestAC6ASessionHeldInItsStoryWorktreeIsRefusedItsProtectedPathsWhereverItWasOpened(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	for _, o := range swOpenings(l) {
		t.Run("AC-6 "+o.name, func(t *testing.T) {
			swRefused(t, swHeld, []swRow{
				swFile("Write the freeze", o, "Write", "sdlc:implementer", filepath.Join(l.wt, ".sdlc", "state", "tests.lock")),
				swFile("Write the active story", o, "Write", "", filepath.Join(l.wt, ".sdlc", "state", "active")),
				swFile("Edit the configuration", o, "Edit", "sdlc:implementer", filepath.Join(l.wt, ".sdlc", "config.json")),
				swFile("Edit CLAUDE.md", o, "Edit", "sdlc:implementer", filepath.Join(l.wt, "CLAUDE.md")),
				swFile("Write the session settings", o, "Write", "sdlc:implementer", filepath.Join(l.wt, ".claude", "settings.json")),
			}, "write-protected-path", nil)
			swRefused(t, swHeld, []swRow{
				swBash("remove the freeze", o, "rm -f "+filepath.Join(l.wt, ".sdlc", "state", "tests.lock")),
				swBash("rewrite the active story", o, "echo B-2 > "+filepath.Join(l.wt, ".sdlc", "state", "active")),
			}, "loop-state-through-the-tool", nil)
			swRefused(t, swHeld, []swRow{
				swBash("rewrite the backlog", o, "echo '{}' > "+filepath.Join(l.main, "user_stories.json")),
				swBash("remove the story's record", o, "rm -f "+filepath.Join(l.main, ".sdlc", "stories", "A-1", "gate-record.json")),
				swFile("Write the story's record", o, "Write", "", filepath.Join(l.main, ".sdlc", "stories", "A-1", "gate-record.json")),
			}, "", nil)
		})
	}
}

// AC-6, the commit gate: a commit of the story's worktree waits for the gates
// however the command reaches the worktree, and goes through once they have
// passed on the work as it stands. A commit of the main checkout is another
// checkout's.
func TestAC6ASessionHeldInItsStoryWorktreeMeetsTheCommitGateWhereverItWasOpened(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	openings := swOpenings(l)
	inWorktree, fromMain, moved := openings[0], openings[1], openings[2]
	commits := []swRow{
		swBash("AC-6 commit in the worktree", inWorktree, `git commit -m "done"`),
		swBash("AC-6 commit in the worktree after moving there", moved, `git commit -m "done"`),
		swBash("AC-6 commit through git -C relative to the main checkout", fromMain, `git -C .sdlc/worktrees/A-1 commit -m "done"`),
		swBash("AC-6 commit through git -C by the worktree's path", fromMain, `git -C `+l.wt+` commit -m "done"`),
		swBash("AC-6 change into the worktree and commit", fromMain, `cd .sdlc/worktrees/A-1 && git commit -m "done"`),
	}
	swRefused(t, swHeld, commits, "commit-gate", nil)

	swReady(t, l)
	swAllowed(t, swHeld, commits, nil)
	swRefused(t, swHeld, []swRow{
		swBash("AC-6 commit in the main checkout", fromMain, `git commit -m "done"`),
	}, "commit-in-another-checkout", nil)
}

// AC-6 holds the session to its rules, not away from its work: its own code,
// reading anywhere, and listing the worktrees go through.
func TestAC6TheSessionsOwnWorkAndReadingAreNotRefused(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	for _, o := range swOpenings(l) {
		t.Run("AC-6 "+o.name, func(t *testing.T) {
			swAllowed(t, swHeld, []swRow{
				swFile("Write the story's code", o, "Write", "sdlc:implementer", filepath.Join(l.wt, "invoice.go")),
				swBash("run the tests", o, "go test ./... -count=1"),
				swBash("read the frozen test", o, "cat "+filepath.Join(l.wt, "invoice_test.go")),
				swBash("read the backlog", o, "cat "+filepath.Join(l.main, "user_stories.json")),
				swBash("list the worktrees", o, "git worktree list"),
				swBash("list the worktrees for a script", o, "git worktree list --porcelain"),
				swBash("list the worktrees directory", o, "ls "+filepath.Join(l.main, ".sdlc", "worktrees")),
				swBash("read another story's loop state", o, "cat "+filepath.Join(l.other, ".sdlc", "state", "active")),
				swBash("look for a word in the log", o, `git log --oneline --grep "worktree remove"`),
				swBash("say the words", o, `echo "git worktree prune and git worktree remove are refused"`),
			}, nil)
		})
	}
	inWorktree := swOpenings(l)[0]
	outside := filepath.Join(wtTempDir(t), "scratch")
	swAllowed(t, swHeld, []swRow{
		swBash("AC-6 rename a file of its own", inWorktree, "mv notes.txt notes.md"),
		swBash("AC-6 remove a directory of its own", inWorktree, "mkdir -p build && rm -rf build"),
		swBash("AC-6 remove a directory in no checkout", inWorktree, "rm -rf "+outside),
	}, nil)
}

// ---------------------------------------------------------------- T1

// T1 and decision 4: the claim names the worktree's path, so a session that
// takes its worktree away, or moves it, holds nothing afterwards and is held
// to nothing. Every spelling of it is refused: git's own worktree commands, and
// a move or a removal of the worktree or a directory above it, by its path,
// relative, after a cd, through the variables and shorthands the shell has for
// where it is, through a link, and through programs that remove for it.
func TestAC6AndT1ASessionCannotEndItsHoldByTakingItsStoryWorktreeAway(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	openings := swOpenings(l)
	w, m := openings[0], openings[1]
	gone := filepath.Join(wtTempDir(t), "moved")
	link := filepath.Join(wtTempDir(t), "link-to-the-worktree")
	if err := os.Symlink(l.wt, link); err != nil {
		link = ""
	}

	rows := []swRow{
		// git's own worktree commands
		swBash("T1 git worktree remove of the worktree it runs in", w, "git worktree remove --force ."),
		swBash("T1 git worktree remove by its path", w, "git worktree remove --force "+l.wt),
		swBash("T1 git worktree remove forced past a lock", w, "git worktree remove -f -f "+l.wt),
		swBash("T1 git worktree remove without force", w, "git worktree remove "+l.wt),
		swBash("T1 git worktree move of the worktree it runs in", w, "git worktree move . "+gone),
		swBash("T1 git worktree move by its path", w, "git worktree move "+l.wt+" ../moved"),
		swBash("T1 git worktree prune", w, "git worktree prune"),
		swBash("T1 git worktree prune now", w, "git worktree prune --expire now"),
		swBash("T1 remove what git keeps for it and prune", w, "rm -rf "+filepath.Join(l.main, ".git", "worktrees", "A-1")+" && git worktree prune"),
		swBash("T1 git -C the main checkout", w, "git -C "+l.main+" worktree remove --force .sdlc/worktrees/A-1"),
		swBash("T1 git --git-dir the common directory", w, "git --git-dir="+filepath.Join(l.main, ".git")+" worktree remove --force "+l.wt),
		swBash("T1 git --git-dir with a space", w, "git --git-dir "+filepath.Join(l.main, ".git")+" worktree prune"),
		swBash("T1 an inline alias", w, "git -c alias.gone='worktree remove --force' gone ."),
		swBash("T1 an inline alias to the shell", w, "git -c alias.gone='!git worktree prune' gone"),
		swBash("T1 an alias set for later", w, "git config alias.gone 'worktree remove --force'"),
		swBash("T1 a global alias to the shell set for later", w, "git config --global alias.gone '!git worktree prune'"),
		swBash("T1 handed to sh -c", w, "sh -c 'git worktree remove --force .'"),
		swBash("T1 handed to bash -c", w, `bash -c "git worktree move . ../moved"`),
		swBash("T1 after a semicolon", w, "true; git worktree prune"),
		swBash("T1 after &&", w, "true && git worktree prune"),
		swBash("T1 after a pipe", w, "echo y | git worktree prune"),
		swBash("T1 in a subshell", w, "(git worktree move . ../moved)"),
		swBash("T1 through xargs", w, "echo . | xargs git worktree remove --force"),
		swBash("T1 through env", w, "env git worktree prune"),
		swBash("T1 quoted words", w, `git 'worktree' "remove" --force .`),
		swBash("T1 a backslash in the subcommand", w, `git work\tree remove --force .`),
		swBash("T1 a backslash in the action", w, `git worktree re\move --force .`),
		swBash("T1 a line continued", w, "git worktree \\\n  prune"),

		// mv of the worktree
		swBash("T1 mv by its path", w, "mv "+l.wt+" "+gone),
		swBash("T1 mv by its path quoted", w, `mv "`+l.wt+`" `+gone),
		swBash("T1 mv relative through ..", w, "mv ../A-1 ../moved"),
		swBash("T1 mv after cd ..", w, "cd .. && mv A-1 moved"),
		swBash("T1 mv from the main checkout's root", w, "cd "+l.main+" && mv .sdlc/worktrees/A-1 moved"),
		swBash("T1 mv of $PWD", w, `mv "$PWD" `+gone),
		swBash("T1 mv of ${PWD}", w, "mv ${PWD} "+gone),
		swBash("T1 mv of $(pwd)", w, `mv "$(pwd)" `+gone),
		swBash("T1 mv of `pwd`", w, "mv `pwd` "+gone),
		swBash("T1 mv of $OLDPWD after cd ..", w, `cd .. && mv "$OLDPWD" moved`),
		swBash("T1 mv of ~+", w, "mv ~+ "+gone),
		swBash("T1 mv of ~- after cd ..", w, "cd .. && mv ~- moved"),
		swBash("T1 mv of the directory above it", w, "mv .. "+filepath.Join(l.main, "wts")),
		swBash("T1 mv of the directory above it by path", w, "mv "+filepath.Join(l.main, ".sdlc", "worktrees")+" "+filepath.Join(l.main, "wts")),
		swBash("T1 mv handed to sh -c", w, "sh -c 'mv "+l.wt+" "+gone+"'"),
		swPowerShell("T1 Move-Item", w, "Move-Item "+l.wt+" "+gone),
		swPowerShell("T1 Rename-Item", w, "Rename-Item "+l.wt+" moved"),

		// removing the worktree
		swBash("T1 rm -rf by its path", w, "rm -rf "+l.wt),
		swBash("T1 rm -rf relative through ..", w, "rm -rf ../A-1"),
		swBash("T1 rm -rf after cd ..", w, "cd .. && rm -rf A-1"),
		swBash("T1 rm -rf of $PWD", w, `rm -rf "$PWD"`),
		swBash("T1 rm -rf of the directory above it", w, "rm -rf "+filepath.Join(l.main, ".sdlc", "worktrees")),
		swBash("T1 rm -rf of .. ", w, "rm -rf .."),
		swBash("T1 find -exec rm", w, "find "+filepath.Join(l.main, ".sdlc", "worktrees")+" -maxdepth 1 -name A-1 -exec rm -rf {} +"),
		swBash("T1 find -delete", w, "find "+filepath.Join(l.main, ".sdlc", "worktrees")+" -delete"),
		swBash("T1 python's rmtree", w, `python3 -c 'import shutil; shutil.rmtree("`+filepath.ToSlash(l.wt)+`")'`),
		swBash("T1 through xargs", w, "echo "+l.wt+" | xargs rm -rf"),
		swPowerShell("T1 Remove-Item -Recurse", w, "Remove-Item -Recurse -Force "+l.wt),

		// from the main checkout, where the session was opened
		swBash("T1 git worktree remove from the main checkout", m, "git worktree remove --force .sdlc/worktrees/A-1"),
		swBash("T1 git worktree prune from the main checkout", m, "git worktree prune"),
		swBash("T1 mv from the main checkout", m, "mv .sdlc/worktrees/A-1 "+gone),
		swBash("T1 rm -rf from the main checkout", m, "rm -rf .sdlc/worktrees/A-1"),
		swBash("T1 rm -rf of every story's worktree from the main checkout", m, "rm -rf .sdlc/worktrees"),
	}
	if link != "" {
		rows = append(rows,
			swBash("T1 git worktree remove through a link", w, "git worktree remove --force "+link),
			swBash("T1 mv through a link with a trailing slash", w, "mv "+link+"/ "+gone))
	}
	swRefused(t, swHeld, rows, "", nil)
}

// T1: `~` is the shell's for the home directory, and a worktree under it is
// the same worktree by that name.
func TestAC6AndT1TheStoryWorktreeUnderHomeIsItselfByTilde(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the home directory is read from USERPROFILE on Windows, and ~ is the shell's there")
	}
	l := swCheckouts(t)
	home := filepath.Dir(l.main)
	t.Setenv("HOME", home)
	rel, err := filepath.Rel(home, l.wt)
	if err != nil {
		t.Fatal(err)
	}
	w := swOpenings(l)[0]
	swRefused(t, swHeld, []swRow{
		swBash("T1 mv by ~", w, "mv ~/"+filepath.ToSlash(rel)+" ~/moved"),
		swBash("T1 rm -rf by ~", w, "rm -rf ~/"+filepath.ToSlash(rel)),
	}, "", nil)
}

// ---------------------------------------------------------------- T2

// T2 and decision 4: another story's worktree is not the session's to remove or
// move either, by git or by hand, and nor is the main checkout the session was
// opened in. Its uncommitted work would go with it, and its story would be
// held by a checkout that no longer exists.
func TestT2ASessionCannotRemoveOrMoveAnotherStorysWorktreeOrTheMainCheckout(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	openings := swOpenings(l)
	w, m := openings[0], openings[1]
	gone := filepath.Join(wtTempDir(t), "moved")
	swRefused(t, swHeld, []swRow{
		swBash("T2 git worktree remove of another story's", w, "git worktree remove --force "+l.other),
		swBash("T2 git worktree remove of another story's relative", w, "git worktree remove --force ../B-2"),
		swBash("T2 git worktree move of another story's", w, "git worktree move "+l.other+" "+gone),
		swBash("T2 git -C another story's worktree remove .", w, "git -C "+l.other+" worktree remove --force ."),
		swBash("T2 git worktree remove of another story's from the main checkout", m, "git worktree remove --force .sdlc/worktrees/B-2"),
		swBash("T2 mv of another story's", w, "mv "+l.other+" "+gone),
		swBash("T2 rm -rf of another story's", w, "rm -rf "+l.other),
		swBash("T2 rm -rf of another story's relative", w, "rm -rf ../B-2"),
		swBash("T2 mv of the main checkout by $PWD", m, `mv "$PWD" `+gone),
		swBash("T2 rm -rf of the main checkout by $PWD", m, `rm -rf "$PWD"`),
		swBash("T2 mv of the main checkout by ~+", m, "mv ~+ "+gone),
		swBash("T2 rm -rf of the main checkout", w, "rm -rf "+l.main),
	}, "", nil)
}

// ---------------------------------------------------------------- T5

// T5 and decision 5: a checkout is found by walking up to the nearest .git of
// this repository, so a worktree made inside a checkout's protected paths
// becomes the nearest checkout for everything beneath it, and what it holds
// is protected by nothing. A held session may not make one there, in its own
// checkout or another, by any spelling of the path or of git.
func TestT5ASessionCannotAddAWorktreeIntoAProtectedPath(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	openings := swOpenings(l)
	w, m := openings[0], openings[1]
	stories := filepath.Join(l.main, ".sdlc", "stories")
	rows := []swRow{
		swBash("T5 into the main checkout's story records", w, "git worktree add "+filepath.Join(stories, "x")),
		swBash("T5 into the main checkout's loop state on a new branch", w, "git worktree add -b evil "+filepath.Join(l.main, ".sdlc", "state", "x")),
		swBash("T5 into the main checkout's session settings, reset", w, "git worktree add -B evil "+filepath.Join(l.main, ".claude", "x")),
		swBash("T5 into another story's loop state, detached", w, "git worktree add --detach "+filepath.Join(l.other, ".sdlc", "state", "x")),
		swBash("T5 into a story's directory as an orphan", w, "git worktree add --orphan -b evil "+filepath.Join(stories, "A-1", "x")),
		swBash("T5 into the common git directory, forced and locked", w, "git worktree add -f --lock --reason why "+filepath.Join(l.main, ".git", "x")),
		swBash("T5 with a commit after the path", w, "git worktree add "+filepath.Join(stories, "x")+" main"),
		swBash("T5 through git -C the main checkout", w, "git -C "+l.main+" worktree add .sdlc/stories/x"),
		swBash("T5 through --git-dir", w, "git --git-dir="+filepath.Join(l.main, ".git")+" worktree add "+filepath.Join(stories, "x")),
		swBash("T5 relative through ..", w, "git worktree add ../../stories/x"),
		swBash("T5 into its own loop state", w, "git worktree add .sdlc/state/x"),
		swBash("T5 into its own session settings", w, "git worktree add .claude/x"),
		swBash("T5 handed to sh -c", w, "sh -c 'git worktree add "+filepath.Join(stories, "x")+"'"),
		swBash("T5 after &&", w, "true && git worktree add "+filepath.Join(l.main, ".sdlc", "state", "x")+" main"),
		swBash("T5 quoted", w, `git worktree add "`+filepath.Join(stories, "x")+`"`),
		swBash("T5 from the main checkout into its story records", m, "git worktree add .sdlc/stories/x"),
		swBash("T5 from the main checkout into the story worktree's loop state", m, "git worktree add .sdlc/worktrees/A-1/.sdlc/state/x"),
	}
	link := filepath.Join(wtTempDir(t), "link-to-the-records")
	if err := os.Symlink(stories, link); err == nil {
		rows = append(rows, swBash("T5 through a link to the story records", w, "git worktree add "+filepath.Join(link, "x")))
	}
	swRefused(t, swHeld, rows, "", nil)
}

// ---------------------------------------------------------------- T4

// T4 and decision 6: once the story may commit, a commit git is pointed at
// another checkout with lands that checkout's work on a branch -- the gates
// reviewed none of it. GIT_DIR and GIT_WORK_TREE by any spelling, the value a
// session already carries, core.worktree, and a --work-tree add before the
// commit, are each a commit in another checkout.
func TestAC6AndT4ACommitPointedAtAnotherCheckoutIsRefusedWhateverTheGatesSay(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	swReady(t, l)
	w := swOpenings(l)[0]
	common := filepath.Join(l.main, ".git")
	swRefused(t, swHeld, []swRow{
		swBash("T4 GIT_DIR in front of the commit", w, "GIT_DIR="+common+" git commit -m x"),
		swBash("T4 GIT_WORK_TREE in front of the commit", w, "GIT_WORK_TREE="+l.main+" git commit -m x"),
		swBash("T4 both, at another story's worktree", w, "GIT_DIR="+filepath.Join(common, "worktrees", "B-2")+" GIT_WORK_TREE="+l.other+" git commit -m x"),
		swBash("T4 GIT_WORK_TREE relative", w, "GIT_WORK_TREE=../../.. git commit -m x"),
		swBash("T4 GIT_DIR quoted", w, `GIT_DIR="`+common+`" git commit -m x`),
		swBash("T4 export earlier, after a semicolon", w, "export GIT_DIR="+common+"; git commit -m x"),
		swBash("T4 export earlier, after &&", w, "export GIT_WORK_TREE="+l.main+" && git commit -m x"),
		swBash("T4 assigned, then exported", w, "GIT_WORK_TREE="+l.main+"; export GIT_WORK_TREE; git commit -m x"),
		swBash("T4 declare -x", w, "declare -x GIT_WORK_TREE="+l.main+"; git commit -m x"),
		swBash("T4 in a subshell", w, "(export GIT_WORK_TREE="+l.main+"; git commit -m x)"),
		swBash("T4 through env", w, "env GIT_WORK_TREE="+l.main+" git commit -m x"),
		swBash("T4 through env -i", w, "env -i GIT_DIR="+common+" git commit -m x"),
		swBash("T4 handed to bash -c", w, "bash -c 'GIT_DIR="+common+" git commit -m x'"),
		swBash("T4 core.worktree", w, "git -c core.worktree="+l.main+" commit -m x"),
		swBash("T4 core.worktree in another case", w, "git -c core.WorkTree="+l.main+" commit -m x"),
		swBash("T4 --work-tree add, then a plain commit", w, "git --work-tree="+l.main+" add -A && git commit -m x"),
		swBash("T4 --work-tree add with a space, then a commit", w, "git --work-tree "+l.main+" add . ; git commit -m x"),
		swBash("T4 GIT_WORK_TREE add, then a plain commit", w, "GIT_WORK_TREE="+l.main+" git add -A; git commit -m x"),
		swPowerShell("T4 PowerShell's environment drive", w, "$env:GIT_WORK_TREE='"+l.main+"'; git commit -m x"),
	}, "commit-in-another-checkout", nil)

	// A value the session already carries is in the environment the hook is
	// run in, and nowhere in the words.
	for name, value := range map[string]string{"GIT_DIR": common, "GIT_WORK_TREE": l.main} {
		swRefused(t, swHeld, []swRow{
			swBash("T4 "+name+" carried by the session", w, "git commit -m x"),
		}, "commit-in-another-checkout", map[string]string{name: value})
	}

	own := filepath.Join(common, "worktrees", "A-1")
	swAllowed(t, swHeld, []swRow{
		swBash("T4 a plain commit of the story's own work", w, "git commit -m x"),
		swBash("T4 GIT_DIR at the worktree's own git directory", w, "GIT_DIR="+own+" git commit -m x"),
		swBash("T4 GIT_WORK_TREE at the worktree itself", w, "GIT_WORK_TREE="+l.wt+" git commit -m x"),
		swBash("T4 core.worktree at the worktree itself", w, "git -c core.worktree="+l.wt+" commit -m x"),
		swBash("T4 reading the main checkout's history through GIT_DIR", w, "GIT_DIR="+common+" git log --oneline"),
		swBash("T4 the main checkout's status through --work-tree", w, "git --work-tree="+l.main+" status"),
		swBash("T4 a diff with GIT_WORK_TREE exported", w, "export GIT_WORK_TREE="+l.main+"; git diff"),
		swBash("T4 a message that mentions GIT_DIR", w, `git commit -m "GIT_DIR=`+common+` is not set here"`),
	}, nil)
	swAllowed(t, swHeld, []swRow{
		swBash("T4 GIT_DIR carried at the worktree's own git directory", w, "git commit -m x"),
	}, map[string]string{"GIT_DIR": own})
}

// T2 and decision 4: a worktree a person moved by hand still commits into this
// repository -- its .git file leads here, only git's way back names where it
// was -- and a commit there is still another checkout's.
func TestAC6AndT2ACommitInALinkedWorktreeMovedByHandIsACommitInAnotherCheckout(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	swReady(t, l)
	moved := filepath.Join(wtTempDir(t), "B-2-moved")
	if err := os.Rename(l.other, moved); err != nil {
		t.Fatal(err)
	}
	w := swOpenings(l)[0]
	swRefused(t, swHeld, []swRow{
		swBash("T2 git -C the moved worktree", w, "git -C "+moved+" commit -m x"),
		swBash("T2 cd into the moved worktree", w, "cd "+moved+" && git commit -m x"),
		swBash("T2 a merge in the moved worktree", w, "git -C "+moved+" merge story/A-1"),
	}, "commit-in-another-checkout", nil)
	swAllowed(t, swHeld, []swRow{
		swBash("T2 reading the moved worktree's history", w, "git -C "+moved+" log --oneline"),
	}, nil)
}

// ---------------------------------------------------------------- T7

// T7: a session held in the main checkout was judged there for every file tool
// call, and a story worktree nested in it was a directory under .sdlc, where
// the main conversation may write and no freeze of the main checkout's
// reaches. Another story's code, its frozen tests and its loop state were all
// writable. A file in a checkout nested in the one the session is held in is
// not the session's.
func TestT7ASessionHeldInTheMainCheckoutCannotWriteAStoryWorktreesFilesWithTheFileTools(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	write(t, l.main, "user_stories.json", swBacklog("in_progress", "in_progress", "in_progress"))
	write(t, l.main, ".sdlc/state/active", "C-3\n")
	write(t, l.main, ".sdlc/state/session", swMain+"\n")
	wtRecord(t, l.main, "C-3")
	swClaim(t, l.main, "C-3")
	hand := filepath.Join(l.main, "wts", "by-hand")
	wtGit(t, l.main, "worktree", "add", "--quiet", "-b", "by-hand", hand, "main")

	m := swOpening{"opened in the main checkout", l.main, l.main}
	cdOther := swOpening{"opened in the main checkout with the shell in another story's worktree", l.main, l.other}
	swRefused(t, swMain, []swRow{
		swFile("T7 Edit another story's code", m, "Edit", "", filepath.Join(l.other, "refund.go")),
		swFile("T7 Write another story's code as the implementer", m, "Write", "sdlc:implementer", filepath.Join(l.other, "refund.go")),
		swFile("T7 MultiEdit a story's code", m, "MultiEdit", "", filepath.Join(l.wt, "invoice.go")),
		swFile("T7 Edit another story's frozen test as the implementer", m, "Edit", "sdlc:implementer", filepath.Join(l.other, "refund_test.go")),
		swFile("T7 Write another story's frozen test as the sdet", m, "Write", "sdlc:sdet", filepath.Join(l.other, "refund_test.go")),
		swFile("T7 Edit another story's frozen test by the main conversation", m, "Edit", "", filepath.Join(l.other, "refund_test.go")),
		swFile("T7 Write another story's loop state", m, "Write", "", filepath.Join(l.other, ".sdlc", "state", "active")),
		swFile("T7 Write another story's configuration", m, "Write", "sdlc:implementer", filepath.Join(l.other, ".sdlc", "config.json")),
		swFile("T7 Write another story's code by its relative path", m, "Write", "sdlc:implementer", filepath.Join(".sdlc", "worktrees", "B-2", "refund.go")),
		swFile("T7 Write a worktree made by hand inside the main checkout", m, "Write", "sdlc:implementer", filepath.Join(hand, "invoice.go")),
		swFile("T7 Write another story's code from inside its worktree", cdOther, "Write", "sdlc:implementer", "refund.go"),
	}, "", nil)

	swAllowed(t, swMain, []swRow{
		swFile("T7 Write the main checkout's own code", m, "Write", "sdlc:implementer", filepath.Join(l.main, "invoice.go")),
		swFile("T7 Write the main checkout's own test before its freeze", m, "Write", "sdlc:sdet", filepath.Join(l.main, "invoice_test.go")),
		swFile("T7 Write a note in the main checkout's story directory", m, "Write", "", filepath.Join(l.main, ".sdlc", "stories", "C-3", "notes.md")),
	}, nil)
}

// ---------------------------------------------------------------- carried

// Decision 7, item 6, from US-029.1: `..` after a directory the same command
// makes is read through the link that follows it, from a story worktree as
// from anywhere. Where d is not there yet, `d/../lnk/x` taken apart by name was
// x beside the link, and the filesystem, once `mkdir -p d` had run, wrote it
// where the link leads: the main checkout's record, or another story's state.
func TestAC6DotDotAfterADirectoryMadeInTheSameCommandIsReadThroughTheLinkFromAStoryWorktree(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	swLink(t, filepath.Join(l.main, ".sdlc", "stories", "A-1"), filepath.Join(l.wt, "lnk"))
	swLink(t, filepath.Join(l.other, ".sdlc", "state"), filepath.Join(l.wt, "lnk2"))
	w := swOpenings(l)[0]
	swRefused(t, swHeld, []swRow{
		swBash("AC-6 mkdir -p d, then d/../lnk into the record", w, "mkdir -p d && echo '{}' > d/../lnk/gate-record.json"),
		swBash("AC-6 two levels made, then back through them", w, "mkdir -p d/e && cp /dev/null d/e/../../lnk/gate-record.json"),
		swBash("AC-6 after a semicolon, into another story's state", w, "mkdir -p d; echo B-2 > d/../lnk2/active"),
		swBash("AC-6 removing through it", w, "mkdir d && rm -f d/../lnk/gate-record.json"),
	}, "", nil)
}

// US-029.1's T12, carried: git's own record of each worktree is under the
// common git directory, and the .git name rule covers it from a story
// worktree nested in the main checkout, for the session's own entry and
// another story's.
func TestAC6WhatGitKeepsForEachWorktreeIsRefusedFromAStoryWorktree(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	w := swOpenings(l)[0]
	entries := filepath.Join(l.main, ".git", "worktrees")
	swRefused(t, swHeld, []swRow{
		swBash("AC-6 rewrite another story's way back", w, "echo x > "+filepath.Join(entries, "B-2", "gitdir")),
		swBash("AC-6 rewrite its own commondir", w, "echo x > "+filepath.Join(entries, "A-1", "commondir")),
		swBash("AC-6 remove another story's entry", w, "rm -rf "+filepath.Join(entries, "B-2")),
	}, "", nil)
}

// US-029.1's T1, carried: an escalation ends the iteration, so only the record
// in the main checkout says the story waits, and a session in the story's
// worktree nested there is refused the approval as it is in a single checkout.
func TestAC6APersonsApprovalIsRefusedInANestedStoryWorktreeWhileTheStoryWaits(t *testing.T) {
	t.Parallel()
	l := swCheckouts(t)
	at := time.Now()
	record := model.NewRecord("A-1", at)
	record.Escalate("spec_unclear", "which greeting?", "", at)
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, l.main, ".sdlc/stories/A-1/gate-record.json", string(raw))
	write(t, l.main, "user_stories.json", swBacklog("awaiting_human", "in_progress", "ready"))
	for _, name := range []string{"active", "session"} {
		if err := os.Remove(filepath.Join(l.wt, ".sdlc", "state", name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range swOpenings(l) {
		for _, command := range []string{"sdlc approve A-1", "sdlc approve"} {
			if got := swAsk(t, wtNobody, o, "Bash", map[string]any{"command": command}, nil); !denied(got) {
				t.Errorf("AC-6 %s: a session ran %q while A-1 waits for a person", o.name, command)
			}
		}
	}
}
