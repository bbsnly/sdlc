package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"
)

// US-119, added after the first freeze: the tests in main_test.go are right
// and too few. These reach what they left alone: the code span a subject is
// put in, the breaking-change spellings, every error return that can be
// reached for real, and main() itself.

// --- codeSpan ---------------------------------------------------------------

// decodeSpan reads s as CommonMark reads a code span, and says what text the
// span stands for. It is the oracle for codeSpan: an independent reading of the
// fence rules, so a test does not repeat the code it checks. s must be exactly
// one span -- opened by a run of backticks and closed by the first later run of
// the same length, with nothing after it.
func decodeSpan(s string) (string, bool) {
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	if n == 0 {
		return "", false
	}
	for j := n; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		k := j
		for k < len(s) && s[k] == '`' {
			k++
		}
		if k-j == n {
			if k != len(s) {
				return "", false // closed early: something follows the span
			}
			content := s[n:j]
			if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' &&
				strings.Trim(content, " ") != "" {
				content = content[1 : len(content)-1]
			}
			return content, true
		}
		j = k
	}
	return "", false
}

// asOneLine is what a subject becomes in a guide: every control character a
// space.
func asOneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

var spanSubjects = []struct{ name, text string }{
	{"plain text", "fix!: drop the old flag"},
	{"one backtick", "fix!: keep a`b together"},
	{"a run of two backticks", "fix!: keep a``b together"},
	{"a run of three backticks", "fix!: keep a```b together"},
	{"two runs of different lengths", "fix!: a`b``c```d"},
	{"a backtick first", "`go.mod` is read first"},
	{"a backtick last", "fix!: drop the option `-x`"},
	{"backticks at both ends", "`both`"},
	{"a run of backticks at both ends", "``both``"},
	{"a backtick alone", "`"},
	{"a run of backticks alone", "```"},
	{"HTML", "feat!: <b>bold</b> <script>x()</script>"},
	{"emphasis marks", "feat!: *star* _under_ **bold** __bold__"},
	{"a link", "feat!: [x](y) and ![i](j)"},
	{"a heading mark and a list mark", "# not a heading - not a list"},
	{"a carriage return", "fix!: carriage\rreturn"},
	{"a carriage return and a line feed", "fix!: one\r\ntwo"},
	{"a tab", "fix!: tab\there"},
	{"an escape", "fix!: \x1b[31mred"},
	{"a delete character", "fix!: del\x7fete"},
	{"a next-line control", "fix!: next\u0085line"},
	{"a control character beside a backtick", "`\rx\r`"},
}

func TestAC1AnySubjectIsOneClosedCodeSpanThatStandsForTheSubject(t *testing.T) {
	for _, c := range spanSubjects {
		t.Run("AC-1 "+c.name, func(t *testing.T) {
			got := codeSpan(c.text)
			for _, r := range got {
				if unicode.IsControl(r) {
					t.Errorf("codeSpan(%q) = %q, which carries the control character %q", c.text, got, r)
				}
			}
			stands, ok := decodeSpan(got)
			if !ok {
				t.Fatalf("codeSpan(%q) = %q, which is not one closed code span", c.text, got)
			}
			if want := asOneLine(c.text); stands != want {
				t.Errorf("codeSpan(%q) = %q stands for %q, want %q", c.text, got, stands, want)
			}
		})
	}
}

// Markup in a subject is text: it has to sit inside the span untouched, not
// be escaped (a backslash would show in a code span) and not be dropped.
func TestAC1MarkupInASubjectStaysInsideTheSpanAsItWasWritten(t *testing.T) {
	for _, text := range []string{"<b>x</b>", "*x*", "_x_", "[x](y)", "<b>*_[x](y)_*</b>"} {
		t.Run("AC-1 "+text, func(t *testing.T) {
			if got, want := codeSpan(text), "`"+text+"`"; got != want {
				t.Errorf("codeSpan(%q) = %q, want %q", text, got, want)
			}
		})
	}
}

// The fence is longer than any run in the text, so a longer run is met with a
// longer fence; a text with no backtick needs no more than one.
func TestAC1TheFenceIsOneLongerThanTheLongestRunOfBackticksInTheText(t *testing.T) {
	for _, c := range []struct {
		text  string
		fence int
	}{
		{"none", 1},
		{"a`b", 2},
		{"a``b", 3},
		{"a```b", 4},
		{"a`b```c``d", 4},
	} {
		got := codeSpan(c.text)
		n := 0
		for n < len(got) && got[n] == '`' {
			n++
		}
		if n != c.fence {
			t.Errorf("AC-1: codeSpan(%q) = %q opens with %d backticks, want %d", c.text, got, n, c.fence)
		}
		if !strings.HasSuffix(got, strings.Repeat("`", n)) {
			t.Errorf("AC-1: codeSpan(%q) = %q does not close with as many as it opens with", c.text, got)
		}
	}
}

