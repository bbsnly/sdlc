package main

// The sandbox program, from the inside (US-067 AC-1 and AC-5). test/e2e builds
// it and runs it as `./task sandbox` does; these call it in this process, which
// is what the coverage profile counts.
//
// What they bind is one function and what a person sees of it:
//
//	func run(args []string, stdout, stderr io.Writer) int
//
//	run({"create"})          exit 0, nothing on stderr, and on stdout lines of
//	                         a word and a path: sandbox, repository, sdlc
//	run({"remove", dir})     exit 0, nothing on stderr, and dir is gone
//	a refusal or a failure   exit 1, nothing on stdout, stderr "sandbox: ..."
//	words it cannot read     exit 2, nothing on stdout, stderr "sandbox: ..."
//	                         naming create and remove
//	-C dir, before the word  the checkout to build sdlc from; "." without it
//
// The sandbox goes directly under os.TempDir(), named sdlc-sandbox-*. The
// program finds go and git on the PATH it is run with, and reads the home, the
// cache and the temp directory from its environment as any program does; the
// tests point all of them at directories of their own, so nothing here is
// written outside t.TempDir() but go's build cache.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The helpers here carry names the program is unlikely to want for itself
// (mustCreate rather than create, wantedPrefix rather than prefix): they share
// package main with it, and these files cannot change once they are frozen.

// thisCheckout is this repository, three directories up.
const thisCheckout = "../../.."

// wantedPrefix is what the name of every sandbox directory starts with.
const wantedPrefix = "sdlc-sandbox-"

// testPlace is the temp directory and the home a test runs the program with.
type testPlace struct{ temp, home string }

func realPath(t *testing.T, dir string) string {
	t.Helper()
	actual, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return actual
}

// isolated points the home, the cache and the temp directory of this process at
// directories of the test's own, for as long as the test runs. Go's caches stay
// where they are and are named outright, since the home they are found by is
// about to change; GOPROXY is off, so a build that wanted the network fails.
func isolated(t *testing.T) testPlace {
	t.Helper()
	for _, name := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		out, err := exec.CommandContext(t.Context(), "go", "env", name).Output()
		if err != nil {
			t.Fatalf("go env %s: %v", name, err)
		}
		t.Setenv(name, strings.TrimSpace(string(out)))
	}
	t.Setenv("GOPROXY", "off")
	p := testPlace{temp: realPath(t, t.TempDir()), home: realPath(t, t.TempDir())}
	for name, value := range map[string]string{
		"HOME":            p.home,
		"USERPROFILE":     p.home,
		"XDG_CACHE_HOME":  filepath.Join(p.home, ".cache"),
		"XDG_CONFIG_HOME": filepath.Join(p.home, ".config"),
		"LOCALAPPDATA":    filepath.Join(p.home, "AppData", "Local"),
		"APPDATA":         filepath.Join(p.home, "AppData", "Roaming"),
		"TMPDIR":          p.temp,
		"TMP":             p.temp,
		"TEMP":            p.temp,
	} {
		t.Setenv(name, value)
	}
	return p
}

// runWith runs the program in this process.
func runWith(args ...string) (stdout, stderr string, code int) {
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return out.String(), errb.String(), code
}

// printedSandbox is what create printed.
type printedSandbox struct{ dir, repo, sdlc, printed string }

func readPrinted(t *testing.T, stdout string) printedSandbox {
	t.Helper()
	paths := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(stdout, "\n"), "\n") {
		word, rest, ok := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if !ok {
			continue
		}
		if _, twice := paths[word]; twice {
			t.Fatalf("create printed %q twice:\n%s", word, stdout)
		}
		paths[word] = strings.TrimSpace(rest)
	}
	m := printedSandbox{dir: paths["sandbox"], repo: paths["repository"], sdlc: paths["sdlc"], printed: stdout}
	for word, path := range map[string]string{"sandbox": m.dir, "repository": m.repo, "sdlc": m.sdlc} {
		if !filepath.IsAbs(path) {
			t.Fatalf("create printed no absolute path after %q:\n%s", word, stdout)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("create printed %s for %q, and it is not there: %v", path, word, err)
		}
	}
	return m
}

