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
// approved the story from the model's own shell.
//
// On the events it records (SubagentStart, SubagentStop, Notification,
// StopFailure, PostCompact, PostToolUseFailure, PostToolUse and Stop) the hook
// writes agent events to the gate record and measurements to the trace, as
// Claude Code reported them, so a payload the model wrote forges that record
// (US-112). Those are refused under a rule of their own. PreToolUse and
// SessionStart record nothing run by hand, and reviewers here drive the hook
// on them; they stay open, as does any other event written out.
//
// The launcher is read as the program, and as a later word under any program,
// `.` and `source` among them, where a recorded event or the expansion follows
// it: read under sh and bash alone, `. plugin/bin/sdlc-hook SubagentStop` ran
// the hook unread (security, round 2, R2-1).
//
// The hook no longer tells its own call by AI_AGENT on the events it records:
// Claude Code in Claude Desktop sets AI_AGENT for a hook too, so the hook
// records whatever AI_AGENT holds, and these rules alone keep a model's
// payload out of the record (US-112, the person's decision of 2026-10-29). So
// interpreter code that runs the hook on a recorded event, as written out, is
// refused as well (interpreterRunsTheHook), and so is a program handed a
// recorded event as its first word, as a copy of the launcher would be
// (firstWordEvent). What stays open: code that builds the event's name, the
// command or the path; a copy of the launcher handed Stop or Notification,
// which `docker stop web` reads as; and a copy run through eval or a script
// file. docs/enforcement.md's Not held names them.

// slashCommandRule is the rule a hand-run UserPromptExpansion is refused
// under, from the session holding a story and from any other.
const slashCommandRule = "slash-command-is-a-human-decision"

// recordedEventRule is the rule a hand-run hook on an event it records is
// refused under, from the session holding a story and from any other.
const recordedEventRule = "recorded-event-comes-from-the-host"

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
	// recordedEvent is one of the events the hook records, in any case.
	recordedEvent
)

// checkHandRunHook refuses a command that runs the sdlc hook on a
// UserPromptExpansion, on an event it cannot read, or on one it records, and
// interpreter code that runs it on the expansion or a recorded event. text is
// the reading, which an interpreter fed on standard input reads.
func checkHandRunHook(r ran, text string) (Finding, bool) {
	event := handRunEvent(r)
	if event == otherEvent {
		event = interpreterRunsTheHook(r.words, r.input, text)
	}
	switch event {
	case otherEvent:
		return Finding{}, false
	case recordedEvent:
		return recordedEventFinding(), true
	default:
		return handRunHookFinding(event), true
	}
}

// recordedEventFinding is the refusal of the hook run by hand on an event it
// records. Round 3's route said a run from the model's shell records nothing
// in the trace, which held only while the hook checked AI_AGENT. Since the
// hook records whatever AI_AGENT holds, a hand-run PreToolUse it refuses is
// recorded as a refusal in the trace of the story the payload's session
// holds; so the route names the session id, and another session's id finds
// no held story and records nothing (AC-15). It does not say the loop's files
// are left as they were, because a commit payload on PreToolUse plants the
// observation Claude Code's own call would.
func recordedEventFinding() Finding {
	return Finding{
		Rule: recordedEventRule,
		Reason: "this runs the sdlc hook on an event it records: on that event the hook writes the story's " +
			"agent events to its gate record and its measurements to the trace, as Claude Code reported " +
			"them, and a payload written by a model is not Claude Code's report",
		Route: "leave the recording to Claude Code, which runs the hook on these events itself; to see what " +
			"the hook answers, run `sdlc hook PreToolUse` with a payload on standard input that has a " +
			"`session_id` of its own: it answers on stdout as it would answer Claude Code, and a payload it " +
			"refuses that carries this session's id is recorded in the held story's trace as a refusal of a " +
			"call that never ran, and counted in its figures",
	}
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
			"and `/sdlc:assign`; to try the hook yourself, run it on PreToolUse, written out as a literal " +
			"word, such as `sdlc hook PreToolUse`",
	}
}

// handRunEvent is the event a command runs the hook on, where it runs the
// hook on the expansion, on an event it cannot read or on one it records, and
// otherEvent where it does not. The words are read as the lexer marked them,
// so that a substituted program or event is $SUB and not gone, and the word
// xargs or find fills in is read as $SUB too: it is one the shell makes.
//
// sdlc and the launcher are read as the program the command runs, where an
// event that cannot be read is refused, and as a later word behind a program
// the reading does not know, where only the expansion written out is: there
// they are as likely a path being copied, added or checked, and refusing
// whatever could not be read refused `cp plugin/bin/sdlc-hook "$D/"` and
// `git add plugin/bin/sdlc-hook plugin/bin/sdlc-hook.cmd`, as staffing.go
// says of sdlc.
func handRunEvent(r ran) hookEvent {
	words := handRunWords(r)
	if len(words) == 0 {
		return otherEvent
	}
	for _, read := range []func([]string, bool) hookEvent{sdlcRunsTheHook, launcherRuns, unknownProgramRunsTheHook} {
		if event := read(words, r.piped); event != otherEvent {
			return event
		}
	}
	switch {
	case handedTheExpansion(words):
		return expansionEvent
	case handedARecordedEvent(words):
		return recordedEvent
	}
	return otherEvent
}

