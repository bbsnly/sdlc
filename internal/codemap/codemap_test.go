package codemap

// US-089: a story's change to the root CODEMAP.md is whole named sections its
// bookkeeper stored before the stop, which `sdlc stop` applies to trunk's map
// as the landing left it. This package is the format: what a stored set is,
// what a section of a map is, and what applying one to the other gives. It
// reads no file, runs no git and knows no story.
//
// The sections are text a model wrote, applied to trunk with no reviewer
// (THREATS T2). So a set is parsed strictly and refused, never repaired; one
// definition of a heading serves the stored set and trunk's file alike; and a
// set is applied whole or not at all.
//
// What these tests hold the package to:
//
//	const AddLine     = "<!-- sdlc:add -->"
//	const ReplaceLine = "<!-- sdlc:replace -->"
//	const MaxBytes    int   // the most bytes a stored set may be
//	const MaxSections int   // the most sections a stored set may hold
//
//	type Section struct {
//		Replace bool   // false adds the section, true replaces the one by this heading
//		Heading string // the heading's text, without "## "
//		Body    string // the lines under it, no blank line first or last, ending "\n"
//	}
//
//	Parse(stored []byte) ([]Section, error)
//	Apply(trunk, cut []byte, sections []Section, replace bool) ([]byte, error)
//
// A stored set is one or more sections. Each begins with a line that is
// exactly AddLine or ReplaceLine, and the line after it is the heading: "## "
// and a text. Everything up to the next such line is the body.
//
// In Apply, trunk is trunk's CODEMAP.md now and cut is the map at the commit
// the story's branch was cut from, which is what the bookkeeper read; nil is
// no file. An added section goes at the end, after one blank line. A replaced
// one takes the place of the section by that heading: its heading line and
// every line up to the next "# " or "## " heading outside a fenced block. With
// replace unset a section that is not what the bookkeeper saw is refused: an
// add of a heading trunk has, a replace of a heading trunk lacks or has twice,
// a replace of a section whose text on trunk is not its text at the cut. With
// replace set, which is a person's `sdlc remap --replace`, the stored section
// goes over whatever trunk holds under that heading. An error names the
// heading it is about.

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// The map every Apply test starts from: a title, a line under it, two
// sections, one blank line between each, one newline at the end.
const cmTrunk = "# CODEMAP\n\nWhat each file is for.\n\n## a.go\n\nKeeps a.\n\n## b.go\n\nKeeps b.\n"

func cmSet(sections ...string) []byte { return []byte(strings.Join(sections, "\n")) }

func cmAdd(heading, body string) string {
	return AddLine + "\n## " + heading + "\n\n" + body
}

func cmReplace(heading, body string) string {
	return ReplaceLine + "\n## " + heading + "\n\n" + body
}

func cmParsed(t *testing.T, stored []byte) []Section {
	t.Helper()
	got, err := Parse(stored)
	if err != nil {
		t.Fatalf("Parse refused a well-formed set: %v\n%s", err, stored)
	}
	return got
}

func cmApplied(t *testing.T, trunk, cut string, sections []Section) string {
	t.Helper()
	got, err := Apply([]byte(trunk), []byte(cut), sections, false)
	if err != nil {
		t.Fatalf("Apply refused sections that are what the bookkeeper saw: %v", err)
	}
	return string(got)
}

// cmRefused holds an apply to being refused whole: an error naming each of
// names, and no map returned for a caller to commit by mistake.
func cmRefused(t *testing.T, got []byte, err error, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("Apply went ahead, and gave:\n%s", got)
	}
	if got != nil {
		t.Errorf("Apply was refused (%v) and still returned a map:\n%s", err, got)
	}
	for _, n := range names {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("the refusal does not name %q: %v", n, err)
		}
	}
}

// ---------------------------------------------------------------- Parse

