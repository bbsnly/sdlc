package shellpolicy

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// sdlc's hook runs a person's typed slash command only where its environment
// is that of a hook Claude Code runs itself for a session a person attends
// (internal/hook/typedby.go, US-130): AI_AGENT holding Claude Code's hook
// label, CLAUDE_CODE_SESSION_ATTENDED=1, and neither CLAUDE_CODE_EXECPATH nor
// GIT_EDITOR=true, which a model's tools have. A model that changed any of
// them in a process it starts could hand the hook a payload it wrote, or start
// a session whose hook looks like the person's. Claude Code writes
// CLAUDE_CODE_SESSION_ATTENDED afresh for every hook, from whether the session
// is a child (CLAUDE_CODE_CHILD_SESSION) and its entrypoint
// (CLAUDE_CODE_ENTRYPOINT), so those two are what a model would change to make
// a nested `claude -p` look attended (Q-A); ATTENDED itself is refused all the
// same, though setting it does nothing.
//
// Round 2 of US-112 refused a list of spellings that take AI_AGENT away, and
// security ran seven kinds of one-line command that got past the list (R2-1).
// So a command is refused here for what it does to a protected name, whatever
// the spelling: a builtin that writes a variable it is named, a value that
// names it, a program the shell makes given it, a program started without it,
// and an interpreter given code that names it. US-130 widened the one name to
// six, through the same readers.
//
// The six are Claude Code's names, kept here though new code outside
// internal/hook/claudecode names no host's variables: the shell rules have no
// host seam, and the person's decision of 2026-11-13T19:41:58Z extends the
// exception of 2026-10-29 to these names, on the terms that they stay in this
// file and can be lifted out with it. CLAUDE_CODE_EXECPATH is also read as a
// program word, as the name of the program is (execPath below, claudeconfig.go).
//
// The evasions US-130 tried against these readers, from the session holding
// the story and from another (2026-11-13), all refused:
//
//   - every row of internal/shellpolicy/us130_test.go, run in bash 5.3.20 and
//     zsh 5.9 as its header records;
//   - every AI_AGENT spelling of oneline_test.go, forgedevent_test.go and
//     hiddenbehind_test.go with each of the five new names in its place,
//     subscripts, `NAME=()`, `let`, `$(( ))`, namerefs, mapfile, getopts,
//     `print -v`, `declare -a` and `-i`, integer, float, `unset $(printf
//     NAME)` and a glob matched against a file of that name among them, read
//     through Inspect and HumanDecisions in a scratch copy: all 29 of those
//     tests passed for each name, and 16 failed with GIT_EDITOR left out of
//     the set;
//   - `export GIT_EDITOR+=x`, `declare -n r=GIT_EDITOR; r=x`, `typeset -gx
//     GIT_EDITOR=x`, in a function too, and `V=GIT_EDITOR; unset "$V"`, which
//     changed the variable in bash, and all but the first two in zsh;
//     `git_editor=vim`, which changed nothing in either and is refused for
//     PowerShell's sake; `env -uGIT_EDITOR`, which macOS's env took; `env
//     --unset=GIT_EDITOR`, `exec env -u CLAUDE_CODE_EXECPATH`, `readonly
//     CLAUDE_CODE_CHILD_SESSION`, `[Environment]::SetEnvironmentVariable(
//     "CLAUDE_CODE_EXECPATH", $null)` and `ri env:CLAUDE_CODE_*`, not run.
//
// What makes Claude Code count a session as one a person attends was read in
// 2.1.321: CLAUDE_CODE_SESSION_KIND can only make it not; an interactive
// session is one started without -p, --print, --init-only or --sdk-url whose
// standard output is a terminal, which no variable sets; and a session that is
// not one counts where it is no child (CLAUDE_CODE_CHILD_SESSION) and its
// entrypoint (CLAUDE_CODE_ENTRYPOINT) is in a fixed set. A nested session
// given a terminal is what the shell rules on starting one hold.
//
// Until the person's decision of 2026-10-29 the hook also recorded nothing on
// the events it records where AI_AGENT was set. Claude Code gives every hook
// its harness label, so the hook records whatever AI_AGENT holds, and a
// hand-run hook on a recorded event is held by forgedhook.go's rules and the
// reader of every reading alone. What still gets past is named in
// docs/enforcement.md's Not held.

