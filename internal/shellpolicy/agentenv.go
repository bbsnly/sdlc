package shellpolicy

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Claude Code sets AI_AGENT for the tools a model runs, and sdlc's hook runs
// no person's slash command where it is set (US-112): a model that takes the
// variable out of its own command could hand the hook a payload it wrote as a
// person's typed /sdlc:approve. Round 2 refused a list of spellings that take
// it away, and security ran seven kinds of one-line command that got past the
// list (R2-1). So a command is refused here for what it does to the name,
// whatever the spelling: a builtin that writes a variable it is named, a value
// that names it, a program the shell makes given it, a program started
// without it, and an interpreter given code that names it.
//
// Until the person's decision of 2026-10-29 the hook also recorded nothing on
// the events it records where AI_AGENT was set. Claude Code 2.1.309 in Claude
// Desktop sets it for a hook too, so the hook records whatever AI_AGENT
// holds, and a hand-run hook on a recorded event is held by forgedhook.go's
// rules and the reader of every reading alone. What still gets past is named
// in docs/enforcement.md's Not held.

// agentEnv is store.AgentEnv's value, copied because shellpolicy and store
// share a tier and neither imports the other. Review holds the pair.
const agentEnv = "AI_AGENT"

// agentMarkerFinding is the refusal of a command that takes AI_AGENT away,
// blanks it or sets it. It does not send the agent to end the iteration: the
// command can nearly always be written another way, and the route says how.
func agentMarkerFinding() Finding {
	return Finding{
		Rule: "enforcement-stays-on",
		Reason: "Claude Code sets AI_AGENT for the tools a model runs, and sdlc's hook runs no person's slash " +
			"command where it is set. A command that removes it, blanks it, sets it to something else, starts " +
			"a program without it, or names it where the shell or an interpreter could change it could hand " +
			"the hook a payload a model wrote as a person's typed command, such as /sdlc:approve, so it is " +
			"refused",
		Route: "leave AI_AGENT as Claude Code set it; a command that names AI_AGENT where the shell or an " +
			"interpreter could change it is refused even when it changes nothing (`declare -p AI_AGENT`, " +
			"`MSG=AI_AGENT make`), so leave the name out of it; spell out a variable's name instead of " +
			"building it from another variable; to drop another variable for one program, use `env -u NAME`, " +
			"not `env -i`; to see what the hook answers, run `sdlc hook PreToolUse` as you are, with a payload " +
			"that has a `session_id` of its own, since a refused one with this session's id is counted in the " +
			"held story's trace; if a rule is wrong, say which one and why",
	}
}

// checkAgentEnv refuses one command the reading gives that takes AI_AGENT
// away from what runs after it, blanks it or sets it; and then one handed an
// event the hook records as its first word, as a copy of the launcher would
// be (firstWordEvent). That is asked here, after AI_AGENT, so that `env -i
// PATH="$PATH" /tmp/y SubagentStop` keeps the rule it was refused under. text
// is the reading.
func checkAgentEnv(r ran, text string) (Finding, bool) {
	if takesAgent(r, text) {
		return agentMarkerFinding(), true
	}
	if words := handRunWords(r); len(words) > 1 && firstWordEvent(words[1]) {
		return recordedEventFinding(), true
	}
	return Finding{}, false
}

// takesAgent reports whether a command takes AI_AGENT away, blanks it or sets
// it, by what it does to the name: it starts its program with an emptied
// environment or with a name unset that may be AI_AGENT, it assigns a name or
// a value that names it, it is a program the shell makes given a word that
// may name it, or it is a builtin or an interpreter that writes it.
func takesAgent(r ran, text string) bool {
	if r.cleared || slices.ContainsFunc(r.unsets, func(n string) bool { return mayBeAgent(n, r.replace) }) {
		return true
	}
	if slices.ContainsFunc(r.env, assignsAgent) || slices.ContainsFunc(r.sets, assignsAgent) {
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
		// names AI_AGENT may be the name it takes away. Given other words, a
		// made program stays open (`X=go; $X test ./...`).
		return slices.ContainsFunc(words[1:], func(w string) bool { return namesAgent(w) || patternOfAgent(w) })
	}
	return builtinTakesAgent(words, r.replace) || interpreterTakesAgent(words, r.input, text)
}

