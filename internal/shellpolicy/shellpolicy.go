// Package shellpolicy reads a shell command well enough to notice the two ways
// around everything else in this tool.
//
// Every other rule governs the file-writing tools. A shell command is not one
// of them, so `cat > .sdlc/stories/A-1/ANALYSIS.md` would walk straight past a
// rule that says only sdlc writes that file, and `git commit` would walk past
// the gate that exists to stand in front of it.
//
// This is pattern matching, not a shell. It is a discipline control, not a
// sandbox: it stops an assistant taking a shortcut, and it does not pretend to
// stop one that is determined to get out. Saying that plainly is better than
// implying a guarantee nobody can keep -- so the rules here are deliberately
// narrow, and each one names the sanctioned route rather than just refusing.
package shellpolicy

import (
	"cmp"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// Words the rules compare against in more than one place, named once so that
// two readers of the same option cannot drift apart by a typing slip.
const (
	// The two rules a refusal over the loop's own files or a frozen test names.
	ruleLoopState  = "loop-state-through-the-tool"
	ruleFrozenTest = "frozen-test-through-the-tool"

	// The loop's own paths, slash-separated as they are inside the loop.
	loopStateDir   = ".sdlc/state"
	loopConfigFile = ".sdlc/config.json"

	// git's global options that take a value, and the joined form of one.
	optGitDir       = "--git-dir"
	optGitDirEquals = "--git-dir="
	optWorkTree     = "--work-tree"
	optNamespace    = "--namespace"
	optConfigEnv    = "--config-env"
	optAttrSource   = "--attr-source"
	optForce        = "--force"

	// Names in the tables of what a git subcommand commits.
	subCherryPick      = "cherry-pick"
	longNoCommit       = "no-commit"
	longStrategyOption = "strategy-option"

	// aliasPrefix opens a configuration key that defines a git alias, and
	// theAlias opens the sentence that names one the rules could not read.
	aliasPrefix = "alias."
	theAlias    = "the alias "

	// findDelete is find's own action that removes what it matched.
	findDelete = "-delete"

	// destinationColon is PowerShell's -Destination joined to its value.
	destinationColon = "-Destination:"

	// unsettledChars are the characters that leave a path for the shell still
	// to settle: an expansion, a glob or a brace.
	unsettledChars = "$%~*?[{"
)

// State is what the loop knows, worked out by the caller. Keeping it out here
// leaves this package a decision table that can be read and tested on its own.
type State struct {
	// CommitReady is whether the story has been through the gates that come
	// before committing. CommitWhy says what is missing when it has not.
	CommitReady bool
	CommitWhy   string

	// StoryFinished is whether every gate on the story has passed, which is
	// when ending the iteration is the runbook's last step rather than the way
	// round every rule at once.
	StoryFinished bool

	// Committed is whether the story's commit gate has passed. After it, the
	// one rebase the session may run is its own branch onto trunk's, so that a
	// story trunk has moved past can still land.
	Committed bool

	// Trunk, when set, is the name of trunk's branch, and IsTrunk reports
	// whether a name git reads in the session's own checkout is trunk's branch
	// ref: a tag or another ref with trunk's short name is not.
	Trunk   func() string
	IsTrunk func(name string) bool

	// Fresh, when set, reports whether the work a commit would record is the
	// work that was reviewed, and says what changed when it is not. It is asked
	// only for a commit whose gates have passed, because answering it means
	// measuring the working tree.
	Fresh func() (bool, string)

	// Commits, when set, is told about a commit this package allows in this
	// repository that commits now, so that the loop can write down the HEAD it
	// is made on. It is asked by the same classification the refusal is, never
	// by a second reading of "is this a commit" somewhere else.
	Commits func()

	// Nested, when set, reports whether a directory, repository-relative and
	// slash-separated, is inside a repository of its own within the project:
	// a submodule, or a fixture a test made. A commit there is that
	// repository's, and the recorder must not write down this one's HEAD for
	// it; the refusal holds it back all the same.
	Nested func(dir string) bool

	// Frozen is every acceptance test the freeze holds, repository-relative
	// and slash-separated. Empty before the freeze, and before then there is
	// nothing here to protect.
	Frozen []string

	// IsTest stands in for Frozen when a freeze exists and cannot be read.
	// Every file it calls a test then counts as frozen: a freeze nobody can
	// read is not no freeze, or breaking it would be the way round it.
	IsTest func(path string) bool

	// NewTest, when set, reports whether a path would be a test file added
	// after the freeze -- one the freeze does not hold, in a project that does
	// not allow new test files. The file tools refused adding one, and a
	// redirect or a `touch` did not.
	NewTest func(path string) bool

	// ImplementerTest, set only when the implementer is the one running the
	// command, reports whether a path is a test file at all. The implementer
	// does not write tests, frozen or not: the file tools refused it and a
	// redirect did not, before the freeze and whenever new test files were
	// allowed after it.
	ImplementerTest func(path string) bool

	// Dir is where the command starts, repository-relative and slash-separated,
	// or empty for the repository root. The Bash tool keeps its directory from
	// one call to the next, so `cd .sdlc/state` in one call and `rm tests.lock`
	// in the next named the freeze in a word that did not say so.
	Dir string

	// Agent is the role running the command, as policy.NormalizeAgent gives
	// it: empty for the main conversation.
	Agent string

	// Resolve, when set, gives the file a word of the command is on disk,
	// repository-relative and slash-separated, or "" when it cannot say. A
	// link to .sdlc, or on Windows a short name such as SDLC~1, names loop
	// state in letters no rule matches, and only the filesystem knows it.
	Resolve func(word string) string

	// Root is the checkout the session is held in, absolute and slash-separated
	// as the session opened it, for the refusals that say what a word was
	// measured against. Empty where the caller does not know it.
	Root string

	// Outside, when set, is the disk's own answer to whether a path Resolve
	// puts outside the repository is outside it: absolute and slash-separated,
	// with no link on the way to it and no other name that could be the
	// repository's, and, with below set, with no checkout of the repository
	// under it. Resolve answers "" for a path it could not look up as much as
	// for one it placed, which is no ground for letting a word go. Nil where
	// the caller has no disk to ask: Resolve's answer then stands for a path,
	// and nothing is known of what is below one.
	Outside func(path string, below bool) bool

	// PowerShell is whether the command is PowerShell's, where a backtick is an
	// escape rather than a command substitution: `Remove-Item CLAUDE`.md`
	// removes CLAUDE.md.
	PowerShell bool

	// Backlog is the backlog file, repository-relative and slash-separated, or
	// empty for none. It is protected as CLAUDE.md is, and is not in that list
	// only because the configuration says where it is.
	Backlog string

	// Shared, when set, says where a word of the command lands among the
	// repository's other checkouts: the main checkout, where the backlog and
	// the story records are kept, and every linked worktree. Nil where the
	// project is its repository's only checkout. A word landing in the project
	// itself lands nowhere here, and Resolve judges it.
	Shared func(word string) []Landing

	// Checkouts, when set, is where the repository's checkouts are on disk, for
	// the rule that refuses taking one away. It is asked once, and only by a
	// command that could take something away.
	Checkouts func() CheckoutSet

	// Carried, when set, is a variable of the environment the session started
	// with, or "" where it is not set. git reads GIT_DIR, GIT_WORK_TREE and
	// its configuration variables from there as well as from the command, and
	// a session that carries GIT_WORK_TREE commits there with a plain `git
	// commit`.
	Carried func(name string) string

	// undecoded is set by Inspect for a reading that another decodes; see
	// pair.
	undecoded bool

	// hold is the branch rule's finding for this call of Inspect, set by it.
	hold *branchHold

	// whole is the command as it was written, as the plain reader read it, or
	// nil; set by Inspect, and see plainCommand.
	whole []plainRun
}

// A Landing is where a word of a command lands in another checkout of the
// project's repository.
type Landing struct {
	// Rel is the path relative to that checkout, slash-separated: "." for
	// the checkout itself, and under ".git" for the git directory it keeps.
	// It is absolute where the repository's checkouts could not be told
	// apart at all, and every word outside the project might be one of them.
	Rel string
	// Own is the project's own git directory, outside it, as a linked
	// worktree's is: this checkout's, but a place no command writes by hand.
	Own bool
}

// Finding is a refusal. An empty Rule means nothing objected.
type Finding struct {
	Rule   string
	Reason string
	Route  string
}

// Message is what the assistant reads.
func (f Finding) Message() string {
	if f.Rule == "" {
		return ""
	}
	return f.Reason + ". Instead: " + f.Route + " [" + f.Rule + "]"
}

// Tools are the Claude Code tools that run a command line, each carrying it in
// tool_input.command. Bash is the obvious one and not the only one: Monitor
// runs a command in the background, and PowerShell is the shell on Windows. A
// rule that looked only at Bash was one tool name away from not applying.
var Tools = map[string]bool{
	"Bash":       true,
	"PowerShell": true,
	"Monitor":    true,
}

// mutating commands: the ones that exist to change a file. A command that only
// reads is nobody's business here, which is why this is a list of verbs rather
// than a list of everything.
var mutating = map[string]bool{
	"rm": true, "mv": true, "cp": true, "tee": true, "dd": true,
	"truncate": true, "install": true, "ln": true, "chmod": true,
	"chown": true, "touch": true, "shred": true, "unlink": true,
	"rmdir": true, "sponge": true,
	// What unpacks, copies or downloads into a path it is given: `tar -xf e.tar
	// -C .sdlc`, `unzip -d .git/hooks`, `curl -o CLAUDE.md`. Counted whatever
	// they are asked, like cp, since what they read is named the same way.
	"tar": true, "rsync": true, "unzip": true, "cpio": true, "scp": true,
	"curl": true, "wget": true,
	// Editors that rewrite a file given on the command line. sed, perl, awk
	// and the interpreters are not here: what they write is decided by their
	// program and not by their arguments, so changesFiles counts them for the
	// loop's record whatever they are asked, and the freeze reads the program
	// instead. Asked of the tool rather than of its program, `-i` was the only
	// write the freeze could see, and `awk 'BEGIN{print "x" > "x_test.go"}'`
	// went through.
	"ed": true, "patch": true,
	// Windows spellings, because Bash on Windows is not always a POSIX shell.
	"del": true, "erase": true, "move": true, "copy": true, "ren": true, "rename": true,
	"rd": true,
	// A link to a protected directory is that directory under a name no rule
	// knows, and a junction needs no privilege to make.
	"mklink": true,
	// PowerShell's cmdlets and their aliases, folded like every other name.
	"remove-item": true, "move-item": true, "copy-item": true, "rename-item": true,
	"new-item": true, "set-content": true, "add-content": true, "clear-content": true,
	"out-file": true, "tee-object": true, "ri": true, "mi": true, "cpi": true, "rni": true,
	"ni": true, "sc": true, "ac": true, "clc": true,
}

// interpreters run a program that can write any file it is given.
var interpreters = map[string]bool{
	"python": true, "python3": true, "py": true, "ruby": true, "node": true, "deno": true, "bun": true, "php": true,
}

// pair is one reading of a command: the text the rules are applied to, and the
// command the loop-state and freeze rules take their words from through m.words.
// The two travel together because they have to agree; see readings.
//
// Called pair and not reading, which is the word the comments and the tests
// use for it, because herestring_test.go declares `type reading int` and a
// frozen file cannot move. A tidy-up that renames this collides with it.
type pair struct {
	text    string
	command string
	// undecoded is a reading that takes `$'status'` for the variable
	// `$status`, where another reading decodes it. That reading has every
	// expansion this one has, so the git rules leave a word the shell has yet
	// to expand to it: read here, `git $'status'` was refused for a `$status`
	// that is not there.
	undecoded bool
}

// readings is a command read as the released tool reads it, as the shell that
// will run it reads it, and, where a POSIX shell would decode `$'...'`, take a
// backslash out or expand a brace list, as it reads once it has. A marker
// nothing closes makes the shell's reading two texts of its own. A rule refuses
// the command when any reading refuses it, so the readings can only add
// refusals to one another. The released reading is not quite the released
// tool's: its lexer reads `\'` and `\"` outside quotes as the shell does, one
// character of the word, where the released lexer opened a string. That drops
// only refusals of text no shell runs, and `echo \'; git commit -m x; echo \'`,
// read as one echo, had hidden a commit.
//
// The released reading is kept because the shell's reading is a reading of
// PowerShell's grammar made without PowerShell. A header the strict test
// declines that PowerShell would accept leaves the raw marker in the text, and
// its quote swallows every command after it: a command refused before this would
// then be allowed. Taking the released reading out is a person's decision, not a
// clean-up, and TestTheShapesOnlyTheReleasedReadingRefusesStayRefused holds four
// shapes it is the only refusal for.
//
// It is asked FIRST. The pair of readings is monotone in whether a command is
// refused but not in which rule refuses it, and a rule name is what a person
// reads and what the hook puts in its JSON, so a command refused before this
// keeps the rule it was refused under.
//
// Each reading's text is made from that reading's own command and never from the
// other's: read the input as it arrived and the lexer meets a live backtick,
// which it reads as a substitution (commands.go:170-173), which is not what the
// released tool answers.
func readings(command string, powerShell bool) []pair {
	released, shell := command, command
	if powerShell {
		released = strings.ReplaceAll(command, "`", "")
		shell = withoutEscapes(command)
	}
	out := make([]pair, 0, 3)
	out = addReadings(out, withoutDocuments(released, powerShell, true), released, false)
	out = addReadings(out, withoutDocuments(shell, powerShell, false), shell, false)
	if !powerShell {
		out = addDecodedReadings(out, shell)
		// And each once more with its brace lists expanded, where it has one.
		for _, p := range slices.Clone(out) {
			if text := bracesExpanded(p.text); text != p.text {
				out = addReadings(out, []string{text}, bracesExpanded(p.command), p.undecoded)
			}
		}
	}
	return out
}

// addReadings adds a reading of the command spelled spelling for each of
// texts.
//
// A reading another has already made is not made again: the two differ only
// where the command holds a here-string marker or a backtick, and a document
// handed over as a bash heredoc is one reading for both.
func addReadings(out []pair, texts []string, spelling string, undecoded bool) []pair {
	for _, text := range texts {
		if !slices.ContainsFunc(out, func(p pair) bool { return p.text == text && p.command == spelling }) {
			out = append(out, pair{text: text, command: spelling, undecoded: undecoded})
		}
	}
	return out
}

// addDecodedReadings adds a third reading, where a POSIX shell decodes
// `$'...'` or takes a backslash out, and only then: a command with neither
// pays nothing for it. Decoded after the documents are taken out, because a
// document is text and its quote -- the apostrophe of "don't" -- would
// otherwise open a string that hid every line after it.
func addDecodedReadings(out []pair, shell string) []pair {
	d := decoded(shell)
	if d == shell {
		return out
	}
	for i := range out {
		out[i].undecoded = true
	}
	// Each undecoded text is decoded, the released reading's too, so
	// that every reading that leaves a word to a decoded one has one.
	for _, released := range []bool{true, false} {
		texts := withoutDocuments(shell, false, released)
		for i, text := range texts {
			texts[i] = decoded(text)
		}
		out = addReadings(out, texts, d, false)
	}
	return out
}

// Inspect reports the first rule that refuses this command.
//
// The rules do not depend on who is asking. A shell command that rewrites the
// loop's record is wrong from every role, including the one whose record it is,
// for the same reason the file-writing rules refuse it from everyone. Two
// exceptions are only wrong because of who is asking: the implementer creating
// a test file, as the file-writing rules have it, and a review recorded by
// anyone but the reviewer it names.
//
// A path that is only named is not refused: the quoted text of an sdlc flag
// that takes text, the path a git pathspec leaves out, and a command that only
// asks sdlc for help. nameOnly says where it is sure of one, and the command is
// let through when the rules allow it read with those taken out. Every refusal
// is the one the rules give the command as it was written, so a command that
// stays refused is refused under the rule that always named it, and a command
// the rules allow as written is never refused here: the reading with the
// mentions taken out can only let a command through.
//
// A word is placed outside the repository only in the command as it was
// written. The plain reader reads that once, here, and every reading the rules
// are asked about is compared with it: one with a mention taken out is not the
// command the shell runs, and with the plain reader given that line to read,
// taking out a quoted text with a space in it made a command plain that was
// not, and the refusal of a path it writes said the path might only be named.
func Inspect(command string, s State) (Finding, bool) {
	s.whole = plainCommand(command, s)
	n := nameOnly(command, s)
	if n.rewritten(command) {
		if _, refused := inspectAs(n.asked, n.named, s); !refused {
			return Finding{}, false
		}
	}
	f, refused := inspectAs(command, command, s)
	if !refused {
		return Finding{}, false
	}
	return n.told(f, command, s), true
}

// inspectAs applies the rules to a command. The rules for what a command runs
// read asked and the rules for the paths it names read named: they are the
// same line except where nameOnly took a mention out, and the text of an sdlc
// flag is taken out of named alone, so a decision written inside a note is
// still read as it always was.
func inspectAs(asked, named string, s State) (Finding, bool) {
	m := &memo{checked: map[string]bool{}, resolved: map[string]string{}, landed: map[string][]Landing{},
		disk: &diskAnswers{}, whole: s.whole}
	s.hold = &branchHold{}
	var f Finding
	var ok bool
	if asked == named {
		f, ok = inspectEach(asked, s, m, inspectReading)
	} else if f, ok = inspectEach(asked, s, m, inspectPrograms); !ok {
		f, ok = inspectEach(named, s, m, inspectPaths)
	}
	if ok {
		return f, true
	}
	// The branch rule answers last, where nothing else refused the command.
	if s.hold.found {
		return s.hold.finding, true
	}
	return Finding{}, false
}

// inspectEach puts every reading of a line to one set of rules, in the order
// readings gives them.
func inspectEach(line string, s State, m *memo, rules func(pair, State, *memo) (Finding, bool)) (Finding, bool) {
	for _, r := range readings(line, s.PowerShell) {
		// With a branch move found, a reading is started only within the
		// bound, and its text is charged to it before it is parsed.
		if s.hold.charge(len(r.text)) {
			return s.hold.finding, true
		}
		s.undecoded = r.undecoded
		if f, ok := rules(r, s, m); ok {
			return f, true
		}
		// What Resolve said of a word, and the folded freeze, are functions of
		// their input, so what one reading worked out the next may have. What a
		// rule has already looked at is not: m.first answers "this rule found
		// nothing in this spelling", which is true of one reading and not of
		// both, and m.line holds the words of a command the next reading spells
		// differently. wordsIn over a 50 KB command is 262 us, so a later reader
		// will see duplicated work here; sharing it is what would break the
		// guarantee above.
		m = &memo{checked: map[string]bool{}, resolved: m.resolved, landed: m.landed, folded: m.folded, disk: m.disk,
			whole: m.whole}
	}
	return Finding{}, false
}

func inspectReading(r pair, s State, m *memo) (Finding, bool) {
	if f, ok := inspectPrograms(r, s, m); ok {
		return f, true
	}
	return inspectPaths(r, s, m)
}

// inspectPrograms applies the rules for what a reading runs.
func inspectPrograms(r pair, s State, _ *memo) (Finding, bool) {
	if f, ok := checkAssignedEnforcement(r.text); ok {
		return f, true
	}
	return checkPrograms(r.text, s)
}

// inspectPaths applies the rules for the paths a reading names.
func inspectPaths(r pair, s State, m *memo) (Finding, bool) {
	text, command := r.text, r.command
	w := &pathWalk{command: command, text: text, s: s, m: m, dir: s.Dir, lost: false}
	w.unsure = movesInAScript(text, s.PowerShell)
	w.old = unknownDir
	// The find expression reader needs the command as it is spelled, with the
	// separators the shell hid from find left as markers. It is made once, the
	// first time a segment runs find; a command with no find pays nothing for it.
	w.plain = markedSegments(pathText(text, s.PowerShell))
	w.alike = m.readAlike(w.plain)
	for idx, marked := range w.plain {
		if s.hold.charge(1) {
			return s.hold.finding, true
		}
		if f, ok := w.segment(idx, marked); ok {
			return f, true
		}
	}
	return Finding{}, false
}

// checkAssignedEnforcement refuses a command that assigns one of the
// variables the enforcement goes by, in any of its segments.
func checkAssignedEnforcement(text string) (Finding, bool) {
	for _, segment := range segments(text) {
		if _, _, assigns := parse(segment); len(assigns) > 0 {
			if f, ok := checkEnforcement(assigns); ok {
				return f, true
			}
		}
	}
	// PowerShell sets a variable as a statement of its own, which parse does not
	// read as an assignment: `$env:CLAUDE_CODE_SESSION_ID='x'; sdlc start`.
	if mayName(text, "CLAUDE_CODE_SESSION_ID") && setsSession.MatchString(text) {
		if f, ok := checkEnforcement([]string{"CLAUDE_CODE_SESSION_ID"}); ok {
			return f, true
		}
	}
	return Finding{}, false
}

// walkPlace is where the command was when a subshell opened: what a `cd`
// inside the subshell leaves as it was.
type walkPlace struct {
	dir  string
	lost bool
	old  string
}

// pathWalk is the path rules part way through one reading of a command: the
// reading, and where the command is as each of its segments is read.
type pathWalk struct {
	command, text string
	s             State
	m             *memo
	// dir is where the command is, and lost whether that is somewhere the
	// words do not place.
	dir  string
	lost bool
	// unsure is whether a script handed to a shell moves and then writes.
	unsure bool
	// Where the command is, for each subshell open around the segment being
	// read. A `cd` in a subshell moves only that subshell: followed as a move
	// of the shell around it, `cd internal && (cd /tmp && ls) && cp e x_test.go`
	// wrote /tmp's x_test.go, and taken as leaving where the command is
	// unknown, `(cd web && npm run build) && cp config.example.json config.json`
	// wrote a frozen fixture's config.json. PowerShell's parentheses and $( )
	// run in the scope around them, so there a cd moves the command.
	//
	// old is where the command was before its last `cd`, which $OLDPWD and ~-
	// name: before the first, somewhere this command does not show.
	old    string
	outer  []walkPlace
	ticked bool
	// plain is the command's segments as spelled, and scan the reader of
	// the finds among them, made the first time a segment runs find.
	plain []string
	scan  *findScan
	// alike is whether the plain reader read every one of those segments as
	// they are read here: see pathQuestion.certain.
	alike bool
}

// segment applies the path rules to one segment of the command, and then
// follows a `cd` it makes.
func (w *pathWalk) segment(idx int, marked string) (Finding, bool) {
	s := w.s
	segment, opens, closes := subshellMarks(marked, &w.ticked)
	if !s.PowerShell {
		w.subshells(opens, closes)
	}
	words, redirects, _ := parse(segment)
	if len(words) == 0 {
		return Finding{}, false
	}
	run, evals := underEval(program(words))
	// A find's expression is read once, here, for every rule that asks.
	found := w.find(idx, segment, run, evals)
	// env -C runs the command in the directory it names, as a cd in front
	// of it would, and leaves the shell where it was. Read from where the
	// shell was, `env -C .. rm -rf B-2` took another story's worktree away,
	// and `env -C .sdlc rm -rf state` the loop's own record.
	at, atLost := w.dir, w.lost
	if run.chdir != "" {
		at, _ = changedDir(w.dir, []string{"cd", run.chdir})
		atLost = w.movedTo(at)
	}
	j := segmentRules{
		command: w.command, exp: &found.exp, redirects: redirects, dir: at, lost: atLost, unsure: w.unsure,
		s: s, m: w.m,
		certain: w.alike && evals == 0 && !atLost && len(run.words) == len(words) && w.m.readFrom(idx, at),
		away:    takeAway{s: s, m: w.m, dir: at, lost: atLost, pwd: w.dir, pwdLost: w.lost, old: w.old, exp: &found.exp},
	}
	if f, ok := j.judge(segment, run); ok {
		return f, true
	}
	if found.spent {
		// More finds the reader gave up on than it reads to the end. A command
		// can be written to come here, so nothing it says is asked: not where
		// the find starts, and not what it goes on to do.
		return spentFinding(found.exp), true
	}
	if found.more {
		// The find's own words run on past a separator inside one of its
		// quoted words, where the segment above ends; or the reader gave up
		// on the find, and its words are taken to run to the end of the
		// command. The rules are asked once more with all of them, from the
		// same directory; where the rest of the command is read from is left
		// as it is.
		whole := run
		whole.words = found.words
		// These are not the words the plain reader was compared with.
		j.certain = false
		if f, ok := j.judge(found.segment, whole); ok {
			if found.toEnd && !changesFiles(run.words) {
				f = laterAction(f)
			}
			return f, true
		}
	}
	// A script handed to a shell runs in a process of its own, and its cd is
	// movesInAScript's. With the eval builtin taken off, a string that opened
	// a subshell around a move was read as moving the shell, and a command
	// that is refused where it stands was read from a directory the shell was
	// never in, and allowed.
	w.follow(run, evals)
	return Finding{}, false
}

// follow moves the walk to where a `cd` among the segment's words leaves the
// shell.
func (w *pathWalk) follow(run invocation, evals int) {
	if next, ok := changedDir(w.dir, run.words); ok && !run.shell && evals == 0 {
		w.old = w.dir
		if w.lost {
			w.old = unknownDir
		}
		w.dir, w.lost = next, w.movedTo(next)
	}
}

// subshells takes the walk out of the subshells a segment closes, back to
// where the command was when each opened, and into those it opens.
func (w *pathWalk) subshells(opens, closes int) {
	for ; closes > 0 && len(w.outer) > 0; closes-- {
		top := w.outer[len(w.outer)-1]
		w.dir, w.lost, w.old = top.dir, top.lost, top.old
		w.outer = w.outer[:len(w.outer)-1]
	}
	for ; opens > 0; opens-- {
		w.outer = append(w.outer, walkPlace{w.dir, w.lost, w.old})
	}
}

// find reads the expression of a segment that runs find, and is the zero
// answer for one that does not.
func (w *pathWalk) find(idx int, segment string, run invocation, evals int) foundFind {
	if len(run.words) == 0 || base(run.words[0]) != "find" {
		return foundFind{}
	}
	if w.scan == nil {
		w.scan = newFindScan(findSpelling(w.text, w.s.PowerShell), w.plain)
	}
	return w.scan.read(idx, segment, run.words, evals)
}

// movedTo reports whether a move to next leaves the command somewhere the
// words do not place.
//
// A relative `cd` from somewhere unknown leads somewhere unknown. So
// does a glob, which names whichever directory it matches; it is kept
// as written, so that the paths under it are matched as globs too:
// dropped, `cd .sdl? && rm state/active` was a file at the root.
func (w *pathWalk) movedTo(next string) bool {
	return next == "" || strings.ContainsAny(next, "*?[") || w.lost && !isAbsolute(next)
}

// segmentRules is the path rules as one segment asks them: the command it
// is part of, what was read of it, and where it runs.
type segmentRules struct {
	command   string
	exp       *findExpression
	redirects []string
	dir       string
	// lost is whether dir is somewhere the words do not place, and unsure
	// whether a script moves and then writes, which the freeze counts as
	// lost.
	lost, unsure bool
	// certain is whether the plain reader read the command as it is read
	// here, and this segment from this directory: see pathQuestion.
	certain bool
	s       State
	m       *memo
	away    takeAway
}

// judge asks the path rules about a command of the segment, spelled segment.
func (j segmentRules) judge(segment string, run invocation) (Finding, bool) {
	// A glob that can come back through `..` is refused for that first,
	// under the same rule: refused for the `.sdlc` it could match as well,
	// `rm -rf build/.*` was sent to `sdlc artifact write` when it had asked
	// to clear a directory.
	if backThroughDotDot(run.words) {
		if f, ok := checkTakesACheckout(j.command, segment, run, j.away); ok {
			return f, true
		}
	}
	q := pathQuestion{
		line: j.command, segment: segment, exp: j.exp, run: run, redirects: j.redirects,
		dir: j.dir, lost: j.lost, certain: j.certain, s: j.s, m: j.m,
	}
	if f, ok := checkLoopState(q); ok {
		return f, true
	}
	// The freeze counts a script that moves and then writes as lost too.
	frozen := q
	frozen.lost = j.lost || j.unsure
	if f, ok := checkFrozenTests(frozen); ok {
		return f, true
	}
	// After the rules a command was refused under before there was one, so
	// that it keeps the rule it was refused under.
	return checkTakesACheckout(j.command, segment, run, j.away)
}

// movesInAScript reports whether a script handed to a shell changes directory
// and then writes a file. The path rules read a quoted script's commands as
// segments of the command around it, from the directory outside it, so where
// that write lands is not known to them: `sh -c 'cd internal && cp e
// x_test.go'`. A script that moves and writes nothing says nothing about the
// rest of the command.
func movesInAScript(text string, powerShell bool) bool {
	runs, subshells := programsAt(text, powerShell)
	moved := map[int]bool{}
	for _, r := range runs {
		if len(r.words) == 0 || r.extra {
			continue
		}
		script := subshells.scriptOf(r.group)
		if script == 0 {
			continue
		}
		if moved[script] && (r.redirects || changesAFile(r.words)) {
			return true
		}
		if _, ok := changedDir("", r.words); ok {
			moved[script] = true
		}
	}
	return false
}

// checkPrograms applies the rules that turn on which program a command runs,
// read with its quoting: the sdlc subcommands a person keeps, a review recorded
// by its reviewer, and the commit gate.
func checkPrograms(text string, s State) (Finding, bool) {
	// Where each subshell is. A `cd` moves only the subshell it runs in, and a
	// subshell starts where the one that opened it was. Followed as a move of
	// the shell around it, `(cd .. && ls) && git commit` was a commit in another
	// repository; not followed at all, `(cd /tmp/fixture && git commit)` was a
	// commit in this one.
	runs, subshells := programsAt(text, s.PowerShell)
	if subshells.tooDeep {
		return tooDeep, true
	}
	w := &programWalk{s: s, subshells: subshells, dirs: map[int]string{0: s.Dir}}
	// GIT_DIR names the repository whatever the directory, and an assignment to
	// it can be anywhere in the command, or a statement of its own in
	// PowerShell: `$env:GIT_DIR='C:\project\.git'; cd \; git commit`. Asked once:
	// asked for every command in it, a long command took seconds. printf -v and
	// read set it too, in words no pattern tells from a mention.
	w.gitDirSet = mayName(text, "GIT_DIR") && setsGitDir.MatchString(text) ||
		slices.ContainsFunc(runs, func(r ran) bool {
			_, ok := assigned(r.sets, "GIT_DIR")
			return ok
		})
	// Where each subshell is, as far as the words place it. The refusal reads
	// a directory it cannot place as the project, which errs toward holding a
	// commit back; the recorder cannot err that way, because a commit made in
	// another repository then wrote down this one's HEAD: `cd "$HOME/other" &&
	// git commit` planted an observation a person's next commit here was
	// recorded on. So a directory reached by a guess is lost, and a commit
	// made from it is allowed as before and written down nowhere.
	w.lost = map[int]bool{0: w.gitDirSet}
	// CDPATH sends a relative `cd` through the directories it lists. Any
	// mention will do, since the cost of reading one wrongly is an observation.
	w.cdPath = strings.Contains(strings.ToLower(text), "cdpath")
	// What git will run, read with the variables each command runs with.
	w.env = newEnvironment(text, s, subshells)
	w.git = &gitRules{s: s, m: &memo{landed: map[string][]Landing{}}}
	for _, r := range runs {
		if f, ok := w.run(r); ok {
			return f, true
		}
	}
	return Finding{}, false
}

// programWalk is checkPrograms part way through a command: where each
// subshell is, in dirs, and whether that is somewhere the words do not
// place, in lost; and what the command as a whole says of GIT_DIR and CDPATH.
type programWalk struct {
	s                 State
	subshells         *subshells
	dirs              map[int]string
	lost              map[int]bool
	gitDirSet, cdPath bool
	env               *environment
	git               *gitRules
}

// run applies the program rules to one command, and then follows a move it
// makes.
func (w *programWalk) run(r ran) (Finding, bool) {
	words := r.words
	dir, unplaced := w.place(r.group)
	w.env.record(r)
	w.git.lost = unplaced
	if r.extra {
		if _, f, ok := w.git.check(r, dir, w.env, 0, false); ok {
			return f, true
		}
		return Finding{}, false
	}
	if len(words) == 0 {
		return Finding{}, false
	}
	if f, ok := checkSdlcCommands(words, r.piped, r.substituted, w.s); ok {
		return f, true
	}
	// Asked before the commit gate and answered after it, so that a commit
	// the gate refused before git was read this way keeps the rule it was
	// refused under.
	commit, gitFinding, refused := w.git.check(r, dir, w.env, 0, false)
	// env -C runs the command where it says, as a cd in front of it
	// would. Read from where the shell was, `env -C <main checkout> git
	// commit` was a commit in this one.
	at, placed := ranIn(r, dir)
	// A directory of "" is the project.
	if w.gitDirSet {
		dir, at = "", ""
	}
	if f, ok := w.git.commit(r, at, commit, placed && !unplaced, false); ok {
		return f, true
	}
	if refused {
		return gitFinding, true
	}
	w.follow(r.group, dir, unplaced, words)
	return Finding{}, false
}

// place is where the subshell a command runs in is, and whether that is
// lost: its own place where it has moved, and otherwise that of the nearest
// subshell around it that has one, which it then has too.
func (w *programWalk) place(group int) (dir string, unplaced bool) {
	dir, ok := w.dirs[group]
	unplaced = w.lost[group]
	for g := group; !ok; {
		g = w.subshells.parent[g]
		dir, ok = w.dirs[g]
		unplaced = w.lost[g]
	}
	w.dirs[group], w.lost[group] = dir, unplaced
	return dir, unplaced
}

// checkSdlcCommands applies the rules for what a command hands sdlc, each
// under its own name and in this order. cut is set for a command a word of
// which held a substitution, where a help flag is not believed.
func checkSdlcCommands(words []string, piped, cut bool, s State) (Finding, bool) {
	if f, ok := checkAssign(words, piped, cut); ok {
		return f, true
	}
	if f, ok := checkRecorder(words, s); ok {
		return f, true
	}
	if f, ok := checkUnfreeze(words); ok {
		return f, true
	}
	if f, ok := checkApprove(words); ok {
		return f, true
	}
	if f, ok := checkAck(words); ok {
		return f, true
	}
	if f, ok := checkRelease(words); ok {
		return f, true
	}
	if f, ok := checkParallel(words); ok {
		return f, true
	}
	if f, ok := checkStop(words, s); ok {
		return f, true
	}
	if f, ok := checkReviewer(words, s); ok {
		return f, true
	}
	// After the rules above, so that what a person keeps and a review that is
	// not the project manager's are refused under their own names; and the
	// subcommand that cannot be read last, so that one from the project
	// manager is refused as everything else it may not run is.
	return checkManagerAndLast(words, piped, cut, s)
}

// checkManagerAndLast applies the project manager's rule and the two after
// it.
func checkManagerAndLast(words []string, piped, cut bool, s State) (Finding, bool) {
	f, ok := checkManager(words, s, func(args []string) bool { return !cut && asksForHelp(args) })
	if !ok {
		return checkLast(words, piped)
	}
	// A command with a help flag that sdlc would not read as help alone was
	// let past this rule before, and some were then refused by one after it.
	// Those keep the name they were refused under.
	if _, before := checkManager(words, s, namesHelp); !before {
		if later, ok := checkLast(words, piped); ok {
			return later, true
		}
	}
	return f, true
}

// checkLast applies the two rules that come after the project manager's.
func checkLast(words []string, piped bool) (Finding, bool) {
	if f, ok := checkUnread(words, piped); ok {
		return f, true
	}
	return checkNestedSession(words)
}

// follow moves the subshell a command ran in to where the command leaves
// it. dir is where it was, "" where GIT_DIR names the repository, and
// unplaced whether that was lost.
func (w *programWalk) follow(group int, dir string, unplaced bool, words []string) {
	if next, ok := changedDir(dir, words); ok {
		// A glob can match this repository as well as any other:
		// `cd /work/proj* && git commit`.
		glob := strings.ContainsAny(next, "*?[")
		if glob {
			next = ""
		}
		// Lost as the other loop is lost, and further: a word the shell
		// expands can be anywhere even when what is left of it looks
		// placed (`cd a/$X/../..` cleans to the root), a relative `cd` is
		// where CDPATH says, and `pushd +1` turns a stack the words do not
		// show.
		w.dirs[group] = next
		w.lost[group] = next == "" || glob || !placedWords(words[1:]) ||
			(unplaced || w.cdPath) && !isAbsolute(next)
	} else if len(words) > 0 && (runsHere[base(words[0])] || words[0] == "" || !placedWords(words[:1])) {
		// A script run by this shell can move it anywhere, and so can a
		// command the shell has yet to expand: `source ./env.sh && git
		// commit`, and `eval "$CD"`, which is read as the command `$CD`.
		w.lost[group] = true
	}
}

// tooDeep refuses a command that nests substitutions further than they are
// read, because what runs past that point was not checked.
var tooDeep = Finding{
	Rule: "command-too-deep-to-read",
	Reason: "the command nests $( ) or backticks deeper than the rules read, so what runs " +
		"inside was not checked",
	Route: "write it as separate commands; nothing a person writes nests this deep",
}

// setsGitDir matches an assignment to GIT_DIR: `GIT_DIR=`, `export GIT_DIR=`,
// or PowerShell's environment drive, as in `$env:GIT_DIR = ` and `Set-Item
// env:GIT_DIR`, `Env:\GIT_DIR`, and .NET's
// `[Environment]::SetEnvironmentVariable('GIT_DIR', ...)`. A mention is not
// one: a commit message that said "unset GIT_DIR", or `grep 'GIT_DIR'`, was a
// commit in the project wherever it was made.
var setsGitDir = regexp.MustCompile(`(?i)\bgit_dir=|env:[\\/]?git_dir\b|setenvironmentvariable\(\s*['"]git_dir['"]`)

// runsHere are the commands that run a script in the shell that calls them,
// where a `cd` inside it moves the command line around it.
var runsHere = map[string]bool{
	"source": true, ".": true, "eval": true, "iex": true, "invoke-expression": true,
}

// placedWords reports whether none of words is one the shell expands into a
// directory the words do not show: a variable, a substitution, a glob, a
// brace list, the directory stack, or an option of pushd and popd that turns
// it.
func placedWords(words []string) bool {
	for _, w := range words {
		if strings.ContainsAny(w, "$%`*?[{") || shellDirectory(clean(w)) ||
			strings.HasPrefix(w, "+") || len(w) > 1 && w[0] == '-' && w[1] >= '0' && w[1] <= '9' {
			return false
		}
	}
	return true
}

// setsSession matches an assignment to CLAUDE_CODE_SESSION_ID: `NAME=`, and
// PowerShell's `$env:NAME = `, `${env:NAME} = `, Set-Item, New-Item,
// Set-Content and Add-Content on `env:NAME`, and
// `[Environment]::SetEnvironmentVariable('NAME', ...)`, the spellings
// powerShellSets reads. `sdlc start` records the
// session it finds there as the one the story holds, so setting it hands the
// story to a session that does not exist. Unlike setsGitDir it is a refusal, so
// a read is not one: `echo $env:CLAUDE_CODE_SESSION_ID` is allowed.
var setsSession = regexp.MustCompile(
	`(?i)\bclaude_code_session_id=|\$env:claude_code_session_id\s*=|\$\{env:claude_code_session_id\}\s*=|` +
		`\b(set-item|new-item|set-content|add-content|si|ni|sc|ac)\s+(-path\s+)?env:[\\/]?claude_code_session_id\b|` +
		`setenvironmentvariable\(\s*['"]claude_code_session_id['"]`)

// mayName reports whether text could name the variable name, an upper-case
// one, where setsSession or setsGitDir could match: upper-casing folds each of
// the letters of these names as their (?i) does, `ſ` to S included. Asked
// first, because either pattern takes milliseconds over a long command, and
// almost none names either variable: over 60 KB of rm, setsSession took 8 ms
// of every call once it read Set-Content as well.
func mayName(text, name string) bool {
	return strings.Contains(strings.ToUpper(text), name)
}

// HumanDecisions reports a command that makes one of the decisions the loop
// keeps for a person: approving work handed over, lifting the freeze, or
// acknowledging the log. The caller asks it where no story is held, because
// `sdlc escalate` ends the iteration and the approval always comes after it --
// which is where nothing else was being enforced, so no approval was ever
// refused -- and reading the log is not part of any story. A command nested too
// deep to read is reported with them, since any of them could be inside it.
//
// Acknowledging the log is always reported. Approving and lifting the freeze
// are decisions on a story, and are reported when storyWaits says a story waits
// for one where the command runs: dir, the directory it has moved to from where
// it started, "." when it has not moved. One that runs after a move this cannot
// follow -- `cd "$OLDPWD"`, `cd ../$name`, `popd` -- is reported without asking,
// because it could be anywhere. A decision on a story that is let through does
// not end the reading: `sdlc approve A-1; sdlc ack --through HEAD` is still an
// acknowledgement.
//
// A command that only asks sdlc for help decides nothing, and is not reported:
// the command is read with each of those taken out, as Inspect reads it, and
// what is reported is what the command as written is reported for.
func HumanDecisions(command string, powerShell bool, storyWaits func(dir string) bool) (Finding, bool) {
	n := nameOnly(command, State{PowerShell: powerShell})
	if n.asked != command {
		if _, reported := humanDecisionsAs(n.asked, powerShell, storyWaits); !reported {
			return Finding{}, false
		}
	}
	return humanDecisionsAs(command, powerShell, storyWaits)
}

func humanDecisionsAs(command string, powerShell bool, storyWaits func(dir string) bool) (Finding, bool) {
	// Both readings, as Inspect asks them and in the same order. This is the
	// entry point the hook uses when no story is being worked on (hook.go:229),
	// so it is where an approval, an unfreeze or an ack is reported at all: a
	// decision one reading throws away as a document has to meet the other, or
	// the shape the four rows of
	// TestTheShapesOnlyTheReleasedReadingRefusesStayRefused hold for Inspect
	// stops being reported here.
	for _, r := range readings(command, powerShell) {
		if f, ok := humanDecisionsOn(r.text, powerShell, storyWaits); ok {
			return f, true
		}
	}
	return Finding{}, false
}

func humanDecisionsOn(text string, powerShell bool, storyWaits func(dir string) bool) (Finding, bool) {
	runs, subshells := programsAt(text, powerShell)
	if subshells.tooDeep {
		return tooDeep, true
	}
	// Where each subshell is, followed as checkPrograms follows it, and whether
	// a move there was one that cannot be read.
	places := map[int]decisionPlace{0: {dir: "."}}
	for _, r := range runs {
		if len(r.words) == 0 || r.extra {
			continue
		}
		here, ok := places[r.group]
		for g := r.group; !ok; {
			g = subshells.parent[g]
			here, ok = places[g]
		}
		places[r.group] = here
		if f, ok := humanDecisionBy(r, here, storyWaits); ok {
			return f, true
		}
		if next, ok := changedDir(here.dir, r.words); ok {
			places[r.group] = here.moved(r, next)
		}
	}
	return Finding{}, false
}

// decisionPlace is where a subshell is as humanDecisionsOn follows it, and
// whether a move there was one that cannot be read.
type decisionPlace struct {
	dir  string
	lost bool
}

// moved is where the command r, run from here, leaves a shell it moves to
// next.
func (here decisionPlace) moved(r ran, next string) decisionPlace {
	unread := r.substituted || next == "" || strings.ContainsAny(next, "$`%(){}*?[")
	return decisionPlace{dir: next, lost: unread || here.lost && !isAbsolute(next)}
}

// humanDecisionBy refuses one command, run from here, that makes a decision
// a person keeps.
func humanDecisionBy(r ran, here decisionPlace, storyWaits func(dir string) bool) (Finding, bool) {
	// An assignment is refused whatever the project holds, as an
	// acknowledgement is: a person can assign to a story at any point after
	// it is started, so whether one waits is never asked.
	if f, ok := checkAssign(r.words, r.piped, r.substituted); ok {
		return f, true
	}
	if f, ok := checkAck(r.words); ok {
		return f, true
	}
	if f, ok := checkRelease(r.words); ok {
		return f, true
	}
	if f, ok := checkParallel(r.words); ok {
		return f, true
	}
	f, onAStory := checkUnfreeze(r.words)
	if !onAStory {
		f, onAStory = checkApprove(r.words)
	}
	// Where env -C sends it, as a cd in front of it would.
	at := here
	if r.chdir != "" {
		next, _ := changedDir(here.dir, []string{"cd", r.chdir})
		at = here.moved(r, next)
	}
	if onAStory && (at.lost || storyWaits(at.dir)) {
		return f, true
	}
	// After the decision above, so that an approval or an unfreeze handed
	// to sdlc on standard input keeps the name of its own rule.
	return checkUnread(r.words, r.piped)
}

// Places is every path-shaped word a command names, each once, in the order
// they first appear: where the command may act, for a caller that has yet to
// find which project that is in.
func Places(command string, powerShell bool) []string {
	if powerShell {
		// Not withoutEscapes, on purpose: here a place a shell would not read is
		// harmless and a missed one is not, and taking an escaped quote off would
		// glue two words into a path that is neither.
		command = strings.ReplaceAll(command, "`", "")
	}
	seen := map[string]bool{}
	var out []string
	for _, word := range wordsIn(command) {
		if !seen[word] {
			seen[word] = true
			out = append(out, word)
		}
	}
	return out
}

// checkEnforcement stops a session turning the loop off from the inside. The
// switch exists for the person who started the session, and a switch an
// assistant can reach is not a control.
func checkEnforcement(assigns []string) (Finding, bool) {
	for _, name := range assigns {
		switch name {
		case "SDLC_ENFORCE", "SDLC_BIN", "CLAUDE_PROJECT_DIR", "CLAUDE_CODE_SESSION_ID":
			return Finding{
				Rule: "enforcement-stays-on",
				Reason: name + " decides whether the loop is enforced at all, and it is " +
					"set by the person who started the session, not from inside it",
				Route: "if a rule is wrong, say which one and why, and stop",
			}, true
		}
	}
	return Finding{}, false
}

// checkNestedSession keeps the story in the session working it.
//
// A story holds only the Claude Code session that took it, and a session
// started from its shell has an id of its own, so `claude -p "..."` run from
// inside was a session no rule here held: it could lift the freeze, commit, or
// end the iteration.
func checkNestedSession(words []string) (Finding, bool) {
	if len(words) == 0 {
		return Finding{}, false
	}
	nested := false
	switch base(words[0]) {
	case "claude", "claude.cmd":
		nested = true
	case "npx", "bunx", "pnpx":
		nested = runsClaude(words[1:])
	case "npm", "bun", "pnpm", "yarn":
		// A package runner's own spelling: `npm exec`, `bun x`, `pnpm dlx`.
		// Installing the package starts nothing.
		if len(words) > 1 {
			switch base(words[1]) {
			case "exec", "x", "dlx":
				nested = runsClaude(words[2:])
			}
		}
	}
	if !nested {
		return Finding{}, false
	}
	return Finding{
		Rule: "story-stays-in-its-session",
		Reason: "a Claude Code session started from here is a session of its own, and the story " +
			"holds only the session working it, so nothing here would hold what that one did",
		Route: "hand the work to this session's agents; a person who wants another session " +
			"opens one in their own terminal",
	}, true
}

// runsClaude reports whether a package runner's arguments run Claude Code: the
// first that is not a flag names its package, or its `claude` program.
func runsClaude(args []string) bool {
	for _, w := range args {
		if strings.HasPrefix(w, "-") {
			continue
		}
		return base(w) == "claude" || strings.Contains(pathrules.Fold(w), "@anthropic-ai/claude-code")
	}
	return false
}

// checkUnfreeze keeps lifting the freeze a person's decision.
//
// The runbook asked agents not to, and nothing else stood in the way: an agent
// with a shell could lift the freeze on the tests it was failing, which is the
// shortcut the freeze exists to take away. A person runs it in their own
// terminal, where this hook is not asked.
func checkUnfreeze(words []string) (Finding, bool) {
	if !runsSubcommand(words, "unfreeze") {
		return Finding{}, false
	}
	return Finding{
		Rule: "unfreeze-is-a-human-decision",
		Reason: "lifting the test freeze is the move an agent would make to reach green, " +
			"so it is not one an agent makes",
		Route: "say which frozen test is wrong and which acceptance criterion it gets " +
			"wrong, and stop there; the person running the session lifts the freeze with " +
			"`sdlc unfreeze --reason \"...\"` in their own terminal",
	}, true
}

// checkStop keeps ending a story part-way a person's decision.
//
// Every rule here holds only while a story is being worked on, so ending the
// iteration was the way round all of them at once. Refusals and the Stop hook
// kept naming it as the way out, and an agent that took it could commit work no
// gate had passed. Ending a story whose gates have all passed is the runbook's
// last step, and is not refused.
func checkStop(words []string, s State) (Finding, bool) {
	if s.StoryFinished || !runsSubcommand(words, "stop") {
		return Finding{}, false
	}
	return Finding{
		Rule: "stop-is-a-human-decision",
		Reason: "ending the iteration before the story's gates have passed turns every rule " +
			"here off, so it is not a step an agent takes",
		Route: "work the next gate -- `sdlc status` shows which; if the story cannot go on, " +
			"hand it to a person with `sdlc escalate <type> --message \"...\"` and stop; the " +
			"person running the session ends the iteration with `sdlc stop` in their own terminal",
	}, true
}

// checkApprove keeps a decision that was handed to a person with that person.
//
// `sdlc escalate` stops the loop to ask somebody a question, and an approval is
// their answer. An agent that could run it would be answering its own question
// -- approving its own work -- which is the one thing the escalation was for.
func checkApprove(words []string) (Finding, bool) {
	if !runsSubcommand(words, "approve") {
		return Finding{}, false
	}
	return Finding{
		Rule: "approval-is-a-human-decision",
		Reason: "an approval is a person's answer to a question the loop stopped to ask them, " +
			"and an agent that gave it would be approving its own work",
		Route: "hand the question over with `sdlc escalate <type> --message \"...\"` and stop; " +
			"the person reads the work and runs `sdlc approve` in their own terminal",
	}, true
}

// checkRelease keeps moving a story between checkouts with a person. An agent
// that could release a story could take another checkout's story from under
// it, or let go of the one it is held to and so of every rule holding it.
func checkRelease(words []string) (Finding, bool) {
	if !runsSubcommand(words, "release") {
		return Finding{}, false
	}
	return Finding{
		Rule: "release-is-a-human-decision",
		Reason: "releasing a story takes it from the checkout holding it, and an agent that did it " +
			"could take a story another checkout is working, or let go of the one it is held to",
		Route: "say which checkout holds the story and stop; a person runs " +
			"`sdlc release <ID> --reason \"...\"` in their own terminal if it is to move",
	}, true
}

// checkParallel keeps starting model sessions with a person. `sdlc parallel`
// runs `claude` once per story, and with --unattended it hands each session a
// list of pre-approved tools: an agent that ran it would be starting work, and
// choosing what that work may do, that nobody asked for.
func checkParallel(words []string) (Finding, bool) {
	if !runsSubcommand(words, "parallel") {
		return Finding{}, false
	}
	return Finding{
		Rule: "parallel-is-a-human-decision",
		Reason: "sdlc parallel starts model sessions, optionally with tools pre-approved, and an " +
			"agent that ran it would be starting work nobody asked for",
		Route: "say which stories you meant and stop; a person runs " +
			"`sdlc parallel <ID>...` in their own terminal, outside Claude Code",
	}, true
}

// checkAck keeps acknowledging the log with the person who read it.
//
// `sdlc log` starts after the commit acknowledged, so moving it forward takes
// every story before it off the list somebody reads. An agent that could run
// it would be deciding which stories nobody needs to look at -- the ones it
// worked on among them.
func checkAck(words []string) (Finding, bool) {
	if !runsSubcommand(words, "ack") {
		return Finding{}, false
	}
	return Finding{
		Rule: "acknowledgement-is-a-human-decision",
		Reason: "acknowledging says a person has read what landed on trunk, and an agent that " +
			"gave it would be deciding which stories nobody needs to look at",
		Route: "say what `sdlc log` lists and stop; the person reads it and runs " +
			"`sdlc ack --through <commit>` in their own terminal",
	}, true
}

// checkReviewer keeps a review the reviewer's own.
//
// Every reviewer records its own verdict with `sdlc review add`, and nothing
// asked who was running it: the implementer could record the code reviewer's
// approval of its own work, and the gate passed on it. A person recording one
// in their own terminal is not asked.
func checkReviewer(words []string, s State) (Finding, bool) {
	role, ok := reviewRole(words)
	if !ok || role == s.Agent {
		return Finding{}, false
	}
	who := "the main conversation"
	if s.Agent != "" {
		who = s.Agent
	}
	return Finding{
		Rule: "review-is-recorded-by-its-reviewer",
		Reason: who + " is recording a review as " + role + ", and a review is only worth " +
			"having from the reviewer it names",
		Route: "delegate to sdlc:" + role + ", which reads the work and records its own verdict " +
			"with `sdlc review add`",
	}, true
}

// reviewRole is the reviewer a `sdlc review add GATE ROLE VERDICT` command
// records a review for, if this is one. The gate and the role are found as a
// pair the loop knows, rather than by position, so a note given before them
// cannot move the role somewhere else.
func reviewRole(words []string) (string, bool) {
	args, ok := sdlcArgs(words)
	if !ok || subcommand(args) != "review" || !hasWord(args, "add") {
		return "", false
	}
	var positional []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			if ValueFlags[args[i]] {
				i++
			}
			continue
		}
		positional = append(positional, args[i])
	}
	for i := 0; i+1 < len(positional); i++ {
		if r, ok := model.FindReviewer(model.Gate(positional[i]), positional[i+1]); ok {
			return r.Role, true
		}
	}
	return "", false
}

