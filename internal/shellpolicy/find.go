package shellpolicy

import (
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// find picks the files it changes by a name, and a name is no path the rules
// read: `find . -name active -delete` removed the loop's record, and
// `find . -path '*state/tests.lock' -delete` the freeze.

// stateFiles are the files the store keeps in .sdlc/state, which only a name
// can pick out of the directory.
var stateFiles = []string{
	".sdlc/state/active", ".sdlc/state/session", ".sdlc/state/tests.lock", ".sdlc/state/stop-blocks.json",
	".sdlc/state/acknowledged",
}

// findArguments are the directories find starts from, and the patterns it picks
// files with: a -name is matched against a file's name, a -path against the
// whole of the path find comes to it by.
func findArguments(args []string) (roots, names, paths []string) {
	roots, i := findRoots(args)
	for ; i+1 < len(args); i++ {
		if wholePath, ok := globOptions(args[i]); ok {
			if wholePath {
				paths = append(paths, unquote(args[i+1]))
			} else {
				names = append(names, unquote(args[i+1]))
			}
			i++
		}
	}
	return roots, names, paths
}

// findRoots are the directories find starts from, and where in args its
// expression starts after them.
func findRoots(args []string) ([]string, int) {
	i := 0
	// The options that come before the directories: -H, -L, -P, -O3, -D.
	for i < len(args) && (args[i] == "-H" || args[i] == "-L" || args[i] == "-P" || args[i] == "-D" ||
		strings.HasPrefix(args[i], "-O")) {
		i++
	}
	// Then the directories, up to the first option or operator. An operator is
	// no directory, and with no directory of its own a find starts where the
	// command runs, which is `.`: it is judged from there with an operator
	// first as it is with a test first.
	var roots []string
	for ; i < len(args) && !strings.HasPrefix(args[i], "-") && !findOperator(args[i]); i++ {
		roots = append(roots, args[i])
	}
	if len(roots) == 0 {
		roots = []string{"."}
	}
	return roots, i
}

// findOperator reports whether a word, as parse hands it over with its quotes
// gone and its backslashes kept, is one of find's expression operators rather
// than a directory or a primary: a negation, a boolean, the comma, or an
// opening parenthesis in a spelling that still reaches here. The closing
// parenthesis and the quoted and escaped `;`, `|` and `&` are turned to spaces
// by escapedSeparators before a word is made, so only the opening forms arrive.
func findOperator(word string) bool {
	switch word {
	case "!", `\!`, "-not", "-a", "-and", "-o", "-or", ",", "(", `\(`:
		return true
	}
	return false
}

// globOptions reports whether a find option is given a glob, and whether the
// glob is matched against the whole path rather than the name.
func globOptions(option string) (wholePath, ok bool) {
	switch option {
	case "-name", "-iname":
		return false, true
	case "-path", "-ipath", "-wholename", "-iwholename":
		return true, true
	}
	return false, false
}

// reaches reports whether find, started at root from dir, can come to the
// project's own files: from the project, from a directory above it, or from a
// directory this cannot place.
func reaches(root, dir string, s State, m *memo) bool {
	r := path.Join(dir, clean(root))
	if isAbsolute(clean(root)) {
		r = clean(root)
	}
	if strings.ContainsAny(r, unsettledChars) {
		return true
	}
	if isAbsolute(r) {
		if s.Resolve == nil {
			return true
		}
		// Outside is also above.
		rel := m.resolve(s, r)
		return rel == "" || rel == "."
	}
	for segment := range strings.SplitSeq(r, "/") {
		if segment != "." && segment != ".." {
			return false
		}
	}
	return true
}

// foundBy is the paths among shapes that find picks with these patterns from
// these roots: by the name a path ends in, or by the whole of it.
//
// A glob that cannot be read is passed over here: what it could pick is the
// find blocks' to refuse, from where the command starts.
func foundBy(roots, names, patterns, shapes []string) []string {
	// Each glob is read once, not once for every shape.
	nameGlobs, pathGlobs := readGlobs(names), readGlobs(patterns)
	var out []string
	for _, shape := range shapes {
		for _, g := range nameGlobs {
			if g.MatchString(path.Base(shape)) {
				out = append(out, shape)
			}
		}
		for _, g := range pathGlobs {
			out = appendFoundByPath(out, g, roots, shape)
		}
	}
	return out
}

// appendFoundByPath adds shape to out once for each root from which find
// comes to it by a path the glob matches; a glob that cannot be read adds
// nothing.
func appendFoundByPath(out []string, g *globPattern, roots []string, shape string) []string {
	if g == nil {
		return out
	}
	for _, root := range roots {
		// The path find comes to it by starts at the root: `./x` from `.`,
		// and `internal/x` from `internal` for the project's internal/x.
		full := strings.TrimSuffix(clean(root), "/") + "/" + shape
		if r := clean(root); r != "." && strings.HasPrefix(shape, r+"/") {
			full = shape
		}
		if g.MatchString(full) {
			out = append(out, shape)
		}
	}
	return out
}

// readGlobs is each of globs read, nil where it cannot be.
func readGlobs(globs []string) []*globPattern {
	out := make([]*globPattern, len(globs))
	for i, g := range globs {
		out[i] = findPattern(g)
	}
	return out
}

// literals is the patterns with a character of their own in them. One that is
// all wildcards is every file where find looks, and names none in particular.
// hasLiteral's scan keeps what it always kept, and the reader adds a bracket
// nothing closes, such as `[]`, which the scan passes over.
func literals(patterns []string) []string {
	var out []string
	for _, p := range patterns {
		if hasLiteral(p) || ownRune(p) {
			out = append(out, p)
		}
	}
	return out
}

// nameMatches is find's -name: a glob against one name, where, unlike in the
// shell, `*` matches a leading dot.
func nameMatches(pattern, name string) bool {
	return findPattern(pattern).MatchString(name)
}

// maxGlob is the longest glob the reader reads; a longer one cannot be read.
// docs/enforcement.md gives the number, and changes with it.
const maxGlob = 512

// A globPattern is a glob read the way find reads it, which is also the way the
// shell expands it: a class that starts with `]`, negation, ranges, POSIX
// named classes and escapes. Read by path.Match and a regular expression built
// at the first `]`, `[]x]_test.go` named no file and `find . -name
// '[]x]_test.go' -delete` took a frozen test away.
//
// It holds two readings, and a name either matches is matched. raw is the
// glob as written, matched against the name as given, with case left to the
// matching; folded is the glob folded as the filesystem folds a name, and is
// matched against the name folded, which keeps what folding does that matching
// a character at a time cannot: a ligature, a decomposed accent, a trailing
// dot. A reading that cannot be made is nil.
type globPattern struct {
	raw, folded []globPart
	// literal is whether the glob has a character of its own outside `*`, `?`
	// and every class, in either reading.
	literal bool
}

// A globPart is a `*`, or one character the glob matches: a literal, `?`, or
// a bracket expression.
type globPart struct {
	star    bool
	negated bool
	// anyOne is an equivalence class, or a range a locale orders: with no
	// collation table to say what either holds, it is any one character to a
	// reading that matches more, and nothing to one that matches less.
	anyOne  bool
	runes   []rune
	ranges  [][2]rune
	classes []func(rune) bool
}

// findPattern is a glob read as find reads -path, -name and their kind, or nil
// where it cannot be read. nil means the glob says nothing of what it picks,
// not that it picks nothing: each caller decides what that is worth.
func findPattern(text string) *globPattern {
	// Checked on the text as given, so that folding can neither turn a byte that
	// is no character into one nor bring a long glob under the bound.
	if len(text) > maxGlob || !utf8.ValidString(text) {
		return nil
	}
	g := &globPattern{}
	var rawLiteral, foldedLiteral bool
	g.raw, rawLiteral = readGlob(text)
	// A glob that folding leaves as it is still has the name folded: read
	// raw alone, `rm internal/strasse/*_test.go` was allowed with
	// internal/straße/x_test.go frozen, which the filesystem finds by that
	// spelling.
	g.folded, foldedLiteral = g.raw, rawLiteral
	if folded := pathrules.Fold(text); folded != text {
		g.folded, foldedLiteral = readGlob(folded)
	}
	if g.raw == nil && g.folded == nil {
		return nil
	}
	g.literal = rawLiteral || foldedLiteral
	return g
}

// MatchString reports whether the glob matches s, in either reading. A glob
// that cannot be read matches nothing here.
func (g *globPattern) MatchString(s string) bool {
	return g != nil && (g.raw != nil && matchParts(g.raw, s) || g.folded != nil && matchParts(g.folded, pathrules.Fold(s)))
}

// readGlob is one reading of a glob, and whether it has a character of its
// own outside every class; nil where it cannot be read.
func readGlob(text string) ([]globPart, bool) {
	parts := make([]globPart, 0, len(text))
	literal, unclosed := false, false
	for i := 0; i < len(text); {
		switch text[i] {
		case '*':
			parts = append(parts, globPart{star: true})
			i++
		case '?':
			parts = append(parts, globPart{negated: true})
			i++
		case '[':
			part, next, open, ok := readBracket(text, i, unclosed)
			if !ok {
				return nil, false
			}
			parts = append(parts, part)
			if open {
				literal, unclosed = true, true
			}
			i = next
		default:
			r, size, ok := escapedRune(text, i)
			if !ok {
				return nil, false
			}
			parts = append(parts, globPart{runes: []rune{r}})
			literal = true
			i += size
		}
	}
	return parts, literal
}

// readBracket reads what the `[` at text[i] is: a class, with where reading
// goes on after it, or with open the bracket itself where nothing closes it.
// unclosed is whether an earlier `[` of this glob was already read that way.
// It is not ok where the glob cannot be read.
func readBracket(text string, i int, unclosed bool) (part globPart, next int, open, ok bool) {
	part, next, closed, held := readClass(text, i+1)
	if next < 0 {
		return globPart{}, 0, false, false
	}
	if closed {
		return part, next, false, true
	}
	// A `[` nothing closes is the bracket it is, and reading goes on
	// after it, as bash reads it; where find matches nothing with such
	// a glob, this reading matches more. Not where the bracket held a
	// collating symbol or an equivalence class: zsh and path.Match end
	// the class at that item's `]`, and read as a literal `[` the glob
	// would match less than either. Nor a second one: no `[` is read to
	// the end of the glob twice, and the reading stays linear.
	if unclosed || held {
		return globPart{}, 0, false, false
	}
	return globPart{runes: []rune{'['}}, i + 1, true, true
}

// readClass reads a bracket expression from just after its `[`: the part it
// is, where reading goes on after its `]`, whether a `]` closed it, and whether
// it held a collating symbol or an equivalence class. next is -1 where it
// cannot be read.
func readClass(text string, i int) (part globPart, next int, closed, held bool) {
	if i < len(text) && (text[i] == '!' || text[i] == '^') {
		part.negated = true
		i++
	}
	for start := i; i < len(text); {
		// A `]` first is one of the members.
		if text[i] == ']' && i > start {
			return part, i + 1, true, held
		}
		after, symbol, ok := readMember(&part, text, i)
		held = held || symbol
		if !ok {
			return part, -1, false, held
		}
		i = after
	}
	return part, i, false, held
}

// readMember reads one member of a bracket expression at i, an item or a
// range between two, adds it to part, and returns where reading goes on and
// whether it held a collating symbol or an equivalence class. It is not ok
// where the member cannot be read, and then adds nothing.
func readMember(part *globPart, text string, i int) (next int, held, ok bool) {
	lo, after, ok := readItem(text, i)
	if !ok {
		return -1, false, false
	}
	held = lo.kind == collatingItem || lo.kind == equivalenceItem
	i = after
	// A `-` last, or before the `]` that closes, is a member.
	if i+1 >= len(text) || text[i] != '-' || text[i+1] == ']' {
		switch lo.kind {
		case namedItem:
			part.classes = append(part.classes, lo.class)
		case equivalenceItem:
			part.anyOne = true
			part.runes = append(part.runes, lo.r)
		default:
			part.runes = append(part.runes, lo.r)
		}
		return i, held, true
	}
	hi, after, ok := readItem(text, i+1)
	held = held || hi.kind == collatingItem || hi.kind == equivalenceItem
	// A range runs between two characters: a named class or an
	// equivalence class at either end is nothing find agrees on.
	if !ok || lo.kind == namedItem || lo.kind == equivalenceItem || hi.kind == namedItem || hi.kind == equivalenceItem {
		return -1, held, false
	}
	// Between any other two characters the order is the locale's. Read
	// by code point, `[{-b]` held nothing and `find . -name '[{-b]ctive'
	// -delete` was allowed, where find in en_US.UTF-8 picks active.
	if plainRange(lo.r, hi.r) {
		part.ranges = append(part.ranges, [2]rune{lo.r, hi.r})
	} else {
		part.anyOne = true
	}
	return after, held, true
}

// plainRange reports whether a range runs between two lower-case letters, two
// upper-case letters or two digits, all ASCII, which is read by code point and
// by letter.
func plainRange(lo, hi rune) bool {
	in := func(first, last rune) bool { return first <= lo && lo <= last && first <= hi && hi <= last }
	return in('a', 'z') || in('A', 'Z') || in('0', '9')
}

// An itemKind is the kind of item a bracket expression holds.
type itemKind int

const (
	plainItem itemKind = iota
	collatingItem
	equivalenceItem
	namedItem
)

// A bracketItem is one member of a bracket expression: a character, or a named
// class.
type bracketItem struct {
	kind  itemKind
	r     rune
	class func(rune) bool
}

// readItem reads one member of a bracket expression at i: `[:name:]`,
// `[=x=]`, `[.x.]`, an escaped character or a plain one, and where reading
// goes on after it. An unknown class name, an item never closed, and a
// collating symbol or equivalence class of other than one character cannot be
// read.
func readItem(text string, i int) (bracketItem, int, bool) {
	if i+1 < len(text) && text[i] == '[' && strings.IndexByte(":=.", text[i+1]) >= 0 {
		delimiter := text[i+1]
		end := strings.Index(text[i+2:], string(delimiter)+"]")
		if end < 0 {
			return bracketItem{}, 0, false
		}
		body, after := text[i+2:i+2+end], i+2+end+2
		if delimiter == ':' {
			class, ok := posixClass(body)
			return bracketItem{kind: namedItem, class: class}, after, ok
		}
		r, size := utf8.DecodeRuneInString(body)
		kind := collatingItem
		if delimiter == '=' {
			kind = equivalenceItem
		}
		return bracketItem{kind: kind, r: r}, after, size > 0 && size == len(body)
	}
	r, size, ok := escapedRune(text, i)
	return bracketItem{kind: plainItem, r: r}, i + size, ok
}

// escapedRune is the character at i, or the one after a `\` there, and how
// many bytes it took. A `\` that ends the glob escapes nothing.
func escapedRune(text string, i int) (rune, int, bool) {
	if text[i] == '\\' {
		if i+1 == len(text) {
			return 0, 0, false
		}
		r, size := utf8.DecodeRuneInString(text[i+1:])
		return r, size + 1, true
	}
	r, size := utf8.DecodeRuneInString(text[i:])
	return r, size, true
}

// posixClass is a POSIX named class. It is a switch, not a map, so that no
// package init builds one.
func posixClass(name string) (func(rune) bool, bool) {
	switch name {
	case "alnum":
		return func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }, true
	case "alpha":
		return unicode.IsLetter, true
	case "blank":
		return func(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) }, true
	case "cntrl":
		return unicode.IsControl, true
	case "digit":
		return unicode.IsDigit, true
	case "graph":
		return func(r rune) bool { return unicode.IsGraphic(r) && !unicode.IsSpace(r) }, true
	case "lower":
		return unicode.IsLower, true
	case "print":
		return unicode.IsPrint, true
	case "punct":
		// The C locale's punctuation is every visible ASCII character that is
		// not a letter or a digit, which Unicode splits into punctuation and
		// symbols.
		return func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }, true
	case "space":
		return unicode.IsSpace, true
	case "upper":
		return unicode.IsUpper, true
	case "xdigit":
		return func(r rune) bool { return unicode.Is(unicode.ASCII_Hex_Digit, r) }, true
	}
	return nil, false
}

