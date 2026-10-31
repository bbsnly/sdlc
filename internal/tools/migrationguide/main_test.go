package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// US-119: `./task migration-guide -- <from> <to>` writes a guide for a person
// to complete. These tests run the tool in-process against repositories with
// real tags, built in a temp directory, and read what it wrote.

// A fixture history. Each commit has a key the tests refer to it by, a
// subject, and paragraphs after the subject (the last of them is the footer
// where one is wanted). The tags sit where each range under test needs them:
//
//	old     breaking, before v1.0.0           outside every range below
//	start   v1.0.0 (and rel/1.0)
//	flag    breaking: "!" with a scope
//	tidy    not breaking
//	config  breaking: BREAKING CHANGE footer
//	mention not breaking: the words in the middle of a sentence
//	        v1.1.0 (annotated)
//	bang    breaking: "!" without a scope
//	        v2.0.0
//	plain   not breaking                       v2.0.1
//	mention2 not breaking, and nothing else    v2.1.0
//	late    breaking, after v2.1.0, untagged
type step struct {
	key, subject string
	paras        []string
	breaking     bool
	tags         []string
	annotated    bool
}

var history = []step{
	{key: "old", subject: "fix(cli)!: change what a bare start does before the range", breaking: true},
	{key: "start", subject: "chore: start the project", tags: []string{"v1.0.0", "rel/1.0"}},
	{key: "flag", subject: "feat(api)!: drop the legacy export flag", breaking: true,
		paras: []string{"The flag was deprecated for two releases."}},
	{key: "tidy", subject: "fix: tidy the output of the list command"},
	{key: "config", subject: "refactor: move the configuration file", breaking: true,
		paras: []string{
			"The file now lives in the config directory.",
			"BREAKING CHANGE: the old location is no longer read",
		}},
	{key: "mention", subject: "docs: explain what a breaking change is",
		paras: []string{"A commit is marked BREAKING CHANGE in its footer, and a subject with a bang " +
			"is breaking! This one is neither, it only mentions the words."},
		tags: []string{"v1.1.0"}, annotated: true},
	{key: "bang", subject: "feat!: require a newer runtime", breaking: true, tags: []string{"v2.0.0"}},
	{key: "plain", subject: "fix: correct a typo in the help text", tags: []string{"v2.0.1"}},
	{key: "mention2", subject: "chore: note how release notes are written",
		paras: []string{"We write BREAKING CHANGE: in a footer only when it is true."},
		tags:  []string{"v2.1.0"}},
	{key: "late", subject: "feat(late)!: only in a later release", breaking: true},
}

type fixture struct {
	root    string
	hash    map[string]string
	subject map[string]string
	paras   map[string][]string
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		root:    t.TempDir(),
		hash:    map[string]string{},
		subject: map[string]string{},
		paras:   map[string][]string{},
	}
	git(t, f.root, "init", "-q", "-b", "main")
	// One git process builds the whole history: a commit and a tag each as
	// their own call made every fixture take over a second.
	var stream strings.Builder
	data := func(msg string) { fmt.Fprintf(&stream, "data %d\n%s\n", len(msg), msg) }
	const who = "Test <test@example.com> 1700000000 +0000"
	for i, s := range history {
		msg := strings.Join(append([]string{s.subject}, s.paras...), "\n\n") + "\n"
		fmt.Fprintf(&stream, "commit refs/heads/main\nmark :%d\ncommitter %s\n", i+1, who)
		data(msg)
		if i > 0 {
			fmt.Fprintf(&stream, "from :%d\n", i)
		}
		stream.WriteString("\n")
		f.subject[s.key] = s.subject
		f.paras[s.key] = s.paras
		for _, tag := range s.tags {
			if s.annotated {
				fmt.Fprintf(&stream, "tag %s\nfrom :%d\ntagger %s\n", tag, i+1, who)
				data("release " + tag)
			} else {
				fmt.Fprintf(&stream, "reset refs/tags/%s\nfrom :%d\n\n", tag, i+1)
			}
		}
		if s.key == "mention" {
			fmt.Fprintf(&stream, "reset refs/heads/feature\nfrom :%d\n\n", i+1)
		}
	}
	cmd := exec.CommandContext(t.Context(), "git", "fast-import", "--quiet")
	cmd.Dir = f.root
	cmd.Stdin = strings.NewReader(stream.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import: %v\n%s", err, out)
	}
	hashes := strings.Fields(git(t, f.root, "log", "--reverse", "--format=%H", "main"))
	if len(hashes) != len(history) {
		t.Fatalf("the fixture has %d commits, want %d", len(hashes), len(history))
	}
	for i, s := range history {
		f.hash[s.key] = hashes[i]
	}
	return f
}

