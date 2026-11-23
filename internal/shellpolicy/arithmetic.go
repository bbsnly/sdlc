package shellpolicy

import (
	"regexp"
	"slices"
	"strings"
	"sync"
)

// arithmeticChanges reports text that can change a variable through the
// shell's arithmetic, which commands-run-as-configured reads as a change of
// the environment the call starts with (A.3). An assignment there needs no
// name written out: `$(( ${x:-HOME}=1 ))`, `(( $(printf HOME)=1 ))`,
// `${a[${x:-HOME}=3]}` and `[[ 1 -eq ${x:-HOME}=1 ]]` each set HOME in bash
// and zsh, and a number in HOME or TMPDIR fails a test run that the freeze
// then took for new tests failing (code review, security F-4). So where the
// shell evaluates arithmetic, any assigning operator refuses, whatever stands
// before it, and so does an increment or a decrement.
//
// It reads, in one pass over the line each:
//
//   - the bodies the shell evaluates as arithmetic: `((…))` and `$((…))`,
//     each `((` paired with its own `))` by depth (a reading that ended a
//     body at the first `))` let `(( ((1)), CI++ ))` through; code review,
//     code-reviewer F-5); `$[…]`; a subscript, `name[…]`, `${name[…]}` and
//     zsh's `$name[…]`; the offset and length of `${name:…}`; zsh's flags in
//     `${(…)…}`, whose padding widths are arithmetic; and what a `${…}`, or
//     a double-quoted string, inside any of them gives, since the shell
//     evaluates that too (`$(( ${x:-HOME=1} ))`). A `$(…)` or a backtick
//     inside is a command of its own, read as one, and so is a single-quoted
//     string outside them, which bash's `${1@P}` and zsh's `${(e)…}` expand
//     and unset and test -v take a subscripted name in (`unset 'a[V++]'`);
//   - a `[[ … ]]` that holds -eq, -ne, -lt, -le, -gt or -ge, whose operands
//     are arithmetic, where a `=` standing alone is a comparison, and `==`,
//     `!=` and `=~` always are;
//   - zsh's builtins that evaluate their words as arithmetic, shift, repeat,
//     return, break and continue, and the arguments of printf and print -f
//     (`shift "V = 0"` set V); and, anywhere on the line, an assigning
//     operator straight after a word the shell makes, a `}`, `)` or backtick
//     closing it or a `$NAME`, which only arithmetic reads as an assignment,
//     as the literal `NAME=` is already read anywhere on the line outside a
//     here-document's body.
//
// The line it is given has those bodies taken out, as every rule's reading
// has. A body whose delimiter is not quoted, which the shell expands, is
// read by expandingDocument in configuredrun.go, which refuses one that holds
// a substitution, arithmetic, a `${…}` or zsh's `$name[…]` on a line naming
// the call, and reads each of them, to the end of the body, with this.
//
// A value an earlier shell call left in a variable, that a bare name in the
// arithmetic evaluates (`(( y ))`, where y holds HOME=1), is the class of an
// export one call makes reaching the next, in docs/enforcement.md's Not held.
func arithmeticChanges(text string) bool {
	if madeNameAssigns().MatchString(text) || mathBuiltinAssigns(text) {
		return true
	}
	return arithmeticScan(text)
}

// parameterName is what may follow a `$` as a parameter's name: a name, or
// one of the digits and special parameters.
const parameterName = "(?:[A-Za-z_][A-Za-z0-9_]*|[0-9@*#?$!-])"

// madeNameAssigns is an assigning operator, or an increment or a decrement,
// straight after the end of a word the shell makes, a closing quote allowed
// between; and an increment or a decrement straight before one.
var madeNameAssigns = sync.OnceValue(func() *regexp.Regexp {
	const made = "(?:[})`]|\\$" + parameterName + ")[\"']?"
	return regexp.MustCompile(made + `(?:(?:[-+*/%&|^]|<<|>>|\*\*)?=(?:[^=~]|$)|\+\+|--)` +
		"|(?:\\+\\+|--)[\"']?(?:\\$|`)")
})