// accepts reports whether the part matches r. A part is read in these ways,
// and takes the widest: a member matches in any case; a plain range holds what
// is between its ends by code point or by letter in either case, and any
// character beyond ASCII; and a range a locale orders holds any one character.
// A negated part leaves out only what all of them count in.
func (p *globPart) accepts(r rune) bool {
	if p.negated {
		return !p.holds(r, false)
	}
	return p.holds(r, true)
}

// holds reports whether r is one of the part's members. Wide, that is in any
// of those readings: r or another case of it, a named class beyond ASCII, and
// for a range, any character beyond ASCII as given, which a locale can put
// between any two ends: in en_US.UTF-8 find picks é with `[a-m]`. Otherwise it
// is in all of them.
func (p *globPart) holds(r rune, wide bool) bool {
	if wide && (p.anyOne || len(p.ranges) > 0 && r >= utf8.RuneSelf) {
		return true
	}
	for c := r; ; {
		if p.member(c, wide) {
			return true
		}
		if c = unicode.SimpleFold(c); !wide || c == r {
			return false
		}
	}
}

// member reports whether c itself is one of the part's members.
func (p *globPart) member(c rune, wide bool) bool {
	if slices.Contains(p.runes, c) {
		return true
	}
	for _, rg := range p.ranges {
		code := rg[0] <= c && c <= rg[1]
		lower := unicode.ToLower(c)
		letter := unicode.ToLower(rg[0]) <= lower && lower <= unicode.ToLower(rg[1])
		if wide && (code || letter) || code && letter {
			return true
		}
	}
	for _, class := range p.classes {
		if class(c) && (wide || c < utf8.RuneSelf) {
			return true
		}
	}
	return false
}