// generate runs the tool as `./task migration-guide -- from to` does, against
// the fixture's root.
func (f *fixture) generate(from, to string) error {
	return run([]string{"-C", f.root, from, to}, io.Discard)
}

func (f *fixture) guidePath(from, to string) string {
	return filepath.Join(f.root, "docs", "migrations", from+"-to-"+to+".md")
}

func (f *fixture) guide(t *testing.T, from, to string) string {
	t.Helper()
	if err := f.generate(from, to); err != nil {
		t.Fatalf("migration-guide %s %s: %v", from, to, err)
	}
	body, err := os.ReadFile(f.guidePath(from, to))
	if err != nil {
		t.Fatalf("migration-guide %s %s wrote no guide at docs/migrations/%s-to-%s.md: %v",
			from, to, from, to, err)
	}
	return string(body)
}

// snapshot is every path under the root but .git, with each file's bytes: what
// "wrote nothing" and "left it unchanged" are measured against.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	seen := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			seen[filepath.ToSlash(rel)] = "<dir>"
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		seen[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return seen
}

func unchanged(t *testing.T, root string, before map[string]string, what string) {
	t.Helper()
	if after := snapshot(t, root); !maps.Equal(before, after) {
		t.Errorf("%s changed the tree: before %d paths, after %d paths: %v", what, len(before), len(after), pathsOnlyIn(after, before))
	}
}

func pathsOnlyIn(a, b map[string]string) []string {
	var out []string
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			out = append(out, k)
		}
	}
	return out
}

// --- reading a guide without pinning its layout -----------------------------

type heading struct {
	level, line int
	text        string
}

// headings lists the ATX headings of a document, outside fenced code.
func headings(lines []string) []heading {
	var out []heading
	fenced := false
	for i, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		n := 0
		for n < len(l) && l[n] == '#' {
			n++
		}
		if n >= 1 && n <= 6 && len(l) > n && l[n] == ' ' {
			text := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(l[n:]), "# "))
			out = append(out, heading{level: n, line: i, text: text})
		}
	}
	return out
}

func headingName(h heading) string {
	return strings.ToLower(strings.TrimSuffix(h.text, ":"))
}

var wantedHeadings = []string{"what changed", "steps", "verify"}

// sections cuts a guide into one region for each "What changed" heading: from
// the heading above it (the commit's own) to the next heading as high or
// higher. It fails the test where those regions are not separate, or one lacks
// any of the three headings -- so a guide that lists every commit under one
// set of headings is not mistaken for one section for each.
func sections(t *testing.T, doc string) []string {
	t.Helper()
	lines := strings.Split(doc, "\n")
	hs := headings(lines)
	var starts []int
	for i, h := range hs {
		if headingName(h) == "what changed" {
			starts = append(starts, i)
		}
	}
	var regions []string
	parents := map[int]bool{}
	for _, s := range starts {
		level := hs[starts[0]].level
		if hs[s].level != level {
			t.Fatalf("the What changed headings are at different levels (%d and %d)", level, hs[s].level)
		}
		p := -1
		for j := s - 1; j >= 0; j-- {
			if hs[j].level < level {
				p = j
				break
			}
		}
		if p < 0 {
			t.Fatalf("the What changed heading on line %d has no heading above it to belong to", hs[s].line+1)
		}
		if parents[p] {
			t.Fatalf("more than one What changed heading sits under %q: the commits are not in sections of their own", hs[p].text)
		}
		parents[p] = true
		end := len(lines)
		for j := p + 1; j < len(hs); j++ {
			if hs[j].level <= hs[p].level {
				end = hs[j].line
				break
			}
		}
		for _, want := range wantedHeadings {
			n := 0
			for _, h := range hs {
				if h.line >= hs[p].line && h.line < end && h.level == level && headingName(h) == want {
					n++
				}
			}
			if n != 1 {
				t.Errorf("the section under %q has %d headings %q, want one", hs[p].text, n, want)
			}
		}
		regions = append(regions, strings.Join(lines[hs[p].line:end], "\n"))
	}
	return regions
}

func short(hash string) string { return hash[:7] }

