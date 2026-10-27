package shellpolicy

// US-035 (from US-052), AC-5 to AC-7 and AC-12, as the classifier sees them:
// the caller says, word by word, whether placing it passed a bound on the
// links followed, and the classifier refuses the command there, under the
// code the bound has. internal/hook wires this to pathrules (Bound and the
// command's budget); here the caller is the test, so no link is made.
//
// What these tests bind, and the implementation has to provide:
//
//	State.Bound func(word string) error
//	    when set, asked of every word the classifier places, as Resolve is
//	    asked it, and of every word a program that makes directories one at a
//	    time is given (mkdir -p, install -D): nil, or an error that errors.Is
//	    pathrules.ErrTooManyLinks or pathrules.ErrLinkBudget
//
// and a refusal for it that Finding.Message renders with its code (D2):
//
//	<word> goes through more than 64 links, more than sdlc follows in one path.
//	Instead: name the place it leads to directly, without the links
//	(SDLC-E0081) [<rule>]
//
// with SDLC-E0082 for a command over its budget. Where the code sits against
// the rule id is the plan's; both are there.

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

const (
	lbOver64   = " goes through more than 64 links, more than sdlc follows in one path"
	lbRoute    = "Instead: name the place it leads to directly, without the links"
	lbWordCode = "SDLC-E0081"
	lbCmdCode  = "SDLC-E0082"
)

var lbRuleID = regexp.MustCompile(`\[[a-z0-9]+(?:-[a-z0-9]+)*\]`)

// lbState is the implementer's State, with every word that starts with lnk
// over the bound err and every other word within it.
func lbState(err error) (State, *[]string) {
	var asked []string
	return State{
		Agent:   "implementer",
		Resolve: func(word string) string { return word },
		Bound: func(word string) error {
			asked = append(asked, word)
			if strings.HasPrefix(word, "lnk") {
				return err
			}
			return nil
		},
	}, &asked
}

// AC-5, AC-7 and AC-12: a word over the bound of one word is refused, whatever
// the program, naming the word as typed, the limit, the route and the code.
func TestAC5AC7AC12AWordOverTheBoundOfOneWordIsRefusedUnderItsCode(t *testing.T) {
	for _, tc := range []struct{ command, word string }{
		{"touch lnk/notes.txt", "lnk/notes.txt"},
		{"cp notes.txt lnk/notes.txt", "lnk/notes.txt"},
		{"echo x > lnk/notes.txt", "lnk/notes.txt"},
		{"echo x | tee lnk/notes.txt", "lnk/notes.txt"},
		{"mkdir -p lnk/made/deeper", "lnk/made/deeper"},
		{"install -D notes.txt lnk/made/notes.txt", "lnk/made/notes.txt"},
	} {
		t.Run("AC-5 "+tc.command, func(t *testing.T) {
			s, _ := lbState(pathrules.ErrTooManyLinks)
			f, refused := Inspect(tc.command, s)
			if !refused {
				t.Fatalf("%s was allowed with %s over the bound", tc.command, tc.word)
			}
			msg := f.Message()
			for _, part := range []string{tc.word + lbOver64, lbRoute, lbWordCode} {
				if !strings.Contains(msg, part) {
					t.Errorf("%s: the refusal does not say %q:\n%s", tc.command, part, msg)
				}
			}
			if f.Rule == "" || !strings.Contains(msg, "["+f.Rule+"]") || !lbRuleID.MatchString(msg) {
				t.Errorf("%s: the refusal carries no rule id in square brackets:\n%s", tc.command, msg)
			}
		})
	}
	t.Run("AC-5 an error that wraps the bound is the bound", func(t *testing.T) {
		s, _ := lbState(fmt.Errorf("%w: 65 links", pathrules.ErrTooManyLinks))
		if f, refused := Inspect("touch lnk/notes.txt", s); !refused || !strings.Contains(f.Message(), lbWordCode) {
			t.Errorf("refused %v: %s", refused, f.Message())
		}
	})
	t.Run("AC-5 a word within the bound is not refused for it", func(t *testing.T) {
		s, asked := lbState(pathrules.ErrTooManyLinks)
		if f, refused := Inspect("touch notes.txt && mkdir -p made/deeper", s); refused {
			t.Errorf("refused: %s", f.Message())
		}
		if len(*asked) == 0 {
			t.Error("Bound was never asked about the words of touch and mkdir -p")
		}
	})
	// THREATS P1: the word goes back to the model, and a word of kilobytes is
	// not echoed whole.
	t.Run("AC-5 a word of four kilobytes is not quoted whole", func(t *testing.T) {
		s, _ := lbState(pathrules.ErrTooManyLinks)
		word := "lnk/" + strings.Repeat("b/", 2048) + "notes.txt"
		f, refused := Inspect("touch "+word, s)
		if !refused || !strings.Contains(f.Message(), lbOver64) {
			t.Fatalf("refused %v: %.200s", refused, f.Message())
		}
		if strings.Contains(f.Message(), word) || len(f.Message()) > 2048 {
			t.Errorf("the refusal quotes a word of %d bytes whole, in %d bytes", len(word), len(f.Message()))
		}
	})
}

// AC-6 and AC-12: a command over its budget is refused under SDLC-E0082, not
// under the code of a word over 64 links, with a route and a rule id.
func TestAC6AC12ACommandOverItsBudgetIsRefusedUnderItsOwnCode(t *testing.T) {
	s, _ := lbState(pathrules.ErrLinkBudget)
	f, refused := Inspect("touch notes.txt lnk/notes.txt", s)
	if !refused {
		t.Fatal("a command over its budget was allowed")
	}
	msg := f.Message()
	if !strings.Contains(msg, lbCmdCode) || strings.Contains(msg, lbWordCode) {
		t.Errorf("the refusal of a command over its budget does not name %s alone:\n%s", lbCmdCode, msg)
	}
	if !strings.Contains(msg, "Instead: ") || f.Rule == "" || !strings.Contains(msg, "["+f.Rule+"]") {
		t.Errorf("the refusal of a command over its budget has no route or no rule id:\n%s", msg)
	}
	// THREATS T5: a rule that refuses the command under its own name, in the
	// part read before the budget was passed, keeps it.
	t.Run("AC-6 a rule that names the command first keeps it", func(t *testing.T) {
		s, _ := lbState(pathrules.ErrLinkBudget)
		if f, refused := Inspect("rm .sdlc/state/active; touch lnk/notes.txt", s); !refused || f.Rule != ruleLoopState {
			t.Errorf("refused %v under %q, want %s", refused, f.Rule, ruleLoopState)
		}
	})
	t.Run("AC-6 an error Bound gives that is neither bound refuses nothing", func(t *testing.T) {
		s, _ := lbState(errors.New("zq7: not a bound"))
		if f, refused := Inspect("touch lnk/notes.txt", s); refused && (strings.Contains(f.Message(), lbWordCode) || strings.Contains(f.Message(), lbCmdCode)) {
			t.Errorf("an error that is no bound was refused as one:\n%s", f.Message())
		}
	})
}

// lbDashState is the implementer's State where -lnk is a chain past the bound
// of one word, -gap one past what the system follows in one lookup, and -st a
// link to the loop's state.
func lbDashState() State {
	return State{
		Agent: "implementer",
		Resolve: func(word string) string {
			if rest, ok := strings.CutPrefix(word, "-st/"); ok {
				return ".sdlc/state/" + rest
			}
			return word
		},
		Bound: func(word string) error {
			switch {
			case strings.HasPrefix(word, "-lnk"):
				return pathrules.ErrTooManyLinks
			case strings.HasPrefix(word, "-gap"):
				return pathrules.ErrLookupLimit
			}
			return nil
		},
	}
}

