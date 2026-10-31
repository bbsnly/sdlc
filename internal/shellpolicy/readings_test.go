package shellpolicy

// US-112, design review round 5. Round 4 read an option a tool's table does
// not know both ways, and a `]]` after zsh's short `if` as the start of
// another command; neither had a bound, and the round-5 reviews found
// spellings both ways missed. These rows hold the round-5 rules to what a
// command does, by rule id:
//
//   - a command that can be read more than 16 ways, or a line whose commands
//     give more than 16 other readings between them, nested scripts included,
//     is refused under command-too-deep-to-read, and not judged on the
//     readings made before the count ran out; up to 16 is read, and an
//     ordinary script of tests and functions starts no reading at all;
//   - a reading that took an unknown option's value goes on reading options;
//   - each tool's valued options, GNU's unique prefixes of its long ones, a
//     shell's -O and +O (zsh's are flags, bash's take a name), PowerShell's
//     parameters, ssh's options after the host, chroot's root and lldb's
//     program among its options;
//   - every reader reads every reading: find's -exec, a here-document's
//     interpreter, trap and eval;
//   - zsh's && and || inside [[ ]], the blocks after `}` (else, elif,
//     always), `function`, and coproc;
//   - a program handed an event the hook records as its first word.
//
// Run in this session under bash 5.3 and zsh 5.9 against a scratch file: zsh
// ran `zsh -c -O`, `zsh -cO`, `zsh -c +O` and `zsh -O -c` with the script;
// bash ran `bash -O extglob -c` and `bash +O extglob -c`; both ran `coproc
// rm`, `coproc { rm; }` and `function f { rm; }; f`, bash `coproc N { rm;
// }` (zsh 5.9 does not); zsh ran `function { rm }`, `if [[ -n a && -n b ]]
// rm`, the `||` and `( -n b )` forms, `if … { true } else { rm }`, `{ true }
// always { rm }` and the `elif` form (bash refuses each as a syntax error);
// nice -n +5, nice -n5, caffeinate -t 1e3, timeout -sKILL and timeout --sig
// KILL (macOS's timeout takes the prefix) ran the command; and both shells
// ran 20 `if [[ -f x ]]; then …; fi` tests and 20 function definitions
// before an rm. `sudo --us root` and `sudo --user root` asked for a password,
// so sudo read the option and its prefix; `ssh -G localhost -v -p 2222`
// printed port 2222, so ssh reads options after the host; `lldb --help` gives
// the program as the first positional and its arguments after `--`. Not run,
// as nothing here has them: strace, ionice, procps watch, GNU stdbuf's long
// options (macOS's stdbuf has none), pwsh, doas, nsenter, unshare, chrt and
// taskset, which rest on their manuals; chroot, which needs root.
// /usr/bin/time and strace refuse -J, so the -J rows are the both-ways
// reading of an option a table lacks, not a spelling that runs: a tool whose
// table misses a real valued option is read the same way.

import (
	"strings"
	"testing"
)

const (
	tooDeepRule = "command-too-deep-to-read"
	lockRule    = "loop-state-through-the-tool"
	rmLock      = freezeRemove
)

// jChain is strace with n options it does not know, each of which may take
// the word after it, and then the words given.
func jChain(n int, then string) string {
	return "strace" + strings.Repeat(" -J", n) + " " + then
}

// forkOnce is a command that one unknown option makes two readings of.
const forkOnce = "strace -J 1 true; "

// commitMeetsTheGate holds a commit, however it is hidden, to the commit gate
// before the code review has passed, as the round-4 rows do.
func commitMeetsTheGate(t *testing.T, command string) {
	t.Helper()
	refusedUnder(t, command, notReady, "commit-gate", "git-command-cannot-be-read")
}

