// Package codemap is the format of a story's change to the root CODEMAP.md:
// what a stored set of sections is, what a section of a map is, and what
// applying one to the other gives. It reads no file, runs no git and knows no
// story.
//
// The sections are text a model wrote, and `sdlc stop` puts them on trunk with
// no reviewer in between. So a set is parsed strictly and refused, never
// repaired; one reading of a heading and of a fence serves the stored set and
// trunk's file alike, so the two cannot disagree about where a section ends;
// and a set is applied whole or not at all.
package codemap

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The line a section of a stored set begins with. The line after it is the
// section's heading.
const (
	AddLine     = "<!-- sdlc:add -->"
	ReplaceLine = "<!-- sdlc:replace -->"
)

// The bounds of a stored set. The stop applies a set while it holds the
// landing lock, and every later reader of the map pays for what it added.
const (
	MaxBytes    = 1 << 18
	MaxSections = 64
)

// maxHeading is the most bytes a heading may be. A heading goes whole into the
// record, the commit's message and what the stop prints, each time.
const maxHeading = 200

// ErrDoesNotApply is in the chain of the error Apply returns for a set that is
// well formed and is not what the bookkeeper saw: trunk's map has changed
// under one of its sections. It is the one refusal a person's `--replace`
// answers.
var ErrDoesNotApply = errors.New("the sections do not apply to the map as it is")

// ErrAlreadyThere is in the chain of the error Apply returns for a set the
// map already holds, every section exactly as it was stored: there is nothing
// to commit, and nothing a person's `--replace` would change. It is what a
// stop killed after its commit leaves, and what a set of sections sent
// unchanged is.
var ErrAlreadyThere = errors.New("the map already holds every section exactly as it was stored")

// ErrCannotTell is in the chain of the error Apply returns where a section on
// trunk that the set would change cannot be told where it ends. A person's
// `--replace` is refused the same way, so the only way on is a person's own
// edit of the map.
var ErrCannotTell = errors.New("the sections cannot be put into the map as it is")

// Section is one section of a stored set.
type Section struct {
	Replace bool   // false adds the section, true replaces the one by this heading
	Heading string // the heading's text, without "## "
	Body    string // the lines under it, no blank line first or last, ending "\n"
}

// Parse reads a stored set as its sections, in order. Anything that is not a
// directive line, a heading line and a body is refused.
func Parse(stored []byte) ([]Section, error) {
	if len(stored) > MaxBytes {
		return nil, fmt.Errorf("the set is more than %d bytes, which is the most a set may be", MaxBytes)
	}
	var (
		out  []Section
		body []string
		open bool
	)
	closeSection := func() {
		if open {
			out[len(out)-1].Body = trimmedBody(body)
		}
		body = body[:0]
	}
	lines := strings.Split(string(stored), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case line == AddLine || line == ReplaceLine:
			closeSection()
			// A directive on the last line has nothing after it, which is
			// refused as any line that is no heading is.
			next := strings.Join(lines[i+1:min(i+2, len(lines))], "")
			text, ok := strings.CutPrefix(next, "## ")
			if !ok {
				return nil, fmt.Errorf("the line after %s is %q, and it has to be a heading: two hash marks, a space and a text",
					line, clipped(next))
			}
			out = append(out, Section{Replace: line == ReplaceLine, Heading: text})
			open = true
			i++
		case open:
			body = append(body, line)
		case strings.TrimSpace(line) != "":
			return nil, fmt.Errorf("the set begins with %q, and it has to begin with the line %s or %s",
				clipped(line), AddLine, ReplaceLine)
		}
	}
	closeSection()
	if err := checkSet(out); err != nil {
		return nil, err
	}
	return out, nil
}

// trimmedBody is a body's lines without the blank ones round them. Those are
// the format's, not the section's.
func trimmedBody(lines []string) string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// clipped is the start of a line for a message: a refused line may be a
// megabyte long.
func clipped(line string) string {
	const most = 60
	return strings.ToValidUTF8(line[:min(len(line), most)], "")
}

// checkSet holds a set to what Parse promises of one. Apply asks it again:
// what reaches it may have been built by hand, or read from a file changed
// since it was checked.
func checkSet(sections []Section) error {
	if len(sections) == 0 {
		return errors.New("the set holds no section")
	}
	if len(sections) > MaxSections {
		return fmt.Errorf("the set holds %d sections, and the most a set may hold is %d", len(sections), MaxSections)
	}
	seen := make(map[string]bool, len(sections))
	for _, s := range sections {
		if err := checkHeading(s.Heading); err != nil {
			return err
		}
		if err := checkBody(s); err != nil {
			return err
		}
		key := strings.ToLower(s.Heading)
		if seen[key] {
			return fmt.Errorf("the set names the section %q twice", s.Heading)
		}
		seen[key] = true
	}
	return nil
}

