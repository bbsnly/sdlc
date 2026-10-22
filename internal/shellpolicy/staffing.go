package shellpolicy

import (
	"slices"
	"strconv"
	"strings"
)

// Who may run which `sdlc staffing` command, and what the project manager may
// run at all.
//
// `sdlc staffing assign` is a person's: what it records says a person chose
// it. `set` and `amend` are the project manager's, and the project manager runs
// no other sdlc command that writes. The word after `staffing` is read against
// the three the reading knows, `show`, `set` and `amend`, and anything else is
// refused: a list of what is refused would have to name every way a shell can
// make the word `assign`.

// How a command reaches sdlc. The three are not treated alike, because a path
// whose last part is sdlc -- a directory to `cd` into, a binary to copy -- is
// found exactly as a run behind a program the reading does not know is.
// Refusing there whatever the reading cannot make out refused `find`, `cp` and
// `git -C` on such a path, in every project.
const (
	// ranSdlc: sdlc is the program the command runs, once what only wraps it is
	// off the front, or a launcher runs it. Here whatever cannot be read is
	// refused.
	ranSdlc = iota
	// laterWord: sdlc is a later word behind a program the reading does not
	// know, as sdlcArgs finds it. Only what is written out is refused.
	laterWord
	// afterFlag: sdlc is a later word after a flag word, which a wrapper's own
	// flag is and a flag's value is too; or it follows a launcher in a form
	// that does not run it, such as `go build` or `go test` with the path of
	// the command's package. Only a written-out `staffing` is refused.
	afterFlag
)

// sdlcUse is one place a command names sdlc, and the words after it.
type sdlcUse struct {
	args []string
	way  int
	// fed is set when a program that hands its command more words from
	// standard input comes before sdlc in the words.
	fed bool
}

// sdlcUses is every place the words of a command -- one from programsAt -- name
// sdlc. It is nil for nearly every command, and says so before anything else.
func sdlcUses(words []string) []sdlcUse {
	if !slices.ContainsFunc(words, isSdlc) {
		return nil
	}
	if isSdlc(words[0]) {
		return []sdlcUse{{args: words[1:]}}
	}
	verbs, launcher := launcherVerbs(base(words[0]))
	if args, ok := launched(words[1:], verbs); launcher && ok {
		// A launcher is told which package to take a program from as well as
		// which program to run, and either can be a word that reads as sdlc.
		// Nothing in the words says which is which, so each one is read as the
		// run, but for one whose next word is sdlc again: that one named the
		// package. Read as the only run, it hid the command behind it.
		return launchedUses(words, args)
	}
	if mentionsOnly(base(words[0])) {
		return nil
	}
	var uses []sdlcUse
	fed := false
	for i := 1; i < len(words); i++ {
		if isSdlc(words[i]) {
			way := laterWord
			if launcher || strings.HasPrefix(words[i-1], "-") {
				way = afterFlag
			}
			uses = append(uses, sdlcUse{args: words[i+1:], way: way, fed: fed})
		}
		fed = fed || feeds(words[i])
	}
	return uses
}

// launchedUses is every run of sdlc a launcher's words can spell, args being
// what follows the first of them among words.
func launchedUses(words, args []string) []sdlcUse {
	var uses []sdlcUse
	for i := -1; i < len(args); i++ {
		if i >= 0 && !isSdlc(args[i]) {
			continue
		}
		if next, _, _ := nextWord(args[i+1:]); !isSdlc(next) {
			// The launcher's own words and what it was told to run before
			// this one: a program there that feeds words makes this fed.
			before := words[:len(words)-len(args)+max(i, 0)]
			uses = append(uses, sdlcUse{args: args[i+1:], fed: slices.ContainsFunc(before, feeds)})
		}
	}
	return uses
}

// launcherVerbs is, for a program that can run sdlc from a package or from
// source, the words of its own after which it does: `*` for one that only
// runs things. `go build ./cmd/sdlc` and a query of the package by name are
// not runs of sdlc.
func launcherVerbs(program string) (string, bool) {
	switch program {
	case "npx", "pnpx", "bunx":
		return "*", true
	case "go":
		return "run", true
	case "npm", "pnpm", "yarn":
		return "exec x dlx", true
	}
	return "", false
}