// handRunWords are the words handRunEvent reads of a command: as the lexer
// marked them, with the word xargs or find fills in as $SUB, and what
// Start-Process runs taken apart; or none for a command that only mentions
// what it is given, `echo SubagentStop`.
func handRunWords(r ran) []string {
	words := r.marked
	if len(words) == 0 {
		words = r.words
	}
	if r.replace != "" && slices.Contains(words, r.replace) {
		words = slices.Clone(words)
		for i, w := range words {
			if w == r.replace {
				words[i] = "$SUB"
			}
		}
	}
	words = started(splitLists(words))
	if len(words) == 0 || mentionsOnly(base(words[0])) {
		return nil
	}
	return words
}

// interpreterRunsTheHook is the event interpreter code runs the hook on,
// where it is the expansion or a recorded event written out: a program that
// runs code (interpretsCode), whose code, its words after it and the reading
// where it is fed standard input, is cut into tokens at every character no
// command or path is made of, and read as handRunEvent reads words: `hook`
// and then the event, or the launcher and then the event. Read as one word,
// `python3 -c 'os.system("sdlc hook SubagentStop < p.json")'` ran the hook on
// a payload the model wrote, which the hook's AI_AGENT check held until the
// person's decision of 2026-10-29 took it off recording. Code that builds the
// event's name, the command or the path is not read.
func interpreterRunsTheHook(words []string, input bool, text string) hookEvent {
	if len(words) == 0 || !interpretsCode(base(words[0])) {
		return otherEvent
	}
	code := words[1:]
	if input {
		code = append(slices.Clip(code), text)
	}
	tokens := []string{words[0]}
	for _, c := range code {
		tokens = append(tokens, strings.FieldsFunc(c, notACommandRune)...)
	}
	switch {
	case handedTheExpansion(tokens):
		return expansionEvent
	case handedARecordedEvent(tokens):
		return recordedEvent
	}
	if event := launcherRuns(tokens, false); event == expansionEvent || event == recordedEvent {
		return event
	}
	return otherEvent
}

// notACommandRune reports a character no program name, event or path is
// written with, at which interpreter code is cut into tokens.
func notACommandRune(c rune) bool {
	return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune(`_./\:@+-`, c)
}

// firstWordEvent reports an event the hook records, in any case, that no
// program takes as its first word: a copy of the launcher handed one, as in
// `cp plugin/bin/sdlc-hook /tmp/y; /tmp/y SubagentStop < p.json`, ran the
// hook on it. Stop and Notification are left out, since `docker stop web`
// and `notify Notification` are no hook (docs/enforcement.md, Not held).
func firstWordEvent(w string) bool {
	return recorded(w) && !strings.EqualFold(w, "stop") && !strings.EqualFold(w, "notification")
}

// sdlcRunsTheHook reads sdlc running `hook`, by every spelling sdlcUses
// reads.
func sdlcRunsTheHook(words []string, piped bool) hookEvent {
	for _, u := range sdlcUses(words) {
		sub, rest, _ := nextWord(u.args)
		if !strings.EqualFold(sub, "hook") {
			continue
		}
		switch event := eventOf(rest, piped || u.fed); {
		case event == expansionEvent, event == recordedEvent, event == unreadEvent && u.way == ranSdlc:
			return event
		}
	}
	return otherEvent
}

// launcherRuns reads the plugin's launcher, sdlc-hook or sdlc-hook.cmd by its
// name or any path, which hands its first word to `sdlc hook`: as the program,
// and as a later word under any program where the expansion or a recorded
// event follows it. A later launcher word with neither after it is a path
// being read, copied or checked (`cat`, `chmod +x`, `sh -n`), and the event
// before it is a word being searched for.
func launcherRuns(words []string, piped bool) hookEvent {
	if isLauncher(words[0]) {
		return eventOf(words[1:], piped)
	}
	for i := 1; i < len(words); i++ {
		if !isLauncher(words[i]) {
			continue
		}
		if event := eventOf(words[i+1:], false); event == expansionEvent || event == recordedEvent {
			return event
		}
	}
	return otherEvent
}

// unknownProgramRunsTheHook reads a program the reading cannot name, one the
// shell makes when the command runs, given `hook` and then an event:
// `"$(which sdlc)" hook "$E"`; or given a recorded event as its first word,
// as the launcher would be: `L=plugin/bin/sdlc-hook; $L SubagentStop`. A
// made program given any other word is not read (`X=go; $X test ./...`).
func unknownProgramRunsTheHook(words []string, piped bool) hookEvent {
	if program := words[0]; program != "" && !strings.ContainsAny(program, "$%`*?[") {
		return otherEvent
	}
	if sub, rest, found := nextWord(words[1:]); found && strings.EqualFold(sub, "hook") {
		return eventOf(rest, piped)
	}
	if eventOf(words[1:], false) == recordedEvent {
		return recordedEvent
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

// handedARecordedEvent reports a program handed a recorded event after a
// `hook` word, as sdlc would be: a copy of sdlc under another name, `/tmp/x
// hook SubagentStart`. A recorded event as the first word alone is not read,
// as the expansion is: `docker stop web` and `echo Stop` are no hook.
func handedARecordedEvent(words []string) bool {
	for i := 1; i < len(words); i++ {
		if strings.EqualFold(words[i], "hook") && eventOf(words[i+1:], false) == recordedEvent {
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
		case recorded(w):
			return recordedEvent
		}
		return otherEvent
	}
	if fed {
		return unreadEvent
	}
	return otherEvent
}

// recorded reports whether an event word, in any case, is one the hook
// records.
func recorded(event string) bool {
	switch strings.ToLower(event) {
	case "subagentstart", "subagentstop", "notification", "stopfailure", "postcompact",
		"posttoolusefailure", "posttooluse", "stop":
		return true
	}
	return false
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