// Red team V20 to V22, security's round-4 block: past 16 readings of one
// command the rest was not read, so the command is refused as too deep to
// read, in both sessions, and not judged on what was read; a commit in the
// last reading is not let through, nor refused as a commit.
func TestAC2AC15AC17ACommandReadMoreThanSixteenWaysIsRefusedAsTooDeepToRead(t *testing.T) {
	nesting, ok := Inspect("echo "+strings.Repeat("$(", maxNesting+1)+"date"+strings.Repeat(")", maxNesting+1), ready)
	if !ok || nesting.Rule != tooDeepRule {
		t.Fatalf("the nesting control was not refused as too deep: %+v", nesting)
	}
	for name, command := range map[string]string{
		"17 then true":      jChain(17, "1 true"),
		"17 then a commit":  jChain(17, "1 "+aCommit),
		"17 then a removal": jChain(17, "1 "+rmLock),
		"40 then true":      jChain(40, "1 true"),
	} {
		t.Run("AC-17 "+name, func(t *testing.T) {
			f := refusedInEverySession(t, command, false, tooDeepRule)
			if f.Reason == nesting.Reason {
				t.Errorf("the refusal of %q gives the reason for nesting, which it does not do: %s", command, f.Reason)
			}
			refusedUnder(t, command, notReady, tooDeepRule)
		})
	}
}

// Red team V23 and V26: the readings are deduplicated, so a chain of
// unknown options gives one reading for each and not a Fibonacci number of
// them; 16 other readings are read, and the 17th is one too many.
func TestAC2AC15AC17SixteenReadingsOfACommandAreReadAndItsCommitStillMeetsTheGate(t *testing.T) {
	for _, n := range []int{8, 15, 16} {
		command := jChain(n, "1 true")
		allowedGlob(t, command, ready)
		leftAloneInEverySession(t, command, false)
		commitMeetsTheGate(t, jChain(n, "1 "+aCommit))
		refusedUnder(t, jChain(n, "1 "+rmLock), ready, lockRule)
	}
}

// Red team V25: the line's count of other readings runs across its
// commands, in both walks, and across a script nested in it.
func TestAC2AC15AC17ALineWhoseCommandsGiveMoreThanSixteenOtherReadingsIsRefused(t *testing.T) {
	for name, command := range map[string]string{
		"17 commands":                strings.Repeat(forkOnce, 17),
		"17 commands, last a commit": strings.Repeat(forkOnce, 16) + "strace -J 1 " + aCommit,
		"9 and 8 in sh -c":           strings.Repeat(forkOnce, 9) + "sh -c '" + strings.Repeat(forkOnce, 8) + "'",
		"8 and 9 in bash -c":         strings.Repeat(forkOnce, 8) + "bash -c '" + strings.Repeat(forkOnce, 9) + "'",
		"6, 6 and 5 two deep":        strings.Repeat(forkOnce, 6) + "sh -c '" + strings.Repeat(forkOnce, 6) + "bash -c \"" + strings.Repeat(forkOnce, 5) + "\"'",
		"17 removals":                strings.Repeat("strace -J 1 rm /tmp/a; ", 17),
	} {
		t.Run("AC-17 "+name, func(t *testing.T) {
			refusedInEverySession(t, command, false, tooDeepRule)
			refusedUnder(t, command, notReady, tooDeepRule)
		})
	}
	for name, command := range map[string]string{
		"16 commands":         strings.Repeat(forkOnce, 16),
		"8 and 8 in sh -c":    strings.Repeat(forkOnce, 8) + "sh -c '" + strings.Repeat(forkOnce, 8) + "'",
		"5, 6 and 5 two deep": strings.Repeat(forkOnce, 5) + "sh -c '" + strings.Repeat(forkOnce, 6) + "bash -c \"" + strings.Repeat(forkOnce, 5) + "\"'",
	} {
		t.Run("AC-17 "+name, func(t *testing.T) {
			allowedGlob(t, command, ready)
			leftAloneInEverySession(t, command, false)
		})
	}
}

