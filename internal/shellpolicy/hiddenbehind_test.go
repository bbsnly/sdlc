package shellpolicy

// US-112, Gate 7 (code review, blocking; security, notes N1 to N4). A word
// that runs the command after it was read as the program, or a command line
// the shell runs was not read at all, so the command met no rule:
//
//   - bash's `time -p` and `time --`: the keyword was taken off and `-p` read
//     as the program. /usr/bin/time and GNU time (`gtime` under Homebrew) with
//     their options, the values of -o and -f and the long forms; strace,
//     ltrace, valgrind, gtimeout, `lldb --` and `chroot DIR`, each of which
//     runs the command after its options;
//   - mapfile and readarray with -C inside a bundle (`-tC`, `-tC'…'`);
//   - zsh's short forms, `if [[ … ]] CMD`, `elif`, `while` and `until`;
//   - a `sh -c`, `bash -c` or `zsh -c` string under find's -exec, -execdir,
//     -ok and -okdir, which the path rules read and the program rules did not.
//
// Each spelling is tried with every kind of command the Gate 7 reviews named:
// AI_AGENT taken away, the hook run by hand on an event it records, the
// freeze's lock removed, the trace removed, a commit before the commit gate,
// `sdlc stop` and `sdlc unfreeze`. A tool whose options are too open to read
// (strace and ltrace take a value after letters the reading cannot all know)
// is refused where either reading of an option it does not know hides a
// commit: that is how the rules treat a write they cannot place.
//
// The person's decision of 2026-10-29 (spec_unclear, option A) takes the
// hook's own AI_AGENT check off recording, because Claude Code 2.1.309 in
// Claude Desktop sets AI_AGENT for a hook too. So the shell rules alone hold
// THREATS S1, and interpreter code that runs the hook by hand on a recorded
// event, which the hook's check used to hold with AI_AGENT kept, is refused
// here as interpreter code that names AI_AGENT is. awk and gawk join that
// class: their system(), a pipe to "sh" and ENVIRON are an interpreter's.
//
// Run in this session under bash 5.3 and zsh 5.9, with AI_AGENT set, against
// a stub child that prints its arguments and AI_AGENT: `time -p`, `time --`
// and `time -p --` ran the child in bash (zsh's time has no options and ran
// `-p` as a command), and `time -p unset AI_AGENT` took it away; /usr/bin/time
// (BSD) ran it with -p, -l, -lp, -o FILE, -a, -h and --; mapfile and
// readarray with -tC, -tC'…' and -c1 -tC ran the callback in bash (zsh has no
// mapfile), and `unset AI_AGENT` there took it from what ran next; zsh ran the
// command after `if [[ 1 ]]`, `if [[ -n x ]]`, `elif [[ 1 ]]`, `while [[ … ]]`
// and `until [[ … ]]` (bash refuses each as a syntax error), and `unset` there
// took AI_AGENT away; find ran `sh -c` and `bash -c` strings under -exec,
// -execdir and -ok; macOS awk's system() and a pipe to "sh" ran the child, and
// `unset AI_AGENT` inside system() took it away. Not run, as nothing here
// would: GNU time's -f, --format, --output, --append, -v and -q, and gtime;
// strace, ltrace, valgrind, gtimeout and gawk, which are not installed;
// chroot, which needs root ("Operation not permitted"); and lldb's `run`,
// which stalled launching the process. Those rows rest on each tool's
// documentation. macOS awk's `delete ENVIRON["AI_AGENT"]` did not reach the
// child; gawk 4.2 and later pass ENVIRON's changes on, by its manual.

import (
	"regexp"
	"strings"
	"testing"
)

// hider puts a command where a reading once lost it. here is set where the
// command runs in the shell itself, so `unset AI_AGENT` there takes the
// variable from what runs next; script is set where the hidden text is a
// command line of a shell of its own.
type hider struct {
	hide         func(command string) string
	here, script bool
}

