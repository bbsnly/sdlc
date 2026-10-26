package shellpolicy

// US-035, AC-1 to AC-3: the classifier tells its caller which tool a command
// put in scope and through which reading, and which reading decided a
// refusal, so that the hook can log both without logging a word of the
// command. Decided by the person (D1): `candidate` and `via` come from closed
// vocabularies, never the spelling typed, and a word that maps to none is not
// reported at all; each distinct pair once a command. (D3): readings are
// reported up to and including the one that refuses, and none after it.
//
// What these tests bind, and the implementation has to provide:
//
//	State.Considered func(candidate, via string)
//	    when set, told each distinct (candidate, via) pair once for a call
//	    of Inspect, in the order the readings are put to the rules
//	Finding.Via string
//	    the reading that decided the refusal: one of readingNames
//	Candidates map[string]bool
//	    the closed vocabulary of candidate: the programs the rules key on
//
// The names of the readings are D1's: the four texts readings() makes.

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// readingNames are the values via may take (D1).
var readingNames = []string{"released", "shell", "decoded", "braces"}

// cnPair is one thing Considered was told.
type cnPair struct{ candidate, via string }

// cnAsk is Inspect of command for the implementer, and every pair Considered
// was told, in order.
func cnAsk(t *testing.T, command string) (Finding, bool, []cnPair) {
	t.Helper()
	var told []cnPair
	s := State{Agent: "implementer", Considered: func(candidate, via string) {
		told = append(told, cnPair{candidate, via})
	}}
	f, refused := Inspect(command, s)
	return f, refused, told
}

// cnVias is the readings told for candidate.
func cnVias(told []cnPair, candidate string) []string {
	var out []string
	for _, p := range told {
		if p.candidate == candidate {
			out = append(out, p.via)
		}
	}
	return out
}

// AC-1: a tool in scope is told, by the name in the vocabulary and the
// reading that brought it in; a tool in scope through several readings is
// told for each; and a pair is told once, however many times the command
// names it.
func TestAC1InspectTellsTheToolInScopeAndEachReadingThatBroughtItInOnce(t *testing.T) {
	t.Run("AC-11 Inspect answers the same with no one to tell", cnSameAnswerWithNoOneToTell)
	for _, tc := range []struct {
		name, command, candidate string
		vias                     []string
	}{
		{"AC-1 one reading", "touch notes.txt", "touch", []string{"released"}},
		{"AC-1 a brace list is a second reading", "touch {a,b}.txt", "touch", []string{"released", "braces"}},
		{"AC-1 a quoted string the shell decodes is a second reading", "touch $'notes.txt'", "touch", []string{"released", "decoded"}},
		{"AC-1 a pair named twice is told once", "touch a.txt; touch b.txt; touch c.txt", "touch", []string{"released"}},
		{"AC-1 a commit", "git commit -m x", "git", []string{"released"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, told := cnAsk(t, tc.command)
			if got := cnVias(told, tc.candidate); !slices.Equal(got, tc.vias) {
				t.Errorf("%s: told %s through %v, want %v (everything told: %v)", tc.command, tc.candidate, got, tc.vias, told)
			}
			seen := map[cnPair]bool{}
			for _, p := range told {
				if seen[p] {
					t.Errorf("%s: told %v twice", tc.command, p)
				}
				seen[p] = true
			}
		})
	}
}

// AC-1 and AC-2 (D1, THREATS I1 and I2): candidate is never the program as
// typed, and nothing outside the vocabularies is ever told. A path to the
// program, a quoted, escaped or capitalised name, and a program the rules do
// not key on are words of the command, and a word of the command is not
// logged: git spelled five ways is told as git, and the made-up program is not
// told at all.
func TestAC1AC2OnlyNamesFromTheClosedVocabulariesAreEverTold(t *testing.T) {
	for _, tc := range []struct {
		command, candidate string
	}{
		{"/usr/bin/touch notes.txt", "touch"},
		{"/usr/local/bin/git commit -m x", "git"},
		{`G\IT commit -m x`, "git"},
		{`"git" commit -m x`, "git"},
		{"GIT commit -m x", "git"},
		{"zq7frobnicate --write notes.txt", ""},
	} {
		t.Run("AC-2 "+tc.command, func(t *testing.T) {
			_, _, told := cnAsk(t, tc.command)
			if tc.candidate != "" && len(cnVias(told, tc.candidate)) == 0 {
				t.Errorf("%s: %s was not told (everything told: %v)", tc.command, tc.candidate, told)
			}
			for _, p := range told {
				if !Candidates[p.candidate] {
					t.Errorf("%s: told candidate %q, which is not in Candidates", tc.command, p.candidate)
				}
				if !slices.Contains(readingNames, p.via) {
					t.Errorf("%s: told via %q, which is not one of %v", tc.command, p.via, readingNames)
				}
				if strings.ContainsAny(p.candidate, "/\\\"' =") || strings.Contains(p.candidate, "zq7") || p.candidate != strings.ToLower(p.candidate) {
					t.Errorf("%s: told candidate %q, the program as it was typed", tc.command, p.candidate)
				}
			}
		})
	}
}