// builtinTakesAgent reports whether a builtin or keyword writes a variable it
// is given by name, and a name it is given there may be AI_AGENT. The name
// positions are each builtin's own; a word elsewhere, a format or a prompt,
// is not read.
func builtinTakesAgent(words []string, replace string) bool {
	may := func(w string) bool { return mayBeAgent(w, replace) }
	switch name := base(words[0]); name {
	case "unset", "export", "declare", "typeset", "local", "readonly", "integer", "float", "private", "getln":
		return declaresAgent(name, words[1:], replace)
	case "mapfile", "readarray":
		_, operands := scanOptions(words[1:], "dnOsuCc")
		return slices.ContainsFunc(operands, may)
	case "read":
		return readTakesAgent(words[1:], may)
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
		return loopTakesAgent(words[1:], replace)
	case "coproc":
		return len(words) > 1 && may(words[1])
	case "zparseopts", "zstyle", "zformat", "zregexparse", "zstat", "strftime", "sysread", "zselect", "zsystem",
		"ztie", "pcre_match", "compadd", "vared":
		// zsh's module builtins take a name in many places among their
		// options and values, so any word that names AI_AGENT is read as one.
		return slices.ContainsFunc(words[1:], func(w string) bool { return namesAgent(w) || patternOfAgent(w) })
	}
	return false
}

// declaresAgent reads the builtins that take names as their operands: every
// word that is not an option, and of one that assigns, the name, and the
// value where it names AI_AGENT, or where the shell makes it for a nameref
// (`declare -n r=$V`). `export` with no option and only names written out
// changes nothing that is exported, and is left alone: `export AI_AGENT`.
func declaresAgent(name string, args []string, replace string) bool {
	if name == "export" && !slices.ContainsFunc(args, func(a string) bool {
		return strings.ContainsAny(a, "-+=") || shellMade(a, replace) || patternOfAgent(a)
	}) {
		return false
	}
	nameref := false
	for i, a := range args {
		if a == "--" {
			return slices.ContainsFunc(args[i+1:], func(a string) bool { return declaredAgent(a, nameref, replace) })
		}
		if len(a) > 1 && (a[0] == '-' || a[0] == '+') {
			nameref = nameref || a[0] == '-' && strings.ContainsRune(a[1:], 'n') &&
				name != "unset" && name != "export"
			continue
		}
		if declaredAgent(a, nameref, replace) {
			return true
		}
	}
	return false
}

// declaredAgent reports an operand of a declaring builtin that may be
// AI_AGENT, or that assigns a value naming it, or, to a nameref, one the shell
// makes.
func declaredAgent(a string, nameref bool, replace string) bool {
	n, v, assigns := strings.Cut(a, "=")
	return mayBeAgent(n, replace) || assigns && (namesAgent(v) || nameref && shellMade(v, replace))
}

// loopTakesAgent reads a for or select loop's words after the keyword: the
// names up to `in` or `do`, which the loop assigns, and the words after `in`,
// which a nameref loop variable is made to refer to in turn (`declare -n r;
// for r in AI_AGENT; do unset r; done`).
func loopTakesAgent(words []string, replace string) bool {
	for i, w := range words {
		if w == "in" {
			return slices.ContainsFunc(words[i+1:], namesAgent)
		}
		if w == "do" {
			return false
		}
		if mayBeAgent(w, replace) {
			return true
		}
	}
	return false
}

