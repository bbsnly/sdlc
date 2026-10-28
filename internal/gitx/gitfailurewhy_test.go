package gitx

// US-009: the `why` of a git failure describes the git subcommand that failed.
//
// Two places turn a failed git into a refusal, and both used to fall back to
// one fixed sentence about `git ls-files` whenever git wrote nothing to
// standard error -- as it does when it is not on PATH at all:
//
//	git()        in gitx.go    -- Files, Clean, Resolve, Changes, ...
//	sealedCall() in landing.go -- MergeBase, ModeAt, Uncommitted, CheckedOut, ...
//
// Each is held here with its own subcommands, and more than one, so that a
// fix at one site only, or a fixed sentence for another subcommand, fails.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// fgHash is a well-formed full commit hash for the calls that take one. Git is
// never reached with it in the tests below, or fails before it matters.
const fgHash = "0123456789abcdef0123456789abcdef01234567"

// fgSilentGit puts a git on PATH that exits with status 3 and writes nothing
// to standard error, which is what an empty-stderr failure looks like when git
// is there but says nothing.
func fgSilentGit(t *testing.T) {
	t.Helper()
	fgFakeGit(t, "#!/bin/sh\nexit 3\n")
}

// fgFakeGit puts a git on PATH that is the script.
func fgFakeGit(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script on PATH is not a command here")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// fgCall is one public function that runs git, and the subcommand it runs.
type fgCall struct {
	name string
	sub  string
	call func(ctx context.Context, dir string) error
}

// fgCalls are the callers of git() in gitx.go and of sealedCall() in
// landing.go, with their subcommands. Each subcommand is one of git's own
// words, so a refusal that names it can be told from one that names another.
func fgCalls() []fgCall {
	return []fgCall{
		{"Files", "ls-files", func(ctx context.Context, d string) error { _, err := Files(ctx, d); return err }},
		{"Clean", "status", func(ctx context.Context, d string) error { _, err := Clean(ctx, d); return err }},
		{"Resolve", "rev-parse", func(ctx context.Context, d string) error { _, err := Resolve(ctx, d, "HEAD"); return err }},
		{"CommitsBetween", "rev-list", func(ctx context.Context, d string) error {
			_, err := CommitsBetween(ctx, d, "HEAD~1", "HEAD")
			return err
		}},
		{"MergeBase", "merge-base", func(ctx context.Context, d string) error {
			_, err := MergeBase(ctx, d, fgHash, fgHash)
			return err
		}},
		{"ModeAt", "ls-tree", func(ctx context.Context, d string) error {
			_, err := ModeAt(ctx, d, fgHash, "go.mod")
			return err
		}},
		{"Uncommitted", "status", func(ctx context.Context, d string) error {
			_, err := Uncommitted(ctx, d, true)
			return err
		}},
		{"CheckedOut", "worktree", func(ctx context.Context, d string) error {
			_, err := CheckedOut(ctx, d, "main")
			return err
		}},
	}
}

// fgRefusal is the sdlc error err carries, which must be git failing to
// finish.
func fgRefusal(t *testing.T, name string, err error) *sdlcerr.Error {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("%s: %v carries no sdlc error", name, err)
	}
	if e.Code != sdlcerr.RepositoryUnreadable {
		t.Fatalf("%s: code = %s, want %s", name, e.Code, sdlcerr.RepositoryUnreadable)
	}
	return e
}

// fgWhyNamesOnly holds a failure's `why` to the subcommand its `what` names:
// that one is in it, and `ls-files` is not unless that is what ran.
func fgWhyNamesOnly(t *testing.T, c fgCall, e *sdlcerr.Error) {
	t.Helper()
	if want := "git " + c.sub + " did not finish"; e.What != want {
		t.Errorf("%s: what = %q, want %q", c.name, e.What, want)
	}
	if !strings.Contains(e.Why, "git "+c.sub) {
		t.Errorf("%s: why = %q, want it to name \"git %s\", the subcommand that ran", c.name, e.Why, c.sub)
	}
	if c.sub != "ls-files" && strings.Contains(e.Why, "ls-files") {
		t.Errorf("%s: why = %q, but git %s ran and ls-files did not", c.name, e.Why, c.sub)
	}
}

