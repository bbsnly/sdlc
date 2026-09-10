package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storyUnderway is a project with an iteration running, which is the only state
// in which a gate has a document to store.
func storyUnderway(t *testing.T) string {
	t.Helper()
	root := project(t)
	mustRun(t, "init")
	mustRun(t, "start")
	return root
}

func stored(t *testing.T, root, file string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", file))
	if err != nil {
		t.Fatalf("%s is not on disk: %v", file, err)
	}
	return string(body)
}

func TestADocumentArrivesOnStandardInputAndLandsWhereTheNextGateLooks(t *testing.T) {
	root := storyUnderway(t)

	const document = "# Analysis\n\nThe greeting is read from the environment."
	got := decode[artifactPayload](t, runWith(t, document, "artifact", "write", "analysis", "--json"))

	if got.Story != "US-001" || got.Name != "analysis" {
		t.Errorf("payload = %+v", got)
	}
	if want := ".sdlc/stories/US-001/ANALYSIS.md"; got.Path != want {
		t.Errorf("path = %q, want %q", got.Path, want)
	}
	// A document without a final newline is a document that breaks the next
	// diff. One is added; nothing else is touched.
	if body := stored(t, root, "ANALYSIS.md"); body != document+"\n" {
		t.Errorf("stored:\n%q", body)
	}
}

func TestADocumentCanComeFromAFileInstead(t *testing.T) {
	root := storyUnderway(t)
	writeFile(t, root, "notes/threats.md", "# Threats\n\nNone crossed.\n")

	mustRun(t, "artifact", "write", "threats", "--file", filepath.Join(root, "notes", "threats.md"))
	if body := stored(t, root, "THREATS.md"); !strings.Contains(body, "None crossed") {
		t.Errorf("stored:\n%q", body)
	}
}

// The record is how a later gate knows the document exists without trusting a
// summary in a conversation it cannot see.
func TestStoringADocumentIsRecorded(t *testing.T) {
	root := storyUnderway(t)
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")

	body, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"artifact"`, "analysis stored at", "ANALYSIS.md"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the record is missing %q:\n%s", want, body)
		}
	}
}

func TestWhatCannotBeStoredSaysWhyAndWhatCan(t *testing.T) {
	for _, c := range []struct {
		name  string
		stdin string
		args  []string
		code  string
		says  []string
	}{
		{
			name: "a name this version does not know", stdin: "x\n",
			args: []string{"artifact", "write", "postmortem"},
			code: "SDLC-E0016", says: []string{"postmortem", "analysis", "threats"},
		},
		{
			name: "nothing on standard input", stdin: "   \n\t\n",
			args: []string{"artifact", "write", "analysis"},
			code: "SDLC-E0017", says: []string{"standard input"},
		},
		{
			name: "a file that is not there", stdin: "",
			args: []string{"artifact", "write", "analysis", "--file", "nowhere.md"},
			code: "SDLC-E0018", says: []string{"nowhere.md"},
		},
		{
			name: "more than a person would read", stdin: strings.Repeat("x", (1<<20)+1),
			args: []string{"artifact", "write", "analysis"},
			code: "SDLC-E0019", says: []string{"1 MiB"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			storyUnderway(t)
			r := runWith(t, c.stdin, c.args...)
			if r.code == 0 {
				t.Fatalf("it was stored anyway:\n%s", r.stdout)
			}
			for _, want := range append(c.says, c.code) {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
				}
			}
		})
	}
}

func TestStoringADocumentWithNoIterationSaysWhatToDo(t *testing.T) {
	project(t)
	mustRun(t, "init")

	r := runWith(t, "# Analysis\n", "artifact", "write", "analysis")
	if r.code == 0 {
		t.Fatal("a document was stored with no story to store it against")
	}
	for _, want := range []string{"SDLC-E0011", "sdlc start"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}
}

// A gate can be re-run, and re-running it must leave one document rather than
// two, because the next gate reads the file and not the history.
func TestStoringADocumentTwiceReplacesIt(t *testing.T) {
	root := storyUnderway(t)
	mustRunWith(t, "first\n", "artifact", "write", "analysis")
	mustRunWith(t, "second\n", "artifact", "write", "analysis")

	if body := stored(t, root, "ANALYSIS.md"); body != "second\n" {
		t.Errorf("stored:\n%q", body)
	}
}

// The list is what an agent reads when it does not know the name, so it has to
// name the file and the gate as well.
func TestListNamesEveryDocumentAndWhereItBelongs(t *testing.T) {
	project(t)
	out := mustRun(t, "artifact", "list").stdout
	for _, want := range []string{"analysis", "ANALYSIS.md", "threats", "THREATS.md", "researcher"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list is missing %q:\n%s", want, out)
		}
	}
}

// The tagline of this tool is that a model cannot talk its way past a gate.
// This is where that is true or it is not: the gates after Gate 2 read the
// documents, not the summary that said they exist.
func TestAGateCannotPassUntilItsDocumentsAreStored(t *testing.T) {
	storyUnderway(t)

	r := run(t, "gate", "analysis", "pass", "--note", "looks fine to me")
	if r.code == 0 {
		t.Fatal("the analysis gate passed with nothing to show for it")
	}
	for _, want := range []string{"SDLC-E0020", "analysis", "threats", "ANALYSIS.md"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	// One of the two is not enough: the error names what is still missing.
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	r = run(t, "gate", "analysis", "pass")
	if r.code == 0 {
		t.Fatal("the gate passed with half its documents")
	}
	if strings.Contains(r.stderr, "ANALYSIS.md") || !strings.Contains(r.stderr, "THREATS.md") {
		t.Errorf("the refusal does not name what is actually missing:\n%s", r.stderr)
	}

	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	mustRun(t, "gate", "analysis", "pass", "--note", "no trust boundary crossed")
}

// A gate can fail because the work could not be done, and the loop has to be
// able to say so. Holding a failure to the same rule would leave no way to
// record the thing the rule exists to catch.
func TestAGateCanStillFailWithNothingStored(t *testing.T) {
	storyUnderway(t)
	mustRun(t, "gate", "analysis", "fail", "--note", "7 open questions the spec does not settle")
	mustRun(t, "gate", "analysis", "pending")
}

// Most gates produce no documents, and the rule must not invent one for them.
func TestAGateWithNoDocumentsPassesOnItsOwn(t *testing.T) {
	storyUnderway(t)
	mustRun(t, "gate", "dor", "pass", "--note", "criteria are testable")
}