// Text that starts or ends with a backtick would run into the fence; one space
// either side keeps them apart, and a text that does neither gets none.
func TestAC1TextThatStartsOrEndsWithABacktickIsPaddedAndOtherTextIsNot(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"`a", "`` `a ``"},
		{"a`", "`` a` ``"},
		{"`a`", "`` `a` ``"},
		{"a`b", "``a`b``"},
		{"ab", "`ab`"},
	} {
		if got := codeSpan(c.text); got != c.want {
			t.Errorf("AC-1: codeSpan(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestAC1ASubjectWithAControlCharacterStaysOnTheOneHeadingLine(t *testing.T) {
	hash := strings.Repeat("ab", 20)
	for _, subject := range []string{"fix!: carriage\rreturn", "fix!: bell\athere", "fix!: line\nfeed", "fix!: tab\there", "`\r`"} {
		doc := render("v1.0.0", "v2.0.0", []commit{{hash, subject}})
		if strings.ContainsAny(doc, "\r\a\t") {
			t.Errorf("AC-1: the guide for %q carries a control character: %q", subject, doc)
		}
		var heading []string
		for l := range strings.SplitSeq(doc, "\n") {
			if strings.HasPrefix(l, "## ") {
				heading = append(heading, l)
			}
		}
		if len(heading) != 1 {
			t.Fatalf("AC-1: the guide for %q has %d section headings, want 1:\n%s", subject, len(heading), doc)
		}
		if !strings.Contains(heading[0], hash[:7]) {
			t.Errorf("AC-1: the heading %q for %q lost its hash: the subject broke the line", heading[0], subject)
		}
		stands, ok := decodeSpan(strings.TrimSuffix(strings.TrimPrefix(heading[0], "## "), " (`"+hash[:7]+"`)"))
		if !ok || stands != asOneLine(subject) {
			t.Errorf("AC-1: the heading %q stands for %q (closed span %v), want %q", heading[0], stands, ok, asOneLine(subject))
		}
	}
}

// The same, end to end: subjects that git holds, read back out of the guide the
// tool wrote, by the oracle.
func TestAC1SubjectsWithBackticksMarkupAndACarriageReturnComeBackOutOfTheGuideAsWritten(t *testing.T) {
	root := newRepo(t)
	tag(t, root, "r1")
	subjects := []string{
		"fix!: keep `go.mod` in step",
		"fix!: run ``a`b`` twice",
		"fix!: drop the option `-x`",
		"`first` in the subject, a footer after it\n\nBREAKING CHANGE: the footer marks it",
		"feat!: <b>bold</b> *star* _under_ [x](y)",
		"fix!: carriage\rreturn inside",
	}
	var hashes []string
	for _, s := range subjects {
		hashes = append(hashes, commitMessage(t, root, s))
	}
	if got := git(t, root, "log", "-1", "--format=%s", hashes[5]); !strings.Contains(got, "\r") {
		t.Fatalf("the fixture's carriage return did not reach git: %q", got)
	}
	tag(t, root, "r2")
	if err := run([]string{"-C", root, "r1", "r2"}, bytes.NewBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "docs", "migrations", "r1-to-r2.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(body)
	if strings.Contains(doc, "\r") {
		t.Errorf("AC-1: the guide carries a carriage return: %q", doc)
	}
	lines := strings.Split(doc, "\n")
	for i, s := range subjects {
		subject, _, _ := strings.Cut(s, "\n")
		h := hashes[i][:7]
		var found []string
		for _, l := range lines {
			if strings.HasPrefix(l, "## ") && strings.Contains(l, h) {
				found = append(found, l)
			}
		}
		if len(found) != 1 {
			t.Errorf("AC-1: %d heading lines carry the hash of %q, want one:\n%s", len(found), subject, doc)
			continue
		}
		span, cut := strings.CutSuffix(strings.TrimPrefix(found[0], "## "), " (`"+h+"`)")
		stands, ok := decodeSpan(span)
		if !cut || !ok || stands != asOneLine(subject) {
			t.Errorf("AC-1: the heading %q stands for %q (closed span %v), want the subject %q", found[0], stands, ok, asOneLine(subject))
		}
	}
}

// --- breaking detection -----------------------------------------------------

// newRepo is an empty repository with one commit, which tag() can mark.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "config", "user.email", "test@example.com")
	git(t, root, "config", "user.name", "Test")
	commitMessage(t, root, "chore: begin")
	return root
}

func tag(t *testing.T, root, name string) { t.Helper(); git(t, root, "tag", name) }

// commitMessage makes an empty commit with the message just as given and returns its
// hash. The message goes by file, so a carriage return in it reaches git.
func commitMessage(t *testing.T, root, message string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "message")
	if err := os.WriteFile(file, []byte(message+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "-q", "--allow-empty", "-F", file)
	return git(t, root, "rev-parse", "HEAD")
}

func TestAC1TheWaysAMessageIsOrIsNotMarkedBreaking(t *testing.T) {
	root := newRepo(t)
	tag(t, root, "r1")
	prev := "r1"
	for _, c := range []struct {
		name, subject, body string
		breaking            bool
	}{
		{"a BREAKING-CHANGE footer with a hyphen", "fix: footer hyphen", "The explanation.\n\nBREAKING-CHANGE: the hyphen spelling counts", true},
		{"a BREAKING CHANGE footer with a space", "fix: footer space", "The explanation.\n\nBREAKING CHANGE: the space spelling counts", true},
		{"a BREAKING CHANGE line in the first paragraph of the body", "fix: first paragraph", "BREAKING CHANGE: said straight after the subject", true},
		{"a BREAKING-CHANGE line at the start of a later line of a paragraph", "fix: later line", "Some words.\nBREAKING-CHANGE: on its own line", true},
		{"a bang after the type and scope", "fix(cli)!: scoped bang", "", true},
		{"a bang after the type", "fix!: bare bang", "", true},
		{"a bang that is not straight after the type", "fix: drop it!: now", "", false},
		{"a bang in the middle of a subject", "fix: drop it! now", "", false},
		{"a bang after a space", "fix !: spaced bang", "", false},
		{"the words in the middle of a line", "fix: mid-line mention", "This is not a BREAKING CHANGE: in a footer.", false},
		{"the hyphen words in the middle of a line", "fix: mid-line hyphen mention", "This is not a BREAKING-CHANGE: in a footer.", false},
		{"the words as the end of a line", "fix: end of line mention", "Do not write BREAKING CHANGE:\nunless it is true", false},
		{"the words without the colon", "fix: no colon", "BREAKING CHANGE the colon is missing", false},
		{"the words in lower case", "fix: lower case", "breaking change: not the footer token", false},
		{"the words with a letter before them", "fix: glued words", "XBREAKING CHANGE: glued to another word", false},
	} {
		msg := c.subject
		if c.body != "" {
			msg += "\n\n" + c.body
		}
		hash := commitMessage(t, root, msg)
		cur := "t" + hash[:7]
		tag(t, root, cur)
		t.Run("AC-1 "+c.name, func(t *testing.T) {
			found, err := breaking(root, prev, cur)
			if err != nil {
				t.Fatal(err)
			}
			want := []commit(nil)
			if c.breaking {
				want = []commit{{hash, c.subject}}
			}
			if len(found) != len(want) || (len(want) == 1 && found[0] != want[0]) {
				t.Errorf("a commit with subject %q and body %q: breaking() = %v, want %v", c.subject, c.body, found, want)
			}
		})
		prev = cur
	}
}

// --- error returns ----------------------------------------------------------

// A file where docs/migrations should be: the directory cannot be made, and
// the person is told which guide that stopped.
func TestAC3AFileWhereTheMigrationsDirectoryShouldBeIsRefusedNamingTheGuideAndLeftAsItWas(t *testing.T) {
	for _, c := range []struct {
		name, blocker string
		files         map[string]string
	}{
		{"a file called docs", "docs", map[string]string{"docs": "my notes\n"}},
		{"a file called docs/migrations", filepath.Join("docs", "migrations"),
			map[string]string{filepath.Join("docs", "migrations"): "my notes\n"}},
	} {
		t.Run("AC-3 "+c.name, func(t *testing.T) {
			f := newFixture(t)
			for rel, text := range c.files {
				path := filepath.Join(f.root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshot(t, f.root)
			err := f.generate("v1.0.0", "v1.1.0")
			if err == nil {
				t.Fatal("the tool said nothing though the guide could not be written")
			}
			rel := filepath.Join("docs", "migrations", "v1.0.0-to-v1.1.0.md")
			if !strings.Contains(err.Error(), "cannot create the directory for "+rel) {
				t.Errorf("err = %q, want it to say the directory for %s cannot be made", err, rel)
			}
			if got, _ := os.ReadFile(filepath.Join(f.root, c.blocker)); string(got) != "my notes\n" {
				t.Errorf("the file in the way is now %q", got)
			}
			unchanged(t, f.root, before, "the failure")
		})
	}
}

// readOnly makes dir unwritable, or skips where the platform or the user lets
// a write through anyway.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a Windows directory's read-only attribute does not stop a file being created in it")
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	probe := filepath.Join(dir, "probe")
	if f, err := os.Create(probe); err == nil {
		_ = f.Close()
		_ = os.Remove(probe)
		t.Skip("this user can write to a read-only directory (running as root?)")
	}
}

func TestAC3AReadOnlyMigrationsDirectoryIsRefusedNamingTheGuideAndNothingIsWritten(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, "docs", "migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	readOnly(t, dir)
	before := snapshot(t, f.root)
	err := f.generate("v1.0.0", "v1.1.0")
	if err == nil {
		t.Fatal("the tool said nothing though the guide could not be written")
	}
	rel := filepath.Join("docs", "migrations", "v1.0.0-to-v1.1.0.md")
	if !strings.Contains(err.Error(), "cannot write "+rel) {
		t.Errorf("err = %q, want it to say %s cannot be written", err, rel)
	}
	unchanged(t, f.root, before, "the failure")
}

func TestAC3AReadOnlyDocsDirectoryStopsTheMigrationsDirectoryBeingMade(t *testing.T) {
	f := newFixture(t)
	docs := filepath.Join(f.root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	readOnly(t, docs)
	before := snapshot(t, f.root)
	err := f.generate("v1.0.0", "v1.1.0")
	if err == nil {
		t.Fatal("the tool said nothing though the guide could not be written")
	}
	rel := filepath.Join("docs", "migrations", "v1.0.0-to-v1.1.0.md")
	if !strings.Contains(err.Error(), "cannot create the directory for "+rel) {
		t.Errorf("err = %q, want it to say the directory for %s cannot be made", err, rel)
	}
	unchanged(t, f.root, before, "the failure")
}

// The path where the guide goes being a directory is the same refusal as a
// file there: the person's own thing is in the way, and is left alone.
func TestAC3ADirectoryWhereTheGuideShouldBeIsRefusedAndLeftAsItWas(t *testing.T) {
	f := newFixture(t)
	path := f.guidePath("v1.0.0", "v1.1.0")
	if err := os.MkdirAll(filepath.Join(path, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, f.root)
	err := f.generate("v1.0.0", "v1.1.0")
	if err == nil || !strings.Contains(err.Error(), "v1.0.0-to-v1.1.0.md") {
		t.Errorf("AC-3: err = %v, want a refusal naming the guide", err)
	}
	unchanged(t, f.root, before, "the refusal")
}

// Not a repository: git fails inside the directory -C names, whether that is a
// plain directory or one that is not there. Both end as a version that is not
// a tag, with nothing written.
func TestAC4GitFailingInsideTheDirectoryNamedByCNamesTheVersionAndWritesNothing(t *testing.T) {
	plain := t.TempDir()
	missing := filepath.Join(t.TempDir(), "not-there")
	for _, c := range []struct{ name, root string }{
		{"a directory that is not a repository", plain},
		{"a directory that does not exist", missing},
	} {
		t.Run("AC-4 "+c.name, func(t *testing.T) {
			err := run([]string{"-C", c.root, "v1.0.0", "v1.1.0"}, bytes.NewBuffer(nil))
			if err == nil {
				t.Fatal("the tool succeeded where git cannot answer")
			}
			if !strings.Contains(err.Error(), `"v1.0.0" is not a git tag`) {
				t.Errorf("err = %q, want it to name v1.0.0 as not a tag", err)
			}
			if _, serr := os.Stat(filepath.Join(c.root, "docs")); !errors.Is(serr, os.ErrNotExist) {
				t.Errorf("a docs directory exists after the failure: %v", serr)
			}
		})
	}
}

// Both tags are there and name commits, but git cannot read the history
// between them: the tool says git log failed, for that range, and writes
// nothing -- not a guide that says no commit is breaking.
func TestAC4GitFailingToReadTheHistoryBetweenTwoTagsSaysSoAndWritesNothing(t *testing.T) {
	root := newRepo(t)
	tag(t, root, "r1")
	middle := commitMessage(t, root, "fix!: a breaking commit")
	commitMessage(t, root, "fix!: another breaking commit")
	tag(t, root, "r2")
	// The commit in the middle of the range is gone from the object store, as
	// after a damaged clone: both tags still resolve, the walk between them
	// cannot.
	object := filepath.Join(root, ".git", "objects", middle[:2], middle[2:])
	if err := os.Chmod(object, 0o644); err != nil {
		t.Fatalf("the commit is not a loose object, so the fixture cannot damage it: %v", err)
	}
	if err := os.Remove(object); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)
	err := run([]string{"-C", root, "r1", "r2"}, bytes.NewBuffer(nil))
	if err == nil {
		t.Fatal("AC-4: the tool wrote a guide for a range git cannot read")
	}
	if !strings.Contains(err.Error(), "git log r1..r2 failed") {
		t.Errorf("AC-4: err = %q, want it to say that git log r1..r2 failed", err)
	}
	unchanged(t, root, before, "the failure")
}

func TestAC4AnOptionTheToolDoesNotHaveFailsWithTheUsageAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	before := snapshot(t, f.root)
	err := run([]string{"-C", f.root, "-no-such-option", "v1.0.0", "v1.1.0"}, bytes.NewBuffer(nil))
	if err == nil {
		t.Fatal("AC-4: the tool succeeded with an option it does not have")
	}
	if !strings.Contains(err.Error(), "usage: ./task migration-guide -- <from-tag> <to-tag>") {
		t.Errorf("AC-4: err = %q, want it to say how to run the task", err)
	}
	unchanged(t, f.root, before, "the failure")
}

func TestAC4TooFewVersionsSaysHowToRunTheTask(t *testing.T) {
	f := newFixture(t)
	err := run([]string{"-C", f.root, "v1.0.0"}, bytes.NewBuffer(nil))
	if err == nil || !strings.Contains(err.Error(), "usage: ./task migration-guide -- <from-tag> <to-tag>") {
		t.Errorf("AC-4: err = %v, want the usage", err)
	}
}

// failingWriter is a stdout that cannot be written to, as a closed pipe is.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("the pipe is closed") }

// The guide is on disk by then and the person is told that the message about
// it could not be given, rather than being told nothing.
func TestAC1AStdoutThatCannotBeWrittenToIsAnErrorAfterTheGuideIsWritten(t *testing.T) {
	f := newFixture(t)
	err := run([]string{"-C", f.root, "v1.0.0", "v1.1.0"}, failingWriter{})
	if err == nil || !strings.Contains(err.Error(), "the pipe is closed") {
		t.Errorf("AC-1: err = %v, want the writer's error", err)
	}
	if _, serr := os.Stat(f.guidePath("v1.0.0", "v1.1.0")); serr != nil {
		t.Errorf("AC-1: the guide is not there: %v", serr)
	}
}

func TestAC1TheToolSaysWhichFileItWroteOnStdout(t *testing.T) {
	f := newFixture(t)
	var out bytes.Buffer
	if err := run([]string{"-C", f.root, "v1.0.0", "v1.1.0"}, &out); err != nil {
		t.Fatal(err)
	}
	want := "wrote " + filepath.Join("docs", "migrations", "v1.0.0-to-v1.1.0.md") + "\n"
	if out.String() != want {
		t.Errorf("AC-1: stdout = %q, want %q", out.String(), want)
	}
}

// --- main() -----------------------------------------------------------------

// runMain runs the real main() as a subprocess, in dir, and reports what it
// wrote and how it exited. main ends the process at its first os.Exit, so it
// cannot be called from a test; TestMain re-executes this binary as the tool.
func runMain(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), migrationguideHelperEnv+"=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		exitCode = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running migrationguide: %v", err)
	}
	return out.String(), errb.String(), exitCode
}

