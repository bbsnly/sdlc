package shellpolicy

import (
	"slices"
	"strings"
)

// A hook run by hand on a payload the model wrote. The hook answers Claude
// Code's UserPromptExpansion by running the slash command its payload names as
// a person's decision, and it cannot tell its own call from Claude Code's: the
// route is chosen by the event word alone (internal/hook/slash.go). So
// `sdlc hook UserPromptExpansion < p.json`, with p.json holding /sdlc:approve,
// approved the story from the model's own shell. The hook run by hand on any
// other event decides nothing a person keeps, and reviewers here drive it so;
// that stays allowed.

// slashCommandRule is the rule a hand-run UserPromptExpansion is refused
// under, from the session holding a story and from any other.
const slashCommandRule = "slash-command-is-a-human-decision"

// hookEvent is what the event word after the hook is, as far as the words show.
type hookEvent int

const (
	// otherEvent is an event written out that is not the expansion, or none
	// at all where nothing hands the hook more words.
	otherEvent hookEvent = iota
	// expansionEvent is UserPromptExpansion, in any case.
	expansionEvent
	// unreadEvent is one the shell makes when the command runs, or one handed
	// over on standard input by xargs or parallel.
	unreadEvent
)

// checkHandRunHook refuses a command that runs the sdlc hook on a
// UserPromptExpansion, or on an event it cannot read.
func checkHandRunHook(r ran) (Finding, bool) {
	event := runsTheHookOnAnExpansion(r)
	if event == otherEvent {
		return Finding{}, false
	}
	return handRunHookFinding(event), true
}

// handRunHookFinding is the refusal, with a reason for each of the two
// causes: the expansion written out, and an event that cannot be read.
func handRunHookFinding(event hookEvent) Finding {
	reason := "this runs the sdlc hook as if a person had typed a slash command: on Claude Code's " +
		"UserPromptExpansion event, the hook runs the command its payload names, such as /sdlc:approve, " +
		"as that person's decision, and a payload written by a model is not a person's"
	if event == unreadEvent {
		reason = "this runs the sdlc hook on an event that cannot be read until the command runs, and it could " +
			"be Claude Code's UserPromptExpansion, on which the hook acts as if a person had typed a slash " +
			"command: it runs the command its payload names, such as /sdlc:approve, as that person's decision, " +
			"and a payload written by a model is not a person's"
	}
	return Finding{
		Rule:   slashCommandRule,
		Reason: reason,
		Route: "say what you wanted decided and stop; a person types `/sdlc:approve <ID>`, or `sdlc approve <ID>` " +
			"in their own terminal, and likewise `/sdlc:unfreeze`, `/sdlc:ack`, `/sdlc:remap`, `/sdlc:release` " +
			"and `/sdlc:assign`; to try the hook yourself, run it on another event written out as a literal " +
			"word, such as `sdlc hook PreToolUse`",
	}
}

// runsTheHookOnAnExpansion is the event a command runs the hook on, where it
// runs the hook on the expansion or on an event it cannot read, and
// otherEvent where it does not. The words are read as the lexer marked them,
// so that a substituted program or event is $SUB and not gone.
//
// sdlc and the launcher are read as the program the command runs, where an
// event that cannot be read is refused, and as a later word behind a program
// the reading does not know, where only the expansion written out is: there
// they are as likely a path being copied, added or checked, and refusing
// whatever could not be read refused `cp plugin/bin/sdlc-hook "$D/"` and
// `git add plugin/bin/sdlc-hook plugin/bin/sdlc-hook.cmd`, as staffing.go
// says of sdlc.
func runsTheHookOnAnExpansion(r ran) hookEvent {
	words := r.marked
	if len(words) == 0 {
		words = r.words
	}
	words = started(splitLists(words))
	if len(words) == 0 || mentionsOnly(base(words[0])) {
		return otherEvent
	}
	for _, read := range []func([]string, bool) hookEvent{sdlcRunsTheHook, launcherRuns, unknownProgramRunsTheHook} {
		if event := read(words, r.piped); event != otherEvent {
			return event
		}
	}
	if handedTheExpansion(words) {
		return expansionEvent
	}
	return otherEvent
}

// sdlcRunsTheHook reads sdlc running `hook`, by every spelling sdlcUses
// reads.
func sdlcRunsTheHook(words []string, piped bool) hookEvent {
	for _, u := range sdlcUses(words) {
		sub, rest, _ := nextWord(u.args)
		if !strings.EqualFold(sub, "hook") {
			continue
		}
		if event := eventOf(rest, piped || u.fed); event == expansionEvent || event == unreadEvent && u.way == ranSdlc {
			return event
		}
	}
	return otherEvent
}

// launcherRuns reads the plugin's launcher, sdlc-hook or sdlc-hook.cmd by its
// name or any path, which hands its first word to `sdlc hook`.
func launcherRuns(words []string, piped bool) hookEvent {
	if isLauncher(words[0]) {
		return eventOf(words[1:], piped)
	}
	for i := 1; i < len(words); i++ {
		if isLauncher(words[i]) && eventOf(words[i+1:], false) == expansionEvent {
			return expansionEvent
		}
	}
	return otherEvent
}