// checkHeading refuses a heading that is not a plain text with nothing round
// it: written after "## " it has to read back as itself, and as nothing a
// person would take for another heading.
func checkHeading(text string) error {
	if text == "" {
		return errors.New("a section's heading has no text")
	}
	if len(text) > maxHeading {
		return fmt.Errorf("the heading that begins %q is more than %d bytes, which is the most a heading may be", clipped(text), maxHeading)
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("the heading %q is not UTF-8 text", text)
	}
	for _, r := range text {
		if r != ' ' && (unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r)) {
			return fmt.Errorf("the heading %q holds a character that does not print (%U)", text, r)
		}
	}
	if level, read, ok := heading("## " + text); !ok || level != 2 || read != text {
		return fmt.Errorf("the heading %q has to be a text with no space before or after it and no closing hash marks", text)
	}
	return nil
}

// checkBody refuses a body that would not stay inside its section once it is
// in the file: a heading at the section's level or above, a fence left open,
// a conflict marker, a directive, or bytes that are not plain text. A
// character that does not show (a byte-order mark, a zero-width space, a mark
// that turns the text round, a line separator) is refused as it is in a
// heading: the text on trunk would not read as its bytes.
func checkBody(s Section) error {
	refuse := func(format string, args ...any) error {
		return fmt.Errorf("the section %q %s", s.Heading, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(s.Body) == "" {
		return refuse("has nothing under its heading")
	}
	if !utf8.ValidString(s.Body) {
		return refuse("is not UTF-8 text")
	}
	lines := strings.Split(strings.TrimSuffix(s.Body, "\n"), "\n")
	if trimmedBody(slices.Clone(lines)) != s.Body {
		return refuse("is not its lines and one newline: it begins or ends with a blank line, or has no newline at its end")
	}
	var in fenced
	for _, line := range lines {
		if why := lineRefused(line, &in); why != "" {
			return refuse("%s", why)
		}
	}
	// The comment is asked of the body as it will be read on trunk: one stored
	// with a comment left open made its own section one no later story could
	// replace.
	if in.open() || read([]byte(rendered(s))).unsure(0) {
		return refuse("leaves something open: a fenced block with no closing fence, or the marks \"<!--\" with no \"-->\" after them " +
			"(where the text only speaks of the marks, write both, or neither)")
	}
	return nil
}

// lineRefused says why one line of a body cannot be in a section, and nothing
// for a line that can. in is the fenced block the lines before it left open:
// what reads as a heading inside one is text.
func lineRefused(line string, in *fenced) string {
	if at := strings.IndexFunc(line, func(r rune) bool {
		return r != '\t' && (unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp))
	}); at >= 0 {
		r, _ := utf8.DecodeRuneInString(line[at:])
		return fmt.Sprintf("holds a character that is not text (%U): a control character, a carriage return, or one that does not show", r)
	}
	switch {
	case conflictMarker(line):
		return fmt.Sprintf("holds a conflict marker: %q", clipped(line))
	case directiveLike(line):
		return fmt.Sprintf("holds the line %q, which reads as a directive", clipped(line))
	case in.step(line):
		return ""
	}
	if level, _, ok := heading(line); ok && level <= 2 {
		return fmt.Sprintf("holds a heading at its own level or above: %q", clipped(line))
	}
	if underline(line) {
		return fmt.Sprintf("holds a line of %q alone, which makes a heading of the line above it", clipped(strings.TrimSpace(line)))
	}
	return ""
}

// conflictMarker reports a line git's merge would have left.
func conflictMarker(line string) bool {
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>", "|||||||"} {
		if strings.HasPrefix(line, marker) {
			return true
		}
	}
	return false
}

// directiveLike reports a line that is a directive or was meant as one,
// however it is spaced or cased. Only the two exact lines are directives, and
// those open a section; anywhere else, each is refused.
func directiveLike(line string) bool {
	squeezed := strings.ToLower(strings.Join(strings.Fields(line), ""))
	return strings.HasPrefix(squeezed, "<!--sdlc:")
}

// underline reports a line of equals signs or of dashes alone, which makes a
// title or a section heading of the paragraph line above it.
func underline(line string) bool {
	t := strings.TrimSpace(line)
	return t != "" && (strings.Trim(t, "=") == "" || strings.Trim(t, "-") == "")
}