// matchParts matches a reading against s with one remembered star: each part
// but a star takes one character, so a mismatch goes back only to the last
// star, and the work is the glob's length times the name's. A matcher that
// went back over every star took past 10^40 steps on forty of them.
func matchParts(parts []globPart, s string) bool {
	name := []rune(s)
	p, n, star, mark := 0, 0, -1, 0
	for n < len(name) {
		switch {
		case p < len(parts) && parts[p].star:
			star, mark = p, n
			p++
		case p < len(parts) && parts[p].accepts(name[n]):
			p++
			n++
		case star >= 0:
			p, mark = star+1, mark+1
			n = mark
		default:
			return false
		}
	}
	for p < len(parts) && parts[p].star {
		p++
	}
	return p == len(parts)
}

// A glob find is given that the hook cannot read says nothing of what find
// goes on to change, and a find that deletes with one is refused from a
// directory that could hold what the rules protect: dropped, `find . -path
// '*[]x]_test.go' -delete` took a frozen test away.

// An unreadCause is why the hook cannot read a glob find is given.
type unreadCause int

const (
	shellCause unreadCause = iota
	splitCause
	readCause
)

// String is the cause as a refusal gives it.
func (c unreadCause) String() string {
	switch c {
	case shellCause:
		return "the shell expands or unescapes part of it before find sees it"
	case splitCause:
		return "a quote or white space splits the word before find sees it"
	default:
		return "it holds a bracket expression, an escape or a byte the hook cannot read, or is longer than " +
			strconv.Itoa(maxGlob) + " bytes"
	}
}

