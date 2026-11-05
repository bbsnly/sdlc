package shellpolicy

import (
	"strings"
	"unicode/utf8"
)

// This file makes the reading of a line that the path rules need where the
// shell keeps whole what they split. They split a command at its separators
// by its text, quotes and all (segments), which reads more everywhere but in a
// word that holds one: `rm 'a;b' .sdlc/stories/US-1/trace.jsonl` was a command
// `rm 'a` and a command `b' .sdlc/…`, and the trace's name stood in no rm. So
// did tee's, sed -i's and mv's, aimed at the gate record and the lock; and a
// process substitution's `)`, and a backslash before a carriage return and a
// newline, moved the path out of its command the same way (US-112, Gate 7
// round 4, security notes 3 to 5).

// addWordReadings adds, for each reading made so far, one in which what the
// shell keeps in a word is written so that the path rules keep it there too
// (wordsKept). It is added to the others, so it can only add refusals, and a
// text with no quote, no parameter, no process substitution, no carriage
// return and no escaped backslash before a newline pays nothing for it.
func addWordReadings(out []pair, reading assignments) []pair {
	for _, p := range out {
		if !mayKeepWords(p.text) {
			continue
		}
		if text := wordsKept(p.text, reading); text != p.text {
			out = addReadings(out, []string{text}, p.command, p.undecoded, p.via)
		}
	}
	return out
}

// mayKeepWords is the cheap test on a text: whether it holds anything
// wordsKept rewrites.
func mayKeepWords(text string) bool {
	return strings.ContainsAny(text, "'\"\r") || strings.Contains(text, "${") ||
		strings.Contains(text, "<(") || strings.Contains(text, ">(") ||
		strings.Contains(text, "=(") || strings.Contains(text, "\\\\\n")
}

