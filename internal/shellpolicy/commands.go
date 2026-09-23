package shellpolicy

import (
	"path"
	"slices"
	"strings"
)

// commandsIn reads a command line the way a shell runs it, far enough to know
// which commands it runs and with which words: a quoted string is one word
// however many spaces, semicolons or backticks it holds, and a command inside
// `$(...)` or backticks is a command of its own.
//
// segments does not understand quoting, on purpose: for the paths the other
// rules look for, a separator inside quotes can only make them notice more.
// Deciding which program runs is different. Read that way, an escalation whose
// message said "please sdlc approve" after a `;` was an approval, a commit
// message with `sdlc approve` in backticks was one too, and `git log --grep
// commit` was a commit.
//
// A backslash is left in the word, as segments leaves it: in PowerShell and in
// a Windows path it is a separator, not an escape. Only a line continuation, and
// an escaped character inside double quotes in a POSIX shell, are read as the
// shell reads them.
func commandsIn(line string, powerShell bool) [][]string {
	return lex(line, powerShell).commands
}

func lex(line string, powerShell bool) *lexer {
	return lexIn(line, powerShell, newSubshells(), 0)
}

// lexIn reads a line that runs in the subshell numbered group, numbering the
// subshells it opens in t.
func lexIn(line string, powerShell bool, t *subshells, group int) *lexer {
	return lexAt(line, powerShell, t, group, 0)
}

// lexAt is lexIn for a line substituted depth levels inside another.
func lexAt(line string, powerShell bool, t *subshells, group, depth int) *lexer {
	l := &lexer{powerShell: powerShell, subshells: t, open: []int{group}, depth: depth}
	l.read([]rune(line))
	l.endCommand()
	return l
}

// maxNesting is how deep substitutions are read. Nothing a person writes comes
// near it.
const maxNesting = 32

// subshells numbers the subshells a line opens, from 1, and records which each
// was opened in and which are scripts handed to another shell. 0 is the shell
// the line starts in.
type subshells struct {
	parent []int
	script []bool
	// tooDeep is set when the line nests substitutions past maxNesting, where
	// what they run is not read.
	tooDeep bool
}

func newSubshells() *subshells {
	return &subshells{parent: []int{0}, script: []bool{false}}
}

func (t *subshells) start(in int) int {
	t.parent = append(t.parent, in)
	t.script = append(t.script, false)
	return len(t.parent) - 1
}

// scriptOf is the innermost script handed to a shell that a subshell is part
// of, or 0 for none.
func (t *subshells) scriptOf(g int) int {
	for g != 0 && !t.script[g] {
		g = t.parent[g]
	}
	return g
}

type lexer struct {
	powerShell bool
	commands   [][]string
	groups     []int  // the subshell each command runs in
	writes     []bool // each command sends its output to a file
	redirected bool   // the command being read does
	// substituted is set for each command a word of which had a substitution
	// taken out of it, and substituting for the command being read: `cd $(pwd)/b`
	// is read as `cd /b`, which is not where it goes.
	substituted  []bool
	substituting bool
	subshells    *subshells
	open         []int // the subshells open around the command being read, innermost last
	words        []string
	word         strings.Builder
	started      bool // a word has begun, even an empty quoted one
	target       bool // the word being read is where a redirection goes
	depth        int  // the substitutions this line is inside
}

func (l *lexer) endWord() {
	if !l.started {
		return
	}
	if l.target {
		l.target = false
	} else {
		l.words = append(l.words, l.word.String())
	}
	l.word.Reset()
	l.started = false
}

func (l *lexer) endCommand() {
	l.endWord()
	l.target = false
	if len(l.words) > 0 {
		l.commands = append(l.commands, l.words)
		l.groups = append(l.groups, l.group())
		l.writes = append(l.writes, l.redirected)
		l.substituted = append(l.substituted, l.substituting)
		l.words = nil
	}
	l.redirected = false
	l.substituting = false
}

func (l *lexer) group() int {
	return l.open[len(l.open)-1]
}

