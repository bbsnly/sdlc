package shellpolicy

import (
	"slices"
	"strconv"
	"strings"
)

// The path rules read a command without its quotes, so that a path hidden in
// a string is still found. That refused a path that was only named: in the
// text of an escalation, behind a pathspec that leaves it out, beside a
// request for help. This file says where such a thing is with certainty only
// named. It reads the command with a scanner of its own that follows the few
// shapes it is sure of and gives up on the rest, and giving up changes nothing:
// the command is then read as it always was.

// TextFlags are the flags of sdlc whose value is text for a person to read
// and never a path sdlc opens. Each is one of ValueFlags. It is exported for
// the command's own tests to hold to the commands it has, as they hold
// ValueFlags: a flag here that a later command read as a path would have its
// path taken for a mention.
var TextFlags = map[string]bool{"--message": true, "--note": true, "--reason": true, "--reject": true}

// gitReads are the git commands that take what follows `--` as pathspecs and
// do nothing with them but read: each prints what it finds in the repository,
// changes no file, and does not print the words it was handed. The path in a
// pathspec that leaves it out is a mention on both counts and for these
// alone. Asked of any git command, `git mv -f -- OTHER ':!a/../PATH'` was let
// through: git mv reads the word as a plain path, folds the `a/..` away and
// moves OTHER over PATH. And `git rev-parse -- ':(exclude)PATH' | INTERPRETER`
// prints the pathspec itself to a program that could act on it. A command
// that is not here is not known to be safe, so one is added only with both
// halves of that checked against git.
var gitReads = map[string]bool{
	"diff": true, "log": true, "show": true, "status": true, "ls-files": true, "grep": true,
	"diff-tree": true, "diff-index": true, "diff-files": true,
}

// shellWords start a command the scanner does not follow: what runs under
// them is decided when the shell gets there, as in `while read l; do $l; done`,
// or is a line the shell ran before and runs again, as `fc`, `history` and
// PowerShell's Invoke-History do.
var shellWords = map[string]bool{
	"while": true, "until": true, "for": true, "if": true, "then": true, "else": true, "elif": true,
	"do": true, "done": true, "fi": true, "case": true, "esac": true, "select": true, "function": true,
	"time": true, "coproc": true, "!": true, "[[": true, "foreach": true, "switch": true, "try": true,
	"trap": true, "fc": true, "history": true, "r": true, "invoke-history": true, "ihy": true,
	"get-history": true, "ghy": true, "h": true,
}

const (
	// namedOnly stands where a mention was taken out. It is a word no rule
	// reads anything into.
	namedOnly = "="
	// helpAlone stands where a command that only asks for help was.
	helpAlone = "sdlc help"

	// Past these the line is read as it always was. They bound the work a
	// command written to be slow can ask for, since the hook has a deadline
	// and answers "allowed" when it is missed.
	maxSwaps   = 64
	maxReaders = 8
)

// Why the scanner was not sure of a mention. Each names one cause, and where
// there is something to do about it, says what.
const (
	whyOtherName   = "sdlc is run under another name or through another program"
	whyAfter       = "a command other than `cd` or sdlc runs before it on the line"
	whyRereads     = "another program on the line runs text it is handed"
	whyConstruct   = "the line holds a loop, a condition or a command named by a word the shell fills in"
	whyToFile      = "what the command prints is sent to a file"
	whyToProgram   = "what the command prints is piped to a program the hook does not know to be one that only reads, which could run it"
	whyBackground  = "the command runs in the background"
	whyLaterWord   = "a later command on the line has a word the shell fills in when it runs, which could be the path"
	whyLaterCode   = "a later command on the line runs a program written on the line, which could be handed the path"
	whyShortFlag   = "the flag is shortened; write `--message`, `--note`, `--reason` or `--reject` in full"
	whyAfterDashes = "the text comes after `--`, where sdlc reads no flags"
	whyPieces      = "the text is not one quoted string: a quote opens or closes inside the word"
	whyVariable    = "the text holds a variable the shell fills in when the command runs; write the path out in the text"

	whyGitOtherName = "git is run under another name or through another program"
	whyGitOption    = "git is given an option before its subcommand, which can change how a pathspec is read"
	whyLiteral      = "GIT_LITERAL_PATHSPECS is set for the session, and with it git reads the pathspec as a file's name; it cannot be cleared on the command, so start the session without it or read the listing without the pathspec"
	whyMagic        = "the pathspec carries magic beside `exclude` that the hook does not read; it reads `top`, `icase` and `glob`"
	whyPathChars    = "the path in the pathspec holds a character the hook does not follow; it follows letters, digits and `._/*?-`"
	whyNoDashes     = "the pathspec is not written after `--`"
	whyBang         = "the pathspec holds `!` outside quotes, which a shell can read as its history; put the pathspec in quotes"
	whyGitCommand   = "the hook reads a pathspec that leaves a path out only for the git commands it knows only to read: diff, log, show, status, ls-files, grep, diff-tree, diff-index and diff-files"
	whyNoIndex      = "with `--no-index` git diff reads the words after `--` as two paths and not as pathspecs"
	whyDotDot       = "the path in the pathspec holds `..`; write the path without it"
	whyGitErrors    = "git's errors, which can quote a pathspec, go with what it prints to a program that could run them; leave the redirect out and pipe with a plain `|`"

	whyEscape     = "the command holds a backslash, and the hook does not follow an escape; write the text without one"
	whyBacktick   = "the command holds a backtick, which PowerShell reads as an escape"
	whyParen      = "the command holds a parenthesis outside quotes, and the hook does not follow a subshell"
	whyBrace      = "the command holds a brace outside quotes, and the hook does not follow a group or a list"
	whyComment    = "the command holds `#` outside quotes, which can start a comment"
	whyHereDoc    = "the command holds a here-document or a here-string, which the hook does not follow"
	whyDollar     = "the command holds a `$` that is not followed by a variable's name; run sdlc or git on a line of its own"
	whyControl    = "the command holds a control character"
	whyUnclosed   = "the command holds a quote that is not closed"
	whyNotASCII   = "the command holds a character outside ASCII where the shell could read it as a quote or a space"
	whyTooMany    = "the command holds more texts and pathspecs than the hook follows on one line"
	whyPowerShell = "the command holds `%`, `@` or `,` outside quotes, which PowerShell can read as more than a character of a word"
)