// wordsKept is text with wordsScan's rewrites made.
func wordsKept(text string, reading assignments) string {
	rs := []rune(text)
	at := make([]int, len(rs))
	for i := range at {
		at[i] = i
	}
	s := &wordsScan{edits: map[int]string{}, read: map[[3]int]bool{}}
	s.line(rs, at, 0, reading)
	if len(s.edits) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + len(s.edits))
	for i, r := range rs {
		if e, ok := s.edits[i]; ok {
			b.WriteString(e)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// splitAt are the characters segments ends a command at, with the `(` of
// `$(`. Inside a word, each is written as kept.
const splitAt = ";&|()`\n"

// kept is what a character of splitAt inside a word is written as, U+FFFD:
// one no name a rule protects holds, so that a pattern holding it matches
// less than it did and never more. Written as `_`, find's
// `! -name "*;*"` became `! -name "*_*"`, which a frozen x_test.go matches.
const kept = string(utf8.RuneError)

// wordsScan reads a line as the shell splits it into commands and words, in
// the common forms and not the shell's whole grammar: a substitution ends at
// the `)` that balances its `(` (closing), which a `case` pattern's `)` does
// too. It notes, by the index of each character in the text, what the
// reading writes in its place:
//
//   - a character of splitAt inside a quoted word, a `${...}` or a `$'...'`
//     is written as kept, unless the command hands that word to a shell, eval
//     or trap as a script, whose separators are its own and whose quotes are
//     read in turn;
//
//   - a process substitution, `<(...)` or `>(...)` anywhere in a word, or
//     zsh's `=(...)` at a word's start, is the `$(...)` it is to the path
//     rules: a word the shell fills in, and a command of its own;
//
//   - a newline after a backslash and a carriage return, or after an escaped
//     backslash, has a blank put in front of it, so that escapedSeparators
//     does not take the backslash and the newline out as a line
//     continuation: the backslash escapes only the character before the
//     newline, and the newline ends the command;
//
//   - a separator just after a quote has a blank put in front of it, so that
//     escapedSeparators does not read the quote, the separator and the quote
//     after it as one quoted separator. In this line the `|` is a pipe, and
//     rm removed FILE, as it did with two single quotes in place of the
//     double ones (US-112, Gate 7 round 5, security N3):
//
//     true ""|'rm' FILE
//
// A quoted operator alone, find's `';'`, is left as escapedSeparators reads it.
// Past maxNesting levels of substitutions and scripts the scan reads no
// further; the program walk refuses such a line.
type wordsScan struct {
	edits map[int]string
	// read holds the text each substitution read so far ran, by its length
	// and the indexes of its first and last characters, so that one is read
	// once. Read again where a handed script was read whole, each level was
	// read twice: `bash -c "; $(…)"` twenty deep took 7.6 s (US-112, Gate 7
	// round 5, code review N1). A substitution's text is read as a line of
	// its own, so a second reading writes the edits the first one did. Two
	// texts with an escape taken out at different places can share a key;
	// the second is then not read, which writes fewer edits, and an edit
	// left out can only leave this reading nearer the one it was made from.
	read map[[3]int]bool
}

// wordsCommand is one command part way through the scan: its words so far,
// unquoted, each word's characters as written, and the quoted parts of them.
type wordsCommand struct {
	words   []string
	written []quotedPart
	word    strings.Builder
	current quotedPart
	started bool
	target  bool
	parts   []quotedPart
}

// quotedPart is a quoted word, or a quoted piece of one: the characters a
// script handed on is read from, the index of each in the text, and whether
// each is a character of the word whatever the command does with it, which a
// substitution's are not.
type quotedPart struct {
	rs    []rune
	at    []int
	fixed []bool
}

func (p *quotedPart) put(rs []rune, at []int, fixed bool) {
	p.rs, p.at = append(p.rs, rs...), append(p.at, at...)
	for range rs {
		p.fixed = append(p.fixed, fixed)
	}
}

// add adds the character at i to the word being read.
func (c *wordsCommand) add(rs []rune, at []int, i int) {
	c.word.WriteRune(rs[i])
	c.current.put(rs[i:i+1], at[i:i+1], true)
	c.started = true
}

// endWord ends the word being read. The word after a redirect is where it
// goes, and not one of the command's words.
func (c *wordsCommand) endWord() {
	if c.started {
		if !c.target {
			c.words = append(c.words, c.word.String())
			c.written = append(c.written, c.current)
		}
		c.target = false
	}
	c.word.Reset()
	c.current = quotedPart{}
	c.started = false
}

// part adds a quoted piece to the word being read.
func (c *wordsCommand) part(p quotedPart) {
	c.word.WriteString(string(p.rs))
	c.current.rs = append(c.current.rs, p.rs...)
	c.current.at = append(c.current.at, p.at...)
	c.current.fixed = append(c.current.fixed, p.fixed...)
	c.started = true
	c.parts = append(c.parts, p)
}

// line reads rs, a line of commands whose characters stand at at in the text.
func (s *wordsScan) line(rs []rune, at []int, depth int, reading assignments) {
	if depth > maxNesting {
		return
	}
	c := &wordsCommand{}
	for i := 0; i < len(rs); i++ {
		if end, ok := s.quoting(rs, at, i, c, depth, reading); ok {
			i = end
			continue
		}
		if end, ok := s.substitution(rs, at, i, c, depth, reading); ok {
			i = end
			continue
		}
		start := i
		var ended bool
		if i, ended = c.plain(rs, at, i); ended {
			if start > 0 && strings.ContainsRune(";&|)", rs[start]) && (rs[start-1] == '\'' || rs[start-1] == '"') {
				s.edits[at[start]] = " " + string(rs[start])
			}
			s.command(c, depth, reading)
			c = &wordsCommand{}
		}
	}
	s.command(c, depth, reading)
}

// quoting reads the character at i where it opens an escape, a quote or a
// parameter, and returns the index of the last character it read. It is not
// ok for any other character, of which it has read nothing.
func (s *wordsScan) quoting(rs []rune, at []int, i int, c *wordsCommand, depth int, reading assignments) (int, bool) {
	r, next := rs[i], runeAt(rs, i+1)
	switch {
	case r == '\\' && next != 0:
		if (next == '\r' || next == '\\') && runeAt(rs, i+2) == '\n' {
			s.edits[at[i+2]] = " \n"
		}
		if next != '\n' {
			c.add(rs, at, i+1)
		}
		return i + 1, true
	case r == '\'', r == '$' && next == '\'':
		from, end := i+1, find(rs, i+1, '\'')
		if r == '$' {
			from, end = i+2, ansiEnd(rs, i+2)
		}
		var p quotedPart
		p.put(rs[from:end], at[from:end], true)
		c.part(p)
		return end, true
	case r == '$' && next == '{':
		return c.parameter(rs, at, i), true
	case r == '"':
		return s.double(rs, at, i+1, c, depth, reading), true
	}
	return i, false
}

// substitution reads a command or a process substitution at i, as a line of
// its own and a word the shell fills in, and returns the index of the last
// character it read. It is not ok for any other character.
//
// bash and zsh read `<(...)` and `>(...)` anywhere in a word, and zsh reads
// `=(...)` at a word's start: read only at a word's start, `rm -f a<(true)
// FILE` and `cat x<(true) > FILE` moved FILE out of its command, and bash
// removed or emptied it (US-112, Gate 7 round 5, code review B1).
func (s *wordsScan) substitution(rs []rune, at []int, i int, c *wordsCommand, depth int, reading assignments) (int, bool) {
	r, next := rs[i], runeAt(rs, i+1)
	var from, end int
	switch {
	case r == '`':
		from, end = i+1, find(rs, i+1, '`')
	case r == '$' && next == '(':
		from, end = i+2, closing(rs, i+2)
	case (r == '<' || r == '>' || r == '=' && !c.started) && next == '(':
		from, end = i+2, closing(rs, i+2)
		s.edits[at[i]] = "$"
	default:
		return i, false
	}
	s.substituted(rs[from:end], at[from:end], depth+1, reading)
	c.word.WriteString("$SUB")
	stop := min(end+1, len(rs))
	c.current.put(rs[i:stop], at[i:stop], false)
	c.started = true
	return end, true
}

// substituted reads rs, the text a substitution runs, as a line of its own,
// unless it has been read already (wordsScan.read).
func (s *wordsScan) substituted(rs []rune, at []int, depth int, reading assignments) {
	key := [3]int{len(at)}
	if len(at) > 0 {
		key = [3]int{len(at), at[0], at[len(at)-1]}
	}
	if !s.read[key] {
		s.read[key] = true
		s.line(rs, at, depth, reading)
	}
}

// plain reads the character at i, one quoting and substitution did not take,
// and returns the index of the last character it read and whether it ended
// the command.
func (c *wordsCommand) plain(rs []rune, at []int, i int) (int, bool) {
	r, next := rs[i], runeAt(rs, i+1)
	switch {
	case r == '#' && !c.started && (i == 0 || strings.ContainsRune(wordStarts, rs[i-1])):
		// A comment starts only at a word's start: read after `{`, `}` or
		// a carriage return, `rm {#'a;b' FILE` hid FILE (security N2).
		return find(rs, i, '\n') - 1, false
	case r == ' ' || r == '\t' || r == '\r':
		c.endWord()
	case r == '<' || r == '>' || r == '&' && next == '>':
		c.endWord()
		// The operator stops before a process substitution, which zsh
		// reads in `>>(...)` and `<<(...)`.
		for i+1 < len(rs) && strings.ContainsRune("<>&|-", rs[i+1]) &&
			(!strings.ContainsRune("<>", rs[i+1]) || runeAt(rs, i+2) != '(') {
			i++
		}
		c.target = true
	case r == '{' && next == '}':
		c.add(rs, at, i)
		c.add(rs, at, i+1)
		return i + 1, false
	case (r == '{' || r == '}') && c.started:
		c.add(rs, at, i)
	case strings.ContainsRune("\n;&|(){}", r):
		return i, true
	default:
		c.add(rs, at, i)
	}
	return i, false
}

// runeAt is rs[i], or 0 past its end.
func runeAt(rs []rune, i int) rune {
	if i < len(rs) {
		return rs[i]
	}
	return 0
}

// double reads a double-quoted string from just after its opening quote, and
// returns where it closes. A substitution inside it is a line of its own,
// and a `${...}` is followed to its end, quotes and all: read to the first
// quote, `rm "${x:-"a;b"}" FILE` ended the string at the inner one, and the
// `;` was read as the end of rm, which removed FILE (US-112, Gate 7 round 5,
// security N4).
func (s *wordsScan) double(rs []rune, at []int, i int, c *wordsCommand, depth int, reading assignments) int {
	var p quotedPart
	for ; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '"':
			c.part(p)
			return i
		case r == '\\' && i+1 < len(rs) && strings.ContainsRune("\"\\$`\n", rs[i+1]):
			i++
			if rs[i] != '\n' {
				p.put(rs[i:i+1], at[i:i+1], true)
			}
		case r == '$' && runeAt(rs, i+1) == '{':
			end := min(parameterEnd(rs, i), len(rs)-1)
			p.put(rs[i:end+1], at[i:end+1], true)
			i = end
		case r == '$' && runeAt(rs, i+1) == '(', r == '`':
			end := find(rs, i+1, '`')
			from := i + 1
			if r == '$' {
				end, from = closing(rs, i+2), i+2
			}
			s.substituted(rs[from:end], at[from:end], depth+1, reading)
			stop := min(end+1, len(rs))
			p.put(rs[i:stop], at[i:stop], false)
			i = end
		default:
			p.put(rs[i:i+1], at[i:i+1], true)
		}
	}
	c.part(p)
	return i
}

