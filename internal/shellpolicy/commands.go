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
// a Windows path it is a separator, not an escape. Only a line continuation,
// an escaped separator or quote outside quotes, and an escaped character inside
// double quotes, in a POSIX shell, are read as the shell reads them.
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
	// env is what a script handed to a shell starts with that the command
	// handing it over set: `GIT_DIR=/elsewhere/.git sh -c 'git log'`.
	env map[int][]string
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

// startsWith records what the subshell g starts with, NAME=value.
func (t *subshells) startsWith(g int, env []string) {
	if len(env) == 0 {
		return
	}
	if t.env == nil {
		t.env = map[int][]string{}
	}
	t.env[g] = env
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
	// marks is each command's words again, with every substitution in them
	// written as $SUB where commands has nothing: `git "$(echo worktree)"
	// prune` is a command whose subcommand the shell has yet to work out, not
	// one whose subcommand is empty.
	marks      [][]string
	groups     []int  // the subshell each command runs in
	writes     []bool // each command sends its output to a file
	redirected bool   // the command being read does
	// substituted is set for each command a word of which had a substitution
	// taken out of it, and substituting for the command being read: `cd $(pwd)/b`
	// is read as `cd /b`, which is not where it goes.
	substituted  []bool
	substituting bool
	// calls is set for each PowerShell command after the call operator, and
	// call for the command being read. `& $G worktree prune` runs what $G
	// names, and without the `&` a command that starts with a variable is a
	// value: `$a -eq $b` compares, and runs nothing.
	calls     []bool
	call      bool
	subshells *subshells
	open      []int // the subshells open around the command being read, innermost last
	words     []string
	marked    []string
	word      strings.Builder
	mark      strings.Builder
	started   bool // a word has begun, even an empty quoted one
	target    bool // the word being read is where a redirection goes
	depth     int  // the substitutions this line is inside
	// inputs is each command's here-strings, `<<< word`, as marked, and input
	// the command being read's; here is set while the word after `<<<` is
	// read. What `read` sets is read from them.
	inputs [][]string
	input  []string
	here   bool
	// fed is set for each command given something on standard input: the
	// output of the command before it in a pipeline, or a file, a document or
	// a here-string redirected in. feeding is the command being read's.
	fed     []bool
	feeding bool
}

func (l *lexer) endWord() {
	if !l.started {
		return
	}
	if l.target {
		l.target = false
		if l.here {
			l.input = append(l.input, l.mark.String())
			l.here = false
		}
	} else {
		l.words = append(l.words, l.word.String())
		l.marked = append(l.marked, l.mark.String())
	}
	l.word.Reset()
	l.mark.Reset()
	l.started = false
}

// put and putRune add to the word being read, in both its spellings.
func (l *lexer) put(s string) {
	l.word.WriteString(s)
	l.mark.WriteString(s)
}

func (l *lexer) putRune(r rune) {
	l.word.WriteRune(r)
	l.mark.WriteRune(r)
}

func (l *lexer) endCommand() {
	l.endWord()
	l.target = false
	if len(l.words) > 0 {
		l.commands = append(l.commands, l.words)
		l.marks = append(l.marks, l.marked)
		l.groups = append(l.groups, l.group())
		l.writes = append(l.writes, l.redirected)
		l.substituted = append(l.substituted, l.substituting)
		l.calls = append(l.calls, l.call)
		l.inputs = append(l.inputs, l.input)
		l.fed = append(l.fed, l.feeding)
		l.call = false
		l.words, l.marked = nil, nil
		l.feeding = false
	}
	l.input, l.here = nil, false
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
	l.mark.WriteString("$SUB")
	if l.depth >= maxNesting {
		// Each level is read again from where it opens, so a line nested
		// thousands deep took longer than Claude Code gives the hook, and a
		// command the hook has not answered in time runs unchecked.
		l.subshells.tooDeep = true
		return
	}
	sub := lexAt(string(inner), l.powerShell, l.subshells, l.subshells.start(l.group()), l.depth+1)
	l.commands = append(l.commands, sub.commands...)
	l.marks = append(l.marks, sub.marks...)
	l.groups = append(l.groups, sub.groups...)
	l.writes = append(l.writes, sub.writes...)
	l.substituted = append(l.substituted, sub.substituted...)
	l.calls = append(l.calls, sub.calls...)
	l.inputs = append(l.inputs, sub.inputs...)
	l.fed = append(l.fed, sub.fed...)
}