// AC-5, AC-7 and AC-12, for a word that begins with a dash. After `--`, as
// `./-x` or as a redirect's target such a word is a file, and the classifier
// skipped it: `touch -- -e65/x`, `echo x > -e65/x` and `mkdir -p -- -h70/...`
// went through more links than the bound and were allowed, `touch ./-s40/active`
// went through 40 into the loop's state, and `echo x > ./-x/state/active`,
// through one link named -x to .sdlc, wrote the loop's state (US-035, the
// verifier's first gap).
func TestAC5AC7AC12AWordThatBeginsWithADashIsHeldToTheBoundLikeAnyOther(t *testing.T) {
	const lookup = " goes through more links than this system follows in one lookup"
	for _, tc := range []struct{ name, command, word, reason string }{
		{"after --", "touch -- -lnk/x", "-lnk/x", lbOver64},
		{"as ./-x", "touch ./-lnk/x", "./-lnk/x", lbOver64},
		{"as a redirect's target", "echo x > -lnk/x", "-lnk/x", lbOver64},
		{"as a directory mkdir -p makes, after --", "mkdir -p -- -lnk/n/x", "-lnk/n/x", lbOver64},
		{"after --, past one lookup", "touch -- -gap/x", "-gap/x", lookup},
		{"as ./-x, past one lookup", "touch ./-gap/x", "./-gap/x", lookup},
		{"as a redirect's target, past one lookup", "echo x > ./-gap/x", "./-gap/x", lookup},
		{"as a directory mkdir -p makes, past one lookup", "mkdir -p -- -gap/n/x", "-gap/n/x", lookup},
	} {
		t.Run("AC-5 "+tc.name, func(t *testing.T) {
			f, refused := Inspect(tc.command, lbDashState())
			if !refused || f.Code != lbWordCode || !strings.HasPrefix(f.Reason, tc.word+tc.reason) {
				t.Errorf("%s: refused %v, %q; want %q under %s", tc.command, refused, f.Message(), tc.word+tc.reason, lbWordCode)
			}
		})
	}
	t.Run("AC-5 a word that is empty once cleaned is resolved nowhere", func(t *testing.T) {
		s := lbDashState()
		var asked []string
		resolve := s.Resolve
		s.Resolve = func(w string) string { asked = append(asked, w); return resolve(w) }
		Inspect(`touch '' "" -- ''`, s)
		if slices.Contains(asked, "") {
			t.Errorf("Resolve was handed the empty word: %q", asked)
		}
	})
	for _, command := range []string{"echo x > ./-st/active", "touch -- -st/active", "cp notes.txt ./-st/active"} {
		t.Run("AC-12 "+command+" writes the loop's state through a link named with a dash", func(t *testing.T) {
			f, refused := Inspect(command, lbDashState())
			if !refused || f.Rule != "loop-state-through-the-tool" {
				t.Errorf("refused %v, %q; want it refused under loop-state-through-the-tool", refused, f.Message())
			}
		})
	}
}

// lbCdState is lbDashState with -x, 10 and sx links to .sdlc, so that a
// relative word after a cd into one of them is in the loop's state.
func lbCdState() State {
	s := lbDashState()
	resolve := s.Resolve
	s.Resolve = func(word string) string {
		for _, dir := range []string{"-x/", "10/", "sx/"} {
			if rest, ok := strings.CutPrefix(word, dir); ok {
				return ".sdlc/" + rest
			}
		}
		return resolve(word)
	}
	return s
}

// AC-5, AC-7, AC-11 and AC-12, for the directory a cd moves to. Its operand
// was taken past every word that begins with a dash, `--` included, so the
// directory after `cd --` was never placed: `cd -- -x && rm state/active`
// with -x a link to .sdlc, and `cd -- -e65 && touch y` through 65 links,
// were allowed. And a directory of digits was dropped as an option (US-035,
// the verifier's second round).
func TestAC5AC7AC12TheDirectoryACdMovesToIsReadAfterItsOptions(t *testing.T) {
	const lookup = " goes through more links than this system follows in one lookup"
	for _, command := range []string{
		"cd -- -x && rm state/active",
		"pushd -- -x && rm state/active",
		"cd -L -- -x && rm state/active",
		"cd 10 && rm state/active",
		"env -C -x rm state/active",
	} {
		t.Run("AC-12 "+command, func(t *testing.T) {
			f, refused := Inspect(command, lbCdState())
			if !refused || f.Rule != "loop-state-through-the-tool" {
				t.Errorf("refused %v, %q; want it refused under loop-state-through-the-tool", refused, f.Message())
			}
		})
	}
	for _, tc := range []struct{ command, reason string }{
		{"cd -- -lnk && touch y", lbOver64},
		{"cd -- -gap && touch active", lookup},
	} {
		t.Run("AC-5 "+tc.command, func(t *testing.T) {
			f, refused := Inspect(tc.command, lbCdState())
			if !refused || f.Code != lbWordCode || !strings.Contains(f.Reason, tc.reason) {
				t.Errorf("refused %v, %q; want %q under %s", refused, f.Message(), tc.reason, lbWordCode)
			}
		})
	}
	// AC-11: what a cd's options and the directories it cannot follow were
	// read as before, they are read as now.
	for command, refuse := range map[string]bool{
		"cd - && touch a":                false,
		"cd -L sx && rm state/active":    true,
		"cd -P sx && rm state/active":    true,
		"cd -- sx && rm state/active":    true,
		"cd -L src && touch a":           false,
		"cd ~ && rm .sdlc/state/active":  true,
		"popd && touch a":                false,
		"cd -P -- src && touch notes.md": false,
	} {
		t.Run("AC-11 "+command, func(t *testing.T) {
			f, refused := Inspect(command, lbCdState())
			if refused != refuse {
				t.Errorf("refused %v, %q; want refused %v, as before", refused, f.Message(), refuse)
			}
		})
	}
}

// AC-12: movedFrom, the cd that leaves a lost walk somewhere spelled from ~,
// reads its directory after `--` as changedDir does, and `cd -` says nowhere.
// A bare cd goes to the home, as `cd ~` does: read as nowhere, a write
// through a link in the home after it was placed in the project (landing
// round, carried from US-120).
func TestAC12MovedFromReadsTheDirectoryAfterItsOptionsAsChangedDirDoes(t *testing.T) {
	for _, tc := range []struct {
		words []string
		want  string
	}{
		{[]string{"cd", "--", "-x"}, "~/proj/-x"},
		{[]string{"cd", "-L", "--", "-x"}, "~/proj/-x"},
		{[]string{"cd", "-L", "sub"}, "~/proj/sub"},
		{[]string{"cd", "10"}, "~/proj/10"},
		{[]string{"cd", "-"}, ""},
		{[]string{"cd", "--", "~/elsewhere"}, "~/elsewhere"},
		{[]string{"cd"}, "~"},
	} {
		if got := movedFrom("~/proj", true, tc.words); got != tc.want {
			t.Errorf("AC-12: movedFrom(~/proj, lost, %q) = %q, want %q", tc.words, got, tc.want)
		}
	}
}

// AC-6 and AC-12: E0082 names the word that spent the budget. Every word asked
// after it is over the budget as well, and a later one spelled otherwise than
// it cleans, `./a` or `"b.txt"`, was quoted in its place; a quoted word with a
// space in it was split (US-035, code review).
func TestAC6AC12ARefusalForTheBudgetNamesTheWordThatSpentIt(t *testing.T) {
	for _, later := range []string{"./a", `"b.txt"`, `"sp ace.txt"`} {
		t.Run("AC-6 then "+later, func(t *testing.T) {
			s, _ := lbState(nil)
			spent := false
			s.Bound = func(word string) error {
				if strings.HasPrefix(word, "lnkspend") {
					spent = true
				}
				if spent && word != "" {
					return pathrules.ErrLinkBudget
				}
				return nil
			}
			f, refused := Inspect("touch lnkspend/x "+later, s)
			want := ", and lnkspend/x is the word that took it past"
			if !refused || f.Code != lbCmdCode || !strings.Contains(f.Reason, want) {
				t.Errorf("refused %v, %q; want %s naming lnkspend/x", refused, f.Message(), lbCmdCode)
			}
		})
	}
}

