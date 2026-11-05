package shellpolicy

import (
	"slices"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
)

// madeWord reports whether the shell makes a word, read on its mark, when the
// command runs: writtenOut's set, a caret, which cmd takes out of a word and
// zsh's EXTENDED_GLOB reads as a pattern, and under PowerShell a leading @, a
// splat that hands sdlc any words. It is one class and not a list of
// spellings: each round of review found a new way a shell makes a word, and a
// reading that cannot see a word as written refuses it. writtenOut keeps its
// own set, since `HEAD^` outside the span is a name.
func madeWord(mark string, powerShell bool) bool {
	return !writtenOut(mark) || strings.ContainsRune(mark, '^') || powerShell && splats(mark)
}

// splats reports whether PowerShell reads a word as a splat, alone or as a
// flag's value after its =, either of which hands sdlc any words.
func splats(mark string) bool {
	_, value, valued := strings.Cut(mark, "=")
	return strings.HasPrefix(mark, "@") || valued && strings.HasPrefix(mark, "-") && strings.HasPrefix(value, "@")
}

// linedUp is the words of a run and the marks to read them on. Where the two
// do not line up, a substitution was cut and moved the words, and the run is
// read on its marks alone, so that every cut shows as made. Neither slice is
// indexed by the other's positions, so no length can panic the hook.
func linedUp(words, marked []string) (read, marks []string) {
	if len(marked) == len(words) {
		return words, marked
	}
	return marked, marked
}

// Where a made word stands, in a person's words, for the refusal's reason.
const (
	madeBeforeSubcommand = "before the subcommand"
	madeBeforeVerb       = "between `review` and the word after it"
	madeVerb             = "as the word after `review`"
	madeGate             = "before the gate and role"
	madeInPair           = "between the gate and the role"
	madeNoPair           = "where the gate and the role should be"
	madeProgram          = "as the program name"
	madeInScript         = "in a script or string a shell runs later"
)

// reviewReading is what the closed reading of `sdlc review` found.
type reviewReading struct {
	role    string // the role of the first readable pair, or ""
	made    string // where a made word stands, or ""
	verb    string // a written verb that is neither add nor list, or ""
	addWord bool   // the words read `review add`
}

// readReview reads the words after sdlc, args, on their marks, from sdlc to
// the gate and role pair, and no further: every value after the pair is the
// reviewer's own. A made word anywhere before the pair could hand sdlc another
// gate and role than the words show, so it is reported where it stands, and
// the caller refuses it to every agent, the reviewer it names among them.
func readReview(args, marks []string, powerShell bool) reviewReading {
	made := func(i int) bool { return i < len(marks) && madeWord(marks[i], powerShell) }
	i, where := skipFlags(args, 0, made, madeBeforeSubcommand)
	if where != "" {
		return reviewReading{made: where}
	}
	// A splat hands sdlc any number of words, so it stands before the
	// subcommand as much as in its place.
	if powerShell && i < len(marks) && splats(marks[i]) {
		return reviewReading{made: madeBeforeSubcommand}
	}
	if i >= len(args) || args[i] != "review" || made(i) {
		return reviewReading{}
	}
	i, where = skipFlags(args, i+1, made, madeBeforeVerb)
	if where != "" {
		return reviewReading{made: where}
	}
	switch {
	case i >= len(args):
		return reviewReading{}
	case made(i):
		return reviewReading{made: madeVerb}
	case args[i] == "list":
		return reviewReading{}
	case args[i] != "add":
		return reviewReading{verb: args[i]}
	}
	return readPair(args[i+1:], marks[min(i+1, len(marks)):], powerShell)
}

// skipFlags walks the flags from i to the next positional word, reporting the
// first made flag name or value as standing at where.
func skipFlags(args []string, i int, made func(int) bool, where string) (int, string) {
	for ; i < len(args) && strings.HasPrefix(args[i], "-"); i++ {
		if made(i) {
			return i, where
		}
		if ValueFlags[args[i]] {
			i++
			if made(i) {
				return i, where
			}
		}
	}
	return i, ""
}

