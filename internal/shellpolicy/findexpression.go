package shellpolicy

import (
	"path"
	"strconv"
	"strings"
)

// This file reads the expression find parses, beside the flat reading of
// find.go. find joins its tests with operators -- `!`, `-not`, `-o`, `-or`,
// `-a`, `-and` and parentheses -- and what it picks is what the whole expression
// picks. The flat reading judges each name test by itself; this one adds the
// files the operators between the tests leave in.
//
// The reading here can only add refusals. A negation is read as leaving every
// file in, never as the complement of the wide glob reader of find.go, so the
// reader always picks at least what find picks; narrowing comes only from a
// positive glob test beside it under AND. Where the expression cannot be
// followed -- a word the shell fills in where an operator could be, an option
// the table below does not know, a group that does not balance, a find inside a
// script the reader cannot take apart -- the find is refused from a root that
// holds or could hold a protected file, by the rules that ask.

// maxFindWords bounds the expression the reader will follow. A longer one
// cannot be read, and is refused where the root holds or could hold a protected
// file. docs/enforcement.md gives the number and changes with it.
const maxFindWords = 512

// findTakes reports how many words a find option is given as values, and
// whether the option is one the reader knows at all. An option in neither GNU
// findutils nor BSD/macOS find(1) is unknown, never "no value" and never "one":
// how many of the words after it are its own cannot be told, so a find that
// holds one cannot be followed.
//
// GNU and BSD differ on which options exist and what they take; an option one
// accepts with no value and the other with a number (such as the depth option)
// is read as taking none, so the number is a word the reader meets, which is
// the safe side. The option that reads find's starting places from a file is
// left unknown on purpose: the command does not show where such a find starts.
func findTakes(option string) (values int, known bool) {
	// The newerXY family (GNU): X in a, B, c, m and Y in a, B, c, m, t, each
	// given one reference file or, for t, a timestamp.
	if len(option) == len("-newerXY") && strings.HasPrefix(option, "-newer") {
		x, y := option[6], option[7]
		if strings.IndexByte("aBcm", x) >= 0 && strings.IndexByte("aBcmt", y) >= 0 {
			return 1, true
		}
	}
	switch option {
	// One value, in GNU find and BSD find alike.
	case "-amin", "-anewer", "-atime", "-cmin", "-cnewer", "-ctime", "-fstype", "-gid", "-group",
		"-ilname", "-inum", "-links", "-lname", "-mmin", "-mtime", "-newer", "-perm", "-regex",
		"-iregex", "-samefile", "-size", "-type", "-uid", "-user", "-maxdepth", "-mindepth":
		return 1, true
	// One value, named in one find or the other.
	case "-used", "-xtype", "-printf", "-context", "-regextype", // GNU
		"-flags", "-Btime", "-Bmin", "-Bnewer", "-mnewer", "-xattrname": // BSD
		return 1, true
	// One value: the six globs, and the files find lists to.
	case "-name", "-iname", "-path", "-ipath", "-wholename", "-iwholename",
		"-fprint", "-fprint0", "-fls":
		return 1, true
	// Two values: the file to list to, and the format.
	case "-fprintf":
		return 2, true
	// No value: a test or an action whose next word is a test, an action or an
	// operator. The depth option is here because BSD's optional number would
	// otherwise be read as a value it does not always take.
	case "-empty", "-true", "-false", "-print", "-print0", "-prune", "-daystart", "-follow",
		"-xdev", "-mount", "-noleaf", "-nouser", "-nogroup", "-readable", "-writable",
		"-executable", "-ls", "-quit", "-depth", findDelete,
		"-d", "-acl", "-xattr", "-sparse", "-ignore_readdir_race", "-noignore_readdir_race",
		"-warn", "-nowarn", "-nocolor":
		return 0, true
	}
	return 0, false
}

// findAction reports whether a find option is an action find runs, rather than
// a test that only narrows: an alternative that holds one still counts as
// picking a file, even where its test does not.
func findAction(option string) bool {
	switch option {
	case findDelete, "-print", "-print0", "-printf", "-ls", "-quit",
		"-fprint", "-fprint0", "-fprintf", "-fls",
		"-exec", "-execdir", "-ok", "-okdir":
		return true
	}
	return false
}

// findOp is one of the operators that combine an expression, left on a postfix
// list for picks to evaluate. opNone marks an operand.
type findOp int

const (
	opNone findOp = iota
	opNot
	opAnd
	opOr
)

// A findItem is one entry of the postfix expression: an operand (a test or an
// action) or an operator.
type findItem struct {
	op     findOp
	isGlob bool         // the operand is a glob test, judged per file
	glob   *globPattern // its pattern, nil where the glob cannot be read
	whole  bool         // a -path glob, matched against the whole path
	pick   bool         // a non-glob operand: find's test is read as always able to match
	action bool         // the operand is an action find runs
	// empty and dir let the checkout reading answer two tests a directory
	// holding a checkout fails: -empty, and a -type that is not d.
	empty  bool
	notDir bool
}

// findCause is why an expression could not be followed, which is what the
// refusal's route turns on: a person told to write a word out in quotes can do
// nothing with that where the cause is an option the reader does not know.
type findCause int

const (
	causeNone   findCause = iota
	causeFilled           // a word the shell fills in where an operator could be
	causeScript           // the find is inside something the reader cannot take apart
	causeStray            // a word that is neither a test nor an operator
	causeShape            // the expression's own shape
	causeDiffer           // the two readings of the command do not agree
)