// unreadGlob is the first glob a find is given that the hook cannot read, as
// the command spells it, and why. words are the find's own words, which parse
// made from segment and program took from the end of them.
//
// The quotes the shell acts on are gone from words, so the segment is split
// again as parse splits it, and the glob is the field in the same place: never
// found by searching the segment for its text, which an earlier word can hold
// too.
func unreadGlob(segment string, words []string) (string, unreadCause, bool) {
	_, raw := spelledFields(segment)
	raw = raw[max(len(raw)-len(words), 0):]
	args := words[1:]
	_, at := findRoots(args)
	for i := at; i+1 < len(args) && i+2 < len(raw); i++ {
		if _, ok := globOptions(args[i]); ok {
			if cause, unread := unreadWord(raw[i+2]); unread {
				return raw[i+2], cause, true
			}
			i++
		}
	}
	return "", 0, false
}

// spelledFields splits a segment into its words as parse splits it, dropping a
// redirect with its target and a leading assignment, and returns each word
// unquoted beside the word as the command spells it, the two lists in step.
// The find expression reader and unreadGlob both read the as-spelled word to
// see what the shell would do to it, which the parsed word has lost.
func spelledFields(segment string) (unquoted, raw []string) {
	for _, f := range wordFields(segment) {
		unquoted = append(unquoted, unquote(f.text))
		raw = append(raw, f.text)
	}
	return unquoted, raw
}

