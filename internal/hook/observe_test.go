package hook

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

func gitIn(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// readyToCommit is the state the hook is in when it allows a commit: an
// iteration running on A-1, a real repository with a commit in it, and a record
// whose gates before the commit have all passed with the reviews stamped
// against the tree as it stands.
func readyToCommit(t *testing.T) string {
	t.Helper()
	root := loopProject(t)
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	gitIn(t, root, "init", "--quiet")
	write(t, root, "invoice.go", "package invoice\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit", "--quiet", "-m", "the work")
	recordPastEveryGate(t, root)
	return root
}

// recordPastEveryGate writes A-1's record with every gate before the commit
// passed and the reviews stamped against the tree as it stands.
func recordPastEveryGate(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: root, Config: cfg}).ReviewSubject(t.Context())
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
			record.AddReview(model.Review{Gate: gate, Role: r.Role,
				Verdict: model.VerdictApprove, Subject: tree}, at)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))
}

// commits are the heads the loop wrote down for the story being worked on.
func commits(t *testing.T, root string) []string {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.New(&config.Project{Root: root, Config: cfg}).ObservedCommits(t.Context(), "A-1")
	if err != nil {
		t.Fatalf("ObservedCommits: %v", err)
	}
	return got
}

func skipIfPermissionsDoNotBind(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this platform does not restrict a directory owner's own writes by permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

// The commit gate records the commits the loop watched the session make, and
// the hook is where the watching happens: it is asked before every command, and
// a commit it allows is one it is about to see made. What it writes down is the
// HEAD that commit will be made on, because the commit itself does not exist
// yet when the hook is asked.
func TestTheHookWritesDownTheHeadEveryAllowedCommitIsMadeOn(t *testing.T) {
	root := readyToCommit(t)
	head := gitIn(t, root, "rev-parse", "HEAD")

	if r := call(t, command(root, "", `git commit -m "the work"`), noEnv); denied(r) {
		t.Fatalf("a commit past every gate was refused: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}
	if got := commits(t, root); len(got) != 1 || got[0] != head {
		t.Errorf("the loop wrote down %v, want the HEAD the commit is made on, %s", got, head)
	}
}

// A commit the hook refused never happened, and a command that makes no commit
// makes none however the refusal has to treat it. An observation planted by
// either says the session was at this HEAD about to commit, and the next commit
// made on that HEAD -- a person's, from their own terminal -- is then recorded
// as the story's. That is US-004's defect on demand.
func TestNothingIsWrittenDownForACommandThatMakesNoCommit(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		refused       bool
	}{
		{"a commit refused because the gates have not passed", `git commit -m "the work"`, true},
		{"setting an alias that commits later", "git config alias.ci commit", false},
		{"a commit rehearsed rather than made", "git commit --dry-run", false},
		{"a command that is not a commit at all", "git status --porcelain", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := readyToCommit(t)
			if tc.refused {
				write(t, root, ".sdlc/stories/A-1/gate-record.json", `{"story":"A-1"}`)
			}
			r := call(t, command(root, "", tc.command), noEnv)
			if got := denied(r); got != tc.refused {
				t.Fatalf("the command was %s: %s", map[bool]string{true: "refused", false: "allowed"}[got],
					r.HookSpecificOutput.PermissionDecisionReason)
			}
			if got := commits(t, root); len(got) != 0 {
				t.Errorf("the loop wrote down %v for a command that commits nothing", got)
			}

			// The control. Nothing written down means nothing only once the
			// recorder is running at all, so the same project is asked about a
			// commit that does make one.
			recordPastEveryGate(t, root)
			if r := call(t, command(root, "", `git commit -m "the work"`), noEnv); denied(r) {
				t.Fatalf("a commit past every gate was refused: %s",
					r.HookSpecificOutput.PermissionDecisionReason)
			}
			if got := commits(t, root); len(got) != 1 {
				t.Fatalf("the loop wrote down %v for a commit it allowed, so nothing above "+
					"was the recorder's answer rather than its absence", got)
			}
		})
	}
}

// `sdlc init` writes no .gitignore, so in many projects whatever the loop
// writes down is committed and published. Which commits are a story's is
// answered by shas and a story id; the command that made them is not needed for
// it, and a command line is exactly where a path, a token or a shape nobody
// wants written down would arrive.
func TestWhatIsWrittenDownIsCommitsAndAStoryAndNotWhatWasTyped(t *testing.T) {
	root := readyToCommit(t)
	secret := "do-not-write-this-down-4f21c9"

	if r := call(t, command(root, "", `git commit -m "the work `+secret+`"`), noEnv); denied(r) {
		t.Fatalf("a commit past every gate was refused: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}
	if got := commits(t, root); len(got) != 1 {
		t.Fatalf("the loop wrote down %v, want the one head the commit is made on", got)
	}

	// Nothing in this test ever commits that message, so git's own files
	// cannot be where the text came from.
	gitDir := filepath.Join(root, ".git")
	err := filepath.WalkDir(gitDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), secret) {
			rel, _ := filepath.Rel(gitDir, path)
			t.Errorf("the command a session typed was written down, in %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The hook fails open on its own bugs: a refusal is a decision the code made,
// never a side effect of something going wrong. A recorder that could stop a
// commit would be the loop refusing finished work over its own note to itself,
// and the note is a convenience -- the refusal is the control.
func TestACommitIsAllowedWhenTheRecorderCannotWriteItDown(t *testing.T) {
	skipIfPermissionsDoNotBind(t)
	root := readyToCommit(t)
	first := gitIn(t, root, "rev-parse", "HEAD")

	if r := call(t, command(root, "", `git commit -m "the work"`), noEnv); denied(r) {
		t.Fatalf("a commit past every gate was refused: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}
	dir := observationOf(t, root, first)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := os.WriteFile(filepath.Join(dir, "probe"), nil, 0o600); err == nil {
		t.Skip("this filesystem does not deny a write to a read-only directory")
	}

	// A second commit, of a file the reviewers never see, so that the head to
	// write down next is not one already written down.
	write(t, root, ".sdlc/notes.md", "a line\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit", "--quiet", "-m", "more")

	r := call(t, command(root, "", `git commit -m "more of the work"`), noEnv)
	if denied(r) {
		t.Fatalf("a commit was refused because the loop could not write down what it watched: %s",
			r.HookSpecificOutput.PermissionDecisionReason)
	}
}

// observationOf is the directory one observation was written into, without this
// test having to know where the format puts it: an observation is named by the
// head it watched.
func observationOf(t *testing.T, root, head string) string {
	t.Helper()
	gitDir, dir := filepath.Join(root, ".git"), ""
	walked := filepath.WalkDir(gitDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == head {
			dir = filepath.Dir(path)
		}
		return err
	})
	if walked != nil {
		t.Fatal(walked)
	}
	if dir == "" {
		t.Fatalf("no observation named %s was written anywhere under %s", head, gitDir)
	}
	return dir
}

// Claude Code kills a hook that outlasts the timeout hooks.json gives it, and
// the freshness check fails open when it cannot measure the tree: it lets the
// commit through with a warning, because git failing here is git failing for
// the commit too. So a budget spent before the first git call is a commit
// allowed with stale reviews and a person's pre-commit approval unchecked.
//
// The parse is the attacker's to make expensive -- US-004 measured 121 KB of
// PowerShell at 29.2 s inside the hook -- and it runs before the tree is
// measured. A deadline taken when the hook starts hands that parse the
// freshness check's budget; one taken at the first git call does not.
//
// The clock is handed in because the only honest way to test a deadline is to
// move time, not to wait for it.
func TestTheBudgetForGitStartsAtTheFirstCallAndNotWhenTheHookBegan(t *testing.T) {
	t.Parallel()
	at := time.Now()
	budget, cancel := gitBudget(t.Context(), func() time.Time { return at })
	defer cancel()

	// A parse that spends more than the whole budget before any git call.
	at = at.Add(treeTimeout + 5*time.Second)

	ctx := budget()
	if err := ctx.Err(); err != nil {
		t.Fatalf("the budget was already spent before the first git call: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("git on the commit path runs with no deadline at all")
	}
	if off := deadline.Sub(at.Add(treeTimeout)); off < -time.Second || off > time.Second {
		t.Errorf("the deadline is %s; want %s, which is %s from the first git call",
			deadline, at.Add(treeTimeout), treeTimeout)
	}

	// One budget for the whole path, not one for each caller: two of them put
	// the worst case at twice treeTimeout, and hooks.json gives the hook 30
	// seconds against a bound of 20.
	at = at.Add(time.Minute)
	if again, _ := budget().Deadline(); !again.Equal(deadline) {
		t.Errorf("the second git call takes a budget of its own, at %s, where the first was at %s",
			again, deadline)
	}

	cancel()
	if ctx.Err() == nil {
		t.Error("cancelling the budget leaves the context running")
	}
}