func TestAC1MainWritesTheGuideInTheDirectoryItIsRunInWhenNoRootIsGiven(t *testing.T) {
	f := newFixture(t)
	out, errb, code := runMain(t, f.root, "v1.0.0", "v1.1.0")
	if code != 0 {
		t.Fatalf("AC-1: exit %d, stderr=%q", code, errb)
	}
	rel := filepath.Join("docs", "migrations", "v1.0.0-to-v1.1.0.md")
	if want := "wrote " + rel + "\n"; out != want {
		t.Errorf("AC-1: stdout = %q, want %q", out, want)
	}
	if errb != "" {
		t.Errorf("AC-1: a run that worked said %q on stderr", errb)
	}
	body, err := os.ReadFile(filepath.Join(f.root, rel))
	if err != nil {
		t.Fatalf("AC-1: nothing was written in the directory main was run in: %v", err)
	}
	assertGuide(t, f, string(body), "v1.0.0", "v1.1.0", []string{"flag", "config"}, without(allKeys, []string{"flag", "config"}))
}

func TestAC1MainTakesTheRepositoryFromDashCAndWritesThereWhateverItIsRunIn(t *testing.T) {
	f := newFixture(t)
	elsewhere := t.TempDir()
	out, errb, code := runMain(t, elsewhere, "-C", f.root, "v1.0.0", "v1.1.0")
	if code != 0 {
		t.Fatalf("AC-1: exit %d, stderr=%q", code, errb)
	}
	rel := filepath.Join("docs", "migrations", "v1.0.0-to-v1.1.0.md")
	if want := "wrote " + rel + "\n"; out != want {
		t.Errorf("AC-1: stdout = %q, want %q", out, want)
	}
	if _, err := os.Stat(filepath.Join(f.root, rel)); err != nil {
		t.Errorf("AC-1: the guide is not in the repository named by -C: %v", err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "docs")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("AC-1: something was written where main was run, not where -C pointed: %v", err)
	}
}