// before is a hider that puts words in front of the command.
func before(words string, here bool) hider {
	return hider{hide: func(c string) string { return words + c }, here: here}
}

// refusedEveryKind holds every kind of command the Gate 7 reviews named,
// hidden by h, to the rule it meets in plain sight.
func refusedEveryKind(t *testing.T, h hider) {
	t.Helper()
	refusedAsAIAgentTakenAway(t, h.hide("env -u AI_AGENT "+then))
	if h.here {
		refusedAsAIAgentTakenAway(t, h.hide("unset AI_AGENT")+"; "+then)
	}
	if h.script {
		refusedAsAIAgentTakenAway(t, h.hide("unset AI_AGENT; "+then))
	}
	refusedInEverySession(t, h.hide("sdlc hook SubagentStop < /tmp/p.json"), false, ruleRecordedEvent)
	refusedUnder(t, h.hide("rm .sdlc/state/tests.lock"), ready, "loop-state-through-the-tool")
	refusedUnder(t, h.hide("rm -f .sdlc/stories/A-1/trace.jsonl"), ready, traceRule)
	refusedUnder(t, h.hide("git commit -m x"), notReady, "commit-gate")
	refusedUnder(t, h.hide("sdlc stop"), ready, "stop-is-a-human-decision")
	unfreeze := h.hide("sdlc unfreeze --reason x")
	refusedUnder(t, unfreeze, ready, "unfreeze-is-a-human-decision")
	if f, ok := HumanDecisions(unfreeze, false, aStoryWaits); !ok || f.Rule != "unfreeze-is-a-human-decision" {
		t.Errorf("a second session may run %q: %+v", unfreeze, f)
	}
}

// The control: every kind in plain sight meets its rule, so a row below that
// is let through was hidden, not wrongly written.
func TestAC2AC15AC17EveryKindOfCommandMeetsItsRuleInPlainSight(t *testing.T) {
	refusedEveryKind(t, hider{hide: func(c string) string { return c }, here: true})
}

// Code review B1, security N2: bash's time keyword with its options.
func TestAC2AC15AC17ACommandAfterTimesOptionsMeetsEveryRule(t *testing.T) {
	for _, words := range []string{"time -p ", "time -- ", "time -p -- "} {
		t.Run("AC-17 "+strings.TrimSpace(words), func(t *testing.T) {
			refusedEveryKind(t, before(words, true))
		})
	}
}

// Code review B1: programs that run the command after their options, the
// values their valued options take included, and GNU's long forms glued or
// not.
func TestAC2AC15AC17ACommandAfterATimerOrATracersOptionsMeetsEveryRule(t *testing.T) {
	for _, words := range []string{
		// The time program, BSD's flags and GNU's.
		"/usr/bin/time ", "/usr/bin/time -p ", "/usr/bin/time -l ", "/usr/bin/time -lp ",
		"/usr/bin/time -h ", "/usr/bin/time -- ", "/usr/bin/time -v ", "/usr/bin/time -q ",
		"/usr/bin/time -o /tmp/t ", "/usr/bin/time -o/tmp/t ", "/usr/bin/time -a -o /tmp/t ",
		"/usr/bin/time -f '%e %M' ", "/usr/bin/time -f%e ",
		"/usr/bin/time --format=%e ", "/usr/bin/time --format %e ",
		"/usr/bin/time --output=/tmp/t ", "/usr/bin/time --output /tmp/t ",
		"/usr/bin/time --append --output /tmp/t ", "/usr/bin/time --verbose ",
		"/usr/bin/time --portability ", "/usr/bin/time --quiet ",
		// The program, not the keyword, by another way of naming it.
		"command time -p ", `\time -p `, "gtime -f %e ", "gtime --output=/tmp/t -v ",
		// Tracers, with the options that take a value.
		"strace ", "strace -f ", "strace -ff -o /tmp/s ", "strace -o /tmp/s ", "strace -o/tmp/s ",
		"strace --output=/tmp/s ", "strace -e trace=execve ", "strace -etrace=execve ",
		"strace --trace=execve ", "strace -s 100 ", "strace -u nobody ", "strace -E FOO=1 ",
		"strace -a 40 ", "strace -X raw ", "strace -P /tmp ", "strace -c -S calls ",
		"ltrace ", "ltrace -f ", "ltrace -o /tmp/l ", "ltrace -e malloc ", "ltrace -s 100 ",
		"ltrace -u nobody ", "ltrace -n 2 ",
		"valgrind ", "valgrind -q ", "valgrind --tool=none ",
		"valgrind --trace-children=yes --log-file=/tmp/v ",
		// GNU timeout under Homebrew's name, read as timeout is.
		"gtimeout 60 ", "gtimeout -s KILL 60 ", "gtimeout --signal=KILL 1m ", "gtimeout -k 5 60 ",
		"gtimeout --foreground --preserve-status 60 ",
		// A debugger handed the program after --, and chroot's new root.
		"lldb -- ", "lldb -b -o run -- ", "lldb --batch -o run -o quit -- ",
		"lldb -b -O 'settings set auto-confirm true' -o run -- ",
		"chroot / ", "chroot -u nobody / ", "chroot --userspec=nobody:nogroup / ", "chroot --skip-chdir / ",
	} {
		t.Run("AC-17 "+strings.TrimSpace(words), func(t *testing.T) {
			refusedEveryKind(t, before(words, false))
		})
	}
}

