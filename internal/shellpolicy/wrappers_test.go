package shellpolicy

// US-029.2, the wrappers in wrappers.go and program: env's options and -S,
// flock and script, and the wrappers whose options take a value. Read as
// flags, a value was the program, and the commit or the approval behind it met
// no rule. Each refused row would be allowed if one option were misread, and
// each allowed row would be refused if the wrapper were taken for more than it
// runs.

import (
	"slices"
	"testing"
)

// AC-6: env's options are read as env reads them, and the command after them
// is the one it runs, -S's string split into words in its place.
func TestAC6EnvsOptionsAreReadAsEnvReadsThem(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		"env -u HOME git -C /work/main commit -m x",
		"env --unset=HOME git -C /work/main commit -m x",
		"env --unset HOME git -C /work/main commit -m x",
		"env -iu HOME git -C /work/main commit -m x",
		"env -uHOME git -C /work/main commit -m x",
		"env --argv0 x git -C /work/main commit -m x",
		"env -P /usr/bin git -C /work/main commit -m x",
		"env -a x git -C /work/main commit -m x",
		"env - git -C /work/main commit -m x",
		"env -- git -C /work/main commit -m x",
		"env --ignore-environment git -C /work/main commit -m x",
		"env -S 'git -C /work/main commit -m x'",
		"env --split-string='git -C /work/main commit -m x'",
		"env --split-string 'git -C /work/main commit -m x'",
		"env -iS'git -C /work/main commit -m x'",
		"env -vS 'GIT_DIR=/work/main/.git git commit -m x'",
		"env -S '-i git -C /work/main commit -m x'",
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	wtRefusedAs(t, "env -S 'sdlc approve A-1'", ready, "approval-is-a-human-decision")
	wtRefusedAs(t, "env -u HOME sdlc approve A-1", ready, "approval-is-a-human-decision")
	for _, command := range []string{
		"env -i",
		"env -u HOME",
		"env -S ''",
		"env -u HOME go test ./...",
		"env -S 'go test ./...'",
		"env GOFLAGS=-count=1 go test ./...",
	} {
		allowed(t, command, s)
	}
}

// AC-6: flock runs the command after its lock, or with -c a command line;
// script runs the command after the file it writes the session to, or with
// -c a command line. Their valued options take their values, and macOS's -F
// takes none.
func TestAC6FlockAndScriptRunTheCommandTheyAreGiven(t *testing.T) {
	for _, command := range []string{
		"flock /tmp/l git commit -m x",
		"flock -x /tmp/l git commit -m x",
		"flock --exclusive /tmp/l git commit -m x",
		"flock -w 5 /tmp/l git commit -m x",
		"flock -w5 /tmp/l git commit -m x",
		"flock -E 3 /tmp/l git commit -m x",
		"flock --wait 5 /tmp/l git commit -m x",
		"flock --timeout 5 /tmp/l git commit -m x",
		"flock --conflict-exit-code 3 /tmp/l git commit -m x",
		"flock -- /tmp/l git commit -m x",
		"flock /tmp/l -c 'git commit -m x'",
		"flock /tmp/l --command 'git commit -m x'",
		"script -q /dev/null git commit -m x",
		"script -a f git commit -m x",
		"script -F f git commit -m x",
		"script -t 0 f git commit -m x",
		"script -tfile f git commit -m x",
		"script -- f git commit -m x",
		"script -c 'git commit -m x' /dev/null",
		"script -qc 'git commit -m x' /dev/null",
		"script -c'git commit -m x' /dev/null",
		"script --command 'git commit -m x' /dev/null",
		"script --command='git commit -m x' /dev/null",
		"script -E never -c 'git commit -m x' /dev/null",
		"script -Ealways -c 'git commit -m x' /dev/null",
		"script --echo never -c 'git commit -m x' /dev/null",
		"script --echo=never -c 'git commit -m x' /dev/null",
		"script --log-out f -c 'git commit -m x'",
	} {
		wtRefusedAs(t, command, notReady, "commit-gate")
	}
	wtRefusedAs(t, "flock /tmp/l -c 'sdlc approve A-1'", ready, "approval-is-a-human-decision")
	wtRefusedAs(t, "script -q /dev/null sdlc approve A-1", ready, "approval-is-a-human-decision")
	for _, command := range []string{
		"flock",
		"flock /tmp/l",
		"flock -n 9",
		"flock -w 5 /tmp/l make build",
		"flock /tmp/l -c 'go test ./...'",
		"script",
		"script f",
		"script -q",
		"script -c",
		"script --command",
		"script -q /dev/null go test ./...",
		"script -F f make",
	} {
		allowed(t, command, notReady)
	}
}