// candidateName is what a word of the vocabulary may hold: nothing a log line
// could be forged with, no space, no `=`, no quote, no line break (I2).
var candidateName = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*$`)

// AC-1 (D1): the vocabulary is the programs the rules key on, git, sdlc, the
// verbs that change a file and the interpreters among them, and every entry is
// a plain name.
func TestAC1TheVocabularyOfCandidatesIsTheProgramsTheRulesKeyOn(t *testing.T) {
	for _, want := range []string{"git", "sdlc", "rm", "touch", "cp", "tee", "mv", "install", "python"} {
		if !Candidates[want] {
			t.Errorf("Candidates does not hold %q, a program the rules key on", want)
		}
	}
	for name := range Candidates {
		if !candidateName.MatchString(name) {
			t.Errorf("Candidates holds %q, which is not a plain lower-case name a log line can carry as it is", name)
		}
	}
}

// AC-3: a refusal carries the reading that decided it. A protected path
// written plainly is refused in the released reading; one spelled in a quoted
// string only the shell decodes is refused in the decoded reading, which is the
// reading a person looking at the log needs to see.
func TestAC3ARefusalCarriesTheReadingThatDecidedIt(t *testing.T) {
	for _, tc := range []struct{ command, rule, via string }{
		{"rm .sdlc/state/active", ruleLoopState, "released"},
		{`touch $'\x2esdlc/state/active'`, ruleLoopState, "decoded"},
		{`echo x > $'\x2esdlc/state/active'`, ruleLoopState, "decoded"},
	} {
		t.Run("AC-3 "+tc.command, func(t *testing.T) {
			f, refused, _ := cnAsk(t, tc.command)
			if !refused {
				t.Fatalf("%s was allowed", tc.command)
			}
			if f.Rule != tc.rule {
				t.Errorf("%s was refused under %s, want %s", tc.command, f.Rule, tc.rule)
			}
			if f.Via != tc.via {
				t.Errorf("%s: Finding.Via = %q, want %q", tc.command, f.Via, tc.via)
			}
		})
	}
}

// AC-3 (D3): the loop stops at the first reading that refuses, and so does the
// telling. The released reading refuses this command, so its brace list is
// never read, and no pair through the braces is told; the refusing reading's
// own tool is.
func TestAC3NoReadingAfterTheOneThatRefusedIsTold(t *testing.T) {
	f, refused, told := cnAsk(t, "rm .sdlc/state/active; touch {a,b}.txt")
	if !refused || f.Via != "released" {
		t.Fatalf("refused %v through %q, want refused through the released reading", refused, f.Via)
	}
	for _, p := range told {
		if p.via != "released" {
			t.Errorf("told %v, a reading after the one that refused", p)
		}
	}
	if len(cnVias(told, "rm")) == 0 {
		t.Errorf("the tool of the refusing reading, rm, was not told: %v", told)
	}
}

// AC-1: with no one to tell, nothing changes: Inspect answers as it did, for
// the same commands, with Considered unset.
func cnSameAnswerWithNoOneToTell(t *testing.T) {
	for _, command := range []string{"touch notes.txt", "rm .sdlc/state/active", `touch $'\x2esdlc/state/active'`, "git commit -m x"} {
		told, toldRefused, _ := cnAsk(t, command)
		plain, plainRefused := Inspect(command, State{Agent: "implementer"})
		if toldRefused != plainRefused || told.Rule != plain.Rule || told.Reason != plain.Reason || told.Route != plain.Route {
			t.Errorf("%s: answered %v %+v with Considered set and %v %+v without", command, toldRefused, told, plainRefused, plain)
		}
	}
}