// Red team C1, security's round-5 N1: a `]]` or `{` with nothing after it
// starts no reading, and a function's body is no fork. An agent's ordinary
// script of tests and functions is far past 16 of them and is not refused.
func TestAC2AC15AC17AnOrdinaryScriptOfTestsAndFunctionsIsNotRefusedAsTooDeep(t *testing.T) {
	var tests, functions, keywords, short strings.Builder
	for i := range 20 {
		n := string(rune('a' + i))
		tests.WriteString("if [[ -f " + n + " ]]; then echo " + n + "; fi\n")
		functions.WriteString("f" + n + "() { echo " + n + "; }\n")
		keywords.WriteString("function g" + n + " {\n  echo " + n + "\n}\n")
		short.WriteString("[[ -f " + n + " ]] && echo " + n + "\n")
	}
	for name, command := range map[string]string{
		"tests inline":                 strings.ReplaceAll(tests.String(), "\n", "; "),
		"tests under bash -c":          "bash -c '" + tests.String() + "'",
		"tests in a here-document":     "bash <<'EOF'\n" + tests.String() + "EOF",
		"tests under zsh -c":           "zsh -c '" + tests.String() + "'",
		"functions inline":             strings.ReplaceAll(functions.String(), "\n", " "),
		"functions under bash -c":      "bash -c '" + functions.String() + "'",
		"functions in a here-document": "bash <<'EOF'\n" + functions.String() + keywords.String() + "EOF",
		"[[ ]] && echo":                strings.ReplaceAll(short.String(), "\n", "; "),
		"[[ ]] && echo under sh -c":    "sh -c '" + short.String() + "'",
		"a brace group, 20 times":      strings.Repeat("{ echo a; echo b; } > /tmp/out; ", 20),
	} {
		t.Run("AC-17 "+name, func(t *testing.T) {
			allowedGlob(t, command, notReady)
			leftAloneInEverySession(t, command, false)
		})
	}
}

// Security's round-4 block, part b; red team V27: a reading that took an
// unknown option's value reads options again after it, so the program is the
// word after them, not the next option.
func TestAC17AReadingThatTookAnOptionsValueGoesOnReadingOptions(t *testing.T) {
	for _, command := range []string{
		"strace -J 1 -f " + aCommit,
		"strace --not-an-option 1 " + aCommit,
		"strace --not-an-option 1 -f -o /tmp/s " + aCommit,
		"strace -J 1 -e trace=execve " + aCommit,
		"ltrace -J 1 -f " + aCommit,
		"/usr/bin/time -J 1 -p " + aCommit,
	} {
		commitMeetsTheGate(t, command)
	}
	refusedUnder(t, "strace -J 1 -f "+rmLock, ready, lockRule)
}

// Security's round-5 B1: zsh's -O and +O are flags, so the script after them
// is still the script; bash's -O takes a shopt name, and +O too.
func TestAC2AC15AC17AShellsOOptionsAreReadAsTheShellReadsThem(t *testing.T) {
	for name, h := range map[string]func(string) string{
		"zsh -c -O":             func(c string) string { return "zsh -c -O '" + c + "'" },
		"zsh -cO":               func(c string) string { return "zsh -cO '" + c + "'" },
		"zsh -c +O":             func(c string) string { return "zsh -c +O '" + c + "'" },
		"zsh -O -c":             func(c string) string { return "zsh -O -c '" + c + "'" },
		"bash -O extglob -c":    func(c string) string { return "bash -O extglob -c '" + c + "'" },
		"bash +O extglob -c":    func(c string) string { return "bash +O extglob -c '" + c + "'" },
		"bash -O a +O b -c":     func(c string) string { return "bash -O extglob +O nullglob -c '" + c + "'" },
		"zsh +o nomatch -c":     func(c string) string { return "zsh +o nomatch -c '" + c + "'" },
		"sh -o errexit -c":      func(c string) string { return "sh -o errexit -c '" + c + "'" },
		"bash --rcfile x -c":    func(c string) string { return "bash --rcfile /tmp/rc -c '" + c + "'" },
		"bash --norc --posix c": func(c string) string { return "bash --norc --posix -c '" + c + "'" },
	} {
		t.Run("AC-17 "+name, func(t *testing.T) {
			refusedEveryKind(t, hider{hide: h, script: true})
		})
	}
	for _, command := range []string{
		"zsh -c -O 'go test ./...'",
		"bash -O extglob -c 'ls !(*.go)'",
		"bash -O globstar -c 'ls **/*.md'",
	} {
		allowedGlob(t, command, notReady)
		leftAloneInEverySession(t, command, false)
	}
}