// substitute reads the text inside `$(...)` or backticks as the commands it
// runs, which happen whatever the word around them is for.
func (l *lexer) substitute(inner []rune) {
	l.started = true
	l.substituting = true
	if l.depth >= maxNesting {
		// Each level is read again from where it opens, so a line nested
		// thousands deep took longer than Claude Code gives the hook, and a
		// command the hook has not answered in time runs unchecked.
		l.subshells.tooDeep = true
		return
	}
	sub := lexAt(string(inner), l.powerShell, l.subshells, l.subshells.start(l.group()), l.depth+1)
	l.commands = append(l.commands, sub.commands...)
	l.groups = append(l.groups, sub.groups...)
	l.writes = append(l.writes, sub.writes...)
	l.substituted = append(l.substituted, sub.substituted...)
}

func (l *lexer) read(rs []rune) {
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\\' && i+1 < len(rs) && rs[i+1] == '\n':
			i++
			l.endWord()
		case r == '\\' && !l.powerShell && i+1 < len(rs) && strings.ContainsRune(";&|", rs[i+1]):
			// An escaped separator is a character of the word, as find's `\;`
			// is. Read as a separator, it ended find before a second -exec.
			i++
			l.word.WriteRune(rs[i])
			l.started = true
		case r == '\'':
			end := find(rs, i+1, '\'')
			l.word.WriteString(string(rs[i+1 : end]))
			l.started = true
			i = end
		case r == '"':
			i = l.quoted(rs, i+1)
		case r == '`':
			end := find(rs, i+1, '`')
			l.substitute(rs[i+1 : end])
			i = end
		case r == '$' && i+1 < len(rs) && rs[i+1] == '(':
			end := closing(rs, i+2)
			l.substitute(rs[i+2 : end])
			i = end
		case r == '$' && i+1 < len(rs) && rs[i+1] == '{':
			end := find(rs, i+2, '}')
			l.word.WriteString(string(rs[i:min(end+1, len(rs))]))
			l.started = true
			i = end
		case l.powerShell && r == '<' && i+1 < len(rs) && rs[i+1] == '#':
			// PowerShell's block comment. Read as a redirect and then a line
			// comment, `<# note #> sdlc approve` hid the command after it.
			l.endWord()
			j := i + 2
			for j+1 < len(rs) && (rs[j] != '#' || rs[j+1] != '>') {
				j++
			}
			i = j + 1
		case r == '#' && !l.started:
			// A comment runs to the end of the line, and nothing in it runs.
			i = find(rs, i, '\n') - 1
		case r == '>' || r == '<' || (r == '&' && i+1 < len(rs) && rs[i+1] == '>'):
			i = l.redirect(rs, i)
		case r == ' ' || r == '\t' || r == '\r':
			l.endWord()
		case r == '{' && i+1 < len(rs) && rs[i+1] == '}':
			// `{}` is a word, the one find puts each file in. Read as a group,
			// `-exec true {} +` ended find before the -exec after it.
			i++
			l.word.WriteString("{}")
			l.started = true
		case r == '(':
			l.endCommand()
			l.open = append(l.open, l.subshells.start(l.group()))
		case r == ')':
			l.endCommand()
			if len(l.open) > 1 {
				l.open = l.open[:len(l.open)-1]
			}
		case strings.ContainsRune("\n;&|{}", r):
			l.endCommand()
		default:
			l.word.WriteRune(r)
			l.started = true
		}
	}
}

// quoted reads a double-quoted string from just after its opening quote, and
// returns where it closes. A command substituted inside it still runs.
func (l *lexer) quoted(rs []rune, i int) int {
	l.started = true
	for ; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '"':
			return i
		case r == '\\' && !l.powerShell && i+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[i+1]):
			i++
			l.word.WriteRune(rs[i])
		case r == '`':
			end := find(rs, i+1, '`')
			l.substitute(rs[i+1 : end])
			i = end
		case r == '$' && i+1 < len(rs) && rs[i+1] == '(':
			end := closing(rs, i+2)
			l.substitute(rs[i+2 : end])
			i = end
		default:
			l.word.WriteRune(r)
		}
	}
	return i
}

// redirect reads a redirection from its operator and returns where the
// operator ends. The word after it is where the output goes, not an argument,
// and a descriptor number stuck to the front, as in `2>`, is part of it.
func (l *lexer) redirect(rs []rune, i int) int {
	if l.started && strings.Trim(l.word.String(), "0123456789") == "" {
		l.word.Reset()
		l.started = false
	}
	l.endWord()
	for i < len(rs) && strings.ContainsRune("<>&|-", rs[i]) {
		l.redirected = l.redirected || rs[i] == '>'
		i++
	}
	// What follows is the target: a file, or the descriptor in `>&2`.
	l.target = true
	return i - 1
}

