package cli

// US-002: the configuration `sdlc init` writes carries a comment block above
// every setting. What is held here is what the story says does not change
// (AC-4), the documentation that describes the blocks (AC-6), and the way the
// golden files of US-026 AC-10 are compared now that init's file has comments
// in it.
//
// The comparison of values is made on text, with the top-level `_` keys cut
// out, and not on a decoded map: a map cannot see a number written 1.0 where it
// was 1, a changed layout, or a value moved to another place, and "the comment
// blocks are added and nothing else in the file changes" is a claim about the
// file.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// ccmSettings is the twelve top-level settings the story names.
var ccmSettings = []string{
	"version", "backlog", "git", "commands", "thresholds", "paths",
	"spec", "loop", "reviews", "freeze", "human_gates", "budget",
}

// ccmStacks is every stack init detects, and none, with the smallest files it
// recognises each by. golden is the file under testdata/init-config that holds
// what init wrote for it before the story, less its top-level `_` keys.
var ccmStacks = []struct {
	name, golden string
	files        map[string]string
}{
	{"none", "none.golden", nil},
	{"Go", "go.golden", map[string]string{"go.mod": "module example.com/x\n"}},
	{"Rust", "rust.golden", map[string]string{"Cargo.toml": ""}},
	{"Node", "node.golden", map[string]string{"package.json": `{"scripts":{"build":"tsc","test":"vitest run","lint":"eslint ."}}`}},
	{"Python", "python.golden", map[string]string{"pyproject.toml": ""}},
}

// ccmWithoutComments is text, a JSON object, with every member at its top level
// whose key starts with _ cut out: the key, its value and the comma after it,
// and the line they stood on when they had it to themselves. Lines left empty
// are dropped, so that where a block leaves a blank line is not part of what
// init writes. A `_` key below the top level stays, because it is not a
// comment: inside `commands` it is a command.
func ccmWithoutComments(text string) (string, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	tok, err := dec.Token()
	if err != nil {
		return "", fmt.Errorf("not a JSON object: %w", err)
	}
	if tok != json.Delim('{') {
		return "", fmt.Errorf("not a JSON object: it starts with %v", tok)
	}
	type cut struct{ from, to int }
	var cuts []cut
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		key, ok := tok.(string)
		if !ok {
			return "", fmt.Errorf("unexpected token %v where a key should be", tok)
		}
		keyEnd := int(dec.InputOffset())
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return "", err
		}
		if !strings.HasPrefix(key, "_") {
			continue
		}
		from, to := strings.LastIndex(text[:keyEnd], `"`+key+`"`), int(dec.InputOffset())
		j := to
		for j < len(text) && (text[j] == ' ' || text[j] == '\t') {
			j++
		}
		if j >= len(text) || text[j] != ',' {
			return "", fmt.Errorf("the comment key %q has no comma after it, so cutting it would leave one before", key)
		}
		to = j + 1
		lineStart := from
		for lineStart > 0 && (text[lineStart-1] == ' ' || text[lineStart-1] == '\t') {
			lineStart--
		}
		if lineStart == 0 || text[lineStart-1] == '\n' {
			k := to
			for k < len(text) && (text[k] == ' ' || text[k] == '\t' || text[k] == '\r') {
				k++
			}
			if k < len(text) && text[k] == '\n' {
				from, to = lineStart, k+1
			}
		}
		cuts = append(cuts, cut{from, to})
	}
	out := text
	for _, c := range slices.Backward(cuts) {
		out = out[:c.from] + out[c.to:]
	}
	var kept strings.Builder
	for line := range strings.Lines(out) {
		if strings.TrimSpace(line) != "" {
			kept.WriteString(line)
		}
	}
	if !json.Valid([]byte(kept.String())) {
		return "", fmt.Errorf("what is left is not JSON:\n%s", kept.String())
	}
	return kept.String(), nil
}

