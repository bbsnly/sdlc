package scaffold

// US-002: the configuration `sdlc init` writes carries a comment block above
// every setting. The blocks are top-level `_<setting>` keys, so what these
// tests have to see is the file's text: which key comes directly before which,
// and whether a key is written twice. json.Unmarshal into a map shows neither
// (the last of two equal keys wins, and the order is gone), so every check here
// reads the tokens in document order.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/bbsnly/sdlc/internal/config"
)

// ccSettings is the twelve top-level settings the story names, in the order the
// template writes them.
var ccSettings = []string{
	"version", "backlog", "git", "commands", "thresholds", "paths",
	"spec", "loop", "reviews", "freeze", "human_gates", "budget",
}

// ccDocURL is what the last line of every block starts with.
const ccDocURL = "https://github.com/bbsnly/sdlc/blob/main/docs/configuration.md#"

// ccDoc is `_doc` as the template wrote it before the story. It stays as it was
// (ANALYSIS.md section 1a, answer 2).
var ccDoc = []string{
	"This is the machine-readable half of your project's contract with the loop.",
	"The other half is the '## SDLC Contract' section of your CLAUDE.md: rules a",
	"script cannot check, written for the assistant to read.",
	"Nothing here starts the loop: a person does, by typing /sdlc:next in Claude Code.",
	"",
	"Commands run from the repository root. Leave one empty to skip that step.",
	"The coverage and mutation commands must print their percentage as the last",
	"number on stdout.",
	"",
	"Run 'sdlc doctor' after changing anything here.",
	"Reference: https://github.com/bbsnly/sdlc/blob/main/docs/configuration.md",
}

// ccStacks is every stack init detects, and none. The marker files are the
// smallest ones Detect accepts.
var ccStacks = []struct {
	name  string
	files map[string]string
}{
	{"none", nil},
	{"Go", map[string]string{"go.mod": "module example.com/x\n"}},
	{"Rust", map[string]string{"Cargo.toml": ""}},
	{"Node", map[string]string{"package.json": `{"scripts":{"build":"tsc","test":"vitest run","lint":"eslint ."}}`}},
	{"Python", map[string]string{"pyproject.toml": ""}},
}

// ccInit is `init` in a fresh directory holding files, and the file it wrote.
func ccInit(t *testing.T, files map[string]string) (root string, raw []byte) {
	t.Helper()
	root = t.TempDir()
	for rel, body := range files {
		touch(t, root, rel, body)
	}
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	return root, []byte(read(t, root, config.File))
}

// ccMember is one top-level key and its value as written.
type ccMember struct {
	key   string
	value json.RawMessage
}

// ccMembers is the top-level members of a JSON object, in the order written and
// with a repeated key repeated.
func ccMembers(raw []byte) ([]ccMember, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("not a JSON object: %w", err)
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("not a JSON object: it starts with %v", tok)
	}
	var out []ccMember
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected token %v where a key should be", tok)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		out = append(out, ccMember{key, value})
	}
	return out, nil
}

// ccWalk calls visit for every key of every object in raw, in document order.
// path is the dotted path of the object the key is in ("" for the top level,
// "[]" standing for an array on the way), and repeated is whether the same
// object has had that key already.
func ccWalk(raw []byte, visit func(path, key string, repeated bool)) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	return ccWalkValue(dec, "", visit)
}

func ccWalkValue(dec *json.Decoder, path string, visit func(path, key string, repeated bool)) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := tok.(string)
			if !ok {
				return fmt.Errorf("unexpected token %v where a key should be", tok)
			}
			visit(path, key, seen[key])
			seen[key] = true
			child := key
			if path != "" {
				child = path + "." + key
			}
			if err := ccWalkValue(dec, child, visit); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := ccWalkValue(dec, path+"[]", visit); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token() // the closing } or ]
	return err
}

// ccRepeatedKeys is the dotted path of every key written twice in one object,
// at any depth.
func ccRepeatedKeys(raw []byte) ([]string, error) {
	var out []string
	err := ccWalk(raw, func(path, key string, repeated bool) {
		if repeated {
			out = append(out, strings.TrimPrefix(path+"."+key, "."))
		}
	})
	return out, err
}

// ccNestedCommentKeys is every key starting with _ that is not at the top
// level. A list under one inside `commands` stops the configuration loading.
func ccNestedCommentKeys(raw []byte) ([]string, error) {
	var out []string
	err := ccWalk(raw, func(path, key string, _ bool) {
		if path != "" && strings.HasPrefix(key, "_") {
			out = append(out, path+"."+key)
		}
	})
	return out, err
}