// A findExpression is one find's expression read: the postfix list of its
// nodes, and, where it could not be followed, the first word it could not
// follow and why. The zero value is an expression that was not followed.
type findExpression struct {
	postfix  []findItem
	operated bool      // a negation, an alternation or a group was read
	followed bool      // the whole expression could be read
	cause    findCause // why not, where it could not
	word     string    // the first word that could not be followed, as spelled
	why      string    // why it could not be followed
	// unknown is the first option the reader did not know, where it met one.
	// Such an expression is never followed, whatever else is recorded.
	unknown string
	stack   []pickAct
}

// unfollowed is an expression that could not be read, and why.
func unfollowed(cause findCause, word, why string) findExpression {
	return findExpression{cause: cause, word: word, why: why}
}

// pickAct is one value on the evaluation stack: whether the subexpression could
// be true for the file, and whether an action in it could be reached.
type pickAct struct{ pick, act bool }

// picks reports whether the expression could pick a file: it could be true for
// it, or an action in it could be reached. operand answers whether each test or
// action could be true for the file.
func (e *findExpression) picks(operand func(it findItem) bool) bool {
	st := e.stack[:0]
	for _, it := range e.postfix {
		switch it.op {
		case opNone:
			st = append(st, pickAct{operand(it), it.action})
		default:
			var ok bool
			if st, ok = applyPick(st, it.op); !ok {
				return false
			}
		}
	}
	e.stack = st
	if len(st) == 0 {
		return false
	}
	top := st[len(st)-1]
	return top.pick || top.act
}

// applyPick applies an operator to the values on top of the evaluation stack,
// and is not ok where the stack holds fewer than the operator takes.
func applyPick(st []pickAct, op findOp) ([]pickAct, bool) {
	switch op {
	case opNot:
		if len(st) < 1 {
			return st, false
		}
		a := st[len(st)-1]
		st[len(st)-1] = pickAct{true, a.act}
	case opAnd:
		if len(st) < 2 {
			return st, false
		}
		b := st[len(st)-1]
		a := st[len(st)-2]
		st = st[:len(st)-1]
		st[len(st)-1] = pickAct{a.pick && b.pick, a.act || (a.pick && b.act)}
	case opOr:
		if len(st) < 2 {
			return st, false
		}
		b := st[len(st)-1]
		a := st[len(st)-2]
		st = st[:len(st)-1]
		st[len(st)-1] = pickAct{a.pick || b.pick, a.act || b.act}
	default:
		// No operator: the stack stands as it is.
	}
	return st, true
}

// pickedShapes is the shapes among shapes the expression picks, from any of the
// roots, matched the way foundBy matches: a -name glob against a shape's base
// name, a -path glob against the whole path find comes to it by.
func (e *findExpression) pickedShapes(roots, shapes []string) []string {
	var out []string
	for _, shape := range shapes {
		for _, root := range roots {
			if e.picks(func(it findItem) bool {
				if it.isGlob {
					return matchesShape(it.glob, it.whole, shape, root)
				}
				return it.pick
			}) {
				out = append(out, shape)
				break
			}
		}
	}
	return out
}

// matchesShape reports whether a glob test picks shape from root, as foundBy
// reads it. A glob that cannot be read answers true: it says nothing of what it
// picks, and under the wide reading that is "could pick".
func matchesShape(g *globPattern, whole bool, shape, root string) bool {
	if g == nil {
		return true
	}
	if !whole {
		return g.MatchString(path.Base(shape))
	}
	full := strings.TrimSuffix(clean(root), "/") + "/" + shape
	if r := clean(root); r != "." && strings.HasPrefix(shape, r+"/") {
		full = shape
	}
	return g.MatchString(full)
}

// a token is one word of a find as the reader takes it from the command: the
// word as the command spells it, the word as find receives it, whether it is a
// marker for a separator the shell hid from find (a closing parenthesis or an
// -exec terminator), and whether the shell fills in or splits the word.
type token struct {
	raw        string
	word       string
	fields     int  // the words parse makes of it: more than one for a quoted word holding white space
	marker     byte // 0, or ')' ';' '|' '&' for a separator the shell hid from find
	filled     bool // the shell expands or unescapes part of it
	splittable bool // the shell could split it into several words
}

// marker bytes stand for the separators findSpelling leaves in the text, so
// the reader sees where a group or an -exec command ends. The replacer that
// places them does not follow quotes, so tokens accepts one only where the
// quote tracker says the shell would hand find the separator as a word.
const (
	markClose = '\x1c' // a closing parenthesis
	markSemi  = '\x1d' // an -exec terminator
	markPipe  = '\x1e'
	markAmp   = '\x1f'
)

// markerSep is the separator a marker stands for, where field is a marker
// alone or a marker in quotes of its own, or 0.
func markerSep(field string) byte {
	if len(field) == 3 && isQuote(field[0]) && field[2] == field[0] {
		field = field[1:2]
	}
	if len(field) != 1 {
		return 0
	}
	switch field[0] {
	case markClose:
		return ')'
	case markSemi:
		return ';'
	case markPipe:
		return '|'
	case markAmp:
		return '&'
	}
	return 0
}

// asSplit is the spelled text as the path rules' splitter leaves it for parse:
// each marker, with the quotes of its own where it has them, turned to spaces,
// and each redirect the splitter rewrites (`>|`, `>&`, `&>`) written as a plain
// one. Nothing is added or taken out, so an offset into one text is an offset
// into the other.
func asSplit(sp string) string {
	b := []byte(sp)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] >= markClose && b[i] <= markAmp:
			if i > 0 && i+1 < len(b) && isQuote(b[i-1]) && b[i+1] == b[i-1] {
				b[i-1], b[i+1] = ' ', ' '
			}
			b[i] = ' '
		case i+1 < len(b) && b[i] == '>' && (b[i+1] == '|' || b[i+1] == '&'):
			b[i+1] = ' '
			i++
		case i+1 < len(b) && b[i] == '&' && b[i+1] == '>':
			b[i] = ' '
			i++
		}
	}
	return string(b)
}

