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
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
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

	// PowerShell is whether the command is PowerShell's, where a backtick is an
	// escape rather than a command substitution: `Remove-Item CLAUDE`.md`
	// removes CLAUDE.md.
	PowerShell bool

	// Backlog is the backlog file, repository-relative and slash-separated, or
	// empty for none. It is protected as CLAUDE.md is, and is not in that list
	// only because the configuration says where it is.
	Backlog string
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
}

// readings is a command read as the released tool reads it and as the shell
// that will run it reads it, which is twice, and three times where a marker
// nothing closes makes the shell's reading two texts of its own. A rule refuses
// the command when any reading refuses it, so this can only add refusals, never
// take one away.
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
	// A reading another has already made is not made again: the two differ only
	// where the command holds a here-string marker or a backtick, and a document
	// handed over as a bash heredoc is one reading for both.
	add := func(texts []string, spelling string) {
		for _, text := range texts {
			if p := (pair{text: text, command: spelling}); !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	add(withoutDocuments(released, powerShell, true), released)
	add(withoutDocuments(shell, powerShell, false), shell)
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
func Inspect(command string, s State) (Finding, bool) {
	m := &memo{checked: map[string]bool{}, resolved: map[string]string{}}
	for _, r := range readings(command, s.PowerShell) {
		if f, ok := inspectReading(r, s, m); ok {
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
		m = &memo{checked: map[string]bool{}, resolved: m.resolved, folded: m.folded}
	}
	return Finding{}, false
}

func inspectReading(r pair, s State, m *memo) (Finding, bool) {
	text, command := r.text, r.command
	for _, segment := range segments(text) {
		if _, _, assigns := parse(segment); len(assigns) > 0 {
			if f, ok := checkEnforcement(assigns); ok {
				return f, true
			}
		}
	}
	// PowerShell sets a variable as a statement of its own, which parse does not
	// read as an assignment: `$env:CLAUDE_CODE_SESSION_ID='x'; sdlc start`.
	if setsSession.MatchString(text) {
		if f, ok := checkEnforcement([]string{"CLAUDE_CODE_SESSION_ID"}); ok {
			return f, true
		}
	}
	if f, ok := checkPrograms(text, s); ok {
		return f, true
	}
	dir, lost := s.Dir, false
	unsure := movesInAScript(text, s.PowerShell)
	// Where the command is, for each subshell open around the segment being
	// read. A `cd` in a subshell moves only that subshell: followed as a move
	// of the shell around it, `cd internal && (cd /tmp && ls) && cp e x_test.go`
	// wrote /tmp's x_test.go, and taken as leaving where the command is
	// unknown, `(cd web && npm run build) && cp config.example.json config.json`
	// wrote a frozen fixture's config.json. PowerShell's parentheses and $( )
	// run in the scope around them, so there a cd moves the command.
	type place struct {
		dir  string
		lost bool
	}
	var outer []place
	ticked := false
	for _, marked := range markedSegments(pathText(text, s.PowerShell)) {
		segment, opens, closes := subshellMarks(marked, &ticked)
		if !s.PowerShell {
			for ; closes > 0 && len(outer) > 0; closes-- {
				dir, lost = outer[len(outer)-1].dir, outer[len(outer)-1].lost
				outer = outer[:len(outer)-1]
			}
			for ; opens > 0; opens-- {
				outer = append(outer, place{dir, lost})
			}
		}
		words, redirects, _ := parse(segment)
		if len(words) == 0 {
			continue
		}
		run := program(words)
		if f, ok := checkLoopState(command, segment, run, redirects, dir, lost, s, m); ok {
			return f, true
		}
		if f, ok := checkFrozenTests(command, segment, run, redirects, dir, lost || unsure, s, m); ok {
			return f, true
		}
		// A script handed to a shell runs in a process of its own, and its cd is
		// movesInAScript's.
		if next, ok := changedDir(dir, run.words); ok && !run.shell {
			// A relative `cd` from somewhere unknown leads somewhere unknown. So
			// does a glob, which names whichever directory it matches; it is kept
			// as written, so that the paths under it are matched as globs too:
			// dropped, `cd .sdl? && rm state/active` was a file at the root.
			dir, lost = next, next == "" || strings.ContainsAny(next, "*?[") || lost && !isAbsolute(next)
		}
	}
	return Finding{}, false
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
	dirs := map[int]string{0: s.Dir}
	// GIT_DIR names the repository whatever the directory, and an assignment to
	// it can be anywhere in the command, or a statement of its own in
	// PowerShell: `$env:GIT_DIR='C:\project\.git'; cd \; git commit`. Asked once:
	// asked for every command in it, a long command took seconds.
	gitDirSet := setsGitDir.MatchString(text)
	// Where each subshell is, as far as the words place it. The refusal reads
	// a directory it cannot place as the project, which errs toward holding a
	// commit back; the recorder cannot err that way, because a commit made in
	// another repository then wrote down this one's HEAD: `cd "$HOME/other" &&
	// git commit` planted an observation a person's next commit here was
	// recorded on. So a directory reached by a guess is lost, and a commit
	// made from it is allowed as before and written down nowhere.
	lost := map[int]bool{0: gitDirSet}
	// CDPATH sends a relative `cd` through the directories it lists. Any
	// mention will do, since the cost of reading one wrongly is an observation.
	cdPath := strings.Contains(strings.ToLower(text), "cdpath")
	for _, r := range runs {
		words := r.words
		dir, ok := dirs[r.group]
		unplaced := lost[r.group]
		for g := r.group; !ok; {
			g = subshells.parent[g]
			dir, ok = dirs[g]
			unplaced = lost[g]
		}
		dirs[r.group], lost[r.group] = dir, unplaced
		if f, ok := checkUnfreeze(words); ok {
			return f, true
		}
		if f, ok := checkApprove(words); ok {
			return f, true
		}
		if f, ok := checkAck(words); ok {
			return f, true
		}
		if f, ok := checkStop(words, s); ok {
			return f, true
		}
		if f, ok := checkReviewer(words, s); ok {
			return f, true
		}
		if f, ok := checkNestedSession(words); ok {
			return f, true
		}
		// A directory of "" is the project.
		if gitDirSet {
			dir = ""
		}
		if f, ok := checkCommit(words, dir, !unplaced, s); ok {
			return f, true
		}
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
			dirs[r.group] = next
			lost[r.group] = next == "" || glob || !placedWords(words[1:]) ||
				(unplaced || cdPath) && !isAbsolute(next)
		} else if len(words) > 0 && (runsHere[base(words[0])] || words[0] == "" || !placedWords(words[:1])) {
			// A script run by this shell can move it anywhere, and so can a
			// command the shell has yet to expand: `source ./env.sh && git
			// commit`, and `eval "$CD"`, which is read as the command `$CD`.
			lost[r.group] = true
		}
	}
	return Finding{}, false
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
// PowerShell's `$env:NAME = `, `Set-Item env:NAME` and
// `[Environment]::SetEnvironmentVariable('NAME', ...)`. `sdlc start` records the
// session it finds there as the one the story holds, so setting it hands the
// story to a session that does not exist. Unlike setsGitDir it is a refusal, so
// a read is not one: `echo $env:CLAUDE_CODE_SESSION_ID` is allowed.
var setsSession = regexp.MustCompile(
	`(?i)\bclaude_code_session_id=|\$env:claude_code_session_id\s*=|` +
		`\b(set-item|new-item|si|ni)\s+(-path\s+)?env:[\\/]?claude_code_session_id\b|` +
		`setenvironmentvariable\(\s*['"]claude_code_session_id['"]`)

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
func HumanDecisions(command string, powerShell bool, storyWaits func(dir string) bool) (Finding, bool) {
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
	type place struct {
		dir  string
		lost bool
	}
	places := map[int]place{0: {dir: "."}}
	for _, r := range runs {
		here, ok := places[r.group]
		for g := r.group; !ok; {
			g = subshells.parent[g]
			here, ok = places[g]
		}
		places[r.group] = here
		if f, ok := checkAck(r.words); ok {
			return f, true
		}
		f, onAStory := checkUnfreeze(r.words)
		if !onAStory {
			f, onAStory = checkApprove(r.words)
		}
		if onAStory && (here.lost || storyWaits(here.dir)) {
			return f, true
		}
		if next, ok := changedDir(here.dir, r.words); ok {
			unread := r.substituted || next == "" || strings.ContainsAny(next, "$`%(){}*?[")
			places[r.group] = place{dir: next, lost: unread || here.lost && !isAbsolute(next)}
		}
	}
	return Finding{}, false
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
	return ok && subcommand(args) == sub
}

// ValueFlags are the flags of sdlc that take the next word as their value. The
// command's own tests hold this to the flags it has.
var ValueFlags = map[string]bool{
	"--file": true, "--gate": true, "--message": true, "--note": true,
	"--reason": true, "--reject": true, "--since": true, "--story": true, "--through": true,
	"--usd": true,
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
func checkCommit(words []string, dir string, placed bool, s State) (Finding, bool) {
	if !isGit(words) {
		return Finding{}, false
	}
	sub, _, _, alias := gitCall(words[1:])
	if !makesACommit(words, sub) || !inThisRepository(words, dir, s) {
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

// checkLoopState stops the shell being the way around every other rule.
func checkLoopState(line, segment string, run invocation, redirects []string, dir string, lost bool, s State, m *memo) (Finding, bool) {
	candidates := append([]string{}, redirects...)
	if changesFiles(run.words) {
		for _, w := range writtenTo(run.words) {
			// A whole directory given to an interpreter is a setting of the
			// program it runs, as in `python3 -m pytest --ignore .sdlc`, not a
			// file for it to write.
			if !interpreters[base(run.words[0])] || !bareDirectory(w) {
				candidates = append(candidates, w)
			}
		}
		if run.piped {
			// `echo .sdlc/state/tests.lock | xargs rm` names the file in
			// another segment altogether.
			candidates = append(candidates, m.words(line)...)
		}
	}
	if runsInlineCode(run.words, segment) {
		// The file a program opens is inside a string, as it is for the
		// freeze below.
		candidates = append(candidates, m.words(line)...)
	}
	if len(run.words) > 0 && base(run.words[0]) == "find" && (hasWord(run.words[1:], "-delete") || runsAWriter(run.words[1:])) {
		// What find picks by a name, from a directory that can come to the
		// project's own files.
		roots, names, patterns := findArguments(run.words[1:])
		var from []string
		for _, r := range roots {
			if reaches(r, dir, s, m) {
				from = append(from, r)
			}
		}
		if len(from) > 0 && len(names)+len(patterns) > 0 {
			shapes := append(protectedShapes(s.Backlog), stateFiles...)
			candidates = append(candidates, foundBy(from, names, patterns, shapes)...)
		}
	}
	// What a command takes away is gone as much as what it writes: `git clean
	// -fdx .sdlc` writes nothing, and empties the loop's record.
	candidates = append(candidates, takenAway(run.words)...)
	// Taking the project's own directory away, or throwing away what its work
	// tree holds that is not committed, takes the loop's record with it.
	for _, w := range takenAway(run.words) {
		if path.Join(dir, clean(w)) == "." && !lost {
			return recordFinding(".sdlc/state"), true
		}
	}
	if discardsTheWorkTree(run.words) && inThisRepository(run.words, dir, s) {
		return recordFinding(".sdlc/state"), true
	}
	spelled := withGlobs(m.spell(s, dir, candidates), func() []string { return protectedShapes(s.Backlog) })
	for _, c := range spelled {
		if !m.first("loop state", c) || m.outside(s, c) {
			continue
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
		if hit, ok := loopState(c); ok && hit == ".sdlc/config.json" {
			return Finding{
				Rule: "loop-state-through-the-tool",
				Reason: hit + " is the loop's configuration, and a shell command is the one " +
					"way around every rule that protects it",
				Route: "configuration is changed by hand, by the person running the session, " +
					"outside a running iteration; say which setting is wrong and stop",
			}, true
		} else if ok {
			return recordFinding(hit), true
		}
	}
	return Finding{}, false
}

// checkFrozenTests is the freeze, applied to the shell.
//
// Every other rule the freeze has is enforced against the file tools, and a
// shell command went straight past them: `Write` to `x_test.go` was refused as
// a frozen acceptance test, and `echo cheat > x_test.go` was allowed. One
// redirect was the whole way round the hinge the loop turns on.
func checkFrozenTests(line, segment string, run invocation, redirects []string, dir string, lost bool, s State, m *memo) (Finding, bool) {
	if len(s.Frozen) == 0 && s.IsTest == nil && s.NewTest == nil && s.ImplementerTest == nil {
		return Finding{}, false
	}
	// A bare name is the end of a frozen path only where the directory it is
	// read from is not known: after a `cd` this cannot follow, and for the
	// names find, xargs, `git -C` and a program's own code work with. Taken
	// that way everywhere, a fixture's config.json made `cp config.example.json
	// config.json` at the root a write to a frozen test.
	inline := runsInlineCode(run.words, segment)
	own := runsAProgramOfItsOwn(run.words)
	byName := lost || inline || run.piped
	if len(run.words) > 0 {
		switch base(run.words[0]) {
		case "find":
			byName = true
		case "git":
			_, moved := gitCommand(run.words[1:])
			byName = byName || moved != ""
		}
	}
	candidates := append([]string{}, redirects...)
	if inline || own {
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
		plain := plainArguments(segment)
		if tookItsProgramElsewhere(run.words) {
			// Nothing on the line says which of the files is data, so none of
			// them is left out.
			plain = nil
		}
		for _, w := range m.words(line) {
			if !plain[w] {
				candidates = append(candidates, w)
			}
		}
	}
	var removed []string
	if changesAFile(run.words) {
		if removes(run.words) {
			removed = writtenTo(run.words)
		} else {
			candidates = append(candidates, writtenTo(run.words)...)
		}
		if run.piped {
			candidates = append(candidates, m.words(line)...)
		}
	}
	rule := "freeze"
	if byName {
		rule = "freeze by name"
	}
	if len(run.words) > 0 && base(run.words[0]) == "find" && (hasWord(run.words[1:], "-delete") || runsAWriter(run.words[1:])) && len(s.Frozen) > 0 {
		// A name find is given with a glob in it, wherever it starts, as the
		// freeze reads find's names. Not one that is all wildcards: from
		// anywhere, that is every file, and `find build -name '*' -delete`
		// was a frozen test.
		roots, names, patterns := findArguments(run.words[1:])
		candidates = append(candidates, foundBy(roots, literals(names), literals(patterns), frozenShapes(s.Frozen))...)
	}
	// A directory taken away takes the frozen tests in it along: `rm -rf
	// internal/calc` removed internal/calc/add_test.go without naming it.
	for _, c := range m.spell(s, dir, takenAway(run.words)) {
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
		if !lost {
			if frozen, ok := globHolding(path.Join(dir, clean(w)), s.Frozen); ok {
				return frozenFinding(frozen), true
			}
		}
	}
	shapes := func() []string { return frozenShapes(s.Frozen) }
	added := withGlobs(m.spell(s, dir, candidates), shapes)
	spelled := slices.Concat(added, withGlobs(m.spell(s, dir, removed), shapes))
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
		frozen, ok := m.frozen(c, s.Frozen, byName)
		w := strings.TrimPrefix(clean(c), "./")
		if !ok && w != "" && s.IsTest != nil && s.IsTest(w) {
			frozen, ok = w, true
		}
		if !ok && w != "" && !taken && s.NewTest != nil && s.NewTest(w) {
			return Finding{
				Rule: "no-new-test-after-the-freeze",
				Reason: w + " would be a new test file added after the freeze, which is the " +
					"freeze with extra steps: a test written now can be written to pass",
				Route: "say which case is missing and stop: the frozen files are frozen too, " +
					"and the person running the session can unfreeze and freeze again so the " +
					"new test is covered",
			}, true
		}
		if ok {
			return frozenFinding(frozen), true
		}
		if w != "" && s.ImplementerTest != nil && s.ImplementerTest(w) {
			return Finding{
				Rule: "implementer-does-not-write-tests",
				Reason: w + " is a test file, and the implementer does not write tests -- through " +
					"the shell any more than through the file tools",
				Route: "make the existing tests pass; if they are wrong, say so rather than " +
					"changing them",
			}, true
		}
	}
	return Finding{}, false
}

func frozenFinding(frozen string) Finding {
	return Finding{
		Rule: "frozen-test-through-the-tool",
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
		Rule: "loop-state-through-the-tool",
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
		return hasWord(words[1:], "-delete") || len(printedTo(words[1:])) > 0
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
		return hasWord(words[1:], "-delete") || runsAWriter(words[1:]) || len(printedTo(words[1:])) > 0
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

// readers are the programs find runs that only read what they are given.
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
			if strings.HasPrefix(a, "-m") && code == "c" {
				return false
			}
			if strings.ContainsAny(a[1:], code) && code != "" {
				return true
			}
		default:
			return name == "deno" && a == "eval"
		}
	}
	return false
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
			if abbreviates(name, "--file") ||
				awk && (abbreviates(name, "--source") || abbreviates(name, "--exec")) {
				return true
			}
		case strings.HasPrefix(a, "-"):
			for _, r := range a[1:] {
				if strings.ContainsRune(inverts, r) {
					return true
				}
				// What follows one of these is that option's value and not more
				// flags. Read as flags, `awk -vf=1 '{print}' x_test.go` was a
				// program taken from a file and an ordinary read was refused.
				if strings.ContainsRune(valued, r) {
					break
				}
			}
		}
	}
	return false
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
		switch base(first) {
		case "if", "then", "else", "elif", "do", "while", "until", "time":
			words = words[1:]
		case "xargs":
			run.piped = true
			words = skipOptions(words[1:])
		case "env":
			words = skipOptions(words[1:])
			for len(words) > 0 && isAssignment(words[0]) {
				words = words[1:]
			}
		case "command", "builtin", "exec", "nohup", "nice", "stdbuf", "caffeinate":
			words = skipOptions(words[1:])
		case "sudo", "doas":
			words = skipValued(words[1:], "-u", "-g", "-h", "-p", "-C", "-D", "-R", "-r", "-t", "-T", "-U")
		case "sh", "bash", "zsh", "dash", "ksh", "pwsh", "powershell":
			run.shell = true
			words = skipOptions(words[1:])
		case "ssh":
			// What follows the host is a command line for the shell at the
			// other end, and localhost is this machine.
			run.shell = true
			words = skipValued(words[1:], "-B", "-b", "-c", "-D", "-E", "-e", "-F", "-I", "-i", "-J",
				"-L", "-l", "-m", "-O", "-o", "-P", "-p", "-Q", "-R", "-S", "-W", "-w")
			if len(words) > 0 {
				words = words[1:]
			}
		case "timeout":
			// skipOptions took the duration for a number an option was given,
			// and then the command for the duration: `timeout 60 git commit`
			// was a command called `commit`.
			words = skipValued(words[1:], "-s", "-k", "--signal", "--kill-after")
			if len(words) > 0 {
				words = words[1:] // the duration
			}
		case "direnv":
			if len(words) < 3 || words[1] != "exec" {
				run.words = append([]string{first}, words[1:]...)
				return run
			}
			words = words[3:]
		case "cmd":
			// cmd's switches start with a slash: `cmd /c del file`, `cmd /s /c`.
			// Git Bash turns an argument that starts with one slash into a
			// Windows path, so there they are written with two, `cmd //c`,
			// which was read as the command and let whatever followed through.
			run.shell = true
			words = words[1:]
			for len(words) > 0 && strings.HasPrefix(words[0], "/") &&
				!strings.Contains(strings.TrimPrefix(words[0][1:], "/"), "/") {
				words = words[1:]
			}
		default:
			run.words = append([]string{first}, words[1:]...)
			return run
		}
	}
	return run
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
	checked  map[string]bool   // a rule and a spelling it has found nothing in
	resolved map[string]string // what Resolve said of a word
	folded   []string          // the freeze, folded
	line     []string          // the words of the whole command
	split    bool
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
	for _, w := range out[:len(out):len(out)] {
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
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(strings.TrimLeft(lines[end], " \t"), quote+"@") {
				end++
			}
			if end == len(lines) {
				if released {
					continue
				}
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
				tail := strings.Join(lines[i+1:], "\n")
				bare := strings.ReplaceAll(strings.ReplaceAll(tail, "'", ""), `"`, "")
				return []string{before + bare, before + tail}
			}
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
			kept = append(kept, closer)
			i = end
			continue
		}
		delimiters := documentDelimiters(lines[i])
		if len(delimiters) == 0 || runsAScript(lines[i]) {
			continue
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
	}
	return []string{strings.Join(kept, "\n")}
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
		if strings.HasSuffix(trimmed, "@"+q) {
			return strings.TrimSuffix(trimmed, "@"+q), q, true
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
	for i := 0; i < at; i++ {
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
		// Behind a wrapper program does not know, the shell is still a word of
		// its own: `firejail sh <<EOF`, `firejail /bin/sh <<EOF`. Only a word
		// that could be the program counts, not a flag's value, as in
		// `shellcheck -s bash`, and not a relative path: the security
		// reviewer's note "hooks exec via /bin/sh" is one word ending in sh,
		// and it ran the review as commands, as `tee completions/zsh` did.
		for i := 1; i < len(run.words); i++ {
			w := run.words[i]
			if strings.HasPrefix(run.words[i-1], "-") || strings.Contains(w, "/") && !isAbsolute(w) {
				continue
			}
			switch base(w) {
			case "sh", "bash", "zsh", "dash", "ksh", "fish", "pwsh", "powershell":
				return true
			}
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
	}
	return glued(shortMagic(text))
}

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
		for _, name := range strings.Split(magic, ",") {
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
			b.WriteString("$SUB")
		}
		inner = append(inner, glued(body))
		i = end
	}
	for _, in := range inner {
		b.WriteString("\n(" + in + ")")
	}
	return b.String()
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
	for _, line := range strings.Split(replacer.Replace(command), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// parse splits one segment into its words, the targets of its redirections, and
// the names of any environment assignments in front of the command.
func parse(segment string) (words, redirects, assigns []string) {
	fields := strings.Fields(strings.NewReplacer(">>", " > ", ">", " > ", "<", " < ").Replace(segment))
	leading := true
	for i := 0; i < len(fields); i++ {
		f := unquote(fields[i])
		switch {
		case f == ">":
			if i+1 < len(fields) {
				redirects = append(redirects, unquote(fields[i+1]))
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
	for _, w := range words {
		if w == want {
			return true
		}
	}
	return false
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
	for _, own := range []string{".sdlc/state", ".sdlc/config.json", ".sdlc/claude-progress.json"} {
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
		if strings.HasPrefix(rest, "/") {
			return strings.TrimPrefix(rest, "/"), true
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