func TestAStoredSetIsReadAsItsSectionsInOrder(t *testing.T) {
	stored := cmSet(
		cmAdd("internal/cli/remap.go", "Package `cli`. The repair a person runs.\n\nA second paragraph.\n"),
		cmReplace("b.go", "Keeps b, and now c.\n"),
	)
	got := cmParsed(t, stored)
	want := []Section{
		{Replace: false, Heading: "internal/cli/remap.go", Body: "Package `cli`. The repair a person runs.\n\nA second paragraph.\n"},
		{Replace: true, Heading: "b.go", Body: "Keeps b, and now c.\n"},
	}
	if len(got) != len(want) {
		t.Fatalf("Parse gave %d sections, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("section %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}

// The blank lines round a body are the format's, not the section's: however
// many the bookkeeper left, the body is its lines and one newline.
func TestABodyIsItsLinesWithoutTheBlankLinesRoundIt(t *testing.T) {
	for name, stored := range map[string]string{
		"no blank line under the heading": AddLine + "\n## c.go\nKeeps c.\n",
		"several blank lines round it":    "\n\n" + AddLine + "\n## c.go\n\n\n\nKeeps c.\n\n\n\n",
		"no newline at the end":           AddLine + "\n## c.go\n\nKeeps c.",
	} {
		t.Run(name, func(t *testing.T) {
			got := cmParsed(t, []byte(stored))
			if len(got) != 1 || got[0].Heading != "c.go" || got[0].Body != "Keeps c.\n" || got[0].Replace {
				t.Errorf("Parse gave %+v, want one added section c.go with the body %q", got, "Keeps c.\n")
			}
		})
	}
}

// What a section may hold: deeper headings, a fenced block that is closed, and
// blank lines between paragraphs, each kept as written.
func TestABodyKeepsDeeperHeadingsAndClosedFences(t *testing.T) {
	body := "Keeps c.\n\n### How it is called\n\n```go\nc.Run()\n```\n\n~~~\nplain\n~~~\n\n#### Deeper still\n\nLast line.\n"
	got := cmParsed(t, cmSet(cmAdd("c.go", body)))
	if len(got) != 1 || got[0].Body != body {
		t.Errorf("Parse gave %+v, want the body as written:\n%s", got, body)
	}
}

// THREATS T2, the shape of a set: anything that is not a directive line, a
// heading line and a body is refused rather than guessed at.
func TestASetThatIsNotDirectiveHeadingBodyIsRefused(t *testing.T) {
	for name, stored := range map[string]string{
		"nothing":                              "",
		"blank lines only":                     "\n\n  \n",
		"a section with no directive":          "## c.go\n\nKeeps c.\n",
		"prose before the first directive":     "Here is the map change.\n" + cmAdd("c.go", "Keeps c.\n"),
		"a directive at the end":               cmAdd("c.go", "Keeps c.\n") + AddLine + "\n",
		"a directive followed by prose":        AddLine + "\nKeeps c.\n",
		"a directive followed by a blank line": AddLine + "\n\n## c.go\n\nKeeps c.\n",
		"a directive nobody defined":           "<!-- sdlc:remove -->\n## a.go\n\nGone.\n",
		"a directive spelled without a space":  "<!-- sdlc:add-->\n## c.go\n\nKeeps c.\n",
		"a directive in another case":          "<!-- SDLC:ADD -->\n## c.go\n\nKeeps c.\n",
		"a directive with a space after it":    AddLine + " \n## c.go\n\nKeeps c.\n",
		"an undefined directive in a body":     cmAdd("c.go", "Keeps c.\n<!-- sdlc:remove -->\nMore.\n"),
		"a whole map, as the file itself":      cmTrunk,
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := Parse([]byte(stored)); err == nil {
				t.Errorf("Parse took it for %+v", got)
			}
		})
	}
}

// THREATS T2, a heading that is not what it looks like, and a replacement
// addressed to the title or to nothing: a section's heading is "## " and a
// text with nothing round it, and no other spelling is one.
func TestAHeadingThatIsNotExactlyTwoHashesASpaceAndAPlainTextIsRefused(t *testing.T) {
	for name, heading := range map[string]string{
		"a trailing space":           "## c.go ",
		"a trailing tab":             "## c.go\t",
		"a tab for the space":        "##\tc.go",
		"two spaces after the marks": "##  c.go",
		"a closing run of marks":     "## c.go ##",
		"no space after the marks":   "##c.go",
		"the title's level":          "# CODEMAP",
		"a deeper level":             "### c.go",
		"no text":                    "## ",
		"the marks alone":            "##",
		"a non-breaking space":       "## c go",
		"a zero-width space":         "## c\u200b.go",
		"a tab inside":               "## c\t.go",
		"an escape character":        "## c\x1b[2K.go",
		"indented":                   "  ## c.go",
		"underlined instead":         "c.go\n----",
	} {
		t.Run(name, func(t *testing.T) {
			for _, directive := range []string{AddLine, ReplaceLine} {
				stored := directive + "\n" + heading + "\n\nKeeps c.\n"
				if got, err := Parse([]byte(stored)); err == nil {
					t.Errorf("%s: Parse took %q for the heading of %+v", directive, heading, got)
				}
			}
		})
	}
}

// THREATS T2, a heading that escapes its section: a body holding a line that
// reads as a heading at the section's level, or above it, would start a
// section of its own once it is in the file, or close every section after it.
func TestABodyHoldingAHeadingAtOrAboveItsOwnLevelIsRefused(t *testing.T) {
	for name, line := range map[string]string{
		"a section heading":         "## b.go",
		"the title's level":         "# Another map",
		"the marks alone":           "##",
		"one mark alone":            "#",
		"indented by two spaces":    "  ## b.go",
		"indented by three":         "   # Another map",
		"a tab after the marks":     "##\tb.go",
		"a closing run of marks":    "## b.go ##",
		"an underline of equals":    "=====",
		"an underline of dashes":    "-----",
		"a heading trunk also has":  "## a.go",
		"the section's own heading": "## c.go",
	} {
		t.Run(name, func(t *testing.T) {
			body := "Keeps c.\n" + line + "\nMore of c.\n"
			got, err := Parse(cmSet(cmAdd("c.go", body)))
			if err == nil {
				t.Fatalf("Parse took a body holding %q: %+v", line, got)
			}
			if !strings.Contains(err.Error(), "c.go") {
				t.Errorf("the refusal does not name the section, c.go: %v", err)
			}
		})
	}
}