// assertGuide checks a guide against the commits it must and must not carry.
func assertGuide(t *testing.T, f *fixture, doc, from, to string, breaking, notBreaking []string) {
	t.Helper()
	first := ""
	for l := range strings.SplitSeq(doc, "\n") {
		if strings.TrimSpace(l) != "" {
			first = l
			break
		}
	}
	if !strings.HasPrefix(first, "# ") || !strings.Contains(first, from) || !strings.Contains(first, to) {
		t.Errorf("the guide's first line is %q, want a title (# ...) naming %s and %s", first, from, to)
	}
	regions := sections(t, doc)
	if len(regions) != len(breaking) {
		t.Errorf("%d sections, want %d (one for each of %v)", len(regions), len(breaking), breaking)
	}
	for _, key := range breaking {
		var in []int
		for i, r := range regions {
			if strings.Contains(r, f.subject[key]) {
				in = append(in, i)
			}
		}
		if len(in) != 1 {
			t.Errorf("the subject of %s (%q) is in %d sections, want exactly one", key, f.subject[key], len(in))
			continue
		}
		if !strings.Contains(regions[in[0]], short(f.hash[key])) {
			t.Errorf("the section for %s does not carry its hash %s", key, short(f.hash[key]))
		}
		for i, r := range regions {
			if i != in[0] && strings.Contains(r, short(f.hash[key])) {
				t.Errorf("the hash of %s is in another commit's section", key)
			}
		}
	}
	for _, key := range notBreaking {
		if strings.Contains(doc, f.subject[key]) {
			t.Errorf("the guide carries the subject of %s, which is not a breaking commit in the range", key)
		}
		if strings.Contains(doc, short(f.hash[key])) {
			t.Errorf("the guide carries the hash of %s, which is not a breaking commit in the range", key)
		}
		for _, p := range f.paras[key] {
			if strings.Contains(doc, p) {
				t.Errorf("the guide carries the body of %s", key)
			}
		}
	}
	if strings.Contains(doc, f.root) {
		t.Errorf("the guide carries the path of the checkout it was made in")
	}
}

type rangeCase struct {
	from, to string
	breaking []string
}

var allKeys = func() []string {
	var ks []string
	for _, s := range history {
		ks = append(ks, s.key)
	}
	return ks
}()

func without(keys, drop []string) []string {
	var out []string
	for _, k := range keys {
		skip := false
		for _, d := range drop {
			skip = skip || d == k
		}
		if !skip {
			out = append(out, k)
		}
	}
	return out
}

var breakingRanges = []rangeCase{
	{"v1.0.0", "v1.1.0", []string{"flag", "config"}},
	{"v1.1.0", "v2.0.0", []string{"bang"}},
	{"v1.0.0", "v2.0.0", []string{"flag", "config", "bang"}},
	{"v1.0.0", "v2.1.0", []string{"flag", "config", "bang"}},
}

// --- AC-1 -------------------------------------------------------------------

func TestAC1TheGuideIsWrittenToDocsMigrationsNamedForBothVersionsAndTitledWithThem(t *testing.T) {
	f := newFixture(t)
	for _, c := range breakingRanges {
		t.Run("AC-1 "+c.from+" to "+c.to, func(t *testing.T) {
			doc := f.guide(t, c.from, c.to)
			first := ""
			for l := range strings.SplitSeq(doc, "\n") {
				if strings.TrimSpace(l) != "" {
					first = l
					break
				}
			}
			if !strings.HasPrefix(first, "# ") || !strings.Contains(first, c.from) || !strings.Contains(first, c.to) {
				t.Errorf("first line = %q, want a title naming %s and %s", first, c.from, c.to)
			}
		})
	}
}

func TestAC1ThereIsOneSectionForEachBreakingCommitInTheRangeAndForNoOther(t *testing.T) {
	for _, c := range breakingRanges {
		t.Run("AC-1 "+c.from+" to "+c.to, func(t *testing.T) {
			f := newFixture(t)
			doc := f.guide(t, c.from, c.to)
			assertGuide(t, f, doc, c.from, c.to, c.breaking, without(allKeys, c.breaking))
		})
	}
}