// runsSubcommand reports whether a command -- one from programsAt -- runs sdlc
// with this subcommand.
func runsSubcommand(words []string, sub string) bool {
	args, ok := sdlcArgs(words)
	return ok && (subcommand(args) == sub || subcommand(slices.DeleteFunc(slices.Clone(args), staffingValueFlag)) == sub)
}

// staffingValueFlag is a flag that takes a value only since the staffing
// commands. With the flag taken out the words are read as they were before
// there was one, so the word after it is read as the subcommand as well as the
// flag's value: read only as the value, a command only a person runs that was
// refused there stopped being refused.
func staffingValueFlag(word string) bool { return word == "--model" || word == "--effort" }

// ValueFlags are the flags of sdlc that take the next word as their value. The
// command's own tests hold this to the flags it has. The help reader and
// named.go go by it too, to tell a flag's value from a flag.
var ValueFlags = map[string]bool{
	"--file": true, "--gate": true, "--message": true, "--note": true,
	"--reason": true, "--reject": true, "--since": true, "--story": true, "--through": true,
	"--usd": true, "--max": true, "--model": true, "--effort": true,
}

// checkCommit puts the commit gate in front of the commit, for a commit in this
// repository: dir is where the command runs, and a commit made in another
// repository -- by `cd`, `git -C`, or a session opened elsewhere -- is not this
// story's to hold back.
//
// placed is whether the words put the command in dir rather than a guess did.
// A commit is written down only when they and its own -C and --git-dir place it
// in the project's own repository: the refusal takes the project as its guess,
// and the recorder taking the same guess wrote down this project's HEAD for a
// commit made in another repository, and a person's next commit here was
// recorded as the story's on that observation.
func checkCommit(words []string, dir string, placed bool, s State, away bool) (Finding, bool) {
	if !isGit(words) {
		return Finding{}, false
	}
	sub, _, _, alias := gitCall(words[1:])
	if !makesACommit(words, sub) {
		return Finding{}, false
	}
	// Once the gates have passed, a rebase is the branch rule's to judge, and
	// its freshness is not asked: the landing checks the change it makes.
	if s.CommitReady && sub == "rebase" {
		return Finding{}, false
	}
	if f, ok := commitElsewhere(words, dir, s, away); ok {
		return f, true
	}
	if !inThisRepository(words, dir, s) {
		return Finding{}, false
	}
	ready, why := s.CommitReady, s.CommitWhy
	// The gates having passed is not the same as the commit being the work
	// that passed them: code edited after the code review went through here
	// and was committed, and only `sdlc gate commit pass` noticed, afterwards.
	if ready && s.Fresh != nil {
		ready, why = s.Fresh()
	}
	if ready {
		// Only a commit this rule allows: the recorder writes down a head only
		// once the whole command has been allowed, and a refused one never
		// happened.
		if s.Commits != nil && placed && ownRepository(words, dir, s) && commitsNow(words[1:], sub, alias) {
			s.Commits()
		}
		return Finding{}, false
	}
	return Finding{
		Rule:   "commit-gate",
		Reason: "this story has not been through the gates that come before committing: " + why,
		// Not `sdlc stop`: ending the iteration turns this rule off, so naming
		// it here told the agent the way round a gate a person was waiting on.
		Route: "finish the gates -- `sdlc status` shows where this story stands; " +
			"committing without them is for the person running the session to decide",
	}, true
}