// THREATS T2, fences: a fence left open in a body swallows every section after
// it when the map is rendered, so a body's fences are closed or it is refused.
func TestABodyWithAFenceLeftOpenIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"backticks":                    "Keeps c.\n\n```go\nc.Run()\n",
		"tildes":                       "Keeps c.\n\n~~~\nplain\n",
		"closed by the other kind":     "Keeps c.\n\n```\nplain\n~~~\n",
		"a second one left open":       "```\none\n```\n\n```\ntwo\n",
		"closed by a shorter fence":    "````\nplain\n```\n",
		"only the opening, at the end": "Keeps c.\n```\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse(cmSet(cmAdd("c.go", body)))
			if err == nil {
				t.Fatalf("Parse took a body whose fence is never closed: %+v", got)
			}
			if !strings.Contains(err.Error(), "c.go") {
				t.Errorf("the refusal does not name the section, c.go: %v", err)
			}
		})
	}
}

// THREATS T2: AC-2 allows no conflict marker on trunk, and nothing in applying
// a section makes one, so the only source is the input.
func TestABodyHoldingAConflictMarkerIsRefused(t *testing.T) {
	for name, line := range map[string]string{
		"ours":            "<<<<<<< HEAD",
		"ours, bare":      "<<<<<<<",
		"the divider":     "=======",
		"theirs":          ">>>>>>> story/US-002",
		"the common base": "||||||| merged common ancestors",
	} {
		t.Run(name, func(t *testing.T) {
			body := "Keeps c.\n" + line + "\nKeeps d.\n"
			if got, err := Parse(cmSet(cmAdd("c.go", body))); err == nil {
				t.Errorf("Parse took a body holding %q: %+v", line, got)
			}
		})
	}
}

// THREATS T2, removal by replacement: the answers allow adding a section and
// replacing one. A section with nothing under its heading is a removal by
// another name.
func TestASectionWithNothingUnderItsHeadingIsRefused(t *testing.T) {
	for name, stored := range map[string]string{
		"a replacement with no body":     ReplaceLine + "\n## a.go\n",
		"a replacement with blank lines": ReplaceLine + "\n## a.go\n\n\n   \n\n",
		"an addition with no body":       AddLine + "\n## c.go\n",
		"an empty one before a full one": ReplaceLine + "\n## a.go\n\n" + cmAdd("c.go", "Keeps c.\n"),
		"an empty one after a full one":  cmAdd("c.go", "Keeps c.\n") + ReplaceLine + "\n## a.go\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse([]byte(stored))
			if err == nil {
				t.Fatalf("Parse took a section with nothing in it: %+v", got)
			}
			if !strings.Contains(err.Error(), "a.go") && !strings.Contains(err.Error(), "c.go") {
				t.Errorf("the refusal names no section: %v", err)
			}
		})
	}
}

// THREATS T2: one heading named twice in a set leaves which one wins to the
// order they are read in.
func TestAHeadingNamedTwiceInOneSetIsRefused(t *testing.T) {
	for name, stored := range map[string][]byte{
		"added twice":          cmSet(cmAdd("c.go", "One.\n"), cmAdd("c.go", "Two.\n")),
		"added and replaced":   cmSet(cmAdd("a.go", "One.\n"), cmReplace("a.go", "Two.\n")),
		"replaced twice":       cmSet(cmReplace("a.go", "One.\n"), cmReplace("a.go", "Two.\n")),
		"twice, another case":  cmSet(cmAdd("c.go", "One.\n"), cmAdd("C.GO", "Two.\n")),
		"twice, with a third":  cmSet(cmAdd("c.go", "One.\n"), cmAdd("d.go", "D.\n"), cmReplace("c.go", "Two.\n")),
		"the same body, twice": cmSet(cmAdd("c.go", "One.\n"), cmAdd("c.go", "One.\n")),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Parse(stored)
			if err == nil {
				t.Fatalf("Parse took a set naming one heading twice: %+v", got)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "c.go") && !strings.Contains(err.Error(), "a.go") {
				t.Errorf("the refusal does not name the heading: %v", err)
			}
		})
	}
}

// THREATS T2, bytes that are not text: the commit is made from the bytes as
// they are, past every filter a checkout would have run them through, so what
// is not plain UTF-8 text with line feeds is refused here.
func TestASetThatIsNotPlainTextWithLineFeedsIsRefused(t *testing.T) {
	good := string(cmSet(cmAdd("c.go", "Keeps c.\n")))
	for name, stored := range map[string]string{
		"a NUL in a body":            strings.Replace(good, "Keeps", "Ke\x00eps", 1),
		"a NUL in a heading":         strings.Replace(good, "c.go", "c\x00.go", 1),
		"invalid UTF-8 in a body":    strings.Replace(good, "Keeps", "Ke\xffeps", 1),
		"invalid UTF-8 in a heading": strings.Replace(good, "c.go", "c\xc3.go", 1),
		"a byte-order mark":          "\xef\xbb\xbf" + good,
		"CRLF line endings":          strings.ReplaceAll(good, "\n", "\r\n"),
		"one CRLF, in the body":      strings.Replace(good, "Keeps c.\n", "Keeps c.\r\n", 1),
		"a carriage return alone":    strings.Replace(good, "Keeps c.", "Keeps\rc.", 1),
		"a CR after the heading":     strings.Replace(good, "## c.go\n", "## c.go\r\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := Parse([]byte(stored)); err == nil {
				t.Errorf("Parse took it: %+v", got)
			}
		})
	}
}