// launched is what follows sdlc where a launcher runs it: sdlc is the
// launcher's first word that is not a flag, or follows one of its verbs.
func launched(rest []string, verbs string) ([]string, bool) {
	ran := false
	for i, w := range rest {
		switch {
		case strings.HasPrefix(w, "-"):
		case isSdlc(w):
			return rest[i+1:], true
		case verbs != "*" && (ran || !slices.Contains(strings.Fields(verbs), w)):
			return nil, false
		default:
			ran = true
		}
	}
	return nil, false
}

// nextWord is the first word that is not a flag or a flag's value, as
// subcommand reads it, and the words after it. A word a substitution was taken
// out of is empty and still a word: found says there was one.
func nextWord(args []string) (word string, rest []string, found bool) {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			if ValueFlags[args[i]] {
				i++
			}
			continue
		}
		return args[i], args[i+1:], true
	}
	return "", nil, false
}

// writtenOut reports whether a word is the word sdlc will be given: not one
// the shell makes when the command runs, from a variable, a substitution, a
// pattern, a brace list, a home directory or the history.
func writtenOut(word string) bool {
	return word != "" && !strings.ContainsAny(word, "$`%(){}*?[~!") && !strings.HasPrefix(word, "=")
}

// asksForHelp reports whether sdlc would print help for these words and do
// nothing else, read as its flag parser reads them: the word after a flag that
// takes a value is that flag's value whatever it looks like, nothing after
// `--` is a flag, and the last help flag is the one that counts. Found anywhere
// among the words, a help flag written as a note or a message let the project
// manager run the command the note belonged to; and `--help --help=false`
// read as help while sdlc ran the command.
//
// A word the shell makes when the command runs could be `--help=false`, so
// with one among them nothing is known. The caller answers for a substitution
// the lexer took out of a word.
func asksForHelp(args []string) bool {
	help := false
	for i := 0; i < len(args) && args[i] != "--"; i++ {
		a := args[i]
		if !writtenOut(a) {
			return false
		}
		name, value, valued := strings.Cut(a, "=")
		switch {
		case a == "--help" || a == "-h":
			help = true
		case valued && (name == "--help" || name == "-h"):
			// What the parser cannot read as true or false is an error to
			// it, and neither help nor the command.
			on, err := strconv.ParseBool(value)
			if err != nil {
				return false
			}
			help = on
		case ValueFlags[a]:
			i++
		}
	}
	return help
}

// namesHelp reports a help flag among these words, as this read one before it
// read them as sdlc does. It is kept for one question: whether a command the
// project manager is now refused was let past that rule before.
func namesHelp(args []string) bool {
	for i := 0; i < len(args) && args[i] != "--"; i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			return true
		case ValueFlags[args[i]]:
			i++
		}
	}
	return false
}

// feeds reports whether a program hands the command after it more words from
// standard input.
func feeds(word string) bool { return base(word) == "xargs" || base(word) == "parallel" }

const assignmentRule = "assignment-is-a-human-decision"

// checkAssign keeps assigning a model or an effort to an agent with a person.
// What `sdlc staffing assign` records says a person chose it, and the project
// manager cannot change it afterwards, so a session that could run it could
// pin every agent where it liked in a person's name.
//
// cut is set for a command a word of which held a substitution. The lexer
// takes the substitution out of the word, so `--help"$(...)"` is read here as
// `--help` while sdlc may be handed `--help=false` and run the command; a help
// flag is not believed there.
func checkAssign(words []string, piped, cut bool) (Finding, bool) {
	for _, u := range sdlcUses(words) {
		first, rest, _ := nextWord(u.args)
		if first != "staffing" {
			continue
		}
		verb, _, found := nextWord(rest)
		if found && verb != "show" && verb != "set" && verb != "amend" ||
			!found && u.way == ranSdlc && (cut || !asksForHelp(u.args)) {
			return Finding{
				Rule: assignmentRule,
				Reason: "the word after `sdlc staffing` is not `show`, `set` or `amend`, so this may be " +
					"`sdlc staffing assign`, which records that a person chose a model or an effort for an agent",
				Route: "say what you would have asked for and stop; a person runs `/sdlc:assign`, or `sdlc staffing assign` " +
					"in their own terminal, outside Claude Code; `sdlc staffing --help` lists the commands",
			}, true
		}
		if piped || u.fed {
			return unread(true), true
		}
	}
	return Finding{}, false
}