// protectedSet is a list of names the readers below look for.
type protectedSet []string

// protectedNames are the six. AI_AGENT is store.AgentEnv's value, copied
// because shellpolicy and store share a tier and neither imports the other;
// internal/hook/typedby.go keeps the four its check reads. Review holds each
// pair.
var protectedNames = protectedSet{
	"AI_AGENT", execPath, "GIT_EDITOR", "CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT",
}

// execPath is the variable that holds the path of the claude binary a model's
// tools run under, and which a hook Claude Code runs itself does not have. It
// is protected, and it is read as a program word too, the way `claude` is:
// `"$CLAUDE_CODE_EXECPATH" -p x` starts a session as surely as `claude -p x`,
// and a rule that read only the second let the first through (the verifier,
// US-130 Gate 6, F-1). Held once, so that the list and the readers cannot
// drift, and so that no other file's code names it.
const execPath = "CLAUDE_CODE_EXECPATH"

// namesExecPath reports whether a word expands CLAUDE_CODE_EXECPATH: it holds
// the name, in any case and with its quotes and backslashes taken out, and a
// `$` or a `%` that makes the shell, PowerShell or cmd read it as a variable.
func namesExecPath(word string) bool {
	w := strings.ToUpper(unescaped.Replace(word))
	return strings.Contains(w, execPath) && strings.ContainsAny(w, "$%")
}

// expandsExecPath is the length of an expansion of CLAUDE_CODE_EXECPATH at the
// start of s, `$NAME` not followed by a name byte or `${NAME}`, and 0 where s
// starts with none. Compared with EqualFold, so that it allocates nothing.
func expandsExecPath(s string) int {
	for _, form := range [...]struct{ open, close string }{{"$", ""}, {"${", "}"}} {
		n := len(form.open) + len(execPath) + len(form.close)
		if len(s) < n || !strings.HasPrefix(s, form.open) || !strings.EqualFold(s[len(form.open):n-len(form.close)], execPath) ||
			!strings.HasSuffix(s[:n], form.close) {
			continue
		}
		if form.close == "" && n < len(s) && nameByte(s[n]) {
			continue
		}
		return n
	}
	return 0
}

// gitEditor is the protected name a model most often writes in front of a git
// command, and the one whose route needs a way round of its own.
const gitEditor = "GIT_EDITOR"

// refusedFor is the refusal of a command that takes reports for. takes is asked
// with the six names, then with each alone; the refusal names the one name it
// reports for alone, and all six where it reports for several or none alone, as
// for a name the shell builds or an emptied environment: a line that reads one
// name and takes away another by a pattern names the one taken.
func refusedFor(takes func(protectedSet) bool) Finding {
	var named protectedSet
	for _, name := range protectedNames {
		if takes(protectedSet{name}) {
			named = append(named, name)
		}
	}
	if len(named) != 1 {
		named = protectedNames
	}
	return protectedFinding(named)
}

