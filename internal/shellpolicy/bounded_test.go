package shellpolicy

// US-035: the parts of the classifier's link bounds that a whole command
// reaches only now and then. Every callback that places a word asks Bound
// first; mkdir with -p has each directory it makes asked; a command can pass a
// bound that no word was asked about; and the refusal quotes the word short
// and as it was typed.

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// bdCalls is every word each callback of bdState was asked, by callback.
type bdCalls map[string][]string

// bdState is a State whose every word-placing callback answers yes and notes
// the word, with Bound saying a word that starts with lnk is over the bound of
// one word and one that starts with spent is over the command's budget.
func bdState() (State, bdCalls, *[]string) {
	calls := bdCalls{}
	var asked []string
	note := func(name string) func(string) bool {
		return func(w string) bool { calls[name] = append(calls[name], w); return true }
	}
	return State{
		Resolve: func(w string) string { calls["Resolve"] = append(calls["Resolve"], w); return "/placed/" + w },
		Shared: func(w string) []Landing {
			calls["Shared"] = append(calls["Shared"], w)
			return []Landing{{}}
		},
		Outside:          func(w string, _ bool) bool { calls["Outside"] = append(calls["Outside"], w); return true },
		ClaudeConfig:     note("ClaudeConfig"),
		ClaudeConfigHeld: note("ClaudeConfigHeld"),
		AgentsConfig:     note("AgentsConfig"),
		AgentsConfigHeld: note("AgentsConfigHeld"),
		Checkouts: func() CheckoutSet {
			return CheckoutSet{Checkouts: []string{"/repo"}, Place: func(w string) []string {
				calls["Place"] = append(calls["Place"], w)
				return []string{"/placed/" + w}
			}}
		},
		Bound: func(w string) error {
			asked = append(asked, w)
			switch {
			case strings.HasPrefix(w, "lnk"), strings.HasPrefix(w, "-lnk"):
				return pathrules.ErrTooManyLinks
			case strings.HasPrefix(w, "spent"):
				return pathrules.ErrLinkBudget
			}
			return nil
		},
	}, calls, &asked
}

// bdAsk asks every word-placing callback of s about w, and reports which
// answered yes.
func bdAsk(s State, w string) map[string]bool {
	return map[string]bool{
		"Resolve":          s.Resolve(w) != "",
		"Shared":           s.Shared(w) != nil,
		"Outside":          s.Outside(w, false),
		"ClaudeConfig":     s.ClaudeConfig(w),
		"ClaudeConfigHeld": s.ClaudeConfigHeld(w),
		"AgentsConfig":     s.AgentsConfig(w),
		"AgentsConfigHeld": s.AgentsConfigHeld(w),
		"Place":            s.Checkouts().Place(w) != nil,
	}
}

func TestAC5AC7NoCallbackPlacesAWordPastTheBoundOfOneWord(t *testing.T) {
	t.Run("AC-5 a word past the bound is placed by none of them, and Bound is asked once", func(t *testing.T) {
		s, calls, asked := bdState()
		b := s.bounded()
		for range 2 {
			for name, yes := range bdAsk(b, "lnk/x") {
				if yes {
					t.Errorf("%s answered yes for a word past the bound of one word", name)
				}
			}
		}
		if len(calls) != 0 {
			t.Errorf("the callbacks were handed a word past the bound: %v", calls)
		}
		if !slices.Equal(*asked, []string{"lnk/x"}) {
			t.Errorf("Bound was asked %q, want lnk/x once for the command", *asked)
		}
	})

	t.Run("AC-5 a word within the bound, or once the budget is spent, is placed as before", func(t *testing.T) {
		for _, w := range []string{"notes.txt", "spent/x"} {
			s, calls, _ := bdState()
			for name, yes := range bdAsk(s.bounded(), w) {
				if !yes {
					t.Errorf("%s: %s did not answer as it would without a bound", w, name)
				}
				if !slices.Equal(calls[name], []string{w}) {
					t.Errorf("%s: %s was handed %q, want the word itself", w, name, calls[name])
				}
			}
		}
	})

	t.Run("AC-5 a word that begins with a dash is asked of the bound like any other, and no word at all is not asked", func(t *testing.T) {
		// After `--`, as `./-x` or as a redirect's target, a word that begins
		// with a dash is a file, and a chain of links can be named so. Skipped,
		// `touch -- -e65/x` went through 65 links and was allowed (US-035,
		// the verifier's first gap).
		s, calls, asked := bdState()
		b := s.bounded()
		if got := b.Resolve("-c"); got != "/placed/-c" {
			t.Errorf("Resolve(-c), within the bound, = %q, want it placed as before", got)
		}
		if got := b.Resolve("-lnk/x"); got != "" {
			t.Errorf("Resolve(-lnk/x), past the bound, = %q, want it placed nowhere", got)
		}
		if got := b.Resolve(""); got != "/placed/" {
			t.Errorf("Resolve(\"\") = %q, want it placed as before", got)
		}
		if !slices.Equal(*asked, []string{"-c", "-lnk/x"}) {
			t.Errorf("Bound was asked %q, want -c and -lnk/x, and not the empty word", *asked)
		}
		if !slices.Equal(calls["Resolve"], []string{"-c", ""}) {
			t.Errorf("Resolve was handed %q, want -c and the empty word, never -lnk/x", calls["Resolve"])
		}
	})

	t.Run("AC-11 the checkouts are kept, and a callback that is not set stays unset", func(t *testing.T) {
		s, _, _ := bdState()
		if got := s.bounded().Checkouts().Checkouts; !slices.Equal(got, []string{"/repo"}) {
			t.Errorf("Checkouts = %q, want [/repo]", got)
		}
		bare := State{Bound: s.Bound, Checkouts: func() CheckoutSet { return CheckoutSet{Checkouts: []string{"/repo"}} }}.bounded()
		if bare.Resolve != nil || bare.Shared != nil || bare.Outside != nil || bare.ClaudeConfig != nil ||
			bare.ClaudeConfigHeld != nil || bare.AgentsConfig != nil || bare.AgentsConfigHeld != nil {
			t.Error("a callback that was not set was set by the bound")
		}
		if bare.Checkouts().Place != nil {
			t.Error("a CheckoutSet with no Place was given one")
		}
		if (State{Bound: s.Bound}).bounded().Checkouts != nil {
			t.Error("Checkouts, not set, was set by the bound")
		}
	})

	t.Run("AC-11 with no Bound every word is placed as before", func(t *testing.T) {
		s, calls, _ := bdState()
		s.Bound = nil
		for name, yes := range bdAsk(s.bounded(), "lnk/x") {
			if !yes || !slices.Equal(calls[name], []string{"lnk/x"}) {
				t.Errorf("%s answered %v having been handed %q, with no Bound set", name, yes, calls[name])
			}
		}
	})
}