// find is the index of the next r at or after i, or the end of rs.
func find(rs []rune, i int, r rune) int {
	for ; i < len(rs); i++ {
		if rs[i] == r {
			return i
		}
	}
	return len(rs)
}

// closing is the index of the parenthesis that closes one opened just before i,
// past quoted strings and nested parentheses, or the end of rs.
func closing(rs []rune, i int) int {
	depth := 1
	for ; i < len(rs); i++ {
		switch rs[i] {
		case '\'':
			i = find(rs, i+1, '\'')
		case '"':
			i = find(rs, i+1, '"')
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(rs)
}

// ran is a command a line runs, and the subshell it runs in: 0 for the shell
// the line starts in, and a number of its own for each subshell, substitution
// and script handed to another shell, whose `cd` moves only that one.
type ran struct {
	words     []string
	group     int
	redirects bool // it sends its output to a file
	// substituted is set when a word of the command, or of the one that handed
	// it to a shell, had a substitution taken out, so its words are not all it
	// was given.
	substituted bool
}

// programsAt is the commands a line runs, with what only wraps them taken off,
// the subshell each runs in, and which subshell opened each. A shell handed a
// string, `cmd /c`, PowerShell's -Command and `eval` run that string, so it is
// read in turn for the commands it runs.
func programsAt(line string, powerShell bool) ([]ran, *subshells) {
	t := newSubshells()
	return programsWithin(line, powerShell, t, 0, 0, false), t
}

func programsWithin(line string, powerShell bool, t *subshells, group, depth int, substituted bool) []ran {
	var out []ran
	l := lexIn(line, powerShell, t, group)
	for i, words := range l.commands {
		for len(words) > 0 && isAssignment(words[0]) {
			words = words[1:]
		}
		run := program(words)
		if len(run.words) == 0 {
			continue
		}
		script, in, cut := run.words, l.groups[i], substituted || l.substituted[i]
		switch {
		case run.shell:
			in = t.start(in)
			t.script[in] = true
		case base(script[0]) == "eval":
			// eval runs its string in this shell, so a `cd` in it moves this one.
			script = script[1:]
		default:
			out = append(out, ran{run.words, in, l.writes[i], cut})
			if base(run.words[0]) == "find" {
				// Each -exec runs a command:
				// `find . -exec true \; -exec git commit -m x \;` runs git.
				for j, w := range run.words {
					if !findRuns(w) {
						continue
					}
					if exec := program(run.words[j+1:]); len(exec.words) > 0 {
						out = append(out, ran{exec.words, in, false, cut})
					}
				}
			}
			continue
		}
		if depth < 4 {
			out = append(out, programsWithin(strings.Join(script, " "), powerShell, t, in, depth+1, cut)...)
		}
	}
	return out
}

// sdlcArgs is what follows sdlc in a command that runs it, however it is
// reached: on PATH, by path, as sdlc.exe, at a version through npx, or with
// `go run ./cmd/sdlc`.
func sdlcArgs(words []string) ([]string, bool) {
	if len(words) == 0 {
		return nil, false
	}
	if isSdlc(words[0]) {
		return words[1:], true
	}
	switch base(words[0]) {
	case "npx", "pnpx", "bunx", "pnpm", "yarn", "npm", "go":
		for i, w := range words[1:] {
			if isSdlc(w) {
				return words[i+2:], true
			}
		}
	}
	// Behind a wrapper program does not know -- `ssh host`, `watch -n 1`,
	// `Start-Process` -- sdlc is still a word of its own. A word after a flag
	// is the flag's value, as in `docker compose -p sdlc stop`.
	switch base(words[0]) {
	case "echo", "printf", "print", "write-host", "write-output", "ls", "dir", "cat", "type",
		"grep", "rg", "head", "tail", "less", "more", "man", "which", "whereis", "file", "stat", "wc":
		// A command that prints or reads its words only mentions sdlc:
		// `echo next: sdlc stop` is not a stop.
		return nil, false
	}
	for i := 1; i < len(words); i++ {
		if isSdlc(words[i]) && !strings.HasPrefix(words[i-1], "-") {
			return words[i+1:], true
		}
	}
	return nil, false
}

// findRuns reports whether a find flag runs the command after it.
func findRuns(flag string) bool {
	return flag == "-exec" || flag == "-execdir" || flag == "-ok" || flag == "-okdir"
}

func isSdlc(word string) bool {
	name := base(word)
	return name == "sdlc" || strings.HasPrefix(name, "sdlc@")
}

// subcommand is the first word that is not a flag or a flag's value. Read as
// the subcommand, a value put before it -- `sdlc --reason x unfreeze` -- hid the
// unfreeze behind `x`.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			if ValueFlags[args[i]] {
				i++
			}
			continue
		}
		return args[i]
	}
	return ""
}