// A tracer's option the reading does not know may take a value or not; read
// either way, the commit after it is a commit.
func TestAC17AnOptionATracerMayTakeAValueAfterIsReadBothWays(t *testing.T) {
	for _, command := range []string{
		"strace -J git commit -m x",
		"strace -J 1 git commit -m x",
		"strace --not-an-option git commit -m x",
		"ltrace -J 1 git commit -m x",
	} {
		refusedUnder(t, command, notReady, "commit-gate", "git-command-cannot-be-read")
	}
}

// Security N1: mapfile and readarray's callback, with -C in a bundle.
func TestAC2AC15AC17AMapfileCallbackInABundleMeetsEveryRule(t *testing.T) {
	for name, h := range map[string]func(string) string{
		"mapfile -tC":       func(c string) string { return "mapfile -tC '" + c + ";:' -c1 x <<< a" },
		"mapfile -tC glued": func(c string) string { return "mapfile -tC'" + c + ";:' -c1 x <<< a" },
		"mapfile -c1 -tC":   func(c string) string { return "mapfile -c1 -tC '" + c + ";:' x <<< a" },
		"readarray -tC":     func(c string) string { return "readarray -tC '" + c + ";:' -c1 x <<< a" },
	} {
		t.Run("AC-17 "+name, func(t *testing.T) {
			refusedEveryKind(t, hider{hide: h, here: true})
		})
	}
}

// Security N3: zsh runs the command after a condition in [[ ]] with no then,
// do or braces (SHORT_LOOPS, on by default).
func TestAC2AC15AC17ZshsShortIfWhileAndUntilMeetEveryRule(t *testing.T) {
	for _, words := range []string{
		"if [[ 1 ]] ",
		"if [[ -n x ]] ",
		"if [[ 0 = 1 ]] { true } elif [[ 1 ]] ",
		"while [[ ! -e /tmp/f ]] ",
		"until [[ -e /tmp/f ]] ",
	} {
		t.Run("AC-17 "+strings.TrimSpace(words), func(t *testing.T) {
			refusedEveryKind(t, before(words, true))
		})
	}
}