// AC-12: where the first word past a bound is one a rule made itself, which
// the command never spells, the refusal quotes the first word of the same kind
// the command does spell; a word of the other kind is never quoted for it.
func TestAC12ARefusalForAWordARuleMadeQuotesTheFirstOfItsKindTheCommandSpells(t *testing.T) {
	s := State{Bound: func(string) error { return nil }}
	for _, tc := range []struct {
		name string
		hits []linkHit
		want string
	}{
		{"of the same kind", []linkHit{
			{err: pathrules.ErrTooManyLinks, word: "made/by/a/rule"},
			{err: pathrules.ErrLinkBudget, word: "lnkb/y"},
			{err: pathrules.ErrTooManyLinks, word: "lnk/x"},
		}, "lnk/x" + lbOver64},
		{"none of the same kind", []linkHit{
			{err: pathrules.ErrTooManyLinks, word: "made/by/a/rule"},
			{err: pathrules.ErrLinkBudget, word: "lnkb/y"},
		}, "made/by/a/rule" + lbOver64},
	} {
		t.Run("AC-12 "+tc.name, func(t *testing.T) {
			in := newInspection()
			in.hits = tc.hits
			f, ok := in.linkFinding(s, "touch lnkb/y lnk/x")
			if !ok || f.Code != lbWordCode || !strings.HasPrefix(f.Reason, tc.want) {
				t.Errorf("linkFinding = %v, %q; want %q under %s", ok, f.Message(), tc.want, lbWordCode)
			}
		})
	}
}

// AC-5: after a cd, the refusal names the word as the person typed it, not
// the cd's directory, which every word joined to it is below: `cd sub &&
// touch lnk65/x` said "sub goes through more than 64 links", and E0082 said
// "sub is the word that took it past" (US-035, the verifier's fourth round).
// The cd's directory is named only where it alone holds the links.
func TestAC5AfterACdTheRefusalNamesTheWordAsTyped(t *testing.T) {
	const lookup = " goes through more links than this system follows in one lookup"
	bound := func(word string) error {
		switch {
		case strings.HasPrefix(word, "sub/lnk65/"), strings.HasPrefix(word, "/work/p/sub/lnk65/"),
			word == "e65", strings.HasPrefix(word, "e65/"), word == "-e65", strings.HasPrefix(word, "-e65/"):
			return pathrules.ErrTooManyLinks
		case strings.HasPrefix(word, "sub/st41/"):
			return pathrules.ErrLookupLimit
		}
		return nil
	}
	for _, tc := range []struct{ command, want, code string }{
		{"cd sub && touch lnk65/x", "lnk65/x" + lbOver64, lbWordCode},
		{"cd sub && touch st41/active", "st41/active" + lookup, lbWordCode},
		{"cd sub; echo x > lnk65/y", "lnk65/y" + lbOver64, lbWordCode},
		{"cd /work/p/sub && touch lnk65/x", "lnk65/x" + lbOver64, lbWordCode},
		{"touch sub/lnk65/x", "sub/lnk65/x" + lbOver64, lbWordCode},
		{"cd -- e65 && touch y", "e65" + lbOver64, lbWordCode},
		{"cd -- -e65 && touch y", "-e65" + lbOver64, lbWordCode},
	} {
		t.Run("AC-5 "+tc.command, func(t *testing.T) {
			s, _ := lbState(nil)
			s.Bound = bound
			f, refused := Inspect(tc.command, s)
			if !refused || f.Code != tc.code || !strings.HasPrefix(f.Reason, tc.want) {
				t.Errorf("refused %v, %q; want %q under %s", refused, f.Message(), tc.want, tc.code)
			}
		})
	}
	words := make([]string, 18)
	for i := range words {
		words[i] = fmt.Sprintf("w%d/n", i)
	}
	for _, tc := range []struct{ command, spender string }{
		{"cd sub && touch " + strings.Join(words, " "), "sub/w17/"},
		{"cd sub && cd .. && touch " + strings.Join(words, " "), "w17/"},
	} {
		t.Run("AC-5 E0082, "+tc.command[:min(len(tc.command), 30)], func(t *testing.T) {
			s, _ := lbState(nil)
			spent := false
			s.Bound = func(word string) error {
				if strings.HasPrefix(word, tc.spender) {
					spent = true
				}
				if spent && word != "" {
					return pathrules.ErrLinkBudget
				}
				return nil
			}
			f, refused := Inspect(tc.command, s)
			want := ", and w17/n is the word that took it past"
			if !refused || f.Code != lbCmdCode || !strings.Contains(f.Reason, want) {
				t.Errorf("refused %v, %q; want %s naming w17/n", refused, f.Message(), lbCmdCode)
			}
		})
	}
}

// AC-12: a decision a person keeps, sent by env -C to a directory whose name
// begins with a dash, is asked about there, as a cd to it would be. Read past
// its options, the directory was dropped and the walk lost (US-035, the
// verifier's second round).
func TestAC12ADecisionSentByEnvCIsAskedAboutWhereItSendsIt(t *testing.T) {
	for command, want := range map[string]string{
		"env -C -x sdlc approve A-1":      "-x",
		"env -C /work/b sdlc approve A-1": "/work/b",
	} {
		var asked []string
		HumanDecisions(command, false, func(dir string) bool { asked = append(asked, dir); return false })
		if !slices.Equal(asked, []string{want}) {
			t.Errorf("AC-12: %q asked about %q, want %q", command, asked, want)
		}
	}
}

// lbSpendingBound is a Bound for which every word from the first that starts
// with spender on is past the command's budget, and every word past the bound
// of one word that past says.
func lbSpendingBound(spender string, past func(string) error) func(string) error {
	spent := false
	return func(word string) error {
		if past != nil {
			if err := past(word); err != nil {
				return err
			}
		}
		if spender != "" && strings.HasPrefix(word, spender) {
			spent = true
		}
		if spent && word != "" {
			return pathrules.ErrLinkBudget
		}
		return nil
	}
}

// AC-5 and AC-6 from a session whose working directory is a subdirectory of
// the project (State.Dir). The rules join every relative word to it, the key
// was never found in the line, and E0082 named a later word with no link, `./a`
// or `a`, as the one that spent the budget; E0081 quoted the joined key for the
// word typed (US-035, code review round 2).
func TestAC5AC6FromASubdirectoryTheRefusalNamesTheWordAsTyped(t *testing.T) {
	words := make([]string, 18)
	for i := range words {
		words[i] = fmt.Sprintf("c%d_0/x", i)
	}
	for _, later := range []string{"./a", "a"} {
		t.Run("AC-6 E0082 then "+later, func(t *testing.T) {
			s, _ := lbState(nil)
			s.Dir = "pkg"
			s.Bound = lbSpendingBound("pkg/c17_0/", nil)
			f, refused := Inspect("touch "+strings.Join(words, " ")+" "+later, s)
			want := ", and c17_0/x is the word that took it past"
			if !refused || f.Code != lbCmdCode || !strings.Contains(f.Reason, want) {
				t.Errorf("refused %v, %q; want %s naming c17_0/x", refused, f.Message(), lbCmdCode)
			}
		})
	}
	over := func(word string) error {
		if strings.HasPrefix(word, "pkg/lnk65/") {
			return pathrules.ErrTooManyLinks
		}
		return nil
	}
	for _, tc := range []struct{ command, want string }{
		{"touch lnk65/x", "lnk65/x" + lbOver64},
		{"cd .. && touch pkg/lnk65/x", "pkg/lnk65/x" + lbOver64},
	} {
		t.Run("AC-5 "+tc.command, func(t *testing.T) {
			s, _ := lbState(nil)
			s.Dir = "pkg"
			s.Bound = over
			f, refused := Inspect(tc.command, s)
			if !refused || f.Code != lbWordCode || !strings.HasPrefix(f.Reason, tc.want) {
				t.Errorf("refused %v, %q; want %q under %s", refused, f.Message(), tc.want, lbWordCode)
			}
		})
	}
}