// gitCommand is the subcommand a git command runs, past git's own options, and
// the directory a -C in front of it moves it to. The word `commit` anywhere
// after git was a commit, so `git log --grep commit` met the commit gate.
func gitCommand(args []string) (sub, dir string) {
	sub, dir, _, _ = gitCall(args)
	return sub, dir
}

// gitCall is gitCommand with the arguments written after the subcommand, and,
// when the subcommand is an inline alias, the words the alias expands to: in
// `git -c alias.ci="commit --dry-run" ci` the flag is inside a word that does
// not start with a dash.
func gitCall(args []string) (sub, dir string, rest, alias []string) {
	aliases := map[string]string{}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-C" && i+1 < len(args):
			i++
			if to := clean(args[i]); isAbsolute(to) {
				dir = to
			} else {
				dir = path.Join(dir, to)
			}
		case a == "-c" && i+1 < len(args):
			i++
			if name, value, ok := strings.Cut(args[i], "="); ok {
				if name, ok := strings.CutPrefix(strings.ToLower(name), "alias."); ok {
					aliases[name] = value
				}
			}
		case a == "--git-dir" || a == "--work-tree" || a == "--namespace" ||
			a == "--config-env" || a == "--attr-source":
			// Read as the subcommand, the value hid it: `git --attr-source HEAD
			// commit` was a command called HEAD.
			i++
		case strings.HasPrefix(a, "-"):
		default:
			// `git -c alias.ci=commit ci` runs commit.
			if value, ok := aliases[strings.ToLower(a)]; ok {
				sub, alias := aliasedCommand(value)
				return sub, dir, args[i+1:], alias
			}
			return a, dir, args[i+1:], nil
		}
	}
	return "", dir, nil, nil
}

// inThisRepository reports whether a git command run in dir works on the
// project's own repository, wherever its -C and --git-dir send it.
func inThisRepository(words []string, dir string, s State) bool {
	_, to := gitCommand(words[1:])
	switch {
	case strings.ContainsAny(to, "$%") || shellDirectory(to):
		// `git -C "$OLDPWD"`, or `git -C ~-`, is somewhere this cannot know,
		// which is not another repository.
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
	case strings.ContainsAny(gitDir, "$%") || shellDirectory(gitDir):
		// Wherever the command runs, `--git-dir` can name this repository:
		// `cd /tmp && git --git-dir="$PROJECT/.git" commit`.
		repository = ".git"
	case isAbsolute(gitDir) || strings.HasPrefix(gitDir, "~"):
		repository = gitDir
	case gitDir != "":
		repository = path.Join(dir, gitDir)
	}
	return s.Resolve == nil || s.Resolve(repository) != ""
}

// ownRepository reports whether a git command run in dir, a directory the words
// placed it in, commits to the project's own repository: not one that -C or
// --git-dir name in words the shell has yet to expand, and not one nested
// inside the project. inThisRepository answers "might it be this one", which
// is the refusal's question; this answers "is it", which is the recorder's.
func ownRepository(words []string, dir string, s State) bool {
	if !placedGit(words[1:]) {
		return false
	}
	resolve := func(p string) string {
		if s.Resolve == nil {
			return path.Clean(p)
		}
		return s.Resolve(p)
	}
	_, to := gitCommand(words[1:])
	switch {
	case isAbsolute(to) || strings.HasPrefix(to, "~"):
		dir = to
	case to != "":
		dir = path.Join(dir, to)
	}
	if gitDir := gitDirOf(words[1:]); gitDir != "" {
		if !isAbsolute(gitDir) && !strings.HasPrefix(gitDir, "~") {
			gitDir = path.Join(dir, gitDir)
		}
		return resolve(gitDir) == ".git"
	}
	// Asked through its .git, as inThisRepository asks, because the root
	// itself resolves to nothing.
	repository := resolve(path.Join(dir, ".git"))
	return repository != "" && (s.Nested == nil || !s.Nested(path.Dir(repository)))
}