// Security's round-5 B2: two readings that reach one word by different ways
// are not one reading. strace gives -o to --argv0 and runs watch, which runs
// a shell on the rest; the reading that took the other --argv0 as a flag
// must not stand in for it.
func TestAC17TwoReadingsThatReachOneWordByDifferentWaysAreBothRead(t *testing.T) {
	commitMeetsTheGate(t, "strace --argv0 -o watch strace --argv0 X '"+aCommit+"'")
	refusedUnder(t, "sudo -J -D -K -X v "+rmLock, ready, lockRule)
	refusedUnder(t, "strace --argv0 -o watch strace --argv0 X '"+rmLock+"'", ready, lockRule)
}

// Red team V29, 5j round 5: coproc runs the command after it, with or
// without a name, and `function` defines one whose body runs when called;
// zsh's `function { … }` runs at once.
func TestAC2AC15AC17WhatCoprocAndFunctionRunMeetsEveryRule(t *testing.T) {
	for name, h := range map[string]hider{
		"coproc":              before("coproc ", false),
		"coproc { }":          {hide: func(c string) string { return "coproc { " + c + "; }" }},
		"coproc N { }":        {hide: func(c string) string { return "coproc N { " + c + "; }" }},
		"function f { }; f":   {hide: func(c string) string { return "function f { " + c + "; }; f" }, here: true},
		"function f() { }; f": {hide: func(c string) string { return "function f() { " + c + "; }; f" }, here: true},
		"function { }":        {hide: func(c string) string { return "function { " + c + " }" }, here: true},
	} {
		t.Run("AC-17 "+name, func(t *testing.T) {
			refusedEveryKind(t, h)
		})
	}
	refusedUnder(t, "function f { rm -f .sdlc/stories/A-1/trace.jsonl; }; f", ready, traceRule)
	refusedUnder(t, "coproc "+aCommit, notReady, "commit-gate", "git-command-cannot-be-read")
	for _, command := range []string{
		"coproc cat",
		"coproc N { cat; }",
		"function f { go test ./...; }; f",
		"function { echo hi }",
	} {
		allowedGlob(t, command, notReady)
		leftAloneInEverySession(t, command, false)
	}
}

// Security's round-4 N2 and round 5: a wrapper's valued options are read
// from its table, long ones as getopt_long reads them, a unique prefix
// included; a prefix of two names is read both ways.
func TestAC2AC15AC17ACommandAfterAWrappersValuedOptionsMeetsEveryRule(t *testing.T) {
	for _, words := range []string{
		"nice -n +5 ", "nice -n5 ", "nice -n 5 -- ", "nice --adjustment 5 ",
		"caffeinate -t 1e3 ", "caffeinate -w 1 ", "caffeinate -i -t 10 ",
		"stdbuf --output L ", "stdbuf --output=L ", "stdbuf -o L ", "stdbuf -oL -e 0 ",
		"sudo --user root ", "sudo --us root ", "sudo -u root ", "sudo -uroot ", "sudo -g wheel -u root ",
		"sudo -D . ", "sudo --chdir . ", "sudo -- ", "sudo -E ", "sudo --pr x ",
		"doas -u root ",
		"ionice --cla 2 ", "ionice -c 2 -n 7 ", "ionice --class=2 ",
		"timeout --sig KILL 5 ", "timeout -sKILL 5 ", "timeout -k 1 5 ", "timeout 5 ",
		"watch --int 5 ", "watch -n 5 ", "watch -n5 -d ",
		"nsenter -t 1 -m ", "nsenter --target 1 -m -u ",
		"unshare -w . ", "unshare -r ", "unshare --wd . -m ",
		"systemd-run -p Nice=5 --scope ", "systemd-run --working-directory=. --scope ",
		"systemd-run -E FOO=1 --scope ", "systemd-run --setenv=FOO=1 --scope ",
		"chrt 10 ", "chrt -f 10 ", "chrt -T 1 -D 2 -d 0 ",
		"taskset 0x1 ", "taskset -c 0 ",
	} {
		t.Run("AC-17 "+strings.TrimSpace(words), func(t *testing.T) {
			refusedEveryKind(t, before(words, false))
		})
	}
}