// heading reads a line as a heading written with hash marks: up to three
// spaces, one to six marks, then the end of the line or a space or tab. The
// text is without the space round it and without a closing run of marks.
func heading(line string) (level int, text string, ok bool) {
	t := strings.TrimRight(line, " \t\r")
	lead := strings.TrimLeft(t, " ")
	rest := strings.TrimLeft(lead, "#")
	level = len(lead) - len(rest)
	if len(t)-len(lead) > 3 || level < 1 || level > 6 || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
		return 0, "", false
	}
	text = strings.Trim(rest, " \t")
	if open := strings.TrimRight(text, "#"); open != text && (open == "" || strings.HasSuffix(open, " ") || strings.HasSuffix(open, "\t")) {
		text = strings.TrimRight(open, " \t")
	}
	return level, text, true
}

// fenced follows a fenced block from line to line. The zero value is outside
// one.
type fenced struct {
	mark byte
	run  int
}

func (f *fenced) open() bool { return f.run > 0 }

// step takes the next line and reports whether it is part of a fenced block,
// its opening and closing lines included.
func (f *fenced) step(line string) bool {
	t := strings.TrimRight(line, " \t\r")
	lead := strings.TrimLeft(t, " ")
	indented := len(t)-len(lead) > 3
	var mark byte
	run := 0
	if !indented && lead != "" && (lead[0] == '`' || lead[0] == '~') {
		mark = lead[0]
		run = len(lead) - len(strings.TrimLeft(lead, string(mark)))
	}
	info := lead[run:]
	switch {
	case f.open():
		if mark == f.mark && run >= f.run && info == "" {
			*f = fenced{}
		}
		return true
	case run >= 3 && (mark == '~' || !strings.Contains(info, "`")):
		// A backtick in what follows the marks makes the line a code span,
		// not a fence.
		*f = fenced{mark: mark, run: run}
		return true
	}
	return false
}

// A map as Apply reads it: its lines, each with its line ending, and where
// each section is.
type document struct {
	lines []string
	spans []span
}

// span is one section: its heading line and every line up to the next
// heading at its level or above, outside a fenced block.
type span struct {
	start, end int
	key        string
}

func read(text []byte) document {
	d := document{lines: strings.SplitAfter(string(text), "\n")}
	if n := len(d.lines); d.lines[n-1] == "" {
		d.lines = d.lines[:n-1]
	}
	var in fenced
	current := -1
	for i, line := range d.lines {
		if in.step(strings.TrimSuffix(line, "\n")) {
			continue
		}
		level, text, ok := heading(strings.TrimSuffix(line, "\n"))
		if !ok || level > 2 {
			continue
		}
		if current >= 0 {
			d.spans[current].end = i
			current = -1
		}
		if level == 2 {
			d.spans = append(d.spans, span{start: i, end: len(d.lines), key: strings.ToLower(text)})
			current = len(d.spans) - 1
		}
	}
	return d
}

// named is the sections by this heading: none, one, or more where a person
// wrote the heading twice.
func (d document) named(key string) []int {
	var at []int
	for i, s := range d.spans {
		if s.key == key {
			at = append(at, i)
		}
	}
	return at
}

// text is a section as it is compared: without the blank lines after it,
// which change with what follows the section and not with the section.
func (d document) text(i int) string {
	return strings.TrimRight(strings.Join(d.lines[d.spans[i].start:d.spans[i].end], ""), "\n \t\r")
}

func rendered(s Section) string { return "## " + s.Heading + "\n\n" + s.Body }

// SectionText is the section of a map by this heading, as Apply compares it,
// and false where the map has none by it or more than one. A caller that has
// more than one map the bookkeeper may have read asks each for a section and
// hands Apply the one to hold trunk's to.
func SectionText(doc []byte, heading string) (string, bool) {
	d := read(doc)
	at := d.named(strings.ToLower(heading))
	if len(at) != 1 {
		return "", false
	}
	return d.text(at[0]), true
}