// Security N4: a shell's string under find is read by the program rules too,
// not only by the path rules.
func TestAC2AC15AC17AShellStringUnderFindMeetsEveryRule(t *testing.T) {
	for name, h := range map[string]func(string) string{
		"-exec sh -c":         func(c string) string { return "find . -maxdepth 0 -exec sh -c '" + c + "' \\;" },
		"-execdir sh -c":      func(c string) string { return "find . -maxdepth 0 -execdir sh -c '" + c + "' \\;" },
		"-ok sh -c":           func(c string) string { return "find . -maxdepth 0 -ok sh -c '" + c + "' \\;" },
		"-okdir sh -c":        func(c string) string { return "find . -maxdepth 0 -okdir sh -c '" + c + "' \\;" },
		"-exec bash -c, plus": func(c string) string { return "find . -maxdepth 0 -exec bash -c '" + c + "' bash {} +" },
		"-exec zsh -c":        func(c string) string { return "find . -maxdepth 0 -exec zsh -c '" + c + "' \\;" },
		"-exec sh -c, named":  func(c string) string { return "find . -maxdepth 0 -exec sh -c '" + c + "' sh {} \\;" },
	} {
		t.Run("AC-17 find "+name, func(t *testing.T) {
			refusedEveryKind(t, hider{hide: h, script: true})
		})
	}
}

// The same programs used as they are meant to be stay open, git reads and
// sdlc's own reads behind them included: the rows above are refused for what
// they hide, not for the program in front.
func TestAC2AC15AC17WhatATimerATracerACallbackOrFindRunsInTheOpenIsLeftAlone(t *testing.T) {
	for _, command := range []string{
		"time -p go test ./...",
		"time -- go build ./...",
		"time -p git status",
		"/usr/bin/time -o /tmp/t go test ./...",
		"/usr/bin/time -f %e go vet ./...",
		"/usr/bin/time -lp go test ./...",
		"gtime --format=%e --output=/tmp/t go test ./...",
		"strace -f -o /tmp/s ./bin/app",
		"strace -c -u nobody ls",
		"strace -f git log -1",
		"ltrace -c ls",
		"valgrind --leak-check=full ./a.out",
		"gtimeout 60 go test ./...",
		"lldb -- ./bin/app",
		"chroot /srv/jail /bin/true",
		"time -p sdlc status",
		"mapfile -t lines < go.mod",
		"readarray -tC 'echo progress' -c 10 lines < go.mod",
		"if [[ -n $CI ]] go test ./...",
		"while [[ ! -e /tmp/ready ]] sleep 1",
		`find . -name '*.go' -exec sh -c 'gofmt -l "$1"' sh {} \;`,
		`find . -maxdepth 1 -exec bash -c 'echo "$1"' bash {} +`,
		`find ./docs -name 'commands.md' -ok sh -c 'wc -l "$1"' sh {} \;`,
		"find . -maxdepth 0 -exec sh -c 'git log -1' \\;",
		"awk '{print $1}' go.mod",
		`awk 'BEGIN{print ENVIRON["HOME"]}'`,
		`awk 'BEGIN{system("go version")}'`,
	} {
		allowedGlob(t, command, notReady)
		leftAloneInEverySession(t, command, false)
	}
}

// The AI_AGENT decision: interpreter code that runs the hook by hand on an
// event it records forges what Claude Code reports, with AI_AGENT kept, now
// that the hook records whatever AI_AGENT holds. It is refused as interpreter
// code that names AI_AGENT is, by what the code names; code that builds the
// event's name stays the documented interpreter residual.
func TestAC2AC15InterpreterCodeThatRunsTheHookOnARecordedEventIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		`python3 -c 'import os; os.system("sdlc hook SubagentStop < /tmp/p.json")'`,
		`python3 -c 'import subprocess; subprocess.run(["sdlc", "hook", "SubagentStart"], stdin=open("/tmp/p.json"))'`,
		"python3 - <<'EOF'\nimport os\nos.system(\"sdlc hook Stop < /tmp/p.json\")\nEOF",
		`node -e 'require("child_process").execSync("sdlc hook PostToolUse < /tmp/p.json")'`,
		`perl -e 'system("sdlc hook Notification < /tmp/p.json")'`,
		`ruby -e 'system("plugin/bin/sdlc-hook SubagentStop < /tmp/p.json")'`,
		`awk 'BEGIN{system("sdlc hook SubagentStop < /tmp/p.json")}'`,
		`gawk 'BEGIN{system("sdlc hook PostCompact < /tmp/p.json")}'`,
		`awk 'BEGIN{print "sdlc hook StopFailure < /tmp/p.json" | "sh"}'`,
	} {
		refusedInEverySession(t, command, false, ruleRecordedEvent)
	}
	for _, command := range []string{
		`python3 -c 'import os; os.system("sdlc hook PreToolUse < /tmp/p.json")'`,
		`python3 -c 'import json; print(json.load(open("p.json"))["hook_event_name"] == "SubagentStop")'`,
		`node -e 'console.log("Stop")'`,
		`awk 'BEGIN{system("sdlc status")}'`,
	} {
		leftAloneInEverySession(t, command, false)
	}
}