// protectedFinding is the refusal of a command that takes one of names away,
// blanks it or sets it. It does not send the agent to end the iteration: the
// command can nearly always be written another way, and the route says how.
func protectedFinding(names protectedSet) Finding {
	it := "it"
	removes, leave := names[0], "leave "+names[0]+" as Claude Code set it"
	if len(names) > 1 {
		them := strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
		it = "one of them"
		// "removes A, B and C, blanks one of them" read as all of them removed
		// (the human advocate, Gate 7 F-1).
		removes, leave = "any of "+them, "leave them as Claude Code set them"
	}
	git := ""
	if slices.Contains(names, gitEditor) {
		// GIT_EDITOR outranks core.editor, so the -c form helps only where the
		// variable is not set.
		git = "a git command needs no GIT_EDITOR of its own, since Claude Code gives a model's shell " +
			"GIT_EDITOR=true (seen in Claude Desktop on macOS), so run it without one; where GIT_EDITOR " +
			"is not set, `git -c core.editor=true ...` keeps the message as written for one command; "
	}
	return Finding{
		Rule: "enforcement-stays-on",
		Reason: "sdlc's hook runs a person's typed slash command only where its environment is that of a hook " +
			"Claude Code runs itself for a session a person attends, which it tells by AI_AGENT, " +
			"CLAUDE_CODE_EXECPATH, GIT_EDITOR and CLAUDE_CODE_SESSION_ATTENDED, and Claude Code works out the " +
			"last from CLAUDE_CODE_CHILD_SESSION and CLAUDE_CODE_ENTRYPOINT. A command that removes " + removes +
			", blanks " + it + ", sets " + it + " to something else, starts a program without " + it + ", or " +
			"names " + it + " where the shell or an interpreter could change it could make a payload a model " +
			"wrote, or a session it starts, pass for a person's typed command, such as /sdlc:approve, so it " +
			"is refused",
		Route: leave + "; a command that names " + it + " where the " +
			"shell or an interpreter could change it is refused even when it changes nothing (`declare -p " +
			names[0] + "`, `MSG=" + names[0] + " make`), so leave the name out of it; " + git + "spell out a " +
			"variable's name instead of building it from another variable; to drop a variable that is not " +
			"one of " + strings.Join(protectedNames, ", ") + " for one program, use `env -u NAME`, not " +
			"`env -i`; to see what the hook answers, run `sdlc hook PreToolUse` as you are, with a payload " +
			"that has a `session_id` of its own, since a refused one with this session's id is counted in the " +
			"held story's trace; if a rule is wrong, say which one and why",
	}
}

// checkProtectedEnv refuses one command the reading gives that takes a
// protected name away from what runs after it, blanks it or sets it; and then
// one handed an event the hook records as its first word, as a copy of the
// launcher would be (firstWordEvent). That is asked here, after the names, so
// that `env -i PATH="$PATH" /tmp/y SubagentStop` keeps the rule it was refused
// under. text is the reading.
func checkProtectedEnv(r ran, text string) (Finding, bool) {
	takes := func(set protectedSet) bool { return set.takesProtected(r, text) }
	if takes(protectedNames) {
		return refusedFor(takes), true
	}
	if words := handRunWords(r); len(words) > 1 && firstWordEvent(words[1]) {
		return recordedEventFinding(), true
	}
	return Finding{}, false
}

// takesProtected reports whether a command takes a name of the set away,
// blanks it or sets it, by what it does to the name: it starts its program
// with an emptied environment or with a name unset that may be one, it assigns
// a name or a value that names one, it is a program the shell makes given a
// word that may name one, or it is a builtin or an interpreter that writes
// one.
func (set protectedSet) takesProtected(r ran, text string) bool {
	if r.cleared || slices.ContainsFunc(r.unsets, func(n string) bool { return set.mayBeProtected(n, r.replace) }) {
		return true
	}
	if slices.ContainsFunc(r.env, set.assignsProtected) || slices.ContainsFunc(r.sets, set.assignsProtected) {
		return true
	}
	words := r.marked
	if len(words) == 0 {
		words = r.words
	}
	if len(words) == 0 {
		return false
	}
	if shellMade(words[0], r.replace) {
		// `X=unset; $X AI_AGENT`: what runs cannot be named, so a word that
		// names a protected name may be the name it takes away. Given other
		// words, a made program stays open (`X=go; $X test ./...`).
		return slices.ContainsFunc(words[1:], func(w string) bool {
			return set.namesProtected(w) || set.patternOfProtected(w)
		})
	}
	return set.builtinTakesProtected(words, r.replace) || set.interpreterTakesProtected(words, r.input, text)
}