// Apply gives trunk's map with the sections in it. trunk is the map now and
// cut is the map the bookkeeper read; nil is no file. An added section goes at
// the end after one blank line, and a replaced one takes the place of the
// section by its heading.
//
// A section that is not what the bookkeeper saw refuses the whole set, by
// name: an add of a heading trunk has, a replace of one it lacks or has
// twice, a replace of a section whose text on trunk is not its text at the
// cut. A replace of a section trunk still holds as the bookkeeper read it
// applies whatever the stored text is, the text trunk has included: a section
// sent unchanged beside one that changes is no reason to refuse the other.
// With replace set, a person's choice, the stored section goes over whatever
// trunk holds under that one heading.
//
// Two refusals hold whoever asks. A set trunk holds already, every section
// exactly, is ErrAlreadyThere: the result would be trunk's map as it is. And
// a section on trunk whose end cannot be told is not replaced.
func Apply(trunk, cut []byte, sections []Section, replace bool) ([]byte, error) {
	if err := checkSet(sections); err != nil {
		return nil, err
	}
	now := read(trunk)
	if names := now.holds(sections); names != "" {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyThere, names)
	}
	over := map[int]Section{}
	var added, refused []string
	kind := ErrDoesNotApply
	for _, s := range sections {
		at, why := now.place(cut, s, replace)
		switch {
		case why != "":
			refused = append(refused, why)
			if at == untold {
				kind = ErrCannotTell
			}
		case at < 0:
			added = append(added, rendered(s))
		default:
			over[at] = s
		}
	}
	if len(refused) > 0 {
		return nil, fmt.Errorf("%w: %s", kind, strings.Join(refused, "; "))
	}
	return now.with(over, added), nil
}

// untold is the place of a section that is refused because its end on trunk
// cannot be told, which is the one refusal neither flag of the repair answers.
const untold = -2

// place is where one section goes in the map: the section it takes the place
// of, or -1 for the end. why is set instead where the map is not what the
// bookkeeper saw for it.
func (d document) place(cut []byte, s Section, replace bool) (at int, why string) {
	found := d.named(strings.ToLower(s.Heading))
	switch {
	case len(found) > 1:
		return 0, fmt.Sprintf("the map has the section %q %d times, so there is no one section to change", s.Heading, len(found))
	case len(found) == 0 && s.Replace:
		return 0, fmt.Sprintf("the map has no section %q to replace", s.Heading)
	case len(found) == 0:
		return -1, ""
	case d.unsure(found[0]):
		return untold, fmt.Sprintf("the section %q in the map holds a heading written by underlining, or a comment left open, "+
			"so where the section ends cannot be told, and text put over it could take a person's lines away", s.Heading)
	case replace:
		return found[0], ""
	}
	on := d.text(found[0])
	if seen, ok := SectionText(cut, s.Heading); s.Replace && ok && seen == on {
		return found[0], ""
	}
	// Refused. Which sentence says why matters to the person who reads it:
	// the last is the only one that means the text is on trunk.
	why = "the map already has a section %q, which was to be added"
	if s.Replace {
		why = "the section %q in the map is no longer what the bookkeeper read"
	}
	if on == strings.TrimRight(rendered(s), "\n") {
		why = "the map already holds the section %q exactly as it was stored"
	}
	return 0, fmt.Sprintf(why, s.Heading)
}

// holds names the sections, where the map holds every one of them exactly as
// it was stored, and is empty where it does not.
func (d document) holds(sections []Section) string {
	names := make([]string, 0, len(sections))
	for _, s := range sections {
		at := d.named(strings.ToLower(s.Heading))
		if len(at) != 1 || d.text(at[0]) != strings.TrimRight(rendered(s), "\n") {
			return ""
		}
		names = append(names, strconv.Quote(s.Heading))
	}
	return strings.Join(names, ", ")
}

// unsure reports a section of the map that sdlc and a reader of the page may
// end at different lines: one that holds a heading written by underlining, or
// a comment left open, which makes text of the heading the section was ended
// at. A stored body may hold neither, and trunk's file may hold both. Text
// stored over such a section took a person's lines away with no refusal.
func (d document) unsure(i int) bool {
	var in fenced
	open, above := false, ""
	for _, line := range d.lines[d.spans[i].start+1 : d.spans[i].end] {
		line = strings.TrimSuffix(line, "\n")
		if !in.step(line) {
			if underline(line) && strings.TrimSpace(above) != "" {
				return true
			}
			open = strings.LastIndex(line, "<!--") > strings.LastIndex(line, "-->") || open && !strings.Contains(line, "-->")
		}
		above = line
	}
	return open
}

// with is the map with each section in over in the place of the one at its
// index, and the added ones at the end after one blank line.
func (d document) with(over map[int]Section, added []string) []byte {
	var out bytes.Buffer
	next := 0
	for i, sp := range d.spans {
		s, ok := over[i]
		if !ok {
			continue
		}
		out.WriteString(strings.Join(d.lines[next:sp.start], ""))
		out.WriteString(rendered(s))
		if sp.end < len(d.lines) {
			out.WriteString("\n")
		}
		next = sp.end
	}
	out.WriteString(strings.Join(d.lines[next:], ""))
	if len(added) == 0 {
		return out.Bytes()
	}
	kept := strings.TrimRight(out.String(), "\n \t")
	if kept != "" {
		kept += "\n\n"
	}
	return []byte(kept + strings.Join(added, "\n"))
}