// A quoteLevel follows the quotes of one shell reading a text, a byte at a
// time. It is the reader's only account of where a quoted word ends, and it is
// never restarted partway through a command.
type quoteLevel struct {
	quote byte // the quote character that is open, or 0
	ansi  bool // the open quote is $'...', where a backslash escapes
	esc   bool // the byte before was a backslash that takes this one
	tick  bool // a backtick with no partner is open
	psub  int  // process substitutions open
}

func (l *quoteLevel) step(s string, i int) {
	c := s[i]
	switch {
	case l.esc:
		l.esc = false
	case l.quote == '\'':
		if c == '\'' {
			l.quote, l.ansi = 0, false
		} else if l.ansi && c == '\\' {
			l.esc = true
		}
	case c == '\\':
		l.esc = true
	case c == '`':
		l.tick = !l.tick
	case l.quote == '"':
		if c == '"' {
			l.quote = 0
		}
	case c == '\'' || c == '"':
		l.quote, l.ansi = c, c == '\'' && i > 0 && s[i-1] == '$'
	case c == '(' && i > 0 && (s[i-1] == '<' || s[i-1] == '>'):
		l.psub++
	case c == ')' && l.psub > 0:
		l.psub--
	}
}

// balanced reports whether the shell is outside every quote and substitution.
func (l *quoteLevel) balanced() bool {
	return l.quote == 0 && !l.tick && l.psub == 0
}

// quoting is two shells' quotes followed together: the one the command is
// given to, and, while that one is inside a quoted word, a shell that would be
// handed the word as a script. That is as deep as the reader goes.
type quoting struct{ out, in quoteLevel }

// script is the quote of the word the outer shell is inside, where that word
// is one a shell could be handed to run, or 0.
func (q *quoting) script() byte {
	if q.out.ansi {
		return 0
	}
	return q.out.quote
}

func (q *quoting) step(s string, i int) {
	was := q.script()
	q.out.step(s, i)
	switch now := q.script(); {
	case was == 0 && now != 0:
		q.in = quoteLevel{}
	case was != 0 && now == was:
		q.in.step(s, i)
	}
}

// A findPiece is one stretch of the spelled text between two of the separators
// splitSegments ends a command at, with the quote state at either end.
type findPiece struct {
	from, to int     // the stretch, as offsets into the spelled text
	q0, q1   quoting // the quotes open before its first byte and after its last
	// joined is set where the separator after the stretch is the `&` of `<&`,
	// which duplicates a descriptor and ends no command.
	joined bool
}

// separatorAt reads the spelled text at i as splitSegments' replacer does, in
// the same order: how many bytes stand there together, whether they end a
// command, and how many of them the next stretch keeps on its front as
// markedSegments does (`)`, a backtick, and the parenthesis of `$(`).
func separatorAt(s string, i int) (n int, ends bool, kept int) {
	rest := s[i:]
	switch {
	case strings.HasPrefix(rest, ">|"), strings.HasPrefix(rest, ">&"), strings.HasPrefix(rest, "&>"):
		return 2, false, 0
	case strings.HasPrefix(rest, "&&"), strings.HasPrefix(rest, "||"):
		return 2, true, 0
	case rest[0] == ';' || rest[0] == '|' || rest[0] == '&' || rest[0] == '\n':
		return 1, true, 0
	case rest[0] == '`' || rest[0] == ')':
		return 1, true, 1
	case strings.HasPrefix(rest, "$("):
		return 2, true, 1
	}
	return 1, false, 0
}

// A findScan is one reading's command as the find expression reader takes it:
// the spelled text, cut where the path rules cut their own, with the quotes
// followed across the whole of it once.
type findScan struct {
	sp     string // findSpelling's text
	flat   string // the same text as parse is to read it
	pieces []findPiece
	// lined is whether the pieces are the path rules' segments, one for one and
	// word for word; differ is the first word that is not, where one is not.
	lined  bool
	differ string
	// next is, for each piece, the first piece from it on that ends outside
	// every quote, or -1; made the first time it is asked for.
	next []int
	// budget bounds the bytes read for the finds of one command, which a
	// command of finds inside one another's quotes, or of finds the reader
	// gives up on, could otherwise make the reader go over once for each.
	budget int
}

// newFindScan cuts the spelled text into its pieces and lines them up with
// plain, the path rules' segments of the same command. Each byte is looked at
// once and each piece parsed once.
func newFindScan(sp string, plain []string) *findScan {
	sc := &findScan{sp: sp, flat: asSplit(sp), budget: 32*len(sp) + 4096}
	var q quoting
	from, q0 := 0, q
	cut := func(to int, joined bool) {
		// A blank stretch is no command, as in splitSegments.
		if strings.TrimSpace(sp[from:to]) != "" {
			sc.pieces = append(sc.pieces, findPiece{from: from, to: to, q0: q0, q1: q, joined: joined})
		}
	}
	// i moves on by at least one byte a turn: separatorAt never answers 0.
	for i := 0; i < len(sp); {
		n, ends, kept := separatorAt(sp, i)
		if ends {
			cut(i, n == 1 && sp[i] == '&' && i > 0 && sp[i-1] == '<')
		}
		for ; n > kept; n-- {
			q.step(sp, i)
			i++
		}
		if ends {
			from, q0 = i, q
		}
		for ; n > 0; n-- {
			q.step(sp, i)
			i++
		}
	}
	cut(len(sp), false)
	sc.lineUp(plain)
	return sc
}