// AC-5 and AC-7: a cd into a directory behind 33 to 64 links names that
// directory, under the lookup-limit reason: Bound of the directory alone does
// not ask the system to follow its own last link, and the word after the cd
// was quoted (US-035, verifier round 5). A word whose own links pass the limit
// after a cd into a plain directory is still the word quoted.
func TestAC5AC7ACdIntoADirectoryPastTheLookupLimitNamesTheDirectory(t *testing.T) {
	const lookup = " goes through more links than this system follows in one lookup"
	bound := func(word string) error {
		switch {
		case strings.HasPrefix(word, "e40/"), strings.HasPrefix(word, "sub/st41/"):
			return pathrules.ErrLookupLimit
		}
		return nil
	}
	for _, tc := range []struct{ command, want string }{
		{"cd e40 && touch x", "e40" + lookup},
		{"cd e40; echo x > y", "e40" + lookup},
		{"cd sub && touch st41/active", "st41/active" + lookup},
	} {
		t.Run("AC-7 "+tc.command, func(t *testing.T) {
			s, _ := lbState(nil)
			s.Bound = bound
			f, refused := Inspect(tc.command, s)
			if !refused || f.Code != lbWordCode || !strings.HasPrefix(f.Reason, tc.want) {
				t.Errorf("refused %v, %q; want %q under %s", refused, f.Message(), tc.want, lbWordCode)
			}
		})
	}
}

// AC-5 and AC-7: from a working directory the walk reached only past the
// lookup limit, every command is refused, echo included, and the refusal named
// "a path this command reaches", nothing a person could act on (US-035,
// security review). It names the working directory; where the walk there
// passed no bound, nothing changes.
func TestAC5AC7FromAWorkingDirectoryPastTheLookupLimitTheRefusalNamesIt(t *testing.T) {
	for _, tc := range []struct {
		name     string
		dirBound error
		code     string
		want     string
	}{
		{"past the lookup limit", pathrules.ErrLookupLimit, lbWordCode,
			"the working directory goes through more links than this system follows in one lookup"},
		{"past 64 links", pathrules.ErrTooManyLinks, lbWordCode, "the working directory" + lbOver64},
		{"too long a path", pathrules.ErrPathTooLong, "SDLC-E0083", "the full path of the working directory"},
	} {
		t.Run("AC-7 "+tc.name, func(t *testing.T) {
			s, _ := lbState(nil)
			s.DirBound = tc.dirBound
			s.Bound = func(word string) error {
				if word == "" {
					return tc.dirBound
				}
				return nil
			}
			f, refused := Inspect("echo ok", s)
			if !refused || f.Code != tc.code || !strings.HasPrefix(f.Reason, tc.want) {
				t.Errorf("refused %v, %q; want %q under %s", refused, f.Message(), tc.want, tc.code)
			}
		})
	}
	t.Run("AC-11 a working directory the walk passed no bound to", func(t *testing.T) {
		s, _ := lbState(nil)
		if f, refused := Inspect("echo ok", s); refused {
			t.Errorf("refused: %q", f.Message())
		}
	})
}

// AC-5: where a word a rule made itself is the first hit, the refusal takes
// the first spelled hit of its kind whole, word and reason together. Lending
// the word alone gave a lookup-limit reason to a word over 64 (US-035, security
// review).
func TestAC5ARefusalTakesItsWordAndItsReasonFromTheSameHit(t *testing.T) {
	in := newInspection()
	in.hits = []linkHit{
		{err: pathrules.ErrLookupLimit, word: "made/by/a/rule"},
		{err: pathrules.ErrTooManyLinks, word: "lnk/x"},
	}
	f, ok := in.linkFinding(State{Bound: func(string) error { return nil }}, "touch lnk/x")
	if !ok || !strings.HasPrefix(f.Reason, "lnk/x"+lbOver64) {
		t.Errorf("linkFinding = %v, %q; want lnk/x with its own reason, more than 64 links", ok, f.Message())
	}
}

// AC-5: a field is read as a redirect's target only where it is one. `2>x`
// names x; `123abc` is a word, not a redirect of descriptor 123 to abc; and a
// copy of a descriptor, `2>&1`, names no file (US-035, code review round 2).
func TestAC5OnlyARedirectHasItsDescriptorReadOffTheFront(t *testing.T) {
	for _, tc := range []struct {
		key, line, want string
		found           bool
	}{
		{"x", "echo a 2>x", "x", true},
		{"x", "echo a &>x", "x", true},
		{"abc", "touch 123abc", "abc", false},
		{"123abc", "touch 123abc", "123abc", true},
		{"1", "ls 2>&1", "1", false},
	} {
		if got, found := spelled(tc.key, tc.line, ""); got != tc.want || found != tc.found {
			t.Errorf("AC-5: spelled(%q, %q) = %q, %v; want %q, %v", tc.key, tc.line, got, found, tc.want, tc.found)
		}
	}
}

// AC-5 and AC-7 (SDLC-E0083): a word whose full path is longer than the
// system looks up in one go is refused under its own rule and code. The walk
// stopped there and read the rest by name, while a shell part of the way down
// writes to it by a shorter relative name, through any link on the way (US-035,
// security review). A long word within the length is placed as any other.
func TestAC5AC7AWordWhoseFullPathIsTooLongIsRefusedUnderItsOwnCode(t *testing.T) {
	long := strings.Repeat("b", 100) + "/lnk/active"
	bound := func(word string) error {
		if strings.HasPrefix(word, strings.Repeat("b", 100)+"/") {
			return pathrules.ErrPathTooLong
		}
		return nil
	}
	for _, command := range []string{"echo x > " + long, "touch " + long} {
		t.Run("AC-7 "+command[:12], func(t *testing.T) {
			s, _ := lbState(nil)
			s.Bound = bound
			f, refused := Inspect(command, s)
			if !refused || f.Code != "SDLC-E0083" || f.Rule != "path-too-long" ||
				!strings.HasPrefix(f.Reason, "the full path of ") || !strings.Contains(f.Message(), "shorter") {
				t.Errorf("refused %v, %q; want it refused under SDLC-E0083 [path-too-long]", refused, f.Message())
			}
			if len(f.Message()) > 2048 {
				t.Errorf("the refusal is %d bytes; the word is not shortened", len(f.Message()))
			}
		})
	}
	t.Run("AC-11 a long word within the length", func(t *testing.T) {
		s, _ := lbState(nil)
		s.Bound = bound
		if f, refused := Inspect("touch "+strings.Repeat("c", 50)+"/x", s); refused {
			t.Errorf("refused: %q", f.Message())
		}
	})
}