// mathBuiltinAssigns reports a builtin given a word that assigns, where zsh
// evaluates the word as arithmetic: shift's, repeat's, return's, break's and
// continue's words, and the arguments printf and print -f give their format
// (`return 'V++'` and `print -f %d 'V++'` each set V).
func mathBuiltinAssigns(text string) bool {
	for _, words := range simpleCommands(text) {
		words, _ = afterFront(withoutRedirects(words))
		if len(words) < 2 {
			continue
		}
		args := words[1:]
		switch base(unescaped.Replace(words[0])) {
		case "shift", "repeat", "return", "break", "continue":
		case "printf":
			args = printfArguments(args)
		case "print":
			if !slices.ContainsFunc(args, func(w string) bool { return len(w) > 1 && w[0] == '-' && strings.Contains(w, "f") }) {
				continue
			}
		default:
			continue
		}
		if slices.ContainsFunc(args, assignsIn) {
			return true
		}
	}
	return false
}

// printfArguments is printf's words after its options and its format.
func printfArguments(words []string) []string {
	for i := 0; i < len(words); i++ {
		switch w := words[i]; {
		case w == "--":
			return words[min(i+2, len(words)):]
		case w == "-v":
			i++
		case strings.HasPrefix(w, "-") && len(w) > 1:
		default:
			return words[i+1:]
		}
	}
	return nil
}

// assignsIn reports an assigning operator anywhere in s, as assignsAt reads
// one.
func assignsIn(s string) bool {
	for p := range len(s) {
		if assignsAt(s, p, false) {
			return true
		}
	}
	return false
}

// assignsAt reports an assigning operator at s[p]: a = that is not part of
// ==, !=, <=, >= or =~, which takes in op= and <<= and >>=; or the first of
// ++ or --. Where standing is set, as among a test's words, a = with a blank
// on both sides is a comparison, and ++ or -- counts only stuck to a name or
// to a word the shell makes, so that a `--` standing as a word is none. A
// test that mixes -eq with `== --help` is read as a change: `--help` is a
// decrement of help where it stands as an operand.
func assignsAt(s string, p int, standing bool) bool {
	switch c := s[p]; c {
	case '=':
		return equalsAssigns(s, p) && (!standing || p == 0 || !isBlank(s[p-1]) || p+1 < len(s) && !isBlank(s[p+1]))
	case '+', '-':
		if p+1 >= len(s) || s[p+1] != c || p > 0 && s[p-1] == c {
			return false
		}
		return !standing || stuckTo(s, p-1) || stuckTo(s, p+2)
	}
	return false
}

