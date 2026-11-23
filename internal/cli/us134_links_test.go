package cli

// US-134, the rows the second security design review asks for:
//
//   - F-10: the gate's and the freeze's output files sit at fixed names in the
//     story's directory, where the main conversation may write. A link planted
//     at one of those names would have sdlc write the commands' output through
//     it, into .sdlc/state/session or the record, which the hook refuses the
//     model's own write to. So whatever sits at the name, its target is left as
//     it was, and the name ends as a regular file holding this run's output, or
//     the run is refused. The story's .gitignore is read only where it is one
//     regular file, and is never written through a link.
//   - F-12: the gate's before-and-after check of the tree never follows a link.
//     A link is compared by its own text, as git stores it; only a regular file
//     is read, and opened so that it cannot block; a change of type is a change.
//
// A row that could hang on a named pipe waits a bounded time and then fails
// with a message, after opening the pipe itself so that whatever was blocked on
// it goes on. The helpers carry an rl prefix, for run logs and links.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// rlPatience is how long a run that should not block is given before the row
// fails: far longer than any of these runs takes, and not unbounded.
const rlPatience = 90 * time.Second

// rlWithin runs sdlc in dir and fails, rather than waits for ever, where it has
// not finished within rlPatience. Every pipe in pipes is then opened for reading
// and writing, which lets an open or a read blocked on it go on.
func rlWithin(t *testing.T, what string, pipes []string, dir string, args ...string) result {
	t.Helper()
	done := make(chan result, 1)
	go func() { done <- wtRun(t, dir, ldSession, "", args...) }()
	select {
	case r := <-done:
		return r
	case <-time.After(rlPatience):
		for _, p := range pipes {
			if f, err := os.OpenFile(p, os.O_RDWR, 0); err == nil {
				_ = f.Close()
			}
		}
		t.Fatalf("%s: sdlc %s had not finished after %s: it blocked on a named pipe", what, strings.Join(args, " "), rlPatience)
		return result{}
	}
}

// rlFifo makes a named pipe at path, or skips where the platform has none.
func rlFifo(t *testing.T, path string) {
	t.Helper()
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("this platform has no mkfifo, so no named pipe to plant")
	}
	if out, err := exec.CommandContext(t.Context(), mkfifo, path).CombinedOutput(); err != nil {
		t.Skipf("this platform makes no named pipe: %v %s", err, out)
	}
}

// rlSymlink makes a symbolic link, or skips where the platform or the user may
// not make one.
func rlSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("this platform makes no symbolic link here: %v", err)
	}
}

// rlTarget is a file in the main checkout's loop state, standing for
// .sdlc/state/session, with bytes of its own, and those bytes.
func rlTarget(t *testing.T, main string) (path, body string) {
	t.Helper()
	path = filepath.Join(main, ".sdlc", "state", "rl-session-stand-in")
	body = "rl-a-session-id-nobody-may-overwrite\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, body
}

// rlPlant puts kind at path: a symbolic link to target, a hard link to it, or a
// named pipe, which is planted beside target and not linked to it.
func rlPlant(t *testing.T, kind, target, path string) {
	t.Helper()
	switch kind {
	case "symlink":
		rlSymlink(t, target, path)
	case "hard link":
		if err := os.Link(target, path); err != nil {
			t.Skipf("this platform makes no hard link here: %v", err)
		}
	case "named pipe":
		rlFifo(t, path)
	default:
		t.Fatalf("no such kind %q", kind)
	}
}

// rlLeftAlone holds a planted log to F-10: the target's bytes are as they were;
// and where the run went ahead, the name is a regular file of its own holding
// marker, this run's output; where it was refused, the refusal names the file.
func rlLeftAlone(t *testing.T, what string, r result, target, was, name, marker string) {
	t.Helper()
	if got := wtRead(t, target); got != was {
		t.Errorf("%s: the file the planted %s pointed at now holds %q, want %q as it was", what, filepath.Base(name), got, was)
	}
	if r.code != 0 {
		if !strings.Contains(r.stderr, filepath.Base(name)) {
			t.Errorf("%s: the run was refused, and the refusal does not name %s:\n%s", what, filepath.Base(name), r.stderr)
		}
		return
	}
	info, err := os.Lstat(name)
	if err != nil {
		t.Fatalf("%s: after the run there is nothing at %s: %v", what, name, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s: after the run %s is %v, not a regular file holding the run's output", what, filepath.Base(name), info.Mode().Type())
	}
	if targetInfo, err := os.Lstat(target); err == nil && os.SameFile(info, targetInfo) {
		t.Errorf("%s: %s is still a second name of the file it was linked to", what, filepath.Base(name))
	}
	if body := wtRead(t, name); !strings.Contains(body, marker) {
		t.Errorf("%s: %s does not hold this run's output, %q:\n%s", what, filepath.Base(name), marker, body)
	}
}

// ---------------------------------------------------------------- F-10, AC-2 and AC-13

// AC-2, security review F-10: whatever sits at implementation-run.log, the
// gate's run neither writes through it nor blocks on it.
func TestAC2TheGatesOutputFileIsNeverWrittenThroughALinkPlantedAtItsName(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "hard link", "named pipe"} {
		t.Run("AC-2 a "+kind, func(t *testing.T) {
			t.Parallel()
			main, wt := rcAtTheGate(t, map[string]string{"test": "echo rl-the-gate-ran; exit 0"})
			target, was := rlTarget(t, main)
			name := filepath.Join(main, ".sdlc", "stories", "US-001", "implementation-run.log")
			rlPlant(t, kind, target, name)

			r := rlWithin(t, "AC-2 a "+kind, []string{name}, wt, "gate", "implementation", "pass", "--note", "the work is done")
			if r.code == 0 && rcGateEntry(t, main)["tree_hash"] == nil {
				t.Fatalf("AC-2 a %s: the gate passed with no run recorded: %v", kind, rcGateEntry(t, main))
			}
			rlLeftAlone(t, "AC-2 a "+kind, r, target, was, name, "rl-the-gate-ran")
		})
	}
}