// lineUp sets lined where every piece has the words of the plain segment in
// the same place. There is no search and no second try: a find is read from the
// piece that stands where its segment stands, or not at all.
func (sc *findScan) lineUp(plain []string) {
	if len(sc.pieces) != len(plain) {
		return
	}
	var tickPlain, tickPiece bool
	for i, p := range sc.pieces {
		segment, _, _ := subshellMarks(plain[i], &tickPlain)
		piece, _, _ := subshellMarks(sc.flat[p.from:p.to], &tickPiece)
		want, _, _ := parse(segment)
		got, _, _ := parse(piece)
		if at, same := sameWords(got, want); !same {
			sc.differ = at
			return
		}
	}
	sc.lined = true
}

// sameWords reports whether two lists of words are the same, a substitution's
// placeholder aside: the spelled text leaves one for a substitution inside a
// word, where the path rules' text leaves nothing. Where they are not, it
// returns the first word that one has and the other does not have in the same
// place.
//
// A word with nothing in it is passed over on both sides. parse makes one of a
// quote character standing alone, which is what the closing quote of a script
// is where a separator the shell hid from find stands before it; the reader
// ends the script at that quote and has no word for it.
func sameWords(got, want []string) (string, bool) {
	i := 0
	for _, w := range want {
		if w = strings.ReplaceAll(w, "$SUB", ""); w == "" {
			continue
		}
		for i < len(got) && strings.ReplaceAll(got[i], "$SUB", "") == "" {
			i++
		}
		if i == len(got) || strings.ReplaceAll(got[i], "$SUB", "") != w {
			return w, false
		}
		i++
	}
	for ; i < len(got); i++ {
		if strings.ReplaceAll(got[i], "$SUB", "") != "" {
			return got[i], false
		}
	}
	return "", true
}

// wordFields are the fields of a segment that parse reads as words of the
// command: not a redirect or its target, and not an assignment in front.
func wordFields(segment string) []field {
	var out []field
	var words []string
	fields := spacedFields(segment)
	leading := true
	for i := 0; i < len(fields); i++ {
		f := unquote(fields[i].text)
		switch {
		case f == ">" || f == "<":
			i++
		case isAssignment(f) && (leading || exporting(words)):
		default:
			leading = false
			words = append(words, f)
			out = append(out, fields[i])
		}
	}
	return out
}

// fieldWords are the words parse makes of one word of a find as the command
// spells it, and whether it ends in a redirect whose target is the word after.
func fieldWords(text string) (words []string, open bool) {
	fields := spacedFields(text)
	for i := 0; i < len(fields); i++ {
		if f := fields[i].text; f == ">" || f == "<" {
			open = i+1 == len(fields)
			i++
			continue
		}
		words = append(words, unquote(fields[i].text))
	}
	return words, open
}

// A foundFind is what the scan makes of one find: its expression, and, where
// the rules are to be asked again with more words than its own segment has,
// those words and the text they were made from. That is a find that runs on
// past the end of its segment, and a find the reader gave up on, whose words
// then run to the end of the command (toEnd). spent is a find given up on once
// the scan's budget was gone, for which no words were made.
type foundFind struct {
	exp          findExpression
	more         bool
	words        []string
	segment      string
	toEnd, spent bool
}

const (
	whyDiffer = "the hook reads the command twice, as the shell splits it and as find is handed it, and " +
		"the two readings part ways"
	whyNoStart = "the hook could not tell where the find starts in the command"
)

// read reads the find of the segment at idx, whose words, as the path rules
// parsed them, are words. evals is how many `eval` stood in front of it.
//
// A find is followed only where every one of these holds, and each is an exit
// of its own that answers "not followed": the pieces line up with the
// segments; the find stands outside every quote, or one quote deep in a script
// written as one word; its end can be found; and tokens accounts for every
// word of it.
//
// A find the reader gives up on here is one whose end it does not know, and an
// end it worked out before giving up is not trusted either. The rules are then
// to judge it with every word to the end of the command: its own segment, as
// the path rules cut it, and after it the text from where that segment ends,
// or the whole text where the reader cannot say where the segment stands.
// Judged on its segment alone, such a find with a separator inside a quoted
// word had its action cut off, and was read as one that changes nothing.
func (sc *findScan) read(idx int, segment string, words []string, evals int) foundFind {
	var out foundFind
	// from is where the find's own piece ends in the text.
	from := 0
	fail := func(cause findCause, word, why string) foundFind {
		return sc.givenUp(segment, from, cause, word, why)
	}
	if !sc.lined || idx >= len(sc.pieces) || len(words) == 0 {
		return fail(causeDiffer, sc.differ, whyDiffer)
	}
	// The word that is the program: as many words from the end of the piece as
	// the find has.
	p := sc.pieces[idx]
	from = p.to
	fields := wordFields(sc.flat[p.from:p.to])
	n := len(fields) - len(words)
	if n < 0 {
		return fail(causeDiffer, words[0], whyNoStart)
	}
	q := p.q0
	script, opened, a, why := sc.standing(&q, p.from, p.from+fields[n].at)
	if why != "" {
		return fail(causeScript, "", why)
	}
	if evalParsesAgain(evals, opened, n, fields) {
		return fail(causeScript, "eval", "the script given to eval is not one quoted word, so eval parses it a second time")
	}
	b, why := sc.end(idx, a, q, script)
	if why != "" {
		return fail(causeScript, "", why)
	}
	if sc.budget -= b - a; sc.budget < 0 {
		return fail(causeShape, "", "the command holds more finds inside one another's quotes than the hook reads")
	}
	if b > p.to {
		// The find runs on past where the path rules ended its segment: its own
		// words are made from all of it, for the rules to be asked again with.
		all, _, _ := parse(sc.flat[a:b])
		full, _ := underEval(program(all))
		if len(full.words) == 0 || base(full.words[0]) != "find" {
			return fail(causeDiffer, words[0], whyNoStart)
		}
		out.more, out.words, out.segment = true, full.words, sc.flat[a:b]
		words = full.words
	}
	if substitutesAProcess(sc.sp[a:b]) {
		return fail(causeScript, "", "the find is given a process substitution")
	}
	tokens, cause, word, why := sc.tokens(a, b, q, script, words)
	if cause != causeNone {
		return fail(cause, word, why)
	}
	out.exp = readFind(tokens)
	return out
}