// builtinTakesProtected reports whether a builtin or keyword writes a variable
// it is given by name, and a name it is given there may be one of the set.
// The name positions are each builtin's own; a word elsewhere, a format or a
// prompt, is not read.
func (set protectedSet) builtinTakesProtected(words []string, replace string) bool {
	may := func(w string) bool { return set.mayBeProtected(w, replace) }
	switch name := base(words[0]); name {
	case "unset", "export", "declare", "typeset", "local", "readonly", "integer", "float", "private", "getln":
		return set.declaresProtected(name, words[1:], replace)
	case "mapfile", "readarray":
		_, operands := scanOptions(words[1:], "dnOsuCc")
		return slices.ContainsFunc(operands, may)
	case "read":
		return readTakesProtected(words[1:], may)
	case "getopts":
		return len(words) > 2 && may(words[2])
	case "printf", "print", "wait":
		opts, _ := scanOptions(words[1:], "vCfuxXp")
		letter := "v"
		if name == "wait" {
			letter = "p"
		}
		return slices.ContainsFunc(opts, func(o option) bool { return o.name == letter && may(o.value) })
	case "for", "select":
		return set.loopTakesProtected(words[1:], replace)
	case "coproc":
		return len(words) > 1 && may(words[1])
	case "zparseopts", "zstyle", "zformat", "zregexparse", "zstat", "strftime", "sysread", "zselect", "zsystem",
		"ztie", "pcre_match", "compadd", "vared":
		// zsh's module builtins take a name in many places among their
		// options and values, so any word that names one is read as one.
		return slices.ContainsFunc(words[1:], func(w string) bool {
			return set.namesProtected(w) || set.patternOfProtected(w)
		})
	}
	return false
}

// declaresProtected reads the builtins that take names as their operands:
// every word that is not an option, and of one that assigns, the name, and the
// value where it names one of the set, or where the shell makes it for a
// nameref (`declare -n r=$V`). `export` with no option and only names written
// out changes nothing that is exported, and is left alone: `export AI_AGENT`.
func (set protectedSet) declaresProtected(name string, args []string, replace string) bool {
	if name == "export" && !slices.ContainsFunc(args, func(a string) bool {
		return strings.ContainsAny(a, "-+=") || shellMade(a, replace) || set.patternOfProtected(a)
	}) {
		return false
	}
	nameref := false
	for i, a := range args {
		if a == "--" {
			return slices.ContainsFunc(args[i+1:], func(a string) bool { return set.declaredProtected(a, nameref, replace) })
		}
		if len(a) > 1 && (a[0] == '-' || a[0] == '+') {
			nameref = nameref || a[0] == '-' && strings.ContainsRune(a[1:], 'n') &&
				name != "unset" && name != "export"
			continue
		}
		if set.declaredProtected(a, nameref, replace) {
			return true
		}
	}
	return false
}

// declaredProtected reports an operand of a declaring builtin that may be one
// of the set, or that assigns a value naming one, or, to a nameref, one the
// shell makes.
func (set protectedSet) declaredProtected(a string, nameref bool, replace string) bool {
	n, v, assigns := strings.Cut(a, "=")
	return set.mayBeProtected(n, replace) || assigns && (set.namesProtected(v) || nameref && shellMade(v, replace))
}

// loopTakesProtected reads a for or select loop's words after the keyword:
// the names up to `in` or `do`, which the loop assigns, and the words after
// `in`, which a nameref loop variable is made to refer to in turn (`declare -n
// r; for r in AI_AGENT; do unset r; done`).
func (set protectedSet) loopTakesProtected(words []string, replace string) bool {
	for i, w := range words {
		if w == "in" {
			return slices.ContainsFunc(words[i+1:], set.namesProtected)
		}
		if w == "do" {
			return false
		}
		if set.mayBeProtected(w, replace) {
			return true
		}
	}
	return false
}

// interpreterTakesProtected reports an interpreter given code that names one
// of the set, in its words or on its standard input: `python3 -c
// 'os.environ.pop("AI_AGENT")'`, and awk's `delete ENVIRON["AI_AGENT"]`,
// which gawk 4.2 and later pass on to what system() runs. A read is refused
// with a write here, since no reading tells them apart. Fed its code, the
// interpreter may stand behind a program the reading does not know, as
// runsAScript reads one: `xyz python3 <<'EOF'` (security, round 5, N3).
func (set protectedSet) interpreterTakesProtected(words []string, input bool, text string) bool {
	code := interpretsCode(base(words[0]))
	if code && slices.ContainsFunc(words[1:], set.mayNameProtected) {
		return true
	}
	return input && (code || namesAShell(words)) && set.mayNameProtected(text)
}

// mayNameProtected reports text that holds one of the set, in any case.
func (set protectedSet) mayNameProtected(text string) bool {
	return slices.ContainsFunc(set, func(name string) bool { return mayName(text, name) })
}