// ccIsListOfStrings is whether value is a JSON array whose every element is a
// string. null, a bare string and an array holding a number are not.
func ccIsListOfStrings(value json.RawMessage) bool {
	if !bytes.HasPrefix(bytes.TrimSpace(value), []byte("[")) {
		return false
	}
	var elements []any
	if json.Unmarshal(value, &elements) != nil {
		return false
	}
	for _, e := range elements {
		if _, ok := e.(string); !ok {
			return false
		}
	}
	return true
}

// ccBlockOf is the lines of the comment block of the setting name, taken from
// the key `_name` wherever it is, and whether there is one.
func ccBlockOf(members []ccMember, name string) ([]string, bool) {
	for _, m := range members {
		if m.key != "_"+name {
			continue
		}
		var lines []string
		if json.Unmarshal(m.value, &lines) != nil {
			return nil, false
		}
		return lines, true
	}
	return nil, false
}

// ccProblem is one top-level setting that has no comment block directly before
// it, with a message that names it.
type ccProblem struct {
	setting string
	message string
}

// ccMissingBlocks is every top-level setting in raw (a key that does not start
// with _) that does not have a key `_<setting>` directly before it whose value
// is a list of strings. A block that is somewhere else in the file does not
// count: the point of the story is that the reader finds it above the setting.
func ccMissingBlocks(raw []byte) ([]ccProblem, error) {
	members, err := ccMembers(raw)
	if err != nil {
		return nil, err
	}
	var out []ccProblem
	for i, m := range members {
		if strings.HasPrefix(m.key, "_") {
			continue
		}
		want := "_" + m.key
		switch {
		case i == 0 || members[i-1].key != want:
			before := "nothing"
			if i > 0 {
				before = strconv.Quote(members[i-1].key)
			}
			out = append(out, ccProblem{m.key, fmt.Sprintf(
				"setting %q has no comment block: the key directly before it should be %q, and it is %s",
				m.key, want, before)})
		case !ccIsListOfStrings(members[i-1].value):
			out = append(out, ccProblem{m.key, fmt.Sprintf(
				"setting %q has a key %q before it, but its value is not a list of strings: %s",
				m.key, want, members[i-1].value)})
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------- AC-1

// AC-1: for every stack init detects and for none, each of the twelve settings
// has `_<setting>` directly before it, holding a list of strings.
func TestAC1EverySettingHasAListOfStringsKeyDirectlyBeforeItForEveryStackAndNone(t *testing.T) {
	for _, stack := range ccStacks {
		t.Run(stack.name, func(t *testing.T) {
			_, raw := ccInit(t, stack.files)
			members, err := ccMembers(raw)
			if err != nil {
				t.Fatal(err)
			}

			var settings []string
			for _, m := range members {
				if !strings.HasPrefix(m.key, "_") {
					settings = append(settings, m.key)
				}
			}
			if !slices.Equal(settings, ccSettings) {
				t.Errorf("the top-level settings are %v, want exactly %v in that order", settings, ccSettings)
			}

			for _, name := range ccSettings {
				at := slices.IndexFunc(members, func(m ccMember) bool { return m.key == name })
				switch {
				case at < 0:
					t.Errorf("setting %q is not written", name)
				case at == 0 || members[at-1].key != "_"+name:
					before := "nothing"
					if at > 0 {
						before = strconv.Quote(members[at-1].key)
					}
					t.Errorf("setting %q has no key %q directly before it; the key before it is %s", name, "_"+name, before)
				case !ccIsListOfStrings(members[at-1].value):
					t.Errorf("%q is not a list of strings: %s", "_"+name, members[at-1].value)
				}
			}

			// Nothing else is a comment: `_commands_doc` is gone, replaced by
			// `_commands`, and `_doc` is the one that was always there.
			allowed := map[string]bool{"_doc": true}
			for _, name := range ccSettings {
				allowed["_"+name] = true
			}
			for _, m := range members {
				if strings.HasPrefix(m.key, "_") && !allowed[m.key] {
					t.Errorf("init writes a comment key %q that is neither _doc nor the block of a setting", m.key)
				}
			}

			if len(members) == 0 || members[0].key != "_doc" {
				t.Fatalf("_doc is not the first key of the file")
			}
			var doc []string
			if err := json.Unmarshal(members[0].value, &doc); err != nil || !slices.Equal(doc, ccDoc) {
				t.Errorf("_doc is not what it was before the story (%v):\n%s", err, members[0].value)
			}

			// A list under a `_` key inside `commands` stops the configuration
			// loading, so a block belongs at the top level and nowhere else.
			nested, err := ccNestedCommentKeys(raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(nested) > 0 {
				t.Errorf("comment keys below the top level: %v", nested)
			}
		})
	}
}

// The reason blocks sit at the top level: `commands` is a map of strings, so a
// list under a `_` key inside it is read as a command and refused, while the
// same key at the top level is skipped. It passes today, because the loader is
// left alone by this story (ANALYSIS.md section 1a, answer 3), and it fails the
// day somebody puts a block where it breaks every gate.
func TestAC1AListUnderAnUnderscoreKeyInsideCommandsIsRefusedWhereTheTopLevelOneIsNot(t *testing.T) {
	if _, err := config.Parse([]byte(`{"_commands": ["a", "b"], "version": 1}`)); err != nil {
		t.Errorf("a block at the top level is refused: %v", err)
	}
	_, err := config.Parse([]byte(`{"commands": {"_note": ["a", "b"]}}`))
	if err == nil {
		t.Fatal("a list under a _ key inside commands loads; the placement rule has lost its reason")
	}
	if !strings.Contains(err.Error(), "_note") {
		t.Errorf("the refusal does not name the key: %v", err)
	}
}

// ---------------------------------------------------------------------- AC-2

// ccHeading is one heading of a Markdown page and the anchor GitHub gives it.
type ccHeading struct {
	text   string
	anchor string
}

var ccHeadingLine = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*\s*$`)

// ccSlug is GitHub's anchor for a heading's text: lower case, everything but
// letters, digits, underscores and hyphens dropped, spaces turned to hyphens.
func ccSlug(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), unicode.IsMark(r), r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ccHeadings is every heading of doc outside a code fence, in order. The second
// heading of the same text gets -1 after its anchor, the third -2.
func ccHeadings(doc string) []ccHeading {
	var out []ccHeading
	seen := map[string]int{}
	fenced := false
	for line := range strings.Lines(doc) {
		line = strings.TrimRight(line, "\r\n")
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		m := ccHeadingLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		slug := ccSlug(m[1])
		anchor := slug
		if n := seen[slug]; n > 0 {
			anchor = slug + "-" + strconv.Itoa(n)
		}
		seen[slug]++
		out = append(out, ccHeading{text: m[1], anchor: anchor})
	}
	return out
}

func ccConfigurationPage(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "configuration.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var ccAnchor = regexp.MustCompile(`^[a-z0-9_-]+$`)

// AC-2: a block is a title, then what the setting controls, then the URL of
// that setting's own heading in docs/configuration.md, in at most six lines.
func TestAC2EachBlockIsATitleWhatTheSettingControlsAndTheURLOfItsOwnHeading(t *testing.T) {
	headings := ccHeadings(ccConfigurationPage(t))
	for _, stack := range ccStacks {
		t.Run(stack.name, func(t *testing.T) {
			_, raw := ccInit(t, stack.files)
			members, err := ccMembers(raw)
			if err != nil {
				t.Fatal(err)
			}
			blocks := map[string][]string{}
			for _, name := range ccSettings {
				lines, ok := ccBlockOf(members, name)
				if !ok {
					t.Errorf("setting %q has no block %q to read", name, "_"+name)
					continue
				}
				blocks[name] = lines
				n := len(lines)
				if n < 3 || n > 6 {
					t.Errorf("%s: the block has %d lines, want 3 to 6: a title, what the setting controls, the URL", name, n)
					continue
				}
				for i, line := range lines {
					if strings.TrimSpace(line) == "" {
						t.Errorf("%s: line %d of the block is empty", name, i+1)
					}
				}

				title, body, url := lines[0], lines[1:n-1], lines[n-1]
				if strings.HasPrefix(title, "http") {
					t.Errorf("%s: the title is a URL: %q", name, title)
				}
				controls := false
				for _, line := range body {
					switch {
					case strings.HasPrefix(line, "http") || strings.Contains(line, "github.com/bbsnly"):
						t.Errorf("%s: a line between the title and the last is a URL: %q", name, line)
					case line == title:
						t.Errorf("%s: a line repeats the title: %q", name, line)
					case len(strings.Fields(line)) >= 3:
						controls = true
					}
				}
				if !controls {
					t.Errorf("%s: no line between the title and the URL says anything (none has three words): %q", name, body)
				}

				anchor, ok := strings.CutPrefix(url, ccDocURL)
				if !ok || !ccAnchor.MatchString(anchor) {
					t.Errorf("%s: the last line is %q, want a URL of the form %s<anchor>", name, url, ccDocURL)
					continue
				}
				at := slices.IndexFunc(headings, func(h ccHeading) bool { return h.anchor == anchor })
				if at < 0 {
					t.Errorf("%s: docs/configuration.md has no heading with the anchor #%s", name, anchor)
					continue
				}
				if own := strings.TrimSpace(strings.ReplaceAll(headings[at].text, "`", "")); own != name {
					t.Errorf("%s: #%s is the heading %q, not the heading of %s itself", name, anchor, headings[at].text, name)
				}
			}

			// A block that is the same text under twelve names, or the same
			// body under twelve URLs, says nothing about any of them.
			titles, bodies, whole := map[string]string{}, map[string]string{}, map[string]string{}
			for _, name := range ccSettings {
				lines := blocks[name]
				if len(lines) < 3 {
					continue
				}
				for kind, seen := range map[string]map[string]string{
					"title": titles, "text between the title and the URL": bodies, "text": whole,
				} {
					var key string
					switch kind {
					case "title":
						key = lines[0]
					case "text":
						key = strings.Join(lines, "\n")
					default:
						key = strings.Join(lines[1:len(lines)-1], "\n")
					}
					if other, dup := seen[key]; dup {
						t.Errorf("%s and %s have the same %s: %q", other, name, kind, key)
					}
					seen[key] = name
				}
			}
			urls := map[string]string{}
			for _, name := range ccSettings {
				if lines := blocks[name]; len(lines) >= 3 {
					if other, dup := urls[lines[len(lines)-1]]; dup {
						t.Errorf("%s and %s end on the same URL %q", other, name, lines[len(lines)-1])
					}
					urls[lines[len(lines)-1]] = name
				}
			}
		})
	}
}

