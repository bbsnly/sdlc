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
	l := lexer{powerShell: powerShell}
	l.read([]rune(line))
	l.endCommand()
	return l.commands
}

type lexer struct {
	powerShell bool
	commands   [][]string
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
		l.words = nil
	}
}

// substitute reads the text inside `$(...)` or backticks as the commands it
// runs, which happen whatever the word around them is for.
func (l *lexer) substitute(inner []rune) {
	l.commands = append(l.commands, commandsIn(string(inner), l.powerShell)...)
	l.started = true
}

func (l *lexer) read(rs []rune) {
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\\' && i+1 < len(rs) && rs[i+1] == '\n':
			i++
			l.endWord()
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
		case r == '#' && !l.started:
			// A comment runs to the end of the line, and nothing in it runs.
			i = find(rs, i, '\n') - 1
		case r == '>' || r == '<' || (r == '&' && i+1 < len(rs) && rs[i+1] == '>'):
			i = l.redirect(rs, i)
		case r == ' ' || r == '\t' || r == '\r':
			l.endWord()
		case strings.ContainsRune("\n;&|(){}", r):
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
	return programsAt(line, powerShell, 0)
}

func programsAt(line string, powerShell bool, depth int) [][]string {
	var out [][]string
	for _, words := range commandsIn(line, powerShell) {
		for len(words) > 0 && isAssignment(words[0]) {
			words = words[1:]
		}
		run := program(words)
		if len(run.words) == 0 {
			continue
		}
		script := run.words
		if !run.shell && base(script[0]) == "eval" {
			script = script[1:]
		} else if !run.shell {
			out = append(out, run.words)
			continue
		}
		if depth < 4 {
			out = append(out, programsAt(strings.Join(script, " "), powerShell, depth+1)...)
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
	return nil, false
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
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-C" && i+1 < len(args):
			i++
			if to := clean(args[i]); isAbsolute(to) {
				dir = to
			} else {
				dir = path.Join(dir, to)
			}
		case a == "-c" || a == "--git-dir" || a == "--work-tree" || a == "--namespace" || a == "--config-env":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a, dir
		}
	}
	return "", dir
}
