package e2e

// US-011: the hook refuses a call it was going to check and could not decide
// by its deadline, shown here through the built binary and the launcher
// hooks.json names.
//
// The deadline is reached through SDLC_HOOK_DEADLINE_MS, set to a millisecond,
// and not through an input that takes the fixed figure to read. A millisecond
// is still longer than the hook takes over an ordinary call (about a third of
// one, measured in process), so each call here carries a few megabytes the hook
// has to read before it can know anything: text in a file being written, or in
// the description beside a short command. Reading that takes many milliseconds
// on any machine, the clock started before it, and the deadline has passed by
// the time the hook learns what the call is. Without the variable the same
// calls are answered as they always were, which the test holds too.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// deadlineVariable is the one setting of the hook's deadline, in whole
// milliseconds, which can only shorten it.
const deadlineVariable = "SDLC_HOOK_DEADLINE_MS"

// Text of a call that a refusal for running out of time has no reason to
// repeat.
const (
	lateCommand = "echo zq7-the-command-as-typed"
	lateFile    = "zq7-the-file-as-named.go"
	lateContent = "zq7-the-content-as-written"
)

// bulk is text enough that reading the call it is in takes the hook well over
// a millisecond.
var bulk = strings.Repeat("a line of a file that is long, as files a session writes sometimes are\n", 8<<20/72)

// deadlineRefusal is `<reason>. Instead: <route> [<rule>]`.
var deadlineRefusal = regexp.MustCompile(`^(.+)\. Instead: (.+) \[([^\]\s]+)\]$`)

// lateHook hands the sandbox's sdlc one PreToolUse call made from dir, in a
// session whose project directory is dir too, with more set in the hook's
// environment. On Linux and macOS it goes through plugin/bin/sdlc-hook; on
// Windows that script does not run and the binary is given the event, as
// sandbox.hook does.
func (sb sandbox) lateHook(t *testing.T, dir string, payload map[string]any, throughLauncher bool, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	name, args := sb.sdlc, []string{"hook", "PreToolUse"}
	if throughLauncher && runtime.GOOS != "windows" {
		name, args = filepath.Join(moduleRoot(t), "plugin", "bin", "sdlc-hook"), []string{"PreToolUse"}
	}
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = append(append(sb.env(t, ""), "CLAUDE_PROJECT_DIR="+dir), env...)
	cmd.Stdin = bytes.NewReader(raw)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	runErr := cmd.Run()
	var exit *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("the hook could not be run: %v", runErr)
	}
	return out.String(), errOut.String(), code
}

// outOfTime holds a reply to being a refusal of a tool call in the form every
// refusal takes, and returns its reason, its route and the rule it names.
func outOfTime(t *testing.T, what, stdout, stderr string, code int) (reason, route, rule string) {
	t.Helper()
	if code != 0 {
		t.Fatalf("%s: the hook exited %d, so its reply is never read:\n%s", what, code, stderr)
	}
	if stdout == carryOn {
		t.Fatalf("%s: the hook let the call through with a deadline of a millisecond long past", what)
	}
	var r refusal
	oneLine(t, stdout, &r)
	got := r.HookSpecificOutput
	if got.HookEventName != "PreToolUse" || got.PermissionDecision != "deny" {
		t.Fatalf("%s: the reply is not a refusal of a tool call: %q", what, stdout)
	}
	m := deadlineRefusal.FindStringSubmatch(got.PermissionDecisionReason)
	if m == nil {
		t.Fatalf("%s: the refusal is not `<reason>. Instead: <route> [<rule>]`: %q", what, got.PermissionDecisionReason)
	}
	return m[1], m[2], m[3]
}