// substitutesAProcess reports whether the text of a find holds a process
// substitution, `<(` or `>(`.
func substitutesAProcess(text string) bool {
	return strings.Contains(text, "<(") || strings.Contains(text, ">(")
}

// givenUp is the answer for a find the reader gives up on: not followed, and
// with every word to the end of the command for the rules to judge it by.
// from is where the find's own piece ends in the text.
func (sc *findScan) givenUp(segment string, from int, cause findCause, word, why string) foundFind {
	out := foundFind{exp: unfollowed(cause, word, why)}
	// The bytes to the end are charged before they are read. Past the budget
	// nothing is made: the command spends the budget itself, so the answer
	// there is the caller's, and reads nothing of the command.
	if sc.budget -= len(sc.flat) - from; sc.budget < 0 {
		out.spent = true
		return out
	}
	out.segment = segment + " " + sc.flat[from:]
	all, _, _ := parse(out.segment)
	full, _ := underEval(program(all))
	out.more, out.toEnd, out.words = true, true, full.words
	return out
}

// standing follows the quotes from the start of the find's piece, at from,
// to a, where the find starts, and says how deep in quotes the find stands:
// the quote of the script it is in, whether that quote opens at a, and with
// it where the find starts inside the quote. why is set where the find
// cannot be followed from there.
func (sc *findScan) standing(q *quoting, from, a int) (script byte, opened bool, at int, why string) {
	for i := from; i < a; i++ {
		q.step(sc.sp, i)
	}
	// How deep in quotes the find stands.
	script = q.script()
	switch {
	case script != 0:
		if !q.in.balanced() || isQuote(sc.sp[a]) {
			return script, false, a, "the find is in a script inside a script"
		}
	case !q.out.balanced():
		return script, false, a, "a quote or a substitution is still open where the find starts"
	case isQuote(sc.sp[a]):
		q.step(sc.sp, a)
		a++
		script, opened = q.script(), true
	}
	return script, opened, a, ""
}

// evalParsesAgain reports whether a find that evals stand in front of is not
// one eval's single quoted word: more than one eval, a find that is not at
// the opening of a quote, or a word before it that is not eval.
func evalParsesAgain(evals int, opened bool, n int, fields []field) bool {
	return evals > 0 && (evals > 1 || !opened || n == 0 || base(unquote(fields[n-1].text)) != "eval")
}

// end is where the find that starts at a ends, or why that cannot be told.
// Outside a script it is the end of the first piece, from its own on, that
// ends outside every quote: a separator inside a quoted word, or inside a
// process substitution, ends no command. Inside a script it is the first such
// piece end as the script's own shell reads its quotes, or where the script's
// quote closes, which has to be the end of the script's word.
func (sc *findScan) end(idx, a int, q quoting, script byte) (int, string) {
	if script == 0 {
		return sc.endOutside(idx)
	}
	return sc.endInScript(idx, a, q, script)
}

// endOutside is end for a find that stands outside every quote.
func (sc *findScan) endOutside(idx int) (int, string) {
	if sc.next == nil {
		sc.next = make([]int, len(sc.pieces)+1)
		sc.next[len(sc.pieces)] = -1
		for k := len(sc.pieces) - 1; k >= 0; k-- {
			sc.next[k] = sc.next[k+1]
			if sc.pieces[k].q1.out.balanced() && !sc.pieces[k].joined {
				sc.next[k] = k
			}
		}
	}
	if k := sc.next[idx]; k >= 0 {
		return sc.pieces[k].to, ""
	}
	return 0, "a quote in the find is never closed"
}

// endInScript is end for a find inside a script written as one quoted word.
func (sc *findScan) endInScript(idx, a int, q quoting, script byte) (int, string) {
	// One pass from the find to the end of the script at the most, and the
	// budget stops a command that asks for it over and over.
	k := idx
	for i := a; i < len(sc.sp); i++ {
		if sc.budget--; sc.budget < 0 {
			return 0, "the command holds more finds inside one another's quotes than the hook reads"
		}
		for k < len(sc.pieces) && sc.pieces[k].to < i {
			k++
		}
		if k < len(sc.pieces) && sc.pieces[k].to == i && q.in.balanced() && !sc.pieces[k].joined {
			return i, ""
		}
		q.step(sc.sp, i)
		if q.script() != script {
			return sc.scriptClosedAt(i, k, &q)
		}
	}
	return 0, "the script's quote is never closed"
}

// scriptClosedAt is the end of a find whose script's quote closes at i, in
// or before piece k, or why that is no end.
func (sc *findScan) scriptClosedAt(i, k int, q *quoting) (int, string) {
	// The script's quote has closed. Whatever follows it in the command is
	// no word of the script.
	if !q.in.balanced() {
		return 0, "a quote inside the script is never closed"
	}
	rest := len(sc.sp)
	if k < len(sc.pieces) && sc.pieces[k].from <= i {
		rest = sc.pieces[k].to
	}
	if after, _ := fieldWords(sc.flat[i+1 : rest]); len(after) > 0 {
		return 0, "the script's quote closes before the end of the command it is given to"
	}
	return i, ""
}