// AC-5 (SDLC-E0083): where the path too long is the working directory, the
// route is a cd to a shorter path. Renaming the directories on the way, the
// route for a word, would move the place the person is working in, when the
// command runs as well from any shorter spelling of it (US-035, landing round).
func TestAC5AWorkingDirectoryPastTheLengthIsRoutedToACdToAShorterPath(t *testing.T) {
	s, _ := lbState(nil)
	s.DirBound = pathrules.ErrPathTooLong
	s.Bound = func(word string) error {
		if word == "" {
			return pathrules.ErrPathTooLong
		}
		return nil
	}
	f, refused := Inspect("echo ok", s)
	if !refused || f.Code != "SDLC-E0083" || !strings.HasPrefix(f.Reason, "the full path of the working directory") {
		t.Fatalf("refused %v, %q; want it refused under SDLC-E0083 for the working directory", refused, f.Message())
	}
	if !strings.Contains(f.Route, "cd to a directory with a shorter path") {
		t.Errorf("the route does not say to cd to a directory with a shorter path: %q", f.Route)
	}
	if strings.Contains(f.Route, "renam") {
		t.Errorf("the route still says to rename directories: %q", f.Route)
	}
}

// AC-5 (THREATS P1): a refusal by the rule for the person's Claude Code
// configuration quotes a long word shortened, as the link-bound refusals do.
// It quoted the word whole, and a word can be kilobytes long, all of it sent
// back to the model (US-035, landing round).
func TestAC5ARefusalForThePersonsConfigurationQuotesALongWordShortened(t *testing.T) {
	word := "~/.claude/" + strings.Repeat("abcdefghi/", 29) + "settings.json"
	for _, command := range []string{"touch " + word, "echo x > " + word} {
		f, refused := Inspect(command, claudeConfigState())
		if !refused || f.Rule != "claude-config-is-the-persons" {
			t.Errorf("%.30s...: refused %v under %q; want it refused under claude-config-is-the-persons", command, refused, f.Rule)
			continue
		}
		if strings.Contains(f.Message(), word) {
			t.Errorf("%.30s...: the refusal quotes the %d-byte word whole", command, len(word))
		}
		if !strings.Contains(f.Reason, word[:20]) || !strings.Contains(f.Reason, "...") {
			t.Errorf("%.30s...: the refusal does not quote the word shortened: %q", command, f.Reason)
		}
	}
	short := "~/.claude/settings.json"
	if f, refused := Inspect("touch "+short, claudeConfigState()); !refused || !strings.Contains(f.Reason, short) {
		t.Errorf("AC-11: a short word is still quoted whole: refused %v, %q", refused, f.Reason)
	}
}

// AC-5 (SDLC-E0081): where the working directory is past a bound on links,
// the route is a cd to a directory that names the place without the links.
// Naming "the place it leads to directly", the route for a word, does not say
// that the place to name is the one the command runs in (US-035, landing
// round).
func TestAC5AWorkingDirectoryPastALinkBoundIsRoutedToACdThatNamesThePlaceWithoutTheLinks(t *testing.T) {
	for name, err := range map[string]error{"past the lookup limit": pathrules.ErrLookupLimit, "past 64 links": pathrules.ErrTooManyLinks} {
		t.Run("AC-5 "+name, func(t *testing.T) {
			s, _ := lbState(nil)
			s.DirBound = err
			s.Bound = func(word string) error {
				if word == "" {
					return err
				}
				return nil
			}
			f, refused := Inspect("echo ok", s)
			if !refused || f.Code != lbWordCode || !strings.HasPrefix(f.Reason, "the working directory") {
				t.Fatalf("refused %v, %q; want it refused under %s for the working directory", refused, f.Message(), lbWordCode)
			}
			const want = "cd to a directory with a shorter path, one that names the place without the links"
			if !strings.Contains(f.Route, want) {
				t.Errorf("the route does not say %q: %q", want, f.Route)
			}
		})
	}
}

// lbHomeCds are the spellings of a cd to the person's home, or below it, that
// the shell reads from HOME, which the same command can set first.
var lbHomeCds = []string{
	"cd", "chdir", "builtin cd", "command cd",
	"cd ~", "cd -- ~", "pushd ~", "cd -L ~", "cd ~/sub",
	"cd $HOME", "cd ${HOME}", `cd "$HOME"`, "cd $HOME/sub", "pushd $HOME", "cd -P $HOME",
}

// lbHomeSet are what the command does to HOME before its cd: nothing, or set
// it to the loop's own directory or below.
var lbHomeSet = []string{
	"",
	"export HOME=$PWD/.sdlc && ",
	"HOME=$PWD/.sdlc; ",
	"export HOME=/work/project/.sdlc/state; ",
}

// AC-5, AC-7 and AC-11: a cd to the person's home leaves the shell where the
// hook cannot place it, as on trunk (US-120's AC-3), so a glob write or a write
// to a held name after it is refused. Read as the home, the place the
// configuration checks are asked about, a bare cd, `cd $HOME` and their kin
// were placed there, US-120's unplaced-cd rules were skipped, and `export
// HOME=$PWD/.sdlc && cd && rm -rf state` took the loop's state away (US-035,
// verifier round 9). Reading the home stays: `ls`, and a write to an ordinary
// name, after the same cd are allowed. Below the home, as on trunk, the walk
// is somewhere under sub, where only a glob is refused: no held name ends in
// sub/state.
func TestAC5AC7AC11AfterACdToTheHomeAWriteToAHeldNameIsRefusedAsOnTrunk(t *testing.T) {
	s := agHeldState().WithDisk(Disk{Stories: func() Listing { return u120Listed("A-1") }})
	for _, cd := range lbHomeCds {
		thens := []string{"rm -f *", "rm -rf state", "touch stories/A-1/PLAN.md"}
		if strings.HasSuffix(cd, "/sub") {
			thens = thens[:1]
		}
		for _, set := range lbHomeSet {
			for _, then := range thens {
				command := set + cd + " && " + then
				t.Run("AC-11 "+command, func(t *testing.T) {
					f, refused := inspectNoPanic(t, command, s)
					if !refused || !strings.Contains(f.Reason, "which the hook cannot place because") {
						t.Errorf("refused %v, %q; want it refused for a cd the hook cannot place", refused, f.Message())
					}
				})
			}
		}
		t.Run("AC-11 "+cd+" && ls", func(t *testing.T) { u120Allowed(t, "AC-11", cd+" && ls", s) })
	}
	for _, command := range []string{"cd ~ && touch notes.txt", "cd && touch notes.txt", "cd ~/sub && touch x", "cd $HOME && touch notes.txt"} {
		t.Run("AC-11 "+command, func(t *testing.T) { u120Allowed(t, "AC-11", command, s) })
	}
	// PowerShell's own spellings of a cd to the home: Set-Location and its
	// alias sl, bare or to $HOME, and $env:HOME set first.
	ps := s
	ps.PowerShell = true
	for _, cd := range []string{"Set-Location", "sl", "Set-Location $HOME", "sl ~"} {
		for _, set := range []string{"", "$env:HOME = \"$PWD/.sdlc\"; "} {
			for _, then := range []string{"Remove-Item -Recurse -Force state", "Remove-Item *", "New-Item stories/A-1/PLAN.md"} {
				command := set + cd + "; " + then
				t.Run("AC-11 PowerShell "+command, func(t *testing.T) {
					f, refused := inspectNoPanic(t, command, ps)
					if !refused || !strings.Contains(f.Reason, "which the hook cannot place because") {
						t.Errorf("refused %v, %q; want it refused for a cd the hook cannot place", refused, f.Message())
					}
				})
			}
		}
		t.Run("AC-11 PowerShell "+cd+"; Get-ChildItem", func(t *testing.T) { u120Allowed(t, "AC-11", cd+"; Get-ChildItem", ps) })
	}
}