// unknownProgramRunsTheHook reads a program the reading cannot name, one the
// shell makes when the command runs, given `hook` and then an event:
// `"$(which sdlc)" hook "$E"`.
func unknownProgramRunsTheHook(words []string, piped bool) hookEvent {
	if program := words[0]; program != "" && !strings.ContainsAny(program, "$%`*?[") {
		return otherEvent
	}
	if sub, rest, found := nextWord(words[1:]); found && strings.EqualFold(sub, "hook") {
		return eventOf(rest, piped)
	}
	return otherEvent
}

// handedTheExpansion reports a program handed the expansion as the launcher
// or sdlc would be: as its first word, or after a `hook`. That reads a copy
// of either under another name, `/tmp/x hook UserPromptExpansion`, and
// PowerShell's `@('hook','UserPromptExpansion')`, which is a command of its
// own to the lexer.
func handedTheExpansion(words []string) bool {
	if len(words) > 1 && strings.EqualFold(words[1], "UserPromptExpansion") {
		return true
	}
	hook := false
	for _, w := range words {
		switch {
		case strings.EqualFold(w, "hook"):
			hook = true
		case hook && strings.EqualFold(w, "UserPromptExpansion"):
			return true
		}
	}
	return false
}

// eventOf is the event the words after the hook name: the first that is not a
// flag. Every event Claude Code has is named in ASCII letters alone, so any
// other word is one the shell makes; and with none, the event is unread only
// where xargs or parallel hands the command more words.
func eventOf(rest []string, fed bool) hookEvent {
	for _, w := range rest {
		switch {
		case strings.HasPrefix(w, "-"):
			continue
		case strings.EqualFold(w, "UserPromptExpansion"):
			return expansionEvent
		case !asciiLetters(w):
			return unreadEvent
		}
		return otherEvent
	}
	if fed {
		return unreadEvent
	}
	return otherEvent
}

// asciiLetters reports whether a word is one or more ASCII letters and
// nothing else.
func asciiLetters(w string) bool {
	for i := range len(w) {
		if c := w[i] | 0x20; c < 'a' || c > 'z' {
			return false
		}
	}
	return w != ""
}

// isLauncher reports whether a word runs the plugin's launcher, by its name
// folded, from any directory.
func isLauncher(word string) bool {
	name := base(word)
	return name == "sdlc-hook" || name == "sdlc-hook.cmd"
}

// splitLists is words with each PowerShell list taken apart: the lexer reads
// `-ArgumentList 'hook','UserPromptExpansion'` as the one word
// `hook,UserPromptExpansion`. Nearly no word holds a comma or a parenthesis,
// so the words are handed back as they are unless one does.
func splitLists(words []string) []string {
	if !slices.ContainsFunc(words, isList) {
		return words
	}
	out := make([]string, 0, len(words)+1)
	for _, w := range words {
		if !isList(w) {
			out = append(out, w)
			continue
		}
		for piece := range strings.SplitSeq(w, ",") {
			if piece = strings.TrimRight(strings.TrimLeft(piece, "@("), ")"); piece != "" {
				out = append(out, piece)
			}
		}
	}
	return out
}

func isList(w string) bool { return strings.ContainsAny(w, ",(@") }

// started is what PowerShell's Start-Process, or cmd's start, runs, as the
// program first and then the words it hands that program. Any other command's
// words are handed back as they are.
func started(words []string) []string {
	if len(words) < 2 {
		return words
	}
	switch base(words[0]) {
	case "start-process", "saps", "start":
	default:
		return words
	}
	run := startedProgram(words)
	return slices.Concat(run[:1], argumentList(run[1:]))
}

// argumentList is the words Start-Process hands its program. PowerShell
// passes one -ArgumentList string as the tail of the command line, so
// `Start-Process sdlc -ArgumentList 'hook UserPromptExpansion'`, one word to
// the lexer and to every reading of it, ran `sdlc hook UserPromptExpansion`.
// Every word is taken apart at white space, the list's first among them where
// it starts with a flag, `'--json hook UserPromptExpansion'`: a parameter's
// name has none, and a value that is not the argument list only gains words,
// which no reading turns from a refusal into an allowance.
func argumentList(words []string) []string {
	out := make([]string, 0, len(words)+1)
	for _, w := range words {
		// `-ArgumentList:'hook UserPromptExpansion'`, and `-Args:` likewise.
		if param, value, joined := strings.Cut(strings.TrimPrefix(w, "-"), ":"); joined && strings.HasPrefix(w, "-") &&
			(cutShort(strings.ToLower(param), "argumentlist") || strings.EqualFold(param, "args")) {
			w = value
		}
		out = append(out, strings.Fields(w)...)
	}
	return out
}

// startedProgram is Start-Process's or start's words with the program they
// run first: the program given by -FilePath, in either of its spellings, or
// the first word where that is not a parameter.
func startedProgram(words []string) []string {
	if !strings.HasPrefix(words[1], "-") {
		return words[1:]
	}
	for i := 1; i < len(words); i++ {
		param, value, joined := strings.Cut(strings.TrimPrefix(words[i], "-"), ":")
		if param = strings.ToLower(param); !cutShort(param, "filepath") && param != "path" && param != "pspath" {
			continue
		}
		if joined && value != "" {
			// `-FilePath:sdlc-hook.cmd`
			return slices.Concat([]string{value}, words[1:i], words[i+1:])
		}
		if i+1 < len(words) {
			return slices.Concat(words[i+1:i+2], words[1:i], words[i+2:])
		}
	}
	return words
}
