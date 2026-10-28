package hook

// US-035, AC-1 to AC-4: the hook's debug log names the tool it considered and
// the reading that brought it into scope, and for a refusal the rule and the
// reading that decided; and it never holds a word of the command. The
// vocabularies are closed (decided D1): `candidate` is a name in
// shellpolicy.Candidates and `via` one of the four readings, so that a person
// can read the log and no command can write into it. Debug log only (D6).
//
// The lines are found by their keys, candidate= and via=, as slog's text
// handler writes them; the message each is under is the implementation's.

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// vzReadings are the values via may take (D1).
var vzReadings = []string{"released", "shell", "decoded", "braces"}

// vzValue finds key=value in a line of slog's text handler, quoted or not.
func vzValue(line, key string) (string, bool) {
	m := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(key) + `=("(?:[^"\\]|\\.)*"|\S*)`).FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	if strings.HasPrefix(m[1], `"`) {
		if v, err := strconv.Unquote(m[1]); err == nil {
			return v, true
		}
	}
	return m[1], true
}

// vzPair is a candidate and the reading it came in by, as a line of the log
// gave them.
type vzPair struct{ candidate, via string }

// vzConsidered is every (candidate, via) the log holds, line by line.
func vzConsidered(log string) []vzPair {
	var out []vzPair
	for line := range strings.SplitSeq(log, "\n") {
		c, hasCandidate := vzValue(line, "candidate")
		v, hasVia := vzValue(line, "via")
		if hasCandidate && hasVia {
			out = append(out, vzPair{c, v})
		}
	}
	return out
}

// vzRun puts cmd to the hook from the implementer and returns whether it was
// refused and everything the hook logged at debug level while it decided.
func vzRun(t *testing.T, root, cmd string) (refused bool, log string) {
	t.Helper()
	buf := debugLines(t)
	r := call(t, command(root, "sdlc-implementer", cmd), noEnv)
	return denied(r), buf.String()
}

// vzVocabulary holds every pair the log gave to the closed vocabularies.
func vzVocabulary(t *testing.T, cmd string, pairs []vzPair) {
	t.Helper()
	for _, p := range pairs {
		if !shellpolicy.Candidates[p.candidate] {
			t.Errorf("%s: the log names candidate=%q, which is not in shellpolicy.Candidates", cmd, p.candidate)
		}
		if !slices.Contains(vzReadings, p.via) {
			t.Errorf("%s: the log names via=%q, which is not one of %v", cmd, p.via, vzReadings)
		}
	}
}

// AC-1: the log names the tool in scope by the name in the vocabulary, never
// as typed, and the reading that brought it in; a tool in scope through two
// readings is logged for each, and a pair once however often the command
// names it.
func TestAC1TheLogNamesTheToolConsideredAndTheReadingThatBroughtItIntoScope(t *testing.T) {
	for _, tc := range []struct {
		name, cmd, candidate string
		vias                 []string
	}{
		{"AC-1 a tool given by its path", "/usr/bin/touch notes.txt", "touch", []string{"released"}},
		{"AC-1 a brace list", "touch {a,b}.txt", "touch", []string{"released", "braces"}},
		{"AC-1 named three times", "touch a.txt; touch b.txt; touch c.txt", "touch", []string{"released"}},
		{"AC-1 a commit spelled in capitals", "GIT commit -m x", "git", []string{"released"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := loopProject(t)
			_, log := vzRun(t, root, tc.cmd)
			pairs := vzConsidered(log)
			var got []string
			for _, p := range pairs {
				if p.candidate == tc.candidate {
					got = append(got, p.via)
				}
			}
			if !slices.Equal(got, tc.vias) {
				t.Errorf("%s: the log names candidate=%s with via %v, want %v; the log:\n%s", tc.cmd, tc.candidate, got, tc.vias, log)
			}
			vzVocabulary(t, tc.cmd, pairs)
		})
	}
}

// AC-3: a refusal is logged with the rule and the reading that decided it, on
// one line, so that a person can tell whether a rule carries any weight. A
// path spelled in a quoted string only the shell decodes is refused in the
// decoded reading.
func TestAC3ARefusalIsLoggedWithItsRuleAndTheReadingThatDecided(t *testing.T) {
	for _, tc := range []struct{ cmd, rule, via string }{
		{"rm .sdlc/state/active", "loop-state-through-the-tool", "released"},
		{`touch $'\x2esdlc/state/active'`, "loop-state-through-the-tool", "decoded"},
	} {
		t.Run("AC-3 "+tc.cmd, func(t *testing.T) {
			root := loopProject(t)
			refused, log := vzRun(t, root, tc.cmd)
			if !refused {
				t.Fatalf("%s was allowed", tc.cmd)
			}
			found := false
			for line := range strings.SplitSeq(log, "\n") {
				rule, _ := vzValue(line, "rule")
				via, _ := vzValue(line, "via")
				found = found || (rule == tc.rule && via == tc.via)
			}
			if !found {
				t.Errorf("%s: no line of the log names rule=%s and via=%s together; the log:\n%s", tc.cmd, tc.rule, tc.via, log)
			}
		})
	}
	// D3: the readings after the one that refused are not examined, so not
	// logged.
	t.Run("AC-3 nothing after the refusing reading", func(t *testing.T) {
		root := loopProject(t)
		cmd := "rm .sdlc/state/active; touch {a,b}.txt"
		_, log := vzRun(t, root, cmd)
		pairs := vzConsidered(log)
		if len(pairs) == 0 {
			t.Fatalf("%s: the log names no candidate at all:\n%s", cmd, log)
		}
		for _, p := range pairs {
			if p.via == "braces" {
				t.Errorf("%s: the log names %v, a reading after the one that refused", cmd, p)
			}
		}
	})
}