// THREATS T2, a huge section or very many: the stop applies the set while it
// holds the landing lock, and every later reader of the map pays for it.
func TestASetOverTheBoundsIsRefused(t *testing.T) {
	if MaxBytes <= 0 || MaxBytes > 1<<20 {
		t.Fatalf("MaxBytes is %d, want a bound above nothing and no more than the 1 MiB a gate's document may be", MaxBytes)
	}
	if MaxSections <= 0 || MaxSections > 1024 {
		t.Fatalf("MaxSections is %d, want a bound a person could read through", MaxSections)
	}
	line := "Keeps c, at length.\n"
	t.Run("one section over the bytes", func(t *testing.T) {
		body := strings.Repeat(line, MaxBytes/len(line)+1)
		if got, err := Parse(cmSet(cmAdd("c.go", body))); err == nil {
			t.Errorf("Parse took a set of %d bytes, over the %d allowed (%d sections)", len(body), MaxBytes, len(got))
		}
	})
	t.Run("a set of exactly the bytes allowed is taken", func(t *testing.T) {
		head := AddLine + "\n## c.go\n\n"
		body := strings.Repeat("x", MaxBytes-len(head)-1) + "\n"
		stored := []byte(head + body)
		if len(stored) != MaxBytes {
			t.Fatalf("the test built %d bytes, want %d", len(stored), MaxBytes)
		}
		if _, err := Parse(stored); err != nil {
			t.Errorf("Parse refused a set of exactly MaxBytes: %v", err)
		}
	})
	sections := func(n int) []byte {
		var all []string
		for i := range n {
			all = append(all, cmAdd("file"+strconv.Itoa(i)+".go", "Keeps it.\n"))
		}
		return cmSet(all...)
	}
	t.Run("one section too many", func(t *testing.T) {
		if got, err := Parse(sections(MaxSections + 1)); err == nil {
			t.Errorf("Parse took %d sections, over the %d allowed", len(got), MaxSections)
		}
	})
	t.Run("exactly the sections allowed are taken", func(t *testing.T) {
		got, err := Parse(sections(MaxSections))
		if err != nil || len(got) != MaxSections {
			t.Errorf("Parse gave %d sections and %v, want all %d", len(got), err, MaxSections)
		}
	})
}

// THREATS T2: a body is text and nothing else. One that looks like a path, an
// option or a command is kept as the text it is.
func TestABodyThatLooksLikeAPathOrACommandIsOnlyText(t *testing.T) {
	body := "--upload-pack=touch /tmp/x\n../../.git/config\n$(sdlc remap US-001 --replace)\n`rm -rf .`\n-- CODEMAP.md other.go\n"
	got := cmParsed(t, cmSet(cmAdd("c.go", body)))
	if len(got) != 1 || got[0].Body != body {
		t.Fatalf("Parse gave %+v, want the body as written", got)
	}
	out := cmApplied(t, cmTrunk, cmTrunk, got)
	if want := cmTrunk + "\n## c.go\n\n" + body; out != want {
		t.Errorf("Apply gave\n%s\nwant\n%s", out, want)
	}
}

// ---------------------------------------------------------------- Apply, adding

// AC-2 and AC-3: an added section goes at the end of trunk's map as it is,
// after one blank line, and nothing else in the file moves.
func TestAC2AnAddedSectionGoesAtTheEndAfterOneBlankLine(t *testing.T) {
	got := cmApplied(t, cmTrunk, cmTrunk, cmParsed(t, cmSet(cmAdd("c.go", "Keeps c.\n"))))
	if want := cmTrunk + "\n## c.go\n\nKeeps c.\n"; got != want {
		t.Errorf("Apply gave\n%q\nwant\n%q", got, want)
	}
}

func TestAC2SeveralAddedSectionsGoInTheOrderTheyWereStored(t *testing.T) {
	sections := cmParsed(t, cmSet(cmAdd("d.go", "Keeps d.\n"), cmAdd("c.go", "Keeps c.\n\nIn two paragraphs.\n")))
	got := cmApplied(t, cmTrunk, cmTrunk, sections)
	if want := cmTrunk + "\n## d.go\n\nKeeps d.\n\n## c.go\n\nKeeps c.\n\nIn two paragraphs.\n"; got != want {
		t.Errorf("Apply gave\n%q\nwant\n%q", got, want)
	}
}