// placedGit reports whether every -C and --git-dir in front of git's
// subcommand is spelled out in the words, rather than expanded by the shell
// into a directory they do not show: `git -C "$OTHER"` and `git
// --git-dir="$G"` are anywhere, and inThisRepository reads them as the
// project, which is right for the refusal and wrong for the recorder.
func placedGit(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case (a == "-C" || a == "--git-dir") && i+1 < len(args):
			i++
			// Empty is where a substitution was: `git -C "$(mktemp -d)"`.
			if args[i] == "" || !placedWords(args[i:i+1]) {
				return false
			}
		case strings.HasPrefix(a, "--git-dir="):
			if a == "--git-dir=" || !placedWords([]string{strings.TrimPrefix(a, "--git-dir=")}) {
				return false
			}
		case a == "-c" || a == "--work-tree" || a == "--namespace" ||
			a == "--config-env" || a == "--attr-source":
			i++
		case !strings.HasPrefix(a, "-"):
			return true
		}
	}
	return true
}

// aliasedCommand is the git subcommand an alias runs, and the words it runs it
// with: `commit -a`, or `!git commit -a` handed to the shell.
func aliasedCommand(value string) (string, []string) {
	fields := strings.Fields(value)
	if len(fields) > 0 && base(fields[0]) == "git" {
		fields = fields[1:]
	}
	sub, _ := gitCommand(fields)
	return sub, fields
}

// makesCommits are the git subcommands that put a commit on the branch.
var makesCommits = map[string]bool{
	"commit": true, "commit-tree": true, "merge": true, "cherry-pick": true, "revert": true,
	"am": true, "rebase": true,
}

// gitValueFlags take the next word as their value, so a flag's name there is
// not the flag: `git merge -m --no-commit wip` commits, with that message.
var gitValueFlags = map[string]bool{
	"-m": true, "--message": true, "-F": true, "--file": true, "-s": true, "--strategy": true,
	"-X": true, "--strategy-option": true, "--into-name": true, "--cleanup": true, "--mainline": true,
}

// leavesItUncommitted reports whether a merge, cherry-pick or revert brings its
// change into the working tree and stops there, as `git cherry-pick -n` and
// `git merge --squash` do. The change is committed later by `git commit`, which
// meets the gate itself.
func leavesItUncommitted(words []string, sub string) bool {
	uncommitted := false
	for i := 1; i < len(words); i++ {
		switch w := words[i]; {
		case gitValueFlags[words[i-1]]:
		case w == "--commit":
			return false
		case w == "--no-commit" && (sub == "merge" || sub == "cherry-pick" || sub == "revert"),
			w == "--squash" && sub == "merge",
			w == "-n" && (sub == "cherry-pick" || sub == "revert"):
			uncommitted = true
		}
	}
	return uncommitted
}

// makesACommit reports whether a git command, running sub, makes a commit.
// Held to `git commit` alone, a branch committed in a worktree elsewhere went
// onto trunk past the gate with `git merge`, and so did `git commit-tree`.
func makesACommit(words []string, sub string) bool {
	if sub == "config" {
		// An alias set now is a commit later: `git config alias.ci commit`.
		for i := 1; i+1 < len(words); i++ {
			if strings.HasPrefix(strings.ToLower(words[i]), "alias.") {
				sub, _ := aliasedCommand(words[i+1])
				return makesCommits[sub]
			}
		}
		return false
	}
	if !makesCommits[sub] || leavesItUncommitted(words, sub) {
		return false
	}
	// Backing out of one part-way commits nothing. Only the flag on its own is
	// that: in `git merge -m --abort wip` it is the message.
	n := len(words)
	return words[n-2] != sub || words[n-1] != "--abort" && words[n-1] != "--quit"
}