// AC-13, security review F-10: the same for freeze-run.log and the freeze's
// narrow run.
func TestAC13TheFreezesOutputFileIsNeverWrittenThroughALinkPlantedAtItsName(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "hard link", "named pipe"} {
		t.Run("AC-13 a "+kind, func(t *testing.T) {
			root := rcAtTheFreeze(t, nil, map[string]string{"test_files": "echo rl-the-new-test-fails; exit 1"})
			target, was := rlTarget(t, root)
			name := filepath.Join(root, ".sdlc", "stories", "US-001", "freeze-run.log")
			rlPlant(t, kind, target, name)
			writeFile(t, root, "internal/invoice_test.go", fcTest)

			r := rlWithin(t, "AC-13 a "+kind, []string{name}, root, "freeze")
			if r.code == 0 && rcRecordAt(t, root)["freeze_run"] == nil {
				t.Fatalf("AC-13 a %s: the freeze was taken with no run recorded", kind)
			}
			rlLeftAlone(t, "AC-13 a "+kind, r, target, was, name, "rl-the-new-test-fails")
		})
	}
}

// AC-13, security review F-10: a story .gitignore that is a link is taken as
// absent and replaced, never written through: the file it points at is left as
// it was, and the name ends as a regular file listing both output names.
func TestAC13AStoryGitignoreThatIsALinkIsReplacedAndNeverWrittenThrough(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "hard link"} {
		t.Run("AC-13 a "+kind, func(t *testing.T) {
			root := rcAtTheFreeze(t, nil, map[string]string{"test_files": "echo rl-the-new-test-fails; exit 1"})
			target, was := rlTarget(t, root)
			rlPlant(t, kind, target, rsIgnoreFile(root))
			writeFile(t, root, "internal/invoice_test.go", fcTest)

			r := rlWithin(t, "AC-13 a "+kind, nil, root, "freeze")
			if got := wtRead(t, target); got != was {
				t.Errorf("AC-13 a %s: the file the story's .gitignore pointed at now holds %q, want %q as it was", kind, got, was)
			}
			if r.code != 0 {
				if !strings.Contains(r.stderr, ".gitignore") {
					t.Errorf("AC-13 a %s: the freeze was refused, and the refusal does not name the .gitignore:\n%s", kind, r.stderr)
				}
				return
			}
			info, err := os.Lstat(rsIgnoreFile(root))
			if err != nil {
				t.Fatalf("AC-13 a %s: after the freeze there is no story .gitignore: %v", kind, err)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("AC-13 a %s: after the freeze the story's .gitignore is %v, not a regular file", kind, info.Mode().Type())
			}
			if targetInfo, err := os.Lstat(target); err == nil && os.SameFile(info, targetInfo) {
				t.Errorf("AC-13 a %s: the story's .gitignore is still a second name of the file it was linked to", kind)
			}
			body := wtRead(t, rsIgnoreFile(root))
			for _, n := range rsOutputNames {
				if !strings.Contains(body, n) {
					t.Errorf("AC-13 a %s: the story's .gitignore does not list %s:\n%s", kind, n, body)
				}
			}
		})
	}
}

// ---------------------------------------------------------------- F-12, AC-3

// rlGateRan holds that the gate ran commands.test once, passed, and recorded
// the tree it hashed before the run.
func rlGateRan(t *testing.T, what string, spy rcSpy, r result, main, want string) {
	t.Helper()
	if got := spy.count(t, "test"); got != 1 {
		t.Fatalf("%s: commands.test ran %d times, want once:\n%s", what, got, r.stderr)
	}
	if r.code != 0 {
		t.Fatalf("%s: the gate was refused, though nothing in the tree changed:\n%s", what, r.stderr)
	}
	if got := rcGateEntry(t, main)["tree_hash"]; got != want {
		t.Errorf("%s: the gate recorded tree hash %v, want %s", what, got, want)
	}
}