// unreadWord reports whether the hook cannot read a glob word as the command
// spells it, and why. Quotes are followed over the word alone, so a `'` inside
// another quote elsewhere on the line, as in `echo "it's"`, does not count.
func unreadWord(word string) (unreadCause, bool) {
	quote, shell := quoteLeftOpen(word)
	if shell {
		return shellCause, true
	}
	if quote != 0 {
		return splitCause, true
	}
	if findPattern(unquote(word)) == nil {
		return readCause, true
	}
	return 0, false
}

// quoteLeftOpen follows the quotes over a word and returns the one still open
// at its end, or with shell that the word holds something the shell fills in
// or escapes, at which the reading stops.
func quoteLeftOpen(word string) (quote byte, shell bool) {
	ansi := false
	for i := 0; i < len(word); i++ {
		switch c := word[i]; {
		case quote != 0 && c == quote:
			quote = 0
		case quote == '\'':
			// In $'...' a backslash is the shell's own escape, and can hide
			// the quote that would end it.
			if ansi && c == '\\' {
				i++
			}
		case shellSpells(word, i):
			return 0, true
		case c == '$' && quote == 0:
			// $'...' and $"..." are quotes; inside "..." a `$` before a quote
			// is the character it is.
			quote, ansi = word[i+1], word[i+1] == '\''
			i++
		case quote == 0 && (c == '\'' || c == '"'):
			quote, ansi = c, false
		}
	}
	return quote, false
}