// pathQuestion is one command of a segment as the path rules ask about it:
// the whole command it is part of and the segment it is, as spelled; the
// expression of the find it runs, as the reader read it; the program and
// its redirects; and where it runs, with whether that is somewhere the
// words do not place.
type pathQuestion struct {
	line, segment string
	exp           *findExpression
	run           invocation
	redirects     []string
	dir           string
	lost          bool
	// certain is whether the command is one the hook reads whole and the
	// plain reader read all of it as it was read here: the same segments, with
	// the same words sent to the same places, and this one from the same
	// directory. Only then is a word of it placed outside the repository. The
	// two readers split a command differently at a separator inside quotes,
	// and with the directory taken from this one, `echo 'a && cd /elsewhere'
	// && rm -f a_test.go` was read as a removal from /elsewhere, and let go.
	certain bool
	s       State
	m       *memo
	// reached are the frozen tests a find's names or operators pick.
	reached []string
}

// findChanges reports whether the words run a find that deletes what it
// finds or runs anything but a reader on it.
func findChanges(words []string) bool {
	return len(words) > 0 && base(words[0]) == "find" && (hasWord(words[1:], findDelete) || runsAWriter(words[1:]))
}

// checkLoopState stops the shell being the way around every other rule.
func checkLoopState(q pathQuestion) (Finding, bool) {
	line, segment, run, m := q.line, q.segment, q.run, q.m
	if q.findsOutside() {
		// find does not leave where it starts: only where its output is sent
		// is left to ask about.
		return q.namesLoopFiles(q.kept(q.redirects, false, eachWord), nil)
	}
	candidates := append([]string{}, q.redirects...)
	candidates = q.addWritten(candidates)
	if runsInlineCode(run.words, segment) {
		// The file a program opens is inside a string, as it is for the
		// freeze below.
		candidates = append(candidates, m.words(line)...)
	}
	// A word placed outside the repository names none of the loop's files: from
	// a directory outside it, `.sdlc/state/active` is that directory's.
	candidates = q.kept(candidates, false, byLanding)
	// What a find's expression picks once its operators are read, kept apart
	// from what the flat reading finds so that the refusal can say so.
	found, picked := q.loopFilesFound()
	candidates = append(candidates, found...)
	// What a command takes away is gone as much as what it writes: `git clean
	// -fdx .sdlc` writes nothing, and empties the loop's record.
	candidates = append(candidates, q.kept(takenAway(run.words), false, eachWord)...)
	if f, ok := q.takesTheRecord(); ok {
		return f, true
	}
	if f, ok := q.takesAnotherCheckout(); ok {
		return f, true
	}
	if f, ok := q.namesLoopFiles(candidates, picked); ok {
		return f, true
	}
	// A glob given to find that cannot be read could pick any of the loop's own
	// files, from a directory that can come to them; and so could an expression
	// the reader cannot follow at all.
	return q.unreadFind(ruleLoopState, nil, func(string) string { return "the loop's own files" })
}