// ---------------------------------------------------------------- AC-1

func TestAC1GitNotOnPathIsSaidAsTheSubcommandThatDidNotFinish(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	for _, c := range fgCalls() {
		t.Run(c.name, func(t *testing.T) {
			err := c.call(t.Context(), dir)
			if err == nil {
				t.Fatalf("%s succeeded with git not on PATH", c.name)
			}
			fgWhyNamesOnly(t, c, fgRefusal(t, c.name, err))
		})
	}
}

func TestAC1GitThatFailsWithoutAWordIsSaidAsTheSubcommandThatDidNotFinish(t *testing.T) {
	fgSilentGit(t)
	dir := t.TempDir()
	for _, c := range fgCalls() {
		t.Run(c.name, func(t *testing.T) {
			err := c.call(t.Context(), dir)
			if err == nil {
				t.Fatalf("%s succeeded though git failed", c.name)
			}
			fgWhyNamesOnly(t, c, fgRefusal(t, c.name, err))
		})
	}
}

// The two sites have to agree with each other and not only with themselves: a
// subcommand run through git() and one run through sealedCall() are told apart
// by name in the same words.
func TestAC1EachSitePutsItsOwnSubcommandInTheReason(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	why := map[string]string{}
	for _, c := range fgCalls() {
		why[c.name] = fgRefusal(t, c.name, c.call(t.Context(), dir)).Why
	}
	if why["Clean"] == why["Resolve"] {
		t.Errorf("git() gave the same reason for status and rev-parse: %q", why["Clean"])
	}
	if why["ModeAt"] == why["CheckedOut"] {
		t.Errorf("sealedCall() gave the same reason for ls-tree and worktree: %q", why["ModeAt"])
	}
	if why["Clean"] == why["Uncommitted"] {
		if why["Uncommitted"] == why["ModeAt"] {
			t.Errorf("the reason is one sentence for every subcommand: %q", why["Clean"])
		}
	}
}

// ---------------------------------------------------------------- AC-2 (guards: these pass today)

func TestAC2WhenGitWritesToStderrTheReasonIsWhatItSaidOnItsFirstLine(t *testing.T) {
	fgFakeGit(t, "#!/bin/sh\nprintf 'fatal: the first line\\nand a second line\\n' >&2\nexit 3\n")
	dir := t.TempDir()
	for _, c := range fgCalls() {
		t.Run(c.name, func(t *testing.T) {
			err := c.call(t.Context(), dir)
			if err == nil {
				t.Fatalf("%s succeeded though git failed", c.name)
			}
			e := fgRefusal(t, c.name, err)
			if want := "git said: fatal: the first line"; e.Why != want {
				t.Errorf("%s: why = %q, want %q", c.name, e.Why, want)
			}
			if want := "git " + c.sub + " did not finish"; e.What != want {
				t.Errorf("%s: what = %q, want %q", c.name, e.What, want)
			}
		})
	}
}

func TestAC2WhenRealGitWritesToStderrTheReasonQuotesIt(t *testing.T) {
	t.Parallel()
	_, err := Files(t.Context(), t.TempDir())
	e := fgRefusal(t, "Files outside a repository", err)
	if !strings.HasPrefix(e.Why, "git said: ") || strings.Contains(e.Why, "\n") || len(e.Why) == len("git said: ") {
		t.Errorf("why = %q, want git said: followed by one line of what git wrote", e.Why)
	}
	if strings.Contains(e.Why, "did not finish") {
		t.Errorf("why = %q, which is the sentence for git saying nothing", e.Why)
	}
}