// A "!" after the type and scope, and a BREAKING CHANGE footer, are the two
// ways Conventional Commits marks a commit; each one alone has to be enough.
func TestAC1ACommitIsBreakingByEitherMarkAlone(t *testing.T) {
	f := newFixture(t)
	doc := f.guide(t, "v1.0.0", "v1.1.0")
	regions := sections(t, doc)
	has := func(key string) bool {
		for _, r := range regions {
			if strings.Contains(r, f.subject[key]) {
				return true
			}
		}
		return false
	}
	if !has("flag") {
		t.Errorf("AC-1: %q (a ! in the subject, and no footer) has no section", f.subject["flag"])
	}
	if !has("config") {
		t.Errorf("AC-1: %q (a BREAKING CHANGE footer, and no !) has no section", f.subject["config"])
	}
}

// The words are in the body of two commits, in the middle of a sentence. That
// is a mention, not a footer, and a guide with a section for it would send a
// person to describe a change that breaks nothing.
func TestAC1ABodyThatOnlyMentionsTheWordsIsNotABreakingCommit(t *testing.T) {
	f := newFixture(t)
	doc := f.guide(t, "v1.0.0", "v1.1.0")
	if strings.Contains(doc, f.subject["mention"]) || strings.Contains(doc, short(f.hash["mention"])) {
		t.Errorf("AC-1: the guide carries %q, whose body only mentions the words", f.subject["mention"])
	}
	for _, r := range sections(t, doc) {
		if strings.Contains(r, "only mentions the words") {
			t.Errorf("AC-1: a section carries the body of a commit that is not breaking")
		}
	}
}

func TestAC1ACommitBeforeFromOrAfterToIsLeftOut(t *testing.T) {
	f := newFixture(t)
	doc := f.guide(t, "v1.0.0", "v2.1.0")
	for _, key := range []string{"old", "late"} {
		if strings.Contains(doc, f.subject[key]) || strings.Contains(doc, short(f.hash[key])) {
			t.Errorf("AC-1: the guide carries %s, which is outside v1.0.0..v2.1.0", key)
		}
	}
	if !strings.Contains(doc, f.subject["flag"]) {
		t.Errorf("AC-1: the guide lacks %q, which is inside the range", f.subject["flag"])
	}
}