// checkUnread refuses a run of sdlc whose subcommand cannot be read: the word
// after sdlc is made when the command runs, or sdlc is handed its words on
// standard input. Either could be a command only a person runs, which is why
// the rule is the person's; what it says is about the subcommand, because the
// command it refuses is most often not an assignment at all.
//
// It is asked only where sdlc is the program run. Behind a program the reading
// does not know, a word that cannot be read is as likely a path or a pattern.
func checkUnread(words []string, piped bool) (Finding, bool) {
	for _, u := range sdlcUses(words) {
		fed := piped || u.fed
		if first, _, found := nextWord(u.args); u.way == ranSdlc && (fed || found && !writtenOut(first)) {
			return unread(fed), true
		}
	}
	return Finding{}, false
}

func unread(fed bool) Finding {
	if fed {
		return Finding{
			Rule: assignmentRule,
			Reason: "sdlc is given its words on standard input, so they cannot be read, and they could be " +
				"a command only a person runs",
			Route: "pass the words on the command line, with the subcommand written out, as in `sdlc status`",
		}
	}
	return Finding{
		Rule: assignmentRule,
		Reason: "the word after sdlc is made when the command runs, so the subcommand cannot be read, " +
			"and it could be one only a person runs",
		Route: "write the subcommand out as a literal word, as in `sdlc status`, with no variable, " +
			"substitution or pattern in it",
	}
}

// checkRecorder keeps recording and amending the staffing with the project
// manager. The session that runs the loop is the one a staffing spends less
// on, and the agents it is about are the ones it leaves out: none of them
// writes it.
func checkRecorder(words []string, s State) (Finding, bool) {
	if s.Agent == "project-manager" {
		return Finding{}, false
	}
	for _, u := range sdlcUses(words) {
		first, rest, _ := nextWord(u.args)
		if verb, _, _ := nextWord(rest); first != "staffing" || verb != "set" && verb != "amend" {
			continue
		}
		who := "the main conversation"
		if s.Agent != "" {
			who = s.Agent
		}
		return Finding{
			Rule: "staffing-is-recorded-by-the-project-manager",
			Reason: who + " is writing the staffing, which says who takes part in the story and what " +
				"each is asked for, and that is not for the session it is about to decide",
			Route: "delegate to sdlc:project-manager with the story and what happened; it records the " +
				"staffing with `sdlc staffing set` and changes it with `sdlc staffing amend`",
		}, true
	}
	return Finding{}, false
}

// sdlcCommands are the commands of sdlc, by name. Behind a program the reading
// does not know, one of these straight after sdlc is a run of it; any other
// word is as likely where a file is being copied to.
var sdlcCommands = []string{
	"init", "status", "story", "log", "ack", "start", "stop", "gate", "artifact", "review", "freeze",
	"unfreeze", "escalate", "approve", "release", "parallel", "cost", "doctor", "version", "staffing",
	"codemap", "remap",
}

// checkManager holds the project manager to the staffing and to what only
// reads. It decides who takes part in a story; an agent that could also record
// a gate or a review would be deciding the story. help says whether the words
// handed to sdlc ask it for help alone.
func checkManager(words []string, s State, help func(args []string) bool) (Finding, bool) {
	if s.Agent != "project-manager" {
		return Finding{}, false
	}
	for _, u := range sdlcUses(words) {
		first, rest, found := nextWord(u.args)
		if u.way == afterFlag || u.way == laterWord && !slices.Contains(sdlcCommands, first) ||
			managerMay(first, rest, found, help(u.args)) {
			continue
		}
		return Finding{
			Rule: "project-manager-only-staffs",
			Reason: "the project manager says who takes part in a story and what each is asked for, " +
				"and runs no sdlc command that writes anything else",
			Route: "record the staffing with `sdlc staffing set` or change it with `sdlc staffing amend`, " +
				"and read with `sdlc staffing show`, `sdlc status` or `sdlc review list`; anything else, " +
				"handing the story to a person included, is for the main conversation running the " +
				"runbook: say so in your result and stop. If you only meant to read, `sdlc version`, " +
				"`sdlc status` and any command with `--help` are allowed; a word that ends in sdlc, " +
				"a path included, is read as a run of it",
		}, true
	}
	return Finding{}, false
}

// managerMay is the list of what the project manager may run: a list of what
// it may, so a command this version does not have, or a subcommand that
// cannot be read, is not one of them.
func managerMay(first string, rest []string, found, help bool) bool {
	next, _, more := nextWord(rest)
	switch {
	case help:
		return true
	case !found:
		return false
	}
	switch first {
	case "status", "log", "doctor", "version", "help":
		return true
	case "staffing":
		return next == "show" || next == "set" || next == "amend"
	case "review", "story", "artifact":
		return next == "list"
	case "cost":
		return !more
	}
	return false
}