// stfWithoutConfigComments is a transcript of the walk of staffing_off_test.go
// with the top-level `_` keys cut out of the .sdlc/config.json it holds, and
// every other byte of it as it was.
func stfWithoutConfigComments(t *testing.T, transcript string) string {
	t.Helper()
	const head = "== .sdlc/config.json\n"
	i := strings.Index(transcript, head)
	if i < 0 {
		t.Fatalf("the transcript holds no %q section", strings.TrimSpace(head))
	}
	start := i + len(head)
	n := strings.Index(transcript[start:], "\n$ sdlc ")
	if n < 0 {
		t.Fatal("the transcript has no command after the configuration it holds")
	}
	section, err := ccmWithoutComments(transcript[start : start+n])
	if err != nil {
		t.Fatalf("the configuration in the transcript: %v", err)
	}
	return transcript[:start] + section + transcript[start+n:]
}

// ccmRoundTrip is the layout the cut has to produce, and what it has to leave.
const ccmBefore = `{
  "_doc": [
    "a, with a comma",
    "",
    "b ] and [ in a string"
  ],

  "version": 1,

  "_commands_doc": "a note, with a comma",
  "commands": {
    "_keep": "a command called _keep",
    "test":  "go test"
  },
  "_budget": ["t", "u [x]"], "budget": { "per_story_usd": 60 }
}
`

const ccmAfter = `{
  "version": 1,
  "commands": {
    "_keep": "a command called _keep",
    "test":  "go test"
  },
   "budget": { "per_story_usd": 60 }
}
`

// What the comparison cuts, and only that: a test of the comparison itself,
// which holds today as well. A comparison that cut more would pass a changed
// value, and one that cut less would fail on every block.
func TestAC4TheComparisonCutsTopLevelCommentKeysAndLeavesEveryOtherByte(t *testing.T) {
	t.Parallel()
	got, err := ccmWithoutComments(ccmBefore)
	if err != nil {
		t.Fatal(err)
	}
	if got != ccmAfter {
		t.Errorf("cut:\n%s\nwant:\n%s", got, ccmAfter)
	}

	// A comment key with nothing after it cannot be cut cleanly: it says so
	// rather than leaving a comma behind.
	if _, err := ccmWithoutComments(`{"a": 1, "_z": ["x"]}`); err == nil {
		t.Error("a comment key that is the last member was cut without a word")
	}
}

func TestAC4TheComparisonSeesAChangedValueAndNotAChangedComment(t *testing.T) {
	t.Parallel()
	base, err := ccmWithoutComments(ccmBefore)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, from, to string
		same           bool
	}{
		{"other words in a block", `"a, with a comma"`, `"other words"`, true},
		{"a longer block", `["t", "u [x]"]`, `["t", "u [x]", "and another line"]`, true},
		{"a block that is a string", `"a note, with a comma"`, `"another note"`, true},
		{"a number", `"version": 1`, `"version": 2`, false},
		{"a number written another way", `"per_story_usd": 60`, `"per_story_usd": 60.0`, false},
		{"a command", `"test":  "go test"`, `"test":  "go vet"`, false},
		{"a comment below the top level", `"_keep": "a command called _keep"`, `"_keep": "another"`, false},
		{"a layout", `"test":  "go test"`, `"test": "go test"`, false},
		{"a key removed", `"version": 1,`, ``, false},
	} {
		changed := strings.Replace(ccmBefore, tc.from, tc.to, 1)
		if changed == ccmBefore {
			t.Fatalf("%s: the fixture does not hold %q", tc.name, tc.from)
		}
		got, err := ccmWithoutComments(changed)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if (got == base) != tc.same {
			t.Errorf("%s: compares equal = %v, want %v", tc.name, got == base, tc.same)
		}
	}
}

