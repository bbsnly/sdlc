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
	// Editors that rewrite a file given on the command line. `sed -i` is the
	// most common way an agent changes a file from a shell, and the record it
	// would be changing is the loop's own evidence -- unlike a test, nothing
	// downstream notices afterwards. Named here whether or not the invocation
	// actually asks for in-place editing: a `sed` that only reads names its
	// input too, and refusing a read of the gate record costs nothing.
	"sed": true, "perl": true, "awk": true, "ed": true, "patch": true,
	"python": true, "python3": true, "ruby": true, "node": true,
	// Windows spellings, because Bash on Windows is not always a POSIX shell.
	"del": true, "erase": true, "move": true, "copy": true,
	// PowerShell's cmdlets and their aliases, folded like every other name.
	"remove-item": true, "move-item": true, "copy-item": true, "rename-item": true,
	"new-item": true, "set-content": true, "add-content": true, "clear-content": true,
	"out-file": true, "ri": true, "mi": true, "cpi": true, "rni": true, "ni": true,
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
	dir := s.Dir
	for _, segment := range segments(withoutDocuments(command)) {
		words, redirects, assigns := parse(segment)
		if len(words) == 0 && len(assigns) == 0 {
			continue
		}
		run := program(words)
		if f, ok := checkEnforcement(assigns); ok {
			return f, true
		}
		if f, ok := checkUnfreeze(words); ok {
			return f, true
		}
		if f, ok := checkApprove(words); ok {
			return f, true
		}
		if f, ok := checkReviewer(words, s); ok {
			return f, true
		}
		if f, ok := checkCommit(run.words, s); ok {
			return f, true
		}
		if f, ok := checkLoopState(command, segment, run, redirects, dir); ok {
			return f, true
		}
		if f, ok := checkFrozenTests(command, segment, run, redirects, dir, s); ok {
			return f, true
		}
		if next, ok := changedDir(dir, run.words); ok {
			dir = next
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
				Route: "if a rule is wrong, say which one and why; `sdlc stop` ends the " +
					"iteration and hands the work back",
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
	if !runsSubcommand(words, "review") || !hasWord(words, "add") {
		return "", false
	}
	var positional []string
	seen := false
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case !seen:
			seen = base(w) == "sdlc"
		case strings.HasPrefix(w, "-"):
		default:
			positional = append(positional, w)
		}
	}
	for i := 0; i+1 < len(positional); i++ {
		if r, ok := model.FindReviewer(model.Gate(positional[i]), positional[i+1]); ok {
			return r.Role, true
		}
	}
	return "", false
}

// runsSubcommand reports whether the command runs sdlc with this subcommand,
// however sdlc is reached: on PATH, by path, as sdlc.exe, through npx, or with
// `go run ./cmd/sdlc`. The subcommand is the first word after sdlc that is not
// a flag, so a note that mentions it is not mistaken for it.
func runsSubcommand(words []string, sub string) bool {
	for i, w := range words {
		if base(w) != "sdlc" {
			continue
		}
		for _, next := range words[i+1:] {
			if strings.HasPrefix(next, "-") {
				continue
			}
			return next == sub
		}
		return false
	}
	return false
}

// checkCommit puts the commit gate in front of the commit.
func checkCommit(words []string, s State) (Finding, bool) {
	if !isGit(words) || !hasWord(words[1:], "commit") {
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
		Route: "finish the gates -- `sdlc status` shows where this story stands -- " +
			"or `sdlc stop` to end the iteration and commit as yourself",
	}, true
}