// readPair walks the words after `review add` to the first readable pair. A
// made positional word before it, the gate included, could be the gate; a
// made flag name could take the next word as its value; and a made value
// between the first positional word and the pair's role could take the role
// away. Before the first positional word one made value takes one word, and
// `review add` takes exactly three, so the written pair still records under
// itself or cli refuses the count.
func readPair(args, marks []string, powerShell bool) reviewReading {
	made := func(i int) bool { return i < len(marks) && madeWord(marks[i], powerShell) }
	positional, last := 0, -1
	madeValue := false
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			next, name, value := pairFlag(args, i, made, powerShell)
			madeValue = madeValue || value
			switch {
			case name:
				return reviewReading{made: madeGate, addWord: true}
			case madeValue && positional > 0:
				return reviewReading{made: madeInPair, addWord: true}
			}
			i = next
			continue
		}
		if made(i) {
			return reviewReading{made: madeGate, addWord: true}
		}
		if r, ok := pairOf(args, last, i); ok {
			return reviewReading{role: r, addWord: true}
		}
		positional, last = positional+1, i
	}
	if noPairRefused(args, positional, madeValue) {
		return reviewReading{made: madeNoPair, addWord: true}
	}
	return reviewReading{addWord: true}
}

// noPairRefused reports whether words with no pair by the end are refused:
// where a value was made, or the positional words are not the three cli
// reads, before help is believed.
func noPairRefused(args []string, positional int, madeValue bool) bool {
	return madeValue || positional != 3 && (positional != 0 || !asksForHelp(args))
}

// pairOf is the role of the pair args[gate] and args[role], where there is a
// gate before it.
func pairOf(args []string, gate, role int) (string, bool) {
	if gate < 0 {
		return "", false
	}
	r, ok := model.FindReviewer(model.Gate(args[gate]), args[role])
	return r.Role, ok
}

// pairFlag reads the flag at args[i]: where its value ends, whether its name
// is made, and whether its value is.
func pairFlag(args []string, i int, made func(int) bool, powerShell bool) (end int, madeName, madeValue bool) {
	name, _, valued := strings.Cut(args[i], "=")
	switch {
	case made(i) && (!valued || madeWord(name, powerShell)):
		return i, true, false
	case made(i):
		return i, false, true
	case ValueFlags[args[i]]:
		return i + 1, false, made(i + 1)
	}
	return i, false, false
}

// programMade reports whether sdlc, found as the program run in words, is a
// program word the shell partly makes, read on its mark: its last path
// element differs between the word and the mark, or the mark read up to its
// first made character names sdlc. `sdlc$(true)` runs sdlc with whatever
// follows, and the lexer leaves `sdlc` in the word.
func programMade(word, mark string, powerShell bool) bool {
	if !madeWord(mark, powerShell) {
		return false
	}
	return base(mark) != base(word) || isSdlc(writtenPrefix(mark))
}

// reviewUse reads one use of sdlc as the program run, its words from at, and
// answers the role of the pair it records or why it is refused.
func reviewUse(words, marks []string, at int, cut, powerShell bool) (string, Finding, bool) {
	args, argMarks := words[at:], marks[at:]
	if at > 0 && programMade(words[at-1], marks[at-1], powerShell) {
		// A made subcommand behind it is checkUnread's, under its own name,
		// and so is one written out that is not review.
		if first, _, found := nextWord(argMarks); found && madeWord(first, powerShell) || otherCommand(args) {
			return "", Finding{}, false
		}
		// The program word may hand sdlc words in front of the written ones,
		// ` review` in `sdlc$(printf ' review') add`, so none of them is read.
		if cut || !asksForHelp(args) {
			return "", madeFinding(madeProgram), true
		}
	}
	read := readReview(args, argMarks, powerShell)
	switch {
	case read.made != "":
		return "", madeFinding(read.made), true
	case read.verb != "" && (cut || !asksForHelp(args)):
		return "", Finding{
			Rule:   reviewerRuleName,
			Reason: "the hook knows `sdlc review add` and `sdlc review list` only, and " + strconv.Quote(read.verb) + " is neither",
			Route:  "use `add` or `list`; `sdlc review --help` lists both",
		}, true
	}
	return read.role, Finding{}, false
}