// A piece is a run of a word the shell reads one way: outside quotes, in
// quotes with nothing in it the shell fills in, or in double quotes with a
// variable.
type pieceKind int

const (
	pieceBare pieceKind = iota
	pieceInert
	pieceLive
)

// namedPiece is one piece with where it sits in the line, quotes included.
// text is what is between the quotes, a slice of the line.
type namedPiece struct {
	kind       pieceKind
	start, end int
	text       string
}

// inside is where the text of the piece starts and ends in the line.
func (p namedPiece) inside() (int, int) {
	if p.kind == pieceBare {
		return p.start, p.end
	}
	return p.start + 1, p.end - 1
}

type namedWord struct{ pieces []namedPiece }

func (w namedWord) end() int { return w.pieces[len(w.pieces)-1].end }

func (w namedWord) text() string {
	if len(w.pieces) == 1 {
		return w.pieces[0].text
	}
	var b strings.Builder
	for _, p := range w.pieces {
		b.WriteString(p.text)
	}
	return b.String()
}

func (w namedWord) bare() bool { return len(w.pieces) == 1 && w.pieces[0].kind == pieceBare }

// is reports a word written outside quotes as exactly this.
func (w namedWord) is(word string) bool { return w.bare() && w.pieces[0].text == word }

// literal reports a word that is what the program will be given.
func (w namedWord) literal() bool {
	for _, p := range w.pieces {
		if p.kind == pieceLive || p.kind == pieceBare && !writtenOut(p.text) {
			return false
		}
	}
	return true
}

type namedRedirect struct {
	op     string
	target *namedWord
}

// namedCommand is one command of the line and the operator that ends it.
type namedCommand struct {
	words      []namedWord
	redirects  []namedRedirect
	start, end int
	next       string
}

func (c namedCommand) texts() []string {
	out := make([]string, len(c.words))
	for i, w := range c.words {
		out[i] = w.text()
	}
	return out
}

// lineScan reads a command from left to right and stops at the first thing it
// does not follow. It is strict on purpose: it is a second reader of shell
// text beside the lexer, and where the two could disagree about what is
// inside a string a mention would be taken out that the shell acts on. So it
// follows words, the two kinds of quote, the operators between commands and
// redirects, and nothing else.
type lineScan struct {
	line       string
	powerShell bool
	i          int
	cmds       []namedCommand
	cur        namedCommand
	word       *namedWord
	pending    *namedRedirect
	begun      bool
	// stop is why the scanner gave up, and runs is set where it gave up at a
	// substitution, which the shell runs and the rules read for themselves.
	stop string
	runs bool
}

func scanLine(line string, powerShell bool) *lineScan {
	n := &lineScan{line: line, powerShell: powerShell}
	for n.i < len(line) && n.stop == "" && !n.runs {
		n.step()
	}
	n.endCommand(len(line), "")
	return n
}

// ahead is the byte after the one being read, or zero at the end. Every look
// ahead goes through it: an index past the end of a line cut short would
// panic, and a panic in the hook answers "allowed".
func (n *lineScan) ahead() byte {
	if n.i+1 < len(n.line) {
		return n.line[n.i+1]
	}
	return 0
}