// commitsNow reports whether a git command that makesACommit calls a commit
// makes one now, rather than later or never. The refusal asks whether a command
// could put a commit on the branch; the loop writes down the HEAD a commit is
// made on, and asks whether this one will. An observation planted by a command
// that commits nothing says the session was about to commit on this HEAD, so
// the next commit made on it -- a person's, from their own terminal -- is
// recorded as the story's. So it is makesACommit minus: an alias, which is a
// commit somebody makes later; commit-tree, which writes an object and moves no
// ref; and a flag in the subcommand's own row below that makes it commit
// nothing. The flags are looked for in args, git's words after `git`, and again
// in an inline alias's expansion. Reading git's own options before the
// subcommand with the subcommand's row can only drop an observation: a value
// there read as a flag is at worst an excluded one, and a word there wrongly
// read as taking the next one can take no further than the subcommand's name,
// which is never a flag, so no flag written after it is skipped.
//
// Each row is read the way git reads the subcommand's own options, measured
// against git 2.55.0 rather than read off a manual page, because the rows
// differ: -s is --strategy for merge and --signoff for commit, cherry-pick and
// revert, and a rule reading merge's table for every subcommand stopped
// looking one word early in four of them. A word is considered only when the
// word before it did not take it as a value. `--<name>`, with any `=<value>`
// cut off, fires when name is a non-empty prefix of an excluded long option:
// git takes an unambiguous abbreviation, and an ambiguous one exits 129 and
// commits nothing, so the prefix over-matches for free. `-<letters>` is walked
// left to right: an excluded letter fires, and a letter whose value is the rest
// of the word stops the walk -- and takes the next word too when it is last and
// that letter takes one. A long option that takes a value takes the next word
// when it has no `=`, and, for commit and merge, when it is abbreviated:
// `git commit --mess --dry-run` commits, with the flag as the message. Cherry-pick and revert take no
// abbreviation of any long option, so there only the whole word takes a value.
// No option of commit or merge has a whole name that is a proper prefix of one
// that takes a value, so an abbreviation is never another option spelled out.
//
// --commit and --no-commit (and -n) are read in order, because git takes the
// last: `git merge --commit --no-commit side`, `git revert --commit
// --no-commit HEAD` and `git cherry-pick --commit -n side` all exit 0 and make
// no commit, and `git cherry-pick -n --commit side` makes one. --squash does not
// give way to --commit, because git refuses the pair.
//
// The two directions of error are not the same size. Reading a word as a value
// when git reads it as a flag records a command that commits nothing, which is
// the defect this exists to fix; so a word takes the next one only where that
// was measured. Reading a flag where git reads a value loses an observation and
// shortens a story's range; where a row is in doubt it errs that way.
//
// What it was tried against, on git 2.55.0 with a change staged (MOVED means
// HEAD moved, "no" that it did not):
//
//	git commit -m msg                              MOVED      records (the control)
//	git commit -m msg --dry-run / --dry / --dr     no         excluded
//	git commit -m msg --porcelain / --short / --long / --null / -z   no  excluded
//	git commit -m msg --por / --sho / --lo / --nu / --l    no  excluded
//	git commit -m msg --s / --n / --d / --p        rc 129     excluded, ambiguous
//	git commit -h, --help; git commit -sh, -s -h   rc 129     excluded
//	git commit -zm msg, -azm msg                   no         excluded
//	git commit -m msg --dry-run=1                  rc 129     excluded, =1 cut first
//	git commit -m msg --dry-run-x                  rc 129     records: a prefix of nothing
//	git commit -sz -m msg, -m msg -s -z, -nsz -m msg       no  excluded: -s is --signoff
//	git commit -m msg -s --dry-run / --porcelain / --short  no  excluded
//	git commit -s -m msg, -m msg -s                MOVED      records
//	git commit -am chore, -amchore:x, -amz         MOVED      records: the walk stops at m
//	git commit -am --dry-run, -sm --dry-run, -am --porcelain   MOVED  records
//	git commit -au --dry-run, -as --dry-run        no         excluded: -u's value is glued only
//	git commit -m --dry-run, --message --dry-run, -F --dry-run   MOVED  records
//	git commit --mess --dry-run, --m --dry-run     MOVED      records: --m abbreviates --message
//	git commit --cleanup verbatim -m msg           MOVED      records
//	git commit --cl verbatim --dry-run, --reu HEAD --dry-run   no  excluded
//	git commit -m msg --no-dry-run / --status / --quiet / --amend / -- f   MOVED  records
//	git commit -X --dry-run                        rc 129     excluded: -X is not commit's
//	git commit -X ours, --strategy ours, --mainline 1    rc 129    records, as every failing commit
//	git merge --sq side, --squ side                no         excluded
//	git merge --no-com side, --no-c side, --s side rc 129     excluded, ambiguous
//	git merge -sh side                             rc 1       records: h is the strategy
//	git merge --mess --no-commit side, --me --squash side   MOVED  never asked: leavesItUncommitted
//	                                                          reads the flag, so makesACommit says no
//	git merge --mess x --no-commit side, --int x --no-commit side   no  excluded
//	git merge --commit --no-commit side            no         excluded: the last one wins
//	git merge --no-commit --commit side            MOVED      makesACommit's to answer; records
//	git cherry-pick -ne side, git revert -ne HEAD  no         excluded: n is --no-commit
//	git cherry-pick -n --commit side               MOVED      records
//	git cherry-pick --commit -n side               no         excluded
//	git cherry-pick -sh side, git revert -sh HEAD  rc 129     excluded
//	git cherry-pick --no-comm side, --clean strip side   rc 128/129  records or excluded: git
//	                                                          takes no abbreviation here
//	git -c alias.ci="commit --dry-run" ci          no         excluded, from the expansion
//	git -c alias.dr="commit --dry-run" dr -m msg   no         excluded
//	git -c alias.ci="commit -m aliased" ci         MOVED      records
//
// Recorded though they commit nothing, and accepted: a command git refuses
// (--dry-run-x, --DRY-RUN, --cleanup -h, a bad --author, a rejecting hook,
// nothing staged) and one that succeeds having nothing to do (`git merge <an
// ancestor>`, `git rebase <up to date>`, `git am -s` reading an empty mailbox,
// `git rebase --edit-todo` and `--show-current-patch` outside a rebase). No
// reading of the words can tell those from a commit in advance. Excluded
// though they commit, and accepted because it is the honest direction: a
// pathspec that looks like a flag (`git commit -m x -- --dry-run`), a value
// split across an inline alias and the words after it (`git -c
// alias.ci="commit -m" ci --dry-run`), `git merge --no-commit --comm side`
// (--comm is not read as --commit), and --squash undone by --no-squash. One
// that under-excludes and is contrived: an alias's expansion is split on
// spaces, not by a shell, so `alias.x='commit "--dry-run"'` keeps its quotes
// and the flag is not seen. Nor is a flag the shell spells for it: `$'--dry-run'`,
// `--dry-\run` and `$DRY` are recorded, and only the first two could be read.
//
// Which repository the commit is made in is checkCommit's to answer, through
// ownRepository, and not this function's; the refusal guesses the project when
// the words do not say, and the recorder takes no guess. What that was tried
// against, through Inspect with every gate passed, the refusal before the gates
// the same for every one:
//
//	git commit -m x; cd internal && git commit; git -C internal commit     records
//	cd /proj && git commit; git -C /proj commit; --git-dir=/proj/.git   records
//	cd "$O"; cd /proj && git commit (placed again by an absolute cd)     records
//	(cd "$O") && git commit (the move stays in its subshell)            records
//	cd "$HOME/other", pushd "$O", cd ~, cd -, cd, popd, then git commit  nothing written
//	cd /work/proj*, cd a/$X/../.., cd "$HOME" && cd other, then commit  nothing written
//	builtin cd "$O", command cd "$O", sh -c 'cd "$O" && git commit'      nothing written
//	source ./env.sh, . ./env.sh, eval "$CD", CDPATH=/tmp cd other       nothing written
//	GIT_DIR=/tmp/x git commit; export GIT_DIR=/tmp/x; git commit        nothing written
//	git -C "$O", -C ~-, -C ~+, -C "$PWD", -C %USERPROFILE%, -C "$(mktemp -d)"   nothing written
//	git -C 'a/$X/../..', git -C internal -C "$O", git --git-dir="$G"    nothing written
//	cd vendor/lib, git -C vendor/lib/src (a repository nested in this)  nothing written
//
// `git -C "$PWD"` and `git -C ~+` are this repository and lose their
// observation, which leaves the story with no range rather than a wrong one.
func commitsNow(args []string, sub string, alias []string) bool {
	row, ok := commitRows[sub]
	if !ok {
		// config and commit-tree: a commit made later, or no ref moved.
		return false
	}
	return !row.commitsNothing(args) && !row.commitsNothing(alias)
}