// shellSpells reports whether the character at word[i] is one the shell acts
// on: a backquote, a backslash, or a `$` that does not open $'...' or $"...".
func shellSpells(word string, i int) bool {
	c := word[i]
	return c == '`' || c == '\\' || c == '$' && (i+1 == len(word) || word[i+1] != '\'' && word[i+1] != '"')
}

// couldHold is whether find, started at root from dir, could come to what the
// rules protect: a frozen test below root, which it names, or the project's
// own files, from a directory that reaches them or one it cannot place. With
// frozen nil it is the loop's own files alone.
func couldHold(root, dir string, lost bool, frozen []string, s State, m *memo) (string, bool) {
	for _, sp := range m.spell(s, dir, []string{root}) {
		// Cleaned as well: compared only as spelled, `internal/.` held no
		// frozen test, and `find internal/. -name GLOB -delete` with a glob
		// longer than maxGlob was allowed, though find picks
		// internal/x_test.go from there.
		for _, w := range []string{sp, path.Clean(clean(sp))} {
			if test, ok := holding(w, frozen); ok {
				return test, true
			}
		}
	}
	return "", reaches(root, dir, s, m) || lost && !isAbsolute(clean(root))
}

// placedRoot reports whether the hook can tell where root is: neither a word
// the shell fills in nor a directory it cannot follow to.
func placedRoot(root, dir string, lost bool, s State, m *memo) bool {
	c := clean(root)
	if isAbsolute(c) {
		return !strings.ContainsAny(c, unsettledChars) && s.Resolve != nil && m.resolve(s, c) != ""
	}
	return !lost && !strings.ContainsAny(path.Join(dir, c), unsettledChars)
}

// unreadFinding refuses a find that goes on to change what it finds under root
// with a glob the hook cannot read. what is what root holds, or could hold.
func unreadFinding(rule, glob, root string, cause unreadCause, what string, known bool) Finding {
	holds := ", which holds " + what
	if !known {
		holds = ", and the hook cannot tell where " + shown(root) + " is, so it could hold " + what
	}
	route := "give find a glob the hook can read, spelled so the shell leaves it for find: `*`, `?`, " +
		"or a bracket expression such as `[a-z]`, `[!.]` or `[[:digit:]]`, in single quotes, with no " +
		"variable, no command substitution and no backslash the shell would act on; or name the " +
		"files to change without a glob" + ruleWayOut(rule)
	return Finding{
		Rule: rule,
		Reason: "find is given a glob the hook cannot read, " + shown(glob) + ", because " + cause.String() +
			". The command goes on to change what it finds under " + shown(root) + holds,
		Route: route,
	}
}

// ruleWayOut is the sanctioned route the two find rules end a refusal with: the
// unfreeze for the freeze, the sdlc command for the loop's own files.
func ruleWayOut(rule string) string {
	if rule == ruleFrozenTest {
		return ". If a frozen test genuinely has to change, say which and why: the person running " +
			"the session lifts the freeze with `/sdlc:unfreeze`, or `sdlc unfreeze --reason ...`"
	}
	return ". The loop's own files are changed through the sdlc command: `sdlc artifact write` " +
		"for a gate's documents, `sdlc review add` for a review, `sdlc gate` for an outcome; " +
		"configuration is changed by hand, by the person running the session"
}