func TestAC7MkdirMakingItsParentsHasEachDirectoryAskedOfTheBound(t *testing.T) {
	refused := []string{
		"mkdir -p lnk/a",
		"mkdir --parents lnk/a",
		"mkdir -vp lnk/a",
		"mkdir -pm 755 lnk/a",
		"mkdir -m 755 -p lnk/a",
		"mkdir --mode 755 -p lnk/a",
		"mkdir --verbose -p lnk/a",
		"mkdir -p -- lnk/a",
		"mkdir -p notes lnk/a",
	}
	allowed := []string{
		"mkdir lnk/a",
		"mkdir -v lnk/a",
		"mkdir -m0p lnk/a",
		"mkdir -vm 755 lnk/a",
		"mkdir -p -m lnkmode notes",
		"mkdir -p --mode lnkmode notes",
		"mkdir -- -p lnk/a",
	}
	for _, command := range refused {
		t.Run("AC-7 "+command, func(t *testing.T) {
			s, _ := lbState(pathrules.ErrTooManyLinks)
			f, refused := Inspect(command, s)
			if !refused || f.Code != lbWordCode || !strings.HasPrefix(f.Reason, "lnk/a"+lbOver64) {
				t.Errorf("refused %v, %q: want lnk/a refused under %s", refused, f.Message(), lbWordCode)
			}
		})
	}
	for _, command := range allowed {
		t.Run("AC-7 "+command+" makes no parent through the link", func(t *testing.T) {
			s, asked := lbState(pathrules.ErrTooManyLinks)
			if f, refused := Inspect(command, s); refused {
				t.Errorf("refused, %q; asked %q", f.Message(), *asked)
			}
		})
	}

	t.Run("AC-7 a relative directory is asked from where a cd left the command", func(t *testing.T) {
		s, _ := lbState(pathrules.ErrTooManyLinks)
		s.Bound = func(w string) error {
			if strings.HasPrefix(w, "sub/lnk") || strings.HasPrefix(w, "/lnk") {
				return pathrules.ErrTooManyLinks
			}
			return nil
		}
		if _, refused := Inspect("cd sub && mkdir -p lnk/a", s); !refused {
			t.Error("cd sub && mkdir -p lnk/a was allowed; sub/lnk/a is past the bound")
		}
		if f, refused := Inspect("mkdir -p lnk/a", s); refused {
			t.Errorf("mkdir -p lnk/a, from the project, refused: %q", f.Message())
		}
		if _, refused := Inspect("cd sub && mkdir -p /lnk/a", s); !refused {
			t.Error("cd sub && mkdir -p /lnk/a was allowed; an absolute directory is asked as it is")
		}
	})

	t.Run("AC-11 with no Bound mkdir -p is allowed as before", func(t *testing.T) {
		s, _ := lbState(pathrules.ErrTooManyLinks)
		s.Bound = nil
		if f, refused := Inspect("mkdir -p lnk/a", s); refused {
			t.Errorf("refused: %q", f.Message())
		}
	})
}