// AC-1, AC-2 and AC-3 as the format meets them: three stories cut from one
// trunk commit each add a section at the same place, the end of the file. Each
// is applied to the map the one before it left, never to the map it was cut
// from, and the result holds every line of each once, with the blank line
// between sections that a union merge loses.
func TestAC1AC2AC3ThreeStoriesAddingAtTheSamePlaceEachApplyToWhatTheLastOneLeft(t *testing.T) {
	cut := cmTrunk
	trunk := cut
	for _, id := range []string{"us001", "us002", "us003"} {
		trunk = cmApplied(t, trunk, cut, cmParsed(t, cmSet(cmAdd(id+".go", "Keeps "+id+".\n"))))
	}
	want := cmTrunk + "\n## us001.go\n\nKeeps us001.\n\n## us002.go\n\nKeeps us002.\n\n## us003.go\n\nKeeps us003.\n"
	if trunk != want {
		t.Fatalf("after the three, the map is\n%q\nwant\n%q", trunk, want)
	}
	for _, line := range []string{"## us001.go", "Keeps us001.", "## us002.go", "Keeps us002.", "## us003.go", "Keeps us003.", "## a.go", "Keeps a.", "## b.go", "Keeps b."} {
		if n := strings.Count(trunk, line+"\n"); n != 1 {
			t.Errorf("the map holds %q %d times, want once", line, n)
		}
	}
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>"} {
		if strings.Contains(trunk, marker) {
			t.Errorf("the map holds a conflict marker, %s", marker)
		}
	}
}

// A map a person wrote need not end as this one does. The added section still
// goes after one blank line, and the file still ends with one newline.
func TestAnAddedSectionGoesAfterOneBlankLineHoweverTrunksMapEnds(t *testing.T) {
	stem := strings.TrimRight(cmTrunk, "\n")
	for name, trunk := range map[string]string{
		"no newline at the end":   stem,
		"blank lines at the end":  stem + "\n\n\n\n",
		"a title and nothing":     "# CODEMAP\n",
		"no heading at all":       "Notes on the code, in no sections.\n",
		"a file that is empty":    "",
		"only blank lines":        "\n\n",
		"spaces on the last line": stem + "\n   \n",
	} {
		t.Run(name, func(t *testing.T) {
			got := cmApplied(t, trunk, trunk, cmParsed(t, cmSet(cmAdd("c.go", "Keeps c.\n"))))
			const section = "## c.go\n\nKeeps c.\n"
			kept := strings.TrimRight(trunk, "\n ")
			switch {
			case kept == "" && got != section:
				t.Errorf("Apply to a map with nothing in it gave %q, want the section alone", got)
			case kept != "" && got != kept+"\n\n"+section:
				t.Errorf("Apply gave\n%q\nwant\n%q", got, kept+"\n\n"+section)
			}
		})
	}
}

// The fourth answer: in a project with no CODEMAP.md the step may create it.
// What it creates holds the sections whole and once, and reads back as a map
// with those sections in it.
func TestAddingToNoMapAtAllMakesOneThatHoldsTheSections(t *testing.T) {
	sections := cmParsed(t, cmSet(cmAdd("c.go", "Keeps c.\n"), cmAdd("d.go", "Keeps d.\n")))
	got, err := Apply(nil, nil, sections, false)
	if err != nil {
		t.Fatalf("Apply refused to make a map where there was none: %v", err)
	}
	text := string(got)
	if !strings.HasSuffix(text, "## c.go\n\nKeeps c.\n\n## d.go\n\nKeeps d.\n") {
		t.Errorf("the new map does not end with the two sections as stored:\n%q", text)
	}
	for _, line := range []string{"## c.go", "Keeps c.", "## d.go", "Keeps d."} {
		if n := strings.Count(text, line+"\n"); n != 1 {
			t.Errorf("the new map holds %q %d times, want once", line, n)
		}
	}
	// The sections are there for the next story to replace.
	next, err := Apply(got, got, cmParsed(t, cmSet(cmReplace("c.go", "Keeps c, and more.\n"))), false)
	if err != nil || !strings.Contains(string(next), "Keeps c, and more.\n") || strings.Contains(string(next), "Keeps c.\n") {
		t.Errorf("a replacement of a section of the map just made gave %q, %v", next, err)
	}
}

// ---------------------------------------------------------------- Apply, replacing