func (l *lexer) read(rs []rune) {
	for i := 0; i < len(rs); i++ {
		// The escapes, quotes and substitutions are asked first, and only a
		// character that is none of them is read as a separator or a
		// character of a word: the order is the one the cases were always
		// tried in.
		if at, ok := l.readQuoting(rs, i); ok {
			i = at
			continue
		}
		i = l.readPlain(rs, i)
	}
}

// readQuoting reads the character at i where it opens an escape, a quote or
// a substitution, and returns the index of the last character it read. It is
// not ok for any other character, of which it has read nothing.
func (l *lexer) readQuoting(rs []rune, i int) (int, bool) {
	r := rs[i]
	switch {
	case r == '\\' && i+1 < len(rs) && rs[i+1] == '\n':
		i++
		l.endWord()
	case r == '\\' && !l.powerShell && i+1 < len(rs) && strings.ContainsRune(";&|'\"", rs[i+1]):
		// An escaped separator is a character of the word, as find's `\;`
		// is. Read as a separator, it ended find before a second -exec. So
		// is an escaped quote: read as a backslash and then a quote, it
		// opened a string, and `echo \'; git commit -m x; echo \'` was one
		// echo, and `'it'\''s'` was it\s.
		i++
		l.putRune(rs[i])
		l.started = true
	case r == '\'':
		end := find(rs, i+1, '\'')
		l.put(string(rs[i+1 : end]))
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
		l.put(string(rs[i:min(end+1, len(rs))]))
		l.started = true
		i = end
	default:
		return i, false
	}
	return i, true
}

// readPlain reads the character at i, one readQuoting did not take: a
// comment, a redirect, a separator, or a character of a word. It returns the
// index of the last character it read.
func (l *lexer) readPlain(rs []rune, i int) int {
	r := rs[i]
	switch {
	case l.powerShell && r == '<' && i+1 < len(rs) && rs[i+1] == '#':
		// PowerShell's block comment. Read as a redirect and then a line
		// comment, `<# note #> sdlc approve` hid the command after it.
		l.endWord()
		i = blockCommentEnd(rs, i)
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
		l.put("{}")
		l.started = true
	case r == '(':
		l.endCommand()
		l.open = append(l.open, l.subshells.start(l.group()))
	case r == ')':
		l.endCommand()
		if len(l.open) > 1 {
			l.open = l.open[:len(l.open)-1]
		}
	case l.powerShell && loneAmpersand(rs, i):
		l.endCommand()
		l.call = true
	case strings.ContainsRune("\n;&|{}", r):
		l.endCommand()
		// A `|` alone hands the next command this one's output; `||` does not.
		if r == '|' && (i+1 == len(rs) || rs[i+1] != '|') && (i == 0 || rs[i-1] != '|') {
			l.feeding = true
		}
	default:
		l.putRune(r)
		l.started = true
	}
	return i
}

// blockCommentEnd is the index of the `>` that closes the PowerShell block
// comment opening at i, or past the end where nothing closes it.
func blockCommentEnd(rs []rune, i int) int {
	j := i + 2
	for j+1 < len(rs) && (rs[j] != '#' || rs[j+1] != '>') {
		j++
	}
	return j + 1
}