// equalsAssigns reports the = at s[p] as no part of ==, !=, <=, >= or =~.
func equalsAssigns(s string, p int) bool {
	if p+1 < len(s) && (s[p+1] == '=' || s[p+1] == '~') {
		return false
	}
	if p == 0 {
		return true
	}
	switch prev := s[p-1]; prev {
	case '=', '!':
		return false
	case '<', '>':
		return p >= 2 && s[p-2] == prev
	}
	return true
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' || c == '\n' }

// stuckTo reports a byte of a name, or the end or start of a word the shell
// makes, at s[i].
func stuckTo(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	return nameByte(s[i]) || strings.IndexByte("$}`)]\"'", s[i]) >= 0
}

// testOperatorAt reports -eq, -ne, -lt, -le, -gt or -ge standing as a word at
// text[i].
func testOperatorAt(text string, i int) bool {
	if i+3 > len(text) || text[i] != '-' || i > 0 && !isBlank(text[i-1]) || i+3 < len(text) && !isBlank(text[i+3]) {
		return false
	}
	switch text[i+1 : i+3] {
	case "eq", "ne", "lt", "le", "gt", "ge":
		return true
	}
	return false
}

// The places arithmeticScan reads.
const (
	frameCommands   = iota // the line, `$(…)`, `(…)`, or a backtick's
	frameDouble            // a double-quoted string
	frameArithmetic        // `((…))` and `$((…))`
	frameBracket           // `$[…]`, and a subscript
	frameBrace             // `${…}` past its head
	frameOffset            // `${name:…}`
	frameTest              // `[[ … ]]`
)

// arithFrame is one place open on arithmeticScan's stack: the byte that
// closes a commands frame, the parentheses open inside an arithmetic one,
// whether its bytes are arithmetic, and the test whose words they are, or -1.
// A test frame notes whether it holds an arithmetic operator and an
// assigning one, and refuses at its end where it holds both.
type arithFrame struct {
	kind    int
	closer  byte
	parens  int
	arith   bool
	test    int
	arithOp bool
	assigns bool
}

// arithmeticFrames bounds the stack: a line nested deeper is read as a
// change, since nothing a person writes nests that deep, and the stack, like
// the pass, stays bounded.
const arithmeticFrames = 4 * maxNesting

// arithmeticScan reads the line once, with a stack of what is open, and asks
// each byte read where it is arithmetic whether an assigning operator starts
// there. Only the frame the byte is in decides, so a command inside
// arithmetic, `$(( $(git rev-list --count HEAD) ))`, is read as a command.
// Every place it reads opens with `(`, `[` or `{`, the last for the offset
// and length of `${name:…}`: a line with none of the three is passed over,
// and one that skipped `{` let `${PWD:GOMAXPROCS++}` through.
func arithmeticScan(text string) bool {
	if !strings.ContainsAny(text, "([{") {
		return false
	}
	s := arithStack{text: text, frames: []arithFrame{{kind: frameCommands, test: -1}}}
	for i := 0; i < len(text); {
		next, refused := s.step(i)
		if refused || len(s.frames) > arithmeticFrames {
			return true
		}
		i = next
	}
	// What the line leaves open runs to its end.
	for len(s.frames) > 1 {
		if s.pop() {
			return true
		}
	}
	return false
}

// arithStack is arithmeticScan's state.
type arithStack struct {
	text   string
	frames []arithFrame
}

func (s *arithStack) top() *arithFrame { return &s.frames[len(s.frames)-1] }

// push opens a frame of kind, which inherits from the one it opens in
// whether its bytes are arithmetic and whose test words they are, as kind
// says.
func (s *arithStack) push(kind int, closer byte) {
	t := s.top()
	f := arithFrame{kind: kind, closer: closer, test: -1}
	switch kind {
	case frameArithmetic, frameBracket, frameOffset:
		f.arith = true
	case frameDouble, frameBrace:
		f.arith, f.test = t.arith, t.test
		if t.kind == frameTest {
			f.test = len(s.frames) - 1
		}
	case frameTest:
		f.test = len(s.frames)
	}
	s.frames = append(s.frames, f)
}

// pop closes the top frame, and reports a test that held an arithmetic
// operator and an assigning one.
func (s *arithStack) pop() bool {
	f := s.frames[len(s.frames)-1]
	s.frames = s.frames[:len(s.frames)-1]
	return f.kind == frameTest && f.arithOp && f.assigns
}

// read asks the byte at i, where the top frame is arithmetic or a test's
// words, whether an assigning operator starts there.
func (s *arithStack) read(i int) bool {
	t := s.top()
	switch {
	case t.arith:
		return assignsAt(s.text, i, false)
	case t.test >= 0:
		test := &s.frames[t.test]
		if t.kind == frameTest && testOperatorAt(s.text, i) {
			test.arithOp = true
		}
		test.assigns = test.assigns || assignsAt(s.text, i, t.kind == frameTest)
	}
	return false
}

// step reads what starts at i, and gives where the scan goes on and whether
// the line is refused.
func (s *arithStack) step(i int) (int, bool) {
	if next, refused, ok := s.open(i); ok {
		return next, refused
	}
	if s.top().kind == frameCommands {
		return s.command(i)
	}
	if next, refused, ok := s.close(i); ok {
		return next, refused
	}
	if s.text[i] == '[' && i > 0 && nameByte(s.text[i-1]) {
		s.push(frameBracket, 0)
		return i + 1, false
	}
	return i + 1, s.read(i)
}

// open reads a quote, an escape or an expansion that starts at i, wherever
// it stands, and says whether one did.
func (s *arithStack) open(i int) (next int, refused, ok bool) {
	text, t := s.text, s.top()
	at := func(p string) bool { return strings.HasPrefix(text[i:], p) }
	c := text[i]
	switch {
	case c == '\\':
		return i + 2, false, true
	case c == '\'' && t.kind != frameDouble:
		next, refused = s.singleQuoted(i)
		return next, refused, true
	case c == '"' && t.kind == frameDouble:
		return i + 1, s.pop(), true
	case c == '"':
		s.push(frameDouble, 0)
		return i + 1, false, true
	case at("$(("):
		s.push(frameArithmetic, 0)
		return i + 3, false, true
	case at("$("):
		s.push(frameCommands, ')')
		return i + 2, false, true
	case at("$["):
		s.push(frameBracket, 0)
		return i + 2, false, true
	case at("${"):
		s.push(frameBrace, 0)
		next, refused = s.braceHead(i + 2)
		return next, refused, true
	case c == '`' && t.kind == frameCommands && t.closer == '`':
		return i + 1, s.pop(), true
	case c == '`':
		s.push(frameCommands, '`')
		return i + 1, false, true
	}
	return i, false, false
}

// singleQuoted reads the single-quoted string that starts at i. In
// arithmetic its body is arithmetic; elsewhere it is read as a line of its
// own, since what bash's `${1@P}` or zsh's `${(e)…}` expands, and the name
// unset or test -v is given with a subscript, `'a[V++]'`, are evaluated.
func (s *arithStack) singleQuoted(i int) (int, bool) {
	t := s.top()
	end := singleQuoteEnd(s.text, i)
	body := s.text[i+1 : min(end, len(s.text))]
	if t.arith && assignsIn(body) || !t.arith && arithmeticScan(body) {
		return 0, true
	}
	if t.test >= 0 && assignsIn(body) {
		s.frames[t.test].assigns = true
	}
	return end + 1, false
}

// close reads what ends or opens a place at i inside arithmetic, a bracket,
// a brace or a test, and says whether it did.
func (s *arithStack) close(i int) (next int, refused, ok bool) {
	text, t := s.text, s.top()
	c := text[i]
	switch t.kind {
	case frameArithmetic:
		switch {
		case c == '(':
			t.parens++
		case c == ')' && t.parens > 0:
			t.parens--
		case strings.HasPrefix(text[i:], "))"):
			return i + 2, s.pop(), true
		}
	case frameBracket:
		if c == ']' {
			return i + 1, s.pop(), true
		}
	case frameBrace, frameOffset:
		if c == ':' && t.kind == frameBrace && text[i-1] == ']' && i+1 < len(text) && strings.IndexByte("-=?+", text[i+1]) < 0 {
			// The offset after a subscript, `${a[@]:V++}`.
			s.push(frameOffset, 0)
			return i + 1, false, true
		}
		if c == '}' {
			refused = t.kind == frameOffset && s.pop()
			return i + 1, s.pop() || refused, true
		}
	case frameTest:
		if strings.HasPrefix(text[i:], "]]") {
			return i + 2, s.pop(), true
		}
	}
	return i, false, false
}

// command reads what starts at i among commands.
func (s *arithStack) command(i int) (int, bool) {
	text, t := s.text, s.top()
	at := func(p string) bool { return strings.HasPrefix(text[i:], p) }
	c := text[i]
	switch {
	case c == t.closer && len(s.frames) > 1:
		return i + 1, s.pop()
	case c == '#' && (i == 0 || strings.IndexByte(" \t\n;&|()", text[i-1]) >= 0):
		if j := strings.IndexByte(text[i:], '\n'); j >= 0 {
			return i + j, false
		}
		return len(text), false
	case at("(("):
		s.push(frameArithmetic, 0)
		return i + 2, false
	case c == '(':
		s.push(frameCommands, ')')
	case at("[[") && (i == 0 || strings.IndexByte(" \t\n;&|(!", text[i-1]) >= 0):
		s.push(frameTest, 0)
		return i + 2, false
	case c == '[' && i > 0 && nameByte(text[i-1]):
		s.push(frameBracket, 0)
	}
	return i + 1, false
}

// braceHead reads the head of a `${` whose body starts at i: `#` or `!`,
// zsh's flags, which are arithmetic where they take a width, and the name;
// and opens a subscript or an offset where one follows.
func (s *arithStack) braceHead(i int) (int, bool) {
	text := s.text
	if i < len(text) && (text[i] == '#' || text[i] == '!') {
		i++
	}
	if i < len(text) && text[i] == '(' {
		end := strings.IndexByte(text[i:], ')')
		if end < 0 {
			end = len(text) - i
		}
		if assignsIn(text[i+1 : i+end]) {
			return 0, true
		}
		i += end + 1
	}
	i = pastParameter(text, i)
	switch {
	case i < len(text) && text[i] == '[':
		s.push(frameBracket, 0)
		return i + 1, false
	case i+1 < len(text) && text[i] == ':' && strings.IndexByte("-=?+", text[i+1]) < 0:
		s.push(frameOffset, 0)
		return i + 1, false
	}
	return i, false
}

// pastParameter is i past the name of a `${` head that starts there, or past
// a special parameter that an offset follows: bash stepped V for `${@:V++}`,
// `${*:V++}`, `${$:V++}`, `${-:V++}` and `${?:V++}` as for `${PWD:V++}`.
func pastParameter(text string, i int) int {
	start := i
	for i < len(text) && nameByte(text[i]) {
		i++
	}
	if i == start && i+1 < len(text) && strings.IndexByte("@*?$-", text[i]) >= 0 && text[i+1] == ':' {
		return i + 1
	}
	return i
}