// AC-6: what flock and script run, as words, and whether it is a command line
// for a shell.
func TestAC6FlockedAndScriptedAreTheCommandAfterTheirOptions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		got   func() ([]string, bool)
		words []string
		line  bool
	}{
		{"flock f cmd", func() ([]string, bool) { return flocked([]string{"f", "git", "log"}) }, []string{"git", "log"}, false},
		{"flock -w 1 f cmd", func() ([]string, bool) { return flocked([]string{"-w", "1", "f", "git"}) }, []string{"git"}, false},
		{"flock f -c line", func() ([]string, bool) { return flocked([]string{"f", "-c", "git log", "extra"}) }, []string{"git log"}, true},
		{"flock f -c", func() ([]string, bool) { return flocked([]string{"f", "-c"}) }, []string{"-c"}, false},
		{"flock -x", func() ([]string, bool) { return flocked([]string{"-x"}) }, nil, false},
		{"script f cmd", func() ([]string, bool) { return scripted([]string{"f", "git", "log"}) }, []string{"git", "log"}, false},
		{"script -c line f", func() ([]string, bool) { return scripted([]string{"-c", "git log", "f"}) }, []string{"git log"}, true},
		{"script -cline f", func() ([]string, bool) { return scripted([]string{"-cgit log", "f"}) }, []string{"git log"}, true},
		{"script --command=line", func() ([]string, bool) { return scripted([]string{"--command=git log"}) }, []string{"git log"}, true},
		{"script --command", func() ([]string, bool) { return scripted([]string{"--command"}) }, nil, true},
		{"script -O out f cmd", func() ([]string, bool) { return scripted([]string{"-O", "out", "f", "git"}) }, []string{"git"}, false},
		{"script -t 2 f cmd", func() ([]string, bool) { return scripted([]string{"-t", "2", "f", "git"}) }, []string{"git"}, false},
		{"script -t f cmd", func() ([]string, bool) { return scripted([]string{"-t", "f", "git"}) }, []string{"git"}, false},
		{"script f", func() ([]string, bool) { return scripted([]string{"f"}) }, nil, false},
	} {
		words, line := tc.got()
		if !slices.Equal(words, tc.words) || line != tc.line {
			t.Errorf("%s = %q, %v; want %q, %v", tc.name, words, line, tc.words, tc.line)
		}
	}
}

// AC-6: exec, stdbuf, ionice and chrt run the command after their options,
// and ionice's and chrt's options, and chrt's priority, are not the command.
func TestAC6WrappersWithValuedOptionsRunTheCommandAfterThem(t *testing.T) {
	for _, command := range []string{
		"exec git commit -m x",
		"exec -a name git commit -m x",
		"stdbuf -o0 git commit -m x",
		"stdbuf -o L git commit -m x",
		"ionice -c 3 git commit -m x",
		"ionice -c3 git commit -m x",
		"ionice --class 3 git commit -m x",
		"chrt 10 git commit -m x",
		"chrt -r 10 git commit -m x",
		"chrt -T 1 -P 2 -D 3 -d 0 git commit -m x",
		"chrt --sched-runtime 1 -d 0 git commit -m x",
		"setsid git commit -m x",
		"unbuffer git commit -m x",
		"caffeinate -i git commit -m x",
	} {
		wtRefusedAs(t, command, notReady, "commit-gate")
	}
	for _, command := range []string{
		"exec -a name go test ./...",
		"stdbuf -o L go test ./...",
		"ionice -c 3 go build ./...",
		"chrt -r 10 make",
		"chrt 10",
	} {
		allowed(t, command, notReady)
	}
}