// The walk of staffing_off_test.go prints the configuration init wrote into its
// transcript, and the golden files hold it as it was written before this story,
// comments and all. The transcripts are compared with the comment keys cut
// out of the configuration in each, and are otherwise compared byte for byte
// as they were: US-026 AC-10's evidence that no command's output changes is
// everything else in the walk.
func TestAC4TheTranscriptComparisonCutsOnlyTheConfigurationsCommentKeys(t *testing.T) {
	t.Parallel()
	const around = "$ sdlc init\nexit 0\nstdout:\n_doc is in this output, and is not a key\n\n"
	transcript := around + "== .sdlc/config.json\n" + ccmBefore + "\n$ sdlc status\nexit 0\nstdout:\n\"_budget\": [\"not a key either\"],\n"
	want := around + "== .sdlc/config.json\n" + ccmAfter + "\n$ sdlc status\nexit 0\nstdout:\n\"_budget\": [\"not a key either\"],\n"
	if got := stfWithoutConfigComments(t, transcript); got != want {
		t.Errorf("cut:\n%s\nwant:\n%s", got, want)
	}
	changed := strings.Replace(transcript, "$ sdlc status\nexit 0", "$ sdlc status\nexit 1", 1)
	if stfWithoutConfigComments(t, changed) == want {
		t.Error("a changed exit code outside the configuration was not seen")
	}
}

// The golden files of the walk were written by the tree before US-026 (and
// before this story), so they still hold `_doc` and `_commands_doc`: cutting the
// comment keys out of both sides of the comparison leaves the evidence as it
// was and does not fix the wording of any block. It holds today and has to keep
// holding.
func TestAC4TheStaffingOffGoldenFilesHoldAConfigurationOnceItsCommentsAreCut(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"prose.golden", "json.golden"} {
		raw, err := os.ReadFile(stfGolden(name))
		if err != nil {
			t.Fatal(err)
		}
		cut := stfWithoutConfigComments(t, string(raw))
		if strings.Contains(cut, `"_doc"`) || strings.Contains(cut, `"_commands_doc"`) {
			t.Errorf("%s: a comment key is left in the configuration", name)
		}
		for _, setting := range ccmSettings {
			if !strings.Contains(cut, "\n  \""+setting+"\":") {
				t.Errorf("%s: the setting %q is gone from the configuration once the comments are cut", name, setting)
			}
		}
	}
}