// Security N2 (round 4), red team V16: the program's words after an operand
// the tool takes first, after options among positionals, and after options
// after the host.
func TestAC17ACommitAfterAnOperandOrAPositionalMeetsTheCommitGate(t *testing.T) {
	for _, command := range []string{
		"ssh localhost -v " + aCommit,
		"ssh -v localhost " + aCommit,
		"ssh localhost -p 2222 -o BatchMode=yes " + aCommit,
		"/usr/bin/time -J 1 " + aCommit,
		"chroot -J 1 / " + aCommit,
		"chroot /srv/jail " + aCommit,
		"chroot --userspec=nobody /srv/jail " + aCommit,
		"lldb git -b -o run -- commit -m x",
		"lldb -b git -o run -- commit -m x",
		"lldb -f git -- commit -m x",
		"lldb --file git -b -- commit -m x",
		"lldb -b -o run -- " + aCommit,
		"parallel -j 2 git commit -m ::: x",
		"parallel -j2 --halt now,fail=1 git commit -m ::: x",
		"parallel -J 1 git commit -m ::: x",
	} {
		commitMeetsTheGate(t, command)
	}
}

// Security's round-5 note: PowerShell's parameters, valued or not, in any
// case and by their short names, before the command it runs.
func TestAC17APowerShellCommandAfterItsParametersMeetsTheCommitGate(t *testing.T) {
	for _, command := range []string{
		"pwsh -ExecutionPolicy Bypass -Command '" + aCommit + "'",
		"pwsh -executionpolicy Bypass -command '" + aCommit + "'",
		"pwsh -ep Bypass -c '" + aCommit + "'",
		"pwsh -NoProfile -WorkingDirectory . -Command '" + aCommit + "'",
		"pwsh -wd . -NoLogo -c '" + aCommit + "'",
		"powershell -ExecutionPolicy Bypass -NonInteractive -Command '" + aCommit + "'",
		"pwsh -Unknown x -Command '" + aCommit + "'",
	} {
		commitMeetsTheGate(t, command)
	}
	refusedUnder(t, "pwsh -ExecutionPolicy Bypass -Command '"+rmLock+"'", ready, lockRule)
}

// Architect round-4 notes 1 to 3, security N3, red team V28: every reader
// reads every reading, not only the first.
func TestAC2AC15AC17EveryReaderReadsEveryReading(t *testing.T) {
	refusedUnder(t, `find . -exec strace -J 1 rm .sdlc/state/tests.lock \;`, ready, lockRule)
	refusedUnder(t, `find . -exec strace -J 1 rm {} \;`, ready, lockRule)
	commitMeetsTheGate(t, `find . -maxdepth 0 -exec strace -J 1 git commit -m x \;`)
	commitMeetsTheGate(t, "eval strace -J 1 "+aCommit)
	commitMeetsTheGate(t, "eval 'strace -J 1 "+aCommit+"'")
	refusedUnder(t, "strace -J trap "+rmLock, ready, lockRule)
	refusedUnder(t, "strace -J 1 trap '"+rmLock+"' EXIT", ready, lockRule)
	refusedUnder(t, "strace -J 1 eval '"+rmLock+"'", ready, lockRule)
	refusedUnder(t, "strace -J 1 python3 - <<'EOF'\nimport os; os.remove('.sdlc/state/tests.lock')\nEOF", ready, lockRule)
	for _, command := range []string{
		"strace -J 1 python3 <<'EOF'\nimport os\nos.environ.pop('AI_AGENT')\nos.system('" + then + "')\nEOF",
		"strace -J 1 python3 - <<'EOF'\nimport os\ndel os.environ['AI_AGENT']\nos.execvp('sdlc', ['sdlc', 'hook', 'PreToolUse'])\nEOF",
		"xyz python3 <<'EOF'\nimport os\nos.environ.pop('AI_AGENT')\nos.system('" + then + "')\nEOF",
		`strace -J 1 python3 -c 'import os; os.environ.pop("AI_AGENT"); os.system("` + then + `")'`,
		`strace -J 1 node -e 'delete process.env.AI_AGENT; require("child_process").execSync("` + then + `")'`,
	} {
		refusedAsAIAgentTakenAway(t, command)
	}
}