// lbDiskBound is a Bound for which a word naming lnk65 is past 64 links, and
// with spent, every word is past the command's budget.
func lbDiskBound(spent bool) func(string) error {
	return func(word string) error {
		switch {
		case spent:
			return pathrules.ErrLinkBudget
		case strings.Contains(word, "lnk65"):
			return pathrules.ErrTooManyLinks
		}
		return nil
	}
}

// AC-6 and AC-10: a word past a bound on links, or asked once the command's
// budget is spent, is not read from the disk: no directory is listed for a
// glob, no archive's members are read, and the disk is not asked whether it is
// a directory. The system follows a path's links again on every read, outside
// the budget, and 2,000 globs below one chain of 31 links held the hook for six
// seconds after the budget was spent (US-035, landing round). The command is
// refused for the bound. Within every bound, the disk is read as US-120's rows
// expect.
func TestAC6AC10AWordPastABoundIsNotReadFromTheDisk(t *testing.T) {
	newDisk := func() *u120Disk {
		return &u120Disk{
			entries:  map[string]Listing{"build": u120Listed("x.txt"), "lnk65": u120Listed("x.txt")},
			archives: map[string]Listing{"/tmp/n.tar": u120Listed("notes/a.txt"), "lnk65/n.tar": u120Listed("notes/a.txt")},
		}
	}
	for _, tc := range []struct {
		name, command, kind string
		spent               bool
	}{
		{"a glob below a word past 64 links", "rm -f lnk65/*", "entries", false},
		{"an archive past 64 links", "tar -xf lnk65/n.tar -C .", "archive", false},
		{"a destination past 64 links", "cp -r /tmp/p/.sdlc lnk65/v2", "isdir", false},
		{"a glob once the budget is spent", "rm -f build/*", "entries", true},
		{"an archive once the budget is spent", "tar -xf /tmp/n.tar -C .", "archive", true},
		{"a destination once the budget is spent", "cp -r /tmp/p/.sdlc notes.v2", "isdir", true},
	} {
		t.Run("AC-6 "+tc.name, func(t *testing.T) {
			d := newDisk()
			s := agHeldState().WithDisk(d.disk(u120Formats...))
			s.Bound = lbDiskBound(tc.spent)
			if f, refused := inspectNoPanic(t, tc.command, s); !refused {
				t.Errorf("%q is allowed; want it refused for the bound", tc.command)
			} else if f.Route == "" {
				t.Errorf("%q: the refusal says nothing to do next: %+v", tc.command, f)
			}
			u120NotAsked(t, "AC-6", tc.command, d, tc.kind)
		})
	}
	for command, question := range map[string]string{
		"rm -f build/*":               "entries build",
		"tar -xf /tmp/n.tar -C .":     "archive /tmp/n.tar",
		"cp -r /tmp/p/.sdlc notes.v2": "isdir notes.v2",
	} {
		t.Run("AC-11 within every bound, "+command, func(t *testing.T) {
			d := newDisk()
			s := agHeldState().WithDisk(d.disk(u120Formats...))
			s.Bound = lbDiskBound(false)
			u120Allowed(t, "AC-11", command, s)
			u120Asked(t, "AC-11", command, d, question)
		})
	}
}

// lbCdpathRows are refused and allowed rows for the CDPATH a relative cd
// searches, with <L> for the loop's directory spelt relative to the project
// (US-035, code review round 5, unfreeze 12). The hook asks the same of a
// project on disk, in internal/hook.
var lbCdpathRows = struct{ refused, allowed []string }{
	refused: []string{
		// A relative entry is searched from the project's root.
		"CDPATH=<L> cd state && rm -f active",
		"CDPATH=./<L> cd state && rm -f active",
		"CDPATH=:<L> cd state && rm -f active",
		"CDPATH=x:<L> cd state && touch active",
		"CDPATH=<L>; cd state && rm -f active",
		"export CDPATH=<L>; cd state && echo x > active",
		"declare -x CDPATH=<L>; cd state && rm -f active",
		"env CDPATH=<L> bash -c 'cd state && rm -f active'",
		"CDPATH=<L> cd stories && rm -f -r x",
		`CDPATH="<L>" cd state && rm -f active`,
		// After a cd the hook cannot place, a relative entry could be
		// anywhere.
		"D=x; cd $D && CDPATH=<L> cd state && rm -f active",
		"D=x; cd $D && CDPATH=x cd build && touch y",
		// A CDPATH with no relative entry leaves such a cd where it was.
		"D=x; cd $D && CDPATH=: cd state && rm -f active",
		"D=x; cd $D && CDPATH=/nonexistent cd state && rm -f active",
		// A mention the hook cannot read as a value: an assigning
		// expansion, an element, a command that may set it, a quote left
		// open, a value that runs on past its quote, a tilde, or a word that
		// only names it.
		"CDPATH=${CDPATH:=<L>} cd state && rm -f active",
		"CDPATH=${CDPATH=<L>} cd state && rm -f active",
		"CDPATH[0]=<L>; cd state && rm -f active",
		"typeset -T CDPATH cdpath; cdpath=(<L>); cd state && rm -f active",
		"CDPATH='<L> cd state && rm -f active",
		"CDPATH='/x'y; cd state && rm -f active",
		"CDPATH=~/x cd build && touch y",
		"grep -rn cdpath internal > notes.txt; cd build && touch stamp",
		"grep -rn CDPATH internal > notes.txt; cd build && touch stamp",
		// A cd in a quote the walk does not follow still moves the walk
		// that reads no quotes, whatever the escaped dollar before it.
		"echo 'a; cd <L>' && rm -f state/active",
		"echo 'a; cd <L>' && env -C state rm -f active",
		`echo \$'\' && cd <L> && rm -rf state && echo '\'`,
		// A cd in a script handed to a shell inside a script is one the walk
		// does not follow.
		`bash -c "bash -c 'cd <L>/state && rm -f active'"`,
		`bash -c "true; sh -c 'cd /tmp; rm -f active'"`,
	},
	allowed: []string{
		"CDPATH=<L> cd build",
		"CDPATH=x cd build && rm -rf out",
		"CDPATH=build cd out && rm -f y",
		"D=x; cd $D && CDPATH=x cd build && ls",
		"unset CDPATH; cd build && rm -rf out",
		"unset -v CDPATH; cd build && touch y",
		"cd internal/shellpolicy && rm -f cdpath.go.orig",
		"echo ${#CDPATH} && cd build && touch y",
		"echo $CDPATH && cd build && rm -f x",
		"echo ${CDPATH} && cd build && touch y",
		"echo ${CDPATH:-x} && cd build && touch y",
		"cd build && rm -rf out; export CDPATH=/nonexistent",
		`echo \$'x' && cd build && touch y`,
		"(cd build && make) && rm -rf out",
		"bash -c 'cd build && make' && (cd build && touch y)",
		"echo 'a; cd build' && (cd out && touch y)",
		"bash -c 'cd build && make' && env -C out touch y",
		`bash -c "sh -c 'cd build && ls'"`,
	},
}

