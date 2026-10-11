package main

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

// US-067 AC-4: the hook, exercised in a sandbox, answers {"continue":true}
// after a forced panic.
//
// Nothing in the released binary forces one, and nothing should: the hook lets
// a call through when it panics, so a way to make it panic that a session could
// reach -- a field of the payload, a word in a command, a file in the project
// -- would turn every rule off for that call. So the panic comes from here.
// This test binary re-execs itself as an sdlc process (panicHelperPrefix, in
// testenv_test.go) whose environment panics when a chosen variable is read,
// and what answers is the released run and crash, in a real process, with the
// sandbox as its directory and the JSON Claude Code sends on a real stdin.
//
// What it is not is the file `go build` wrote, which the rest of the sandbox's
// tests run: test/e2e holds that one to the protocol where it does not panic.

// panicSession is the session working the story in the sandbox.
const panicSession = "sandbox-session-under-test"

// crashSandbox is a sandbox made by the program `./task sandbox` runs, with
// its story started by panicSession, and the environment its commands run in.
type crashSandbox struct {
	repo, tree, sdlc string
	env              []string
}

func newCrashSandbox(t *testing.T) crashSandbox {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	resolved := func(dir string) string {
		if actual, err := filepath.EvalSymlinks(dir); err == nil {
			return actual
		}
		return dir
	}
	goEnv := func(name string) string {
		out, err := exec.CommandContext(t.Context(), "go", "env", name).Output()
		if err != nil {
			t.Fatalf("go env %s: %v", name, err)
		}
		return strings.TrimSpace(string(out))
	}
	homes := func(home, temp string) []string {
		env := []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + home, "USERPROFILE=" + home,
			"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"), "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
			"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"), "APPDATA=" + filepath.Join(home, "AppData", "Roaming"),
			"TMPDIR=" + temp, "TMP=" + temp, "TEMP=" + temp,
			"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		}
		for _, name := range []string{"SystemRoot", "SystemDrive", "ComSpec", "PATHEXT", "WINDIR"} {
			if v, ok := os.LookupEnv(name); ok {
				env = append(env, name+"="+v)
			}
		}
		return env
	}

	program := filepath.Join(t.TempDir(), "sandbox")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", program, "./internal/tools/sandbox")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the sandbox program, internal/tools/sandbox, does not build: %v\n%s", err, out)
	}

	create := exec.CommandContext(t.Context(), program, "create")
	create.Dir = root
	create.Env = append(homes(resolved(t.TempDir()), resolved(t.TempDir())),
		"GOCACHE="+goEnv("GOCACHE"), "GOMODCACHE="+goEnv("GOMODCACHE"), "GOPATH="+goEnv("GOPATH"), "GOPROXY=off")
	var stdout, stderr bytes.Buffer
	create.Stdout, create.Stderr = &stdout, &stderr
	if err := create.Run(); err != nil {
		t.Fatalf("sandbox create: %v\n%s%s", err, stdout.String(), stderr.String())
	}
	paths := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(stdout.String(), "\n"), "\n") {
		if word, rest, ok := strings.Cut(strings.TrimRight(line, "\r"), " "); ok {
			paths[word] = strings.TrimSpace(rest)
		}
	}
	sb := crashSandbox{repo: paths["repository"], sdlc: paths["sdlc"]}
	if paths["sandbox"] == "" || sb.repo == "" || sb.sdlc == "" {
		t.Fatalf("sandbox create did not print where the sandbox, its repository and its sdlc are:\n%s", stdout.String())
	}
	sb.tree = filepath.Join(sb.repo, ".sdlc", "worktrees", "US-001")

	// Everything the story's commands write outside the repository goes in the
	// sandbox, as it does in test/e2e.
	home, temp := filepath.Join(paths["sandbox"], "home"), filepath.Join(paths["sandbox"], "tmp")
	for _, dir := range []string{home, temp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sb.env = append(homes(home, temp), "CLAUDE_PROJECT_DIR="+sb.repo)

	start := exec.CommandContext(t.Context(), sb.sdlc, "start")
	start.Dir = sb.repo
	start.Env = slices.Concat(sb.env, []string{"CLAUDE_CODE_SESSION_ID=" + panicSession})
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("sdlc start in the sandbox: %v\n%s", err, out)
	}
	return sb
}

// hook runs this binary as `sdlc hook <event>` in the sandbox, with the
// payload on standard input and an environment that panics when the variable
// named is read.
func (sb crashSandbox) hook(t *testing.T, panicOn, event, payload string, extra ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "hook", event)
	cmd.Dir = sb.repo
	cmd.Env = slices.Concat(sb.env, []string{mainHelperEnv + "=" + panicHelperPrefix + panicOn}, extra)
	cmd.Stdin = strings.NewReader(payload)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running the hook: %v", err)
	}
	return out.String(), errb.String(), code
}

// neverRead is a variable nothing reads, so an environment that panics on it
// never panics: the same process, taking the path a hook takes every day.
const neverRead = "SDLC_TEST_A_VARIABLE_NOTHING_READS"