// interpreterTakesAgent reports an interpreter given code that names
// AI_AGENT, in its words or on its standard input: `python3 -c
// 'os.environ.pop("AI_AGENT")'`, and awk's `delete ENVIRON["AI_AGENT"]`,
// which gawk 4.2 and later pass on to what system() runs. A read is refused
// with a write here, since no reading tells them apart. Fed its code, the
// interpreter may stand behind a program the reading does not know, as
// runsAScript reads one: `xyz python3 <<'EOF'` (security, round 5, N3).
func interpreterTakesAgent(words []string, input bool, text string) bool {
	code := interpretsCode(base(words[0]))
	if code && slices.ContainsFunc(words[1:], func(w string) bool { return mayName(w, agentEnv) }) {
		return true
	}
	return input && (code || namesAShell(words)) && mayName(text, agentEnv)
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

// assignsAgent reports an assignment, NAME=value, whose name or value names
// AI_AGENT: `AI_AGENT= cmd`, and `V=AI_AGENT` kept for a later `unset "$V"`.
func assignsAgent(a string) bool {
	_, v, ok := strings.Cut(a, "=")
	return ok && (namesAgent(a) || namesAgent(v))
}

// namesAgent reports whether a word names AI_AGENT, once its backslashes are
// taken out and it is folded to upper case: AI_AGENT itself, a subscript of
// it, or either followed by `=` or `+=` and a value. Folding refuses
// `ai_agent`, which bash and zsh read as another name, and in exchange holds
// PowerShell and cmd, where names are not case-sensitive.
func namesAgent(w string) bool {
	w = strings.ToUpper(strings.ReplaceAll(w, `\`, ""))
	if name, _, ok := strings.Cut(w, "="); ok {
		w = strings.TrimSuffix(name, "+")
	}
	return w == agentEnv || strings.HasPrefix(w, agentEnv+"[")
}

// mayBeAgent reports whether a word in a name position could be AI_AGENT: it
// names it, the shell makes the name as the command runs, or it is a pattern
// that can match it. A subscript the shell makes, `arr[$i]`, leaves the name
// as written.
func mayBeAgent(w, replace string) bool {
	name, _, _ := strings.Cut(w, "=")
	head := name
	if i := strings.IndexByte(name, '['); i > 0 && strings.HasSuffix(name, "]") {
		head = name[:i]
	}
	return namesAgent(name) || shellMade(head, replace) || patternOfAgent(name)
}

// shellMade reports a word the shell makes as the command runs: one that holds a
// variable, a substitution or a cmd or PowerShell variable, or the word xargs
// or find fills in.
func shellMade(w, replace string) bool {
	return strings.ContainsAny(w, "$`%") || replace != "" && w == replace
}

// patternOfAgent reports a pattern that can match AI_AGENT. A glob is
// matched against the name, so `AI_*`, `AI_AGEN?`, `AI_AGEN[T]` and `*` are
// and `CD*` and `*.go` are not; a zsh pattern, with `(`, `|`, `<`, `^`, `#`
// or `~`, is one where the text before its first special character starts
// the name: `AI_AGEN(T)`, `^FOO`.
func patternOfAgent(w string) bool {
	if !strings.ContainsAny(w, "*?[(|<^#~") {
		return false
	}
	w = strings.ToUpper(strings.ReplaceAll(w, `\`, ""))
	if i := strings.IndexAny(w, "(|<^#~"); i >= 0 {
		head := w[:i]
		if j := strings.IndexAny(head, "*?["); j >= 0 {
			head = head[:j]
		}
		return strings.HasPrefix(agentEnv, head)
	}
	matched, err := path.Match(w, agentEnv)
	return matched || err != nil
}

// setsAgent matches an assignment to AI_AGENT written as text, in any of the
// places the program rules do not read as one: arithmetic (`(( AI_AGENT = 0
// ))`, `let 'AI_AGENT = 0'`), an expansion that assigns (`${AI_AGENT:=}`), a
// subscript (`AI_AGENT[0]=`), an increment, and PowerShell's and .NET's forms.
// `$AI_AGENT=`, `MY_AI_AGENT=`, `==` and `${AI_AGENT:-x}` are no assignment.
// Compiled on first use, so that init does no work.
var setsAgent = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:^|[^$\w])ai_agent(?:\[[^\]]*\])?\s*(?:[-+*/%&|^]|<<|>>|::?)?=(?:[^=]|$)` +
		`|(?:^|[^$\w])ai_agent\s*(?:\+\+|--)(?:[^\w-]|$)` +
		`|(?:^|[^-\w])(?:\+\+|--)ai_agent\b` +
		`|\$\{env:ai_agent\}\s*\+?=` +
		`|\b(?:set-item|new-item|remove-item|clear-item|rename-item|move-item|copy-item|set-content|add-content|` +
		`clear-content|si|ni|ri|cli|rni|mi|cpi|sc|ac|clc|rm|del|erase|rd|rmdir)\b[^;|\n]*?env:[\\/]?ai_agent\b` +
		`|setenvironmentvariable\(\s*['"]ai_agent['"]`)
})

// agentText refuses a reading whose text assigns AI_AGENT as setsAgent reads
// it, or, on a PowerShell line, takes it away through the env: drive, a new
// environment for a process, or the quoted name.
func agentText(text string, powerShell bool, reading assignments) (Finding, bool) {
	if mayName(text, agentEnv) && setsAgent().MatchString(text) || powerShell && powerShellTakesAgent(text, reading) {
		return agentMarkerFinding(), true
	}
	return Finding{}, false
}

// powerShellTakesAgent reads the PowerShell forms the text form does not
// (round 3, scout rule; not run, as no PowerShell was at hand):
//
//   - a path on the env: drive that names AI_AGENT or holds a wildcard, on a
//     line with a command that does more than read (`ri env:*`, `Rename-Item
//     Env:AI_AGENT X`);
//   - the drive alone, on such a line that also removes or renames, or gives
//     a command AI_AGENT or a pattern of it (`gci env: | ri`, `cd env:; ri
//     AI_AGENT`, `New-Item -Path Env: -Name AI_AGENT`); a variable of another
//     name made there stays open, as the GIT_DIR rule reads it;
//   - Start-Process given a new environment (`-UseNewEnvironment`, and
//     PowerShell 7.4's `-Environment`, whose $null value removes a name);
//   - and the name quoted alone, as .NET's ProcessStartInfo takes it, except
//     as the name GetEnvironmentVariable( reads.
func powerShellTakesAgent(text string, reading assignments) bool {
	if quotesAgent(text) {
		return true
	}
	// The line is read for its commands only where a reading needs them.
	named, bare := envDrivePaths(text)
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
			slices.ContainsFunc(r.words, func(w string) bool { return namesAgent(w) || patternOfAgent(w) })
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
// `${env:`, which are variables: whether one names AI_AGENT or holds a
// wildcard, and whether one is the drive alone.
func envDrivePaths(text string) (named, bare bool) {
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
		named = named || strings.ContainsAny(p, "*?[") || namesAgent(p)
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

// quotesAgent reports the name quoted alone, `'AI_AGENT'` or `"AI_AGENT"`,
// anywhere but as the name GetEnvironmentVariable( reads.
func quotesAgent(text string) bool {
	lower := strings.ToLower(text)
	for at := 0; ; {
		i := strings.Index(lower[at:], "ai_agent")
		if i < 0 {
			return false
		}
		i += at
		at = i + len("ai_agent")
		if i == 0 || at >= len(lower) || !strings.ContainsRune(`'"`, rune(lower[i-1])) ||
			!strings.ContainsRune(`'"`, rune(lower[at])) {
			continue
		}
		before := strings.TrimRight(lower[:i-1], " \t")
		if !strings.HasSuffix(before, "getenvironmentvariable(") {
			return true
		}
	}
}

// startOf reports whether p is a start of name, as PowerShell takes a
// parameter by any start of its name.
func startOf(p, name string) bool {
	return len(p) <= len(name) && name[:len(p)] == p
}

// readTakesAgent reads the read builtin's words. zsh's `read 'NAME?prompt'`
// names NAME. The values of -a, bash's array, and of -t, -n, -N and -u, which
// take a number in bash and in zsh may leave the name as their value, are
// read as names too.
func readTakesAgent(args []string, may func(string) bool) bool {
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
