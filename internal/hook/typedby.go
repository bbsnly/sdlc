package hook

// Whether a person typed the slash command the hook was handed (US-130).
//
// Claude Code gives every process it starts a label in AI_AGENT, and until
// US-130 the slash route refused a person's command wherever that variable was
// set. In Claude Desktop every hook has it, so a person's /sdlc:approve typed
// there answered that it did not run (2026-11-13). Read in Claude Code 2.1.321
// in Claude Desktop, a hook it runs itself for a session a person opened has
// AI_AGENT=claude-code_<version>_harness and CLAUDE_CODE_SESSION_ATTENDED=1,
// and neither CLAUDE_CODE_EXECPATH nor GIT_EDITOR; a model's Bash has
// ..._agent, CLAUDE_CODE_EXECPATH and GIT_EDITOR=true, and so does the hook it
// runs by hand; the hook of a `claude -p` started from a model's Bash has the
// harness label and CLAUDE_CODE_SESSION_ATTENDED=0. A typed person's command
// runs only where all four say it is Claude Code's own hook in an attended
// session (option B, the person's decision of 2026-11-13), and only GIT_EDITOR
// set to true counts as a model's (the person's decision of
// 2026-11-13T19:41:58Z), so a person whose own shell exports an editor is not
// refused. Anything else refuses, a part that cannot be read among it.
//
// What this tells is a process Claude Code started itself in an attended
// session, not a person at a keyboard: a hook from the project's own
// configuration passes it too, and docs/enforcement.md's Not held says so.
// The shell rules (internal/shellpolicy/agentenv.go) refuse a model's command
// that changes any of these names, and the two Claude Code works
// CLAUDE_CODE_SESSION_ATTENDED out from.
//
// Every host name the check reads sits in this file. The frozen tests drive
// the slash route through RunWith, which has no host, so the check cannot sit
// at the host's edge without a seam those tests do not have. The person's
// decision of 2026-11-13T19:41:58Z extends the exception of 2026-10-29 that
// keeps trace.go's names here to this file, on the same terms: kept in one
// file, so that it can be lifted out whole.

import "strings"

const (
	// agentVariable is store.AgentEnv's name, kept here so that the file
	// holds every name it reads; review holds the pair, and the copy in
	// shellpolicy/agentenv.go.
	agentVariable = "AI_AGENT"
	// execPathVariable and gitEditorVariable are set for a model's tools and
	// not for a hook Claude Code runs itself.
	execPathVariable  = "CLAUDE_CODE_EXECPATH"
	gitEditorVariable = "GIT_EDITOR"
	// attendedVariable is written afresh for every hook Claude Code runs: 1
	// for a session a person attends, 0 for a `claude -p` started from another
	// session's tool call.
	attendedVariable = "CLAUDE_CODE_SESSION_ATTENDED"
	// hookLabelHead and hookLabelTail are the two ends of the label Claude
	// Code gives a process it starts on its own behalf, a hook among them,
	// with its version between them, dots written as dashes.
	hookLabelHead = "claude-code_"
	hookLabelTail = "_harness"
)

// unread is one part of the check that does not hold: the variable's name,
// and whether it was absent rather than holding something else. Never the
// value: the hook's environment holds tokens.
type unread struct {
	name   string
	absent bool
}

// notTypedBy is each part of the check that does not hold where the hook
// runs, in the order a refusal names them, and nothing where Claude Code ran
// the hook itself for a session a person attends.
func notTypedBy(getenv func(string) string) []unread {
	var out []unread
	if label := getenv(agentVariable); !hookLabel(label) {
		out = append(out, unread{agentVariable, label == ""})
	}
	if getenv(execPathVariable) != "" {
		out = append(out, unread{name: execPathVariable})
	}
	if getenv(gitEditorVariable) == "true" {
		out = append(out, unread{name: gitEditorVariable})
	}
	if attended := getenv(attendedVariable); attended != "1" {
		out = append(out, unread{attendedVariable, attended == ""})
	}
	return out
}

// hookLabel reports whether label is the one Claude Code gives a hook it runs
// itself: claude-code_, a version of digits joined by single dashes, and
// _harness. Read by hand rather than by a regular expression, so that nothing
// is compiled on the hook's path. A label of another shape, or of another
// host, is not one, though a later Claude Code may write its own so.
func hookLabel(label string) bool {
	version, ok := strings.CutPrefix(label, hookLabelHead)
	if !ok {
		return false
	}
	if version, ok = strings.CutSuffix(version, hookLabelTail); !ok {
		return false
	}
	for part := range strings.SplitSeq(version, "-") {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return true
}

// whyNotTyped is what a person is told about a part that does not hold, by
// the variable's name and never its value, with what they can do about it.
func whyNotTyped(u unread) string {
	switch u.name {
	case agentVariable:
		return "the environment variable AI_AGENT does not hold Claude Code's label for a hook it runs " +
			"itself, claude-code_ then its version then _harness, as Claude Code 2.1.321 in Claude Desktop " +
			"gives it. Update Claude Desktop or Claude Code; if AI_AGENT is set in the shell you started it " +
			"from, or in Claude Desktop's local environment editor, take it out there and start the session again"
	case execPathVariable:
		return "the environment variable CLAUDE_CODE_EXECPATH is set where this hook runs, which Claude Code " +
			"sets for a model's tools and not for a hook it runs itself. If you started this session from a " +
			"terminal that Claude Code opened, quit that terminal and start it from one you opened yourself"
	case gitEditorVariable:
		return "the environment variable GIT_EDITOR is true where this hook runs, as Claude Code sets it for " +
			"a model's tools. If you set it so yourself, in your shell or in Claude Desktop's local environment " +
			"editor, take it out there and start the session again"
	}
	return "the environment variable CLAUDE_CODE_SESSION_ATTENDED is not 1 where this hook runs, as Claude " +
		"Code leaves it for a `claude -p` started from another session, and an older Claude Code does not set " +
		"it. Type the command in an interactive session you opened yourself"
}

// notTypedAnswer is the refusal of a person's command where the check does
// not hold: that it did not run, each part that failed, what a person does
// instead, the slash command named first (US-108 AC-12), and where the page
// that says more is. Each part is named, since a person who fixes the first
// and types again meets the next (the human advocate, Gate 7 F-2).
func notTypedAnswer(name string, failed []unread) expansionAnswer {
	slash := "/sdlc:" + name
	why := make([]string, 0, len(failed))
	for _, u := range failed {
		why = append(why, whyNotTyped(u))
	}
	return shown(slash + " did not run: the hook could not tell that a person typed it: " +
		strings.Join(why, "; and ") +
		". In Claude Desktop, type `!" + terminalForm(name) + "` with the same arguments at the prompt " +
		"instead, or run `" + terminalForm(name) + "` with the same arguments in a terminal outside Claude " +
		"Code, where sdlc has to be installed (`npx @bbsnly/sdlc install`). docs/troubleshooting.md, under " +
		"\"A slash command did not run\", says more.")
}

// unreadLog is each part that failed for the debug log, by name and whether
// it was absent or held something else, so that a log read later tells a
// Claude Code that changed what it sets from a model's route.
func unreadLog(failed []unread) []string {
	out := make([]string, 0, len(failed))
	for _, u := range failed {
		how := "differs"
		if u.absent {
			how = "absent"
		}
		out = append(out, u.name+" "+how)
	}
	return out
}
