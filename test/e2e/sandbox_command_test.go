// US-067, the sandbox command itself: what it makes, what it prints, what it
// removes and refuses to, what it leaves alone (AC-1, AC-5), and what
// CONTRIBUTING.md says about it (AC-6). sandbox_test.go has the helpers.

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// entries is every name in dir, hidden ones too.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

// checkoutStatus is what git reports of this checkout: every file changed,
// added or untracked in it.
func checkoutStatus(t *testing.T) string {
	t.Helper()
	return gitOut(t, moduleRoot(t), "status", "--porcelain", "--untracked-files=all")
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// storiesOf is the id and status of each story in the backlog of repo.
func storiesOf(t *testing.T, repo string) map[string]string {
	t.Helper()
	return statuses(t, filepath.Join(repo, "user_stories.json"))
}

// titleOf is the title of the one story `sdlc init` writes.
func titleOf(t *testing.T, repo string) string {
	t.Helper()
	var backlog struct {
		Stories []struct {
			Title string `json:"title"`
		} `json:"stories"`
	}
	readJSON(t, filepath.Join(repo, "user_stories.json"), &backlog)
	if len(backlog.Stories) != 1 {
		t.Fatalf("the sandbox's backlog holds %d stories, want the one `sdlc init` writes", len(backlog.Stories))
	}
	return backlog.Stories[0].Title
}

// ---------------------------------------------------------------- AC-1

func TestAC1OneCommandMakesASandboxPrintsWhereItIsAndRemovesItTouchingNothingElse(t *testing.T) {
	parallel(t)
	before := checkoutStatus(t)
	sb := newSandbox(t)

	t.Run("AC-1 the directory is the program's own, directly under the temp directory", func(t *testing.T) {
		if filepath.Dir(sb.dir) != sb.temp || !strings.HasPrefix(filepath.Base(sb.dir), sandboxPrefix) {
			t.Errorf("the sandbox is %s, want a directory named %s* directly under %s", sb.dir, sandboxPrefix, sb.temp)
		}
		for word, path := range map[string]string{"sandbox": sb.dir, "repository": sb.repo, "sdlc": sb.sdlc} {
			if actual, err := filepath.EvalSymlinks(path); err != nil || actual != path {
				t.Errorf("create printed %s for %q, which is not that path with its links resolved (%s, %v): "+
					"sdlc prints resolved paths, and the two have to compare equal", path, word, actual, err)
			}
		}
		if sb.repo == sb.dir || !within(sb.repo, sb.dir) || !within(sb.sdlc, sb.dir) || within(sb.sdlc, sb.repo) {
			t.Errorf("the repository %s and the binary %s have to be in the sandbox %s, and the binary outside the repository",
				sb.repo, sb.sdlc, sb.dir)
		}
		if !strings.HasSuffix(sb.printed, "\n") {
			t.Errorf("create's last line has no newline: %q", sb.printed)
		}
	})

	t.Run("AC-1 it is outside the checkout", func(t *testing.T) {
		if within(sb.dir, moduleRoot(t)) {
			t.Errorf("the sandbox %s is inside the checkout %s", sb.dir, moduleRoot(t))
		}
		// A repository of its own, not a directory of one above it.
		if top := sb.mustGit(t, sb.repo, "rev-parse", "--show-toplevel"); !samePath(top, sb.repo) {
			t.Errorf("git takes %s for part of the repository at %s", sb.repo, top)
		}
	})

	t.Run("AC-1 the repository has sdlc initialised, one ready story, and nothing uncommitted", func(t *testing.T) {
		if got := storiesOf(t, sb.repo); len(got) != 1 || got["US-001"] != "ready" {
			t.Errorf("the backlog holds %v, want US-001 and nothing else, ready", got)
		}
		if !exists(filepath.Join(sb.repo, ".sdlc", "config.json")) {
			t.Error("there is no .sdlc/config.json: sdlc is not initialised")
		}
		// `sdlc start` begins work only from a trunk with nothing uncommitted on
		// it, so a sandbox left as `sdlc init` leaves a directory cannot be used.
		if dirty := sb.mustGit(t, sb.repo, "status", "--porcelain", "--untracked-files=all"); dirty != "" {
			t.Errorf("the sandbox has uncommitted files:\n%s", dirty)
		}
		var cfg struct {
			Git struct {
				Trunk string `json:"trunk_branch"`
			} `json:"git"`
		}
		readJSON(t, filepath.Join(sb.repo, ".sdlc", "config.json"), &cfg)
		if branch := sb.mustGit(t, sb.repo, "symbolic-ref", "--short", "HEAD"); branch != cfg.Git.Trunk || branch == "" {
			t.Errorf("the sandbox is on %q, and its configuration names %q as trunk", branch, cfg.Git.Trunk)
		}
		stdout, stderr, code := sb.outcome(t, sandboxSession, sb.repo, "", "status")
		want := "No iteration running.\n\n  backlog  1 ready\n  next up  US-001  " + titleOf(t, sb.repo) +
			"\n\nA person runs `sdlc start`, or types /sdlc:next in Claude Code, to begin.\n"
		if stdout != want || stderr != "" || code != 0 {
			t.Errorf("sdlc status in a new sandbox: exit %d\nstdout %q\nstderr %q\nwant stdout %q", code, stdout, stderr, want)
		}
	})

	t.Run("AC-1 making it touched nothing else", func(t *testing.T) {
		if got := entries(t, sb.temp); !slices.Equal(got, []string{filepath.Base(sb.dir)}) {
			t.Errorf("the temp directory the program ran with holds %v, want the sandbox alone", got)
		}
		// Whatever the program's own git, go and sdlc keep under a home goes in
		// the sandbox: go's telemetry was the first thing found here.
		if got := entries(t, sb.home); len(got) != 0 {
			t.Errorf("the home directory the program ran with was written to: %v", got)
		}
		if after := checkoutStatus(t); after != before {
			t.Errorf("the checkout changed:\nbefore\n%s\nafter\n%s", before, after)
		}
	})

	sb.removed(t)

	t.Run("AC-1 removing it leaves nothing, and nothing else is gone", func(t *testing.T) {
		if exists(sb.dir) {
			t.Errorf("%s is still there after remove", sb.dir)
		}
		if got := entries(t, sb.temp); len(got) != 0 {
			t.Errorf("remove left %v in the temp directory", got)
		}
		if got := entries(t, sb.home); len(got) != 0 {
			t.Errorf("remove wrote %v in the home directory", got)
		}
		if after := checkoutStatus(t); after != before {
			t.Errorf("the checkout changed:\nbefore\n%s\nafter\n%s", before, after)
		}
	})

	t.Run("AC-1 a second remove is refused, and says what to do", func(t *testing.T) {
		stdout, stderr, code := runProgram(t, sb.program, programEnv(t, sb.home, sb.temp), "remove", sb.dir)
		refusedRemoval(t, stdout, stderr, code)
	})
}

// refusedRemoval holds a remove that had to be refused to an exit code of 1,
// nothing on stdout, and words on stderr that begin with the program's name
// and name the command a removable directory comes from.
func refusedRemoval(t *testing.T, stdout, stderr string, code int) {
	t.Helper()
	if code != 1 || stdout != "" {
		t.Errorf("remove exited %d and printed %q, want exit 1 and nothing on stdout\nstderr %q", code, stdout, stderr)
	}
	if !strings.HasPrefix(stderr, "sandbox: ") || !strings.Contains(stderr, "./task sandbox") {
		t.Errorf("the refusal does not say what to do, in the program's own name and naming ./task sandbox:\n%q", stderr)
	}
}

// T-5: remove takes a path and deletes a tree, and whoever calls it may be a
// model with nobody watching. It removes a directory create made and nothing
// else. internal/tools/sandbox holds the whole table; these are the cases a
// built program is asked from a shell.
func TestAC1RemoveRefusesADirectoryCreateDidNotMake(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	env := programEnv(t, sb.home, sb.temp)
	keep := "kept.txt"

	lookalike := filepath.Join(sb.temp, sandboxPrefix+"lookalike")
	elsewhere := scratch(t)
	for _, dir := range []string{lookalike, elsewhere} {
		writeFile(t, filepath.Join(dir, keep), "a person's file\n")
	}

	for _, c := range []struct {
		name string
		from string
		path string
		kept string
	}{
		{"AC-1 a directory under the temp directory with a sandbox's name and nothing create wrote", "", lookalike, filepath.Join(lookalike, keep)},
		{"AC-1 a directory that is not under the temp directory", "", elsewhere, filepath.Join(elsewhere, keep)},
		{"AC-1 the working directory, named as a dot", elsewhere, ".", filepath.Join(elsewhere, keep)},
		{"AC-1 no directory at all", elsewhere, "", filepath.Join(elsewhere, keep)},
		{"AC-1 the repository inside a sandbox, which is not the sandbox", "", sb.repo, filepath.Join(sb.repo, "user_stories.json")},
		{"AC-1 the temp directory a sandbox is in", "", sb.temp, filepath.Join(sb.repo, "user_stories.json")},
	} {
		t.Run(c.name, func(t *testing.T) {
			from := c.from
			if from == "" {
				from = moduleRoot(t)
			}
			stdout, stderr, code := runProgramIn(t, from, sb.program, env, "-C", moduleRoot(t), "remove", c.path)
			if !exists(c.kept) {
				t.Fatalf("remove %q deleted %s", c.path, c.kept)
			}
			if code == 0 {
				t.Errorf("remove %q exited 0", c.path)
			}
			if stdout != "" || !strings.HasPrefix(stderr, "sandbox: ") {
				t.Errorf("remove %q: stdout %q, stderr %q; want nothing on stdout and a refusal that begins \"sandbox: \"", c.path, stdout, stderr)
			}
		})
	}

	// A sandbox moved away keeps what create wrote in it, and is no longer where
	// create put it: both have to hold.
	t.Run("AC-1 a sandbox moved out of the temp directory", func(t *testing.T) {
		moved := filepath.Join(scratch(t), filepath.Base(sb.dir))
		if err := os.Rename(sb.dir, moved); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runProgram(t, sb.program, env, "remove", moved)
		if !exists(filepath.Join(moved, filepath.Base(sb.repo), "user_stories.json")) {
			t.Fatalf("remove deleted a directory outside the temp directory: %s", moved)
		}
		refusedRemoval(t, stdout, stderr, code)
		if err := os.Rename(moved, sb.dir); err != nil {
			t.Fatal(err)
		}
	})

	// And put back, it is the sandbox create made, which remove takes.
	sb.removed(t)
	if exists(sb.dir) {
		t.Errorf("%s is still there after remove", sb.dir)
	}
	if !exists(filepath.Join(lookalike, keep)) {
		t.Errorf("removing %s took %s with it", sb.dir, lookalike)
	}
}

// T-3 and T-7: `./task sandbox` is not a test, so nothing strips the
// environment it is run in. A session that has git's own variables set, the
// loop's switches, or go flags of its own must get the same sandbox, in the
// same place, built the same way.
func TestAC1TheSandboxIsTheSameWhateverTheSessionThatMakesItHasSet(t *testing.T) {
	parallel(t)
	decoy := scratch(t)
	sb := newSandboxWith(t, sandboxProgram(t),
		"GIT_DIR="+filepath.Join(decoy, "git"),
		"GIT_WORK_TREE="+decoy,
		"GIT_INDEX_FILE="+filepath.Join(decoy, "index"),
		"SDLC_ENFORCE=0",
		"SDLC_BIN="+filepath.Join(decoy, "no-such-sdlc"),
		"CLAUDE_PROJECT_DIR="+decoy,
		"CLAUDE_CODE_SESSION_ID=the-developers-own-session",
		// With -n go prints what it would build and builds nothing.
		"GOFLAGS=-n",
	)
	if got := entries(t, decoy); len(got) != 0 {
		t.Errorf("the program wrote %v where the session's own variables pointed", got)
	}
	if got := storiesOf(t, sb.repo); len(got) != 1 || got["US-001"] != "ready" {
		t.Errorf("the backlog holds %v, want US-001 and nothing else, ready", got)
	}
	if dirty := sb.mustGit(t, sb.repo, "status", "--porcelain", "--untracked-files=all"); dirty != "" {
		t.Errorf("the sandbox has uncommitted files:\n%s", dirty)
	}
	// The developer's session is recorded nowhere in it (T-3).
	for name, body := range files(t, sb.repo) {
		if strings.Contains(body, "the-developers-own-session") {
			t.Errorf("%s in the sandbox names the session the program was run from", name)
		}
	}
	if stdout, stderr, code := sb.outcome(t, "", sb.repo, "", "version"); code != 0 || strings.TrimSpace(stdout) == "" {
		t.Errorf("the sdlc the program built does not run: exit %d\n%s%s", code, stdout, stderr)
	}
}

// ---------------------------------------------------------------- AC-5

// AC-5 says the sandbox makes no network call and writes nothing outside its
// own temporary directory, and that a test fails if it does.
//
// What is held here is narrower, and it is said plainly (S2 of the analysis):
// every route known to leave the directory is closed, and each has a check
// that fails if it opens. No unprivileged facility on Linux, macOS and Windows
// alike lets a test see an arbitrary write or connection of a process tree, so
// a write to a place nobody thought of is not seen. The routes:
//
//   - the lock every writing command takes under the user's cache directory,
//     and the scratch directories it makes for git under the temp directory:
//     home, cache and temp all point into the sandbox, the lock is found
//     there, and the directories they pointed at before hold what they held;
//   - the one fetch in the binary, reached only where git.remote is true:
//     the configuration says false and the repository has no remote;
//   - anything else git might be made to fetch: the repository's own
//     configuration refuses every transport, and a fetch is tried to show it;
//   - a client of its own: the binary links neither net/http nor crypto/tls.
//
// The exception is go's build and module caches, which the program that makes
// the sandbox writes while it builds sdlc: a build with neither would start
// from nothing and fetch from the network.
func TestAC5TheSandboxClosesEveryKnownRouteOutOfItsDirectory(t *testing.T) {
	parallel(t)
	before := checkoutStatus(t)
	sb := newSandbox(t)
	title := titleOf(t, sb.repo)

	// A whole story, start to trunk, by the session under test.
	sb.does(t, sb.repo, "", "", "start")
	sb.worksEveryGate(t)
	if r := sb.does(t, sb.tree(), "", "", "stop"); r.code != 0 {
		t.Fatalf("sdlc stop exited %d:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := storiesOf(t, sb.repo); got["US-001"] != "done" {
		t.Fatalf("the story is %q after the run, want done (%s)", got["US-001"], title)
	}

	t.Run("AC-5 nothing was written beside the sandbox, in the home the program ran with, or in the checkout", func(t *testing.T) {
		if got := entries(t, sb.temp); !slices.Equal(got, []string{filepath.Base(sb.dir)}) {
			t.Errorf("the directory holding the sandbox holds %v, want the sandbox alone", got)
		}
		if got := entries(t, sb.home); len(got) != 0 {
			t.Errorf("the home directory the program ran with was written to: %v", got)
		}
		if after := checkoutStatus(t); after != before {
			t.Errorf("the checkout changed:\nbefore\n%s\nafter\n%s", before, after)
		}
	})

	t.Run("AC-5 the lock every writing command takes went inside the sandbox", func(t *testing.T) {
		var locks []string
		err := filepath.WalkDir(sb.dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() && d.Name() == "locks" && filepath.Base(filepath.Dir(p)) == "sdlc" {
				locks = append(locks, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(locks) == 0 {
			t.Errorf("no sdlc/locks directory anywhere under %s: the commands took their lock somewhere else", sb.dir)
		}
	})

	t.Run("AC-5 the repository has no remote and the loop is told there is none", func(t *testing.T) {
		if remotes := sb.mustGit(t, sb.repo, "remote"); remotes != "" {
			t.Errorf("the sandbox has remotes: %s", remotes)
		}
		var cfg struct {
			Git struct {
				Remote *bool `json:"remote"`
			} `json:"git"`
		}
		readJSON(t, filepath.Join(sb.repo, ".sdlc", "config.json"), &cfg)
		if cfg.Git.Remote == nil || *cfg.Git.Remote {
			t.Errorf("git.remote in the sandbox's configuration is %v, want false: true is what reaches the one fetch in the binary", cfg.Git.Remote)
		}
	})

	t.Run("AC-5 git refuses every transport in the sandbox", func(t *testing.T) {
		if got, err := sb.git(t, sb.repo, "config", "--local", "--get", "protocol.allow"); err != nil || got != "never" {
			t.Fatalf("protocol.allow in the repository's own configuration is %q (%v), want never", got, err)
		}
		// A repository beside it, inside the sandbox, to fetch from: with no
		// transport allowed git stops before it opens anything, even a file.
		other := filepath.Join(sb.own(t, "tmp"), "other.git")
		sb.mustGit(t, sb.dir, "init", "--quiet", "--bare", other)
		out, err := sb.git(t, sb.repo, "fetch", other)
		if err == nil || !strings.Contains(out, "not allowed") {
			t.Errorf("a fetch in the sandbox was not refused as a transport that is not allowed (%v):\n%s", err, out)
		}
	})
}

// The imports are a property of the checkout and hold today; they are here so
// that a client of the network cannot arrive in the binary, or in the program
// that makes the sandbox, without a test failing. It cannot be "no net": pflag
// imports it.
func TestAC5NeitherTheBinaryNorTheSandboxProgramLinksANetworkClient(t *testing.T) {
	parallel(t)
	deps := func(t *testing.T, pkg string) []string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", pkg)
		cmd.Dir = moduleRoot(t)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
		return strings.Fields(string(out))
	}
	t.Run("AC-5 cmd/sdlc", func(t *testing.T) {
		list := deps(t, "./cmd/sdlc")
		for _, banned := range []string{"net/http", "crypto/tls"} {
			if slices.Contains(list, banned) {
				t.Errorf("the binary links %s", banned)
			}
		}
		// The sandbox is tooling: nothing the released binary links knows of it.
		for _, pkg := range list {
			if strings.Contains(pkg, "sandbox") {
				t.Errorf("the binary links %s", pkg)
			}
		}
	})
	t.Run("AC-5 internal/tools/sandbox", func(t *testing.T) {
		// A directory of tests and no program lists no imports, and would pass.
		cmd := exec.CommandContext(t.Context(), "go", "list", "-f", "{{.Name}} {{len .GoFiles}}", "./internal/tools/sandbox")
		cmd.Dir = moduleRoot(t)
		if out, err := cmd.Output(); err != nil || !strings.HasPrefix(string(out), "main ") || strings.TrimSpace(string(out)) == "main 0" {
			t.Fatalf("internal/tools/sandbox is not a program yet (%v): %q", err, out)
		}
		list := deps(t, "./internal/tools/sandbox")
		for _, banned := range []string{"net/http", "crypto/tls"} {
			if slices.Contains(list, banned) {
				t.Errorf("the sandbox program links %s", banned)
			}
		}
	})
}

// ---------------------------------------------------------------- AC-6

// sandboxSection is the section of CONTRIBUTING.md that says how sdlc is
// tested in a sandbox. AC-6 says docs/; nothing under docs/ describes testing
// sdlc itself and CONTRIBUTING.md does, so that is where it goes (S1 of the
// analysis).
const sandboxSection = "## Testing in a sandbox"

// fencedBlocks is the content of each fenced block in a Markdown text.
func fencedBlocks(text string) []string {
	var blocks []string
	var block []string
	fenced := false
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if fenced {
				blocks = append(blocks, strings.Join(block, "\n")+"\n")
			}
			fenced, block = !fenced, nil
			continue
		}
		if fenced {
			block = append(block, line)
		}
	}
	return blocks
}

func TestAC6TheContributingGuideNamesTheSandboxCommandAndQuotesWhatItPrints(t *testing.T) {
	parallel(t)
	guide := section(readFile(t, filepath.Join(moduleRoot(t), "CONTRIBUTING.md")), sandboxSection)
	if guide == "" {
		t.Fatalf("CONTRIBUTING.md has no %q section", sandboxSection)
	}

	t.Run("AC-6 it names the command that makes one, the one that removes it, and the tests that use one", func(t *testing.T) {
		for _, want := range []string{"./task sandbox", "./task sandbox-remove", "test/e2e"} {
			if !strings.Contains(guide, want) {
				t.Errorf("the section does not mention %s", want)
			}
		}
	})

	t.Run("AC-6 the commands it names are tasks that run the program", func(t *testing.T) {
		taskfile := readFile(t, filepath.Join(moduleRoot(t), "Taskfile.yml"))
		for task, runs := range map[string]string{
			"sandbox":        "go run ./internal/tools/sandbox create",
			"sandbox-remove": "go run ./internal/tools/sandbox remove",
		} {
			body := taskBody(taskfile, task)
			if body == "" {
				t.Errorf("Taskfile.yml has no task %s", task)
			} else if !strings.Contains(body, runs) {
				t.Errorf("the task %s does not run %q:\n%s", task, runs, body)
			}
		}
	})

	t.Run("AC-6 the output it quotes is what the command prints", func(t *testing.T) {
		sb := newSandbox(t)
		// The path differs on every run and on every machine, and a path from
		// anyone's machine is refused in a tracked file: the guide shows
		// <sandbox> for the directory, with the slashes of the loop.
		printed := strings.ReplaceAll(sb.printed, "\r\n", "\n")
		printed = strings.ReplaceAll(printed, sb.dir, "<sandbox>")
		printed = strings.ReplaceAll(filepath.ToSlash(printed), "sdlc.exe\n", "sdlc\n")
		if !strings.Contains(printed, "<sandbox>") {
			t.Fatalf("create's output does not name the sandbox: %q", sb.printed)
		}
		for _, block := range fencedBlocks(guide) {
			if strings.Contains(block, printed) {
				return
			}
		}
		t.Errorf("no fenced block of the section holds what `./task sandbox` prints, which is:\n%s", printed)
	})

	// T-1: whatever the guide says to run, a session in a real project can run
	// too. It says how to make a sandbox and which tests work a story in one; a
	// line that ran a person's decision "from outside the session" would work
	// just as well in the project the reader is standing in.
	t.Run("AC-6 it gives no way to run a person's decision", func(t *testing.T) {
		for _, recipe := range []string{
			"sdlc approve", "sdlc unfreeze", "sdlc ack", "sdlc release", "sdlc parallel",
			"CLAUDE_CODE_SESSION_ID", "SDLC_ENFORCE",
		} {
			if strings.Contains(guide, recipe) {
				t.Errorf("the section spells out %q", recipe)
			}
		}
	})
}

// taskBody is the lines of one task of a Taskfile: from its name, two spaces
// in, to the next line that is indented no further.
func taskBody(taskfile, name string) string {
	lines := strings.Split(taskfile, "\n")
	start := slices.Index(lines, "  "+name+":")
	if start < 0 {
		return ""
	}
	end := start + 1
	for end < len(lines) && (strings.TrimSpace(lines[end]) == "" || strings.HasPrefix(lines[end], "   ")) {
		end++
	}
	return strings.Join(lines[start:end], "\n")
}
