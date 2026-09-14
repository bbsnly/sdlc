package shellpolicy

import (
	"path"
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
	l := &lexer{powerShell: powerShell, subshells: t, open: []int{group}}
	l.read([]rune(line))
	l.endCommand()
	return l
}

// subshells numbers the subshells a line opens, from 1, and records which each
// was opened in and which are scripts handed to another shell. 0 is the shell
// the line starts in.
type subshells struct {
	parent []int
	script []bool
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
	subshells  *subshells
	open       []int // the subshells open around the command being read, innermost last
	words      []string
	word       strings.Builder
	started    bool // a word has begun, even an empty quoted one
	target     bool // the word being read is where a redirection goes
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
		l.words = nil
	}
	l.redirected = false
}

func (l *lexer) group() int {
	return l.open[len(l.open)-1]
}

// substitute reads the text inside `$(...)` or backticks as the commands it
// runs, which happen whatever the word around them is for.
func (l *lexer) substitute(inner []rune) {
	sub := lexIn(string(inner), l.powerShell, l.subshells, l.subshells.start(l.group()))
	l.commands = append(l.commands, sub.commands...)
	l.groups = append(l.groups, sub.groups...)
	l.writes = append(l.writes, sub.writes...)
	l.started = true
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

// programsIn is the commands a line runs, with what only wraps them taken off.
// A shell handed a string, `cmd /c`, PowerShell's -Command and `eval` run that
// string, so it is read in turn for the commands it runs.
func programsIn(line string, powerShell bool) [][]string {
	var out [][]string
	runs, _ := programsAt(line, powerShell)
	for _, r := range runs {
		out = append(out, r.words)
	}
	return out
}

// ran is a command a line runs, and the subshell it runs in: 0 for the shell
// the line starts in, and a number of its own for each subshell, substitution
// and script handed to another shell, whose `cd` moves only that one.
type ran struct {
	words     []string
	group     int
	redirects bool // it sends its output to a file
}

// programsAt is programsIn with the subshell each command runs in, and which
// subshell opened each.
func programsAt(line string, powerShell bool) ([]ran, *subshells) {
	t := newSubshells()
	return programsWithin(line, powerShell, t, 0, 0), t
}

func programsWithin(line string, powerShell bool, t *subshells, group, depth int) []ran {
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
		script, in := run.words, l.groups[i]
		switch {
		case run.shell:
			in = t.start(in)
			t.script[in] = true
		case base(script[0]) == "eval":
			// eval runs its string in this shell, so a `cd` in it moves this one.
			script = script[1:]
		default:
			out = append(out, ran{run.words, in, l.writes[i]})
			if base(run.words[0]) == "find" {
				// Each -exec runs a command:
				// `find . -exec true \; -exec git commit -m x \;` runs git.
				for j, w := range run.words {
					if !findRuns(w) {
						continue
					}
					if exec := program(run.words[j+1:]); len(exec.words) > 0 {
						out = append(out, ran{exec.words, in, false})
					}
				}
			}
			continue
		}
		if depth < 4 {
			out = append(out, programsWithin(strings.Join(script, " "), powerShell, t, in, depth+1)...)
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
				return aliasedCommand(value), dir
			}
			return a, dir
		}
	}
	return "", dir
}

// aliasedCommand is the git subcommand an alias runs: `commit -a`, or
// `!git commit -a` handed to the shell.
func aliasedCommand(value string) string {
	fields := strings.Fields(value)
	if len(fields) > 0 && base(fields[0]) == "git" {
		fields = fields[1:]
	}
	sub, _ := gitCommand(fields)
	return sub
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
				return makesCommits[aliasedCommand(words[i+1])]
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
