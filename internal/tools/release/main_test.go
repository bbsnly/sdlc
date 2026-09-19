package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/release"
)

// The library is tested next door. What can only go wrong here is argument
// parsing, and it did: `release check -tag v1` -- the spelling this tool
// documents and the release workflow uses -- left -tag unset, so the preflight
// that exists to stop a mismatched tag being released checked nothing and
// reported success.
func TestTheTagIsCheckedWhicheverSideOfTheSubcommandItIsOn(t *testing.T) {
	for _, args := range [][]string{
		{"check", "-C", "../../..", "-tag", "v9.9.9"},
		{"-C", "../../..", "check", "-tag", "v9.9.9"},
		{"-C", "../../..", "-tag", "v9.9.9", "check"},
	} {
		err := run(args, io.Discard)
		if err == nil {
			t.Errorf("release %s passed on a tag that does not match the version",
				strings.Join(args, " "))
		}
	}
}

// The repository's own version is the one every file agrees on, so the matching
// tag has to pass -- otherwise the check above would be satisfied by a tool
// that always failed.
func TestTheMatchingTagPasses(t *testing.T) {
	var out strings.Builder
	if err := run([]string{"check", "-C", "../../..", "-tag", "v" + thisVersion(t)}, &out); err != nil {
		t.Fatalf("the repository's own tag was refused: %v", err)
	}
	if !strings.Contains(out.String(), "every file that carries it agrees") {
		t.Errorf("stdout = %q", out.String())
	}
}

// A mistyped flag lands in the argument list. Ignoring it is how a check goes
// on reporting success while checking something other than what was asked.
func TestAStrayArgumentIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"check", "-C", "../../..", "v9.9.9"},
		{"check", "-C", "../../..", "-tag", "v9.9.9", "extra"},
	} {
		if err := run(args, io.Discard); err == nil {
			t.Errorf("release %s was accepted", strings.Join(args, " "))
		}
	}
}

func TestAnUnknownCommandSaysWhatThereIs(t *testing.T) {
	err := run([]string{"publish", "-C", "../../.."}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "check, version or notes") {
		t.Errorf("err = %v", err)
	}
}

func thisVersion(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	if err := run([]string{"version", "-C", "../../.."}, &out); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out.String())
}