// loneAmpersand reports whether the character at i is an `&` with no other
// beside it, which in PowerShell is the call operator and not `&&`.
func loneAmpersand(rs []rune, i int) bool {
	return rs[i] == '&' && (i+1 == len(rs) || rs[i+1] != '&') && (i == 0 || rs[i-1] != '&')
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
			l.putRune(rs[i])
		case r == '`':
			end := find(rs, i+1, '`')
			l.substitute(rs[i+1 : end])
			i = end
		case r == '$' && i+1 < len(rs) && rs[i+1] == '(':
			end := closing(rs, i+2)
			l.substitute(rs[i+2 : end])
			i = end
		default:
			l.putRune(r)
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
		l.mark.Reset()
		l.started = false
	}
	l.endWord()
	start := i
	for i < len(rs) && strings.ContainsRune("<>&|-", rs[i]) {
		l.redirected = l.redirected || rs[i] == '>'
		l.feeding = l.feeding || rs[i] == '<'
		i++
	}
	// What follows is the target: a file, the descriptor in `>&2`, or the
	// text a here-string hands the command.
	l.target = true
	l.here = string(rs[start:i]) == "<<<"
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
	// marked is words as the lexer marked them, with $SUB where a substitution
	// was, and with what only wraps the command taken off as it was for words.
	// git is read from these, because a subcommand or a place the shell has
	// yet to work out is not one that is empty.
	marked []string
	// env is what the command runs with that it sets for itself, NAME=value
	// as marked: the assignments in front of it and those env gives it.
	env []string
	// sets is what it sets for the commands after it in its subshell: a line
	// of assignments alone, or export, declare and their like. A command that
	// only sets has no words.
	sets []string
	// chdir is where env -C runs it, or "".
	chdir string
	// expression is a PowerShell command the call operator does not run, so
	// that a variable in front of it is a value and not a program.
	expression bool
	// extra is a reading of a script handed to a shell with its substitutions
	// marked, which only the git rules read: the other rules read the script
	// once, as they always have.
	extra bool
	// piped is set when the command, or the one that handed it to a shell, is
	// given more words on standard input, through xargs or GNU parallel: the
	// words here are then not all it runs with.
	piped bool
	// input is set when the command, or the one that handed it to a shell, is
	// given anything on standard input: a pipeline's output, a file, a
	// document or a here-string. What a Claude Code session reads there is a
	// prompt the words do not show.
	input bool
}

// programsAt is the commands a line runs, with what only wraps them taken off,
// the subshell each runs in, and which subshell opened each. A shell handed a
// string, `cmd /c`, PowerShell's -Command and `eval` run that string, so it is
// read in turn for the commands it runs.
func programsAt(line string, powerShell bool) ([]ran, *subshells) {
	t := newSubshells()
	return programsWithin(line, powerShell, t, 0, 0, false, false, false), t
}

func programsWithin(line string, powerShell bool, t *subshells, group, depth int, substituted, piped, input bool) []ran {
	p := programScan{powerShell: powerShell, t: t, depth: depth, substituted: substituted, piped: piped, input: input}
	p.l = lexIn(line, powerShell, t, group)
	for i, words := range p.l.commands {
		p.command(i, words)
	}
	return p.out
}

// programScan is programsWithin part way through a line: the line as lexed,
// what the line itself was handed with, and the commands found so far.
type programScan struct {
	powerShell                bool
	t                         *subshells
	depth                     int
	substituted, piped, input bool
	l                         *lexer
	out                       []ran
}

// lexedCommand is one command of a line as programScan reads it: the
// program it runs, with what wraps it taken off, from its words and again
// from its words with the substitutions marked; the subshell it runs in;
// whether it or the line holds a substitution, and whether it is fed more
// words on standard input; the variables it runs with, and those a
// `${NAME:=word}` among its words sets.
type lexedCommand struct {
	run, marked     invocation
	in              int
	cut, fed, input bool
	env, defaults   []string
}

// command adds what the i-th command of the line runs.
func (p *programScan) command(i int, words []string) {
	l, t := p.l, p.t
	marks := l.marks[i]
	var prefix []string
	for len(words) > 0 && isAssignment(words[0]) {
		prefix = append(prefix, marks[0])
		words, marks = words[1:], marks[1:]
	}
	run := program(words)
	c := lexedCommand{run: run, in: l.groups[i], cut: p.substituted || l.substituted[i], fed: p.piped || run.piped, input: p.input || l.fed[i]}
	if !p.powerShell {
		c.defaults = defaulted(l.marks[i])
	}
	if len(run.words) == 0 {
		if len(prefix) > 0 && !run.shell {
			// A line of assignments alone keeps them for what follows:
			// `GIT_DIR=/elsewhere/.git; git log`.
			p.out = append(p.out, ran{group: c.in, sets: slices.Concat(c.defaults, prefix)})
		}
		return
	}
	c.marked = program(marks)
	c.env = slices.Concat(prefix, c.marked.assigns)
	script := run.words
	switch {
	case run.shell:
		c.in = t.start(c.in)
		t.script[c.in] = true
		t.startsWith(c.in, c.env)
	case base(script[0]) == "eval":
		// eval runs its string in this shell, so a `cd` in it moves this one,
		// and what is set in front of it stays set, as it does for the
		// shell's own builtins.
		script = script[1:]
		if len(c.env) > 0 {
			p.out = append(p.out, ran{group: c.in, sets: c.env})
		}
	default:
		p.plain(i, c)
		return
	}
	if p.depth >= 4 {
		return
	}
	p.script(script, c)
}