// tokens are the words of the find that stands in the spelled text from a to
// b, the program among them left off, or why they cannot be told. words are the
// find's own words as parse made them, and every one has to be accounted for by
// exactly one token, a token of several fields counting that many: a word
// swallowed into a quote the reader did not follow shows as a list that does
// not agree.
func (sc *findScan) tokens(a, b int, q quoting, script byte, words []string) ([]token, findCause, string, string) {
	ts := tokenScan{sc: sc, a: a, b: b, script: script}
	level := &q.out
	if script != 0 {
		level = &q.in
	}
	start, cause, word, why := ts.split(&q, level)
	if cause != causeNone {
		return nil, cause, word, why
	}
	if start >= 0 {
		if cause, word, why := ts.emit(start, b); cause != causeNone {
			return nil, cause, word, why
		}
	}
	if !level.balanced() {
		return nil, causeScript, "", "a quote in the find is never closed"
	}
	return ts.agreed(words)
}

// tokenScan is tokens part way through the find's text: the tokens made so
// far, the fields the path rules would make of the same text, and whether
// the word before ended in a redirect whose target is still to come.
type tokenScan struct {
	sc      *findScan
	a, b    int
	script  byte
	out     []token
	got     []string
	pending bool
}

// blankAt reports whether the character at sp[i] is white space.
func blankAt(sp string, i int) bool {
	return sp[i] == ' ' || sp[i] == '\t' || sp[i] == '\n' || sp[i] == '\r'
}

// split cuts the find's text into words at the white space outside every
// quote of level, which is one of q's, and makes a token of each. It returns
// where a word still open at the end of the text starts, or -1, and the
// cause of the first word no token could be made of.
func (ts *tokenScan) split(q *quoting, level *quoteLevel) (int, findCause, string, string) {
	sp := ts.sc.sp
	start := -1
	for i := ts.a; i < ts.b; i++ {
		space := level.quote == 0 && !level.esc && blankAt(sp, i)
		if space && start >= 0 {
			if cause, word, why := ts.emit(start, i); cause != causeNone {
				return start, cause, word, why
			}
			start = -1
		} else if !space && start < 0 {
			start = i
		}
		q.step(sp, i)
	}
	return start, causeNone, "", ""
}

// emit makes a token of the word from start to end, or says why none can be
// made.
func (ts *tokenScan) emit(start, end int) (findCause, string, string) {
	raw := ts.sc.sp[start:end]
	if sep := markerSep(raw); sep != 0 {
		return ts.separator(raw, sep, start, end)
	}
	if strings.ContainsAny(raw, "\x1c\x1d\x1e\x1f") {
		return causeScript, "", "a quoted or escaped separator is part of a longer word"
	}
	if ts.script == '"' && (strings.Contains(raw, `\\`) || strings.Contains(raw, `\"`)) {
		return causeScript, raw, "a script in double quotes holds a backslash before a quote or another backslash"
	}
	fields, open := fieldWords(ts.sc.flat[start:end])
	if ts.pending && len(fields) > 0 {
		// The target of the redirect that ended the word before.
		fields, ts.pending = fields[1:], false
	}
	ts.pending = ts.pending || open
	if len(fields) == 0 {
		return causeNone, "", ""
	}
	ts.got = append(ts.got, fields...)
	filled, splittable, _, _ := shellFill(raw, 0, false)
	// What a script in double quotes holds, the outer shell fills in before
	// the script's own shell reads a word of it.
	filled = filled || ts.script == '"' && strings.ContainsAny(raw, "$`")
	ts.out = append(ts.out, token{raw: raw, word: unquote(raw), fields: len(fields), filled: filled, splittable: splittable})
	return causeNone, "", ""
}

// separator makes the token of a separator the shell hid from find, sep,
// spelled raw from start to end.
func (ts *tokenScan) separator(raw string, sep byte, start, end int) (findCause, string, string) {
	sp := ts.sc.sp
	// The replacer puts a space on each side of an escaped separator, so
	// one written against another word shows as that word two bytes off.
	if len(raw) == 1 && (start-2 >= ts.a && !blankAt(sp, start-2) || end+1 < ts.b && !blankAt(sp, end+1)) {
		return causeScript, string(sep), "an escaped separator is written against another word"
	}
	ts.out = append(ts.out, token{raw: string(sep), word: string(sep), marker: sep})
	return causeNone, "", ""
}

// agreed is the tokens after the program's own, once every word the path
// rules made of the find is accounted for by one of them, or why they are
// not.
func (ts *tokenScan) agreed(words []string) ([]token, findCause, string, string) {
	got, out := ts.got, ts.out
	if len(got) > 0 {
		got[0] = strings.TrimLeft(got[0], "({!")
	}
	if word, same := sameWords(got, words); !same {
		return nil, causeDiffer, word, whyDiffer
	}
	if len(out) == 0 || out[0].fields != 1 || out[0].filled {
		return nil, causeDiffer, words[0], whyNoStart
	}
	return out[1:], causeNone, "", ""
}