// commitRow is one subcommand's options, as far as telling whether it commits
// now needs them.
type commitRow struct {
	// never are the long options that make the command commit nothing, and
	// letters the short ones.
	never   []string
	letters string
	// held and heldLetters make it commit nothing unless a --commit comes
	// after them.
	held        []string
	heldLetters string
	// ends are the letters whose value is the rest of the word, and next those
	// of them that take the next word when nothing is written after them.
	ends, next string
	// valued are the long options that take the next word, abbreviated where
	// abbreviates says git takes an abbreviation.
	valued      []string
	abbreviates bool
}

var commitRows = map[string]commitRow{
	"commit": {
		never: []string{"dry-run", "porcelain", "short", "long", "null", "help"}, letters: "zh",
		ends: "mFcCtuS", next: "mFcCt",
		valued:      []string{"message", "file", "reuse-message", "reedit-message", "template", "cleanup"},
		abbreviates: true,
	},
	"merge": {
		never: []string{"squash", "help"}, letters: "h", held: []string{"no-commit"},
		ends: "mFsSX", next: "mFsX",
		valued:      []string{"message", "file", "strategy", "strategy-option", "into-name", "cleanup"},
		abbreviates: true,
	},
	"cherry-pick": {
		never: []string{"help"}, letters: "h", held: []string{"no-commit"}, heldLetters: "n",
		ends: "mSX", next: "mX", valued: []string{"mainline", "strategy", "strategy-option", "cleanup"},
	},
	"revert": {
		never: []string{"help"}, letters: "h", held: []string{"no-commit"}, heldLetters: "n",
		ends: "mSX", next: "mX", valued: []string{"mainline", "strategy", "strategy-option", "cleanup"},
	},
	"am":     {never: []string{"help"}, letters: "h", ends: "CpS"},
	"rebase": {never: []string{"help"}, letters: "h", ends: "sSX", next: "sX"},
}