// plain adds a command that hands no string to a shell, and with a find the
// commands its -exec run.
func (p *programScan) plain(i int, c lexedCommand) {
	l, run, marked := p.l, c.run, c.marked
	sets := slices.Concat(c.defaults, exported(marked.words, l.inputs[i], p.powerShell))
	p.out = append(p.out, ran{words: run.words, group: c.in, redirects: l.writes[i], substituted: c.cut,
		marked: marked.words, env: c.env, sets: sets, chdir: marked.chdir, piped: c.fed, input: c.input,
		expression: p.powerShell && !l.calls[i]})
	if base(run.words[0]) != "find" {
		return
	}
	// Each -exec runs a command:
	// `find . -exec true \; -exec git commit -m x \;` runs git.
	for j, w := range run.words {
		if !findRuns(w) {
			continue
		}
		if exec := program(run.words[j+1:]); len(exec.words) > 0 {
			p.out = append(p.out, ran{words: exec.words, group: c.in, substituted: c.cut,
				marked: exec.words, env: c.env, piped: c.fed})
		}
	}
}

// script adds the commands a string handed to a shell or to eval runs,
// script being the words of that string.
func (p *programScan) script(script []string, c lexedCommand) {
	t := p.t
	p.out = append(p.out, programsWithin(strings.Join(script, " "), p.powerShell, t, c.in, p.depth+1, c.cut, c.fed, c.input)...)
	// The script once more as the shell will run it, with each substitution
	// standing where it was: to the reading above, `sh -c "git $(echo
	// worktree) prune"` is `git  prune`, and git's subcommand is not empty.
	if len(c.marked.words) < len(script) {
		return
	}
	markedScript := c.marked.words[len(c.marked.words)-len(script):]
	if slices.Equal(markedScript, script) {
		return
	}
	again := t.start(c.in)
	t.script[again] = c.run.shell
	t.startsWith(again, c.env)
	for _, r := range programsWithin(strings.Join(markedScript, " "), p.powerShell, t, again, p.depth+1, c.cut, c.fed, c.input) {
		r.extra = true
		p.out = append(p.out, r)
	}
}

// exported is what export, declare and their like set for the commands after
// them: `export GIT_WORK_TREE=/elsewhere`, `declare -gx GIT_DIR=/elsewhere/.git`.
// printf -v and read set a variable as an assignment does, and an export
// after them hands it to git: read as neither, `printf -v GIT_DIR %s
// /elsewhere/.git; export GIT_DIR; git commit` was a commit in this checkout.
// input is the command's here-strings, which read reads.
func exported(words, input []string, powerShell bool) []string {
	if len(words) == 0 {
		return nil
	}
	switch base(words[0]) {
	case "export", "declare", "typeset", "local", "readonly":
	case "printf":
		return printfSets(words[1:])
	case "read":
		return readSets(words[1:], input)
	case "set-item", "si", "new-item", "ni", "set-content", "sc", "add-content", "ac":
		if powerShell {
			return powerShellItemSets(base(words[0]), words[1:])
		}
		return nil
	default:
		return nil
	}
	var sets []string
	for _, w := range words[1:] {
		if isAssignment(w) {
			sets = append(sets, w)
		}
	}
	return sets
}