// mustCreate makes a sandbox from this checkout, which has to work.
func mustCreate(t *testing.T) printedSandbox {
	t.Helper()
	stdout, stderr, code := runWith("-C", thisCheckout, "create")
	if code != 0 || stderr != "" {
		t.Fatalf("create exited %d:\nstdout %q\nstderr %q", code, stdout, stderr)
	}
	return readPrinted(t, stdout)
}

func entriesOf(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range list {
		out = append(out, e.Name())
	}
	return out
}

func present(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func keptFile(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(file, []byte("a person's file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

// gitAt runs the machine's git in dir, reading no configuration but the
// repository's own.
func gitAt(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ---------------------------------------------------------------- create

func TestAC1CreateMakesARepositoryWithSdlcInitialisedAndOneReadyStoryAndPrintsWhereItIs(t *testing.T) {
	p := isolated(t)
	m := mustCreate(t)

	if filepath.Dir(m.dir) != p.temp || !strings.HasPrefix(filepath.Base(m.dir), wantedPrefix) {
		t.Errorf("the sandbox is %s, want a directory named %s* directly under %s", m.dir, wantedPrefix, p.temp)
	}
	for _, path := range []string{m.dir, m.repo, m.sdlc} {
		if realPath(t, path) != path {
			t.Errorf("create printed %s, which is not that path with its links resolved", path)
		}
		if path != m.dir && !strings.HasPrefix(path, m.dir+string(filepath.Separator)) {
			t.Errorf("%s is not inside the sandbox %s", path, m.dir)
		}
	}
	if strings.HasPrefix(m.sdlc, m.repo+string(filepath.Separator)) {
		t.Errorf("the binary %s is inside the repository, where a story would commit it", m.sdlc)
	}
	if !strings.HasSuffix(m.printed, "\n") {
		t.Errorf("create's last line has no newline: %q", m.printed)
	}

	backlog, err := os.ReadFile(filepath.Join(m.repo, "user_stories.json"))
	if err != nil {
		t.Fatalf("the sandbox has no backlog: %v", err)
	}
	if strings.Count(string(backlog), `"id": "US-`) != 1 || !strings.Contains(string(backlog), `"status": "ready"`) {
		t.Errorf("the backlog does not hold one story, ready:\n%s", backlog)
	}
	if !present(filepath.Join(m.repo, ".sdlc", "config.json")) {
		t.Error("there is no .sdlc/config.json: sdlc is not initialised")
	}
	if dirty, err := gitAt(t, m.repo, "status", "--porcelain", "--untracked-files=all"); err != nil || dirty != "" {
		t.Errorf("the sandbox is not a repository with everything committed (%v):\n%s", err, dirty)
	}
	if top, err := gitAt(t, m.repo, "rev-parse", "--show-toplevel"); err != nil || realPath(t, top) != m.repo {
		t.Errorf("git takes the repository for %s (%v), want %s", top, err, m.repo)
	}
	// AC-5: no remote, and no transport git would use to reach one.
	if remotes, _ := gitAt(t, m.repo, "remote"); remotes != "" {
		t.Errorf("the sandbox has remotes: %s", remotes)
	}
	if allow, err := gitAt(t, m.repo, "config", "--local", "--get", "protocol.allow"); err != nil || allow != "never" {
		t.Errorf("protocol.allow in the repository's own configuration is %q (%v), want never", allow, err)
	}

	// The binary is sdlc, and it reads the sandbox as a project with one story
	// to start.
	status := exec.CommandContext(t.Context(), m.sdlc, "status")
	status.Dir = m.repo
	// With a home of its own in the sandbox, so that what is found in the
	// test's below was written by create and not by this.
	own := filepath.Join(m.dir, "home")
	status.Env = append(os.Environ(), "HOME="+own, "USERPROFILE="+own,
		"XDG_CACHE_HOME="+filepath.Join(own, ".cache"), "LOCALAPPDATA="+filepath.Join(own, "AppData", "Local"))
	out, err := status.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "backlog  1 ready") {
		t.Errorf("sdlc status in the sandbox (%v):\n%s", err, out)
	}

	// AC-1 and AC-5: nothing else was touched.
	if got := entriesOf(t, p.temp); !slices.Equal(got, []string{filepath.Base(m.dir)}) {
		t.Errorf("the temp directory holds %v, want the sandbox alone", got)
	}
	if got := entriesOf(t, p.home); len(got) != 0 {
		t.Errorf("the home directory was written to: %v", got)
	}
}

func TestAC1RemoveTakesTheSandboxAwayAndLeavesAnotherOneAlone(t *testing.T) {
	p := isolated(t)
	first, second := mustCreate(t), mustCreate(t)
	if first.dir == second.dir {
		t.Fatalf("two creates printed the same directory: %s", first.dir)
	}

	stdout, stderr, code := runWith("-C", thisCheckout, "remove", first.dir)
	if code != 0 || stderr != "" {
		t.Fatalf("remove exited %d:\nstdout %q\nstderr %q", code, stdout, stderr)
	}
	if present(first.dir) {
		t.Errorf("%s is still there", first.dir)
	}
	if got := entriesOf(t, p.temp); !slices.Equal(got, []string{filepath.Base(second.dir)}) {
		t.Errorf("after removing one of two sandboxes the temp directory holds %v, want the other alone", got)
	}
	if !present(filepath.Join(second.repo, "user_stories.json")) {
		t.Errorf("removing %s damaged %s", first.dir, second.dir)
	}

	// A second remove of the same directory has nothing to take.
	stdout, stderr, code = runWith("-C", thisCheckout, "remove", first.dir)
	wantRefused(t, "a sandbox already removed", stdout, stderr, code)

	// And from the checkout, with no -C at all, as `./task sandbox-remove` runs it.
	t.Chdir(thisCheckout)
	if stdout, stderr, code = runWith("remove", second.dir); code != 0 || stderr != "" || present(second.dir) {
		t.Errorf("remove without -C exited %d and left %v:\nstdout %q\nstderr %q", code, present(second.dir), stdout, stderr)
	}
	if got := entriesOf(t, p.home); len(got) != 0 {
		t.Errorf("the home directory was written to: %v", got)
	}
}

// `./task sandbox` runs the program in the checkout with no -C.
func TestAC1CreateWithoutCBuildsFromTheDirectoryItIsRunIn(t *testing.T) {
	p := isolated(t)
	t.Chdir(thisCheckout)
	stdout, stderr, code := runWith("create")
	if code != 0 || stderr != "" {
		t.Fatalf("create exited %d:\nstdout %q\nstderr %q", code, stdout, stderr)
	}
	m := readPrinted(t, stdout)
	if filepath.Dir(m.dir) != p.temp {
		t.Errorf("the sandbox is %s, want it under %s", m.dir, p.temp)
	}
}

// T-3 and T-7: the program is run by a session, with whatever that session has
// set. Git's own variables would send the repository somewhere else, the
// loop's would turn enforcement off or record the developer's session, and go
// flags would build something other than what is in the checkout.
func TestAC1CreateIsNotMovedByGitsVariablesTheLoopsOrGoFlags(t *testing.T) {
	isolated(t)
	decoy := realPath(t, t.TempDir())
	for name, value := range map[string]string{
		"GIT_DIR":                filepath.Join(decoy, "git"),
		"GIT_WORK_TREE":          decoy,
		"GIT_INDEX_FILE":         filepath.Join(decoy, "index"),
		"SDLC_ENFORCE":           "0",
		"SDLC_BIN":               filepath.Join(decoy, "no-such-sdlc"),
		"CLAUDE_PROJECT_DIR":     decoy,
		"CLAUDE_CODE_SESSION_ID": "the-developers-own-session",
		// With -n go prints what it would build and builds nothing.
		"GOFLAGS": "-n",
	} {
		t.Setenv(name, value)
	}
	m := mustCreate(t)

	if got := entriesOf(t, decoy); len(got) != 0 {
		t.Errorf("the program wrote %v where the session's own variables pointed", got)
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	if dirty, err := gitAt(t, m.repo, "status", "--porcelain", "--untracked-files=all"); err != nil || dirty != "" {
		t.Errorf("the sandbox is not a repository with everything committed (%v):\n%s", err, dirty)
	}
	if present(filepath.Join(m.repo, ".sdlc", "state", "session")) {
		t.Error("the sandbox records a session, and nobody has started a story in it")
	}
	version := exec.CommandContext(t.Context(), m.sdlc, "version")
	if out, err := version.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) == "" {
		t.Errorf("the sdlc the program built does not run (%v):\n%s", err, out)
	}
}

// ---------------------------------------------------------------- remove refuses

// wantRefused holds a remove to exit 1, nothing on stdout, and a refusal on stderr
// in the program's name that names the command a removable directory comes
// from.
func wantRefused(t *testing.T, what, stdout, stderr string, code int) {
	t.Helper()
	if code != 1 || stdout != "" {
		t.Errorf("%s: remove exited %d and printed %q, want exit 1 and nothing on stdout\nstderr %q", what, code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "sandbox: ") || !strings.Contains(stderr, "./task sandbox") {
		t.Errorf("%s: the refusal does not say what to do, in the program's own name and naming ./task sandbox:\n%q", what, stderr)
	}
}

// T-5. Remove deletes a tree it is given the path of, and whoever gives it may
// be a model with nobody watching. It takes a directory create made, proved by
// what create wrote in it and by where it is, and nothing else.
//
// The cases run in order, and the ones at the end name directories that are
// not the test's own: this checkout, and the root of the file system. They are
// asked only once every case before them was refused and left its directory
// whole, so a program that removed whatever it was given never gets that far.
func TestAC1RemoveRefusesEveryDirectoryCreateDidNotMake(t *testing.T) {
	p := isolated(t)
	m := mustCreate(t)
	backlog := filepath.Join(m.repo, "user_stories.json")

	lookalike := filepath.Join(p.temp, wantedPrefix+"lookalike")
	elsewhere := realPath(t, t.TempDir())
	fakeCheckout := realPath(t, t.TempDir())
	file := filepath.Join(p.temp, wantedPrefix+"a-file")
	if err := os.WriteFile(file, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := map[string]string{
		lookalike:    keptFile(t, lookalike),
		elsewhere:    keptFile(t, elsewhere),
		fakeCheckout: keptFile(t, fakeCheckout),
		p.home:       keptFile(t, p.home),
	}

	type attempt struct {
		name string
		args []string
		kept []string
	}
	ask := func(a attempt) {
		t.Helper()
		stdout, stderr, code := runWith(a.args...)
		for _, k := range append(a.kept, backlog) {
			if !present(k) {
				t.Fatalf("%s: remove deleted %s", a.name, k)
			}
		}
		if code == 0 {
			t.Fatalf("%s: remove exited 0\nstdout %q", a.name, stdout)
		}
		if stdout != "" || !strings.HasPrefix(stderr, "sandbox: ") {
			t.Errorf("%s: stdout %q, stderr %q; want nothing on stdout and a refusal that begins \"sandbox: \"", a.name, stdout, stderr)
		}
	}

	for _, a := range []attempt{
		{"a directory with a sandbox's name and testPlace and nothing create wrote in it",
			[]string{"-C", thisCheckout, "remove", lookalike}, []string{kept[lookalike]}},
		{"a directory that is not under the temp directory",
			[]string{"-C", thisCheckout, "remove", elsewhere}, []string{kept[elsewhere]}},
		{"a file with a sandbox's name",
			[]string{"-C", thisCheckout, "remove", file}, []string{file}},
		{"a path with nothing at it",
			[]string{"-C", thisCheckout, "remove", filepath.Join(p.temp, wantedPrefix+"never-printedSandbox")}, nil},
		{"no path", []string{"-C", thisCheckout, "remove", ""}, nil},
		{"the repository in a sandbox, which is not the sandbox",
			[]string{"-C", thisCheckout, "remove", m.repo}, nil},
		{"the temp directory the sandbox is in",
			[]string{"-C", thisCheckout, "remove", p.temp}, []string{kept[lookalike]}},
		{"the home directory",
			[]string{"-C", thisCheckout, "remove", p.home}, []string{kept[p.home]}},
		{"the checkout named by -C",
			[]string{"-C", fakeCheckout, "remove", fakeCheckout}, []string{kept[fakeCheckout]}},
		{"the directory above the checkout named by -C",
			[]string{"-C", fakeCheckout, "remove", filepath.Dir(fakeCheckout)}, []string{kept[fakeCheckout]}},
	} {
		ask(a)
	}

	// The working directory, as a dot and as the directory above it.
	t.Run("the working directory", func(t *testing.T) {
		inside := filepath.Join(elsewhere, "inside")
		if err := os.Mkdir(inside, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(inside)
		ask(attempt{"a dot", []string{"remove", "."}, []string{kept[elsewhere]}})
		ask(attempt{"two dots", []string{"remove", ".."}, []string{kept[elsewhere]}})
	})

	// A sandbox keeps what create wrote in it wherever it goes, so the place is
	// checked as well: moved out of the temp directory, and renamed inside it.
	t.Run("a sandbox that is no longer where create printedSandbox it", func(t *testing.T) {
		moved := filepath.Join(elsewhere, filepath.Base(m.dir))
		renamed := filepath.Join(p.temp, "renamed")
		for _, to := range []string{moved, renamed} {
			if err := os.Rename(m.dir, to); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, code := runWith("-C", thisCheckout, "remove", to)
			if !present(filepath.Join(to, filepath.Base(m.repo), "user_stories.json")) {
				t.Fatalf("remove deleted %s", to)
			}
			wantRefused(t, to, stdout, stderr, code)
			if err := os.Rename(to, m.dir); err != nil {
				t.Fatal(err)
			}
		}
	})

	// A link in the sandbox's place, to a directory that is not one: the link
	// is resolved before anything is compared, and what it leads to is kept.
	t.Run("a link with a sandbox's name, to a person's directory", func(t *testing.T) {
		link := filepath.Join(p.temp, wantedPrefix+"link")
		if err := os.Symlink(elsewhere, link); err != nil {
			t.Skipf("this platform will not make a link here: %v", err)
		}
		ask(attempt{"a link to a directory outside the temp directory",
			[]string{"-C", thisCheckout, "remove", link}, []string{kept[elsewhere]}})
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
	})

	if t.Failed() {
		t.Fatal("remove did not refuse a directory of the test's own, so it is not asked about this checkout or the root")
	}
	root, err := filepath.Abs(thisCheckout)
	if err != nil {
		t.Fatal(err)
	}
	ask(attempt{"this checkout", []string{"-C", thisCheckout, "remove", root}, []string{filepath.Join(root, "go.mod")}})
	ask(attempt{"the root of the file system",
		[]string{"-C", thisCheckout, "remove", filepath.VolumeName(root) + string(filepath.Separator)}, []string{filepath.Join(root, "go.mod")}})

	// And the sandbox itself, back where create made it, is still removable.
	if stdout, stderr, code := runWith("-C", thisCheckout, "remove", m.dir); code != 0 || stderr != "" || present(m.dir) {
		t.Errorf("remove of the sandbox exited %d and left %v:\nstdout %q\nstderr %q", code, present(m.dir), stdout, stderr)
	}
	if !present(kept[lookalike]) {
		t.Errorf("removing %s took %s with it", m.dir, lookalike)
	}
}

// Windows will not remove the directory a process stands in, and on the others
// a shell left standing in a deleted directory fails in ways nobody connects
// to this. It is a refusal on all three, with the sandbox left whole.
func TestAC1RemoveRefusesASandboxTheWorkingDirectoryIsIn(t *testing.T) {
	isolated(t)
	m := mustCreate(t)
	root, err := filepath.Abs(thisCheckout)
	if err != nil {
		t.Fatal(err)
	}
	for _, standing := range []string{m.repo, m.dir} {
		t.Run(strings.TrimPrefix(standing, filepath.Dir(m.dir)), func(t *testing.T) {
			t.Chdir(standing)
			stdout, stderr, code := runWith("-C", root, "remove", m.dir)
			if !present(filepath.Join(m.repo, "user_stories.json")) {
				t.Fatalf("remove deleted the sandbox the process was standing in")
			}
			wantRefused(t, "standing in "+standing, stdout, stderr, code)
		})
	}
	if stdout, stderr, code := runWith("-C", root, "remove", m.dir); code != 0 || present(m.dir) {
		t.Errorf("remove from outside the sandbox exited %d:\nstdout %q\nstderr %q", code, stdout, stderr)
	}
}

// A removal that stops half-way is a failure to report, not to force and not
// to pass over: the directory is still there, and the person is told.
func TestAC1ARemoveThatCannotFinishSaysSoAndExitsOne(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory's permissions do not stop a file in it being removed on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root removes a file whatever its directory's permissions say")
	}
	isolated(t)
	m := mustCreate(t)
	locked := filepath.Join(m.repo, "locked")
	keptFile(t, locked)
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	stdout, stderr, code := runWith("-C", thisCheckout, "remove", m.dir)
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "sandbox: ") {
		t.Errorf("remove of a sandbox it cannot delete: exit %d, stdout %q, stderr %q; want exit 1 and the reason on stderr", code, stdout, stderr)
	}
	if !present(locked) {
		t.Error("the directory that could not be removed is gone")
	}

	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if stdout, stderr, code = runWith("-C", thisCheckout, "remove", m.dir); code != 0 || present(m.dir) {
		t.Errorf("remove, once it could finish, exited %d:\nstdout %q\nstderr %q", code, stdout, stderr)
	}
}

// ---------------------------------------------------------------- words it cannot read

func TestAC1WordsTheProgramCannotReadAreRefusedWithTheTwoItTakes(t *testing.T) {
	p := isolated(t)
	for _, args := range [][]string{
		nil,
		{"-C", thisCheckout},
		{"destroy"},
		{"create", "extra"},
		{"remove"},
		{"remove", filepath.Join(p.temp, wantedPrefix+"one"), filepath.Join(p.temp, wantedPrefix+"two")},
		{"--no-such-flag", "create"},
		{"-C"},
		// A person-only step is no word of this program (T-1).
		{"approve", "US-001"},
		{"unfreeze"},
		{"ack"},
	} {
		stdout, stderr, code := runWith(args...)
		if code != 2 || stdout != "" {
			t.Errorf("sandbox %q exited %d and printed %q, want exit 2 and nothing on stdout", args, code, stdout)
		}
		if !strings.HasPrefix(stderr, "sandbox: ") || !strings.Contains(stderr, "create") || !strings.Contains(stderr, "remove") {
			t.Errorf("sandbox %q does not say, in its own name, that it takes create and remove:\n%q", args, stderr)
		}
	}
	if got := entriesOf(t, p.temp); len(got) != 0 {
		t.Errorf("words the program could not read left %v in the temp directory", got)
	}
}

// ---------------------------------------------------------------- create fails

// wantFailed holds a create that could not finish to exit 1, nothing on stdout, a
// reason on stderr in the program's name, and nothing left behind.
func wantFailed(t *testing.T, what string, p testPlace, stdout, stderr string, code int) {
	t.Helper()
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "sandbox: ") {
		t.Errorf("%s: create exited %d, stdout %q, stderr %q; want exit 1, nothing on stdout and the reason on stderr", what, code, stdout, stderr)
	}
	if got := entriesOf(t, p.temp); len(got) != 0 {
		t.Errorf("%s: a create that failed left %v in the temp directory", what, got)
	}
	if got := entriesOf(t, p.home); len(got) != 0 {
		t.Errorf("%s: a create that failed wrote %v in the home directory", what, got)
	}
}

func TestAC1CreateFromADirectoryThatIsNotAnSdlcCheckoutFailsAndLeavesNothing(t *testing.T) {
	p := isolated(t)
	empty := realPath(t, t.TempDir())
	other := realPath(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(other, "go.mod"), []byte("module example.com/other\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for what, dir := range map[string]string{
		"an empty directory":             empty,
		"another module":                 other,
		"a directory that is not there":  filepath.Join(empty, "missing"),
		"a directory inside this module": filepath.Join(thisCheckout, "docs"),
	} {
		stdout, stderr, code := runWith("-C", dir, "create")
		wantFailed(t, what, p, stdout, stderr, code)
	}
}

// The temp directory is where a sandbox goes and what remove measures a path
// against, so one that cannot be used stops both, and neither makes do with
// another place.
func TestAC1ATempDirectoryThatCannotBeUsedStopsCreateAndRemove(t *testing.T) {
	p := isolated(t)
	fixtures := realPath(t, t.TempDir())
	lookalike := filepath.Join(fixtures, wantedPrefix+"lookalike")
	kept := keptFile(t, lookalike)

	missing := filepath.Join(fixtures, "missing")
	file := filepath.Join(fixtures, "a-file")
	if err := os.WriteFile(file, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unusable := map[string]string{"one that is not there": missing, "a file": file}

	dangling := filepath.Join(fixtures, "dangling")
	if err := os.Symlink(missing, dangling); err == nil {
		unusable["a link to nothing"] = dangling
	}
	// A directory nothing can be made in is arranged only where the mode bits
	// can arrange it: Windows keeps no write bit for a directory, and root is
	// not held by one anywhere.
	readOnly := ""
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		readOnly = filepath.Join(fixtures, "read-only")
		if err := os.Mkdir(readOnly, 0o555); err != nil {
			t.Fatal(err)
		}
		unusable["a directory nothing can be made in"] = readOnly
	}

	for what, temp := range unusable {
		for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
			t.Setenv(name, temp)
		}
		stdout, stderr, code := runWith("-C", thisCheckout, "create")
		if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "sandbox: ") {
			t.Errorf("create with %s for a temp directory exited %d, stdout %q, stderr %q; want exit 1 and the reason on stderr",
				what, code, stdout, stderr)
		}
		stdout, stderr, code = runWith("-C", thisCheckout, "remove", lookalike)
		if !present(kept) {
			t.Fatalf("remove with %s for a temp directory deleted %s", what, lookalike)
		}
		if code == 0 || stdout != "" || !strings.HasPrefix(stderr, "sandbox: ") {
			t.Errorf("remove with %s for a temp directory exited %d, stdout %q, stderr %q; want a refusal on stderr",
				what, code, stdout, stderr)
		}
	}
	// Nowhere else was used instead.
	for _, dir := range []string{p.temp, p.home} {
		if got := entriesOf(t, dir); len(got) != 0 {
			t.Errorf("with no temp directory to use, %s was written to: %v", dir, got)
		}
	}
	// Only where it was made: entriesOf ends the test on a directory that is
	// not there, which is what failed this on Windows.
	if readOnly != "" {
		if got := entriesOf(t, readOnly); len(got) != 0 {
			t.Errorf("the read-only directory holds %v", got)
		}
	}
}

// A sandbox under another name that is still a sandbox's, in the same place,
// may be taken for the one create made or may not: what create writes to know
// its own is the program's to decide. Either way it is all or nothing.
func TestAC1ASandboxRenamedToAnotherSandboxsNameIsTakenWholeOrLeftWhole(t *testing.T) {
	p := isolated(t)
	m := mustCreate(t)
	renamed := filepath.Join(p.temp, wantedPrefix+"renamed")
	if err := os.Rename(m.dir, renamed); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runWith("-C", thisCheckout, "remove", renamed)
	if code == 0 {
		if present(renamed) || stderr != "" {
			t.Errorf("remove exited 0, left %v and said %q on stderr", present(renamed), stderr)
		}
		return
	}
	wantRefused(t, "a renamed sandbox", stdout, stderr, code)
	if !present(filepath.Join(renamed, filepath.Base(m.repo), "user_stories.json")) {
		t.Errorf("remove refused %s and deleted part of it", renamed)
	}
}

func TestAC1CreateWithoutGoOrGitOnPathFailsAndLeavesNothing(t *testing.T) {
	p := isolated(t)
	t.Setenv("PATH", realPath(t, t.TempDir()))
	stdout, stderr, code := runWith("-C", thisCheckout, "create")
	wantFailed(t, "a PATH with neither go nor git on it", p, stdout, stderr, code)
}

// standIn puts a program of the given name first on PATH that runs the script,
// with $real standing for the machine's own program of that name.
func standIn(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for " + name + " is a POSIX shell script")
	}
	program, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not on PATH: %v", name, err)
	}
	dir := realPath(t, t.TempDir())
	body := "#!/bin/sh\nreal='" + program + "'\n" + script
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestAC1CreateFailsAndLeavesNothingWhenTheBuildFails(t *testing.T) {
	p := isolated(t)
	standIn(t, "go", "echo 'go: the build failed' >&2\nexit 1\n")
	stdout, stderr, code := runWith("-C", thisCheckout, "create")
	wantFailed(t, "a go that fails", p, stdout, stderr, code)
}

func TestAC1CreateFailsAndLeavesNothingWhenGitCannotMakeARepository(t *testing.T) {
	p := isolated(t)
	standIn(t, "git", "for a in \"$@\"; do [ \"$a\" = init ] && { echo 'git: no' >&2; exit 1; }; done\nexec \"$real\" \"$@\"\n")
	stdout, stderr, code := runWith("-C", thisCheckout, "create")
	wantFailed(t, "a git that cannot init", p, stdout, stderr, code)
}

// A create ends one of two ways: a sandbox that is whole, or nothing at all.
// What goes wrong part-way differs with how the program goes about it, so each
// of these breaks one step it may take and holds it to that, without saying
// which step it has to take: a git that fails at one of its commands, and a go
// that builds an sdlc which does not run.
func TestAC1ACreateThatFailsPartWayLeavesNoHalfMadeSandbox(t *testing.T) {
	wholeOrNothing := func(t *testing.T, p testPlace) {
		t.Helper()
		stdout, stderr, code := runWith("-C", thisCheckout, "create")
		if code != 0 {
			wantFailed(t, "a create that could not finish", p, stdout, stderr, code)
			return
		}
		if stderr != "" {
			t.Errorf("create exited 0 with %q on stderr", stderr)
		}
		m := readPrinted(t, stdout)
		if !present(filepath.Join(m.repo, "user_stories.json")) || !present(filepath.Join(m.repo, ".sdlc", "config.json")) {
			t.Errorf("create exited 0 and the sandbox at %s has no backlog or no configuration", m.dir)
		}
		if got := entriesOf(t, p.temp); !slices.Equal(got, []string{filepath.Base(m.dir)}) {
			t.Errorf("the temp directory holds %v, want the sandbox alone", got)
		}
	}

	for _, word := range []string{"config", "add", "commit", "rev-parse", "symbolic-ref", "branch", "status"} {
		t.Run("git "+word+" fails", func(t *testing.T) {
			p := isolated(t)
			standIn(t, "git", "for a in \"$@\"; do [ \"$a\" = "+word+" ] && { echo 'git: no' >&2; exit 1; }; done\nexec \"$real\" \"$@\"\n")
			wholeOrNothing(t, p)
		})
	}

	t.Run("the sdlc that was built does not run", func(t *testing.T) {
		p := isolated(t)
		// The real build, and then the binary it wrote, wherever -o said,
		// replaced with a program that fails.
		standIn(t, "go", "\"$real\" \"$@\" || exit $?\n"+
			"out=''\nwhile [ $# -gt 0 ]; do\n  case \"$1\" in\n    -o) out=\"$2\" ;;\n    -o=*) out=\"${1#-o=}\" ;;\n  esac\n  shift\ndone\n"+
			"if [ -n \"$out\" ] && [ -f \"$out\" ]; then printf '#!/bin/sh\\necho broken >&2\\nexit 3\\n' > \"$out\"; fi\n")
		wholeOrNothing(t, p)
	})
}

// ---------------------------------------------------------------- main

// run is called directly everywhere above. This is the one test that proves
// main hands it os.Args and the real streams, and exits with what it returns.
func TestMainHandsRunItsArgumentsAndExitsWithItsCode(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), os.Args[0], "destroy")
	cmd.Env = append(os.Environ(), sandboxHelperEnv+"=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("sandbox destroy: %v, want exit 2\nstderr %q", err, errb.String())
	}
	wantOut, wantErr, _ := runWith("destroy")
	if out.String() != wantOut || errb.String() != wantErr {
		t.Errorf("main printed %q and %q, run prints %q and %q for the same words", out.String(), errb.String(), wantOut, wantErr)
	}
}