func (n *lineScan) step() {
	switch c := n.line[n.i]; {
	case c >= 0x80:
		n.stop = whyNotASCII
	case c == ' ' || c == '\t' || n.powerShell && c == '\r':
		n.endWord()
		n.i++
	case c == '\n' || c == ';':
		n.endCommand(n.i, string(c))
		n.i++
	case c == '&' || c == '|':
		n.operator(c)
	case c == '<' || c == '>':
		n.redirect()
	case c == '\'':
		n.single()
	case c == '"':
		n.double()
	case c == '$':
		n.dollar()
	default:
		n.plain(c)
	}
}

// plain takes a byte of a word written outside quotes, or gives up at one
// that means more to a shell than itself.
func (n *lineScan) plain(c byte) {
	switch {
	case c == '`' && n.powerShell:
		n.stop = whyBacktick
	case c == '`':
		n.runs = true
	case c == '\\' && !n.powerShell:
		n.stop = whyEscape
	case c == '(' || c == ')':
		n.stop = whyParen
	case c == '{' || c == '}':
		n.stop = whyBrace
	case c == '#':
		n.stop = whyComment
	// PowerShell is asked by no test and was run by nobody who wrote this,
	// so what it is known to read differently ends the reading: after the
	// stop-parsing token `--%` a single quote is no longer a quote, and
	// `sdlc --% escalate q --message 'a | Remove-Item PATH #'` is two
	// commands; `@` splats or opens a here-string; a comma makes a list.
	case n.powerShell && (c == '%' || c == '@' || c == ','):
		n.stop = whyPowerShell
	case c < 0x20 || c == 0x7f:
		n.stop = whyControl
	default:
		n.add(pieceBare, n.i, n.i+1, n.line[n.i:n.i+1])
		n.i++
	}
}

func (n *lineScan) operator(c byte) {
	next := n.ahead()
	switch {
	case c == '&' && next == '>':
		n.redirect()
	case next == c:
		n.endCommand(n.i, n.line[n.i:n.i+2])
		n.i += 2
	case c == '|' && next == '&':
		n.endCommand(n.i, "|&")
		n.i += 2
	// A lone `&` ends a command here in PowerShell too, where it is the
	// call operator: `& sdlc x` is then nothing, and `sdlc x`. What the
	// operator calls is the word after it, so that reading is right where
	// the word is the bare name, and any other word (a string, a variable,
	// a block) is not taken for sdlc or git by the rules that follow.
	default:
		n.endCommand(n.i, string(c))
		n.i++
	}
}

// redirect takes a redirect's operator. The word that follows is its target,
// and a number written against it is the descriptor it moves, not a word of
// the command.
func (n *lineScan) redirect() {
	at := n.i
	if w := n.word; w != nil && w.end() == at && w.bare() && strings.Trim(w.pieces[0].text, "0123456789") == "" {
		n.word = nil
	}
	n.endWord()
	n.flush()
	end := at
	for end < len(n.line) && strings.IndexByte("<>&|", n.line[end]) >= 0 {
		end++
	}
	op := n.line[at:end]
	if strings.HasPrefix(op, "<<") {
		n.stop = whyHereDoc
		return
	}
	n.begin(at)
	n.cur.end = end
	n.pending = &namedRedirect{op: op}
	n.i = end
}

func (n *lineScan) single() {
	closes := strings.IndexByte(n.line[n.i+1:], '\'')
	if closes < 0 {
		n.stop = whyUnclosed
		return
	}
	end := n.i + 1 + closes
	text := n.line[n.i+1 : end]
	// PowerShell takes the typographic quotes for quotes, inside a string
	// too, so one there may end the string where this reading does not.
	if n.powerShell && !ascii(text) {
		n.stop = whyNotASCII
		return
	}
	n.add(pieceInert, n.i, end+1, text)
	n.i = end + 1
}

func (n *lineScan) double() {
	kind := pieceInert
	for j := n.i + 1; j < len(n.line); j++ {
		switch c := n.line[j]; {
		case c == '"':
			n.add(kind, n.i, j+1, n.line[n.i+1:j])
			n.i = j + 1
			return
		case c == '$' && j+1 < len(n.line) && nameByte(n.line[j+1]):
			kind = pieceLive
		default:
			if n.stop, n.runs = n.inDouble(c, j); n.stop != "" || n.runs {
				return
			}
		}
	}
	n.stop = whyUnclosed
}