// printfSets is what `printf -v NAME [--] FORMAT ARGUMENTS` sets: the format
// with each %s given the next argument, and a value the hook cannot know where
// the format holds anything else printf would work out. Read as the format,
// the `--` in `printf -v GIT_DIR -- %s /elsewhere/.git` was the value, and the
// commit after it one in this checkout.
func printfSets(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	name, ok := strings.CutPrefix(args[0], "-v")
	if !ok {
		return nil
	}
	if args = args[1:]; name == "" && len(args) > 0 {
		name, args = args[0], args[1:]
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return nil
	}
	value := unknownValue
	switch format := args[0]; {
	case strings.HasPrefix(format, "-"):
	case format == "%s":
		value = strings.Join(args[1:], "")
	case !strings.ContainsAny(format, `%\`):
		value = format
	}
	return []string{name + "=" + value}
}

// readSets is what read sets: one name to the first line of the here-string
// it is given, read with at most -r, and otherwise each name to a value the
// hook cannot know, since what comes on standard input is not in the command.
func readSets(args, input []string) []string {
	exact := len(input) > 0
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		exact = exact && args[0] == "-r"
		args = args[1:]
	}
	value := unknownValue
	if exact && len(args) == 1 {
		line, _, _ := strings.Cut(input[len(input)-1], "\n")
		value = strings.TrimSpace(line)
	}
	sets := make([]string, 0, len(args))
	for _, name := range args {
		sets = append(sets, name+"="+value)
	}
	return sets
}

// powerShellItemSets is what Set-Item, New-Item, Set-Content and Add-Content
// set on the Env: drive, their parameters in any order and their path quoted
// or not: the path given with -Path or -LiteralPath, or first; New-Item's
// -Name, under the drive itself; and the value given with -Value, or next. A
// value the words do not give comes down the pipeline, and is one the hook
// cannot know. powerShellSets reads a few orders of them; read by it alone,
// `Set-Item -Value <main checkout>/.git -Path Env:GIT_DIR` set nothing.
func powerShellItemSets(cmdlet string, args []string) []string {
	p := powerShellItemParams(cmdlet, args)
	if !p.named && len(p.positional) > 0 {
		p.path, p.positional = p.positional[0], p.positional[1:]
	}
	if !p.valued {
		p.value = unknownValue
		if len(p.positional) > 0 {
			p.value = p.positional[0]
		}
	}
	rest, ok := cutPrefixFold(p.path, "env:")
	if rest = strings.TrimLeft(rest, `\/`); rest != "" {
		p.name = rest
	}
	if !ok || p.name == "" {
		return nil
	}
	return []string{p.name + "=" + p.value}
}

// itemParams is what the words of one of those cmdlets bind: its path, name
// and value, whether the path and the value were given by name, and the words
// no parameter took.
type itemParams struct {
	path, name, value string
	named, valued     bool
	positional        []string
}

// powerShellItemParams reads the parameters powerShellItemSets goes by.
func powerShellItemParams(cmdlet string, args []string) itemParams {
	var p itemParams
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			p.positional = append(p.positional, a)
			continue
		}
		param, v, joined := strings.Cut(a[1:], ":")
		param = strings.ToLower(param)
		// A parameter takes the next word for its value where it is not a
		// switch and either had no colon, `-Path env:X`, or a colon with
		// nothing after it, `-Path: env:X`. Read as an empty value, the second
		// had lost the place.
		if !powerShellSwitch(cmdlet, param) && (!joined || v == "") && i+1 < len(args) {
			i++
			v = args[i]
		}
		switch {
		case cutShort(param, "path"), cutShort(param, "literalpath"), param == "pspath", param == "lp":
			p.path, p.named = v, true
		case cutShort(param, "name"):
			p.name = v
		case cutShort(param, "value"):
			p.value, p.valued = v, true
		}
	}
	return p
}

// powerShellSwitch reports whether a parameter of the cmdlet, lower case and
// perhaps cut short, is a switch, which takes no value after it. The switches
// are read per cmdlet, not as one list: New-Item has no -NoNewline or
// -PassThru, so `New-Item -N env:X` binds -N to -Name, not to a switch, and the
// place is not lost. cmdlet is the base name, an alias included.
func powerShellSwitch(cmdlet, param string) bool {
	// Common to all four: the risk-free switches every one takes.
	switches := []string{"force", "whatif", "confirm", "verbose", "debug", "usetransaction"}
	switch cmdlet {
	case "set-item", "si":
		switches = append(switches, "passthru")
	case "set-content", "sc", "add-content", "ac":
		switches = append(switches, "passthru", "nonewline", "asbytestream")
	}
	for _, s := range switches {
		if cutShort(param, s) {
			return true
		}
	}
	return false
}

// cutShort reports whether param is name, or the start of it, as PowerShell
// takes a parameter's name cut short.
func cutShort(param, name string) bool {
	return param != "" && strings.HasPrefix(name, param)
}

// cutPrefixFold is strings.CutPrefix, with the prefix's letters in either case.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

// defaulted is what `${NAME:=word}` and `${NAME=word}` in words set: the shell
// gives NAME the word where nothing set it before, and it stays set for the
// commands after. Read as a mention, `: ${GIT_DIR:=/elsewhere/.git}; export
// GIT_DIR; git commit` was a commit in this checkout. It is read as setting
// NAME whether or not something had, and the word runs to the first `}`.
//
// A value that still holds a `$`, such as one holding another `${…}`, cannot be
// placed (elsewhere reads it as another checkout), so it is recorded as the
// unknown value rather than copied: a word of `${A=${A=…}`, whose every value
// held the copy after it, had grown the reading's memory as its square. The
// closing `}` for each is found from closeBraces, built once for the word, so
// the run of them that share one closing `}` is read in time linear in the word.
func defaulted(words []string) []string {
	var sets []string
	for _, w := range words {
		if !strings.Contains(w, "${") {
			continue
		}
		closes := closeBraces(w)
		for i := 0; i+1 < len(w); i++ {
			set, ok, last := defaultedAt(w, i, closes)
			if last {
				break
			}
			if ok {
				sets = append(sets, set)
			}
		}
	}
	return sets
}

// defaultedAt is what a `${NAME:=word}` or `${NAME=word}` that opens at
// w[i] sets, where one does; i is before the last character of w, and closes
// is closeBraces of w. last is set where one opens there that nothing
// closes, after which no other in the word is closed either.
func defaultedAt(w string, i int, closes []int) (set string, ok, last bool) {
	if w[i] != '$' || w[i+1] != '{' {
		return "", false, false
	}
	j := i + 2
	for j < len(w) && nameRune(rune(w[j])) {
		j++
	}
	name := w[i+2 : j]
	if name == "" || name[0] >= '0' && name[0] <= '9' {
		return "", false, false
	}
	if k := j; k < len(w) && w[k] == ':' {
		j++
	}
	if j >= len(w) || w[j] != '=' {
		return "", false, false
	}
	start := j + 1
	end := closes[start]
	if end == len(w) {
		// No `}` closes it, and none later can either, since a later
		// `${` starts further right than this value does.
		return "", false, true
	}
	value := w[start:end]
	if strings.IndexByte(value, '$') >= 0 {
		value = unknownValue
	}
	return name + "=" + value, true, false
}

// closeBraces[p] is the index of the first `}` at or after p in w, or len(w)
// where there is none. It is built by one scan from the end, so that a run of
// `${` sharing one closing `}` needs no rescan for each.
func closeBraces(w string) []int {
	next := make([]int, len(w)+1)
	next[len(w)] = len(w)
	for p := len(w) - 1; p >= 0; p-- {
		if w[p] == '}' {
			next[p] = p
		} else {
			next[p] = next[p+1]
		}
	}
	return next
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
	if mentionsOnly(base(words[0])) {
		return nil, false
	}
	for i := 1; i < len(words); i++ {
		if isSdlc(words[i]) && !strings.HasPrefix(words[i-1], "-") {
			return words[i+1:], true
		}
	}
	return nil, false
}

// mentionsOnly reports whether a program prints or reads its words, so that
// sdlc among them is only mentioned: `echo next: sdlc stop` is not a stop.
func mentionsOnly(program string) bool {
	switch program {
	case "echo", "printf", "print", "write-host", "write-output", "ls", "dir", "cat", "type",
		"grep", "rg", "head", "tail", "less", "more", "man", "which", "whereis", "file", "stat", "wc":
		return true
	}
	return false
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
			dir = movedBy(dir, args[i])
		case a == "-c" && i+1 < len(args):
			i++
			noteAlias(aliases, args[i])
		case a == optGitDir || a == optWorkTree || a == optNamespace ||
			a == optConfigEnv || a == optAttrSource:
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

// movedBy is where a -C takes git from dir: to the directory it names where
// that is absolute, and to it below dir where it is not.
func movedBy(dir, word string) string {
	to := clean(word)
	if isAbsolute(to) {
		return to
	}
	return path.Join(dir, to)
}

// noteAlias records the alias a `-c alias.NAME=VALUE` defines, under its name
// in lower case; any other setting is not one gitCall reads.
func noteAlias(aliases map[string]string, setting string) {
	if name, value, ok := strings.Cut(setting, "="); ok {
		if name, ok := strings.CutPrefix(strings.ToLower(name), aliasPrefix); ok {
			aliases[name] = value
		}
	}
}

// inThisRepository reports whether a git command run in dir works on the
// project's own repository, wherever its -C and --git-dir send it.
func inThisRepository(words []string, dir string, s State) bool {
	// Another repository is a directory outside the project. Its .git is
	// asked about rather than the directory, which for the project root
	// itself resolves to nothing, the same as outside: `cd /path/to/project &&
	// git commit`, the usual way to spell it, went past the gate.
	repository := repositoryOf(words, gitPlace(words, dir))
	if s.Resolve == nil || s.Resolve(repository) != "" {
		return true
	}
	// A linked worktree keeps its git directory outside itself, and named in
	// full, that is still this checkout's repository.
	if s.Shared != nil {
		for _, l := range s.Shared(clean(repository)) {
			if l.Own {
				return true
			}
		}
	}
	return false
}

// gitPlace is the directory a git command run in dir works in, once its -C
// have moved it. A directory not followed is "", which is the project.
func gitPlace(words []string, dir string) string {
	_, to := gitCommand(words[1:])
	switch {
	case strings.ContainsAny(to, "$%") || shellDirectory(to):
		// `git -C "$OLDPWD"`, or `git -C ~-`, is somewhere this cannot know,
		// which is not another repository.
		return ""
	case isAbsolute(to) || strings.HasPrefix(to, "~"):
		// The lookup reads ~ as the home it names.
		return to
	case to != "":
		return path.Join(dir, to)
	}
	return dir
}

// repositoryOf is the git directory a git command working in dir uses: the
// .git there, or the one --git-dir names.
func repositoryOf(words []string, dir string) string {
	switch gitDir := gitDirOf(words[1:]); {
	case strings.ContainsAny(gitDir, "$%") || shellDirectory(gitDir):
		// Wherever the command runs, `--git-dir` can name this repository:
		// `cd /tmp && git --git-dir="$PROJECT/.git" commit`.
		return ".git"
	case isAbsolute(gitDir) || strings.HasPrefix(gitDir, "~"):
		return gitDir
	case gitDir != "":
		return path.Join(dir, gitDir)
	}
	return path.Join(dir, ".git")
}

// workTreeOf is the work tree a git command working in dir is given with
// --work-tree, or "" where it is given none, or one the shell has yet to expand.
func workTreeOf(words []string, dir string) string {
	args, tree := words[1:], ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == optWorkTree && i+1 < len(args):
			i++
			tree = clean(args[i])
		case strings.HasPrefix(a, "--work-tree="):
			tree = clean(strings.TrimPrefix(a, "--work-tree="))
		case a == "-C" || a == "-c" || a == optGitDir || a == optNamespace ||
			a == optConfigEnv || a == optAttrSource:
			i++
		case !strings.HasPrefix(a, "-"):
			i = len(args)
		}
	}
	switch {
	case tree == "" || strings.ContainsAny(tree, "$%") || shellDirectory(tree):
		return ""
	case isAbsolute(tree) || strings.HasPrefix(tree, "~"):
		return tree
	}
	return path.Join(dir, tree)
}

// inAnotherCheckout reports whether a git command run in dir works on another
// checkout of the project's repository, through its git directory or its work
// tree, or on the project's own git directory named in full.
func inAnotherCheckout(words []string, dir string, s State, m *memo) bool {
	dir = gitPlace(words, dir)
	for _, p := range []string{repositoryOf(words, dir), workTreeOf(words, dir)} {
		if p != "" && len(m.shared(s, p)) > 0 {
			return true
		}
	}
	return false
}

// commitElsewhere refuses a commit made in another checkout of the project's
// repository. The story's gates were passed on the work in the checkout it is
// held in, and a commit in another one records work nobody reviewed: that the
// story may commit says nothing about it.
//
// away is gitRules' answer for what the words do not show: git's variables,
// core.worktree, and another checkout's files staged earlier in the command.
func commitElsewhere(words []string, dir string, s State, away bool) (Finding, bool) {
	if s.Shared == nil {
		return Finding{}, false
	}
	if away {
		return inAnotherCheckoutFinding, true
	}
	dir = gitPlace(words, dir)
	m := &memo{landed: map[string][]Landing{}}
	for _, p := range []string{repositoryOf(words, dir), workTreeOf(words, dir)} {
		if p == "" {
			continue
		}
		for _, l := range m.shared(s, p) {
			if !l.Own {
				return inAnotherCheckoutFinding, true
			}
		}
	}
	return Finding{}, false
}

var inAnotherCheckoutFinding = Finding{
	Rule: "commit-in-another-checkout",
	Reason: "this commit would be made in another checkout of this repository, and the " +
		"story's gates cover only the checkout it is held in: what they reviewed is not " +
		"what would be committed there",
	Route: "commit from the checkout the story is held in",
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
		case (a == "-C" || a == optGitDir) && i+1 < len(args):
			i++
			// Empty is where a substitution was: `git -C "$(mktemp -d)"`.
			if args[i] == "" || !placedWords(args[i:i+1]) {
				return false
			}
		case strings.HasPrefix(a, optGitDirEquals):
			if a == optGitDirEquals || !placedWords([]string{strings.TrimPrefix(a, optGitDirEquals)}) {
				return false
			}
		case a == "-c" || a == optWorkTree || a == optNamespace ||
			a == optConfigEnv || a == optAttrSource:
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
	"commit": true, "commit-tree": true, "merge": true, subCherryPick: true, "revert": true,
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
		case w == "--no-commit" && (sub == "merge" || sub == subCherryPick || sub == "revert"),
			w == "--squash" && sub == "merge",
			w == "-n" && (sub == subCherryPick || sub == "revert"):
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
			if strings.HasPrefix(strings.ToLower(words[i]), aliasPrefix) {
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
		never: []string{"squash", "help"}, letters: "h", held: []string{longNoCommit},
		ends: "mFsSX", next: "mFsX",
		valued:      []string{"message", "file", "strategy", longStrategyOption, "into-name", "cleanup"},
		abbreviates: true,
	},
	subCherryPick: {
		never: []string{"help"}, letters: "h", held: []string{longNoCommit}, heldLetters: "n",
		ends: "mSX", next: "mX", valued: []string{"mainline", "strategy", longStrategyOption, "cleanup"},
	},
	"revert": {
		never: []string{"help"}, letters: "h", held: []string{longNoCommit}, heldLetters: "n",
		ends: "mSX", next: "mX", valued: []string{"mainline", "strategy", longStrategyOption, "cleanup"},
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
		if w == "--commit" {
			held = false
			continue
		}
		never, holds, skip := r.option(w)
		if never {
			return true
		}
		held = held || holds
		if skip {
			i++
		}
	}
	return held
}

// option reads one word other than --commit as this row's options: never
// where it holds one that makes the command commit nothing, holds where it
// holds one a later --commit undoes, and skip where its last option takes the
// next word as its value. A word that is no option is none of the three.
func (r commitRow) option(w string) (never, holds, skip bool) {
	switch {
	case strings.HasPrefix(w, "--"):
		return r.long(w)
	case strings.HasPrefix(w, "-") && len(w) > 1:
		return r.bundle(w)
	}
	return false, false, false
}

// long is option for a long option.
func (r commitRow) long(w string) (never, holds, skip bool) {
	name, _, valued := strings.Cut(w[2:], "=")
	if name == "" {
		return false, false, false
	}
	if prefixOfAny(name, r.never) {
		return true, false, false
	}
	return false, prefixOfAny(name, r.held), !valued && r.takesNext(name)
}

// bundle is option for a bundle of letters, read up to the first whose
// value is the rest of the word.
func (r commitRow) bundle(w string) (never, holds, skip bool) {
	for j := 1; j < len(w); j++ {
		c := w[j : j+1]
		if strings.Contains(r.letters, c) {
			return true, false, false
		}
		if strings.Contains(r.heldLetters, c) {
			holds = true
		}
		if strings.Contains(r.ends, c) {
			return false, holds, j == len(w)-1 && strings.Contains(r.next, c)
		}
	}
	return false, holds, false
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
		case a == optGitDir && i+1 < len(args):
			i++
			gitDir = clean(args[i])
		case strings.HasPrefix(a, optGitDirEquals):
			gitDir = clean(strings.TrimPrefix(a, optGitDirEquals))
		case a == "-C" || a == "-c" || a == optWorkTree || a == optNamespace ||
			a == optConfigEnv || a == optAttrSource:
			i++
		case !strings.HasPrefix(a, "-"):
			return gitDir
		}
	}
	return gitDir
}
