package shellpolicy

// US-129 AC-7, the half that holds the classifier still while its
// allocations are cut: Inspect answers every command of the existing test
// corpus as it did at the story's base, decision and reasons both, in each
// session the corpus asks in (corpusState) and from both shell tools. The
// corpus is every string literal of this package's test files at 37e3ac8:
// commands, and words and paths that are read as commands here too. The base
// has no fuzz target and so no fuzz seed. The adversarial shapes of
// internal/tools/hooktime have a golden of their own beside their allocation
// rows.
//
// A change in how a word is split is a classifier change, and shows here as a
// finding that differs.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// us129Golden was written from 37e3ac8 with US129_WRITE_GOLDEN=1, before
// US-129's freeze, and written again the same way from US-130's tree with
// us130_test.go set aside, so that the corpus is still the base's: US-130
// changes the answers of 160 commands on purpose. In 158 an
// enforcement-stays-on refusal's reason and route now name the six variables
// the slash route's check rests on (US-130 AC-3); the other two run a glob as
// the program on a line that names claude, which US-130 refuses as
// story-stays-in-its-session (AC-2). Each line is the sum of one command, then
// the sum of its answers in us129Sessions' order.
var us129Golden = filepath.Join("testdata", "us129_inspect.golden")

// us129Sessions are corpusState's sessions, from Bash and from PowerShell.
var us129Sessions = []struct {
	who string
	ps  bool
}{{"", false}, {"passed", false}, {"frozen", false}, {"implementer", false}, {"", true}, {"implementer", true}}

// us129Corpus is every distinct string literal of this package's test files
// but this one, in order.
func us129Corpus(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	fset := gotoken.NewFileSet()
	for _, name := range files {
		if strings.HasPrefix(name, "us129_") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == gotoken.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil && strings.TrimSpace(s) != "" {
					seen[s] = true
				}
			}
			return true
		})
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

func us129Answer(command, who string, ps bool) string {
	f, refused := Inspect(command, corpusState(who, ps))
	if !refused {
		return "allowed"
	}
	return fmt.Sprintf("refused rule=%q via=%q code=%q route=%q reason=%q", f.Rule, f.Via, f.Code, f.Route, f.Reason)
}

// us129Sum is the first twelve hex digits of the SHA-256 of text. The golden
// holds sums, not the answers themselves, which came to 11 MB: a command
// that differs is named by the row with what Inspect says of it now, and
// what the base said is the base's to show.
func us129Sum(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:6])
}

// us129Answers is every session's answer for command, in us129Sessions'
// order.
func us129Answers(command string) string {
	var b strings.Builder
	for _, s := range us129Sessions {
		tool := "Bash"
		if s.ps {
			tool = "PowerShell"
		}
		fmt.Fprintf(&b, "%s %q: %s\n", tool, s.who, us129Answer(command, s.who, s.ps))
	}
	return b.String()
}

func TestAC7InspectAnswersEveryCommandOfTheCorpusAsItDidAtTheBase(t *testing.T) {
	corpus := us129Corpus(t)
	if os.Getenv("US129_WRITE_GOLDEN") != "" {
		var b strings.Builder
		for _, command := range corpus {
			fmt.Fprintf(&b, "%s %s\n", us129Sum(command), us129Sum(us129Answers(command)))
		}
		if err := os.MkdirAll(filepath.Dir(us129Golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(us129Golden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(us129Golden)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for line := range strings.Lines(string(raw)) {
		command, answers, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			t.Fatalf("%s holds a line with no answer: %q", us129Golden, line)
		}
		want[command] = answers
	}
	if len(want) < 1000 {
		t.Fatalf("%s holds %d commands; the corpus at the base had more than 9,000", us129Golden, len(want))
	}
	differ := 0
	for _, command := range corpus {
		key := us129Sum(command)
		answers, ok := want[key]
		if !ok {
			continue
		}
		delete(want, key)
		if got := us129Answers(command); us129Sum(got) != answers {
			if differ++; differ <= 20 {
				t.Errorf("AC-7 Inspect answers %q otherwise than at the base; now:\n%s", command, got)
			}
		}
	}
	if differ > 20 {
		t.Errorf("AC-7 and %d more commands", differ-20)
	}
	// The corpus is the base's test files, which AC-1 keeps as they were: a
	// command of the golden that is no longer in them is a test file changed.
	if len(want) > 0 {
		t.Errorf("AC-7 %d commands of the golden are no longer in this package's test files", len(want))
	}
}