// inDouble says why the scanner gives up at a byte inside double quotes, if
// it does: there the shell still expands and still reads an escape.
func (n *lineScan) inDouble(c byte, at int) (stop string, runs bool) {
	switch {
	case c >= 0x80 && n.powerShell:
		return whyNotASCII, false
	case c == '`' && n.powerShell:
		return whyBacktick, false
	case c == '`':
		return "", true
	case c == '$' && at+1 < len(n.line) && n.line[at+1] == '(':
		return "", true
	case c == '$':
		return whyDollar, false
	case c == '\\' && !n.powerShell:
		return whyEscape, false
	}
	return "", false
}

func (n *lineScan) dollar() {
	switch next := n.ahead(); {
	case next == '(':
		n.runs = true
	case !nameByte(next):
		n.stop = whyDollar
	default:
		n.add(pieceBare, n.i, n.i+1, "$")
		n.i++
	}
}

func nameByte(b byte) bool { return b < 0x80 && nameRune(rune(b)) }

func ascii(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func (n *lineScan) begin(at int) {
	if !n.begun {
		n.begun = true
		n.cur.start = at
	}
}

// add puts a piece on the word being read. Bytes outside quotes that touch
// are one piece, and its text is cut from the line again rather than joined:
// joined a byte at a time, a long word cost the square of its length.
func (n *lineScan) add(kind pieceKind, start, end int, text string) {
	n.begin(start)
	if n.word == nil {
		n.word = &namedWord{}
	}
	if last := len(n.word.pieces) - 1; last >= 0 && kind == pieceBare && n.word.pieces[last].kind == pieceBare {
		p := &n.word.pieces[last]
		p.end = end
		p.text = n.line[p.start:end]
		return
	}
	n.word.pieces = append(n.word.pieces, namedPiece{kind: kind, start: start, end: end, text: text})
}

func (n *lineScan) endWord() {
	if n.word == nil {
		return
	}
	w := *n.word
	n.word = nil
	n.cur.end = w.end()
	if n.pending == nil {
		n.cur.words = append(n.cur.words, w)
		return
	}
	n.pending.target = &w
	n.flush()
}

// flush puts a redirect on the command, with its target or without one.
func (n *lineScan) flush() {
	if n.pending != nil {
		n.cur.redirects = append(n.cur.redirects, *n.pending)
		n.pending = nil
	}
}

func (n *lineScan) endCommand(at int, op string) {
	n.endWord()
	n.flush()
	if !n.begun {
		n.cur.start, n.cur.end = at, at
	}
	n.cur.next = op
	n.cmds = append(n.cmds, n.cur)
	n.cur, n.begun = namedCommand{}, false
}

// swap is a stretch of the line and what stands there instead.
type swap struct {
	start, end int
	with       string
}

// doubt is the first mention the scanner was not sure of: why, and whether it
// was a pathspec of git's or the text of an sdlc flag.
type doubt struct {
	why string
	git bool
}

// naming is a command read for what it only names. asked is the command with
// each request for help alone swapped for `sdlc help`; named is asked with
// each mention the scanner is sure of taken out, a text leaving its
// path-shaped words behind; lenient is asked with every mention taken out
// whole, sure or not, and is empty where the scanner was sure of them all.
// lenient is only ever asked whether a refusal is for a doubtful mention.
type naming struct {
	asked, named, lenient string
	doubt                 doubt
	// stop is why the scanner did not follow the line, where it did not.
	stop string
}

func (n naming) rewritten(command string) bool { return n.asked != command || n.named != command }

// mayNameOnly reports a command that could hold something this file reads.
// Most commands hold none, and pay two searches for it.
func mayNameOnly(command string) bool {
	if strings.Contains(command, "git") && strings.Contains(command, ":") {
		return true
	}
	return strings.Contains(strings.ToLower(command), "sdlc")
}

// nameOnly finds what a command only names.
func nameOnly(command string, s State) naming {
	n := naming{asked: command, named: command}
	if !mayNameOnly(command) {
		return n
	}
	scan := scanLine(command, s.PowerShell)
	if scan.runs {
		return n
	}
	if scan.stop != "" {
		n.stop = scan.stop
		return n
	}
	j := &judge{cmds: scan.cmds, carried: s.Carried, rereads: rereads(scan.cmds)}
	j.each()
	if len(j.help)+len(j.sure)+len(j.maybe) > maxSwaps {
		n.stop = whyTooMany
		return n
	}
	n.asked = swapped(command, j.help)
	n.named = swapped(command, slices.Concat(j.help, j.sure))
	if len(j.maybe) > 0 {
		n.lenient = swapped(command, slices.Concat(j.help, takenOut(j.sure), j.maybe))
		n.doubt = j.first
	}
	return n
}

// takenOut is these swaps with nothing left of what each takes out.
func takenOut(swaps []swap) []swap {
	out := slices.Clone(swaps)
	for i := range out {
		out[i].with = namedOnly
	}
	return out
}

// swapped is the command with each stretch swapped. A stretch that overlaps
// the one before it, or does not lie in the command, is left as it is.
func swapped(command string, swaps []swap) string {
	if len(swaps) == 0 {
		return command
	}
	slices.SortFunc(swaps, func(a, b swap) int { return a.start - b.start })
	var b strings.Builder
	at := 0
	for _, s := range swaps {
		if s.start < at || s.end < s.start || s.end > len(command) {
			continue
		}
		b.WriteString(command[at:s.start])
		b.WriteString(s.with)
		at = s.end
	}
	b.WriteString(command[at:])
	return b.String()
}

// rereads says why no mention on a line is sure, where a command on it hands
// text to something that runs it or is one the scanner does not follow: a
// shell, eval, xargs, a loop, a program named by a variable. What such a
// command runs is not on the line to be read.
func rereads(cmds []namedCommand) string {
	for _, c := range cmds {
		if why := rereadsText(c); why != "" {
			return why
		}
	}
	return ""
}

func rereadsText(c namedCommand) string {
	if len(c.words) == 0 {
		return ""
	}
	first := c.words[0]
	if !first.literal() || shellWord(first) {
		return whyConstruct
	}
	run, evals := underEval(program(c.texts()))
	if run.shell || run.piped || evals > 0 || runsHere[base(first.text())] ||
		len(run.words) > 0 && runsHere[base(run.words[0])] {
		return whyRereads
	}
	return ""
}

func shellWord(first namedWord) bool { return shellWords[strings.ToLower(base(first.text()))] }

// filledIn reports a command with a word the shell makes when the command
// runs, or one that runs an earlier line again. A mention stays on the line
// after it is taken out of the reading, and the shell hands the words of one
// command to the next: `sdlc escalate x --message 'rm PATH'; perl -e
// 'system(@ARGV)' "$_"` was let through with the text taken for a mention,
// and removed the file, because `$_` is the last word of the command before.
// A word is the shell's to fill in wherever it is not written out, so this
// asks that of every word and of where each redirect points, and does not
// list the variables.
func filledIn(c namedCommand) bool {
	for _, r := range c.redirects {
		if r.target != nil && !r.target.literal() {
			return true
		}
	}
	if len(c.words) == 0 {
		return false
	}
	// `^old^new` runs the line before with one word changed.
	if shellWord(c.words[0]) || strings.HasPrefix(c.words[0].text(), "^") {
		return true
	}
	return slices.ContainsFunc(c.words, func(w namedWord) bool { return !w.literal() })
}

// runsCode reports a command that is handed a program on the line: an
// interpreter with inline code, or awk and sed with theirs. These are the
// commands the path rules read the whole line for, since the files such a
// program touches are in no argument; and a program can find the words of
// the line it runs on without the shell handing them over.
func runsCode(c namedCommand) bool {
	if len(c.words) == 0 {
		return false
	}
	run, _ := underEval(program(c.texts()))
	return runsInlineCode(run.words, "") || runsAProgramOfItsOwn(run.words)
}

// carries says why a command could be given the words of one before it on
// the line, or nothing.
func carries(c namedCommand) string {
	switch {
	case filledIn(c):
		return whyLaterWord
	case runsCode(c):
		return whyLaterCode
	}
	return ""
}

// wordsOnly is a text with everything but its path-shaped words taken out.
// It is what stands in for the text of an sdlc flag: the operators the
// quote-blind reading took for the shell's are gone, and every word is still
// on the line for a rule that reads the whole of it.
func wordsOnly(text string) string {
	if words := wordsIn(text); len(words) > 0 {
		return strings.Join(words, " ")
	}
	return namedOnly
}

// judge goes through the commands of a line and sorts what each names into
// what is surely a mention and what may be one.
type judge struct {
	cmds    []namedCommand
	carried func(string) string
	rereads string
	// placed is whether every command so far is `cd` or plain sdlc. After
	// anything else, `sdlc` and `git` may no longer be the programs: a
	// function or an alias of that name defined earlier on the line runs
	// instead, and runs the text.
	placed bool
	// after[i] is why a command at i or later could be given the words of
	// one before it, or nothing: see carries.
	after             []string
	help, sure, maybe []swap
	first             doubt
}

func (j *judge) each() {
	j.placed = true
	j.after = make([]string, len(j.cmds)+1)
	for i := len(j.cmds) - 1; i >= 0; i-- {
		j.after[i] = firstOf(carries(j.cmds[i]), j.after[i+1])
	}
	for i, c := range j.cmds {
		if len(c.words) == 0 {
			continue
		}
		here, first := j.placed, c.words[0]
		sdlc := first.is("sdlc") || first.is("sdlc.exe")
		j.placed = j.placed && (sdlc || first.is("cd"))
		switch {
		case sdlc:
			j.sdlc(i, here)
		case first.is("git"):
			j.excludes(c.words[1:], j.gitDoubt(i, here))
		default:
			j.other(c)
		}
	}
}

func (j *judge) unsure(why string, git bool, s swap) {
	if j.first.why == "" {
		j.first = doubt{why: why, git: git}
	}
	j.maybe = append(j.maybe, s)
}

// sdlc reads a command that runs plain sdlc.
func (j *judge) sdlc(i int, here bool) {
	c := j.cmds[i]
	why := j.sdlcDoubt(i, here)
	if why == "" && helpOnly(c) {
		j.help = append(j.help, swap{c.start, c.end, helpAlone})
		return
	}
	j.texts(c.words[1:], why)
}

// sdlcDoubt says what stands against reading a command that runs plain sdlc
// for what it only names, or nothing.
func (j *judge) sdlcDoubt(i int, here bool) string {
	c := j.cmds[i]
	switch {
	case !here:
		return whyAfter
	case j.rereads != "":
		return j.rereads
	case c.next == "&":
		return whyBackground
	case !descriptorsOnly(c):
		return whyToFile
	case !onlyReaders(j.cmds, i):
		return whyToProgram
	}
	return j.after[i+1]
}

// other reads a command that runs neither plain sdlc nor plain git, for a use
// of either under another name: nothing in it is sure, and the refusal says
// why.
func (j *judge) other(c namedCommand) {
	for k, w := range c.words {
		if isSdlc(w.text()) {
			j.texts(c.words[k+1:], whyOtherName)
			return
		}
		if base(w.text()) == "git" {
			j.excludes(c.words[k+1:], whyGitOtherName)
			return
		}
	}
}

// helpOnly reports a command that asks sdlc for help and for nothing else,
// every word of it written out.
func helpOnly(c namedCommand) bool {
	for _, w := range c.words[1:] {
		if !w.literal() {
			return false
		}
	}
	// The repair's help is not believed: it is the one person-only command
	// whose help no test holds to the command tree, so the spelling is read
	// as the command it names. `sdlc help remap` prints the same text.
	return asksForHelp(c.texts()[1:]) && !runsRemap(c.texts())
}

// descriptorsOnly reports a command whose redirects only move one descriptor
// onto another, as `2>&1` does. Any other opens a file.
func descriptorsOnly(c namedCommand) bool {
	for _, r := range c.redirects {
		if r.op != ">&" || r.target == nil || !r.target.bare() {
			return false
		}
		if _, err := strconv.Atoi(r.target.text()); err != nil {
			return false
		}
	}
	return true
}

// onlyReaders reports whether what command i prints stays where nothing can
// run it: on the terminal, or in a short pipeline of programs that only read.
// sdlc prints the text it is given, and `sdlc escalate x --message 'ACT' |
// INTERPRETER` runs it; nothing tells a program that prints what it reads
// from one that runs it, so the list is of the programs known not to.
func onlyReaders(cmds []namedCommand, i int) bool {
	for piped := 0; i < len(cmds); i++ {
		switch cmds[i].next {
		case "", ";", "\n", "&&", "||":
			return true
		case "|":
		default:
			return false
		}
		if piped++; piped > maxReaders || i+1 >= len(cmds) || !reads(cmds[i+1]) {
			return false
		}
	}
	return false
}

// reads reports a command that is one of readers, under its own name, with
// every word written out and no file opened.
func reads(c namedCommand) bool {
	if len(c.words) == 0 || !c.words[0].bare() || !readers[c.words[0].text()] || !descriptorsOnly(c) {
		return false
	}
	for _, w := range c.words {
		if !w.literal() {
			return false
		}
	}
	return true
}

// texts finds the text each text flag among these words is given.
func (j *judge) texts(args []namedWord, why string) {
	dashed := false
	for i := 0; i < len(args); i++ {
		if args[i].is("--") {
			dashed = true
			continue
		}
		name, value, last := flagAndValue(args, i)
		i = last
		if len(value) > 0 && (TextFlags[name] || abbreviatesTextFlag(name)) {
			j.text(name, value, why, dashed)
		}
	}
}

// flagAndValue reads the word at i as a flag and finds the pieces of its
// value where the value is quoted: the rest of the word after `--flag=`, or
// the next word. last is the last word it took.
func flagAndValue(args []namedWord, i int) (name string, value []namedPiece, last int) {
	w := args[i]
	if w.pieces[0].kind != pieceBare {
		return "", nil, i
	}
	if len(w.pieces) > 1 {
		name, glued := strings.CutSuffix(w.pieces[0].text, "=")
		if !glued {
			return "", nil, i
		}
		return name, w.pieces[1:], i
	}
	name = w.pieces[0].text
	takes := ValueFlags[name]
	if !takes && !abbreviatesTextFlag(name) || i+1 >= len(args) {
		return "", nil, i
	}
	if args[i+1].bare() {
		if takes {
			return "", nil, i + 1
		}
		return "", nil, i
	}
	return name, args[i+1].pieces, i + 1
}

func abbreviatesTextFlag(name string) bool {
	for flag := range TextFlags {
		if name != flag && abbreviates(name, flag) {
			return true
		}
	}
	return false
}

// text sorts the value of a text flag. It is surely text when it is one
// string the shell fills nothing into, straight after the flag written in
// full, before any `--`, in a command the caller found nothing against.
func (j *judge) text(name string, value []namedPiece, why string, dashed bool) {
	s := swap{value[0].start, value[len(value)-1].end, namedOnly}
	switch {
	case !TextFlags[name]:
		why = whyShortFlag
	case why != "":
	case dashed:
		why = whyAfterDashes
	case len(value) != 1:
		why = whyPieces
	case value[0].kind == pieceLive:
		why = whyVariable
	default:
		s.with = wordsOnly(value[0].text)
		j.sure = append(j.sure, s)
		return
	}
	j.unsure(why, false, s)
}

// gitDoubt says what stands against the pathspecs of a command that runs
// plain git, or nothing.
func (j *judge) gitDoubt(i int, here bool) string {
	c := j.cmds[i]
	switch {
	case !here:
		return whyAfter
	case j.rereads != "":
		return j.rereads
	case len(c.words) < 2 || !c.words[1].bare() || strings.HasPrefix(c.words[1].text(), "-"):
		return whyGitOption
	case !gitReads[c.words[1].text()]:
		return whyGitCommand
	case slices.ContainsFunc(c.words, func(w namedWord) bool { return w.is("--no-index") }):
		return whyNoIndex
	case j.carried != nil && j.carried("GIT_LITERAL_PATHSPECS") != "":
		return whyLiteral
	}
	if why := j.gitCarried(i); why != "" {
		return why
	}
	return gitReaches(j.cmds, i)
}

// gitCarried says why a command after a git command could be given the path
// it leaves out. The programs git's own listing is piped to may run code of
// their own, since reading the listing with one is what the pathspec is for;
// no other command on the line may, and none at all may hold a word the
// shell fills in.
func (j *judge) gitCarried(i int) string {
	for i+1 < len(j.cmds) && j.cmds[i].next == "|" {
		i++
		if filledIn(j.cmds[i]) {
			return whyLaterWord
		}
	}
	return j.after[i+1]
}

// gitReaches says why what a git command prints could carry the path it
// leaves out to a program that runs it, or nothing.
func gitReaches(cmds []namedCommand, i int) string {
	c := cmds[i]
	switch {
	case onlyReaders(cmds, i):
		return ""
	case c.next == "&":
		return whyBackground
	// What follows is a pipe to a program that could run what it reads. An
	// exclude can only take lines out of what git finds, so the listing is
	// what it would be for any other path left out; but git's errors can
	// quote the pathspec, and they join the pipe through `|&` or `2>&1`.
	case c.next != "|" || len(c.redirects) > 0:
		return whyGitErrors
	}
	return ""
}

// excludes finds the pathspecs among these words that leave a path out, and
// takes the path out of each: the magic stays, so the rules for what a git
// command does to the files it names read the command as they did.
func (j *judge) excludes(args []namedWord, why string) {
	dashed := false
	for _, w := range args {
		if w.is("--") {
			dashed = true
			continue
		}
		if len(w.pieces) != 1 || w.pieces[0].kind == pieceLive {
			continue
		}
		p := w.pieces[0]
		at, odd, found := excludeAt(p.text)
		if !found {
			continue
		}
		from, to := p.inside()
		j.exclude(swap{from + at, to, namedOnly}, firstOf(odd, why), dashed, p)
	}
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (j *judge) exclude(s swap, why string, dashed bool, p namedPiece) {
	switch {
	case why != "":
	case !dashed:
		why = whyNoDashes
	case p.kind == pieceBare && strings.Contains(p.text, "!"):
		why = whyBang
	default:
		j.sure = append(j.sure, s)
		return
	}
	j.unsure(why, true, s)
}

// excludeAt reads a word as a pathspec that leaves a path out, and says where
// the path starts in it. odd is why the hook does not read it, where it holds
// magic or a path the hook does not follow; it is still a candidate then, so
// that the refusal can say so.
func excludeAt(spec string) (at int, odd string, found bool) {
	rest, magic := strings.CutPrefix(spec, ":")
	if !magic {
		return 0, "", false
	}
	if long, ok := strings.CutPrefix(rest, "("); ok {
		return longExclude(spec, long)
	}
	i, excludes := 0, false
	for ; i < len(rest) && strings.IndexByte("!^/", rest[i]) >= 0; i++ {
		excludes = excludes || rest[i] != '/'
	}
	if !excludes || i == len(rest) {
		return 0, "", false
	}
	return 1 + i, oddPath(rest[i:]), true
}

// longExclude reads the `:(magic)path` form. With `exclude` among the magic
// the path is left out whatever else is there, but what else is there can
// change which path that is, so only the magic that does not is read.
func longExclude(spec, long string) (at int, odd string, found bool) {
	magic, path, closed := strings.Cut(long, ")")
	if !closed || path == "" {
		return 0, "", false
	}
	for name := range strings.SplitSeq(magic, ",") {
		switch name {
		case "exclude":
			found = true
		case "top", "icase", "glob":
		default:
			odd = whyMagic
		}
	}
	if odd == "" {
		odd = oddPath(path)
	}
	return len(spec) - len(path), odd, found
}

// oddPath says why the hook does not read the path of a pathspec, or nothing.
// A path with `..` in it names a file by a way round, and a command that
// reads the word as a plain path folds it to the file itself.
func oddPath(path string) string {
	switch {
	case !pathspecPath(path):
		return whyPathChars
	case strings.Contains(path, ".."):
		return whyDotDot
	}
	return ""
}

// pathspecPath reports a path of the characters a path is plainly made of.
// Anything else could mean something to git or to a shell that this reading
// does not know.
func pathspecPath(p string) bool {
	for i := range len(p) {
		c := p[i]
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !letter && strings.IndexByte("._/*?-", c) < 0 {
			return false
		}
	}
	return p != ""
}

// pathRule reports a rule that refuses a command for a path it names. Only
// these can be told the hook is not sure: a decision kept for a person, or a
// substitution, is refused for what the command runs.
func pathRule(rule string) bool {
	switch rule {
	case ruleLoopState, ruleFrozenTest, "protected-path-through-the-tool", "no-new-test-after-the-freeze",
		"implementer-does-not-write-tests":
		return true
	}
	return false
}

// told adds to a refusal that the hook cannot tell whether the path is acted
// on or only named, where that is so: the refusal is for a path, and either
// the line would be let through with the mentions the scanner was not sure of
// taken out, or the scanner did not follow the line and it holds a text flag
// or a pathspec that leaves a path out. A real act beside a doubtful mention
// keeps the words it always had, because the line is refused either way.
func (n naming) told(f Finding, command string, s State) Finding {
	if !pathRule(f.Rule) {
		return f
	}
	switch {
	case n.stop != "":
		if git, held := candidateHeld(command, s.PowerShell); held {
			return cannotTell(f, n.stop, git)
		}
	case n.lenient != "":
		if _, refused := inspectAs(n.asked, n.lenient, s); !refused {
			return cannotTell(f, n.doubt.why, n.doubt.git)
		}
	}
	return f
}

func cannotTell(f Finding, why string, git bool) Finding {
	f.Reason += "; the hook cannot tell whether the command acts on it or only names it, because " + why
	how := "if the path is only named in the text of an sdlc flag, write the command so that the hook can " +
		"tell: sdlc as its own command, run as `sdlc`, with the text in one pair of quotes straight after " +
		"`--message`, `--note`, `--reason` or `--reject`, and what it prints left on the terminal or piped " +
		"only to programs that only read, such as head, tail, cat, grep or wc"
	if git {
		how = "if the path is only named in a pathspec that leaves it out, write the command so that the " +
			"hook can tell: git as its own command, run as `git` with diff, log, show, status, ls-files or " +
			"grep next, the pathspec in quotes after `--`, and what it prints left on the terminal or piped " +
			"with a plain `|` and no redirect"
	}
	f.Route = how + ". Otherwise: " + f.Route
	return f
}

// candidateHeld reports whether a line the scanner did not follow holds a
// text flag of sdlc or a pathspec that leaves a path out, as the lexer reads
// it, and which.
func candidateHeld(command string, powerShell bool) (git, held bool) {
	for _, r := range readings(command, powerShell) {
		runs, _ := programsAt(r.text, powerShell)
		for _, run := range runs {
			if holdsTextFlag(run.words) {
				return false, true
			}
			if holdsExclude(run.words) {
				return true, true
			}
		}
	}
	return false, false
}

func holdsTextFlag(words []string) bool {
	for _, u := range sdlcUses(words) {
		for _, a := range u.args {
			name, _, _ := strings.Cut(a, "=")
			if TextFlags[name] || abbreviatesTextFlag(name) {
				return true
			}
		}
	}
	return false
}

func holdsExclude(words []string) bool {
	if !isGit(words) {
		return false
	}
	for _, a := range words[1:] {
		if _, _, found := excludeAt(a); found {
			return true
		}
	}
	return false
}