// shellFill follows the quotes of one word and reports whether the shell fills
// it in or unescapes part of it, and whether it could then split into several
// words (an expansion outside double quotes). It is unreadWord's scan, told
// where it begins and returning where it ends.
func shellFill(word string, quote byte, ansi bool) (filled, splittable bool, outQuote byte, outAnsi bool) {
	for i := 0; i < len(word); i++ {
		switch c := word[i]; {
		case quote != 0 && c == quote:
			quote, ansi = 0, false
		case quote == '\'':
			if ansi && c == '\\' {
				i++
			}
		case shellSpells(word, i):
			filled, splittable = true, splittable || quote != '"'
			if c == '\\' {
				// The byte a backslash takes is no quote, whatever it is.
				i++
			}
		case c == '$':
			// $'...' and $"..." are quotes the shell reads and then the word is
			// one word: filled, and splittable only outside double quotes.
			filled, splittable = true, splittable || quote != '"'
			quote, ansi = word[i+1], word[i+1] == '\''
			i++
		case opensAQuote(quote, c):
			quote, ansi = c, false
		}
	}
	return filled, splittable, quote, ansi
}

// opensAQuote reports whether c, read outside every quote, opens one.
func opensAQuote(quote, c byte) bool {
	return quote == 0 && (c == '\'' || c == '"')
}

// readFind reads a find's words, after the program, into a postfix list for
// picks, or records the first word it could not follow.
func readFind(tokens []token) findExpression {
	var e findExpression
	// After the leading flags, the roots: a word that is neither an option nor
	// an operator. A word the shell fills in is a directory only as the first
	// of them; a later one stands where an operator could be.
	i := 0
	for i < len(tokens) && isLeadingFlag(tokens[i].word) {
		i++
	}
	rootsSeen := 0
	for i < len(tokens) {
		t := tokens[i]
		if t.marker != 0 || strings.HasPrefix(t.word, "-") || findOperator(t.word) {
			break
		}
		if (t.filled || t.splittable) && rootsSeen > 0 {
			return unfollowed(causeFilled, t.raw, "the command gives find more than one place to start, one the shell fills in")
		}
		rootsSeen++
		i++
	}
	// The expression, read into postfix with an operator stack. More than
	// maxFindWords words cannot be followed.
	if len(tokens)-i > maxFindWords {
		return unfollowed(causeShape, "", "the expression is longer than "+strconv.Itoa(maxFindWords)+" words")
	}
	e.readExpression(tokens[i:])
	return e
}

// isLeadingFlag reports whether a word is one of find's options before the
// directories: -H, -L, -P, -D, -O3.
func isLeadingFlag(word string) bool {
	return word == "-H" || word == "-L" || word == "-P" || word == "-D" || strings.HasPrefix(word, "-O")
}

// readExpression reads tokens into the postfix list, or records the first word
// it could not follow. It is a shunting-yard with an operator stack and no
// recursion, so a crafted depth of groups cannot overflow it. Its last
// statement is the one place an expression is called followed.
func (e *findExpression) readExpression(tokens []token) {
	y := operatorYard{e: e, wantOperand: true}
	for i := 0; i < len(tokens); i++ {
		next, ok := y.read(tokens, i)
		if !ok {
			return
		}
		i = next
	}
	if !y.finish() {
		return
	}
	// An expression that holds an option the reader does not know is not
	// followed: how many words the option took cannot be told, so neither can
	// what the words after it are.
	e.followed = e.unknown == ""
}

// operatorYard is readExpression part way through the tokens: the operators
// still to be written to the postfix list, and whether a test, an action or
// what opens one is what may stand next.
type operatorYard struct {
	e           *findExpression
	ops         []findOp // operators and group marks (opNone marks a `(`)
	wantOperand bool
}

// fail records the first word that could not be followed, and why. It
// answers false, for the caller to hand on.
func (y *operatorYard) fail(cause findCause, word, why string) bool {
	y.e.cause, y.e.word, y.e.why = cause, word, why
	return false
}

// popTo writes out the operators on the stack that bind at least as tightly
// as prec, down to the nearest group mark.
func (y *operatorYard) popTo(prec int) {
	for len(y.ops) > 0 {
		top := y.ops[len(y.ops)-1]
		if top == opNone || opPrec(top) < prec {
			return
		}
		y.e.postfix = append(y.e.postfix, findItem{op: top})
		y.ops = y.ops[:len(y.ops)-1]
	}
}

// and puts the AND find reads between two tests written side by side, where
// one has just been read.
func (y *operatorYard) and() {
	if !y.wantOperand {
		y.popTo(opPrec(opAnd))
		y.ops = append(y.ops, opAnd)
	}
}

// read reads the token at i, and with it the values a test or an action
// takes, and returns the index of the last token it read. It is not ok at
// the first word it could not follow, which it has recorded.
func (y *operatorYard) read(tokens []token, i int) (int, bool) {
	t := tokens[i]
	switch {
	case t.marker == ')':
		return i, y.closeGroup()
	case t.marker != 0:
		return i, y.fail(causeStray, t.raw, "a separator where find reads a test, an action or an operator")
	case t.word == "(" || t.word == `\(`:
		y.prefix(opNone)
	case t.word == "!" || t.word == `\!` || t.word == "-not":
		y.prefix(opNot)
	case t.word == "-o" || t.word == "-or":
		if !y.infix(opOr, t.raw, "an alternation with no test before it") {
			return i, false
		}
		y.e.operated = true
	case t.word == "-a" || t.word == "-and":
		return i, y.infix(opAnd, t.raw, "a conjunction with no test before it")
	case t.word == ",":
		return i, y.fail(causeShape, ",", "a comma, which the hook does not read between find's tests")
	case t.filled:
		return i, y.fail(causeFilled, t.raw, "a word the shell fills in where an operator could be")
	case !strings.HasPrefix(t.word, "-"):
		return i, y.fail(causeStray, t.raw, "a word that is neither a test nor an operator, where only one of those can stand")
	default:
		return y.primary(tokens, i)
	}
	return i, true
}