// Code review note, security N2's awk: awk and gawk are interpreters. Code
// that names AI_AGENT, through ENVIRON or in a command line it hands the
// shell, is refused as python's is; gawk 4.2 and later pass a change to
// ENVIRON on to system() and to a pipe.
func TestAC2AC15AwkCodeThatNamesAIAgentIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		`awk 'BEGIN{delete ENVIRON["AI_AGENT"]; system("sdlc hook PreToolUse < /tmp/p.json")}'`,
		`gawk 'BEGIN{delete ENVIRON["AI_AGENT"]; system("sdlc hook PreToolUse < /tmp/p.json")}'`,
		`gawk 'BEGIN{ENVIRON["AI_AGENT"] = ""; system("sdlc hook PreToolUse < /tmp/p.json")}'`,
		`awk 'BEGIN{system("unset AI_AGENT; sdlc hook PreToolUse < /tmp/p.json")}'`,
		`awk 'BEGIN{system("env -u AI_AGENT sdlc hook PreToolUse < /tmp/p.json")}'`,
		`awk 'BEGIN{print "unset AI_AGENT; sdlc hook PreToolUse < /tmp/p.json" | "sh"}'`,
		"gawk -f /dev/stdin <<'EOF'\nBEGIN { delete ENVIRON[\"AI_AGENT\"]; system(\"sdlc hook PreToolUse < /tmp/p.json\") }\nEOF",
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// namingIsEnough is a route that says a command that only names AI_AGENT is
// refused, whatever it does with it.
var namingIsEnough = regexp.MustCompile("(?i)\\b(names|naming|mentions|mentioning)\\s+(only\\s+)?(it|the variable|`?AI_AGENT`?)\\b")

// The human advocate's Gate 7 note 4, and the AI_AGENT decision: a command
// that only names AI_AGENT is refused, and the route says that naming it is
// what tripped the rule. Neither refusal says the hook records nothing where
// AI_AGENT is set, or that a hand-run hook records nothing in the trace: since
// the decision, the hook records whatever AI_AGENT holds, and a hand-run
// PreToolUse that is refused adds a refusal record.
func TestAC2AC15TheAIAgentRefusalSaysNamingItIsEnoughAndNoRefusalPromisesNothingIsRecorded(t *testing.T) {
	for _, command := range []string{"declare -p AI_AGENT", "MSG=AI_AGENT make"} {
		f := refusedInEverySession(t, command, false, "enforcement-stays-on")
		if !namingIsEnough.MatchString(f.Route) {
			t.Errorf("the route of the refusal of %q does not say that naming AI_AGENT is enough to be refused: %s", command, f.Route)
		}
		if strings.Contains(f.Reason+" "+f.Route, "records nothing") {
			t.Errorf("the refusal of %q says the hook records nothing, which no longer holds: %+v", command, f)
		}
	}
	f := refusedInEverySession(t, "sdlc hook SubagentStop < /tmp/p.json", false, ruleRecordedEvent)
	if strings.Contains(f.Reason+" "+f.Route, "records nothing") {
		t.Errorf("the refusal of a hand-run hook says a hand-run hook records nothing, which no longer holds: %+v", f)
	}
}