// Red team round 5, security N2 (round 4): zsh's short forms whose `]]`
// does not follow `[[` in the same command, the blocks after `}`, and
// `always`.
func TestAC2AC15AC17ZshsShortFormsAfterAndOrElseAndAlwaysMeetEveryRule(t *testing.T) {
	for name, h := range map[string]hider{
		"if [[ && ]]":      before("if [[ -n a && -n b ]] ", true),
		"if [[ || ]]":      before("if [[ -n a || -n b ]] ", true),
		"if [[ && ( ) ]]":  before("if [[ -n a && ( -n b ) ]] ", true),
		"if [[ ( ) && ]]":  before("if [[ ( -n a ) && -n b ]] ", true),
		"while [[ && ]]":   before("while [[ -n a && ! -e /tmp/f ]] ", true),
		"} else { }":       {hide: func(c string) string { return "if [[ 0 = 1 ]] { true } else { " + c + " }" }, here: true},
		"} elif [[ ]] { }": {hide: func(c string) string { return "if [[ 0 = 1 ]] { true } elif [[ 1 ]] { " + c + " }" }, here: true},
		"} elif [[ && ]]":  before("if [[ 0 = 1 ]] { true } elif [[ -n a && -n b ]] ", true),
		"} always { }":     {hide: func(c string) string { return "{ true } always { " + c + " }" }, here: true},
		"&& } always { }":  {hide: func(c string) string { return "{ true && true } always { " + c + " }" }, here: true},
		"else, elif and always": {hide: func(c string) string {
			return "if [[ 0 = 1 ]] { true } elif [[ 0 = 2 ]] { true } else { { true } always { " + c + " } }"
		}, here: true},
	} {
		t.Run("AC-17 "+name, func(t *testing.T) {
			refusedEveryKind(t, h)
		})
	}
	for _, command := range []string{
		"if [[ -n $CI && -n $GO ]] go test ./...",
		"if [[ 0 = 1 ]] { true } else { go vet ./... }",
		"{ go test ./... } always { rm -f /tmp/cover.out }",
		"grep -c ']]' docs/commands.md",
	} {
		allowedGlob(t, command, notReady)
		leftAloneInEverySession(t, command, false)
	}
}

// Security N1 (round 4), red team V30: a program handed an event the hook
// records as its first word, a copy of the launcher, is refused in every
// session; the rows that clear the environment first keep their own rule
// (oneline_test.go), and a word that only mentions an event stays open.
func TestAC2AC15AProgramHandedARecordedEventAsItsFirstWordIsRefusedInEverySession(t *testing.T) {
	for _, command := range []string{
		"/tmp/y SubagentStart < /tmp/p.json",
		"/tmp/y SubagentStop < /tmp/p.json",
		"/tmp/y PostToolUse < /tmp/p.json",
		"/tmp/y PostToolUseFailure < /tmp/p.json",
		"/tmp/y StopFailure < /tmp/p.json",
		"/tmp/y PostCompact < /tmp/p.json",
		"/tmp/y subagentstop < /tmp/p.json",
		"cp plugin/bin/sdlc-hook /tmp/y; /tmp/y SubagentStop < /tmp/p.json",
		"ln -s \"$PWD/plugin/bin/sdlc-hook\" /tmp/y && /tmp/y SubagentStart < /tmp/p.json",
		"bash /tmp/y SubagentStop < /tmp/p.json",
		"./y SubagentStart < /tmp/p.json",
		"nice /tmp/y SubagentStop < /tmp/p.json",
	} {
		refusedInEverySession(t, command, false, ruleRecordedEvent)
	}
	refusedAsAIAgentTakenAway(t, `env -i PATH="$PATH" /tmp/y SubagentStop < /tmp/p.json`)
	for _, command := range []string{
		"echo SubagentStop",
		"grep SubagentStart x",
		"grep -rn PostToolUseFailure docs",
		"docker stop web",
		"launchctl stop x",
		`git commit -m "record SubagentStop and Stop"`,
	} {
		leftAloneInEverySession(t, command, false)
	}
}