func TestAC2AReplacedSectionTakesThePlaceOfTheOneByItsHeadingAndNothingElseMoves(t *testing.T) {
	for _, tc := range []struct{ name, heading, body, want string }{
		{"a section in the middle", "a.go", "Keeps a, and more.\n\nIn two paragraphs.\n",
			"# CODEMAP\n\nWhat each file is for.\n\n## a.go\n\nKeeps a, and more.\n\nIn two paragraphs.\n\n## b.go\n\nKeeps b.\n"},
		{"the last section", "b.go", "Keeps b, and more.\n",
			"# CODEMAP\n\nWhat each file is for.\n\n## a.go\n\nKeeps a.\n\n## b.go\n\nKeeps b, and more.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := cmApplied(t, cmTrunk, cmTrunk, cmParsed(t, cmSet(cmReplace(tc.heading, tc.body))))
			if got != tc.want {
				t.Errorf("Apply gave\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// A section runs to the next heading at its level or above. Deeper headings
// and a fenced block are inside it, a "## " line in the fence included, and go
// with it when it is replaced; the section after it is untouched.
func TestAReplacedSectionGoesWithItsDeeperHeadingsAndItsFences(t *testing.T) {
	trunk := "# CODEMAP\n\n## a.go\n\nKeeps a.\n\n### Called by\n\n```md\n## not a section\n# nor a title\n```\n\nLast of a.\n\n## b.go\n\nKeeps b.\n"
	got := cmApplied(t, trunk, trunk, cmParsed(t, cmSet(cmReplace("a.go", "Keeps a, rewritten.\n"))))
	if want := "# CODEMAP\n\n## a.go\n\nKeeps a, rewritten.\n\n## b.go\n\nKeeps b.\n"; got != want {
		t.Errorf("Apply gave\n%q\nwant\n%q", got, want)
	}
}

// A title after the sections closes the one before it, as a "# " heading does
// in the file a person reads.
func TestASectionEndsAtAHeadingAboveItsLevel(t *testing.T) {
	trunk := "# CODEMAP\n\n## a.go\n\nKeeps a.\n\n# Appendix\n\nKept by hand.\n"
	got := cmApplied(t, trunk, trunk, cmParsed(t, cmSet(cmReplace("a.go", "Keeps a, rewritten.\n"))))
	if want := "# CODEMAP\n\n## a.go\n\nKeeps a, rewritten.\n\n# Appendix\n\nKept by hand.\n"; got != want {
		t.Errorf("Apply gave\n%q\nwant\n%q", got, want)
	}
}

func TestAnAddAndAReplaceInOneSetAreBothApplied(t *testing.T) {
	sections := cmParsed(t, cmSet(cmAdd("c.go", "Keeps c.\n"), cmReplace("a.go", "Keeps a, and calls c.\n")))
	got := cmApplied(t, cmTrunk, cmTrunk, sections)
	want := "# CODEMAP\n\nWhat each file is for.\n\n## a.go\n\nKeeps a, and calls c.\n\n## b.go\n\nKeeps b.\n\n## c.go\n\nKeeps c.\n"
	if got != want {
		t.Errorf("Apply gave\n%q\nwant\n%q", got, want)
	}
}

// AC-3: a replacement is made against trunk's map as it is now. What another
// story added or changed elsewhere since the cut stays.
func TestAC3AReplacementKeepsWhatTrunkGainedElsewhereSinceTheCut(t *testing.T) {
	now := strings.Replace(cmTrunk, "Keeps b.\n", "Keeps b, as another story left it.\n", 1) + "\n## z.go\n\nAnother story's.\n"
	got := cmApplied(t, now, cmTrunk, cmParsed(t, cmSet(cmReplace("a.go", "Keeps a, rewritten.\n"), cmAdd("c.go", "Keeps c.\n"))))
	want := "# CODEMAP\n\nWhat each file is for.\n\n## a.go\n\nKeeps a, rewritten.\n\n## b.go\n\nKeeps b, as another story left it.\n\n## z.go\n\nAnother story's.\n\n## c.go\n\nKeeps c.\n"
	if got != want {
		t.Errorf("Apply gave\n%q\nwant\n%q", got, want)
	}
}

// ---------------------------------------------------------------- Apply, refused

// The first and sixth answers: a section that is not what the bookkeeper saw
// is refused by name, and the set with it. Each of these is one.
func TestASectionThatIsNotWhatTheBookkeeperSawIsRefusedByName(t *testing.T) {
	changed := strings.Replace(cmTrunk, "Keeps a.\n", "Keeps a, as another story left it.\n", 1)
	twice := cmTrunk + "\n## a.go\n\nA second a.\n"
	fenced := "# CODEMAP\n\n## a.go\n\nKeeps a.\n\n```md\n## c.go\n```\n"
	for _, tc := range []struct {
		name       string
		trunk, cut string
		section    string
		names      string
	}{
		{"an add of a heading trunk has", cmTrunk, cmTrunk, cmAdd("a.go", "Another a.\n"), "a.go"},
		{"an add of a heading trunk gained since the cut", cmTrunk + "\n## c.go\n\nAnother story's c.\n", cmTrunk, cmAdd("c.go", "Keeps c.\n"), "c.go"},
		{"an add of exactly what trunk gained", cmTrunk + "\n## c.go\n\nKeeps c.\n", cmTrunk, cmAdd("c.go", "Keeps c.\n"), "c.go"},
		{"a replace of a heading trunk lacks", cmTrunk, cmTrunk, cmReplace("c.go", "Keeps c.\n"), "c.go"},
		{"a replace of a section another story replaced", changed, cmTrunk, cmReplace("a.go", "Keeps a, rewritten.\n"), "a.go"},
		{"a replace of a section added after the cut", cmTrunk + "\n## c.go\n\nAnother story's c.\n", cmTrunk, cmReplace("c.go", "Keeps c.\n"), "c.go"},
		{"a replace of a section trunk no longer has", "# CODEMAP\n\n## b.go\n\nKeeps b.\n", cmTrunk, cmReplace("a.go", "Keeps a, rewritten.\n"), "a.go"},
		{"a replace of a heading trunk has twice", twice, twice, cmReplace("a.go", "Keeps a, rewritten.\n"), "a.go"},
		{"an add of a heading trunk has twice", twice, twice, cmAdd("a.go", "A third a.\n"), "a.go"},
		{"a replace of a heading that is only inside a fence", fenced, fenced, cmReplace("c.go", "Keeps c.\n"), "c.go"},
		{"a replace where there is no map", "", "", cmReplace("a.go", "Keeps a.\n"), "a.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Apply([]byte(tc.trunk), []byte(tc.cut), cmParsed(t, cmSet(tc.section)), false)
			cmRefused(t, got, err, tc.names)
		})
	}
}

// THREATS T2, look-alike headings: two headings a person reads as the same are
// the same section to the matcher too, so an add of one beside the other is
// refused rather than made a duplicate.
func TestAnAddOfAHeadingThatOnlyLooksNewIsRefused(t *testing.T) {
	for name, onTrunk := range map[string]string{
		"another case":               "## A.GO",
		"a trailing space":           "## a.go ",
		"trailing spaces and tabs":   "## a.go \t ",
		"a closing run of marks":     "## a.go ##",
		"two spaces after the marks": "##  a.go",
		"a tab after the marks":      "##\ta.go",
	} {
		t.Run(name, func(t *testing.T) {
			trunk := "# CODEMAP\n\n" + onTrunk + "\n\nKeeps a.\n\n## b.go\n\nKeeps b.\n"
			got, err := Apply([]byte(trunk), []byte(trunk), cmParsed(t, cmSet(cmAdd("a.go", "Another a.\n"))), false)
			cmRefused(t, got, err, "a.go")
		})
	}
}

// A "## " line inside a fenced block on trunk is not a section, so it does not
// stand in the way of a section by that name, and it is not cut in two.
func TestAHeadingInsideAFenceOnTrunkIsNotASection(t *testing.T) {
	trunk := "# CODEMAP\n\n## a.go\n\nKeeps a.\n\n```md\n## c.go\n\nAn example.\n```\n\nLast of a.\n"
	got := cmApplied(t, trunk, trunk, cmParsed(t, cmSet(cmAdd("c.go", "Keeps c.\n"))))
	if want := trunk + "\n## c.go\n\nKeeps c.\n"; got != want {
		t.Errorf("Apply gave\n%q\nwant\n%q", got, want)
	}
}

// A story's sections are applied whole or not at all: one that no longer
// applies refuses the set, wherever in the set it is.
func TestOneSectionThatDoesNotApplyRefusesTheWholeSet(t *testing.T) {
	changed := strings.Replace(cmTrunk, "Keeps a.\n", "Keeps a, as another story left it.\n", 1)
	for name, set := range map[string][]byte{
		"the refused one last":    cmSet(cmAdd("c.go", "Keeps c.\n"), cmReplace("a.go", "Keeps a, rewritten.\n")),
		"the refused one first":   cmSet(cmReplace("a.go", "Keeps a, rewritten.\n"), cmAdd("c.go", "Keeps c.\n")),
		"the refused one between": cmSet(cmAdd("c.go", "Keeps c.\n"), cmReplace("a.go", "Keeps a, rewritten.\n"), cmReplace("b.go", "Keeps b, rewritten.\n")),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Apply([]byte(changed), []byte(cmTrunk), cmParsed(t, set), false)
			cmRefused(t, got, err, "a.go")
			if err != nil && strings.Contains(err.Error(), "c.go") {
				t.Errorf("the refusal names c.go, which applies: %v", err)
			}
		})
	}
}

func TestApplyingNoSectionsIsRefused(t *testing.T) {
	got, err := Apply([]byte(cmTrunk), []byte(cmTrunk), nil, false)
	cmRefused(t, got, err)
}

// What reaches Apply is read as untrusted again: a caller that built a Section
// by hand, or read one from a file changed since it was checked, gets the
// refusal Parse would have given.
func TestApplyRefusesASectionParseWouldHaveRefused(t *testing.T) {
	for name, s := range map[string]Section{
		"a body holding a section heading": {Heading: "c.go", Body: "Keeps c.\n## b.go\n\nGone.\n"},
		"a body holding a title":           {Heading: "c.go", Body: "# Another map\n"},
		"an empty body":                    {Replace: true, Heading: "a.go", Body: ""},
		"a blank body":                     {Replace: true, Heading: "a.go", Body: "\n\n"},
		"a heading with a newline in it":   {Heading: "c.go\n## b.go", Body: "Keeps c.\n"},
		"a heading with a space after it":  {Heading: "c.go ", Body: "Keeps c.\n"},
		"no heading":                       {Heading: "", Body: "Keeps nothing.\n"},
		"a conflict marker":                {Heading: "c.go", Body: "<<<<<<< HEAD\nKeeps c.\n"},
		"a fence left open":                {Heading: "c.go", Body: "```\nKeeps c.\n"},
		"a carriage return":                {Heading: "c.go", Body: "Keeps c.\r\n"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Apply([]byte(cmTrunk), []byte(cmTrunk), []Section{s}, false)
			if err == nil {
				t.Fatalf("Apply took %+v and gave:\n%s", s, got)
			}
			if got != nil {
				t.Errorf("Apply was refused and still returned a map:\n%s", got)
			}
		})
	}
}

// ---------------------------------------------------------------- Apply, a person's --replace

// The sixth answer: with replace set the stored section goes over trunk's,
// whatever trunk holds under that heading now. What trunk held there is gone
// from the file; every other section is as it was.
func TestWithReplaceSetTheStoredSectionGoesOverTrunks(t *testing.T) {
	changed := strings.Replace(cmTrunk, "Keeps a.\n", "Keeps a, as another story left it.\n", 1)
	for _, tc := range []struct {
		name, trunk, section, want string
	}{
		{"a replace of a section another story replaced", changed, cmReplace("a.go", "Keeps a, rewritten.\n"),
			"# CODEMAP\n\nWhat each file is for.\n\n## a.go\n\nKeeps a, rewritten.\n\n## b.go\n\nKeeps b.\n"},
		{"an add of a heading trunk gained since the cut", cmTrunk + "\n## c.go\n\nAnother story's c.\n", cmAdd("c.go", "Keeps c.\n"),
			cmTrunk + "\n## c.go\n\nKeeps c.\n"},
		{"a replace of a section added after the cut", cmTrunk + "\n## c.go\n\nAnother story's c.\n", cmReplace("c.go", "Keeps c.\n"),
			cmTrunk + "\n## c.go\n\nKeeps c.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sections := cmParsed(t, cmSet(tc.section))
			if got, err := Apply([]byte(tc.trunk), []byte(cmTrunk), sections, false); err == nil {
				t.Fatalf("precondition: without replace it was applied:\n%s", got)
			}
			got, err := Apply([]byte(tc.trunk), []byte(cmTrunk), sections, true)
			if err != nil {
				t.Fatalf("Apply with replace set was refused: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("Apply gave\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// With replace set, sections that still apply are applied as they are without
// it: the flag changes what happens to a section trunk changed, nothing else.
func TestWithReplaceSetSectionsThatStillApplyAreAppliedAsWithoutIt(t *testing.T) {
	sections := cmParsed(t, cmSet(cmAdd("c.go", "Keeps c.\n"), cmReplace("a.go", "Keeps a, rewritten.\n")))
	plain, err := Apply([]byte(cmTrunk), []byte(cmTrunk), sections, false)
	if err != nil {
		t.Fatal(err)
	}
	forced, err := Apply([]byte(cmTrunk), []byte(cmTrunk), sections, true)
	if err != nil {
		t.Fatalf("Apply with replace set was refused: %v", err)
	}
	if !bytes.Equal(plain, forced) {
		t.Errorf("with replace set Apply gave\n%q\nwithout it\n%q", forced, plain)
	}
}

// Replace is a choice between two texts of one section. Where trunk holds the
// heading twice there is no one section to put it over, and a malformed
// section is malformed whoever asks.
func TestWithReplaceSetWhatCannotBePutOverOneSectionIsStillRefused(t *testing.T) {
	twice := cmTrunk + "\n## a.go\n\nA second a.\n"
	got, err := Apply([]byte(twice), []byte(cmTrunk), cmParsed(t, cmSet(cmReplace("a.go", "Keeps a, rewritten.\n"))), true)
	cmRefused(t, got, err, "a.go")

	got, err = Apply([]byte(cmTrunk), []byte(cmTrunk), []Section{{Replace: true, Heading: "a.go", Body: "## b.go\n"}}, true)
	cmRefused(t, got, err)
}

// AC-2 as a property of every map Apply returns here: no conflict marker, one
// newline at the end, and no heading at the section's level twice.
func TestAC2NoMapApplyReturnsHoldsAMarkerOrAHeadingTwice(t *testing.T) {
	changed := strings.Replace(cmTrunk, "Keeps a.\n", "Keeps a, as another story left it.\n", 1)
	for name, tc := range map[string]struct {
		trunk   string
		set     []byte
		replace bool
	}{
		"an add":                 {cmTrunk, cmSet(cmAdd("c.go", "Keeps c.\n")), false},
		"a replace":              {cmTrunk, cmSet(cmReplace("a.go", "Keeps a, rewritten.\n")), false},
		"both":                   {cmTrunk, cmSet(cmAdd("c.go", "Keeps c.\n"), cmReplace("b.go", "Keeps b, rewritten.\n")), false},
		"a person's replace":     {changed, cmSet(cmReplace("a.go", "Keeps a, rewritten.\n")), true},
		"a person's replace-add": {cmTrunk + "\n## c.go\n\nAnother story's c.\n", cmSet(cmAdd("c.go", "Keeps c.\n")), true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Apply([]byte(tc.trunk), []byte(cmTrunk), cmParsed(t, tc.set), tc.replace)
			if err != nil {
				t.Fatal(err)
			}
			text := string(got)
			if !strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\n\n") {
				t.Errorf("the map does not end with exactly one newline:\n%q", text)
			}
			seen := map[string]bool{}
			for line := range strings.SplitSeq(text, "\n") {
				for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>"} {
					if strings.HasPrefix(line, marker) {
						t.Errorf("the map holds a conflict marker: %q", line)
					}
				}
				if strings.HasPrefix(line, "## ") {
					if seen[line] {
						t.Errorf("the map holds %q twice", line)
					}
					seen[line] = true
				}
			}
			if strings.Contains(text, "\n\n\n") {
				t.Errorf("the map holds two blank lines together, where every section was one apart:\n%q", text)
			}
		})
	}
}