// addWritten adds to candidates what a command that changes files is given
// to write, and for one fed on standard input every word of the line.
func (q pathQuestion) addWritten(candidates []string) []string {
	run := q.run
	if !changesFiles(run.words) {
		return candidates
	}
	for _, w := range writtenTo(run.words) {
		// A whole directory given to an interpreter is a setting of the
		// program it runs, as in `python3 -m pytest --ignore .sdlc`, not a
		// file for it to write.
		if !interpreters[base(run.words[0])] || !bareDirectory(w) {
			candidates = append(candidates, w)
		}
	}
	if run.piped && q.m.first("loop state on a piped line", q.dir) {
		// `echo .sdlc/state/tests.lock | xargs rm` names the file in
		// another segment altogether. The line is read once from each
		// directory: every word of it was spelled again for each `xargs`
		// in it, and a hundred of them took most of a 60 KB command's time.
		candidates = append(candidates, q.m.words(q.line)...)
	}
	return candidates
}

// loopFilesFound is the loop's own files and the protected paths a find that
// changes files picks: found by the flat reading of its names, and picked by
// its expression once the operators are read.
func (q pathQuestion) loopFilesFound() (found, picked []string) {
	run, s, exp := q.run, q.s, q.exp
	if !findChanges(run.words) {
		return nil, nil
	}
	// What find picks by a name, from a directory that can come to the
	// project's own files.
	roots, names, patterns := findArguments(run.words[1:])
	var from []string
	for _, r := range roots {
		if reaches(r, q.dir, s, q.m) {
			from = append(from, r)
		}
	}
	if len(from) > 0 && len(names)+len(patterns) > 0 {
		shapes := append(protectedShapes(s.Backlog), stateFiles...)
		found = foundBy(from, names, patterns, shapes)
	}
	// Beside the flat reading, the shapes the expression picks once its
	// operators are read: a negated or alternated test leaves in files its
	// name does not say. Asked only of an expression the reader followed
	// that holds an operator, and only from a root that reaches the project.
	if exp.followed && exp.operated && len(from) > 0 {
		picked = exp.pickedShapes(from, append(protectedShapes(s.Backlog), stateFiles...))
	}
	return found, picked
}

// takesTheRecord refuses a command that takes the loop's record away with
// the directory or the work tree that holds it.
func (q pathQuestion) takesTheRecord() (Finding, bool) {
	run := q.run
	// Taking the project's own directory away, or throwing away what its work
	// tree holds that is not committed, takes the loop's record with it.
	for _, w := range takenAway(run.words) {
		if path.Join(q.dir, clean(w)) == "." && !q.lost {
			return recordFinding(loopStateDir), true
		}
	}
	if discardsTheWorkTree(run.words) && (inThisRepository(run.words, q.dir, q.s) || inAnotherCheckout(run.words, q.dir, q.s, q.m)) {
		return recordFinding(loopStateDir), true
	}
	return Finding{}, false
}

// takesAnotherCheckout refuses a command that takes another checkout away.
func (q pathQuestion) takesAnotherCheckout() (Finding, bool) {
	// Another checkout taken away takes its loop state with it.
	for _, w := range q.m.spell(q.s, q.dir, takenAway(q.run.words)) {
		for _, l := range q.m.shared(q.s, w) {
			if l.Rel == "." {
				return Finding{
					Rule: ruleLoopState,
					Reason: clean(w) + " is another checkout of this repository, and taking it away takes " +
						"its loop state and its uncommitted work with it",
					Route: "leave the other checkouts to the people and sessions working in them; " +
						"removing one is for a person to do, in their own terminal",
				}, true
			}
		}
	}
	return Finding{}, false
}

// namesLoopFiles refuses the first of the candidates, or of what a find's
// operators picked, that names one of the loop's own files or a protected
// path.
func (q pathQuestion) namesLoopFiles(candidates, picked []string) (Finding, bool) {
	s, m := q.s, q.m
	shapes := func() []string { return protectedShapes(s.Backlog) }
	spelled := withGlobs(m.spell(s, q.dir, candidates), shapes)
	// The flat reading's candidates first, so that a command it refuses is
	// refused in the same words as ever; then what the operators leave in.
	flat := len(spelled)
	spelled = append(spelled, withGlobs(m.spell(s, q.dir, picked), shapes)...)
	for i, c := range spelled {
		if !m.first("loop state", c) {
			continue
		}
		if f, ok := loopFileFinding(c, s, m); ok {
			return leftIn(f, i >= flat), true
		}
	}
	return Finding{}, false
}

// loopFileFinding is the refusal of a word that names one of the loop's own
// files or a protected path, in this checkout or another.
func loopFileFinding(c string, s State, m *memo) (Finding, bool) {
	if f, ok := sharedState(c, s, m); ok {
		return f, true
	}
	if m.outside(s, c) {
		return Finding{}, false
	}
	if hit, ok := protectedPath(c, s.Backlog); ok {
		return Finding{
			Rule: "protected-path-through-the-tool",
			Reason: hit + " is protected while a story is being worked on, and a shell " +
				"command is the one way around the rule that protects it",
			Route: "change it by hand outside a running iteration; if what it says is " +
				"wrong, stop and say what rather than editing it",
		}, true
	}
	hit, ok := loopState(c)
	if !ok {
		return Finding{}, false
	}
	if hit == loopConfigFile {
		return Finding{
			Rule: ruleLoopState,
			Reason: hit + " is the loop's configuration, and a shell command is the one " +
				"way around every rule that protects it",
			Route: "configuration is changed by hand, by the person running the session, " +
				"outside a running iteration; say which setting is wrong and stop",
		}, true
	}
	return recordFinding(hit), true
}

// unreadFind refuses, under rule, a find that changes files and that the
// hook cannot read: one given a glob it cannot read, and then one whose
// expression the reader could not follow, from the first root that could
// come to what the rule protects. frozen is the frozen tests, or nil for the
// loop's own files alone, and what names what is protected, given the test
// the root holds.
func (q pathQuestion) unreadFind(rule string, frozen []string, what func(test string) string) (Finding, bool) {
	run := q.run
	if len(run.words) == 0 || base(run.words[0]) != "find" || !changesFiles(run.words) {
		return Finding{}, false
	}
	if glob, cause, ok := unreadGlob(q.segment, run.words); ok {
		if r, test, ok := q.rootThatCouldHold(frozen); ok {
			return unreadFinding(rule, glob, r, cause, what(test), placedRoot(r, q.dir, q.lost, q.s, q.m)), true
		}
	}
	if !q.exp.followed {
		if r, test, ok := q.rootThatCouldHold(frozen); ok {
			return unfollowedFinding(rule, *q.exp, r, what(test), placedRoot(r, q.dir, q.lost, q.s, q.m)), true
		}
	}
	return Finding{}, false
}

// rootThatCouldHold is the first of the find's roots from which it could
// come to what the rule protects, and the frozen test it holds where it
// holds one.
func (q pathQuestion) rootThatCouldHold(frozen []string) (root, test string, ok bool) {
	roots, _ := findRoots(q.run.words[1:])
	for _, r := range roots {
		if test, ok := couldHold(r, q.dir, q.lost, frozen, q.s, q.m); ok {
			return r, test, true
		}
	}
	return "", "", false
}

// sharedState is checkLoopState for a word landing in another checkout of the
// repository. Another checkout's backlog, records, configuration and git
// directory are protected as the project's are: a session held in one
// checkout wrote the main checkout's records, where the story's evidence is,
// by naming them in full, and every rule stopped at its own checkout's edge.
func sharedState(word string, s State, m *memo) (Finding, bool) {
	for _, l := range m.shared(s, word) {
		protected, byName := protectedPath(l.Rel, s.Backlog)
		hit, state := loopState(l.Rel)
		switch {
		case !byName && !state:
			continue
		case isAbsolute(l.Rel):
			return Finding{
				Rule: ruleLoopState,
				Reason: clean(word) + " could be this repository's own evidence: its main checkout could " +
					"not be found, so anything outside this checkout that might be it is refused",
				Route: "run `sdlc doctor` to see why the main checkout could not be found",
			}, true
		case byName:
			return Finding{
				Rule: "protected-path-through-the-tool",
				Reason: protected + " in another checkout of this repository is protected while a story is " +
					"being worked on, and a shell command is the one way around the rule that protects it",
				Route: "change it by hand outside a running iteration; if what it says is " +
					"wrong, stop and say what rather than editing it",
			}, true
		case hit == loopConfigFile:
			return Finding{
				Rule: ruleLoopState,
				Reason: clean(word) + " is the configuration of another checkout of this repository, and " +
					"a shell command is the one way around every rule that protects it",
				Route: "configuration is changed by hand, by the person running the session, " +
					"outside a running iteration; say which setting is wrong and stop",
			}, true
		}
		return recordFinding(hit), true
	}
	return Finding{}, false
}

// checkFrozenTests is the freeze, applied to the shell.
//
// Every other rule the freeze has is enforced against the file tools, and a
// shell command went straight past them: `Write` to `x_test.go` was refused as
// a frozen acceptance test, and `echo cheat > x_test.go` was allowed. One
// redirect was the whole way round the hinge the loop turns on.
func checkFrozenTests(q pathQuestion) (Finding, bool) {
	s, run := q.s, q.run
	if len(s.Frozen) == 0 && s.IsTest == nil && s.NewTest == nil && s.ImplementerTest == nil {
		return Finding{}, false
	}
	inline := runsInlineCode(run.words, q.segment)
	own := runsAProgramOfItsOwn(run.words)
	byName := q.frozenByName(inline)
	if q.findsOutside() {
		// Only where its output is sent is left to ask about.
		return q.namesFrozenTests(q.kept(q.redirects, false, eachWord), nil, nil, byName)
	}
	candidates := append([]string{}, q.redirects...)
	if inline || own {
		candidates = q.addProgramWords(candidates)
	}
	candidates, removed := q.addFrozenWritten(candidates)
	// A word placed outside the repository is no test of its freeze, whatever
	// its name: the Write tool refuses it for being outside, and the shell has
	// nothing there to protect.
	candidates, removed = q.kept(candidates, false, byLandingOrRead), q.kept(removed, true, eachWord)
	// The frozen tests a find's expression picks once its operators are read,
	// kept apart from what the flat reading finds.
	found, picked := q.frozenTestsFound()
	candidates = append(candidates, found...)
	q.reached = append(append([]string{}, found...), picked...)
	if f, ok := q.takesFrozenTests(); ok {
		return f, true
	}
	if f, ok := q.namesFrozenTests(candidates, picked, removed, byName); ok {
		return f, true
	}
	// A glob given to find that cannot be read could pick any frozen test below
	// where find starts, and from a directory that can come to the project's
	// own files, any of them.
	if len(s.Frozen) == 0 {
		return Finding{}, false
	}
	return q.unreadFind(ruleFrozenTest, s.Frozen, func(test string) string {
		return "a frozen acceptance test (" + cmp.Or(test, s.Frozen[0]) + ")"
	})
}