// write builds a scratch repository root carrying only what release.Version and
// release.Notes read: the tests below check dispatch and argument handling, not
// the library, so the fixture is as small as those two functions need.
func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A flag typo before the subcommand must not be swallowed, or a workflow that
// misspells a flag finds out only when the release it never checked is wrong.
func TestAMistypedFlagBeforeTheSubcommandIsRefused(t *testing.T) {
	err := run([]string{"-bogus", "check"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want a refusal that names the flag", err)
	}
}

// The second parse -- the one that makes `release check -tag v1` mean what it
// documents -- must refuse a typo just as the first one does.
func TestAMistypedFlagAfterTheSubcommandIsRefused(t *testing.T) {
	err := run([]string{"check", "-bogus"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("err = %v, want a refusal that names the flag", err)
	}
}

// The notes command is the library's Notes call by another name, dispatched on
// a version read from the fixture's own plugin.json. Comparing its output
// against release.Notes called a second time would only prove the library
// agrees with itself; a fixture with a changelog written out by hand, and the
// section it must produce written out the same way, proves the dispatch and
// the plumbing actually carry the library's answer to the caller.
func TestNotesPrintsTheRepositorysOwnChangelogSection(t *testing.T) {
	root := t.TempDir()
	write(t, root, release.Source, `{"name":"sdlc","version":"1.2.3"}`)
	write(t, root, release.ChangelogFile,
		"# Changelog\n\n## [1.2.3]\n\n- added a thing\n- fixed another\n\n## [1.0.0]\n\n- old\n")

	var out strings.Builder
	if err := run([]string{"notes", "-C", root}, &out); err != nil {
		t.Fatal(err)
	}
	const want = "- added a thing\n- fixed another"
	if got := strings.TrimSuffix(out.String(), "\n"); got != want {
		t.Errorf("notes = %q, want %q", got, want)
	}
}

// notes asks for the version before it asks for the section; a repository
// with no version to read must fail there, not print an empty section.
func TestNotesFailsWhenTheVersionCannotBeDetermined(t *testing.T) {
	err := run([]string{"notes", "-C", t.TempDir()}, io.Discard)
	if err == nil {
		t.Fatal("want an error when there is no plugin.json to read a version from")
	}
}

// version is the same failure one command earlier: with nothing to read, it
// must fail rather than print an empty line.
func TestVersionFailsWhenTheSourceCannotBeRead(t *testing.T) {
	err := run([]string{"version", "-C", t.TempDir()}, io.Discard)
	if err == nil {
		t.Fatal("want an error when there is no plugin.json to read a version from")
	}
}

// A version with no changelog section is exactly the case `release check`
// exists to catch; `release notes` must fail the same way, not print nothing.
func TestNotesFailsWhenTheChangelogHasNoSectionForTheVersion(t *testing.T) {
	root := t.TempDir()
	write(t, root, release.Source, `{"name":"sdlc","version":"9.9.9"}`)
	write(t, root, release.ChangelogFile, "# Changelog\n\n## [1.0.0]\n\n- old\n")

	err := run([]string{"notes", "-C", root}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "9.9.9") {
		t.Errorf("err = %v, want it to name the missing version", err)
	}
}

// check's own preflight is release.Check; a repository it cannot even parse
// must fail the command, not print a false "every file agrees".
func TestCheckFailsWhenTheRepositoryCannotBeRead(t *testing.T) {
	root := t.TempDir()
	write(t, root, release.Source, "{not json")

	var out strings.Builder
	err := run([]string{"check", "-C", root}, &out)
	if err == nil {
		t.Fatal("want an error, not a clean report, for a plugin.json that will not parse")
	}
	if out.String() != "" {
		t.Errorf("stdout = %q, want nothing printed on failure", out.String())
	}
}

// -o is the one thing `emit` does besides writing to the caller's stdout; a
// workflow step reads the version back from that file, not from a log.
func TestDashOWritesToAFileInsteadOfStdout(t *testing.T) {
	out := filepath.Join(t.TempDir(), "version.txt")
	var stdout strings.Builder
	if err := run([]string{"version", "-C", "../../..", "-o", out}, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "" {
		t.Errorf("stdout = %q, want nothing written there when -o is given", stdout.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != thisVersion(t) {
		t.Errorf("file contents = %q", got)
	}
}

// mainHelperEnv re-execs this test binary as a plain `release` process: main
// itself is only ever the two lines that turn run's error into stderr and an
// exit code, and that wiring is the one thing run's own extensive tests below
// cannot see.
const mainHelperEnv = "SDLC_RELEASE_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(mainHelperEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runBinary(t *testing.T, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Env = append(os.Environ(), mainHelperEnv+"=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		exitCode = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running release: %v", err)
	}
	return out.String(), errb.String(), exitCode
}

// A repository this coherent already has a test: internal/release's own
// TestThisRepositoryIsCoherent. What main adds on top of run is the exit code
// and the streams, and a fixture proves that without depending on the state
// of the real repository the test happens to run inside.
func TestMainPrintsAndExitsCleanOnSuccess(t *testing.T) {
	root := t.TempDir()
	write(t, root, release.Source, `{"name":"sdlc","version":"1.2.3"}`)
	write(t, root, "npm/package.json", `{"name":"sdlc","version":"1.2.3"}`)
	write(t, root, release.ChangelogFile, "# Changelog\n\n## [1.2.3]\n\n- something\n")

	out, errb, code := runBinary(t, []string{"check", "-C", root})
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	if !strings.Contains(out, "every file that carries it agrees") {
		t.Errorf("stdout = %q", out)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want nothing on success", errb)
	}
}

// The failure path is the one main adds to run: without it, a check that
// found problems would still exit 0 and a workflow would ship anyway.
func TestMainReportsRunsErrorToStderrAndExitsOne(t *testing.T) {
	_, errb, code := runBinary(t, []string{"publish", "-C", "../../.."})
	if code != 1 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	if !strings.Contains(errb, "release: ") || !strings.Contains(errb, "check, version or notes") {
		t.Errorf("stderr = %q", errb)
	}
}
