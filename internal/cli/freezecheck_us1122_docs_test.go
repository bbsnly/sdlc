package cli

// US-112.2 AC-6, the words: the pages, the runbook and the test author's own
// instructions say what the freeze check is and who acts on its refusal. These
// hold the meaning and not a sentence, in the passage that is about it, so a
// page may be reworded as it likes as long as it still says so.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fdFence is one code fence of a page: the word after its opening and its body.
type fdFence struct{ lang, body string }

func fdFences(doc string) []fdFence {
	var out []fdFence
	var current *fdFence
	for line := range strings.SplitSeq(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !strings.HasPrefix(trimmed, "```"):
			if current != nil {
				current.body += line + "\n"
			}
		case current == nil:
			current = &fdFence{lang: strings.TrimPrefix(trimmed, "```")}
		default:
			out = append(out, *current)
			current = nil
		}
	}
	return out
}

// fdExample is the example for Go on the configuration page: the fence that hands
// the list to gofmt. Where it is the command as the configuration holds it, in
// JSON, it comes back as the shell reads it.
func fdExample(t *testing.T) string {
	t.Helper()
	for _, f := range fdFences(us1121Source(t, "docs/configuration.md")) {
		if !strings.Contains(f.body, "SDLC_FREEZE_FILES") || !strings.Contains(f.body, "gofmt") {
			continue
		}
		if f.lang != "json" {
			return f.body
		}
		var doc struct {
			Commands map[string]string `json:"commands"`
			Check    string            `json:"freeze_check"`
		}
		text := strings.TrimRight(strings.TrimSpace(f.body), ",")
		if !strings.HasPrefix(text, "{") {
			text = "{" + text + "}"
		}
		if err := json.Unmarshal([]byte(text), &doc); err != nil {
			t.Fatalf("the example is not JSON: %v\n%s", err, f.body)
		}
		if doc.Check != "" {
			return doc.Check
		}
		return doc.Commands["freeze_check"]
	}
	t.Fatal("docs/configuration.md has no code fence that hands SDLC_FREEZE_FILES to gofmt")
	return ""
}

func TestAC6TheConfigurationPageDescribesTheFreezeCheckAndWhatItCosts(t *testing.T) {
	t.Parallel()
	doc := us1121Source(t, "docs/configuration.md")
	commands := us1121Section(t, doc, "## `commands`")
	if !regexp.MustCompile("(?m)^\\| `freeze_check` \\|").MatchString(commands) {
		t.Error("the commands table has no row for freeze_check")
	}
	flat := fcFlat(doc)
	if !strings.Contains(flat, "sdlc_freeze_files") {
		t.Error("the page does not name SDLC_FREEZE_FILES")
	}
	if !regexp.MustCompile(`\b(one run|runs? once|once)\b[^.]{0,40}\bfreeze\b`).MatchString(flat) {
		t.Error("the page does not say what the check costs: one run per freeze")
	}
	if trust := us1121Section(t, doc, "### Trusting the commands"); !strings.Contains(trust, "freeze_check") {
		t.Errorf("the warning about the commands sdlc runs itself does not list freeze_check:\n%s", trust)
	}
}

// The example copes with a test file naming code not yet written, which is what
// golangci-lint stops at: decision 9.
func TestAC6Decision9TheGoExampleCopesWithTestsNamingCodeNotYetWritten(t *testing.T) {
	t.Parallel()
	example := fdExample(t)
	for _, want := range []string{
		"gofmt", "gitleaks", "--redact", "--no-color", "golangci-lint", "go vet", "read -r",
	} {
		if !strings.Contains(example, want) {
			t.Errorf("the example does not use %q:\n%s", want, example)
		}
	}
	// In place of no-internal-docs, which takes no list and reads tracked files.
	if !strings.Contains(example, "grep") || !strings.Contains(example, "Users") {
		t.Errorf("the example does not look through the list for account paths:\n%s", example)
	}
	// THREATS 3: names are data. A name can hold $() or a leading dash.
	if regexp.MustCompile(`\beval\b`).MatchString(example) {
		t.Errorf("the example evaluates what it was handed:\n%s", example)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no shell to read the example with: %v", err)
	}
	if out, err := exec.CommandContext(t.Context(), sh, "-n", "-c", example).CombinedOutput(); err != nil {
		t.Errorf("the example is not a shell command: %v\n%s\n%s", err, out, example)
	}

	saysUnlinted := false
	for _, paragraph := range us1121Paragraphs(us1121Source(t, "docs/configuration.md")) {
		flat := fcFlat(paragraph)
		if strings.Contains(flat, "lint") && regexp.MustCompile(`(do|does) not compile|non-compiling|not compiling`).MatchString(flat) {
			saysUnlinted = true
		}
	}
	if !saysUnlinted {
		t.Error("the page does not say that tests in a package that does not compile go unlinted at the freeze")
	}
}