// AC-5, AC-7 and AC-11 (code review round 5): the CDPATH a relative cd
// searches is read as the shell reads it, wherever its entries lead. A
// relative entry was dropped at the project's root, so `CDPATH=.sdlc cd state
// && rm -f active` was allowed, as it is on trunk. A mention of CDPATH the
// hook cannot read as a value refuses the relative writes after it; a read,
// an unset or a longer word that holds the name changes nothing.
func TestAC5AC7AC11TheCdpathARelativeCdSearchesIsReadAsTheShellReadsIt(t *testing.T) {
	s := agHeldState()
	for _, row := range lbCdpathRows.refused {
		command := strings.ReplaceAll(row, "<L>", ".sdlc")
		t.Run("AC-7 "+row, func(t *testing.T) {
			if f, refused := inspectNoPanic(t, command, s); !refused {
				t.Errorf("%q is allowed; want it refused", command)
			} else if f.Route == "" {
				t.Errorf("%q: the refusal says nothing to do next: %+v", command, f)
			}
		})
	}
	for _, row := range lbCdpathRows.allowed {
		command := strings.ReplaceAll(row, "<L>", ".sdlc")
		t.Run("AC-11 "+row, func(t *testing.T) { u120Allowed(t, "AC-11", command, s) })
	}
	t.Run("AC-5 the route for a relative entry after an unplaced cd", func(t *testing.T) {
		f, refused := inspectNoPanic(t, "D=x; cd $D && CDPATH=x cd build && touch y", s)
		if want := "spell each CDPATH entry as an absolute path"; !refused || !strings.Contains(f.Message(), want) {
			t.Errorf("refused %v; the refusal does not say %q: %s", refused, want, f.Message())
		}
	})
}

// AC-11 (code review round 5): a backslash before a dollar escapes it, so the
// quote after it opens a plain quote, in which a backslash escapes nothing.
// Read at the quote, `\$'x'` was taken for a `$'...'` string, in which `\'`
// does not close it. In PowerShell a backslash escapes nothing and `$'` opens
// no such string.
func TestAC11AnEscapedDollarBeforeAQuoteOpensAPlainQuote(t *testing.T) {
	for _, tc := range []struct {
		text       string
		powerShell bool
		want       byte
	}{
		{`echo \$'\' x`, false, 0},
		{`echo \$'x`, false, '\''},
		{`echo $'\' x`, false, ansiQuote},
		{`echo \\$'x`, false, ansiQuote},
		{`echo \$'x' && cd build`, false, 0},
		{`echo \$'x'`, true, 0},
	} {
		t.Run("AC-11 "+tc.text, func(t *testing.T) {
			if got := quoteAfter(tc.text, 0, tc.powerShell); got != tc.want {
				t.Errorf("quoteAfter(%q, PowerShell %v) = %d, want %d", tc.text, tc.powerShell, got, tc.want)
			}
		})
	}
}

// lbWithCdpath is s with CDPATH in the environment the session started in
// set to value, or s itself where value is "".
func lbWithCdpath(s State, value string) State {
	if value != "" {
		s.Carried = func(name string) string {
			if name == "CDPATH" {
				return value
			}
			return ""
		}
	}
	return s
}

// lbRoundTrips is n of `cd <dir> && make; cd ..; `, with dir the directory
// each names.
func lbRoundTrips(n int, dir func(i int) string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "cd %s && make; cd ..; ", dir(i))
	}
	return b.String()
}

// AC-5, AC-6, AC-7 and AC-11 (verifier rounds 12 and 13, unfreeze 13): the
// places a cd that searches CDPATH may leave the shell in are bounded, and
// counted as places. After a cd the hook cannot place, ten or twenty cds to
// names of their own, each searching two entries, pass the bound, and a
// relative write after them is refused for the places; three stay within it.
// Ten to the same name add no place after the first where CDPATH holds `.`
// after its entries, since every shell then finds /a/x again from /a/x, and
// are allowed, as on trunk (option B, unfreeze 14). Without the `.`, zsh tries
// its own directory first and lands in /a/x/x (bash 5.3 and zsh 5.9, unfreeze
// 15), so 20 placed cds to build, each followed by a write, pass the bound. A
// cd into a directory and back, with CDPATH in the environment or on the
// line, leaves the shell in one of two places however often it is done, in
// either shell, since `cd ..` searches no CDPATH: counted as entries, eight
// round trips were refused, where trunk allows them. The hook asks the same
// of a project on disk, in internal/hook.
func TestAC5AC6AC7AC11TheCdpathPlaceBoundCountsPlacesNotEntries(t *testing.T) {
	s := agHeldState()
	distinct := func(n int) string {
		var b strings.Builder
		for i := 1; i <= n; i++ {
			fmt.Fprintf(&b, "CDPATH=/a:/b cd x%d; ", i)
		}
		return b.String()
	}
	refused := map[string]string{
		"20 placed CDPATH cds to build with no . entry": strings.Repeat("CDPATH=/a:/b cd build && touch y; ", 20),
	}
	for _, n := range []int{10, 20} {
		refused[fmt.Sprintf("%d CDPATH cds to names of their own after an unplaced cd", n)] = "D=x; cd $D; " + distinct(n) + "touch y"
	}
	for name, cmd := range refused {
		t.Run("AC-7 "+name, func(t *testing.T) {
			f, refused := inspectNoPanic(t, cmd, s)
			if want := "more places than the hook follows"; !refused || !strings.Contains(f.Reason, want) {
				t.Errorf("refused %v; want it refused saying %q: %s", refused, want, f.Message())
			}
		})
	}
	for _, cmd := range []string{
		"D=x; cd $D; " + distinct(3) + "touch y",
		"D=x; cd $D; " + strings.Repeat("CDPATH=/a:/b:. cd x; ", 10) + "touch y",
	} {
		t.Run("AC-11 "+cmd, func(t *testing.T) { u120Allowed(t, "AC-11", cmd, s) })
	}
	// A relative entry of the session's own CDPATH, searched after an
	// unplaced cd, could be anywhere too; the reason says the CDPATH is the
	// session's, since the line sets none.
	t.Run("AC-7 a relative entry the session carries, after an unplaced cd", func(t *testing.T) {
		f, refused := inspectNoPanic(t, "D=x; cd $D && cd build && touch y", lbWithCdpath(s, "lib"))
		if want := "the CDPATH the session started with"; !refused || !strings.Contains(f.Reason, want) {
			t.Errorf("refused %v; want it refused saying %q: %s", refused, want, f.Message())
		}
	})
	same := func(int) string { return "a" }
	each := func(i int) string { return fmt.Sprintf("s%d", i) }
	for _, row := range []struct{ name, cmd, cdpath string }{
		{"two CDPATH cds after an unplaced cd", "D=x; cd $D; CDPATH=/a:/b cd x; CDPATH=/a:/b cd x; touch y", ""},
		{"8 round trips, CDPATH in the environment", lbRoundTrips(8, same) + "touch stamp", "/w"},
		{"20 round trips, CDPATH in the environment", lbRoundTrips(20, same) + "touch stamp", "/w"},
		{"8 round trips, CDPATH exported on the line", "export CDPATH=/w; " + lbRoundTrips(8, same) + "touch stamp", ""},
		{"8 different directories, CDPATH in the environment", lbRoundTrips(8, each) + "touch stamp", "/w"},
		{"8 round trips, CDPATH of . and a home", lbRoundTrips(8, same) + "touch stamp", ".:/home/dev"},
		{"7 round trips", lbRoundTrips(7, same) + "touch stamp", "/w"},
		{"20 subshells", strings.Repeat("(cd a && make); ", 20) + "touch stamp", "/w"},
		{"20 placed CDPATH cds", strings.Repeat("CDPATH=/a:/b:. cd build && touch y; ", 20), ""},
	} {
		t.Run("AC-11 "+row.name, func(t *testing.T) { u120Allowed(t, "AC-11", row.cmd, lbWithCdpath(s, row.cdpath)) })
	}
}

// AC-5 and AC-7 (verifier round 13): after a cd the hook cannot place, a cd
// to .claude that searches CDPATH could still be the project's .claude, where
// the shell goes when no entry holds it; round 12 allowed a program run there.
func TestAC5AC7AfterAnUnplacedCdACdpathCdToClaudeIsStillTheProjects(t *testing.T) {
	s := agHeldState()
	for _, cmd := range []string{
		"D=x; cd $D && CDPATH=/tmp cd .claude && python3 tool.py x",
		"D=x; cd $D && CDPATH=/nonexistent cd .claude && python3 tool.py x",
	} {
		t.Run("AC-7 "+cmd, func(t *testing.T) {
			if _, refused := inspectNoPanic(t, cmd, s); !refused {
				t.Errorf("%q is allowed; want it refused", cmd)
			}
		})
	}
}