// frozenByName is whether a bare name is read as the end of a frozen path.
//
// A bare name is the end of a frozen path only where the directory it is
// read from is not known: after a `cd` this cannot follow, and for the
// names find, xargs, `git -C` and a program's own code work with. Taken
// that way everywhere, a fixture's config.json made `cp config.example.json
// config.json` at the root a write to a frozen test.
func (q pathQuestion) frozenByName(inline bool) bool {
	run := q.run
	byName := q.lost || inline || run.piped
	if len(run.words) > 0 {
		switch base(run.words[0]) {
		case "find":
			byName = true
		case "git":
			_, moved := gitCommand(run.words[1:])
			byName = byName || moved != ""
		}
	}
	return byName
}

// addProgramWords adds to candidates the words of the whole line, for a
// command that runs a program written on it, but for those the program is
// only given to read.
func (q pathQuestion) addProgramWords(candidates []string) []string {
	// `python3 -c '...'` is a program, not a list of arguments, and the
	// file it opens is inside a string. There is no parsing this without
	// being an interpreter, so the whole of it is searched instead: a
	// frozen path appearing anywhere in code that runs is enough. The whole
	// command line, because a `;` inside the program splits it into
	// segments, and a here-document puts the program on lines of its own.
	// A plain argument is left out: it is a file the program is given to
	// read, as in `perl -ne 'print' x_test.go`, and reading a frozen test
	// that way is never refused. Only that way: named anywhere else on the
	// line, or beside a program that came from elsewhere, a read is refused
	// along with the writes, which is the cost the whole-line reading is
	// worth and what docs/enforcement.md is careful to say.
	//
	// awk's and sed's programs are the same case and arrive here without
	// runsInlineCode, which they are deliberately kept out of: they get the
	// whole-line reading without the bare-name matching byName would bring
	// with it. Asked only what their arguments say, this allowed
	// `awk 'BEGIN{print "x" > "internal/x_test.go"}'`, which was run
	// against a scratch file with the real tool and overwrote it.
	//
	// The words come from line, which is the reading's command with its
	// documents still in it, and not from the text the rules are applied
	// to. `awk -f - <<'EOF'` puts the program in a body withoutDocuments
	// takes out, and drawing the candidates from there would lose it with
	// no test to notice.
	plain := plainArguments(q.segment)
	if tookItsProgramElsewhere(q.run.words) {
		// Nothing on the line says which of the files is data, so none of
		// them is left out.
		plain = nil
	}
	for _, w := range q.m.words(q.line) {
		if !plain[w] {
			candidates = append(candidates, w)
		}
	}
	return candidates
}

// addFrozenWritten adds to candidates what a command that changes a file is
// given to write, and for one fed on standard input every word of the line;
// what a command only takes away is returned apart, as removed.
func (q pathQuestion) addFrozenWritten(candidates []string) (added, removed []string) {
	run := q.run
	if !changesAFile(run.words) {
		return candidates, nil
	}
	if removes(run.words) {
		removed = writtenTo(run.words)
	} else {
		candidates = append(candidates, writtenTo(run.words)...)
	}
	if run.piped {
		candidates = append(candidates, q.m.words(q.line)...)
	}
	return candidates, removed
}

// frozenTestsFound is the frozen tests a find that changes files picks:
// found by the flat reading of its names, and picked by its expression once
// the operators are read.
func (q pathQuestion) frozenTestsFound() (found, picked []string) {
	run, s, exp := q.run, q.s, q.exp
	if !findChanges(run.words) || len(s.Frozen) == 0 {
		return nil, nil
	}
	// A name find is given with a glob in it, wherever it starts, as the
	// freeze reads find's names. Not one that is all wildcards: from
	// anywhere, that is every file, and `find build -name '*' -delete`
	// was a frozen test.
	roots, names, patterns := findArguments(run.words[1:])
	found = foundBy(roots, literals(names), literals(patterns), frozenShapes(s.Frozen))
	// The frozen tests the expression picks once its operators are read,
	// from each root that could hold one below it.
	if exp.followed && exp.operated {
		var could []string
		for _, r := range roots {
			if _, ok := couldHold(r, q.dir, q.lost, s.Frozen, s, q.m); ok {
				could = append(could, r)
			}
		}
		if len(could) > 0 {
			picked = exp.pickedShapes(could, frozenShapes(s.Frozen))
		}
	}
	return found, picked
}

// takesFrozenTests refuses a command that takes away a directory holding a
// frozen test.
func (q pathQuestion) takesFrozenTests() (Finding, bool) {
	run, s, m := q.run, q.s, q.m
	// A directory taken away takes the frozen tests in it along: `rm -rf
	// internal/calc` removed internal/calc/add_test.go without naming it.
	for _, c := range m.spell(s, q.dir, takenAway(run.words)) {
		// Outside the project, a directory ending in internal/calc is another
		// project's: `rm -rf /tmp/snap/internal` cleans up a scratch copy.
		if m.outside(s, c) {
			continue
		}
		if frozen, ok := holding(c, s.Frozen); ok {
			return frozenFinding(frozen), true
		}
	}
	// A glob is expanded where the command runs, so that has to be known. Read
	// as written as well, `cd dist && rm -rf *` was every frozen test. A word
	// with no glob in it matches here as itself.
	for _, w := range takenAway(run.words) {
		if !q.lost {
			if frozen, ok := globHolding(path.Join(q.dir, clean(w)), s.Frozen); ok {
				return frozenFinding(frozen), true
			}
		}
	}
	return Finding{}, false
}

// namesFrozenTests refuses the first word that names a frozen test or a
// test file the freeze does not let through: among the candidates, then what
// a find's operators picked, then what the command only takes away.
func (q pathQuestion) namesFrozenTests(candidates, picked, removed []string, byName bool) (Finding, bool) {
	s, m := q.s, q.m
	rule := "freeze"
	if byName {
		rule = "freeze by name"
	}
	shapes := func() []string { return frozenShapes(s.Frozen) }
	added := withGlobs(m.spell(s, q.dir, candidates), shapes)
	// What the flat reading finds first, in the words it has always been
	// refused in; then what a find's operators leave in.
	flat := len(added)
	added = append(added, withGlobs(m.spell(s, q.dir, picked), shapes)...)
	spelled := slices.Concat(added, withGlobs(m.spell(s, q.dir, removed), shapes))
	for i, c := range spelled {
		// A file taken away is no test added: removing the test files the freeze
		// does not hold is what SDLC-E0043 asks for. Remembered apart, or `rm
		// x_test.go; echo > x_test.go` read the second as already checked.
		taken := i >= len(added)
		key := rule
		if taken {
			key = rule + ", taken away"
		}
		if !m.first(key, c) {
			continue
		}
		if f, ok := q.frozenWord(c, byName, taken, i >= flat && !taken); ok {
			return f, true
		}
	}
	return Finding{}, false
}

// frozenWord is the refusal of one word under the freeze: taken where the
// command only takes it away, and left where a find's operators picked it.
func (q pathQuestion) frozenWord(c string, byName, taken, left bool) (Finding, bool) {
	s := q.s
	frozen, ok := q.m.frozen(c, s.Frozen, byName)
	w := strings.TrimPrefix(clean(c), "./")
	if !ok && w != "" && s.IsTest != nil && s.IsTest(w) {
		frozen, ok = w, true
	}
	if !ok && w != "" && !taken && s.NewTest != nil && s.NewTest(w) {
		return q.unplacedWord(Finding{
			Rule: "no-new-test-after-the-freeze",
			Reason: w + " would be a new test file added after the freeze, which is the " +
				"freeze with extra steps: a test written now can be written to pass",
			Route: "say which case is missing and stop: the frozen files are frozen too, " +
				"and the person running the session can unfreeze and freeze again so the " +
				"new test is covered",
		}, c), true
	}
	if ok {
		return leftIn(q.explained(frozenFinding(frozen), c, frozen), left), true
	}
	if w != "" && s.ImplementerTest != nil && s.ImplementerTest(w) {
		return q.unplacedWord(Finding{
			Rule: "implementer-does-not-write-tests",
			Reason: w + " is a test file, and the implementer does not write tests -- through " +
				"the shell any more than through the file tools",
			Route: "make the existing tests pass; if they are wrong, say so rather than " +
				"changing them",
		}, c), true
	}
	return Finding{}, false
}

func frozenFinding(frozen string) Finding {
	return Finding{
		Rule: ruleFrozenTest,
		Reason: frozen + " is a frozen acceptance test, and a shell command is " +
			"the one way around every rule that protects it",
		Route: "leave it alone -- it was locked by content when the test gate " +
			"passed, and every gate after that is measured against it. If it " +
			"genuinely has to change, say which and why: the person running the " +
			"session lifts the freeze with `sdlc unfreeze --reason ...`, which puts the " +
			"reason on the record",
	}
}

func recordFinding(hit string) Finding {
	return Finding{
		Rule: ruleLoopState,
		Reason: hit + " is the loop's own record, and a shell command is the one " +
			"way around every rule that protects it",
		Route: "the sdlc command owns this: `sdlc artifact write` for a gate's " +
			"documents, `sdlc review add` for a review, `sdlc gate` for an " +
			"outcome; the freeze is lifted by the person running the session",
	}
}

// changesAFile reports whether this command, as it is written, exists to
// change a file. One that only reads it is left alone: reading a frozen test is
// how the implementer knows what to implement, and `sed -n 1,20p x_test.go` has
// to go through.
//
// For sed, awk and gawk this is half of what the freeze asks. What their own
// program writes is in no argument of theirs, so checkFrozenTests reads the
// whole line for them as well through runsAProgramOfItsOwn; the read this keeps
// allowed is the one named as a plain argument.
func changesAFile(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch name := base(words[0]); name {
	case "sed", "perl":
		return editsInPlace(words[1:])
	case "awk", "gawk":
		// gawk rewrites what it reads with its inplace extension loaded;
		// otherwise it needs a redirect, which is already counted.
		for _, a := range words[1:] {
			if strings.Contains(a, "inplace") {
				return true
			}
		}
		return false
	case "find":
		return hasWord(words[1:], findDelete) || len(printedTo(words[1:])) > 0
	case "git":
		return gitChangesFiles(words[1:])
	default:
		return mutating[name]
	}
}

// changesFiles is changesAFile for the loop's own record and the protected
// paths, which is broader: refusing a read of them costs little, since cat,
// grep, head and the file tools read them, and nothing downstream notices
// a record rewritten the way the gate notices a frozen test that changed.
//
// So a program that can write through what it is given counts whatever it
// is asked to do. Counted only when editing in place, sed wrote with its `w`
// command, awk with `print > FILENAME`, `python3 -m json.tool` with its second
// argument, and every interpreter with a script put in /tmp first.
func changesFiles(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch base(words[0]) {
	case "find":
		// What find runs is a write unless it is a program that only reads:
		// `-exec wc -l {} +` changes nothing, and `-exec ./tidy {} +` cannot be
		// seen to leave anything alone.
		return hasWord(words[1:], findDelete) || runsAWriter(words[1:]) || len(printedTo(words[1:])) > 0
	case "sed", "perl", "awk", "gawk":
		return true
	}
	if interpreters[base(words[0])] {
		return true
	}
	return changesAFile(words)
}

// runsAWriter reports whether find runs anything but a reader on what it finds.
func runsAWriter(args []string) bool {
	for i, a := range args {
		switch a {
		case "-exec", "-execdir", "-ok", "-okdir":
			if i+1 == len(args) || !readers[base(args[i+1])] {
				return true
			}
		}
	}
	return false
}

// readers are the programs that only read what they are given. Two rules go
// by it: what find may run on the files it finds, and, in named.go, what the
// output of a command may be piped to while a path in its text is still only
// named. A program added for the first is trusted by the second, so one goes
// in only if it cannot run what arrives on its standard input, whatever its
// options. ripgrep's `--pre` runs a program on a file named as an argument
// and not on what is piped in (seen with ripgrep 14.1.1), and a file named
// there is read by the rules as it always was.
var readers = map[string]bool{
	"cat": true, "head": true, "tail": true, "wc": true, "grep": true, "rg": true,
	"ls": true, "stat": true, "file": true, "du": true, "cksum": true,
	"md5sum": true, "sha1sum": true, "sha256sum": true, "shasum": true,
	"diff": true, "cmp": true, "basename": true, "dirname": true, "realpath": true,
}

// gitChangesFiles reports whether a git command rewrites or removes the files
// named after it: `git rm`, `git mv`, `git checkout -- path`, `git restore`.
func gitChangesFiles(args []string) bool {
	switch sub, _, rest, _ := gitCall(args); sub {
	case "mv", "checkout":
		return true
	case "stash":
		// A stash of named paths takes those paths away.
		return len(stashPaths(rest)) > 0
	case "rm":
		// --cached takes a path out of the index and leaves the file.
		return !gitOption(rest, "--cached")
	case "restore":
		// --staged alone puts back the index, and the file stays as it is.
		staged := gitOption(rest, "--staged") || shortFlag(rest, 'S')
		return !staged || gitOption(rest, "--worktree") || shortFlag(rest, 'W')
	}
	return false
}

// editsInPlace reports whether sed or perl was asked to rewrite the files it
// reads: -i on its own, bundled with other flags (`-Ei`, `-pi.bak`), or
// --in-place. Only a flag spelled `-i...` counted, and `sed -Ei` is common.
func editsInPlace(args []string) bool {
	for _, a := range args {
		name, _, _ := strings.Cut(a, "=")
		switch {
		case len(name) >= 3 && len(name) <= len("--in-place") && name == "--in-place"[:len(name)]:
			// GNU sed takes any start of a long option that names only one, and
			// no other option of sed's starts with --i: `sed --in-pl`.
			return true
		case strings.HasPrefix(a, "--") || !strings.HasPrefix(a, "-"):
			continue
		}
		for _, r := range a[1:] {
			if r == 'i' {
				return true
			}
			// What follows one of these is that option's argument, not more
			// flags: `perl -Mstrict`, `sed -e ...`. Not perl's -l, which takes a
			// number or nothing and then more flags: read as taking the rest,
			// `perl -lpi` hid its -i.
			if strings.ContainsRune("efIMmx", r) {
				break
			}
		}
	}
	return false
}

// runsInlineCode reports whether this is an interpreter being handed a program
// on the command line or on standard input, where the files it touches are not
// arguments at all.
//
// Only the interpreter's own options are read. Everything from the script or
// the module on belongs to it, so `python3 -m pytest -c setup.cfg` runs tests
// rather than a program called setup.cfg.
func runsInlineCode(words []string, segment string) bool {
	if len(words) == 0 {
		return false
	}
	var code string // the short options whose argument is a program
	name := base(words[0])
	switch name {
	case "python", "python3", "py":
		code = "c"
	case "perl", "ruby", "node", "bun":
		code = "eEp"
	case "php":
		code = "r"
	case "deno":
	default:
		return false
	}
	// A here-document is a program on standard input: `python3 - <<'EOF'`.
	if strings.Contains(segment, "<<") {
		return true
	}
	for _, a := range words[1:] {
		switch {
		case a == "-":
			return true
		case a == "--eval" || a == "--print" ||
			strings.HasPrefix(a, "--eval=") || strings.HasPrefix(a, "--print="):
			return true
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-"):
			if inline, decided := shortOptionsRunCode(a, code); decided {
				return inline
			}
		default:
			return name == "deno" && a == "eval"
		}
	}
	return false
}

// shortOptionsRunCode reads one word of an interpreter's short options, a
// being a word that starts with a single dash and code the letters whose
// argument is a program. It decides where the word names a module, which is
// not inline code, or holds one of those letters; any other word leaves the
// question to the words after it.
func shortOptionsRunCode(a, code string) (inline, decided bool) {
	if strings.HasPrefix(a, "-m") && code == "c" {
		return false, true
	}
	if strings.ContainsAny(a[1:], code) && code != "" {
		return true, true
	}
	return false, false
}

// runsAProgramOfItsOwn reports whether this is awk, gawk or sed: a tool whose
// program writes files by means no argument of the command line shows. `print
// > FILE` is awk's own output and `w` is sed's, neither of them a shell
// redirect parse reads, and both were run against a scratch file with the real
// tools while the freeze allowed the command.
//
// The same idea as runsInlineCode, and deliberately a second function rather
// than three more names in that one. byName in checkFrozenTests is computed
// from inline, so folding them together would match a bare name against the end
// of every frozen path as well, and a fixture's main.go in another package was
// an ordinary edit read as a write to a frozen test. runsInlineCode's other
// caller is the loop-state rule, which this is not about.
//
// These three names are read at changesAFile and changesFiles too, both with
// perl beside them. A spelling none of the three knows is open at all of them.
func runsAProgramOfItsOwn(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch base(words[0]) {
	case "awk", "gawk", "sed":
		return true
	}
	return false
}