// madeProgramRead is what follows a program word the shell makes, read where
// sdlcUses found no sdlc that runs.
type madeProgramRead struct {
	args, marks []string
	// sdlcRead is set where the word's written start names sdlc (`sdlc$X`,
	// `sdlc*`): sdlc runs, and args are the words after it.
	sdlcRead bool
	// wrapped is set where sdlc follows what a known wrapper's made option
	// leaves in front of it (`env $X sdlc`, `timeout $(printf -- -s) 9 5
	// sdlc`): sdlc runs, and args are its words.
	wrapped bool
}

// readMadeProgram reads a run whose program word the shell makes, or whose
// program word is a duration, a count or a signal name where something was
// cut, which only a wrapper's cut option leaves there. Where sdlc cannot be
// read in it (`sdl?`, `"$SDLC"`, `$(command -v sdlc)`), the program may not be
// sdlc at all, so args are what follows a later written sdlc, or else what
// follows the program word, and only a reading of them as `review add` or a
// person's command is refused: refusing anything after such a word refused
// `$CC`, `"$EDITOR"` and `$PAGER` in every project.
func readMadeProgram(words, marks []string, uses []sdlcUse, cut, powerShell, undecoded bool) (madeProgramRead, bool) {
	// A reading another reading decodes waits for it where the program word,
	// less its $, names sdlc: `$'sdlc' remap US-001` is the remap rule's.
	if len(marks) == 0 || undecoded && isSdlc(strings.TrimPrefix(words[0], "$")) {
		return madeProgramRead{}, false
	}
	left := cut && timeoutValue(words[0])
	if !left && !madeWord(marks[0], powerShell) {
		return madeProgramRead{}, false
	}
	if slices.ContainsFunc(uses, func(u sdlcUse) bool { return u.way == ranSdlc }) {
		return madeProgramRead{}, false
	}
	if !left && isSdlc(writtenPrefix(marks[0])) {
		return madeProgramRead{args: words[1:], marks: marks[1:], sdlcRead: true}, true
	}
	return laterSdlc(words, marks, powerShell, left)
}

// laterSdlc reads the words after a made program word in which sdlc cannot be
// read: from a later written sdlc, wrapped where only what a wrapper's made
// option leaves stands before it, or else from the program word. left is a
// program word a cut option left, which is no program without a sdlc after it.
func laterSdlc(words, marks []string, powerShell, left bool) (madeProgramRead, bool) {
	wrapped := true
	for j := 1; j < len(words); j++ {
		if isSdlc(words[j]) && !madeWord(marks[j], powerShell) {
			return madeProgramRead{args: words[j+1:], marks: marks[j+1:], wrapped: wrapped}, true
		}
		wrapped = wrapped && (timeoutValue(words[j]) || madeWord(marks[j], powerShell))
	}
	return madeProgramRead{args: words[1:], marks: marks[1:]}, !left
}

