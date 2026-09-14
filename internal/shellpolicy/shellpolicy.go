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
	// and the interpreters are not here: each can write through its program
	// whatever it is asked to do, which changesFiles counts for the loop's
	// record, and a frozen test is read with them, which changesAFile allows.
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

// Inspect reports the first rule that refuses this command.
//
// The rules do not depend on who is asking. A shell command that rewrites the
// loop's record is wrong from every role, including the one whose record it is,
// for the same reason the file-writing rules refuse it from everyone. Two
// exceptions are only wrong because of who is asking: the implementer creating
// a test file, as the file-writing rules have it, and a review recorded by
// anyone but the reviewer it names.
func Inspect(command string, s State) (Finding, bool) {
	if s.PowerShell {
		command = strings.ReplaceAll(command, "`", "")
	}
	text := withoutDocuments(command)
	for _, segment := range segments(text) {
		if _, _, assigns := parse(segment); len(assigns) > 0 {
			if f, ok := checkEnforcement(assigns); ok {
				return f, true
			}
		}
	}
	if f, ok := checkPrograms(text, s); ok {
		return f, true
	}
	dir, lost := s.Dir, false
	unsure := movesInAScript(text, s.PowerShell)
	m := &memo{checked: map[string]bool{}, resolved: map[string]string{}}
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
	for _, marked := range markedSegments(text) {
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
		if f, ok := checkLoopState(command, segment, run, redirects, dir, s, m); ok {
			return f, true
		}
		if f, ok := checkFrozenTests(command, segment, run, redirects, dir, lost || unsure, s, m); ok {
			return f, true
		}
		// A script handed to a shell runs in a process of its own, and its cd is
		// movesInAScript's.
		if next, ok := changedDir(dir, run.words); ok && !run.shell {
			// A relative `cd` from somewhere unknown leads somewhere unknown.
			dir, lost = next, next == "" || lost && !isAbsolute(next)
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
	for _, r := range runs {
		words := r.words
		dir, ok := dirs[r.group]
		for g := r.group; !ok; {
			g = subshells.parent[g]
			dir, ok = dirs[g]
		}
		dirs[r.group] = dir
		if f, ok := checkUnfreeze(words); ok {
			return f, true
		}
		if f, ok := checkApprove(words); ok {
			return f, true
		}
		if f, ok := checkStop(words, s); ok {
			return f, true
		}
		if f, ok := checkReviewer(words, s); ok {
			return f, true
		}
		// A directory of "" is the project.
		if gitDirSet {
			dir = ""
		}
		if f, ok := checkCommit(words, dir, s); ok {
			return f, true
		}
		if next, ok := changedDir(dir, words); ok {
			dirs[r.group] = next
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

// HumanDecisions reports a command that makes one of the decisions the loop
// keeps for a person: approving work handed over, or lifting the freeze. These
// hold with no story being worked on, because `sdlc escalate` ends the
// iteration and the approval always comes after it -- which is where nothing
// else was being enforced, so no approval was ever refused.
func HumanDecisions(command string, powerShell bool) (Finding, bool) {
	if powerShell {
		command = strings.ReplaceAll(command, "`", "")
	}
	text := withoutDocuments(command)
	if _, subshells := programsAt(text, powerShell); subshells.tooDeep {
		return tooDeep, true
	}
	for _, words := range programsIn(text, powerShell) {
		if f, ok := checkUnfreeze(words); ok {
			return f, true
		}
		if f, ok := checkApprove(words); ok {
			return f, true
		}
	}
	return Finding{}, false
}

// checkEnforcement stops a session turning the loop off from the inside. The
// switch exists for the person who started the session, and a switch an
// assistant can reach is not a control.
func checkEnforcement(assigns []string) (Finding, bool) {
	for _, name := range assigns {
		switch name {
		case "SDLC_ENFORCE", "SDLC_BIN", "CLAUDE_PROJECT_DIR":
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

// runsSubcommand reports whether a command -- one from programsIn -- runs sdlc
// with this subcommand.
func runsSubcommand(words []string, sub string) bool {
	args, ok := sdlcArgs(words)
	return ok && subcommand(args) == sub
}

// ValueFlags are the flags of sdlc that take the next word as their value. The
// command's own tests hold this to the flags it has.
var ValueFlags = map[string]bool{
	"--file": true, "--gate": true, "--message": true, "--note": true,
	"--reason": true, "--reject": true, "--story": true, "--usd": true,
}

// checkCommit puts the commit gate in front of the commit, for a commit in this
// repository: dir is where the command runs, and a commit made in another
// repository -- by `cd`, `git -C`, or a session opened elsewhere -- is not this
// story's to hold back.
func checkCommit(words []string, dir string, s State) (Finding, bool) {
	if !isGit(words) {
		return Finding{}, false
	}
	sub, to := gitCommand(words[1:])
	if !makesACommit(words, sub) {
		return Finding{}, false
	}
	switch {
	case strings.ContainsAny(to, "$%"):
		// `git -C "$OLDPWD"` is somewhere this cannot know, which is not
		// another repository.
		dir = ""
	case isAbsolute(to) || strings.HasPrefix(to, "~"):
		// The lookup reads ~ as the home it names.
		dir = to
	case to != "":
		dir = path.Join(dir, to)
	}
	// Another repository is a directory outside the project. Its .git is
	// asked about rather than the directory, which for the project root
	// itself resolves to nothing, the same as outside: `cd /path/to/project &&
	// git commit`, the usual way to spell it, went past the gate. A directory
	// not followed is "", which is the project.
	repository := path.Join(dir, ".git")
	switch gitDir := gitDirOf(words[1:]); {
	case strings.ContainsAny(gitDir, "$%"):
		// Wherever the command runs, `--git-dir` can name this repository:
		// `cd /tmp && git --git-dir="$PROJECT/.git" commit`.
		repository = ".git"
	case isAbsolute(gitDir) || strings.HasPrefix(gitDir, "~"):
		repository = gitDir
	case gitDir != "":
		repository = path.Join(dir, gitDir)
	}
	if s.Resolve != nil && s.Resolve(repository) == "" {
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
func checkLoopState(line, segment string, run invocation, redirects []string, dir string, s State, m *memo) (Finding, bool) {
	candidates := append([]string{}, redirects...)
	if changesFiles(run.words) {
		for _, w := range run.words[1:] {
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
	for _, c := range m.spell(s, dir, candidates) {
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
			return Finding{
				Rule: "loop-state-through-the-tool",
				Reason: hit + " is the loop's own record, and a shell command is the one " +
					"way around every rule that protects it",
				Route: "the sdlc command owns this: `sdlc artifact write` for a gate's " +
					"documents, `sdlc review add` for a review, `sdlc gate` for an " +
					"outcome; the freeze is lifted by the person running the session",
			}, true
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
	if inline {
		// `python3 -c '...'` is a program, not a list of arguments, and the
		// file it opens is inside a string. There is no parsing this without
		// being an interpreter, so the whole of it is searched instead: a
		// frozen path appearing anywhere in code that runs is enough. The whole
		// command line, because a `;` inside the program splits it into
		// segments, and a here-document puts the program on lines of its own.
		// A plain argument is left out: it is a file the program is given to
		// read, as in `perl -ne 'print' x_test.go`, and reading a frozen test
		// is never refused.
		plain := plainArguments(segment)
		for _, w := range m.words(line) {
			if !plain[w] {
				candidates = append(candidates, w)
			}
		}
	}
	if changesAFile(run.words) {
		candidates = append(candidates, run.words[1:]...)
		if run.piped {
			candidates = append(candidates, m.words(line)...)
		}
	}
	rule := "freeze"
	if byName {
		rule = "freeze by name"
	}
	for _, c := range m.spell(s, dir, candidates) {
		if !m.first(rule, c) {
			continue
		}
		frozen, ok := m.frozen(c, s.Frozen, byName)
		w := strings.TrimPrefix(clean(c), "./")
		if !ok && w != "" && s.IsTest != nil && s.IsTest(w) {
			frozen, ok = w, true
		}
		if !ok && w != "" && s.NewTest != nil && s.NewTest(w) {
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
			return Finding{
				Rule: "frozen-test-through-the-tool",
				Reason: frozen + " is a frozen acceptance test, and a shell command is " +
					"the one way around every rule that protects it",
				Route: "leave it alone -- it was locked by content when the test gate " +
					"passed, and every gate after that is measured against it. If it " +
					"genuinely has to change, say which and why: the person running the " +
					"session lifts the freeze with `sdlc unfreeze --reason ...`, which puts the " +
					"reason on the record",
			}, true
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

// changesAFile reports whether this command, as it is written, exists to
// change a file. One that only reads it is left alone: reading a frozen test is
// how the implementer knows what to implement, and `sed -n 1,20p x_test.go` has
// to go through.
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
		return hasWord(words[1:], "-delete")
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
		// The command find runs is out of sight.
		return hasWord(words[1:], "-delete") || hasWord(words[1:], "-exec") ||
			hasWord(words[1:], "-execdir") || hasWord(words[1:], "-ok") || hasWord(words[1:], "-okdir")
	case "sed", "perl", "awk", "gawk":
		return true
	}
	if interpreters[base(words[0])] {
		return true
	}
	return changesAFile(words)
}

// gitChangesFiles reports whether a git command rewrites or removes the files
// named after it: `git rm`, `git mv`, `git checkout -- path`, `git restore`.
func gitChangesFiles(args []string) bool {
	switch sub, _ := gitCommand(args); sub {
	case "rm", "mv", "checkout", "restore":
		return true
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
	case to == "" || to == "-" || strings.HasPrefix(to, "~") || strings.HasPrefix(to, "$"),
		strings.ContainsAny(to, "*?["):
		// A glob names whichever directory it matches.
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
	// ~+ is the directory the command runs in, ~- the one before it and ~2 one
	// on the stack. The shell knows where those are and the lookup does not: put
	// outside, `rm ~+/.sdlc/state/active` was nothing of the project's.
	if name, _, _ := strings.Cut(c[1:], "/"); c[0] == '~' {
		switch strings.TrimRight(name, "0123456789") {
		case "+", "-":
			return false
		case "":
			if name != "" {
				return false
			}
		}
	}
	return m.resolve(s, c) == ""
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
func withoutDocuments(command string) string {
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
		if opener, quote, ok := hereString(lines[i]); ok {
			end := i + 1
			for end < len(lines) && !strings.HasPrefix(strings.TrimLeft(lines[end], " \t"), quote+"@") {
				end++
			}
			if end == len(lines) {
				continue
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
	return strings.Join(kept, "\n")
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
func hereString(line string) (opener, quote string, ok bool) {
	trimmed := strings.TrimRight(line, " \t\r")
	for _, q := range []string{"'", `"`} {
		if strings.HasSuffix(trimmed, "@"+q) {
			return strings.TrimSuffix(trimmed, "@"+q), q, true
		}
	}
	return "", "", false
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

func unquote(f string) string {
	for _, q := range []string{`"`, "'"} {
		if len(f) >= 2 && strings.HasPrefix(f, q) && strings.HasSuffix(f, q) {
			return f[1 : len(f)-1]
		}
	}
	return strings.Trim(f, `"'`)
}

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