// tookItsProgramElsewhere reports whether awk, gawk or sed was given its
// program somewhere other than its first argument: from a file with -f or
// --file, or from -e/--source and gawk's -E/--exec. Then the first argument is
// a data file like every one after it, and nothing on the line says which of
// them the program only reads.
//
// Every word is read, and not only the first. Stopped at the first argument
// that is not a flag, `awk -F : -f w.awk other.txt x_test.go` and `awk -v q=1
// -f w.awk other.txt x_test.go` ended the scan on the option's own value, never
// saw the -f, and overwrote the frozen test.
func tookItsProgramElsewhere(words []string) bool {
	// The name first and the flags only after it. A set applied by flag rather
	// than by name puts perl's -e in it, and `perl -ne 'print' x_test.go` is a
	// read the freeze has always allowed.
	if !runsAProgramOfItsOwn(words) {
		return false
	}
	// sed is the only one of the three without --source and --exec. Its -e is a
	// program on the command line, which the freeze already reads where it
	// stands, and its -E is extended regular expressions: inverting either
	// would refuse `sed -e '1,5p' -n x_test.go` and `sed -E 's/a/b/' x_test.go`,
	// both of them reads.
	awk := base(words[0]) != "sed"
	inverts, valued := "f", "efli"
	if awk {
		inverts, valued = "feE", "vFfeilE"
	}
	for _, a := range words[1:] {
		switch {
		case strings.HasPrefix(a, "--"):
			name, _, _ := strings.Cut(a, "=")
			if namesAProgramSource(name, awk) {
				return true
			}
		case strings.HasPrefix(a, "-"):
			// What follows a letter in valued is that option's value and not
			// more flags. Read as flags, `awk -vf=1 '{print}' x_test.go` was a
			// program taken from a file and an ordinary read was refused.
			if bundleHolds(a[1:], inverts, valued) {
				return true
			}
		}
	}
	return false
}

// namesAProgramSource reports whether a long option, cut at its `=`, gives
// the tool its program: --file, and for awk and gawk --source and --exec.
func namesAProgramSource(name string, awk bool) bool {
	return abbreviates(name, "--file") ||
		awk && (abbreviates(name, "--source") || abbreviates(name, "--exec"))
}

// abbreviates reports whether name is a start of option that GNU accepts for
// it: `--fil` is --file, and so is `--f`, which names --follow-symlinks and
// --field-separator as well. Refusing the wider set is the direction this errs
// in.
//
// Both bounds are load-bearing, and editsInPlace carries the same pair for the
// same reasons. Without the first, the end-of-options `--` is a start of every
// option and `sed -n 1p -- x_test.go` is refused. Without the second the slice
// panics on `sed --line-length=80 -n l x_test.go`, and a panic in the hook
// answers {"continue":true}: a refusal lost without a word to anyone.
func abbreviates(name, option string) bool {
	return len(name) >= 3 && len(name) <= len(option) && name == option[:len(name)]
}

// plainArguments are the words of a segment written as bare arguments: not an
// option, and with no quote or bracket in them, so not a piece of a program.
func plainArguments(segment string) map[string]bool {
	plain := map[string]bool{}
	for _, f := range strings.Fields(segment)[1:] {
		if !strings.HasPrefix(f, "-") && !strings.ContainsAny(f, `'"()[]{};`) {
			plain[f] = true
		}
	}
	return plain
}

// invocation is the program a segment runs, once what only wraps it is taken
// off the front.
type invocation struct {
	words []string // the program and its arguments
	piped bool     // its arguments also arrive on standard input, through xargs
	shell bool     // a shell was taken off the front, so the words are a script it runs
	// assigns are the variables env sets for the program, NAME=value.
	assigns []string
	// chdir is where env -C runs the program, or "".
	chdir string
}

// program takes off the front of a segment whatever only runs the command after
// it: `(`, `then`, `env`, `sudo`, `sh -c`, `xargs` and their like. Read as the
// first word, `(git commit)`, `env git commit` and `sh -c 'git commit'` were
// commands called `(git`, `env` and `sh`, and no rule knew any of them.
func program(words []string) invocation {
	var run invocation
	for len(words) > 0 {
		first := strings.TrimLeft(words[0], "({!")
		if first == "" {
			words = words[1:]
			continue
		}
		var done bool
		if words, done = run.unwrap(first, words); done {
			return run
		}
	}
	return run
}

// unwrap takes one wrapper off the front of words, whose first word, with
// what opens a group taken off it, is first, and returns the words after it.
// It is done where first is the program itself, or a wrapper that runs
// nothing: run then has its words.
func (run *invocation) unwrap(first string, words []string) ([]string, bool) {
	switch base(first) {
	case "if", "then", "else", "elif", "do", "while", "until", "time":
		words = words[1:]
	case "xargs":
		run.piped = true
		// Read as flags, the value of `xargs -I {} rm {}` was the program.
		words = skipOptions(skipValued(words[1:], "-I", "-n", "-P", "-L", "-d", "-s", "-E", "-a",
			"--arg-file", "--delimiter", "--max-args", "--max-procs", "--max-lines", "--max-chars",
			"--replace", "--eof"))
	case "env":
		words = run.env(words[1:])
	case "command", "builtin", "nohup", "nice", "caffeinate", "setsid", "unbuffer", "chronic", "firejail":
		words = skipOptions(words[1:])
	case "busybox", "toybox":
		// The applet after it is the program: `busybox rm -rf dir`.
		words = words[1:]
	case "taskset":
		// Its options, then the CPUs, then the command.
		words = afterFirst(skipValued(words[1:]))
	case "ts":
		// task-spooler queues the command after its options.
		words = skipOptions(skipValued(words[1:], "-L", "-D", "-N", "-W"))
	case "nsenter":
		words = skipValued(words[1:], "-t", "--target", "-S", "--setuid", "-G", "--setgid")
	case "unshare":
		words = run.skipValuedTo(words[1:], []string{"-w", "--wd"}, "-S", "--setuid", "-G", "--setgid",
			"-R", "--root", "--propagation", "--setgroups", "--map-user", "--map-group", "--map-users",
			"--map-groups")
	case "systemd-run":
		words = run.skipValuedTo(words[1:], []string{"--working-directory"}, "-u", "--unit", "-p",
			"--property", "--description", "--slice", "-E", "--setenv", "--uid", "--gid", "--nice", "-M",
			"--machine", "-H", "--host", "--service-type", "--on-active", "--on-boot", "--on-startup",
			"--on-unit-active", "--on-unit-inactive", "--on-calendar", "--path-property",
			"--socket-property", "--timer-property")
	case "watch":
		// watch hands its words to sh -c, as one command line.
		run.shell = true
		words = skipValued(words[1:], "-n", "--interval", "-q", "--equexit", "-s", "--shotsdir")
	case "parallel":
		// GNU parallel runs its command through a shell, once for each
		// argument after :::, which are left among its words, and takes
		// them from standard input where there are none.
		run.shell, run.piped = true, true
		words = skipValued(words[1:], "-j", "--jobs", "-P", "--max-procs", "-n", "--max-args", "-N",
			"--max-replace-args", "-L", "--max-lines", "-s", "--max-chars", "-a", "--arg-file", "-d",
			"--delimiter", "-E", "-I", "-C", "--colsep", "-S", "--sshlogin", "--sshloginfile", "--slf",
			"--results", "--res", "--joblog", "--timeout", "--delay", "--memfree", "--load", "--tmpdir",
			"--workdir", "--wd", "--tagstring", "--env", "--header", "--retries", "--halt", "--basefile",
			"--bf", "--return", "--transferfile", "--tf", "--nice", "--rpl")
	case "exec":
		words = skipOptions(skipValued(words[1:], "-a"))
	case "stdbuf":
		words = skipOptions(skipValued(words[1:], "-i", "-o", "-e"))
	case "ionice":
		words = skipValued(words[1:], "-c", "-n", "-p", "-P", "-u",
			"--class", "--classdata", "--pid", "--pgid", "--uid")
	case "chrt":
		// Its options, then the priority, then the command.
		words = skipValued(words[1:], "-T", "-P", "-D", "--sched-runtime", "--sched-period", "--sched-deadline")
		if len(words) > 0 && strings.Trim(words[0], "0123456789") == "" {
			words = words[1:]
		}
	case "flock", "script":
		// With a command line of their own they are sh -c.
		var line bool
		words, line = flockedOrScripted(base(first), words[1:])
		run.shell = run.shell || line
	case "sudo", "doas":
		words = run.skipValuedTo(words[1:], []string{"-D", "--chdir"},
			"-u", "-g", "-h", "-p", "-C", "-R", "-r", "-t", "-T", "-U")
	case "sh", "bash", "zsh", "dash", "ksh", "pwsh", "powershell":
		run.shell = true
		words = skipOptions(words[1:])
	case "ssh":
		// What follows the host is a command line for the shell at the
		// other end, and localhost is this machine.
		run.shell = true
		words = skipValued(words[1:], "-B", "-b", "-c", "-D", "-E", "-e", "-F", "-I", "-i", "-J",
			"-L", "-l", "-m", "-O", "-o", "-P", "-p", "-Q", "-R", "-S", "-W", "-w")
		words = afterFirst(words)
	case "timeout":
		// skipOptions took the duration for a number an option was given,
		// and then the command for the duration: `timeout 60 git commit`
		// was a command called `commit`.
		words = afterFirst(skipValued(words[1:], "-s", "-k", "--signal", "--kill-after")) // the duration
	case "direnv":
		if len(words) < 3 || words[1] != "exec" {
			run.words = append([]string{first}, words[1:]...)
			return nil, true
		}
		words = words[3:]
	case "cmd":
		// cmd's switches start with a slash: `cmd /c del file`, `cmd /s /c`.
		// Git Bash turns an argument that starts with one slash into a
		// Windows path, so there they are written with two, `cmd //c`,
		// which was read as the command and let whatever followed through.
		run.shell = true
		words = afterCmdSwitches(words[1:])
	default:
		run.words = append([]string{first}, words[1:]...)
		return nil, true
	}
	return words, false
}

// afterFirst is words without the first of them, where there is one: the
// one word a wrapper takes before its command.
func afterFirst(words []string) []string {
	if len(words) > 0 {
		return words[1:]
	}
	return words
}

// flockedOrScripted is what flock or script, whichever name is, runs, and
// whether that is a command line.
func flockedOrScripted(name string, args []string) ([]string, bool) {
	if name == "flock" {
		return flocked(args)
	}
	return scripted(args)
}

// afterCmdSwitches is cmd's words after its switches, each a word that
// starts with one slash or two and holds no other.
func afterCmdSwitches(words []string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "/") &&
		!strings.Contains(strings.TrimPrefix(words[0][1:], "/"), "/") {
		words = words[1:]
	}
	return words
}

// underEval is an invocation with the shell's `eval` taken off the front of
// it, and how many were: eval runs its words in the shell that calls it, so
// to the path rules the command after it is the command. What the outer
// invocation set stays set, and what wraps the command under the builtin is
// taken off in turn.
//
// Only the path rules ask this. The rules that turn on which program runs read
// the builtin themselves, with its string and the variables set in front of
// it, and are handed what program makes of the words as they always were.
func underEval(run invocation) (invocation, int) {
	evals := 0
	// Each turn leaves fewer words than the one before.
	for len(run.words) > 1 && base(run.words[0]) == "eval" {
		under := program(run.words[1:])
		under.shell = under.shell || run.shell
		under.piped = under.piped || run.piped
		under.assigns = slices.Concat(run.assigns, under.assigns)
		under.chdir = cmp.Or(under.chdir, run.chdir)
		run = under
		evals++
	}
	return run, evals
}

// skipValued drops the flags a wrapper takes before the command it runs, with
// the value each of the valued ones is given: `sudo -u me`, `timeout -s KILL`.
func skipValued(words []string, valued ...string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") {
		if hasWord(valued, words[0]) && len(words) > 1 {
			words = words[1:]
		}
		words = words[1:]
	}
	return words
}

// skipValuedTo is skipValued for a wrapper with an option that runs the
// command in another directory, as env -C does, which it keeps as where the
// command runs: `sudo -D <main checkout> git commit` commits there.
func (run *invocation) skipValuedTo(words, moves []string, valued ...string) []string {
	for len(words) > 0 && strings.HasPrefix(words[0], "-") {
		name, value, glued := strings.Cut(words[0], "=")
		moved := hasWord(moves, name)
		if (moved && !glued || hasWord(valued, words[0])) && len(words) > 1 {
			words = words[1:]
			value = words[0]
		}
		if moved {
			run.chdir = value
		}
		words = words[1:]
	}
	return words
}

// skipOptions drops the flags a wrapper takes before the command it runs, and
// the numbers some of them take (`nice -n 10`).
func skipOptions(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		if !strings.HasPrefix(w, "-") && strings.Trim(w, "0123456789") != "" {
			break
		}
		words = words[1:]
	}
	return words
}

// changedDir follows a `cd`, so that a relative path after it is read from where
// the command actually is. A directory it cannot follow -- home, `-`, a
// variable, where popd goes back to -- is "", and from there on a bare name is
// read as the name a frozen path ends in.
func changedDir(dir string, words []string) (string, bool) {
	if len(words) == 0 {
		return dir, false
	}
	switch base(words[0]) {
	case "cd", "pushd", "chdir", "set-location", "sl", "push-location":
	case "popd", "pop-location":
		return "", true
	default:
		return dir, false
	}
	args := skipOptions(words[1:])
	if len(args) > 1 && strings.EqualFold(args[0], "/d") {
		// cmd's switch to change drive as well, not a directory called /d.
		args = args[1:]
	}
	if len(args) == 0 {
		return "", true
	}
	to := clean(args[0])
	switch {
	case to == "" || to == "-" || strings.HasPrefix(to, "~") || strings.HasPrefix(to, "$"):
		return "", true
	case isAbsolute(to):
		return to, true
	}
	return path.Join(dir, to), true
}

// spellings are the ways the words of a command can name a file: as written;
// as the value given to an option, like `dd of=path` or `--output=path`; and
// relative to the directory the command is in.
func spellings(dir string, words []string) []string {
	var out []string
	for _, w := range words {
		named := namedBy(w)
		out = append(out, named...)
		if dir == "" || dir == "." {
			continue
		}
		for _, n := range named {
			if c := clean(n); c != "" && !isAbsolute(c) {
				out = append(out, path.Join(dir, c))
			}
		}
	}
	return out
}