func TestAC6AC12ACommandPastABoundNoWordWasAskedAboutIsRefusedUnderItsCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		code   string
		reason string
	}{
		{"the budget", pathrules.ErrLinkBudget, lbCmdCode, "the words of this command go through more than the "},
		{"the bound of one word", pathrules.ErrTooManyLinks, lbWordCode, "a path this command reaches" + lbOver64},
		{"an error that is neither", errors.New("the disk said no"), "", ""},
	} {
		t.Run("AC-12 "+tc.name, func(t *testing.T) {
			s, _ := lbState(nil)
			s.Bound = func(w string) error {
				if w == "" {
					return tc.err
				}
				return nil
			}
			f, refused := Inspect("ls", s)
			if tc.code == "" {
				if refused {
					t.Errorf("refused for an error that is no bound: %q", f.Message())
				}
				return
			}
			if !refused || f.Code != tc.code || !strings.HasPrefix(f.Reason, tc.reason) || !strings.Contains(f.Message(), tc.code) {
				t.Fatalf("refused %v, %q: want %q under %s", refused, f.Message(), tc.reason, tc.code)
			}
			if strings.Contains(f.Reason, "is the word") {
				t.Errorf("%q names a word, and no word was asked about", f.Reason)
			}
		})
	}
}

func TestAC5AWordPastWhatTheSystemFollowsInOneLookupIsRefusedSayingSo(t *testing.T) {
	s, _ := lbState(pathrules.ErrLookupLimit)
	f, refused := Inspect("touch lnk/x", s)
	want := "lnk/x goes through more links than this system follows in one lookup"
	if !refused || f.Code != lbWordCode || !strings.HasPrefix(f.Reason, want) {
		t.Errorf("refused %v, %q: want %q under %s", refused, f.Message(), want, lbWordCode)
	}
}

func TestAC5TheRefusalQuotesTheWordAsTheCommandTypedIt(t *testing.T) {
	for _, tc := range []struct{ key, line, want string }{
		{"lnk/x", "touch 'lnk/x'", "'lnk/x'"},
		{"lnk/x", "echo x 2>lnk/x;", "lnk/x"},
		{"lnk/notes.txt", "install -D notes.txt lnk", "lnk"},
		{"lnk/notes.txt", "cp notes.txt lnk lnk/deeper", "lnk"},
		{"sub/lnk", "touch a b", "sub/lnk"},
		// After a cd the rules join a word to the cd's directory, and that
		// directory, a word every such key is below, was quoted for it: `cd
		// sub && touch lnk65/x` said "sub goes through more than 64 links"
		// (US-035, the verifier's fourth round).
		{"sub/lnk65/x", "cd sub && touch lnk65/x", "lnk65/x"},
		{"sub/st41/active", "cd sub && touch st41/active", "st41/active"},
		{"sub/lnk65/y", "cd sub; echo x > lnk65/y", "lnk65/y"},
		{"/work/p/sub/lnk65/x", "cd /work/p/sub && touch lnk65/x", "lnk65/x"},
		{"sub/w17/n", "cd sub && touch w0/n w17/n", "w17/n"},
		{"sub/lnk65/x", "touch sub/lnk65/x", "sub/lnk65/x"},
		{"sub/lnk65/x", "pushd sub && touch lnk65/x", "lnk65/x"},
		{"sub/lnk65/x", "cd -L -- sub && touch lnk65/x", "lnk65/x"},
	} {
		if got := typed(tc.key, tc.line); got != tc.want {
			t.Errorf("AC-5: typed(%q, %q) = %q, want %q", tc.key, tc.line, got, tc.want)
		}
	}
}

func TestAC5ALongWordIsShortenedAtTheEdgesOfItsCharacters(t *testing.T) {
	if w := strings.Repeat("a", 64); shortened(w) != w {
		t.Errorf("AC-5: a word of 64 bytes was shortened to %q", shortened(w))
	}
	// One byte before and after puts a cut 30 bytes from either end inside a
	// three-byte character.
	w := "a" + strings.Repeat("€", 30) + "b"
	got := shortened(w)
	head, tail, ok := strings.Cut(got, "...")
	switch {
	case !ok || !utf8.ValidString(got):
		t.Errorf("AC-5: shortened(%q) = %q, want valid text around ...", w, got)
	case !strings.HasPrefix(w, head) || !strings.HasSuffix(w, tail):
		t.Errorf("AC-5: shortened(%q) = %q, not the word's own start and end", w, got)
	case len(head) > 30 || len(tail) > 30 || len(head) < 28 || len(tail) < 28:
		t.Errorf("AC-5: shortened(%q) kept %d and %d bytes, want 30 at most, cut back to a character's start", w, len(head), len(tail))
	}
}

func TestAC7ABundleOfShortOptionsHoldsAFlagOnlyBeforeAModesValue(t *testing.T) {
	for w, want := range map[string]bool{
		"-p": true, "-vp": true, "-pm": true, "-v": false, "-mp": false, "-m0p": false, "-": false,
	} {
		if got := bundles(w, 'p'); got != want {
			t.Errorf("AC-7: bundles(%q, 'p') = %v, want %v", w, got, want)
		}
	}
}