func TestAC10TheHookRefusesACallItCouldNotCheckInTimeThroughTheBuiltBinaryAndTheLauncher(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")

	millisecond := deadlineVariable + "=1"
	command := func(session, dir string) map[string]any {
		return toolCall(session, dir, "", "Bash", map[string]any{"command": lateCommand, "description": bulk})
	}
	file := func(session, dir string) map[string]any {
		return toolCall(session, dir, "", "Write",
			map[string]any{"file_path": filepath.Join(dir, lateFile), "content": lateContent + "\n" + bulk})
	}
	silent := func(t *testing.T, what, stdout, stderr string, code int) {
		t.Helper()
		if stdout != carryOn || stderr != "" || code != 0 {
			if len(stdout) > 600 {
				stdout = stdout[:600]
			}
			t.Errorf("%s: exit %d\nstdout %q\nstderr %q\nwant exactly %q, nothing on stderr and exit 0", what, code, stdout, stderr, carryOn)
		}
	}

	// The command is one the loop has no rule against, so with the variable
	// set there is one reason only for the hook to refuse it.
	stdout, stderr, code := sb.lateHook(t, tree, command(sandboxSession, tree), true)
	silent(t, "the command, with no deadline set", stdout, stderr, code)

	stdout, stderr, code = sb.lateHook(t, tree, command(sandboxSession, tree), true, millisecond)
	commandReason, commandRoute, rule := outOfTime(t, "AC-10, a command in the session working the story", stdout, stderr, code)
	commandReply := stdout

	t.Run("AC-10 a command the hook could not read in time is refused, and told to run in shorter parts", func(t *testing.T) {
		if low := strings.ToLower(commandReason); !strings.Contains(low, "time") || !strings.Contains(low, "command") {
			t.Errorf("the reason does not say the hook could not read the command in time: %q", commandReason)
		}
		if !strings.Contains(strings.ToLower(commandRoute), "shorter parts") {
			t.Errorf("the route does not say to run it in shorter parts: %q", commandRoute)
		}
		for _, text := range []string{"zq7", bulk[:40]} {
			if strings.Contains(commandReply, text) {
				t.Errorf("the refusal repeats %q from the call: %q", text, commandReply)
			}
		}
	})

	t.Run("AC-10 a file the hook could not check in time is refused under the same rule, in its own words", func(t *testing.T) {
		stdout, stderr, code := sb.lateHook(t, tree, file(sandboxSession, tree), true, millisecond)
		reason, route, fileRule := outOfTime(t, "a file written in the session working the story", stdout, stderr, code)
		if fileRule != rule {
			t.Errorf("a file is refused under %s and a command under %s, want one rule for running out of time", fileRule, rule)
		}
		if low := strings.ToLower(reason); !strings.Contains(low, "time") || !strings.Contains(low, "change") {
			t.Errorf("the reason does not say the hook could not check the change in time: %q", reason)
		}
		if low := strings.ToLower(route); !strings.Contains(low, "again") || !strings.Contains(low, "person") {
			t.Errorf("the route does not say to try again and to tell a person if it is refused again: %q", route)
		}
		if reason == commandReason || route == commandRoute {
			t.Errorf("a file is refused in the words a command is: %q, %q", reason, route)
		}
		for _, text := range []string{"zq7", bulk[:40], filepath.Base(sb.dir)} {
			if strings.Contains(stdout, text) {
				t.Errorf("the refusal repeats %q from the call: %.600q", text, stdout)
			}
		}

		// The same call with no deadline set is not refused for running out
		// of time, whatever else the loop says about it.
		stdout, _, _ = sb.lateHook(t, tree, file(sandboxSession, tree), true)
		if strings.Contains(stdout, "["+rule+"]") {
			t.Errorf("with no deadline set the file was refused for running out of time: %.600q", stdout)
		}
	})

	t.Run("AC-10 the rule the refusal names has its section in the enforcement page", func(t *testing.T) {
		page := readFile(t, filepath.Join(moduleRoot(t), "docs", "enforcement.md"))
		if !strings.Contains(page, "### `"+rule+"`") {
			t.Errorf("docs/enforcement.md has no section for the %s rule", rule)
		}
	})

	t.Run("AC-10 the launcher and the binary give the same reply", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the POSIX launcher does not run here; internal/plugin holds sdlc-hook.cmd to the binary's replies")
		}
		direct, _, _ := sb.lateHook(t, tree, command(sandboxSession, tree), false, millisecond)
		if direct != commandReply {
			t.Errorf("the launcher printed %q and the binary %q", commandReply, direct)
		}
	})

	t.Run("AC-11 a value the hook does not take leaves the call answered as it was", func(t *testing.T) {
		for _, value := range []string{"0", "-1", "+1", "1.0", " 1", "0x1", "", "soon", "999999999"} {
			stdout, stderr, code := sb.lateHook(t, tree, command(sandboxSession, tree), true, deadlineVariable+"="+value)
			silent(t, "the command, with "+deadlineVariable+"="+value, stdout, stderr, code)
		}
	})

	// The plugin is installed for every session, and one that is not working
	// a story hears nothing from it: not with a deadline of a millisecond and a
	// call that takes far longer than that to read.
	t.Run("AC-7 a session that holds no story hears nothing of the deadline", func(t *testing.T) {
		stdout, stderr, code := sb.lateHook(t, tree, file(sandboxOther, tree), true, millisecond)
		silent(t, "a file written by a session that holds no story", stdout, stderr, code)

		apart := scratch(t)
		for _, session := range []string{sandboxSession, sandboxOther} {
			stdout, stderr, code = sb.lateHook(t, apart, command(session, apart), true, millisecond)
			silent(t, "a command of session "+session+" in a directory that is no project's", stdout, stderr, code)
			stdout, stderr, code = sb.lateHook(t, apart, file(session, apart), true, millisecond)
			silent(t, "a file of session "+session+" in a directory that is no project's", stdout, stderr, code)
		}
	})

	t.Run("AC-7 with enforcement off the deadline refuses nothing", func(t *testing.T) {
		for what, call := range map[string]map[string]any{
			"a command": command(sandboxSession, tree), "a file": file(sandboxSession, tree),
		} {
			stdout, stderr, code := sb.lateHook(t, tree, call, true, millisecond, "SDLC_ENFORCE=0")
			silent(t, what+" in the session working the story, with SDLC_ENFORCE=0", stdout, stderr, code)
		}
	})
}