// namedBy is the ways one word can name a file, the word as written first.
func namedBy(w string) []string {
	named := []string{w}
	// Outside PowerShell a backslash escapes the character after it, and the
	// shell takes it out: `.sd\lc` is .sdlc. Read as a separator as well, for
	// the Windows spelling.
	if strings.Contains(w, `\`) {
		named = append(named, strings.ReplaceAll(w, `\`, ""))
	}
	if _, value, ok := strings.Cut(w, "="); ok && value != "" {
		named = append(named, value)
	}
	// PowerShell gives a parameter its value after a colon as well:
	// `Set-Content -Path:CLAUDE.md`.
	if strings.HasPrefix(w, "-") {
		if _, value, ok := strings.Cut(w, ":"); ok && value != "" {
			named = append(named, value)
		}
	}
	// A short option takes its value without a space, after it and any
	// flags bundled before it: `-C.sdlc`, `-oCLAUDE.md`, `-sSLo.sdlc/x`.
	// Only after letters, which options are: every spelling is looked up on
	// disk, and `curl -d{...}` is not thousands of paths.
	if len(w) > 2 && w[0] == '-' && w[1] != '-' {
		for i := 2; i < len(w) && isLetter(w[i-1]); i++ {
			named = append(named, w[i:])
		}
	}
	return named
}

// isLetter reports whether b can be a short option: `-x`, not the `.` or `/`
// that starts a path.
func isLetter(b byte) bool {
	return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// memo is what one Inspect has already worked out. A long command names the
// same words in segment after segment, and each was matched against the whole
// freeze and looked up on disk again for every segment it appeared in: a
// hundred `echo ... | xargs rm` took longer than Claude Code gives the hook,
// which then let the command through without a word.
type memo struct {
	checked  map[string]bool      // a rule and a spelling it has found nothing in
	resolved map[string]string    // what Resolve said of a word
	landed   map[string][]Landing // what Shared said of a word
	folded   []string             // the freeze, folded
	line     []string             // the words of the whole command
	split    bool
	disk     *diskAnswers // what the hook said of the checkouts, for every reading
	whole    []plainRun   // the command as the plain reader read it, or nil; see plainCommand
}

// diskAnswers is what State.Checkouts said, asked once for a command, and where
// each word it was asked about is.
type diskAnswers struct {
	asked  bool
	tree   *checkoutTree
	placed map[string][]string
}

// checkouts is the tree of the repository's checkouts, or nil where the hook
// gave none.
func (m *memo) checkouts(s State) *checkoutTree {
	if m.disk == nil {
		m.disk = &diskAnswers{}
	}
	if !m.disk.asked {
		m.disk.asked = true
		if s.Checkouts != nil {
			m.disk.tree = newCheckoutTree(s.Checkouts())
		}
	}
	return m.disk.tree
}

// place is where the tree's Place puts a word, asked once per command.
func (m *memo) place(t *checkoutTree, word string) []string {
	if t.place == nil {
		if isAbsolute(word) {
			return []string{word}
		}
		return nil
	}
	if m.disk.placed == nil {
		m.disk.placed = map[string][]string{}
	}
	p, ok := m.disk.placed[word]
	if !ok {
		p = t.place(word)
		m.disk.placed[word] = p
	}
	return p
}

// first reports whether this is the first time a rule looks at a spelling in
// this command. A spelling that was looked at before found nothing, or the
// command would have been refused then.
func (m *memo) first(rule, spelling string) bool {
	key := rule + "\x00" + spelling
	if m.checked[key] {
		return false
	}
	m.checked[key] = true
	return true
}

// words is wordsIn(line), worked out once: line is the whole command.
func (m *memo) words(line string) []string {
	if !m.split {
		m.line, m.split = wordsIn(line), true
	}
	return m.line
}

// frozen matches a word from a command line against the frozen set, allowing
// for the spellings the same file arrives under: as written, with a ./ in
// front, or as an absolute path; and, byName, as the name a frozen path ends
// in. The freeze is folded once, not for every word.
func (m *memo) frozen(word string, frozen []string, byName bool) (string, bool) {
	word = strings.TrimPrefix(filepath.ToSlash(clean(word)), "./")
	if word == "" {
		return "", false
	}
	if m.folded == nil {
		m.folded = make([]string, len(frozen))
		for i, f := range frozen {
			m.folded[i] = pathrules.Fold(f)
		}
	}
	word = pathrules.Fold(word)
	for i, folded := range m.folded {
		switch {
		case word == folded,
			strings.HasSuffix(word, "/"+folded),
			byName && strings.HasSuffix(folded, "/"+word):
			return frozen[i], true
		}
	}
	return "", false
}

// spell is spellings, and then the file each spelling is on disk where that is
// another name: through a link, or a Windows short name. Each word is looked up
// once per command.
func (m *memo) spell(s State, dir string, words []string) []string {
	out := spellings(dir, words)
	if s.Resolve == nil {
		return out
	}
	for _, w := range slices.Clip(out) {
		c := strings.TrimPrefix(clean(w), "./")
		if c == "" || strings.HasPrefix(c, "-") {
			continue
		}
		if r := m.resolve(s, c); r != "" && r != c {
			out = append(out, r)
		}
	}
	return out
}

func (m *memo) resolve(s State, c string) string {
	r, ok := m.resolved[c]
	if !ok {
		r = s.Resolve(c)
		m.resolved[c] = r
	}
	return r
}

// shared is what Shared says of a word, asked once per command. A word the
// shell has yet to expand is somewhere this cannot know.
func (m *memo) shared(s State, word string) []Landing {
	if s.Shared == nil {
		return nil
	}
	c := clean(word)
	if c == "" || strings.HasPrefix(c, "-") || shellDirectory(c) || strings.ContainsAny(c, "$%") {
		return nil
	}
	l, ok := m.landed[c]
	if !ok {
		l = s.Shared(c)
		m.landed[c] = l
	}
	return l
}

// outside reports whether a word names a path the lookup puts outside the
// project, where there is no loop state and nothing of the project's to
// protect: the user's own ~/.claude, or a plugin under it, is not the
// project's .claude.
func (m *memo) outside(s State, word string) bool {
	// Not ./~, which is a directory called ~ in the project.
	c := clean(word)
	if s.Resolve == nil || !isAbsolute(c) && !strings.HasPrefix(c, "~") {
		return false
	}
	// Put outside, `rm ~+/.sdlc/state/active` was nothing of the project's.
	if shellDirectory(c) {
		return false
	}
	return m.resolve(s, c) == ""
}

// shellDirectory reports whether a word starts at ~+, the directory the
// command runs in, ~- the one before it, or ~2 one on the stack. The shell
// knows where those are, and the lookup, which knows only the user's home,
// does not.
func shellDirectory(word string) bool {
	rest, ok := strings.CutPrefix(word, "~")
	if !ok {
		return false
	}
	name, _, _ := strings.Cut(rest, "/")
	switch strings.TrimRight(name, "0123456789") {
	case "+", "-":
		return true
	case "":
		return name != ""
	}
	return false
}

// bareDirectory reports whether a word is one of the protected directories
// themselves, with nothing inside named.
func bareDirectory(word string) bool {
	switch pathrules.Fold(strings.TrimPrefix(clean(word), "./")) {
	case ".git", ".sdlc", ".claude":
		return true
	}
	return false
}

func isAbsolute(p string) bool {
	return strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':')
}

// wordsIn pulls every path-shaped run of characters out of a piece of text, so
// that a filename quoted inside a program is still a filename.
func wordsIn(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !inAPath(r) })
}

// inAPath reports whether a rune can appear in a path this cares about.
func inAPath(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '/', r == '_', r == '-':
		return true
	case r >= 0x80:
		// A name spelled with a ligature is still that name to the
		// filesystem, so a letter outside ASCII cannot end a path here.
		return true
	}
	return false
}

// withoutDocuments takes the bodies of here-documents out of a command line.
//
// A document is text handed to a command, and every agent hands its review or
// its plan to sdlc that way. Read as commands, a plan that said "commit with
// `git commit`" was refused by the commit gate, and a review that warned
// against `sdlc unfreeze` was refused as an unfreeze. A body that goes to a
// shell or an interpreter is kept: that text is what runs.
//
// released and the shell pick which reading this is; see readings. It answers
// one text, and two where a here-string marker nothing closes leaves the tail to
// be read in two ways that cannot be put in one text.
func withoutDocuments(command string, powerShell, released bool) []string {
	lines := strings.Split(command, "\n")
	kept := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		kept = append(kept, lines[i])
		// PowerShell's here-string, `@'` at the end of a line to `'@` at the
		// start of one, is a document too. What it goes to is on the closing
		// line, so that is where to look for something that runs it.
		//
		// The markers are taken off either way. Left in, `@'` opened a quoted
		// word that swallowed the body and the command on the closing line, so
		// a body that does run, and `'@ | sdlc approve`, were never read.
		if opener, quote, ok := header(lines[i], powerShell, released); ok {
			end := hereStringEnd(lines, i, quote)
			if end == len(lines) {
				if released {
					continue
				}
				return unclosedHereString(kept, opener, lines[i+1:])
			}
			kept = withoutHereString(kept, lines, i, end, opener, quote)
			i = end
			continue
		}
		i = pastDocuments(lines, i)
	}
	return []string{strings.Join(kept, "\n")}
}

// hereStringEnd is the index of the line that closes the here-string opened
// on lines[i] with quote, or len(lines) where none does.
func hereStringEnd(lines []string, i int, quote string) int {
	end := i + 1
	for end < len(lines) && !strings.HasPrefix(strings.TrimLeft(lines[end], " \t"), quote+"@") {
		end++
	}
	return end
}

// unclosedHereString is the two readings of a command whose here-string
// marker nothing closes: kept are the lines up to and with the one that
// holds the marker, opener that line before the marker, and rest the lines
// after it.
func unclosedHereString(kept []string, opener string, rest []string) []string {
	// Nothing closes it, and the marker left in opened a quote that
	// swallowed every line after it, so none was read. The marker
	// goes, and what follows is read twice: with its quotes taken
	// out, so a quote of its own cannot swallow the next line, and
	// as written, so a `#` or a `<#` that taking the quotes out
	// makes live cannot hide what comes after it. Only the tail
	// -- taking the quotes off everything kept would unbalance
	// the lines before the marker.
	//
	// The two are separate readings and not two halves of one text.
	// One after the other, the `<#` that taking the quotes off `echo
	// '<#'` makes live opened a block comment, which runs to `#>` or
	// to the end of the text (commands.go:183), and it swallowed the
	// copy written to expose the command; so did the `${` of `echo
	// '${'`, which runs to its `}`. Apart, neither can reach the
	// other, whatever a later arm of the lexer reads that way.
	//
	// The scan ends here: carried on, a thousand lines each ending
	// in an unclosed marker would append the rest again for every
	// one of them. That also leaves a `<<` document after an
	// unclosed marker read as commands, which refuses more than it
	// need, never less.
	kept[len(kept)-1] = opener
	before := strings.Join(kept, "\n") + "\n"
	tail := strings.Join(rest, "\n")
	bare := strings.ReplaceAll(strings.ReplaceAll(tail, "'", ""), `"`, "")
	return []string{before + bare, before + tail}
}

// withoutHereString is kept with a here-string taken out of it: one that
// opens on lines[i], the last line kept, and closes on lines[end]. The
// markers go, and the body with them unless something runs it.
func withoutHereString(kept, lines []string, i, end int, opener, quote string) []string {
	closer := strings.TrimPrefix(strings.TrimLeft(lines[end], " \t"), quote+"@")
	kept[len(kept)-1] = opener
	// A here-string is usually put in a variable and used on a later
	// line, so a later line that uses the variable is where it may be
	// run: `$s = @'...'@` and then `Invoke-Expression $s`. Only such a
	// line: a commit message in $msg was commands whenever the script
	// also ran `python -m pytest`.
	rest := []string{opener, closer}
	if name, _, ok := strings.Cut(opener, "="); ok && strings.HasPrefix(strings.TrimSpace(name), "$") {
		name = strings.ToLower(strings.TrimSpace(name))
		for _, later := range lines[end+1:] {
			if strings.Contains(strings.ToLower(later), name) {
				rest = append(rest, later)
			}
		}
	}
	if runsAScript(strings.Join(rest, "\n")) {
		kept = append(kept, lines[i+1:end]...)
	}
	return append(kept, closer)
}

// pastDocuments is the index of the last line of the here-documents that
// lines[i] opens, whose bodies are then left out; or i itself where it opens
// none, or hands them to something that runs them.
func pastDocuments(lines []string, i int) int {
	delimiters := documentDelimiters(lines[i])
	if len(delimiters) == 0 || runsAScript(lines[i]) {
		return i
	}
	for _, d := range delimiters {
		for i+1 < len(lines) {
			i++
			// `<<-` lets the closing line be indented with tabs.
			if strings.TrimLeft(lines[i], "\t") == d {
				break
			}
		}
	}
	return i
}

// documentDelimiters lists the words that end the here-documents one line
// opens, in order, with their quoting taken off.
func documentDelimiters(line string) []string {
	var out []string
	rest := line
	for {
		i := strings.Index(rest, "<<")
		if i < 0 {
			return out
		}
		rest = rest[i+2:]
		// A `<<<` here-string has no body, and names no delimiter here: the
		// word below stops at its third `<` before it starts.
		word := strings.TrimLeft(strings.TrimPrefix(rest, "-"), " \t")
		if end := strings.IndexAny(word, " \t;&|<>()"); end >= 0 {
			word = word[:end]
		}
		if word = strings.NewReplacer(`'`, "", `"`, "", `\`, "").Replace(word); word != "" {
			out = append(out, word)
		}
	}
}

// hereString reports whether a line opens a PowerShell here-string, with the
// line before the marker and the quote that closes it.
//
// Left as it was, so the released reading calls what the released tool called;
// header is the strict test beside it.
func hereString(line string) (opener, quote string, ok bool) {
	trimmed := strings.TrimRight(line, " \t\r")
	for _, q := range []string{"'", `"`} {
		if before, ok0 := strings.CutSuffix(trimmed, "@"+q); ok0 {
			return before, q, true
		}
	}
	return "", "", false
}

// header reports whether a line opens a here-string in the shell that will run
// the command, with the line before the marker and the quote that closes it.
//
// Bash has no here-string: there `@'` is an `@` and an ordinary quote, and a
// command wrapped in two of them ran while the classifier read the whole of it
// as a document. PowerShell has one only where the marker starts a token, which
// startsAToken settles. Under released neither test applies and any line ending
// in the marker opens a document, in either shell, as it did before.
func header(line string, powerShell, released bool) (opener, quote string, ok bool) {
	opener, quote, ok = hereString(line)
	if released {
		return opener, quote, ok
	}
	if !ok || !powerShell || !startsAToken(line) {
		return "", "", false
	}
	return opener, quote, true
}

// startsAToken reports whether the `@` of the marker a line ends in begins a
// token, which is the only place PowerShell reads a here-string header.
//
// The last two characters are all the released test looks at, and five line
// endings are not a header to PowerShell. Four are settled here: the closing
// quote of an ordinary string, a marker inside a string, one inside a comment,
// and one glued to a bare word. The fifth, a marker whose quote a backtick
// escapes, is withoutEscapes's, which has taken it out before this is asked.
//
// The line is read for the lexer's quotes and for the comment opensAComment
// has -- a second definition of the lexer's and not that one, as the comment
// there says -- and no further. The lexer reads a line at commands.go:163-194;
// there is no arm here for the backtick, for `$(`, for `${` or for the block
// comment it opens at `<#` (:183). A marker after one of those is still taken
// for a header, and what follows it is still skipped as a document. Nothing
// before this line is read either: a string or a comment opened further up is
// not read yet, so such a marker is still taken for a header here, as it was
// before.
func startsAToken(line string) bool {
	trimmed := strings.TrimRight(line, " \t\r")
	at := len(trimmed) - 2
	var inSingle, inDouble bool
	for i := range at {
		switch c := trimmed[i]; {
		case inSingle:
			inSingle = c != '\''
		case inDouble:
			inDouble = c != '"'
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case opensAComment(trimmed, i):
			return false
		}
	}
	if inSingle || inDouble {
		return false
	}
	// What may stand in front of the marker. Deliberately short: a character
	// left out only means the released reading answers for that line, where a
	// character wrongly let in would make a false header of a bare word.
	return at == 0 || strings.IndexByte(" \t=;|&({,", trimmed[at-1]) >= 0
}

// opensAComment reports whether the byte at i begins a comment, which a `#` does
// only where a token starts. Taken anywhere, `echo a#b @'` stopped being a
// header and the body its closing line pipes to iex was hidden behind the raw
// marker.
//
// A token starts after a line break or a space, and after the characters
// startsAToken already takes a marker to start after: read as white space only,
// `echo x;# @'` was not a comment, its apostrophe was counted as a delimiter,
// and withoutEscapes deleted a real quote further down.
//
// This is a second definition of the rule the lexer keeps in `started`
// (commands.go:192), not a reading of that one, and measured the two disagree
// both ways. The set below opens a comment after `,` and `=` where the lexer
// does not, so `echo a,# @'` and `$a =# @'` are declined as headers and
// the raw marker's quote swallows the tail: they are NOT closed, though `;#`,
// `|#`, `&#`, `(#` and `{#` are. It does not open one after `)` or `}` where
// the lexer does (endCommand clears `started`), so `echo (1)# @'` is taken for
// a header and its body skipped as a document.
//
// Matching the sets is not the fix, and not one line: adding `)` and `}` here
// gains refusals and gives some up. `echo ${x}# @'` over a commit is refused
// now and would stop being refused, because the lexer's `${` arm leaves a
// token started, so to it the `#` is no comment, and a header declined leaves
// the raw marker whose quote then swallows the tail. Over one corpus of 43,800
// shapes it turned 438 from allowed to refused and 144 the other way. The
// monotone fix is to give a declined header what the unclosed branch in
// withoutDocuments already does -- drop the marker and read the tail twice --
// so that declining one can never hide what follows. That is a change of its
// own.
func opensAComment(text string, i int) bool {
	return text[i] == '#' && (i == 0 || strings.IndexByte(" \t\r\n=;|&({,", text[i-1]) >= 0)
}

// withoutEscapes takes PowerShell's escape character out of a command, leaving
// what it escaped.
//
// A backtick escapes the character after it, so a quote behind one is a
// character of the word and not a delimiter: deleting the backtick alone left a
// live quote, and it swallowed the command on the next line. The two are read
// together, so a doubled backtick cannot reach the quote after it.
//
// Whether a backtick escapes at all turns on what the quotes before it opened,
// so the quotes counted are the ones that are delimiters. A quote inside a
// comment or inside a double-quoted string is a character like any other:
// counted, the apostrophe of a line `# @'` inverted every quote after it, and a
// backtick further down then deleted a delimiter that was real, which left the
// commit after it inside a quoted word and hid it.
func withoutEscapes(command string) string {
	if strings.IndexByte(command, '`') < 0 {
		return command
	}
	var b strings.Builder
	b.Grow(len(command))
	var inSingle, inDouble, inComment bool
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case inComment:
			// Nothing in a comment runs and nothing in one is escaped.
			inComment = c != '\n'
		case inSingle:
			// Inside a single-quoted string PowerShell escapes nothing, so there
			// the backtick goes and the quote after it still closes the string:
			// taking that quote off paired the quotes around a live command and
			// hid it.
			inSingle = c != '\''
		case c == '`':
			if i++; i < len(command) && strings.IndexByte("'\"`", command[i]) < 0 {
				b.WriteByte(command[i])
			}
			continue
		case c == '\'' && !inDouble:
			inSingle = true
		case c == '"':
			inDouble = !inDouble
		case !inDouble && opensAComment(command, i):
			inComment = true
		}
		if c != '`' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// runsAScript reports whether a command on this line could run the text it is
// handed, so that a here-document going to it is a program and not a document.
// The programs are what count, read with their quoting: every word on the line
// counted, so a review whose note said "the error source is lost" handed its
// document to `source`, and a table row in it that said `git commit` was a
// commit.
func runsAScript(line string) bool {
	for _, words := range commandsIn(line, false) {
		for len(words) > 0 && isAssignment(words[0]) {
			words = words[1:]
		}
		run := program(words)
		if run.shell || run.piped {
			return true
		}
		if len(run.words) == 0 {
			continue
		}
		switch base(run.words[0]) {
		case "fish", "eval", "source", ".", "iex", "invoke-expression",
			"python", "python3", "perl", "ruby", "node", "deno", "bun", "php":
			return true
		}
		if namesAShell(run.words) {
			return true
		}
	}
	return false
}

// namesAShell reports whether a word after the first of a command could be a
// shell it runs.
func namesAShell(words []string) bool {
	// Behind a wrapper program does not know, the shell is still a word of
	// its own: `firejail sh <<EOF`, `firejail /bin/sh <<EOF`. Only a word
	// that could be the program counts, not a flag's value, as in
	// `shellcheck -s bash`, and not a relative path: the security
	// reviewer's note "hooks exec via /bin/sh" is one word ending in sh,
	// and it ran the review as commands, as `tee completions/zsh` did.
	for i := 1; i < len(words); i++ {
		w := words[i]
		if strings.HasPrefix(words[i-1], "-") || strings.Contains(w, "/") && !isAbsolute(w) {
			continue
		}
		switch base(w) {
		case "sh", "bash", "zsh", "dash", "ksh", "fish", "pwsh", "powershell":
			return true
		}
	}
	return false
}

// segments splits a command line into the pieces that run on their own. It does
// not understand quoting well enough to be a shell, and does not need to: a
// separator hidden inside quotes makes one segment out of two, which can only
// make this notice more, never less.
func segments(command string) []string {
	return splitSegments(command, false)
}

// pathText is a command as the path rules split it. They split it by its text,
// not by the shell's grammar, so a `(`, `)` or `;` that is an argument, quoted
// or escaped as find's are, ended the command there: in `find . \( -name active
// \) -delete` the -delete was a command of its own, and nothing was deleted, as
// read. A line continued with a backslash is one line. PowerShell escapes with
// a backtick, which Inspect has already taken out.
func pathText(text string, powerShell bool) string {
	if !powerShell {
		text = escapedSeparators.Replace(text)
	} else {
		text = locationCalls.ReplaceAllString(text, "${1}$$PWD")
	}
	return glued(shortMagic(text))
}

// locationCalls are PowerShell's names for where it is, in parentheses, which
// split as the path rules split a command were a word `(Get-Location` and a
// command `)`: `Remove-Item -Recurse (Get-Location)`. `$(Get-Location)` is
// glued's.
var locationCalls = regexp.MustCompile(`(?i)(^|[^$])\((?:get-location|gl|pwd)\)(?:\.path)?`)

// shortMagic is a command with git's long pathspec magic written the short way,
// which has no parenthesis for the command to be split at: `git stash push --
// ':(exclude)build' src` was a stash of `:(exclude` alone, and src a command of
// its own. Only exclude is kept, as `!`; `:(top)` left as `:` is the top, as `:/`
// is.
func shortMagic(text string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, ":(")
		if i < 0 {
			break
		}
		j := strings.IndexByte(text[i:], ')')
		if j < 0 {
			break
		}
		magic := text[i+2 : i+j]
		if strings.Trim(magic, "abcdefghijklmnopqrstuvwxyz,:=_-") != "" {
			// Not magic: what is between the parentheses stays as it is.
			b.WriteString(text[:i+2])
			text = text[i+2:]
			continue
		}
		exclude := false
		for name := range strings.SplitSeq(magic, ",") {
			exclude = exclude || name == "exclude"
		}
		b.WriteString(text[:i+1])
		if exclude {
			b.WriteByte('!')
		}
		text = text[i+j+1:]
	}
	b.WriteString(text)
	return b.String()
}