// parameter reads a `${...}` from its `$` at i, and returns the index of the
// brace that closes it: one word, whatever separators it holds, so that `rm
// ${x:-a;b} FILE` removes FILE. Each character inside is taken for one of
// the word, a substitution's too: that reads less of what it runs than the
// shell does, and the other readings read that.
func (c *wordsCommand) parameter(rs []rune, at []int, i int) int {
	end := parameterEnd(rs, i)
	var p quotedPart
	p.put(rs[i+2:end], at[i+2:end], true)
	c.part(p)
	return end
}

// parameterEnd is the index of the brace that closes the `${...}` whose `$`
// is at i, or the end of rs, past escapes, quotes, and the braces and
// parentheses nested in it. Counted to the first brace, `rm ${x:-\}a;b}
// FILE` ended at the escaped one, and `rm "${x:-"a;b"}" FILE` at the quote
// inside it, and in each rm removed FILE (US-112, Gate 7 round 5, security
// N4).
func parameterEnd(rs []rune, i int) int {
	var q rune
	braces, parens := 0, 0
	for j := i + 1; j < len(rs); j++ {
		switch r := rs[j]; {
		case q == '\'' && r != q:
		case r == '\\':
			j++
		case r == q:
			q = 0
		case q != 0:
		case r == '\'' || r == '"':
			q = r
		case r == '(':
			parens++
		case r == ')':
			parens--
		case r == '{':
			braces++
		case r == '}' && parens <= 0:
			if braces--; braces == 0 {
				return j
			}
		}
	}
	return len(rs)
}