// AC-3, security review F-12: a link in the tree to a named pipe is compared by
// its own text, as git stores it, and never followed, so the gate's check of
// the tree neither blocks nor refuses; tracked or not, alike. Tracked is in the
// index: a commit in the worktree would move HEAD, which the gate refuses. The pipe is
// outside the repository, and nothing writes to it.
func TestAC3ALinkInTheTreeToANamedPipeDoesNotHangTheGate(t *testing.T) {
	t.Parallel()
	for _, tracked := range []bool{false, true} {
		what := "AC-3 an untracked link to a named pipe"
		if tracked {
			what = "AC-3 a tracked link to a named pipe"
		}
		t.Run(what, func(t *testing.T) {
			t.Parallel()
			spy := newRCSpy(t)
			main, wt := rcAtTheGate(t, map[string]string{"test": spy.reports("test", "exit 0")})
			pipe := filepath.Join(wtTempDir(t), "pipe")
			rlFifo(t, pipe)
			rlSymlink(t, pipe, filepath.Join(wt, "pipe-link"))
			if tracked {
				wtGit(t, wt, "add", "--", "pipe-link")
			}
			want := rcSubject(t, wt)

			r := rlWithin(t, what, []string{pipe}, wt, "gate", "implementation", "pass", "--note", "the work is done")
			rlGateRan(t, what, spy, r, main, want)
		})
	}
}

// AC-3, security review F-12: a link in the tree to a file outside the
// repository is not read. The command rewrites the file outside; the link's own
// text does not change, so the gate is not refused.
func TestAC3ALinkInTheTreeToAFileOutsideTheRepositoryIsNotRead(t *testing.T) {
	t.Parallel()
	spy := newRCSpy(t)
	outside := filepath.Join(wtTempDir(t), "outside.txt")
	if err := os.WriteFile(outside, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main, wt := rcAtTheGate(t, map[string]string{"test": spy.reports("test", "printf 'after\\n' > "+suSh(outside))})
	rlSymlink(t, outside, filepath.Join(wt, "outside-link"))
	want := rcSubject(t, wt)

	r := rlWithin(t, "AC-3 a link to a file outside", nil, wt, "gate", "implementation", "pass", "--note", "the work is done")
	rlGateRan(t, "AC-3 a link to a file outside", spy, r, main, want)
	if got := wtRead(t, outside); got != "after\n" {
		t.Fatalf("AC-3 the command did not rewrite the file outside: %q", got)
	}
}

// AC-3, security review F-12: a change of a listed path's type, or of a link's
// own text, is a change, though a reader that follows links would find the same
// bytes after as before. Each has the gate refused, naming the path, with no
// block on a pipe.
func TestAC3AFileSwappedForAPipeOrALinkOrALinkRetargetedHasTheGateRefusedNamingIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, command, file string
		// pipe is the path of a pipe the command makes, for the bounded wait.
		pipe string
		// link is a link added to the index before the gate, and its target:
		// a commit in the worktree would move HEAD, which the gate refuses.
		link, linkTo string
	}{
		{name: "AC-3 a tracked file swapped for a named pipe", command: `rm README.md && mkfifo README.md`, file: "README.md", pipe: "README.md"},
		// readme.copy holds README.md's bytes, so a reader that follows the
		// link finds what it found before.
		{name: "AC-3 a tracked file swapped for a link to the same bytes", command: `cp README.md readme.copy && rm README.md && ln -s readme.copy README.md`, file: "README.md"},
		// same.txt and also.txt hold the same bytes: only the link's text moves.
		{name: "AC-3 a tracked link retargeted", command: `ln -sfn also.txt current`, file: "current", link: "current", linkTo: "same.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.pipe != "" {
				if _, err := exec.LookPath("mkfifo"); err != nil {
					t.Skip("this platform has no mkfifo, so no named pipe to swap in")
				}
			}
			spy := newRCSpy(t)
			main, wt := rcAtTheGate(t, map[string]string{"test": spy.reports("test", tc.command)})
			if tc.link != "" {
				writeFile(t, wt, "same.txt", "the same bytes\n")
				writeFile(t, wt, "also.txt", "the same bytes\n")
				rlSymlink(t, tc.linkTo, filepath.Join(wt, tc.link))
				wtGit(t, wt, "add", "--", "same.txt", "also.txt", tc.link)
			}
			before := rcGateEntry(t, main)
			var pipes []string
			if tc.pipe != "" {
				pipes = []string{filepath.Join(wt, tc.pipe)}
			}

			r := rlWithin(t, tc.name, pipes, wt, "gate", "implementation", "pass", "--note", "the work is done")
			if got := spy.count(t, "test"); got != 1 {
				t.Fatalf("%s: commands.test ran %d times, want once", tc.name, got)
			}
			if r.code == 0 {
				t.Fatalf("%s: the gate passed though commands.test changed %s:\n%s", tc.name, tc.file, r.stdout)
			}
			if !strings.Contains(r.stderr, rcCode) {
				t.Errorf("%s: the refusal does not carry %s:\n%s", tc.name, rcCode, r.stderr)
			}
			suSays(t, r.stderr, tc.file)
			entry := rcGateEntry(t, main)
			if entry["status"] == string(model.GatePass) || entry["status"] != before["status"] {
				t.Errorf("%s: the gate went from %v to %v: nothing is to be recorded", tc.name, before["status"], entry["status"])
			}
		})
	}
}
