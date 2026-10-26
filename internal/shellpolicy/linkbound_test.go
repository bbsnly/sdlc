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
		{[]string{"cd"}, ""},
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