// Answer 1 of the person's decisions: the headings that existed keep their
// anchors, because other pages link to them. It passes today and must keep
// passing, whatever headings are added beside them.
func TestAC2TheHeadingsOtherPagesLinkToStillExist(t *testing.T) {
	headings := ccHeadings(ccConfigurationPage(t))
	for _, anchor := range []string{
		"backlogpath", "pathstests", "pathssrc", "specpaths", "reviewsgate7_advisory",
		"freezeallow_new_test_files", "commands", "thresholds", "git", "version", "loop", "human_gates", "budget",
	} {
		if !slices.ContainsFunc(headings, func(h ccHeading) bool { return h.anchor == anchor }) {
			t.Errorf("docs/configuration.md has lost the heading #%s, which other pages link to", anchor)
		}
	}
}

// Answer 2: `_commands_doc` is replaced by `_commands`, which carries what it
// said. The part of that which was ever different is which stack init guessed,
// so the block names it. (The wording around it is the author's.)
func TestAC2TheCommandsBlockCarriesTheStackNoteTheOldCommandsDocKeyHad(t *testing.T) {
	for _, stack := range ccStacks {
		t.Run(stack.name, func(t *testing.T) {
			_, raw := ccInit(t, stack.files)
			members, err := ccMembers(raw)
			if err != nil {
				t.Fatal(err)
			}
			lines, ok := ccBlockOf(members, "commands")
			if !ok {
				t.Fatal(`there is no "_commands" block`)
			}
			text := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
			want := "Guessed from a " + stack.name + " project."
			if stack.name == "none" {
				want = "No stack was recognised, so these are empty."
			}
			if !strings.Contains(text, want) {
				t.Errorf("the _commands block does not say %q:\n%s", want, strings.Join(lines, "\n"))
			}
			for _, other := range ccStacks {
				if other.name != stack.name && other.name != "none" && strings.Contains(text, "Guessed from a "+other.name+" project") {
					t.Errorf("the _commands block of a %s project names %s", stack.name, other.name)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------- AC-3

// AC-3: the file init writes, with its blocks, is valid JSON, writes no key
// twice in any object, and the loader reports no setting it does not know.
// Valid JSON and no repeated key hold today as well, so the test first asks for
// the blocks: without them it would say nothing about this story.
func TestAC3TheFileInitWritesIsValidJSONWithNoRepeatedKeyAndNoUnknownSetting(t *testing.T) {
	for _, stack := range ccStacks {
		t.Run(stack.name, func(t *testing.T) {
			root, raw := ccInit(t, stack.files)
			if !json.Valid(raw) {
				t.Fatalf("the file is not valid JSON:\n%s", raw)
			}
			problems, err := ccMissingBlocks(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range problems {
				t.Error(p.message)
			}
			repeated, err := ccRepeatedKeys(raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(repeated) > 0 {
				t.Errorf("keys written twice in one object: %v", repeated)
			}
			if _, err := config.Load(root); err != nil {
				t.Errorf("the configuration does not load: %v", err)
			}
			if unknown := config.UnknownKeys(root); len(unknown) > 0 {
				t.Errorf("the loader reports settings it does not know: %v", unknown)
			}
		})
	}
}

// The check above is only worth something if it sees a repeated key. A decoder
// into a map does not: the last one wins and nothing is said.
func TestAC3TheRepeatedKeyCheckSeesAKeyWrittenTwiceAtAnyDepth(t *testing.T) {
	for _, tc := range []struct {
		name, doc string
		want      []string
	}{
		{"nothing repeated", `{"_a":["x"],"a":{"k":1},"b":[{"k":1},{"k":2}]}`, nil},
		{"at the top level", `{"a":1,"b":2,"a":3}`, []string{"a"}},
		{"a block written twice", `{"_paths":["x"],"paths":{},"_paths":["y"]}`, []string{"_paths"}},
		{"inside an object", `{"a":{"k":1,"k":2}}`, []string{"a.k"}},
		{"inside an object in an array", `{"a":[{"k":1,"k":2}]}`, []string{"a[].k"}},
		{"the same key in two objects", `{"a":{"k":1},"b":{"k":1}}`, nil},
	} {
		got, err := ccRepeatedKeys([]byte(tc.doc))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: repeated keys = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------- AC-5

// AC-5: a top-level setting with no block directly before it fails the tests
// and is named. The real check runs on what the template renders for every
// stack; the fixtures after it prove the check is able to fail.
func TestAC5EverySettingInTheTemplateHasABlockDirectlyBeforeIt(t *testing.T) {
	for _, stack := range ccStacks {
		t.Run(stack.name, func(t *testing.T) {
			_, raw := ccInit(t, stack.files)
			members, err := ccMembers(raw)
			if err != nil {
				t.Fatal(err)
			}
			settings := 0
			for _, m := range members {
				if !strings.HasPrefix(m.key, "_") {
					settings++
				}
			}
			if settings != len(ccSettings) {
				t.Errorf("the template writes %d top-level settings, want %d: this check would pass over a different file", settings, len(ccSettings))
			}
			problems, err := ccMissingBlocks(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range problems {
				t.Error(p.message)
			}
		})
	}
}

// A check that has only ever been run on the real template proves nothing: if
// it never fails, it never failed. These documents have a block missing, a block
// in the wrong place, and a block that is not a list of strings, and the check
// has to name exactly the settings that are wrong.
func TestAC5TheCheckNamesTheSettingWhoseBlockIsMissingMisplacedOrNotAList(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want []string
	}{
		{"every setting has its block", `{"_doc":["d"],"_a":["A"],"a":1,"_b":["B"],"b":2,"_c":["C"],"c":3}`, nil},
		{"one block is missing", `{"_doc":["d"],"_a":["A"],"a":1,"b":2,"_c":["C"],"c":3}`, []string{"b"}},
		{"the first setting has none", `{"a":1,"_b":["B"],"b":2}`, []string{"a"}},
		{"the last setting has none", `{"_a":["A"],"a":1,"_b":["B"],"b":2,"c":3}`, []string{"c"}},
		{"a block is there but after its setting", `{"_a":["A"],"a":1,"b":2,"_b":["B"],"_c":["C"],"c":3}`, []string{"b"}},
		{"a block is there but with a key between", `{"_a":["A"],"a":1,"_b":["B"],"_c":["C"],"b":2,"c":3}`, []string{"b", "c"}},
		{"a comment key that is not the setting's name", `{"_a_doc":["A"],"a":1}`, []string{"a"}},
		{"the value is a string", `{"_a":["A"],"a":1,"_b":"B","b":2}`, []string{"b"}},
		{"the value is null", `{"_a":null,"a":1}`, []string{"a"}},
		{"the value is an object", `{"_a":{"title":"A"},"a":1}`, []string{"a"}},
		{"a list holds a number", `{"_a":["A",2],"a":1}`, []string{"a"}},
		{"two settings have none", `{"a":1,"b":2}`, []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems, err := ccMissingBlocks([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range problems {
				got = append(got, p.setting)
				if !strings.Contains(p.message, strconv.Quote(p.setting)) {
					t.Errorf("the message does not name the setting %q: %s", p.setting, p.message)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("settings reported = %v, want %v", got, tc.want)
			}
		})
	}
}