// AC-4: every setting's value, for every stack and for none, is what init wrote
// before the story. The files under testdata/init-config were captured from
// the template as it stood before the story, with the top-level `_` keys cut
// out, and nothing here reads the implementation to learn what to expect. It
// passes today, by design: it guards what must not change, and it fails the day
// a value, a command or the layout of a line moves.
func TestAC4EverySettingHasTheValueInitWroteBeforeTheStoryForEveryStackAndNone(t *testing.T) {
	t.Parallel()
	for _, stack := range ccmStacks {
		t.Run(stack.name, func(t *testing.T) {
			t.Parallel()
			root := gitProject(t)
			for rel, body := range stack.files {
				writeFile(t, root, rel, body)
			}
			mustRun(t, "init")
			raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := ccmWithoutComments(string(raw))
			if err != nil {
				t.Fatal(err)
			}
			wantRaw, err := os.ReadFile(filepath.Join("testdata", "init-config", stack.golden))
			if err != nil {
				t.Fatalf("what init wrote before the story is not there to compare with: %v", err)
			}
			want := string(wantRaw)
			if got == want {
				return
			}
			gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
			for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
				var g, w string
				if i < len(gotLines) {
					g = gotLines[i]
				}
				if i < len(wantLines) {
					w = wantLines[i]
				}
				if g != w {
					t.Fatalf("init writes a different configuration for a %s project, less its comments, at line %d:\n got  %q\n want %q",
						stack.name, i+1, g, w)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------- AC-6

// ccmFence is one fenced code block of a Markdown page.
type ccmFence struct{ lang, body string }

func ccmFences(doc string) []ccmFence {
	var out []ccmFence
	var open *ccmFence
	for line := range strings.Lines(doc) {
		trimmed := strings.TrimSpace(line)
		if lang, fenced := strings.CutPrefix(trimmed, "```"); fenced {
			if open == nil {
				open = &ccmFence{lang: strings.TrimSpace(lang)}
			} else {
				out = append(out, *open)
				open = nil
			}
			continue
		}
		if open != nil {
			open.body += line
		}
	}
	return out
}

// ccmSentences is the prose of a Markdown page outside its code, as sentences
// with their line breaks undone, so that where a line of the file wraps does not
// decide whether a sentence is found.
func ccmSentences(doc string) []string {
	var paragraphs []string
	var current []string
	fenced := false
	flush := func() {
		if len(current) > 0 {
			paragraphs = append(paragraphs, strings.Join(current, " "))
			current = nil
		}
	}
	for line := range strings.Lines(doc) {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			flush()
			fenced = !fenced
		case fenced:
		case trimmed == "":
			flush()
		default:
			current = append(current, trimmed)
		}
	}
	flush()

	var out []string
	for _, p := range paragraphs {
		start := 0
		for i := range len(p) {
			if (p[i] == '.' || p[i] == '!' || p[i] == '?') && (i+1 == len(p) || p[i+1] == ' ') {
				out = append(out, strings.TrimSpace(p[start:i+1]))
				start = i + 1
			}
		}
		if rest := strings.TrimSpace(p[start:]); rest != "" {
			out = append(out, rest)
		}
	}
	return out
}

var (
	ccmAboveEach     = regexp.MustCompile(`(above|before|ahead of) (each|every) (top-level )?setting`)
	ccmNeverASetting = regexp.MustCompile(`(never|not) (read|treated|taken|loaded|used) as (a |an )?settings?`)
	ccmStartsWith    = regexp.MustCompile(`\b(start|starts|starting|begin|begins|beginning) with\b`)
)

// ccmSaysInitWritesABlock is whether s says that init writes a comment block
// above each setting.
func ccmSaysInitWritesABlock(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "init") && strings.Contains(l, "comment block") && ccmAboveEach.MatchString(l)
}

// ccmSaysATopLevelKeyIsNeverASetting is whether s says that a top-level key
// starting with _ is never read as a setting.
func ccmSaysATopLevelKeyIsNeverASetting(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "top-level") && strings.Contains(l, "`_`") && ccmNeverASetting.MatchString(l)
}

// ccmSaysAnyKeyWithAnUnderscoreIsSkipped is whether s says something about a key
// that starts with _ without saying it is a top-level one. It is not true of a
// key inside `commands`, which is read as a command.
func ccmSaysAnyKeyWithAnUnderscoreIsSkipped(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "`_`") && ccmStartsWith.MatchString(l) && !strings.Contains(l, "top-level")
}

// The three checks on the page's sentences, put to sentences that do and do not
// say what they look for. They hold today, and are what makes the page's tests
// able to fail.
func TestAC6TheChecksOnThePagesSentencesAcceptTheClaimsAndRefuseTheOldWording(t *testing.T) {
	t.Parallel()
	const old = "A key that starts with `_` is a comment and is not reported, and `commands` takes any name."
	for _, tc := range []struct {
		name  string
		check func(string) bool
		said  string
		want  bool
	}{
		{"init writes", ccmSaysInitWritesABlock, "`sdlc init` writes a comment block above each setting.", true},
		{"init writes, reworded", ccmSaysInitWritesABlock, "Above every setting, init writes a comment block that says what it controls.", true},
		{"no init", ccmSaysInitWritesABlock, "A comment block sits above each setting.", false},
		{"no block", ccmSaysInitWritesABlock, "`sdlc init` writes the settings above each section.", false},
		{"not above each", ccmSaysInitWritesABlock, "`sdlc init` writes a comment block for the commands.", false},
		{"never a setting", ccmSaysATopLevelKeyIsNeverASetting, "A top-level key that starts with `_` is never read as a setting.", true},
		{"only a comment", ccmSaysATopLevelKeyIsNeverASetting, "A top-level key that starts with `_` is a comment.", false},
		{"any key", ccmSaysATopLevelKeyIsNeverASetting, old, false},
		{"the old wording is any key", ccmSaysAnyKeyWithAnUnderscoreIsSkipped, old, true},
		{"top-level is any key", ccmSaysAnyKeyWithAnUnderscoreIsSkipped, "A top-level key that starts with `_` is never read as a setting.", false},
		{"no underscore", ccmSaysAnyKeyWithAnUnderscoreIsSkipped, "A key that starts with a letter is a setting.", false},
	} {
		if got := tc.check(tc.said); got != tc.want {
			t.Errorf("%s: check(%q) = %v, want %v", tc.name, tc.said, got, tc.want)
		}
	}
}

// AC-6: the page says that init writes a comment block above each setting.
func TestAC6TheConfigurationPageSaysInitWritesACommentBlockAboveEachSetting(t *testing.T) {
	t.Parallel()
	if !slices.ContainsFunc(ccmSentences(page(t, "configuration.md")), ccmSaysInitWritesABlock) {
		t.Error("docs/configuration.md has no sentence saying that init writes a comment block above each setting")
	}
}

// AC-6, narrowed by the person (ANALYSIS.md section 1a, answer 3): the page says
// that a top-level key starting with _ is never read as a setting, and says
// nothing wider, because the same key inside `commands` is read as a command.
func TestAC6TheConfigurationPageSaysATopLevelKeyStartingWithAnUnderscoreIsNeverASetting(t *testing.T) {
	t.Parallel()
	sentences := ccmSentences(page(t, "configuration.md"))
	if !slices.ContainsFunc(sentences, ccmSaysATopLevelKeyIsNeverASetting) {
		t.Error("docs/configuration.md has no sentence saying that a top-level key starting with `_` is never read as a setting")
	}
	for _, s := range sentences {
		if ccmSaysAnyKeyWithAnUnderscoreIsSkipped(s) {
			t.Errorf("docs/configuration.md says of any key starting with `_` what is true of a top-level one only: %q", s)
		}
	}
}

// ccmShown is one comment block as the page shows it in a code fence.
type ccmShown struct {
	setting string
	lines   []string
}

var ccmBlockKey = regexp.MustCompile(`"_([a-z_]+)"\s*:\s*\[`)

// ccmShownBlocks is every block of one of the twelve settings that a code fence
// of doc shows, read as the list of strings it is, wherever in the fence it is.
func ccmShownBlocks(t *testing.T, doc string) []ccmShown {
	t.Helper()
	var out []ccmShown
	for _, fence := range ccmFences(doc) {
		for _, at := range ccmBlockKey.FindAllStringSubmatchIndex(fence.body, -1) {
			setting := fence.body[at[2]:at[3]]
			if !slices.Contains(ccmSettings, setting) {
				continue
			}
			var lines []string
			if err := json.NewDecoder(strings.NewReader(fence.body[at[1]-1:])).Decode(&lines); err != nil {
				t.Errorf("the page shows _%s, and it is not a list of strings: %v", setting, err)
				continue
			}
			out = append(out, ccmShown{setting, lines})
		}
	}
	return out
}

// AC-6: the page shows a block as it is written, line for line. The page is
// headed "What init writes for a Go project", so the block is the one a Go
// project's init writes. The first JSON example on the page has to stay free of
// `_` keys: TestTheConfigurationExampleIsWhatInitWrites compares it with init's
// output less its comments, and holds that today.
func TestAC6TheBlockTheConfigurationPageShowsIsTheBlockInitWritesLineForLine(t *testing.T) {
	t.Parallel()
	shown := ccmShownBlocks(t, page(t, "configuration.md"))
	if len(shown) == 0 {
		t.Fatal("docs/configuration.md shows no comment block as init writes it, in a code fence, under a key `_<setting>`")
	}

	root := gitProject(t)
	writeFile(t, root, "go.mod", "module example.com/x\n")
	mustRun(t, "init")
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]json.RawMessage
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	for _, s := range shown {
		var lines []string
		if err := json.Unmarshal(written["_"+s.setting], &lines); err != nil {
			t.Errorf("init writes no block _%s that the page could be showing: %v", s.setting, err)
			continue
		}
		if !reflect.DeepEqual(s.lines, lines) {
			t.Errorf("the page shows _%s as\n%q\nand init writes\n%q", s.setting, s.lines, lines)
		}
	}
}