// command ends a command: each of its quoted parts that holds a character
// segments splits at is written over, or, where the command hands its words
// to a shell, read as the script it is.
func (s *wordsScan) command(c *wordsCommand, depth int, reading assignments) {
	c.endWord()
	var parts []quotedPart
	for _, p := range c.parts {
		if splits(p) {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return
	}
	if handsAScript(c.words, reading) {
		for _, script := range scripts(c) {
			s.line(script.rs, script.at, depth+1, reading)
		}
		return
	}
	for _, p := range parts {
		for k, r := range p.rs {
			if p.fixed[k] && strings.ContainsRune(splitAt, r) {
				s.edits[p.at[k]] = kept
			}
		}
	}
}

// scripts are the scripts a command that hands its words on runs, each read
// whole: eval's and trap's words joined by blanks, as the shell joins them,
// and otherwise each word that holds a quoted separator, as a shell's -c
// runs the one. Read one quoted part at a time, `sh -c 'rm '"'"'a;b'"'"'
// FILE'` and `eval 'rm "a' ';b"' FILE` each split the word rm was handed,
// and rm removed FILE (US-112, Gate 7 round 5, security N4). A blank eval
// puts between two words stands nowhere in the text, so it is at -1, where
// no edit is ever written.
func scripts(c *wordsCommand) []quotedPart {
	for k := range c.words {
		handed, from, ok := evaluated(opened(c.words[k]), c.words[k:])
		if !ok {
			continue
		}
		var script quotedPart
		for j, w := range c.written[k+from : k+from+len(handed)] {
			if j > 0 {
				script.put([]rune{' '}, []int{-1}, true)
			} else {
				w = gluedTail(w, handed[0])
			}
			script.put(w.rs, w.at, true)
		}
		return []quotedPart{script}
	}
	var out []quotedPart
	for _, w := range c.written {
		if splits(w) {
			out = append(out, w)
		}
	}
	return out
}

// splits reports whether a quoted part holds a character segments splits
// at, other than as an operator alone that escapedSeparators reads already,
// or a `(` alone, at which segments does not split.
func splits(p quotedPart) bool {
	if len(p.rs) == 1 && strings.ContainsRune(";&|()", p.rs[0]) {
		return false
	}
	for k, r := range p.rs {
		if p.fixed[k] && strings.ContainsRune(splitAt, r) {
			return true
		}
	}
	return false
}

// handsAScript reports whether every reading of a command's words hands its
// words on to a shell, eval or trap as a script, so that a quoted word of it
// is read as a script's text and not as a word.
func handsAScript(words []string, reading assignments) bool {
	for len(words) > 0 && reading.is(words[0]) {
		words = words[1:]
	}
	if len(words) == 0 {
		return false
	}
	// A command whose readings were cut short at the cap is refused by both
	// walks whatever this reading says, so the readings made are enough.
	run := programUnderEval(words, reading)
	for _, r := range everyReading(run) {
		if !r.shell && r.evals == 0 {
			return false
		}
	}
	return true
}

// gluedTail is the end of the written word w that holds the first word handed
// on, where that is glued to the builtin's own: an alias's value after its
// `name=`, and mapfile's callback after -C. Read with the word whole, the
// script handed on began `r=rm …`, an assignment in front of a command
// called -f.
func gluedTail(w quotedPart, first string) quotedPart {
	n := utf8.RuneCountInString(first)
	if n >= len(w.rs) {
		return w
	}
	cut := len(w.rs) - n
	return quotedPart{rs: w.rs[cut:], at: w.at[cut:], fixed: w.fixed[cut:]}
}

// ansiEnd is the index of the quote that closes a `$'...'` whose text starts
// at i, where a backslash escapes the character after it.
func ansiEnd(rs []rune, i int) int {
	for ; i < len(rs); i++ {
		switch rs[i] {
		case '\\':
			i++
		case '\'':
			return i
		}
	}
	return len(rs)
}