// closeGroup reads a `)`.
func (y *operatorYard) closeGroup() bool {
	if y.wantOperand {
		return y.fail(causeShape, ")", "a closing parenthesis with no test before it")
	}
	// Pop to the matching `(`.
	for len(y.ops) > 0 && y.ops[len(y.ops)-1] != opNone {
		y.e.postfix = append(y.e.postfix, findItem{op: y.ops[len(y.ops)-1]})
		y.ops = y.ops[:len(y.ops)-1]
	}
	if len(y.ops) == 0 {
		return y.fail(causeShape, ")", "a closing parenthesis with no group open")
	}
	y.ops = y.ops[:len(y.ops)-1] // drop the `(`
	y.e.operated = true
	y.wantOperand = false
	return true
}

// prefix reads what stands before a test: a `(`, whose mark is opNone, or a
// negation.
func (y *operatorYard) prefix(op findOp) {
	y.and()
	y.ops = append(y.ops, op)
	y.e.operated = true
	y.wantOperand = true
}

// infix reads an operator written between two tests, spelled raw, and fails
// with why where no test stands before it.
func (y *operatorYard) infix(op findOp, raw, why string) bool {
	if y.wantOperand {
		return y.fail(causeShape, raw, why)
	}
	y.popTo(opPrec(op))
	y.ops = append(y.ops, op)
	y.wantOperand = true
	return true
}

// primary reads a test or an action at i, and the values it is given.
func (y *operatorYard) primary(tokens []token, i int) (int, bool) {
	item, next, word, why := y.e.readPrimary(tokens, i)
	if why != "" {
		return i, y.fail(causeFilled, word, why)
	}
	y.and()
	y.e.postfix = append(y.e.postfix, item)
	y.wantOperand = false
	return next, true
}

// finish writes out the operators left on the stack once the tokens are
// read, and fails where one has no test to govern or a group is still open.
func (y *operatorYard) finish() bool {
	if y.wantOperand && len(y.e.postfix)+len(y.ops) > 0 {
		return y.fail(causeShape, "", "an operator or a group with no test to govern")
	}
	for len(y.ops) > 0 {
		top := y.ops[len(y.ops)-1]
		if top == opNone {
			return y.fail(causeShape, "(", "a group that is never closed")
		}
		y.e.postfix = append(y.e.postfix, findItem{op: top})
		y.ops = y.ops[:len(y.ops)-1]
	}
	return true
}

// readPrimary reads one test or action at i and the values it takes, and
// returns the node, the index of its last token, and, where its words could
// not be followed, the word and why.
func (e *findExpression) readPrimary(tokens []token, i int) (findItem, int, string, string) {
	opt := tokens[i].word
	switch opt {
	case "-exec", "-execdir", "-ok", "-okdir":
		next, word, why := readExec(tokens, i)
		return findItem{pick: true, action: true}, next, word, why
	}
	values, known := findTakes(opt)
	item := findItem{pick: true, action: findAction(opt)}
	switch {
	case opt == "-empty":
		item.empty = true
	case opt == "-type" && i+1 < len(tokens) && !strings.Contains(tokens[i+1].word, "d"):
		item.notDir = true
	}
	if whole, isGlob := globOptions(opt); isGlob {
		item = findItem{isGlob: true, whole: whole}
		if i+1 < len(tokens) {
			item.glob = findPattern(tokens[i+1].word)
		}
	}
	if !known && e.unknown == "" {
		// Reading goes on as if the option took no value, so that the refusal
		// can name a word the shell fills in after it.
		e.unknown = tokens[i].raw
	}
	// Its values, which are not examined: a separator the shell hid from find
	// is the value it is.
	return item, min(i+values, len(tokens)-1), "", ""
}

// readExec reads the command an -exec kind of action runs, to the terminator
// the shell hid from find, or to a `+` after `{}`, or to the end of the words.
// A word the shell could split could become the terminator, and a word after a
// filled word that reads as an operator could be the expression that follows a
// filled terminator: either cannot be followed.
func readExec(tokens []token, i int) (int, string, string) {
	filledBefore := false
	last := ""
	for i++; i < len(tokens); i++ {
		t := tokens[i]
		if t.marker == ';' {
			return i, "", ""
		}
		if t.marker != 0 {
			last = ""
			continue
		}
		if t.word == "+" && last == "{}" {
			return i, "", ""
		}
		if cause := execUnfollowed(t, filledBefore); cause != "" {
			return i, t.raw, cause
		}
		if t.filled {
			filledBefore = true
		}
		last = t.word
	}
	// The command ran to the end of the words: find rejects that, and reading
	// it to the end swallows nothing find would run.
	return i - 1, "", ""
}

// execUnfollowed is why a word of an -exec command cannot be followed, or
// nothing where it can: the split is asked before the place after a filled
// word, as readExec has always asked them.
func execUnfollowed(t token, filledBefore bool) string {
	if t.splittable {
		return "a word in an -exec command the shell could split into the terminator"
	}
	if filledBefore && (findOperator(t.word) || strings.HasPrefix(t.word, "-")) {
		return "a word after an -exec command that could stand where the expression goes on"
	}
	return ""
}

// opPrec is an operator's binding: a negation binds tighter than an implicit or
// written AND, which binds tighter than OR. It is asked of those three only: a
// group's mark is stopped at before its binding is asked for.
func opPrec(op findOp) int {
	switch op {
	case opNot:
		return 3
	case opAnd:
		return 2
	default:
		return 1
	}
}