// unfollowedFinding refuses a find whose expression the hook cannot read as
// find parses it, under root, which holds or could hold what the find would
// change. The reason quotes the word the reader stopped at, and the route is
// chosen by the cause: told to write a word out in quotes, a person whose find
// holds an option the hook does not know, or stands inside a script, has
// nothing to act on.
func unfollowedFinding(rule string, e findExpression, root, what string, known bool) Finding {
	holds := ", which holds " + what
	if !known {
		holds = ", and the hook cannot tell where " + shown(root) + " is, so it could hold " + what
	}
	why := e.why
	if e.word != "" {
		why += ", at " + shown(e.word)
	}
	var route string
	switch {
	case e.unknown != "":
		why = shown(e.unknown) + " is not an option of find that the hook knows, so it cannot tell " +
			"which of the words after it are the option's own"
		if e.cause == causeFilled {
			why += ", and the shell fills in " + shown(e.word) + " after it"
		}
		route = "if " + shown(e.unknown) + " is a slip, correct it; if your find has such an option, " +
			"pick the files with options the hook knows, or list them with a find that only prints " +
			"and name the ones to change"
	case e.cause == causeFilled:
		route = "write the word out as plain text in single quotes, so the shell leaves it for find -- " +
			"not a variable, a command substitution or a backslash the shell would act on; or put the " +
			"option before the directories; or run one find for each filter, so the hook need not read a " +
			"group"
	case e.cause == causeScript:
		route = "run the find as a command of its own, not inside `sh -c` or `eval`; or write the " +
			"script as one word in single quotes, with no script inside it; or, where a quote is never " +
			"closed, close it, or take the comment that holds it off the command"
	case e.cause == causeStray:
		route = "put every directory before the first test, and take a comment or an unclosed quote " +
			"off the command"
	case e.cause == causeDiffer:
		route = "run the find as a command of its own, with each word written out plainly"
	default:
		route = "run one find for each filter, so the hook need not read a group"
	}
	return Finding{
		Rule: rule,
		Reason: "find's expression cannot be read as find parses it: " + why +
			". The command goes on to change what it finds under " + shown(root) + holds,
		Route: route + ruleWayOut(rule),
	}
}

// spentFinding refuses a find the reader gave up on after the command had used
// up what the reader reads to the end for such finds. It is under the rule
// that find is refused under from the project's own directory, wherever it
// starts and whatever it does: the hook has read neither.
func spentFinding(e findExpression) Finding {
	const rule = ruleLoopState
	why := e.why
	if e.word != "" {
		why += ", at " + shown(e.word)
	}
	return Finding{
		Rule: rule,
		Reason: "find's expression cannot be read as find parses it: " + why + "; and the command holds " +
			"more finds the hook cannot follow than it reads to the end, so this one is refused wherever " +
			"it starts and whatever it does",
		Route: "split the line into separate commands, and run each find as a command of its own" +
			ruleWayOut(rule),
	}
}

// laterAction is a refusal of a find whose own segment changes no file, made
// because the reader gave up on it and a word after it on the line is an
// action that changes files. Whether that action is the find's own, past a
// separator in one of its quoted words, or another command's, the hook cannot
// tell. Told only why the find could not be followed, a person whose find only
// reads saw a refusal of a find that changes nothing.
func laterAction(f Finding) Finding {
	f.Reason += "; no word before the first separator after this find changes a file, but a later word " +
		"on the line is an action of find that does, and the hook cannot tell where this find ends, " +
		"so the action is judged as its own"
	f.Route = "run this find as a command of its own; failing that, " + f.Route
	return f
}

// leftIn is a refusal with it said, where picked is set, that the file is one
// a find's operators leave in. Only a refusal the flat reading does not make
// is told so: one it does make keeps the words it has always had.
func leftIn(f Finding, picked bool) Finding {
	if picked {
		f.Reason += "; the find never names it, and the operators between its tests leave it among " +
			"the files the find picks"
		f.Route += "; or run one find for each filter, so that each says by name what it picks"
	}
	return f
}

// printedTo are the files find writes what it finds to: `-fprint FILE`,
// `-fprint0 FILE`, `-fprintf FILE FORMAT` and `-fls FILE`.
func printedTo(args []string) []string {
	var files []string
	for i, a := range args {
		switch a {
		case "-fprint", "-fprint0", "-fprintf", "-fls":
			if i+1 < len(args) {
				files = append(files, args[i+1])
			}
		}
	}
	return files
}