// The breaking commit named by a section is the one in the history, whatever
// repository the guide is made in: a second repository with other subjects
// and other hashes gives other sections.
func TestAC1TheSectionsComeFromTheRepositoryTheToolIsRunIn(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "config", "user.email", "test@example.com")
	git(t, root, "config", "user.name", "Test")
	git(t, root, "commit", "-q", "--allow-empty", "-m", "chore: begin")
	git(t, root, "tag", "r1")
	subjects := []string{
		"feat(zebra)!: replace the zebra protocol",
		"fix(yak)!: reject empty yak names",
		"feat: add a quiet mode",
	}
	hashes := map[string]string{}
	for _, s := range subjects {
		git(t, root, "commit", "-q", "--allow-empty", "-m", s)
		hashes[s] = git(t, root, "rev-parse", "HEAD")
	}
	git(t, root, "tag", "r2")
	if err := run([]string{"-C", root, "r1", "r2"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "docs", "migrations", "r1-to-r2.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(body)
	regions := sections(t, doc)
	if len(regions) != 2 {
		t.Fatalf("AC-1: %d sections, want 2:\n%s", len(regions), doc)
	}
	for _, s := range subjects[:2] {
		n := 0
		for _, r := range regions {
			if strings.Contains(r, s) && strings.Contains(r, short(hashes[s])) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("AC-1: %q and its hash are together in %d sections, want one", s, n)
		}
	}
	if strings.Contains(doc, subjects[2]) || strings.Contains(doc, short(hashes[subjects[2]])) {
		t.Errorf("AC-1: the guide carries the commit that is not breaking")
	}
	if !strings.HasPrefix(strings.TrimSpace(doc), "# ") || !strings.Contains(strings.SplitN(strings.TrimSpace(doc), "\n", 2)[0], "r2") {
		t.Errorf("AC-1: the title does not name both versions: %q", strings.SplitN(doc, "\n", 2)[0])
	}
}

// A guide with a timestamp or any other thing that changes between runs cannot
// be reviewed by diff. The tool refuses to overwrite, so the file is removed
// between the two runs.
func TestAC1TheSameRangeGivesTheSameGuideEachTime(t *testing.T) {
	f := newFixture(t)
	first := f.guide(t, "v1.0.0", "v2.0.0")
	if err := os.Remove(f.guidePath("v1.0.0", "v2.0.0")); err != nil {
		t.Fatal(err)
	}
	if second := f.guide(t, "v1.0.0", "v2.0.0"); second != first {
		t.Errorf("AC-1: the second guide differs from the first:\n%s\n---\n%s", first, second)
	}
}

// --- AC-2 -------------------------------------------------------------------

var quiet = regexp.MustCompile(`(?i)\bno\b.*\bbreaking\b`)

func TestAC2NoBreakingCommitStillWritesTheFileAndSaysSo(t *testing.T) {
	for _, c := range []rangeCase{
		{"v2.0.0", "v2.0.1", nil},
		{"v2.0.1", "v2.1.0", nil}, // the only commit has the words in a sentence
	} {
		t.Run("AC-2 "+c.from+" to "+c.to, func(t *testing.T) {
			f := newFixture(t)
			doc := f.guide(t, c.from, c.to)
			if !quiet.MatchString(doc) {
				t.Errorf("the guide does not say that no commit is marked breaking:\n%s", doc)
			}
			for _, h := range headings(strings.Split(doc, "\n")) {
				if headingName(h) == "what changed" {
					t.Errorf("the guide has a What changed section though no commit is breaking:\n%s", doc)
				}
			}
			for _, key := range allKeys {
				if strings.Contains(doc, f.subject[key]) || strings.Contains(doc, short(f.hash[key])) {
					t.Errorf("the guide carries commit %s, which is not marked breaking", key)
				}
			}
		})
	}
}

// --- AC-3 -------------------------------------------------------------------

func TestAC3AFileThatExistsIsRefusedNamedAndLeftAsItWas(t *testing.T) {
	for _, c := range []struct {
		name, from, to, existing string
	}{
		{"notes in the way of a guide with sections", "v1.0.0", "v1.1.0", "my own notes\n"},
		{"notes in the way of a guide with none", "v2.0.0", "v2.0.1", "my own notes\n"},
		{"an empty file", "v1.0.0", "v1.1.0", ""},
	} {
		t.Run("AC-3 "+c.name, func(t *testing.T) {
			f := newFixture(t)
			path := f.guidePath(c.from, c.to)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(c.existing), 0o644); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, f.root)
			err := f.generate(c.from, c.to)
			if err == nil {
				t.Fatal("the tool overwrote a guide that was there, and said nothing")
			}
			if !strings.Contains(err.Error(), c.from+"-to-"+c.to+".md") {
				t.Errorf("err = %q, want it to name %s-to-%s.md", err, c.from, c.to)
			}
			got, rerr := os.ReadFile(path)
			if rerr != nil || !bytes.Equal(got, []byte(c.existing)) {
				t.Errorf("the file is now %q (%v), want %q", got, rerr, c.existing)
			}
			unchanged(t, f.root, before, "the refusal")
		})
	}
}

// What a person wrote into a generated guide is what the tool must never lose:
// the second run is the one that could.
func TestAC3ASecondRunDoesNotOverwriteTheGuideThePersonCompleted(t *testing.T) {
	f := newFixture(t)
	generated := f.guide(t, "v1.0.0", "v1.1.0")
	completed := generated + "\nThe flag is gone; use the new one.\n"
	path := f.guidePath("v1.0.0", "v1.1.0")
	if err := os.WriteFile(path, []byte(completed), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, f.root)
	err := f.generate("v1.0.0", "v1.1.0")
	if err == nil || !strings.Contains(err.Error(), "v1.0.0-to-v1.1.0.md") {
		t.Errorf("AC-3: err = %v, want a refusal naming the file", err)
	}
	if got, _ := os.ReadFile(path); string(got) != completed {
		t.Errorf("AC-3: the person's text is gone: %q", got)
	}
	unchanged(t, f.root, before, "the second run")
}

// --- AC-4 -------------------------------------------------------------------