// timeoutValue reports whether w is what timeout takes as an option's value
// or its duration: a number, with a unit or without, or a signal name as
// `kill -l` lists them on Linux and macOS, in either case, with SIG or
// without. Left as the program word by a cut option, `timeout $(printf -- -s)
// KILL 5 sdlc` read sdlc as a later word.
func timeoutValue(w string) bool {
	if num := strings.TrimRight(w, "smhd"); num != "" && num != "." &&
		strings.Trim(num, "0123456789.") == "" && strings.Count(num, ".") <= 1 {
		return true
	}
	name := strings.ToUpper(w)
	name = strings.TrimPrefix(name, "SIG")
	if strings.HasPrefix(name, "RTMIN") || strings.HasPrefix(name, "RTMAX") {
		return true
	}
	switch name {
	case "HUP", "INT", "QUIT", "ILL", "TRAP", "ABRT", "IOT", "BUS", "EMT", "FPE", "KILL", "USR1", "SEGV", "USR2",
		"PIPE", "ALRM", "TERM", "STKFLT", "CHLD", "CLD", "CONT", "STOP", "TSTP", "TTIN", "TTOU", "URG", "XCPU",
		"XFSZ", "VTALRM", "PROF", "WINCH", "IO", "POLL", "PWR", "INFO", "SYS", "LOST", "UNUSED":
		return true
	}
	return false
}

// checkMadeProgram is the reviewer rule's reading of readMadeProgram.
func checkMadeProgram(words, marks []string, cut, powerShell, undecoded bool) (Finding, bool) {
	m, ok := readMadeProgram(words, marks, sdlcUses(words), cut, powerShell, undecoded)
	if !ok || m.sdlcRead && !cut && asksForHelp(m.args) {
		return Finding{}, false
	}
	// A made subcommand behind it is checkUnread's, under its own name, and
	// so is one written out that is not review where sdlc is read.
	if (m.sdlcRead || m.wrapped) && subcommandMade(m.marks, powerShell) || m.sdlcRead && otherCommand(m.args) {
		return Finding{}, false
	}
	if read := readReview(m.args, m.marks, powerShell); m.sdlcRead || read.addWord || read.made != "" {
		return madeFinding(madeProgram), true
	}
	return Finding{}, false
}

// otherCommand reports whether args, sdlc's words after a program word the
// shell makes, name a command of sdlc other than review. Such a line is
// refused as a made program word under checkUnread's rule, whose reason says
// sdlc could be handed a person's command: under this rule, the project
// manager's `sdlc$(true) staffing set` was refused as a review it never
// named.
func otherCommand(args []string) bool {
	first, _, found := nextWord(args)
	return found && first != "review" && slices.Contains(sdlcCommands, first)
}

// personOnly reports whether args, read as sdlc's, are a command only a person
// runs, by the person's own checks, so that a later one follows its own check
// with no second list here.
func personOnly(args []string) bool {
	words := append([]string{"sdlc"}, args...)
	for _, check := range []func([]string) (Finding, bool){checkApprove, checkUnfreeze, checkAck, checkRemap, checkRelease} {
		if _, ok := check(words); ok {
			return true
		}
	}
	_, ok := checkAssign(words, false, false)
	return ok
}

// writtenPrefix is a mark up to its first made character.
func writtenPrefix(mark string) string {
	if i := strings.IndexAny(mark, "$`%(){}*?[~!^"); i >= 0 {
		return mark[:i]
	}
	return mark
}

// madeFinding is the refusal of a made word in the span of `sdlc review`.
func madeFinding(where string) Finding {
	reason := "a word " + where + " is made by the shell when the command runs, so the hook cannot read which " +
		"gate and role sdlc is handed: such a word could hand sdlc a different command, one that records a review"
	route := "write the subcommand, and for a review its gate and role, as plain words, and put flags after them"
	if where == madeProgram {
		reason = "this runs sdlc through a program name, or an option of a wrapper such as `env` or `timeout`, that " +
			"the shell makes when the command runs, so the hook cannot read what sdlc is handed: it could be a " +
			"review recorded under another reviewer's name, or a command only a person runs"
		route = "write the program word (the program name, the command's first word) out: `sdlc`, or " +
			"`npx sdlc@<version>` with the version typed, and any wrapper's options, then the subcommand, and for a " +
			"review its gate and role, as plain words, and put flags after them"
	}
	return Finding{Rule: reviewerRuleName, Reason: reason, Route: route}
}

const reviewerRuleName = "review-is-recorded-by-its-reviewer"