// glued is a command with each substitution taken out of the word it is written
// in, and read after the command as a subshell of its own. Split where it
// stands, `rm $(pwd)/.sdlc/state/active` was a command called `rm` and one
// called `/.sdlc/state/active`. At the start of a word it leaves `$SUB`, which is somewhere, not the root of the disk; inside a word,
// nothing. PowerShell's backticks are gone by now, so there only `$(` is one.
func glued(text string) string {
	return gluedIn(text, false)
}

// gluedIn is glued, and, when inWords is set, leaves `$SUB` for a substitution
// standing inside a word as well as at the start of one. The find expression
// reader sets it, so that a substitution partway through a word still reads as
// a word the shell fills in rather than vanishing; the path rules leave it as
// nothing, which is what they have always done.
func gluedIn(text string, inWords bool) string {
	var b strings.Builder
	var inner []string
	for i := 0; i < len(text); {
		body, end := substitutionAt(text, i)
		if end < 0 {
			b.WriteByte(text[i])
			i++
			continue
		}
		if i == 0 || startsAWord(text[i-1]) || isQuote(text[i-1]) && (i == 1 || startsAWord(text[i-2])) {
			// `pwd` is where the command is, which the shell calls $PWD.
			if hereIs(body) {
				b.WriteString("$PWD")
			} else {
				b.WriteString("$SUB")
			}
		} else if inWords {
			b.WriteString("$SUB")
		}
		inner = append(inner, gluedIn(body, inWords))
		i = end
	}
	for _, in := range inner {
		b.WriteString("\n(" + in + ")")
	}
	return b.String()
}

// hereIs reports whether a substitution's command prints where the command is:
// `pwd`, `pwd -L` or `pwd -P`, and PowerShell's Get-Location and its aliases.
func hereIs(body string) bool {
	switch strings.ToLower(strings.Join(strings.Fields(body), " ")) {
	case "pwd", "pwd -l", "pwd -p", "get-location", "gl", "(get-location).path", "$pwd", "$pwd.path":
		return true
	}
	return false
}

// substitutionAt is the command inside a `$(...)` or a backquoted one that
// starts at i, and where the substitution ends; -1 when none starts there or it
// is never closed.
func substitutionAt(text string, i int) (string, int) {
	switch {
	case strings.HasPrefix(text[i:], "$("):
		depth := 0
		for j := i + 1; j < len(text); j++ {
			switch text[j] {
			case '(':
				depth++
			case ')':
				if depth--; depth == 0 {
					return text[i+2 : j], j + 1
				}
			}
		}
	case text[i] == '`':
		if j := strings.IndexByte(text[i+1:], '`'); j >= 0 {
			return text[i+1 : i+1+j], i + j + 2
		}
	}
	return "", -1
}

// startsAWord reports whether what follows c is the start of a word. An option's
// value glued to its name, as in `of=$(pwd)/x`, needs nothing of its own: the
// whole word is a spelling, and the path in it is found where it ends.
func startsAWord(c byte) bool {
	return strings.IndexByte(" \t\r\n;&|()<>", c) >= 0
}

func isQuote(c byte) bool {
	return c == '"' || c == '\''
}

// escapedSeparators are the characters splitSegments ends a command at, as an
// argument. A `(` is not one of them.
var escapedSeparators = strings.NewReplacer(
	"\\\r\n", "", "\\\n", "",
	`\)`, " ", `\;`, " ", `\|`, " ", `\&`, " ",
	`')'`, " ", `';'`, " ", `'|'`, " ", `'&'`, " ",
	`")"`, " ", `";"`, " ", `"|"`, " ", `"&"`, " ",
)

// findSeparators is escapedSeparators' sibling: it leaves each hidden separator
// as a marker instead of a space, so the find expression reader sees where a
// group or an -exec command ends. A closing parenthesis becomes markClose, an
// -exec terminator markSemi, and `|` and `&` their own markers. An escaped one
// is a word of its own; a quoted one keeps its quotes, because this replacer
// does not know whether they are quotes to the shell that reads them, and the
// reader's quote tracker has to see them to say.
// findSpelling, not pathText, uses it; see findSpelling.
var findSeparators = strings.NewReplacer(
	"\\\r\n", "", "\\\n", "",
	`\)`, " \x1c ", `\;`, " \x1d ", `\|`, " \x1e ", `\&`, " \x1f ",
	`')'`, "'\x1c'", `';'`, "'\x1d'", `'|'`, "'\x1e'", `'&'`, "'\x1f'",
	`")"`, "\"\x1c\"", `";"`, "\"\x1d\"", `"|"`, "\"\x1e\"", `"&"`, "\"\x1f\"",
)

// findSpelling is pathText's sibling for the find expression reader: the same
// pipeline, so that its text is cut into the commands pathText's is, but with
// each separator the shell hid from find left as a marker (findSeparators) and
// a substitution inside a word left as `$SUB` (gluedIn). A raw marker byte in
// the command is replaced by U+FFFD first, so a marker can only come from the
// replacer and never from the command. PowerShell has no escaped separators,
// so there this is pathText.
//
// pathText and findSpelling change together: a change to one not made to the
// other shows as the two texts not lining up command for command, which the
// reader answers with "cannot follow" for every find in the command.
func findSpelling(text string, powerShell bool) string {
	if powerShell {
		return pathText(text, powerShell)
	}
	text = strings.Map(func(r rune) rune {
		if r >= markClose && r <= markAmp {
			return '\uFFFD'
		}
		return r
	}, text)
	return gluedIn(shortMagic(findSeparators.Replace(text)), true)
}

// markedSegments is segments with where each subshell opens and closes left on
// the front of the segment after it: `(` for a subshell or a `$(`, `)` for its
// end, and a backtick for either end of a backtick substitution.
func markedSegments(command string) []string {
	return splitSegments(command, true)
}

// subshellMarks takes markedSegments' marks off the front of a segment, and
// counts the subshells they open and close before it. ticked is whether a
// backtick substitution is open.
func subshellMarks(marked string, ticked *bool) (segment string, opens, closes int) {
	for {
		marked = strings.TrimLeft(marked, " \t")
		if marked == "" {
			return "", opens, closes
		}
		switch c := marked[0]; {
		case c == '(' || c == '`' && !*ticked:
			*ticked = *ticked || c == '`'
			opens++
		case c == ')' || c == '`':
			*ticked = *ticked && c != '`'
			if opens > 0 {
				opens--
			} else {
				closes++
			}
		default:
			return marked, opens, closes
		}
		marked = marked[1:]
	}
}

func splitSegments(command string, marked bool) []string {
	sub, tick, end := "\n", "\n", "\n"
	if marked {
		sub, tick, end = "\n(", "\n`", "\n)"
	}
	// `>|` is a redirect that overwrites, not a pipe, and read as a pipe it
	// left the file it writes as a command of its own. `>&` and `&>` are
	// redirects too, and are taken as one before a lone `&` is.
	//
	// A lone `&` ends a command as `;` does: the shell's background operator,
	// and PowerShell's call operator in front of one. Neither split, `true &
	// git commit` was a command called `true`, and `& git commit` one called
	// `&`, and the commit gate knew neither.
	replacer := strings.NewReplacer(
		">|", ">", ">&", ">", "&>", ">",
		"&&", "\n", "||", "\n", ";", "\n", "|", "\n", "&", "\n", "`", tick, "$(", sub, ")", end,
	)
	var out []string
	for line := range strings.SplitSeq(replacer.Replace(command), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// parse splits one segment into its words, the targets of its redirections, and
// the names of any environment assignments in front of the command.
func parse(segment string) (words, redirects, assigns []string) {
	fields := spacedFields(segment)
	leading := true
	for i := 0; i < len(fields); i++ {
		f := unquote(fields[i].text)
		switch {
		case f == ">":
			if i+1 < len(fields) {
				redirects = append(redirects, unquote(fields[i+1].text))
				i++
			}
		case f == "<":
			i++
		case isAssignment(f) && (leading || exporting(words)):
			name, _, _ := strings.Cut(f, "=")
			assigns = append(assigns, name)
		default:
			leading = false
			words = append(words, f)
		}
	}
	return words, redirects, assigns
}

// A field is one word of a segment as spacedFields splits it, and where in the
// segment it starts.
type field struct {
	text string
	at   int
}

// spacedFields splits a segment at white space, with each redirect operator
// (`>>`, `>`, `<`) a field of its own, as `>` or `<`. A descriptor number
// written against the operator is part of the redirect and no word of the
// command: a run of digits that starts a word and is followed at once by the
// operator is dropped, wherever on the line the redirect stands.
//
// Quotes are not followed, here as in the rest of parse: a `>` inside a quoted
// word is read as a redirect all the same.
func spacedFields(segment string) []field {
	var out []field
	start, afterSpace, skip := -1, true, false
	flush := func(end int) {
		if start >= 0 {
			out = append(out, field{segment[start:end], start})
			start = -1
		}
	}
	for i, r := range segment {
		switch {
		case skip:
			// The second `>` of `>>`, which is one operator.
			skip = false
		case r == '>' || r == '<':
			if start >= 0 && afterSpace && strings.Trim(segment[start:i], "0123456789") == "" {
				start = -1
			}
			flush(i)
			out = append(out, field{string(r), i})
			skip = r == '>' && strings.HasPrefix(segment[i+1:], ">")
			afterSpace = false
		case unicode.IsSpace(r):
			flush(i)
			afterSpace = true
		case start < 0:
			start = i
		}
	}
	flush(len(segment))
	return out
}

func isAssignment(f string) bool {
	name, _, ok := strings.Cut(f, "=")
	if !ok || name == "" {
		return false
	}
	for _, r := range name {
		if !nameRune(r) {
			return false
		}
	}
	return true
}

// nameRune is what a shell allows in an environment variable's name.
func nameRune(r rune) bool {
	return r == '_' ||
		r >= 'A' && r <= 'Z' ||
		r >= 'a' && r <= 'z' ||
		r >= '0' && r <= '9'
}

// exporting reports whether the words so far are a command that takes
// NAME=VALUE arguments, so that `export SDLC_ENFORCE=0` is read the same way as
// `SDLC_ENFORCE=0 something`.
func exporting(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch base(words[0]) {
	case "export", "set", "declare", "typeset", "env", "setenv":
		return true
	}
	return false
}

// isGit reports whether a command runs git, or hub, which hands git its words.
func isGit(words []string) bool {
	return len(words) > 0 && (base(words[0]) == "git" || base(words[0]) == "hub")
}

func hasWord(words []string, want string) bool {
	return slices.Contains(words, want)
}

// loopState reports whether a word names something the loop owns.
//
// It is matched by shape rather than by a list of exact paths, so that it
// covers every story rather than only the one being worked on, and so that an
// absolute path, a "./" prefix or a Windows separator lands in the same place.
// A scratch file inside a story's directory is not loop state: the file-writing
// rules already allow those, and a shell rule that disagreed with them would be
// a rule nobody could follow.
func loopState(word string) (string, bool) {
	// Folded, as the file-writing rules are: `.SDLC/state/active` and
	// `.sdlc/ﬆate/active` are the same file on macOS and Windows, and every
	// comparison below is against a lower-case name.
	p := pathrules.Fold(strings.TrimPrefix(clean(word), "./"))
	if p == "" {
		return "", false
	}
	for _, own := range []string{loopStateDir, loopConfigFile, ".sdlc/claude-progress.json"} {
		if at(p, own) {
			return own, true
		}
	}
	// The directory itself, taken wholesale. Every path inside it was covered
	// and this was not, so `rm -rf .sdlc/state` was refused while `rm -rf
	// .sdlc` -- which destroys the same thing and more -- went through.
	if p == ".sdlc" || strings.HasSuffix(p, "/.sdlc") {
		return ".sdlc", true
	}
	rest, inStories := cutAt(p, ".sdlc/stories")
	if !inStories {
		return "", false
	}
	// .sdlc/stories itself, or a whole story directory, taken wholesale.
	if rest == "" || !strings.Contains(rest, "/") {
		return p, true
	}
	_, tail, _ := strings.Cut(rest, "/")
	if strings.HasPrefix(tail, "reviews/") || tail == "reviews" {
		return p, true
	}
	if tail == model.RecordFile {
		return p, true
	}
	if _, ok := model.ArtifactByFile(tail); ok {
		return p, true
	}
	return "", false
}

// protectedShellPaths are the human-owned paths the file-writing rules protect
// and loopState does not cover. Between the two, the shell refuses every path
// in policy.ProtectedPaths, and a test holds it to that: `Write` to CLAUDE.md was
// refused and `echo > CLAUDE.md` was not, and a rewritten contract, or a
// settings file that turns the hooks off, takes every gate after it with it.
var protectedShellPaths = []string{".git", ".claude", "CLAUDE.md"}

// protectedPath reports whether a word names one of them, or the backlog,
// folded as the filesystem folds it.
func protectedPath(word, backlog string) (string, bool) {
	p := pathrules.Fold(strings.TrimPrefix(clean(word), "./"))
	if p == "" {
		return "", false
	}
	for _, own := range protectedShellPaths {
		if at(p, pathrules.Fold(own)) {
			return own, true
		}
	}
	if backlog != "" && at(p, pathrules.Fold(backlog)) {
		return backlog, true
	}
	return "", false
}

// at reports whether p is the named path or something inside it, wherever the
// named path sits in p.
func at(p, want string) bool {
	_, ok := cutAt(p, want)
	return ok
}

// cutAt finds want as a whole path segment run inside p and returns what
// follows it, without a leading slash.
func cutAt(p, want string) (string, bool) {
	switch {
	case p == want:
		return "", true
	case strings.HasPrefix(p, want+"/"):
		return strings.TrimPrefix(p, want+"/"), true
	}
	if i := strings.Index(p, "/"+want); i >= 0 {
		rest := p[i+1+len(want):]
		if rest == "" {
			return "", true
		}
		if after, ok := strings.CutPrefix(rest, "/"); ok {
			return after, true
		}
	}
	return "", false
}

func clean(word string) string {
	p := strings.ReplaceAll(unquote(word), "\\", "/")
	// A leading `\\` is a network path, and `\\?\` or `\\.\` a Windows device
	// path. Collapsed, `\\?\C:\proj` would name a directory on the current drive,
	// and the project's own files in it would pass for files outside it.
	head := ""
	if rest, ok := strings.CutPrefix(p, "//"); ok {
		head, p = "//", rest
	}
	// A doubled separator, or a `.` between two, is the same path to every
	// filesystem: `.sdlc//config.json` and `.sdlc/./state/active` are the loop's
	// own files. Compared as they were spelled, they matched no rule.
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	for strings.Contains(p, "/./") {
		p = strings.ReplaceAll(p, "/./", "/")
	}
	return head + strings.TrimSuffix(p, "/")
}

// unquote is a word as the shell hands it over: without its quotes, wherever
// they are in it. Only the ends were taken off, so `'.sdlc'/state/active` and
// `.sd"lc"/state/active` were no path the rules knew. `$'...'` is quoted too.
func unquote(f string) string {
	return quotes.Replace(f)
}

var quotes = strings.NewReplacer(`$'`, "", `$"`, "", `"`, "", "'", "")

// base is the command a word runs, folded: on macOS and Windows `RM` and
// `rm.exe` find the same program as `rm`.
func base(word string) string {
	word = pathrules.Fold(clean(word))
	if i := strings.LastIndex(word, "/"); i >= 0 {
		word = word[i+1:]
	}
	// `(sdlc unfreeze)` runs sdlc, whatever the parenthesis is stuck to.
	return strings.TrimSuffix(strings.TrimLeft(word, "({!"), ".exe")
}