// AC-5 (verifier rounds 12 and 13): a refusal names the operand the command
// writes, read after its options and after `--`, never an option, and the
// refusal for a line that only names CDPATH says so, not that it sets it.
func TestAC5ARefusalNamesTheOperandAndSaysALineOnlyNamesCdpath(t *testing.T) {
	s := agHeldState()
	for _, row := range []struct{ cmd, names, notNames string }{
		{"cd .sdlc/stories && rm -rf x", ".sdlc/stories/x", ".sdlc/stories/-"},
		{"CDPATH=.sdlc cd stories && rm -f -r x", ".sdlc/stories/x", ".sdlc/stories/-"},
		{"cd .sdlc/stories && rm -rf -- -x", ".sdlc/stories/-x", ".sdlc/stories/-r"},
		{"grep -rn cdpath internal > notes.txt; cd build && touch stamp", "names CDPATH", "sets CDPATH"},
	} {
		t.Run("AC-5 "+row.cmd, func(t *testing.T) {
			f, refused := inspectNoPanic(t, row.cmd, s)
			if !refused {
				t.Fatalf("%q is allowed; want it refused", row.cmd)
			}
			if !strings.Contains(f.Reason, row.names) || strings.Contains(f.Reason, row.notNames) {
				t.Errorf("the reason should say %q and not %q: %s", row.names, row.notNames, f.Reason)
			}
		})
	}
}

// lbLinkedState is agHeldState in which sub/x/x/lc is a link into the loop's
// stories, so that only a walk standing in sub/x/x reaches the loop's state
// through it: no name it writes is held.
func lbLinkedState() State {
	s := agHeldState()
	resolve := s.Resolve
	s.Resolve = func(word string) string {
		r := resolve(word)
		if rest, ok := strings.CutPrefix(r, "sub/x/x/lc"); ok {
			return ".sdlc/stories" + rest
		}
		return r
	}
	return s
}

// AC-5 and AC-7 (verifier round 14, unfreeze 16): after a cd the hook cannot
// place, a place a cd that searches CDPATH certainly leaves for an entry's
// directory is dropped, and only then. Each row breaks one of the conditions
// reachedFirst holds it to, so the shell, standing in <root>/sub/x, can go on
// to <root>/sub/x/x and write through lc into the loop's stories, which only
// the place catches. The same CDPATH twice drops the place and is allowed, as
// is one cd. The hook asks the same of a link on disk, in internal/hook.
func TestAC5AC7ACdpathPlaceIsDroppedOnlyWhereTheShellCertainlyLeavesIt(t *testing.T) {
	s := lbLinkedState()
	sub := s.Root + "/sub"
	first := "D=x; cd $D; CDPATH=" + sub + ":. cd x; "
	exported := "D=x; cd $D; export CDPATH=" + sub + ":.; cd x; "
	for _, row := range []struct{ name, cmd, session string }{
		{"S1, the second cd's own CDPATH lacks the entry", first + "CDPATH=/nonexistent:. cd x; touch lc/new.json", ""},
		{"S1, a later export lacks the entry", exported + "export CDPATH=/nonexistent:.; cd x; touch lc/new.json", ""},
		{"S2, an empty entry first", first + "CDPATH=:" + sub + ":. cd x; touch lc/new.json", ""},
		{"S2b, . first", first + "CDPATH=.:" + sub + ":. cd x; touch lc/new.json", ""},
		{"S3, unset CDPATH between", exported + "unset CDPATH; cd x; touch lc/new.json", ""},
		{"S3b, CDPATH= in front of the second cd", exported + "CDPATH= cd x; touch lc/new.json", ""},
		// The session's CDPATH the same as the line's, so that only the unset
		// or the empty assignment keeps the place.
		{"S3, unset CDPATH between, the session's CDPATH the same", exported + "unset CDPATH; cd x; touch lc/new.json", sub + ":."},
		{"S3b, CDPATH= in front of the second cd, the session's CDPATH the same", exported + "CDPATH= cd x; touch lc/new.json", sub + ":."},
		{"only the session's CDPATH", "D=x; cd $D; cd x; cd x; touch lc/new.json", sub + ":."},
	} {
		t.Run("AC-7 "+row.name, func(t *testing.T) {
			if _, refused := inspectNoPanic(t, row.cmd, lbWithCdpath(s, row.session)); !refused {
				t.Errorf("%q is allowed; the shell can reach lc/new.json, in the loop's stories", row.cmd)
			}
		})
	}
	for _, cmd := range []string{
		first + "CDPATH=" + sub + ":. cd x; touch lc/new.json",
		first + "touch lc/new.json",
	} {
		t.Run("AC-11 "+cmd, func(t *testing.T) { u120Allowed(t, "AC-11", cmd, s) })
	}
}

// AC-7 (verifier round 14, unfreeze 16): reachedFirst, one case for each
// condition under which a shell standing in /r/sub/x certainly leaves it on a
// cd to x, for the entry /r/sub: in every value; only absolute entries before
// it; an empty or `.` entry after it; and CDPATH neither unset nor assigned
// empty on the line, nor read in a way the hook cannot follow. No value at
// all, the session's alone, is never enough.
func TestAC7ReachedFirstHoldsAPlaceOnlyUnderEveryCondition(t *testing.T) {
	const q, to = "/r/sub/x", "x"
	for _, tc := range []struct {
		name string
		p    searchPath
		want bool
	}{
		{"every condition holds", searchPath{values: [][]string{{"/r/sub", "."}}}, true},
		{"an absolute entry before it", searchPath{values: [][]string{{"/a", "/r/sub", ""}}}, true},
		{"1: a second value lacks the entry", searchPath{values: [][]string{{"/r/sub", "."}, {"/z", "."}}}, false},
		{"1: a second value is unset", searchPath{values: [][]string{{"/r/sub", "."}, nil}}, false},
		{"2: an empty entry before it", searchPath{values: [][]string{{"", "/r/sub", "."}}}, false},
		{"2: . before it", searchPath{values: [][]string{{".", "/r/sub", "."}}}, false},
		{"2: a relative entry before it", searchPath{values: [][]string{{"rel", "/r/sub", "."}}}, false},
		{"3: nothing after it", searchPath{values: [][]string{{"/r/sub"}}}, false},
		{"3: only absolute entries after it", searchPath{values: [][]string{{"/r/sub", "/b"}}}, false},
		{"4: unset or assigned empty on the line", searchPath{values: [][]string{{"/r/sub", "."}}, cleared: true}, false},
		{"read in a way the hook cannot follow", searchPath{values: [][]string{{"/r/sub", "."}}, unread: true}, false},
		{"no value the line assigns", searchPath{carried: []string{"/r/sub", "."}}, false},
		{"the place is no entry's", searchPath{values: [][]string{{"/r/other", "."}}}, false},
		{"a relative entry that matches", searchPath{values: [][]string{{"sub", "."}}}, false},
	} {
		t.Run("AC-7 "+tc.name, func(t *testing.T) {
			if got := tc.p.reachedFirst(q, to); got != tc.want {
				t.Errorf("reachedFirst(%q, %q) with %+v = %v, want %v", q, to, tc.p, got, tc.want)
			}
		})
	}
	if (searchPath{values: [][]string{{"sub", "."}}}).reachedFirst("sub/x", to) {
		t.Errorf("AC-7: a relative entry is the shell's own directory's child, and no place is certain")
	}
}