// commitsNothing reports whether words hold a flag of this row's that makes the
// command commit nothing.
func (r commitRow) commitsNothing(words []string) bool {
	held := false
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "--commit":
			held = false
		case strings.HasPrefix(w, "--"):
			name, _, valued := strings.Cut(w[2:], "=")
			if name == "" {
				continue
			}
			if prefixOfAny(name, r.never) {
				return true
			}
			if prefixOfAny(name, r.held) {
				held = true
			}
			if !valued && r.takesNext(name) {
				i++
			}
		case strings.HasPrefix(w, "-") && len(w) > 1:
			for j := 1; j < len(w); j++ {
				c := w[j : j+1]
				if strings.Contains(r.letters, c) {
					return true
				}
				if strings.Contains(r.heldLetters, c) {
					held = true
				}
				if strings.Contains(r.ends, c) {
					if j == len(w)-1 && strings.Contains(r.next, c) {
						i++
					}
					break
				}
			}
		}
	}
	return held
}

// takesNext reports whether the long option name takes the next word as its
// value.
func (r commitRow) takesNext(name string) bool {
	if r.abbreviates {
		return prefixOfAny(name, r.valued)
	}
	return slices.Contains(r.valued, name)
}

// prefixOfAny reports whether name is a non-empty prefix of one of options.
func prefixOfAny(name string, options []string) bool {
	for _, o := range options {
		if strings.HasPrefix(o, name) {
			return true
		}
	}
	return false
}

// gitDirOf is the repository a --git-dir in front of git's subcommand names.
func gitDirOf(args []string) string {
	gitDir := ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--git-dir" && i+1 < len(args):
			i++
			gitDir = clean(args[i])
		case strings.HasPrefix(a, "--git-dir="):
			gitDir = clean(strings.TrimPrefix(a, "--git-dir="))
		case a == "-C" || a == "-c" || a == "--work-tree" || a == "--namespace" ||
			a == "--config-env" || a == "--attr-source":
			i++
		case !strings.HasPrefix(a, "-"):
			return gitDir
		}
	}
	return gitDir
}
