package gitx

// US-093: the diff driver keys the landing's measures read. Fingerprint and
// LinesFingerprint override every diff.<name>.algorithm the repository
// configures with myers, as git's command line takes it, because before git
// 2.42 a driver's own algorithm outranked the command line. The stop refuses a
// name that command line cannot carry (AC-2), and does so in the cli, not here:
// the commit gate measures the change with these two too, and has to go on
// working for a repository with such a name. So they measure, and do not
// refuse, for any name (AC-2); and the empty name, diff..algorithm, which git
// applies to a path whose diff attribute is empty, is overridden with the rest
// as the merge side's is (AC-2).
//
// The empty name's override cannot be seen in a fingerprint on a git from 2.42,
// where the command line outranks the driver whatever is done, so it is read
// off the git the measure runs: a git first on PATH that logs each call and
// runs the real one. Helpers carry a dn prefix, for driver names.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// dnLoggingGit puts a git first on PATH that appends each call's arguments, one
// call to a line, to the file it returns, and then runs the git found there
// now.
func dnLoggingGit(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the logging git is a shell script, which Windows does not run as git")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH to log")
	}
	dir := lgTempDir(t)
	log := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + filepath.ToSlash(log) + "'\nexec '" + filepath.ToSlash(gitPath) + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// dnCalls is every call logged in log so far, and then forgets them.
func dnCalls(t *testing.T, log string) string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// AC-2: the diff driver with an empty name is overridden like every other.
// Both measures hand git the algorithm for diff..algorithm, as they do for a
// driver with a name; the driver with a name is the control, which shows the log
// sees the setting when it is handed over.
func TestAC2TheEmptyDiffDriverNameIsOverriddenWithTheOthersByBothMeasures(t *testing.T) {
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	run(t, root, "config", "diff.custom.algorithm", "patience")
	run(t, root, "config", "diff..algorithm", "patience")
	log := dnLoggingGit(t)

	measures := map[string]func() error{
		"Fingerprint": func() error { _, err := Fingerprint(t.Context(), root, from, to); return err },
		"LinesFingerprint": func() error {
			_, err := LinesFingerprint(t.Context(), root, from, to)
			return err
		},
	}
	for name, measure := range measures {
		t.Run("AC-2 "+name, func(t *testing.T) {
			dnCalls(t, log)
			if err := measure(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			calls := dnCalls(t, log)
			if !strings.Contains(calls, " -c diff.custom.algorithm=myers ") {
				t.Fatalf("precondition: the call to git does not override diff.custom.algorithm, so the log shows nothing about the others:\n%s", calls)
			}
			if !strings.Contains(calls, " -c diff..algorithm=myers ") {
				t.Errorf("git was not handed diff..algorithm=myers, so a repository's diff driver with an empty name keeps its own algorithm:\n%s", calls)
			}
		})
	}
}

// AC-2: the measures do not refuse a diff driver whose name git's command line
// cannot carry. The commit gate measures the story's change with them, and the
// decision is that it goes on passing for such a repository; the stop's
// refusal is the cli's, raised before it measures. Each name has a measure
// that works, for the same change, with a hash and no error.
func TestAC2TheMeasuresDoNotRefuseADiffDriverWhoseNameTheCommandLineCannotCarry(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"st=eer", "st{sdlc-private}eer", "{sdlc-private}", "St=EER"} {
		t.Run("AC-2 "+name, func(t *testing.T) {
			root, from := lgRepo(t)
			_, to := lgStoryWork(t, root)
			run(t, root, "config", "--", "diff."+name+".algorithm", "patience")

			full, err := Fingerprint(t.Context(), root, from, to)
			if err != nil || full == "" {
				t.Errorf("Fingerprint = %q, %v, want a fingerprint and no error", full, err)
			}
			lines, err := LinesFingerprint(t.Context(), root, from, to)
			if err != nil || lines == "" {
				t.Errorf("LinesFingerprint = %q, %v, want a fingerprint and no error", lines, err)
			}
		})
	}
}