func TestAC3MainRefusesAFileThatExistsExitsOneAndNamesTheFileOnStderr(t *testing.T) {
	f := newFixture(t)
	if _, _, code := runMain(t, f.root, "v1.0.0", "v1.1.0"); code != 0 {
		t.Fatalf("the first run exited %d", code)
	}
	path := f.guidePath("v1.0.0", "v1.1.0")
	completed := "the person's text\n"
	if err := os.WriteFile(path, []byte(completed), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errb, code := runMain(t, f.root, "v1.0.0", "v1.1.0")
	if code != 1 {
		t.Errorf("AC-3: exit %d, want 1", code)
	}
	if out != "" {
		t.Errorf("AC-3: a refusal said %q on stdout", out)
	}
	if want := "migration-guide: " + filepath.Join("docs", "migrations", "v1.0.0-to-v1.1.0.md") + " already exists"; !strings.HasPrefix(errb, want) {
		t.Errorf("AC-3: stderr = %q, want it to start %q", errb, want)
	}
	if got, _ := os.ReadFile(path); string(got) != completed {
		t.Errorf("AC-3: the person's text is now %q", got)
	}
}

func TestAC4MainNamesAVersionThatIsNotATagOnStderrExitsOneAndWritesNothing(t *testing.T) {
	f := newFixture(t)
	before := snapshot(t, f.root)
	out, errb, code := runMain(t, f.root, "v1.0.0", "v9.9.9")
	if code != 1 {
		t.Errorf("AC-4: exit %d, want 1", code)
	}
	if out != "" {
		t.Errorf("AC-4: a failure said %q on stdout", out)
	}
	if !strings.HasPrefix(errb, "migration-guide: ") || !strings.Contains(errb, `"v9.9.9" is not a git tag`) {
		t.Errorf("AC-4: stderr = %q, want it to start with the tool's name and name v9.9.9", errb)
	}
	unchanged(t, f.root, before, "the failure")
}

func TestAC4MainRunInADirectoryThatIsNotARepositoryExitsOneAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	out, errb, code := runMain(t, dir, "v1.0.0", "v1.1.0")
	if code != 1 || out != "" {
		t.Errorf("AC-4: exit %d, stdout %q, want exit 1 and nothing on stdout", code, out)
	}
	if !strings.Contains(errb, `"v1.0.0" is not a git tag`) {
		t.Errorf("AC-4: stderr = %q, want it to name v1.0.0", errb)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("AC-4: a docs directory exists after the failure: %v", err)
	}
}

func TestAC4MainWithTooFewVersionsExitsOneAndSaysHowToRunTheTask(t *testing.T) {
	f := newFixture(t)
	out, errb, code := runMain(t, f.root, "v1.0.0")
	if code != 1 || out != "" {
		t.Errorf("AC-4: exit %d, stdout %q, want exit 1 and nothing on stdout", code, out)
	}
	if !strings.HasPrefix(errb, "migration-guide: usage: ./task migration-guide -- <from-tag> <to-tag>") {
		t.Errorf("AC-4: stderr = %q", errb)
	}
}