// fdNamedCommand is what the page says to put in the configuration, with the
// example saved where the page saves it.
func fdNamedCommand(t *testing.T) string {
	t.Helper()
	for _, f := range fdFences(us1121Source(t, "docs/configuration.md")) {
		if strings.Contains(f.body, "scripts/freeze-check.sh") && f.lang == "json" {
			var doc struct {
				Check string `json:"freeze_check"`
			}
			if err := json.Unmarshal([]byte("{"+f.body+"}"), &doc); err != nil {
				t.Fatalf("the page's command is not JSON: %v\n%s", err, f.body)
			}
			return doc.Check
		}
	}
	t.Fatal("docs/configuration.md does not say how to name scripts/freeze-check.sh")
	return ""
}

// fdNeeds skips where a tool the example calls is not on PATH: without it the
// example refuses every tree, which says nothing of the example.
func fdNeeds(t *testing.T, tools ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the example is a POSIX script, and nothing has run it under Git Bash")
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("the example calls %s, which is not on PATH here: %v", tool, err)
		}
	}
}

// fdGoModule is what each case starts from: a package that compiles, and a test
// of it that lints clean, none of which the check is handed until it changes.
func fdGoModule() map[string]string {
	return map[string]string{
		".gitignore":           "/.sdlc/state/\n",
		"go.mod":               "module example.com/fc\n\ngo 1.26\n",
		"internal/a/a.go":      "// Package a is what the tests are of.\npackage a\n\n// One is one.\nfunc One() int { return 1 }\n",
		"internal/a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fail()\n\t}\n}\n",
	}
}

// fdTestFile is a test file of package a holding body as a test function's.
func fdTestFile(imports, body string) string {
	return "package a\n\nimport (\n" + imports + "\t\"testing\"\n)\n\nfunc TestTwo(t *testing.T) {\n" + body + "}\n"
}

// fdOwnCache is the settings that give a process a home, a cache and a temp
// directory of the test's own. The linter keeps its cache under the first two, and
// the run's own home is held empty: testenv fails the package over what is left
// there. It keeps a lock in the third, and refuses with "parallel golangci-lint is
// running" while any other test, or a person's `./task lint`, holds that.
func fdOwnCache(t *testing.T) []string {
	t.Helper()
	home := wtTempDir(t)
	return []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "TMPDIR=" + home}
}

// fdRunExample puts the example under freeze with the files given as the test
// author's work, and returns what the freeze said. The check writes nothing into
// the tree, as the page promises, whatever it decides.
func fdRunExample(t *testing.T, root string, files map[string]string) result {
	t.Helper()
	writeFile(t, root, "scripts/freeze-check.sh", fdExample(t))
	command := fdNamedCommand(t)
	for _, setting := range fdOwnCache(t) {
		name, value, _ := strings.Cut(setting, "=")
		command = name + "=" + suSh(value) + " " + command
	}
	fcSet(t, root, command)
	for rel, body := range files {
		writeFile(t, root, rel, body)
	}
	tree := func() []string {
		var lines []string
		for line := range strings.SplitSeq(wtGit(t, root, "status", "--porcelain", "--untracked-files=all"), "\n") {
			if !strings.Contains(line, ".sdlc/") {
				lines = append(lines, line)
			}
		}
		return lines
	}
	before := tree()
	r := run(t, "freeze")
	if after := tree(); !slices.Equal(after, before) {
		t.Errorf("the example changed the tree:\n%q\nwas\n%q", after, before)
	}
	return r
}