// checkLoopState stops the shell being the way around every other rule.
func checkLoopState(line, segment string, run invocation, redirects []string, dir string) (Finding, bool) {
	candidates := append([]string{}, redirects...)
	if changesFiles(run.words) {
		candidates = append(candidates, run.words[1:]...)
		if run.piped {
			// `echo .sdlc/state/tests.lock | xargs rm` names the file in
			// another segment altogether.
			candidates = append(candidates, wordsIn(line)...)
		}
	}
	if runsInlineCode(run.words, segment) {
		// The file a program opens is inside a string, as it is for the
		// freeze below.
		candidates = append(candidates, wordsIn(line)...)
	}
	for _, c := range spellings(dir, candidates) {
		if hit, ok := protectedPath(c); ok {
			return Finding{
				Rule: "protected-path-through-the-tool",
				Reason: hit + " is protected while a story is being worked on, and a shell " +
					"command is the one way around the rule that protects it",
				Route: "change it by hand outside a running iteration; if a rule in it is " +
					"wrong, stop and say which one rather than editing it",
			}, true
		}
		if hit, ok := loopState(c); ok {
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
func checkFrozenTests(line, segment string, run invocation, redirects []string, dir string, s State) (Finding, bool) {
	if len(s.Frozen) == 0 && s.IsTest == nil && s.NewTest == nil && s.ImplementerTest == nil {
		return Finding{}, false
	}
	candidates := append([]string{}, redirects...)
	if runsInlineCode(run.words, segment) {
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
		for _, w := range wordsIn(line) {
			if !plain[w] {
				candidates = append(candidates, w)
			}
		}
	}
	if changesAFile(run.words) {
		candidates = append(candidates, run.words[1:]...)
		if run.piped {
			candidates = append(candidates, wordsIn(line)...)
		}
	}
	for _, c := range spellings(dir, candidates) {
		frozen, ok := isFrozen(c, s.Frozen)
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

// isFrozen matches a word from a command line against the frozen set, allowing
// for the spellings the same file arrives under: as written, with a ./ in
// front, or as an absolute path.
func isFrozen(word string, frozen []string) (string, bool) {
	word = strings.TrimPrefix(filepath.ToSlash(clean(word)), "./")
	if word == "" {
		return "", false
	}
	word = pathrules.Fold(word)
	for _, f := range frozen {
		folded := pathrules.Fold(f)
		switch {
		case word == folded,
			strings.HasSuffix(word, "/"+folded),
			strings.HasSuffix(folded, "/"+word):
			return f, true
		}
	}
	return "", false
}

// changesAFile reports whether this command, as it is written, exists to
// change a file.
//
// Narrower than the mutating list, and deliberately so. That list names `sed`
// whether or not the invocation edits in place, because refusing a read of the
// loop's record costs nothing. A frozen test is different: reading one is how
// the implementer knows what to implement, and `sed -n 1,20p x_test.go` has to
// go through.
func changesAFile(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch name := base(words[0]); name {
	case "sed", "perl":
		return editsInPlace(words[1:])
	case "awk", "grep":
		// Neither writes where it is pointed; both need a redirect, which is
		// already counted.
		return false
	case "python", "python3", "ruby", "node", "deno", "bun", "php":
		// An interpreter writes through its program, which runsInlineCode
		// reads. A file named after it is a script or its input, and
		// `python3 -m pytest tests/test_x.py` is how the tests run.
		return false
	case "find":
		return hasWord(words[1:], "-delete")
	case "git":
		return gitChangesFiles(words[1:])
	default:
		return mutating[name]
	}
}

// changesFiles is changesAFile for the loop's own record, which is broader for
// the reason the mutating list is: refusing a read of the record costs nothing.
func changesFiles(words []string) bool {
	if len(words) == 0 {
		return false
	}
	switch name := base(words[0]); name {
	case "find":
		return hasWord(words[1:], "-delete") || hasWord(words[1:], "-exec") ||
			hasWord(words[1:], "-execdir") || hasWord(words[1:], "-ok") || hasWord(words[1:], "-okdir")
	case "git":
		return gitChangesFiles(words[1:])
	default:
		return mutating[name]
	}
}

// gitChangesFiles reports whether a git command rewrites or removes the files
// named after it: `git rm`, `git mv`, `git checkout -- path`, `git restore`.
func gitChangesFiles(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-C" || a == "-c":
			i++ // the option's own argument
		case strings.HasPrefix(a, "-"):
		default:
			return a == "rm" || a == "mv" || a == "checkout" || a == "restore"
		}
	}
	return false
}

// editsInPlace reports whether sed or perl was asked to rewrite the files it
// reads: -i on its own, bundled with other flags (`-Ei`, `-pi.bak`), or
// --in-place. Only a flag spelled `-i...` counted, and `sed -Ei` is common.
func editsInPlace(args []string) bool {
	for _, a := range args {
		switch {
		case a == "--in-place" || strings.HasPrefix(a, "--in-place="):
			return true
		case strings.HasPrefix(a, "--") || !strings.HasPrefix(a, "-"):
			continue
		}
		for _, r := range a[1:] {
			if r == 'i' {
				return true
			}
			// What follows one of these is that option's argument, not more
			// flags: `perl -Mstrict`, `sed -e ...`.
			if strings.ContainsRune("efIlMmx", r) {
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
	case "python", "python3":
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
		case "env", "command", "builtin", "exec", "nohup", "sudo", "doas", "nice", "stdbuf",
			"sh", "bash", "zsh", "dash", "ksh", "pwsh", "powershell":
			words = skipOptions(words[1:])
		case "timeout":
			words = skipOptions(words[1:])
			if len(words) > 0 {
				words = words[1:] // the duration
			}
		default:
			run.words = append([]string{first}, words[1:]...)
			return run
		}
	}
	return run
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
// variable -- starts again from the root, which only loses the prefix: every
// word is still read as it was written as well.
func changedDir(dir string, words []string) (string, bool) {
	if len(words) == 0 {
		return dir, false
	}
	switch base(words[0]) {
	case "cd", "pushd", "chdir", "set-location", "sl":
	default:
		return dir, false
	}
	args := skipOptions(words[1:])
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
		if _, value, ok := strings.Cut(w, "="); ok && value != "" {
			named = append(named, value)
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

// runsAScript reports whether anything on this line could run the text it is
// handed, so that a here-document going to it is a program and not a document.
func runsAScript(line string) bool {
	for _, segment := range segments(line) {
		words, _, _ := parse(segment)
		for _, w := range words {
			switch base(w) {
			case "sh", "bash", "zsh", "dash", "ksh", "fish", "pwsh", "powershell", "cmd",
				"eval", "source", ".", "xargs",
				"python", "python3", "perl", "ruby", "node", "deno", "bun", "php":
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
	// `>|` is a redirect that overwrites, not a pipe, and read as a pipe it
	// left the file it writes as a command of its own.
	replacer := strings.NewReplacer(
		">|", ">", "&&", "\n", "||", "\n", ";", "\n", "|", "\n", "`", "\n", "$(", "\n", ")", "\n",
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

func isGit(words []string) bool { return len(words) > 0 && base(words[0]) == "git" }

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

// protectedPath reports whether a word names one of them, folded as the
// filesystem folds it.
func protectedPath(word string) (string, bool) {
	p := pathrules.Fold(strings.TrimPrefix(clean(word), "./"))
	if p == "" {
		return "", false
	}
	for _, own := range protectedShellPaths {
		if at(p, pathrules.Fold(own)) {
			return own, true
		}
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
	return strings.TrimSuffix(strings.ReplaceAll(unquote(word), "\\", "/"), "/")
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