// The same reply from a process that is in no project at all, for every event
// the plugin registers. It needs no sandbox, so it holds today, and it is what
// goes on holding the crash path if the sandbox program is ever taken away.
func TestAC4APanickingHookProcessAnswersContinueTrueWhereverItRuns(t *testing.T) {
	dir := t.TempDir()
	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop"} {
		for _, debug := range []string{"", "SDLC_DEBUG=1"} {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "hook", event)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), mainHelperEnv+"="+panicHelperPrefix+"*", "HOME="+dir, "USERPROFILE="+dir)
			if debug != "" {
				cmd.Env = append(cmd.Env, debug)
			}
			cmd.Stdin = strings.NewReader(`{"hook_event_name":"` + event + `","session_id":"s","cwd":"` + strings.ReplaceAll(dir, `\`, `\\`) + `"}`)
			var out, errb bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &errb
			if err := cmd.Run(); err != nil {
				t.Errorf("%s %s: the hook did not exit 0, so its reply is never read: %v", event, debug, err)
			}
			if out.String() != "{\"continue\":true}\n" {
				t.Errorf("%s %s: stdout is %q, want exactly {\"continue\":true} and a newline", event, debug, out.String())
			}
			if !strings.Contains(errb.String(), "sdlc crashed: forced by the test") || !strings.Contains(errb.String(), "This is a bug") {
				t.Errorf("%s %s: stderr does not carry the report:\n%s", event, debug, errb.String())
			}
			// The first read is the one that says whether debugging is on, so
			// the crash path cannot know, and takes the quiet one.
			if strings.Contains(errb.String(), "goroutine ") {
				t.Errorf("%s %s: a stack was printed by a crash that could not know debugging was on:\n%s", event, debug, errb.String())
			}
		}
	}

	// A command that is not a hook has nobody parsing its stdout: it says
	// nothing there and exits 1.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "version")
	cmd.Env = append(os.Environ(), mainHelperEnv+"="+panicHelperPrefix+"*")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || out.Len() != 0 || !strings.Contains(errb.String(), "This is a bug") {
		t.Errorf("a command that panics: %v, stdout %q, stderr %q; want exit 1, nothing on stdout and the report on stderr", err, out.String(), errb.String())
	}
}

func TestAC4AHookThatPanicsInTheSandboxAnswersContinueTrueAndNothingElse(t *testing.T) {
	sb := newCrashSandbox(t)

	slash := func(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }
	// Two calls the hook answers with something other than "carry on" when it
	// does not panic, so that "carry on" after a panic can only be the crash
	// path's answer: a write to the loop's own state, and the session's stop.
	events := []struct {
		event, payload, healthy string
	}{
		{"PreToolUse",
			`{"hook_event_name":"PreToolUse","session_id":"` + panicSession + `","cwd":"` + slash(sb.tree) +
				`","tool_name":"Write","tool_input":{"file_path":"` + slash(filepath.Join(sb.tree, ".sdlc", "state", "active")) + `"}}`,
			`"permissionDecision":"deny"`},
		{"Stop",
			`{"hook_event_name":"Stop","session_id":"` + panicSession + `","cwd":"` + slash(sb.tree) + `","stop_hook_active":false}`,
			`"decision":"block"`},
	}

	for _, e := range events {
		t.Run("AC-4 "+e.event+": without a panic the hook refuses", func(t *testing.T) {
			stdout, stderr, code := sb.hook(t, neverRead, e.event, e.payload)
			if code != 0 || !strings.Contains(stdout, e.healthy) {
				t.Fatalf("the hook did not answer %s, so a panic that lets the call through would show nothing: exit %d\nstdout %q\nstderr %q",
					e.healthy, code, stdout, stderr)
			}
		})

		// "*" panics on the first variable read, before the payload is; the
		// other two are read once the payload has been, in the middle of
		// deciding.
		for _, variable := range []string{"*", "SDLC_ENFORCE", "CLAUDE_PROJECT_DIR"} {
			t.Run("AC-4 "+e.event+": a panic reading "+variable, func(t *testing.T) {
				stdout, stderr, code := sb.hook(t, variable, e.event, e.payload)
				if stdout != "{\"continue\":true}\n" {
					t.Errorf("stdout is %q, want exactly {\"continue\":true} and a newline", stdout)
				}
				// Claude Code reads a hook's reply only when it exits 0.
				if code != 0 {
					t.Errorf("the hook exited %d, so its reply is never read", code)
				}
				// The report is for the debug log, and says the panic happened:
				// without it, this would pass against a hook that allowed the
				// call for a reason of its own.
				for _, want := range []string{"sdlc crashed: forced by the test", "This is a bug"} {
					if !strings.Contains(stderr, want) {
						t.Errorf("stderr does not say %q:\n%s", want, stderr)
					}
				}
				if strings.Contains(stderr, "goroutine ") {
					t.Errorf("a stack was printed with debugging off:\n%s", stderr)
				}
			})
		}

		t.Run("AC-4 "+e.event+": with debugging on the stack goes to stderr and never to stdout", func(t *testing.T) {
			stdout, stderr, code := sb.hook(t, "SDLC_ENFORCE", e.event, e.payload, "SDLC_DEBUG=1")
			if stdout != "{\"continue\":true}\n" || code != 0 {
				t.Errorf("exit %d, stdout %q; want exactly {\"continue\":true} and a newline, and exit 0", code, stdout)
			}
			if !strings.Contains(stderr, "goroutine ") || !strings.Contains(stderr, "sdlc crashed: forced by the test") {
				t.Errorf("stderr does not carry the report and the stack:\n%s", stderr)
			}
		})
	}

	// The panic let the write through, and that is all it did: the story is
	// where it was, and nothing was recorded on it.
	if active, err := os.ReadFile(filepath.Join(sb.tree, ".sdlc", "state", "active")); err != nil || string(active) != "US-001\n" {
		t.Errorf("the story the sandbox holds is %q (%v), want US-001", active, err)
	}
}