// The page's whole example under real tools, without a stand-in for any of them.
// A word row can hold what the page says and a parse can hold that it is shell;
// neither can hold that a test naming code not yet written passes and a secret
// does not. The cases that reach golangci-lint have a row of their own, because
// the linter has a further way to be unusable here.
func TestAC6Decision9TheDocumentedExampleJudgesATreeAsThePageSays(t *testing.T) {
	t.Parallel()
	// Built here, because `./task secrets` and `./task no-internal-docs` read this
	// file: a real token and a real account path are what they refuse.
	token := "ghp_" + "x7Kq2Vb9Lm4Zt1Rw8Yp3Nc6Hd5Sg0Jf2Ae7U"
	account := func(prefix string) string { return prefix + "alice/notes" }

	refused := func(said ...string) func(*testing.T, string, result) {
		return func(t *testing.T, root string, r result) {
			t.Helper()
			if !strings.Contains(r.stderr, fcCode) {
				t.Errorf("exit %d, want the refusal %s:\n%s", r.code, fcCode, r.stderr)
			}
			fcUnfrozen(t, root)
			for _, want := range said {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
				}
			}
		}
	}
	taken := func(t *testing.T, root string, r result) {
		t.Helper()
		if r.code != 0 || !wtExists(fcLockPath(root)) {
			t.Errorf("exit %d, want the freeze taken:\n%s%s", r.code, r.stdout, r.stderr)
		}
	}

	for name, c := range map[string]struct {
		files  map[string]string
		judged func(*testing.T, string, result)
	}{
		"AC-6 a test naming a function not yet written": {
			map[string]string{"internal/a/two_test.go": fdTestFile("", "\tTwo()\n")}, taken,
		},
		"AC-6 a test importing a package not yet written": {
			map[string]string{"internal/a/two_test.go": fdTestFile("\t\"example.com/fc/internal/notyet\"\n", "\tnotyet.Two()\n")}, taken,
		},
		"AC-6 a directory holding only a test": {
			map[string]string{"internal/b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { Unwritten() }\n"}, taken,
		},
		"AC-6 a file that is not formatted": {
			map[string]string{"internal/a/two_test.go": "package a\nfunc  two( ) {}\n"},
			refused("gofmt: ./internal/a/two_test.go"),
		},
		"AC-6 a file that does not parse": {
			map[string]string{"internal/a/two_test.go": "package a\n\nfunc two( {\n"},
			refused("./internal/a/two_test.go"),
		},
		"AC-6 a finding of go vet": {
			map[string]string{"internal/a/two_test.go": fdTestFile("\t\"fmt\"\n", "\tt.Log(fmt.Sprintf(\"%d\", \"not a number\"))\n")},
			refused("two_test.go"),
		},
		"AC-6 a secret, which stays redacted": {
			map[string]string{"testdata/creds.txt": "token = " + token + "\n"},
			func(t *testing.T, root string, r result) {
				t.Helper()
				refused("REDACTED")(t, root, r)
				if strings.Contains(r.stderr, token) {
					t.Errorf("the refusal prints the secret:\n%s", r.stderr)
				}
			},
		},
		"AC-6 an account path as macOS spells it": {
			map[string]string{"testdata/notes.txt": "see " + account("/Users/") + "\n"},
			refused("account paths in ./testdata/notes.txt"),
		},
		"AC-6 an account path as Linux spells it, beside the one allowed": {
			map[string]string{"testdata/notes.txt": "/home/dev/x and " + account("/home/") + "\n"},
			refused("account paths in ./testdata/notes.txt"),
		},
		"AC-6 an account path as Git Bash spells it": {
			map[string]string{"testdata/notes.txt": "see " + account("/c/Users/") + "\n"},
			refused("account paths in ./testdata/notes.txt"),
		},
		"AC-6 an account path as Windows spells it": {
			map[string]string{"testdata/notes.txt": "see " + account(`C:\Users\`) + "\n"},
			refused("account paths in ./testdata/notes.txt"),
		},
		"AC-6 the one account the path rule allows": {
			map[string]string{"testdata/notes.txt": "see /home/dev/x\n"}, taken,
		},
		"AC-6 a name that holds a command": {
			map[string]string{"testdata/$(touch pwned).txt": "fine\n", "testdata/-leading.txt": "fine\n", "testdata/a name.txt": "fine\n"},
			func(t *testing.T, root string, r result) {
				t.Helper()
				taken(t, root, r)
				if wtExists(filepath.Join(root, "pwned")) {
					t.Error("a file name was run as a command")
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fdNeeds(t, "go", "gofmt", "gitleaks")
			root := fcStarted(t, fdGoModule())
			c.judged(t, root, fdRunExample(t, root, c.files))
		})
	}
}

// fdNeedsALinter skips unless the golangci-lint on PATH can lint this Go at all.
// One too old for it refuses every tree with "Running error", which the page says
// goes to a person: that is the linter's state, and nothing about the example.
func fdNeedsALinter(t *testing.T) {
	t.Helper()
	fdNeeds(t, "go", "gofmt", "gitleaks", "golangci-lint")
	dir := wtTempDir(t)
	writeFile(t, dir, "go.mod", "module example.com/probe\n\ngo 1.26\n")
	writeFile(t, dir, "probe.go", "// Package probe has nothing for a linter to find.\npackage probe\n")
	probe := exec.CommandContext(t.Context(), "golangci-lint", "run", "./...")
	probe.Dir = dir
	probe.Env = append(os.Environ(), fdOwnCache(t)...)
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("the golangci-lint on PATH cannot lint an empty package with this Go: %v\n%s", err, out)
	}
}

// What the example asks of golangci-lint: a clean tree passes through it, and what
// it finds refuses the freeze. The space in the name is the page's "as a whole
// name": a word split there would hand go and the linter a directory that is not one.
func TestAC6Decision9TheDocumentedExampleLintsThePackagesThatCompile(t *testing.T) {
	t.Parallel()
	t.Run("AC-6 a clean tree, with a space in a name", func(t *testing.T) {
		t.Parallel()
		fdNeedsALinter(t)
		root := fcStarted(t, fdGoModule())
		r := fdRunExample(t, root, map[string]string{
			"internal/a/two more_test.go": fdTestFile("", "\tif One() == 2 {\n\t\tt.Fail()\n\t}\n"),
		})
		if r.code != 0 || !wtExists(fcLockPath(root)) {
			t.Errorf("exit %d, want the freeze taken:\n%s%s", r.code, r.stdout, r.stderr)
		}
	})
	t.Run("AC-6 a finding of the linter", func(t *testing.T) {
		t.Parallel()
		fdNeedsALinter(t)
		root := fcStarted(t, fdGoModule())
		r := fdRunExample(t, root, map[string]string{
			"internal/a/two_test.go": fdTestFile("\t\"os\"\n", "\tos.Remove(\"nothing\")\n"),
		})
		if !strings.Contains(r.stderr, fcCode) || !strings.Contains(r.stderr, "two_test.go") || !strings.Contains(r.stderr, "errcheck") {
			t.Errorf("exit %d, want the refusal naming the file and the linter's finding:\n%s", r.code, r.stderr)
		}
		fcUnfrozen(t, root)
	})
}

// fdWithoutGitleaks is PATH holding, by link as fcShellless does, what the script
// calls but gitleaks, which is the one tool it calls for every name. Only a test
// Go runs by itself may change PATH.
func fdWithoutGitleaks(t *testing.T) {
	t.Helper()
	bin := wtTempDir(t)
	for _, tool := range []string{"sh", "git", "mktemp", "rm", "grep", "cat", "dirname", "sort"} {
		found, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("the example calls %s, which is not on PATH here: %v", tool, err)
		}
		if err := os.Symlink(found, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

// A list with nothing in it has no work for any tool, so this runs wherever there
// is a POSIX shell. Printing an empty list leaves one empty line to read, and an
// example that took it for a name would scan the whole tree, or fail for want of
// the scanner, with nothing new to judge.
func TestAC6Decision9TheDocumentedExamplePassesAnEmptyListWithNoToolAtAll(t *testing.T) {
	fdNeeds(t)
	root := fcStarted(t, map[string]string{".gitignore": "/.sdlc/state/\n", "old_test.go": fcTest})
	fdWithoutGitleaks(t)
	r := fdRunExample(t, root, nil)
	if r.code != 0 || !wtExists(fcLockPath(root)) {
		t.Errorf("exit %d, want the freeze taken:\n%s%s", r.code, r.stdout, r.stderr)
	}
}

// With a tool the example needs not there, the check fails and the freeze is
// refused: a scan that could not run is not a scan that passed.
func TestAC6Decision9TheDocumentedExampleRefusesWithAToolItNeedsMissing(t *testing.T) {
	fdNeeds(t)
	root := fcStarted(t, map[string]string{".gitignore": "/.sdlc/state/\n", "old_test.go": fcTest})
	fdWithoutGitleaks(t)
	r := fdRunExample(t, root, map[string]string{"testdata/notes.txt": "notes\n"})
	if !strings.Contains(r.stderr, fcCode) || !strings.Contains(r.stderr, "gitleaks") {
		t.Errorf("exit %d, want the refusal naming the missing program:\n%s", r.code, r.stderr)
	}
	fcUnfrozen(t, root)
}

func TestAC6TheCommandsPageSaysWhatTheRefusalMeansAndDoctorsLine(t *testing.T) {
	t.Parallel()
	doc := us1121Source(t, "docs/commands.md")
	freeze := us1121Section(t, doc, "## `sdlc freeze`")
	for _, want := range []string{"commands.freeze_check", "troubleshooting.md#sdlc-e0085", "SDLC_FREEZE_FILES"} {
		if !strings.Contains(freeze, want) {
			t.Errorf("the section on sdlc freeze does not say %q:\n%s", want, freeze)
		}
	}
	if flat := fcFlat(freeze); !strings.Contains(flat, "test author") || !strings.Contains(flat, "refuse") {
		t.Errorf("the section does not say the test author acts on the refusal:\n%s", freeze)
	}
	if doctor := us1121Section(t, doc, "## `sdlc doctor`"); !strings.Contains(fcFlat(doctor), "freeze check") {
		t.Errorf("the section on sdlc doctor does not mention the freeze check:\n%s", doctor)
	}
}

// docs/ reproduces command output, and these two guides quote doctor's whole
// report: the new line goes in them where doctor prints it. Decision 4.
func TestAC5TheGuidesThatQuoteDoctorsReportShowTheFreezeCheckLine(t *testing.T) {
	t.Parallel()
	line := regexp.MustCompile(`(?m)^ {2}ok {7}freeze check {9}commands\.freeze_check is `)
	for _, guide := range []string{"docs/guides/adding-to-an-existing-project.md", "docs/guides/why-was-i-refused.md"} {
		quoted := false
		for _, f := range fdFences(us1121Source(t, guide)) {
			at := strings.Index(f.body, "sdlc on PATH")
			if at < 0 {
				continue
			}
			quoted = true
			if found := line.FindStringIndex(f.body); found == nil || found[0] > at {
				t.Errorf("%s quotes doctor's report without the freeze check line above sdlc on PATH:\n%s", guide, f.body)
			}
		}
		if !quoted {
			t.Errorf("%s no longer quotes doctor's report, so this holds nothing", guide)
		}
	}
}

// Decision 12: one heading, covering all three states.
func TestAC6Decision12TheTroubleshootingHeadingCoversEveryStateOfTheRefusal(t *testing.T) {
	t.Parallel()
	flat := fcFlat(us1121Section(t, us1121Source(t, "docs/troubleshooting.md"), "### SDLC-E0085"))
	for _, want := range []string{
		"commands.freeze_check", "sdlc freeze", "test author", "person",
		"40", "changed", "shell", "rename",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the section on SDLC-E0085 does not say %q:\n%s", want, flat)
		}
	}
}

// fdSaysRunsTheCheck is whether some paragraph of text has the freeze check run
// before the work is handed back.
func fdSaysRunsTheCheck(text string) bool {
	for _, paragraph := range us1121Paragraphs(text) {
		flat := fcFlat(paragraph)
		if !strings.Contains(flat, "freeze check") && !strings.Contains(flat, "freeze_check") {
			continue
		}
		if regexp.MustCompile(`\bhand(s|ing|ed)?\b[^.]{0,20}\bback\b|\bbefore\b`).MatchString(flat) {
			return true
		}
	}
	return false
}

func TestAC6TheRunbookAndTheTestAuthorSayTheCheckRunsBeforeHandingBack(t *testing.T) {
	t.Parallel()
	gate3 := us1121Section(t, us1121Source(t, "plugin/skills/next/SKILL.md"), "## Gate 3")
	if !fdSaysRunsTheCheck(gate3) {
		t.Errorf("Gate 3 of the runbook does not say the test author runs the freeze check before handing back:\n%s", gate3)
	}
	if !strings.Contains(fcFlat(gate3), "test author") {
		t.Errorf("Gate 3 of the runbook does not say who runs it:\n%s", gate3)
	}
	sdet := us1121Source(t, "plugin/agents/sdet.md")
	if !fdSaysRunsTheCheck(sdet) {
		t.Errorf("the test author's instructions do not say to run the freeze check before handing back:\n%s", sdet)
	}
}