func TestAC4AVersionThatIsNotATagFailsNamingItAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	cases := []struct{ name, from, to, bad string }{
		{"a tag that does not exist as from", "v9.9.9", "v1.1.0", "v9.9.9"},
		{"a tag that does not exist as to", "v1.0.0", "v9.9.9", "v9.9.9"},
		{"a branch as to", "v1.0.0", "feature", "feature"},
		{"a branch as from", "main", "v1.1.0", "main"},
		{"HEAD", "v1.0.0", "HEAD", "HEAD"},
		{"a full commit hash as to", "v1.0.0", f.hash["config"], f.hash["config"]},
		{"a short commit hash as from", short(f.hash["flag"]), "v1.1.0", short(f.hash["flag"])},
		{"part of a tag's name", "v1.0", "v1.1.0", "v1.0"},
		{"a revision that starts at a tag", "v1.0.0", "v1.1.0~1", "v1.1.0~1"},
		{"a revision that starts at a tag, as from", "v1.1.0^", "v2.0.0", "v1.1.0^"},
	}
	for _, c := range cases {
		t.Run("AC-4 "+c.name, func(t *testing.T) {
			before := snapshot(t, f.root)
			err := f.generate(c.from, c.to)
			if err == nil {
				t.Fatalf("migration-guide %s %s succeeded", c.from, c.to)
			}
			if !strings.Contains(err.Error(), c.bad) {
				t.Errorf("err = %q, want it to name %q", err, c.bad)
			}
			unchanged(t, f.root, before, "the failure")
		})
	}
}

func TestAC4AWordThatCouldBeTakenForAnOptionOrACommandIsNotATag(t *testing.T) {
	f := newFixture(t)
	for _, word := range []string{"--help", "-x", "--output=x", "a;touch x", "$(touch x)", ""} {
		for _, place := range []string{"from", "to"} {
			t.Run("AC-4 "+place+" "+word, func(t *testing.T) {
				from, to := "v1.0.0", "v1.1.0"
				if place == "from" {
					from = word
				} else {
					to = word
				}
				before := snapshot(t, f.root)
				// After "--" the word is a word whatever it looks like.
				err := run([]string{"-C", f.root, "--", from, to}, io.Discard)
				if err == nil {
					t.Fatalf("migration-guide -- %q %q succeeded", from, to)
				}
				if word != "" && !strings.Contains(err.Error(), word) {
					t.Errorf("err = %q, want it to name %q", err, word)
				}
				unchanged(t, f.root, before, "the failure")

				// Without "--" a word that looks like an option may be taken
				// for one; it must still fail, and still write nothing.
				if err := run([]string{"-C", f.root, from, to}, io.Discard); err == nil {
					t.Errorf("migration-guide %q %q succeeded", from, to)
				}
				unchanged(t, f.root, before, "the failure")
			})
		}
	}
}

// Git allows a "/" in a tag's name, and the tool would otherwise write the
// guide into a directory the person did not choose.
func TestAC4ATagThatCannotBeAFileNameIsRefusedAndNothingIsWritten(t *testing.T) {
	f := newFixture(t)
	if got := git(t, f.root, "tag", "--list", "rel/1.0"); got != "rel/1.0" {
		t.Fatalf("the fixture has no tag rel/1.0: %q", got)
	}
	for _, c := range []struct{ name, from, to string }{
		{"as from", "rel/1.0", "v1.1.0"},
		{"as to", "v1.0.0", "rel/1.0"},
	} {
		t.Run("AC-4 rel/1.0 "+c.name, func(t *testing.T) {
			before := snapshot(t, f.root)
			err := f.generate(c.from, c.to)
			if err == nil {
				t.Fatalf("migration-guide %s %s succeeded", c.from, c.to)
			}
			if !strings.Contains(err.Error(), "rel/1.0") {
				t.Errorf("err = %q, want it to name rel/1.0", err)
			}
			unchanged(t, f.root, before, "the refusal")
		})
	}
}

func TestAC4ADirectoryThatIsNotARepositoryHasNoTagsAndFails(t *testing.T) {
	root := t.TempDir()
	before := snapshot(t, root)
	err := run([]string{"-C", root, "v1.0.0", "v1.1.0"}, io.Discard)
	if err == nil {
		t.Fatal("AC-4: the tool succeeded where there is no repository")
	}
	if !strings.Contains(err.Error(), "v1.0.0") && !strings.Contains(err.Error(), "v1.1.0") {
		t.Errorf("AC-4: err = %q, want it to name a version", err)
	}
	unchanged(t, root, before, "the failure")
}

func TestAC4FewerThanTwoVersionsFailsAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	for _, args := range [][]string{
		{"-C", f.root},
		{"-C", f.root, "v1.0.0"},
		{"-C", f.root, "--", "v1.0.0"},
	} {
		before := snapshot(t, f.root)
		if err := run(args, io.Discard); err == nil {
			t.Errorf("AC-4: run %v succeeded", args)
		}
		unchanged(t, f.root, before, "the failure")
	}
}