// vzSecret is shaped like a credential, and vzPrivate like a path; neither may
// reach the log, which is a file anyone on the machine may read (0644).
const (
	vzSecret  = "ghp_" + "zq7SecretShapedToken0123456789abcdefXYZ" // split so the secrets scan does not take it for a token
	vzPrivate = "private/zq7-notes.txt"
)

// AC-2: the log never holds the command or any word of it, on an allow and on a
// refusal, a refusal for a word through more than 64 links included, whose
// reason does quote the word. A log that says nothing holds no word either, so
// each case first finds the lines AC-1 adds: this is not passed by silence.
func TestAC2TheLogHoldsNoWordOfTheCommandOnAnAllowOrARefusal(t *testing.T) {
	root := lbProject(t)
	write(t, root, vzPrivate, "x\n")
	linked := lbAtOneName(t, root, "zq7link", 65, "elsewhere", "zq7-notes.txt")
	for _, tc := range []struct {
		name, cmd string
		refused   bool
	}{
		{"AC-2 an allow", "echo " + vzSecret + " | tee " + vzPrivate, false},
		{"AC-2 a refusal", "cp " + vzPrivate + " .sdlc/state/zq7-" + vzSecret, true},
		{"AC-2 a refusal at the bound of a word", "touch " + linked + " # " + vzSecret, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refused, log := vzRun(t, root, tc.cmd)
			if refused != tc.refused {
				t.Fatalf("%s: refused %v, want %v", tc.cmd, refused, tc.refused)
			}
			if len(vzConsidered(log)) == 0 {
				t.Fatalf("the log names no candidate, so it says nothing to hold to AC-2:\n%s", log)
			}
			for _, word := range append(strings.Fields(tc.cmd), "zq7", tc.cmd) {
				if len(word) < 3 || slices.Contains([]string{"echo", "tee", "cp", "touch"}, word) {
					// A program the vocabulary names is logged by that name.
					continue
				}
				if strings.Contains(log, word) {
					t.Errorf("%s: the log holds %q, a word of the command:\n%s", tc.name, word, log)
				}
			}
		})
	}
}

// AC-4: the page a refused reader is sent to points at the part of the message
// that is right for them, a code to look up among them now that a refusal at a
// bound carries one (D2), and at the log lines of AC-1 and AC-3.
func TestAC4TheRefusalGuidePointsAtThePartsOfTheMessageAndTheNewLogLines(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../docs/guides/why-was-i-refused.md")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.ReplaceAll(string(raw), "\r\n", "\n")
	section := func(heading string) string {
		_, rest, found := strings.Cut(page, "\n## "+heading+"\n")
		if !found {
			t.Fatalf("docs/guides/why-was-i-refused.md has no section %q", heading)
		}
		body, _, _ := strings.Cut(rest, "\n## ")
		return body
	}

	t.Run("AC-4 read the refusal: a code, and where it is looked up", func(t *testing.T) {
		read := section("Read the refusal")
		for _, want := range []string{"SDLC-E", "troubleshooting.md"} {
			if !strings.Contains(read, want) {
				t.Errorf("\"Read the refusal\" does not mention %q", want)
			}
		}
	})
	t.Run("AC-4 see every decision: candidate and via, and the reading that refused", func(t *testing.T) {
		see := section("See every decision")
		for _, want := range []string{"`candidate`", "`via`"} {
			if !strings.Contains(see, want) {
				t.Errorf("\"See every decision\" does not explain %s", want)
			}
		}
		considered, decided := false, false
		for line := range strings.SplitSeq(see, "\n") {
			candidate, hasCandidate := vzValue(line, "candidate")
			via, hasVia := vzValue(line, "via")
			rule, _ := vzValue(line, "rule")
			if hasVia && !slices.Contains(vzReadings, via) {
				t.Errorf("an example line gives via=%q, which is not one of %v: %s", via, vzReadings, line)
			}
			if hasCandidate && !shellpolicy.Candidates[candidate] {
				t.Errorf("an example line gives candidate=%q, which is not in shellpolicy.Candidates: %s", candidate, line)
			}
			considered = considered || (hasCandidate && hasVia)
			decided = decided || (rule != "" && hasVia)
		}
		if !considered {
			t.Error("\"See every decision\" shows no log line with candidate= and via=")
		}
		if !decided {
			t.Error("\"See every decision\" shows no log line of a refusal with rule= and via=")
		}
	})
}