// interpretsCode reports a program that runs code it is given: the
// interpreters, perl, and awk's family, whose system(), a pipe to "sh" and
// ENVIRON are an interpreter's.
func interpretsCode(name string) bool {
	switch name {
	case "perl", "awk", "gawk", "mawk", "nawk":
		return true
	}
	return interpreters[name]
}

// assignsProtected reports an assignment, NAME=value, whose name or value
// names one of the set: `AI_AGENT= cmd`, and `V=AI_AGENT` kept for a later
// `unset "$V"`.
func (set protectedSet) assignsProtected(a string) bool {
	_, v, ok := strings.Cut(a, "=")
	return ok && (set.namesProtected(a) || set.namesProtected(v))
}

// namesProtected reports whether a word names one of the set, once its
// backslashes are taken out and it is folded to upper case: the name itself,
// a subscript of it, or either followed by `=` or `+=` and a value. Folding
// refuses `ai_agent`, which bash and zsh read as another name, and in exchange
// holds PowerShell and cmd, where names are not case-sensitive.
func (set protectedSet) namesProtected(w string) bool {
	w = strings.ToUpper(strings.ReplaceAll(w, `\`, ""))
	if name, _, ok := strings.Cut(w, "="); ok {
		w = strings.TrimSuffix(name, "+")
	}
	return slices.ContainsFunc(set, func(name string) bool { return w == name || strings.HasPrefix(w, name+"[") })
}

// mayBeProtected reports whether a word in a name position could be one of
// the set: it names one, the shell makes the name as the command runs, or it
// is a pattern that can match one. A subscript the shell makes, `arr[$i]`,
// leaves the name as written.
func (set protectedSet) mayBeProtected(w, replace string) bool {
	name, _, _ := strings.Cut(w, "=")
	head := name
	if i := strings.IndexByte(name, '['); i > 0 && strings.HasSuffix(name, "]") {
		head = name[:i]
	}
	return set.namesProtected(name) || shellMade(head, replace) || set.patternOfProtected(name)
}

// shellMade reports a word the shell makes as the command runs: one that holds a
// variable, a substitution or a cmd or PowerShell variable, or the word xargs
// or find fills in.
func shellMade(w, replace string) bool {
	return strings.ContainsAny(w, "$`%") || replace != "" && w == replace
}

// patternOfProtected reports a pattern that can match one of the set. A glob
// is matched against each name, so `AI_*`, `AI_AGEN?`, `AI_AGEN[T]`,
// `CLAUDE_CODE_*` and `*` are and `CD*` and `*.go` are not; a zsh pattern,
// with `(`, `|`, `<`, `^`, `#` or `~`, is one where the text before its first
// special character starts a name: `AI_AGEN(T)`, `^FOO`.
func (set protectedSet) patternOfProtected(w string) bool {
	if !strings.ContainsAny(w, "*?[(|<^#~") {
		return false
	}
	w = strings.ToUpper(strings.ReplaceAll(w, `\`, ""))
	if i := strings.IndexAny(w, "(|<^#~"); i >= 0 {
		head := w[:i]
		if j := strings.IndexAny(head, "*?["); j >= 0 {
			head = head[:j]
		}
		return slices.ContainsFunc(set, func(name string) bool { return strings.HasPrefix(name, head) })
	}
	return slices.ContainsFunc(set, func(name string) bool {
		matched, err := path.Match(w, name)
		return matched || err != nil
	})
}

// setsProtected matches, for each protected name, an assignment to it written
// as text, in any of the places the program rules do not read as one:
// arithmetic (`(( AI_AGENT = 0 ))`, `let 'AI_AGENT = 0'`), an expansion that
// assigns (`${AI_AGENT:=}`), a subscript (`AI_AGENT[0]=`), an increment, and
// PowerShell's and .NET's forms. `$AI_AGENT=`, `MY_AI_AGENT=`, `==` and
// `${AI_AGENT:-x}` are no assignment. Compiled on first use, so that init does
// no work.
var setsProtected = sync.OnceValue(func() map[string]*regexp.Regexp {
	out := make(map[string]*regexp.Regexp, len(protectedNames))
	for _, name := range protectedNames {
		n := regexp.QuoteMeta(strings.ToLower(name))
		out[name] = regexp.MustCompile(`(?i)(?:^|[^$\w])` + n + `(?:\[[^\]]*\])?\s*(?:[-+*/%&|^]|<<|>>|::?)?=(?:[^=]|$)` +
			`|(?:^|[^$\w])` + n + `\s*(?:\+\+|--)(?:[^\w-]|$)` +
			`|(?:^|[^-\w])(?:\+\+|--)` + n + `\b` +
			`|\$\{env:` + n + `\}\s*\+?=` +
			`|\b(?:set-item|new-item|remove-item|clear-item|rename-item|move-item|copy-item|set-content|add-content|` +
			`clear-content|si|ni|ri|cli|rni|mi|cpi|sc|ac|clc|rm|del|erase|rd|rmdir)\b[^;|\n]*?env:[\\/]?` + n + `\b` +
			`|setenvironmentvariable\(\s*['"]` + n + `['"]`)
	}
	return out
})

// setsProtectedText reports text that assigns one of the set as setsProtected
// reads it.
func (set protectedSet) setsProtectedText(text string) bool {
	return slices.ContainsFunc(set, func(name string) bool {
		return mayName(text, name) && setsProtected()[name].MatchString(text)
	})
}

// protectedText refuses a reading whose text assigns a protected name as
// setsProtected reads it, or, on a PowerShell line, takes one away through the
// env: drive, a new environment for a process, or the quoted name.
func protectedText(text string, powerShell bool, reading assignments) (Finding, bool) {
	takes := func(set protectedSet) bool {
		return set.setsProtectedText(text) || powerShell && set.powerShellTakesProtected(text, reading)
	}
	if takes(protectedNames) {
		return refusedFor(takes), true
	}
	return Finding{}, false
}

// powerShellTakesProtected reads the PowerShell forms the text form does not
// (round 3, scout rule; not run, as no PowerShell was at hand):
//
//   - a path on the env: drive that names one of the set or holds a wildcard,
//     on a line with a command that does more than read (`ri env:*`,
//     `Rename-Item Env:AI_AGENT X`);
//   - the drive alone, on such a line that also removes or renames, or gives
//     a command a name of the set or a pattern of one (`gci env: | ri`, `cd
//     env:; ri AI_AGENT`, `New-Item -Path Env: -Name AI_AGENT`); a variable of
//     another name made there stays open, as the GIT_DIR rule reads it;
//   - Start-Process given a new environment (`-UseNewEnvironment`, and
//     PowerShell 7.4's `-Environment`, whose $null value removes a name);
//   - and a name quoted alone, as .NET's ProcessStartInfo takes it, except
//     as the name GetEnvironmentVariable( reads.
func (set protectedSet) powerShellTakesProtected(text string, reading assignments) bool {
	if set.quotesProtected(text) {
		return true
	}
	// The line is read for its commands only where a reading needs them.
	named, bare := set.envDrivePaths(text)
	if lower := strings.ToLower(text); !named && !bare && !strings.Contains(lower, "start") &&
		!strings.Contains(lower, "saps") {
		return false
	}
	runs, _ := programsAt(text, true, reading)
	if slices.ContainsFunc(runs, startsAfresh) {
		return true
	}
	if !named && !bare || !slices.ContainsFunc(runs, func(r ran) bool {
		return len(r.words) > 0 && !readOnlyCmdlet(base(r.words[0]))
	}) {
		return false
	}
	return named || slices.ContainsFunc(runs, func(r ran) bool {
		return len(r.words) > 0 && removerCmdlet(base(r.words[0])) ||
			slices.ContainsFunc(r.words, func(w string) bool { return set.namesProtected(w) || set.patternOfProtected(w) })
	})
}

// removerCmdlet reports a cmdlet, or an alias of one, that takes a name off
// the drive it is given, or empties it.
func removerCmdlet(name string) bool {
	switch name {
	case "remove-item", "ri", "rm", "del", "erase", "rd", "rmdir", "clear-item", "cli", "rename-item", "rni",
		"move-item", "mi", "clear-content", "clc":
		return true
	}
	return false
}

// envDrivePaths reads the paths on PowerShell's env: drive, not `$env:` or
// `${env:`, which are variables: whether one names one of the set or holds a
// wildcard, and whether one is the drive alone.
func (set protectedSet) envDrivePaths(text string) (named, bare bool) {
	lower := strings.ToLower(text)
	for at := 0; ; {
		i := strings.Index(lower[at:], "env:")
		if i < 0 {
			return named, bare
		}
		i += at
		at = i + len("env:")
		if i > 0 && (lower[i-1] == '$' || lower[i-1] == '{' || lower[i-1] == '_' ||
			lower[i-1] >= 'a' && lower[i-1] <= 'z' || lower[i-1] >= '0' && lower[i-1] <= '9') {
			continue
		}
		end := at
		for end < len(text) && !strings.ContainsRune(" \t\r\n;|,)}'\"", rune(text[end])) {
			end++
		}
		p := strings.TrimLeft(text[at:end], `\/`)
		bare = bare || p == ""
		named = named || strings.ContainsAny(p, "*?[") || set.namesProtected(p)
	}
}

// readOnlyCmdlet reports a cmdlet, or an alias of one, that only reads what
// it is given.
func readOnlyCmdlet(name string) bool {
	switch name {
	case "get-item", "gi", "get-childitem", "gci", "ls", "dir", "get-content", "gc", "cat", "type",
		"get-itemproperty", "gp", "test-path", "select-string", "sls", "write-output", "echo", "write",
		"write-host", "sort-object", "sort", "where-object", "where", "?", "select-object", "select",
		"foreach-object", "foreach", "%", "format-table", "ft", "format-list", "fl", "out-string",
		"measure-object", "measure":
		return true
	}
	return false
}

// startsAfresh reports Start-Process, or an alias of it, given a parameter
// that names -UseNewEnvironment or -Environment by any start of it.
func startsAfresh(r ran) bool {
	if len(r.words) < 2 {
		return false
	}
	switch base(r.words[0]) {
	case "start-process", "saps", "start":
	default:
		return false
	}
	for _, w := range r.words[1:] {
		param, _, _ := strings.Cut(strings.ToLower(w), ":")
		if p, ok := strings.CutPrefix(param, "-"); ok && p != "" &&
			(startOf(p, "usenewenvironment") || startOf(p, "environment")) {
			return true
		}
	}
	return false
}

// quotesProtected reports a name of the set quoted alone, `'AI_AGENT'` or
// `"AI_AGENT"`, anywhere but as the name GetEnvironmentVariable( reads.
func (set protectedSet) quotesProtected(text string) bool {
	lower := strings.ToLower(text)
	return slices.ContainsFunc(set, func(name string) bool {
		name = strings.ToLower(name)
		for at := 0; ; {
			i := strings.Index(lower[at:], name)
			if i < 0 {
				return false
			}
			i += at
			at = i + len(name)
			if i == 0 || at >= len(lower) || !strings.ContainsRune(`'"`, rune(lower[i-1])) ||
				!strings.ContainsRune(`'"`, rune(lower[at])) {
				continue
			}
			before := strings.TrimRight(lower[:i-1], " \t")
			if !strings.HasSuffix(before, "getenvironmentvariable(") {
				return true
			}
		}
	})
}

// startOf reports whether p is a start of name, as PowerShell takes a
// parameter by any start of its name.
func startOf(p, name string) bool {
	return len(p) <= len(name) && name[:len(p)] == p
}

// readTakesProtected reads the read builtin's words. zsh's `read
// 'NAME?prompt'` names NAME. The values of -a, bash's array, and of -t, -n,
// -N and -u, which take a number in bash and in zsh may leave the name as
// their value, are read as names too.
func readTakesProtected(args []string, may func(string) bool) bool {
	opts, operands := scanOptions(args, "adinNptuA")
	for _, o := range opts {
		switch o.name {
		case "a", "t", "n", "N", "u", "A":
			if may(o.value) {
				return true
			}
		}
	}
	return slices.ContainsFunc(operands, func(w string) bool {
		name, _, _ := strings.Cut(w, "?")
		return may(name)
	})
}