// ------------------------------------------------------------------ the docs

// paragraphs is a document's paragraphs, each on one line.
func paragraphs(text string) []string {
	var out []string
	for p := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		if flat := strings.Join(strings.Fields(p), " "); flat != "" {
			out = append(out, flat)
		}
	}
	return out
}

// someParagraph reports whether one paragraph holds every one of the words,
// whatever their case.
func someParagraph(text string, words ...string) bool {
	for _, p := range paragraphs(text) {
		low, all := strings.ToLower(p), true
		for _, w := range words {
			all = all && strings.Contains(low, strings.ToLower(w))
		}
		if all {
			return true
		}
	}
	return false
}

// Three places say what happens when the hook runs out of time, and each said,
// or implied, that the call goes through. What they have to say now is what is
// refused, and what is not: the limit that is left is a person's to know about,
// and a page that calls the hole closed is worse than the hole.
func TestAC9TheDocsSayWhatIsRefusedForRunningOutOfTimeAndWhatIsStillLeftOpen(t *testing.T) {
	parallel(t)
	docs := moduleRoot(t)
	for name, text := range map[string]string{
		"docs/enforcement.md": readFile(t, filepath.Join(docs, "docs", "enforcement.md")),
		"SECURITY.md":         readFile(t, filepath.Join(docs, "SECURITY.md")),
		`docs/troubleshooting.md, under "Warnings the hook prints"`: section(
			strings.ReplaceAll(readFile(t, filepath.Join(docs, "docs", "troubleshooting.md")), "\r\n", "\n"),
			"## Warnings the hook prints"),
	} {
		t.Run("AC-9 "+name, func(t *testing.T) {
			if text == "" {
				t.Fatal("there is nothing to read")
			}
			if !someParagraph(text, "hook", "command", "in time", "refuse") {
				t.Errorf("no paragraph says that a command the hook cannot read in time is refused")
			}
			if !someParagraph(text, "kill", "before", "whether the call is one it checks", "through", "nothing") {
				t.Errorf("no paragraph names what is still left open: a hook that Claude Code kills before it has " +
					"learnt whether the call is one it checks still lets the call through and says nothing")
			}
		})
	}
}

func TestAC11TheConfigurationPageNamesTheVariableAndSaysItCanOnlyShorten(t *testing.T) {
	parallel(t)
	page := readFile(t, filepath.Join(moduleRoot(t), "docs", "configuration.md"))
	if !strings.Contains(page, "`"+deadlineVariable+"`") {
		t.Fatalf("docs/configuration.md does not name %s", deadlineVariable)
	}
	if !someParagraph(page, deadlineVariable, "shorten") {
		t.Errorf("docs/configuration.md names %s and no paragraph that does says it can only shorten the deadline", deadlineVariable)
	}
}
