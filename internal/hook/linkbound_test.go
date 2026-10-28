package hook

// US-035 (from US-052), AC-5 to AC-7 and AC-12: the hook bounds the work of
// placing a command's words, and a command refused at a bound is refused
// under a code a person can look up.
//
// Before this, a word through more than 64 links was read by name after the
// 64th and allowed; a word whose links outran what the OS follows in one
// lookup (macOS 32, Linux 40) was read by name past the first link it could
// not read, so a chain of 40 to 100 links into the loop's state was allowed
// for touch, tee, a redirect, install -D and mkdir -p alike; and nothing
// bounded the links a whole command could make the hook follow, so a command
// of sixty 255-link words came within a few words of the hook's deadline.
// SECURITY.md applies: these tests name the class, not a recipe.
//
// What the refusals say (AC-5, decided D2):
//
//	<word> goes through more than 64 links, more than sdlc follows in one path
//	route: name the place it leads to directly, without the links
//	SDLC-E0081 for a word over 64 links (AC-5, AC-7), SDLC-E0082 for a command
//	over its budget (AC-6), and the rule id in square brackets, as every
//	refusal of the hook ends
//
// pathrules.CommandLinks is the budget, the figure the plan derives (D4).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/bbsnly/sdlc/internal/pathrules"
)

// The words of AC-5, and the two codes of D2.
const (
	lbOver64      = " goes through more than 64 links, more than sdlc follows in one path"
	lbRoute       = "Instead: name the place it leads to directly, without the links"
	lbWordCode    = "SDLC-E0081"
	lbCommandCode = "SDLC-E0082"
)

// lbRuleID is the rule id every refusal of the hook carries.
var lbRuleID = regexp.MustCompile(`\[[a-z0-9]+(?:-[a-z0-9]+)*\]`)

// lbLink makes at a symlink to target, or skips where none can be made.
func lbLink(t *testing.T, target, at string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
}

// lbPastTheLookup is n, a number of links past what macOS (32) and Linux (40)
// follow in one lookup and within the bound of one word, or on Windows 64,
// the one number that is both there: Windows follows 63 reparse points in one
// lookup and took chains of 41 and 50 links in one go, so rows built to stop
// at the system's limit were placed where they lead and allowed (CI on
// 38e0ed0).
func lbPastTheLookup(n int) int {
	if runtime.GOOS == "windows" {
		return 64
	}
	return n
}

// lbAtOneName makes name in root the head of a chain of n links, each the next
// one's name and the last leading to target, which is relative to root; and
// returns the word name/file, which goes through all n at its first name.
func lbAtOneName(t *testing.T, root, name string, n int, target, file string) string {
	t.Helper()
	for i := range n {
		to := fmt.Sprintf("%s.%d", name, i+1)
		if i == n-1 {
			to = filepath.Join(root, filepath.FromSlash(target))
		}
		at := name
		if i > 0 {
			at = fmt.Sprintf("%s.%d", name, i)
		}
		lbLink(t, to, filepath.Join(root, at))
	}
	return name + "/" + file
}

// lbADirectoryAtATime makes n directories, name-1 to name-n, each holding one
// link, n, to the next, the last to target, which is relative to root; and
// returns the word name-1/n/.../n/file, which crosses one link in each.
func lbADirectoryAtATime(t *testing.T, root, name string, n int, target, file string) string {
	t.Helper()
	for i := 1; i <= n; i++ {
		write(t, root, fmt.Sprintf("%s-%d/.keep", name, i), "")
	}
	for i := 1; i <= n; i++ {
		to := filepath.Join(root, fmt.Sprintf("%s-%d", name, i+1))
		if i == n {
			to = filepath.Join(root, filepath.FromSlash(target))
		}
		lbLink(t, to, filepath.Join(root, fmt.Sprintf("%s-%d", name, i), "n"))
	}
	return fmt.Sprintf("%s-1/%s%s", name, strings.Repeat("n/", n), file)
}

// lbWithDotDot makes n links, each to a directory of its own by its absolute
// path, and returns the word k1/../k2/../.../kn/../file, which follows and
// leaves each in turn.
func lbWithDotDot(t *testing.T, root, name string, n int, file string) string {
	t.Helper()
	var word strings.Builder
	for i := 1; i <= n; i++ {
		write(t, root, fmt.Sprintf("%s-s%d/.keep", name, i), "")
		link := fmt.Sprintf("%s-k%d", name, i)
		lbLink(t, filepath.Join(root, fmt.Sprintf("%s-s%d", name, i)), filepath.Join(root, link))
		word.WriteString(link + "/../")
	}
	word.WriteString(file)
	return word.String()
}

// lbProject is a project held by the session, with a directory nothing
// protects for chains to lead to and the file a command copies from.
func lbProject(t *testing.T) string {
	t.Helper()
	root := loopProject(t)
	write(t, root, "elsewhere/.keep", "")
	write(t, root, "notes.txt", "notes\n")
	return root
}

// lbAsk puts command to the hook from the implementer and returns the reason
// it was refused for, or "" where it was allowed.
func lbAsk(t *testing.T, root, cmd string) string {
	t.Helper()
	r := call(t, command(root, "sdlc-implementer", cmd), noEnv)
	if !denied(r) {
		return ""
	}
	return r.HookSpecificOutput.PermissionDecisionReason
}

// lbRefusedForTheWord holds a reason to AC-5's refusal of word: the word as
// typed where it is short enough to quote whole, the limit, the route, the code
// and a rule id.
func lbRefusedForTheWord(t *testing.T, cmd, word, reason string) {
	t.Helper()
	if reason == "" {
		t.Errorf("%.80s... was allowed; a word in it goes through more than 64 links", cmd)
		return
	}
	want := lbOver64
	if len(word) <= 64 {
		want = word + lbOver64
	}
	for _, part := range []string{want, lbRoute, lbWordCode} {
		if !strings.Contains(reason, part) {
			t.Errorf("the refusal of %.80s... does not say %q:\n%s", cmd, part, reason)
		}
	}
	if !lbRuleID.MatchString(reason) {
		t.Errorf("the refusal of %.80s... names no rule id in square brackets:\n%s", cmd, reason)
	}
}

// AC-5 and AC-12: a word through 65 links is refused, however it crosses them
// and wherever they lead, under SDLC-E0081, naming the word as typed. A word
// through 30 is not.
func TestAC5AC12AWordThroughMoreThan64LinksIsRefusedNamingTheWordAndTheLimit(t *testing.T) {
	t.Run("AC-5 every link at its first name, to a directory nothing protects", func(t *testing.T) {
		root := lbProject(t)
		word := lbAtOneName(t, root, "lnk", 65, "elsewhere", "notes.txt")
		for _, cmd := range []string{"touch " + word, "cp notes.txt " + word, "echo x > " + word} {
			lbRefusedForTheWord(t, cmd, word, lbAsk(t, root, cmd))
		}
	})
	t.Run("AC-5 one link in each directory, to a directory nothing protects", func(t *testing.T) {
		root := lbProject(t)
		word := lbADirectoryAtATime(t, root, "d", 65, "elsewhere", "notes.txt")
		lbRefusedForTheWord(t, "touch "+word, word, lbAsk(t, root, "touch "+word))
	})
	t.Run("AC-5 a link and then dot-dot, sixty-five times", func(t *testing.T) {
		root := lbProject(t)
		word := lbWithDotDot(t, root, "k", 65, "notes.txt")
		lbRefusedForTheWord(t, "touch "+word, word, lbAsk(t, root, "touch "+word))
	})
	t.Run("AC-12 the sixty-fifth link leads into the loop's state", func(t *testing.T) {
		root := lbProject(t)
		word := lbAtOneName(t, root, "st", 65, ".sdlc/state", "active")
		lbRefusedForTheWord(t, "touch "+word, word, lbAsk(t, root, "touch "+word))
	})
	t.Run("AC-5 thirty links are within every platform's own limit", func(t *testing.T) {
		root := lbProject(t)
		for _, word := range []string{
			lbAtOneName(t, root, "near", 30, "elsewhere", "notes.txt"),
			lbADirectoryAtATime(t, root, "dir", 30, "elsewhere", "notes.txt"),
		} {
			if reason := lbAsk(t, root, "touch "+word); reason != "" {
				t.Errorf("touch %s, through 30 links to a place nothing protects, was refused:\n%s", word, reason)
			}
		}
	})
	// THREATS P1: the word goes back to the model as part of a tool result,
	// and a word of kilobytes is not echoed whole.
	t.Run("AC-5 a word of four kilobytes is not quoted whole", func(t *testing.T) {
		root := lbProject(t)
		word := lbAtOneName(t, root, "big", 65, "elsewhere", strings.Repeat("b/", 2048)+"notes.txt")
		reason := lbAsk(t, root, "touch "+word)
		lbRefusedForTheWord(t, "touch "+word, word, reason)
		if strings.Contains(reason, word) || len(reason) > 2048 {
			t.Errorf("the refusal quotes a word of %d bytes whole, in %d bytes", len(word), len(reason))
		}
	})
}

// AC-7 and AC-12: a program that walks a path one directory at a time is
// refused past 64 links, whatever it is, under the same code: mkdir -p, which
// no rule classifies, and install -D, which makes the directories it writes
// into.
func TestAC7AC12AProgramThatWalksADirectoryAtATimeIsRefusedPast64Links(t *testing.T) {
	root := lbProject(t)
	word := lbADirectoryAtATime(t, root, "walk", 65, "elsewhere", "made/deeper")
	for _, cmd := range []string{"mkdir -p " + word, "install -D notes.txt " + word + "/notes.txt"} {
		lbRefusedForTheWord(t, cmd, word, lbAsk(t, root, cmd))
	}
}

// AC-7 and AC-11 (decided D7): a chain past what the OS follows in one lookup
// and short of 64 links, into the loop's state, was allowed, because its
// placing stopped at the first link it could not read and the rest was read
// by name. These rows are new: they were allowed before. Each is refused now,
// under whichever rule refuses it, for every program that writes the word.
func TestAC7AChainIntoTheLoopsStatePastTheOSLimitAndWithin64IsNotLetThrough(t *testing.T) {
	for _, n := range []int{41, 50, 64} {
		t.Run(fmt.Sprintf("AC-7 %d links", n), func(t *testing.T) {
			root := lbProject(t)
			word := lbADirectoryAtATime(t, root, "gap", n, ".sdlc/state", "active")
			for _, cmd := range []string{
				"touch " + word,
				"echo x | tee " + word,
				"echo x > " + word,
				"install -D notes.txt " + word,
			} {
				if reason := lbAsk(t, root, cmd); reason == "" {
					t.Errorf("%s..., through %d links into the loop's state, was allowed", cmd[:min(len(cmd), 40)], n)
				}
			}
		})
	}
}

// lbWords makes count words in root, each through a chain of its own of n
// links at its first name, to the directory nothing protects.
func lbWords(t *testing.T, root, prefix string, count, n int) []string {
	t.Helper()
	words := make([]string, count)
	for i := range count {
		words[i] = lbAtOneName(t, root, fmt.Sprintf("%s%d", prefix, i), n, "elsewhere", "notes.txt")
	}
	return words
}

// AC-6 and AC-12: a command whose words follow more links than its budget is
// refused under SDLC-E0082, though no word of it is over 64. Each word is
// through 30 links, within every platform's own limit, so only the budget
// can refuse it.
func TestAC6AC12ACommandWhoseWordsFollowMoreLinksThanItsBudgetIsRefused(t *testing.T) {
	root := lbProject(t)
	words := lbWords(t, root, "w", pathrules.CommandLinks/30+2, 30)
	cmd := "touch " + strings.Join(words, " ")
	reason := lbAsk(t, root, cmd)
	if reason == "" {
		t.Fatalf("a command of %d words of 30 links each, %d links against a budget of %d, was allowed",
			len(words), 30*len(words), pathrules.CommandLinks)
	}
	if !strings.Contains(reason, lbCommandCode) {
		t.Errorf("the refusal of a command over its budget does not name %s:\n%s", lbCommandCode, reason)
	}
	if strings.Contains(reason, lbWordCode) {
		t.Errorf("the refusal of a command over its budget names %s, the code of a word over 64 links:\n%s", lbWordCode, reason)
	}
	if !lbRuleID.MatchString(reason) || !strings.Contains(reason, "Instead: ") {
		t.Errorf("the refusal of a command over its budget has no route or no rule id:\n%s", reason)
	}

	// An everyday command, tens of words a few links each, is untouched by
	// the budget (D4).
	t.Run("AC-6 an everyday command is within the budget", func(t *testing.T) {
		everyday := lbWords(t, root, "e", 20, 2)
		if reason := lbAsk(t, root, "touch "+strings.Join(everyday, " ")); reason != "" {
			t.Errorf("twenty words through two links each were refused:\n%s", reason)
		}
	})
	// THREATS T5: the budget does not take the place of a rule that refuses
	// the command under its own name in the part already read.
	t.Run("AC-6 a rule that names the command first keeps it", func(t *testing.T) {
		reason := lbAsk(t, root, "rm .sdlc/state/active; "+cmd)
		if !strings.Contains(reason, "[loop-state-through-the-tool]") {
			t.Errorf("removing the loop's state ahead of the words was not refused under loop-state-through-the-tool:\n%s", reason)
		}
	})
}

// AC-5, AC-7 and AC-12 through the hook, for a word that begins with a dash.
// After `--`, as `./-x` or as a redirect's target such a word is a file, and
// the shell rules skipped it: chains past the bound and in the 34-to-64 gap
// were allowed, and so was a write into the loop's state through one link
// named -x (US-035, the verifier's first gap). Options are still options.
func TestAC5AC7AC12AWordThatBeginsWithADashIsHeldToTheBoundThroughTheHook(t *testing.T) {
	const lookup = "more links than this system follows in one lookup"
	root := lbProject(t)
	over := lbAtOneName(t, root, "-e65", 65, "elsewhere", "x")
	gapLinks := lbPastTheLookup(50)
	gap := lbADirectoryAtATime(t, root, "-g", gapLinks, "elsewhere", "x")
	walk := lbADirectoryAtATime(t, root, "-h", 70, "elsewhere", "x")
	lbLink(t, filepath.Join(root, ".sdlc", "state"), filepath.Join(root, "-st"))

	for _, tc := range []struct{ cmd, word string }{
		{"touch -- " + over, over},
		{"touch ./" + over, "./" + over},
		{"echo x > " + over, over},
		{"cp notes.txt -- " + over, over},
	} {
		t.Run("AC-5 "+tc.cmd, func(t *testing.T) {
			lbRefusedForTheWord(t, tc.cmd, tc.word, lbAsk(t, root, tc.cmd))
		})
	}
	for _, cmd := range []string{"touch -- " + gap, "touch ./" + gap, "echo x > ./" + gap} {
		t.Run(fmt.Sprintf("AC-7 %s, through %d links", cmd[:16], gapLinks), func(t *testing.T) {
			reason := lbAsk(t, root, cmd)
			if !strings.Contains(reason, lbWordCode) || !strings.Contains(reason, lookup) {
				t.Errorf("%.40s..., through %d links one directory at a time: want it refused under %s saying %q, got:\n%s", cmd, gapLinks, lbWordCode, lookup, reason)
			}
		})
	}
	t.Run("AC-7 mkdir -p -- through 70 links, one directory at a time", func(t *testing.T) {
		cmd := "mkdir -p -- " + walk
		lbRefusedForTheWord(t, cmd, walk, lbAsk(t, root, cmd))
	})
	for _, cmd := range []string{"echo x > ./-st/active", "touch -- -st/active", "cp notes.txt ./-st/active"} {
		t.Run("AC-12 "+cmd, func(t *testing.T) {
			if reason := lbAsk(t, root, cmd); !strings.Contains(reason, "[loop-state-through-the-tool]") {
				t.Errorf("a write into the loop's state through the link -st was not refused under loop-state-through-the-tool:\n%s", reason)
			}
		})
	}
	for _, cmd := range []string{"touch -c notes.txt", "cp -r elsewhere copy", "rm -rf build"} {
		t.Run("AC-11 "+cmd+" is still allowed", func(t *testing.T) {
			if reason := lbAsk(t, root, cmd); reason != "" {
				t.Errorf("refused:\n%s", reason)
			}
		})
	}
}

// AC-5 and AC-12 through the hook, for the directory a cd moves to after
// `--`: it was never placed, so with -x a link to .sdlc `cd -- -x && rm
// state/active` was allowed, and so was a cd through 65 links (US-035, the
// verifier's second round).
func TestAC5AC12TheDirectoryACdMovesToAfterDashDashIsPlacedThroughTheHook(t *testing.T) {
	root := lbProject(t)
	lbLink(t, filepath.Join(root, ".sdlc"), filepath.Join(root, "-x"))
	over := strings.TrimSuffix(lbAtOneName(t, root, "-e65", 65, "elsewhere", "x"), "/x")
	for _, cmd := range []string{
		"cd -- -x && rm state/active",
		"pushd -- -x && rm state/active",
		"cd -L -- -x && rm state/active",
		"env -C -x rm state/active",
	} {
		t.Run("AC-12 "+cmd, func(t *testing.T) {
			if reason := lbAsk(t, root, cmd); !strings.Contains(reason, "[loop-state-through-the-tool]") {
				t.Errorf("a write into the loop's state through the link -x was not refused under loop-state-through-the-tool:\n%s", reason)
			}
		})
	}
	t.Run("AC-5 cd -- through 65 links", func(t *testing.T) {
		cmd := "cd -- " + over + " && touch y"
		if reason := lbAsk(t, root, cmd); !strings.Contains(reason, lbWordCode) || !strings.Contains(reason, lbOver64) {
			t.Errorf("%s: want it refused under %s, got:\n%s", cmd, lbWordCode, reason)
		}
	})
	t.Run("AC-11 cd -- elsewhere && touch y is still allowed", func(t *testing.T) {
		if reason := lbAsk(t, root, "cd -- elsewhere && touch y"); reason != "" {
			t.Errorf("refused:\n%s", reason)
		}
	})
}

// AC-5 through the hook: after a cd, the refusal names the word as the person
// typed it. The cd's directory, which every word joined to it is below, was
// quoted instead, and sent the person to a directory with no links in it:
// `cd sub && touch lnk65/x` said "sub goes through more than 64 links"
// (US-035, the verifier's fourth round). The cd's directory is named only
// where it alone holds the links, as `cd -- e65`. 41 links at one name is past
// what macOS (32) and Linux (40) follow in one lookup, and 64 past Windows' 63.
func TestAC5AfterACdTheRefusalNamesTheWordAsTypedThroughTheHook(t *testing.T) {
	const lookup = "goes through more links than this system follows in one lookup"
	root := lbProject(t)
	sub := filepath.Join(root, "sub")
	write(t, root, "sub/.keep", "")
	lbAtOneName(t, sub, "lnk65", 65, "../elsewhere", "x")
	st := fmt.Sprintf("st%d", lbPastTheLookup(41))
	lbAtOneName(t, sub, st, lbPastTheLookup(41), "../.sdlc/state", "active")
	lbAtOneName(t, root, "e65", 65, "elsewhere", "x")
	lbAtOneName(t, root, "-e65", 65, "elsewhere", "x")
	inSub, inRoot := make([]string, 18), make([]string, 18)
	for i := range 18 {
		inSub[i] = lbAtOneName(t, sub, fmt.Sprintf("w%d", i), 30, "../elsewhere", "n")
		inRoot[i] = lbAtOneName(t, root, fmt.Sprintf("v%d", i), 30, "elsewhere", "n")
	}
	for _, tc := range []struct{ cmd, want string }{
		{"cd sub && touch lnk65/x", "lnk65/x" + lbOver64},
		{"cd sub && touch " + st + "/active", st + "/active " + lookup},
		{"cd sub; echo x > lnk65/y", "lnk65/y" + lbOver64},
		{"cd " + filepath.ToSlash(sub) + " && touch lnk65/x", "lnk65/x" + lbOver64},
		{"touch sub/lnk65/x", "sub/lnk65/x" + lbOver64},
		{"cd -- e65 && touch y", "e65" + lbOver64},
		{"cd -- -e65 && touch y", "-e65" + lbOver64},
		{"cd sub && touch " + strings.Join(inSub, " "), ", and w17/n is the word that took it past"},
		{"cd sub && cd .. && touch " + strings.Join(inRoot, " "), ", and v17/n is the word that took it past"},
	} {
		t.Run("AC-5 "+tc.cmd[:min(len(tc.cmd), 40)], func(t *testing.T) {
			reason := lbAsk(t, root, tc.cmd)
			if !strings.Contains(reason, tc.want) {
				t.Errorf("%.60s...: want the refusal to say %q, got:\n%s", tc.cmd, tc.want, reason)
			}
			if strings.Contains(reason, "sub goes through") || strings.Contains(reason, "sub is the word") {
				t.Errorf("%.60s...: the refusal names the cd's directory, which holds no link:\n%s", tc.cmd, reason)
			}
		})
	}
}

// lbHomeAsk is the refusal of cmd, run by tool in session, or "" where it is
// allowed.
func lbHomeAsk(t *testing.T, root, session, tool, cmd string) string {
	t.Helper()
	r := call(t, toolCallFrom(session, root, tool, map[string]any{"command": cmd}), noEnv)
	if !denied(r) {
		return ""
	}
	return r.HookSpecificOutput.PermissionDecisionReason
}

// AC-5 and AC-7 through the hook, for a word spelled through a variable for
// the person's home. The configuration check placed such a word in the home,
// while the bound read it as a path in the project that was not there, and
// within every bound: `touch $HOME/<41 links>/x` was allowed, where the same
// word spelled with ~ was refused (US-035, code review). 41 links, one in each
// directory, is past what macOS (32) and Linux (40) follow in one lookup, and
// on Windows 64 is past its 63 (lbPastTheLookup); 65 is past the bound of one
// word. The chains lead to a directory nothing protects, so only the bound can
// refuse them; and the session holding the story and one that does not are
// held alike.
func TestAC5AC7AWordThroughAVariableForTheHomeIsBoundAsItIsPlaced(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	write(t, home, "elsewhere/.keep", "")
	root := lbProject(t)
	past := lbPastTheLookup(41)
	chains := map[int]string{
		past: lbADirectoryAtATime(t, home, fmt.Sprintf("hb%d", past), past, "elsewhere", "x"),
		65:   lbADirectoryAtATime(t, home, "hb65", 65, "elsewhere", "x"),
	}
	const lookup = "more links than this system follows in one lookup"
	refusedForTheWord := func(t *testing.T, reason, typed string) {
		t.Helper()
		if !strings.Contains(reason, lbWordCode) || !strings.Contains(reason, lbRoute) ||
			!strings.Contains(reason, lbOver64) && !strings.Contains(reason, lookup) {
			t.Errorf("want it refused under %s for the links, got:\n%s", lbWordCode, reason)
			return
		}
		if !strings.Contains(reason, typed[:20]) {
			t.Errorf("the refusal does not quote the word as typed, %q...:\n%s", typed[:20], reason)
		}
	}
	for _, n := range []int{past, 65} {
		for _, spelling := range []string{"$HOME", "${HOME}", `"$HOME"`, "%USERPROFILE%"} {
			word := spelling + "/" + chains[n]
			for _, cmd := range []string{"touch " + word, "install -D f " + word, "echo x > " + word} {
				for _, session := range []string{working, "session-b"} {
					t.Run(fmt.Sprintf("AC-5 %d links, %s, %s", n, strings.Fields(cmd)[0]+" "+spelling, session), func(t *testing.T) {
						refusedForTheWord(t, lbHomeAsk(t, root, session, "Bash", cmd), word)
					})
				}
			}
		}
		for _, session := range []string{working, "session-b"} {
			word := "$env:USERPROFILE/" + chains[n]
			t.Run(fmt.Sprintf("AC-5 %d links, PowerShell New-Item $env:USERPROFILE, %s", n, session), func(t *testing.T) {
				refusedForTheWord(t, lbHomeAsk(t, root, session, "PowerShell", "New-Item "+word), word)
			})
		}
	}
	for _, cmd := range []string{"touch $HOME/notes.txt", "touch ~/notes.txt", `touch "${HOME}"/notes.txt`} {
		for _, session := range []string{working, "session-b"} {
			t.Run("AC-11 "+cmd+" is still allowed, "+session, func(t *testing.T) {
				if reason := lbHomeAsk(t, root, session, "Bash", cmd); reason != "" {
					t.Errorf("refused:\n%s", reason)
				}
			})
		}
	}
}

// lbAskIn puts command to the hook from a session whose working directory is
// cwd, and returns the reason it was refused for, or "" where it was allowed.
func lbAskIn(t *testing.T, cwd, session, cmd string) string {
	t.Helper()
	return lbHomeAsk(t, cwd, session, "Bash", cmd)
}

// AC-5 and AC-7 through the hook: from a working directory a shell reached
// one cd at a time past the system's lookup limit, every command is refused,
// echo included, and the refusal said only "a path this command reaches"
// (US-035, security review). It names the working directory, in the session
// holding the story and in one that does not. 42 links, one in each directory,
// is past what macOS (32) and Linux (40) follow in one lookup, the directory's
// own last link counted out: the walk there looks up each name on the way, and
// a lookup of the last name does not follow its link, which the walk follows
// itself from the directory before. One link past the limit, the hook places
// the working directory where it leads and judges each word from there, so
// with 41 links Linux rightly let `echo ok` through (CI on 2492472), and a
// write to the loop state at the end of such a chain is refused as one.
//
// Windows follows 63 reparse points in one lookup and will not look up a
// working directory 64 links deep either, but the walk follows that
// directory's own last link itself, so it meets the limit only with a name
// below. There the working directory is named at 65 links, for the bound of
// one word, and one 42 links deep is placed where it leads, so `echo ok` from
// it is allowed (CI on 38e0ed0).
func TestAC5AC7FromAWorkingDirectoryPastTheLookupLimitTheHookNamesIt(t *testing.T) {
	root := lbProject(t)
	depth, want := 42, "the working directory goes through more links than this system follows in one lookup"
	if runtime.GOOS == "windows" {
		depth, want = 65, "the working directory"+lbOver64
	}
	deep := filepath.Join(root, filepath.FromSlash(lbADirectoryAtATime(t, root, fmt.Sprintf("wd%d", depth), depth, "elsewhere", "")))
	state := filepath.Join(root, filepath.FromSlash(lbADirectoryAtATime(t, root, "st41", 41, ".sdlc/state", "")))
	allowed := map[int]string{10: filepath.Join(root, filepath.FromSlash(lbADirectoryAtATime(t, root, "wd10", 10, "elsewhere", "")))}
	if runtime.GOOS == "windows" {
		allowed[42] = filepath.Join(root, filepath.FromSlash(lbADirectoryAtATime(t, root, "wd42", 42, "elsewhere", "")))
	}
	for _, session := range []string{working, "session-b"} {
		t.Run(fmt.Sprintf("AC-7 echo ok, %d links deep, %s", depth, session), func(t *testing.T) {
			reason := lbAskIn(t, deep, session, "echo ok")
			if !strings.Contains(reason, want) || !strings.Contains(reason, lbWordCode) {
				t.Errorf("want it refused under %s saying %q, got:\n%s", lbWordCode, want, reason)
			}
		})
		t.Run("AC-5 the route is a cd that names the place without the links, "+session, func(t *testing.T) {
			reason := lbAskIn(t, deep, session, "echo ok")
			const want = "cd to a directory with a shorter path, one that names the place without the links"
			if !strings.Contains(reason, want) {
				t.Errorf("the refusal does not say %q:\n%s", want, reason)
			}
		})
		for links, cwd := range allowed {
			t.Run(fmt.Sprintf("AC-11 echo ok, %d links deep, %s", links, session), func(t *testing.T) {
				if reason := lbAskIn(t, cwd, session, "echo ok"); reason != "" {
					t.Errorf("refused:\n%s", reason)
				}
			})
		}
	}
	// Only the session holding the story is held on the loop state, from the
	// root as from the chain.
	t.Run("AC-7 touch active, 41 links deep into .sdlc/state, "+working, func(t *testing.T) {
		reason := lbAskIn(t, state, working, "touch active")
		if !strings.Contains(reason, ".sdlc/state is the loop's own record") && !strings.Contains(reason, lbWordCode) {
			t.Errorf("want it refused as the loop state or under %s, got:\n%s", lbWordCode, reason)
		}
	})
}

// AC-5 and AC-6 through the hook, from a session whose working directory is a
// subdirectory of the project. The rules join every word to it, and E0082
// named a later word with no link, `./a` or `a`, as the one that took the
// command past 512; E0081 quoted the joined word, not the one typed (US-035,
// code review round 2).
func TestAC5AC6FromASubdirectoryTheHookNamesTheWordAsTyped(t *testing.T) {
	root := lbProject(t)
	pkg := filepath.Join(root, "pkg")
	write(t, root, "pkg/.keep", "")
	lbAtOneName(t, pkg, "lnk65", 65, "../elsewhere", "x")
	words := make([]string, 18)
	for i := range words {
		words[i] = lbAtOneName(t, pkg, fmt.Sprintf("c%d", i), 30, "../elsewhere", "x")
	}
	for _, tc := range []struct{ cmd, want, not string }{
		{"touch " + strings.Join(words, " ") + " ./a", ", and c17/x is the word that took it past", "a is the word"},
		{"touch " + strings.Join(words, " ") + " a", ", and c17/x is the word that took it past", "a is the word"},
		{"touch lnk65/x", "lnk65/x" + lbOver64, "pkg/lnk65/x"},
		{"cd .. && touch pkg/lnk65/x", "pkg/lnk65/x" + lbOver64, ""},
	} {
		t.Run("AC-5 "+tc.cmd[:min(len(tc.cmd), 40)]+" ... "+tc.cmd[max(0, len(tc.cmd)-12):], func(t *testing.T) {
			reason := lbAskIn(t, pkg, working, tc.cmd)
			if !strings.Contains(reason, tc.want) {
				t.Errorf("%.60s...: want the refusal to say %q, got:\n%s", tc.cmd, tc.want, reason)
			}
			if tc.not != "" && strings.Contains(reason, tc.not) {
				t.Errorf("%.60s...: the refusal says %q:\n%s", tc.cmd, tc.not, reason)
			}
		})
	}
}

// AC-5 and AC-7 through the hook: a cd into a directory behind more links than
// the system follows in one lookup, and fewer than 65, names that directory.
// The word after the cd was quoted, because the directory alone, whose last
// link is not followed to place it, was within every bound (US-035, verifier
// round 5). 41 links at one name is past macOS (32) and Linux (40), and 64
// past Windows' 63.
func TestAC5AC7ACdIntoADirectoryPastTheLookupLimitIsNamedThroughTheHook(t *testing.T) {
	root := lbProject(t)
	dir := fmt.Sprintf("e%d", lbPastTheLookup(41))
	lbAtOneName(t, root, dir, lbPastTheLookup(41), "elsewhere", "x")
	for _, cmd := range []string{"cd " + dir + " && touch x", "cd " + dir + "; echo x > y"} {
		t.Run("AC-7 "+cmd, func(t *testing.T) {
			reason := lbAsk(t, root, cmd)
			want := dir + " goes through more links than this system follows in one lookup"
			if !strings.Contains(reason, want) || !strings.Contains(reason, lbWordCode) {
				t.Errorf("want it refused under %s saying %q, got:\n%s", lbWordCode, want, reason)
			}
		})
	}
}

// lbLookupLength is the length of the shortest path this system will not look
// up, probed below root with short names, or 0 where it looked up every path
// tried or is Windows, whose error for one is not checked on a real system.
func lbLookupLength(root string) int {
	if runtime.GOOS == "windows" {
		return 0
	}
	long := root + "/" + strings.Repeat(strings.Repeat("a", 99)+"/", 700)
	tooLong := func(p string) bool {
		_, err := os.Lstat(p)
		return errors.Is(err, syscall.ENAMETOOLONG)
	}
	if !tooLong(long) {
		return 0
	}
	lo, hi := len(root)+1, len(long)
	for lo < hi {
		if mid := (lo + hi) / 2; tooLong(long[:mid]) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// AC-5 and AC-7 through the hook (SDLC-E0083): from a working directory near
// the system's length on a path, a relative word through a link into the
// loop's state, whose full path is past that length, is refused under its own
// rule and code. The walk stopped where the system would not look the path up
// and read the rest by name, while a shell there writes to it by its shorter
// relative name (US-035, security review). A long word within the length, and
// a long word that is no path, are allowed; and the debug log of the refusal
// holds no word of the command (AC-2).
func TestAC5AC7AWordWhoseFullPathIsTooLongIsRefusedThroughTheHook(t *testing.T) {
	root := lbProject(t)
	limit := lbLookupLength(root)
	if limit == 0 {
		t.Skipf("no path below the project up to 70,000 bytes is refused as too long here, or this is Windows, whose error for one (206) is not checked on a real system; pathTooLong's mapping holds it in internal/pathrules")
	}
	dir := filepath.Join(root, "d")
	for rest := limit - 6 - len(dir) - 1; rest > 200; rest = limit - 6 - len(dir) - 1 {
		dir += "/" + strings.Repeat("A", min(200, rest-101))
	}
	b := strings.Repeat("b", limit-6-len(dir)-1)
	c := strings.Repeat("c", 50)
	for _, d := range []string{b, c} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Skipf("this system will not make a directory %d bytes long: %v", len(dir)+1+len(d), err)
		}
	}
	lbLink(t, filepath.Join(root, ".sdlc", "state"), filepath.Join(dir, b, "l"))
	for _, cmd := range []string{"echo x > " + b + "/l/active", "touch " + b + "/l/active"} {
		for _, session := range []string{working, "session-b"} {
			t.Run("AC-7 "+strings.Fields(cmd)[0]+" through the link, "+session, func(t *testing.T) {
				buf := debugLines(t)
				reason := lbAskIn(t, dir, session, cmd)
				for _, part := range []string{"the full path of ", "shorter", "SDLC-E0083", "[path-too-long]"} {
					if !strings.Contains(reason, part) {
						t.Errorf("the refusal does not say %q:\n%.400s", part, reason)
					}
				}
				log := buf.String()
				if !strings.Contains(log, "path-too-long") {
					t.Errorf("AC-3: the debug log does not name the rule, so it says nothing to hold to AC-2:\n%s", log)
				}
				// echo and touch are logged by their names in the vocabulary (AC-1).
				for _, word := range []string{b[:20], "l/active", "x >"} {
					if strings.Contains(log, word) {
						t.Errorf("AC-2: the debug log holds %q, a word of the command", word)
					}
				}
			})
		}
	}
	for _, cmd := range []string{"touch " + c + "/x", "echo x > " + c + "/notes.txt", "echo ok"} {
		t.Run("AC-11 "+cmd[:min(len(cmd), 12)]+" within the length", func(t *testing.T) {
			if reason := lbAskIn(t, dir, working, cmd); reason != "" {
				t.Errorf("refused:\n%.400s", reason)
			}
		})
	}
	run, tok := strings.Repeat("x", 300), strings.Repeat("T", 300)
	for name, cmd := range map[string]string{
		"a 400-byte commit message":     `git commit -m "` + strings.Repeat("word ", 80) + `"`,
		"a commit message with slashes": `git commit -m "` + strings.Repeat("fix a/b/c and d/e/f ", 6) + run + `"`,
		"a 300-byte echo":               `echo "` + run + `" > notes.txt`,
		"a 300-byte printf":             "printf '%s' " + tok,
		"a 300-byte grep pattern":       "grep -r '" + run + "' .",
		"python3 -c code":               `python3 -c "` + strings.Repeat("print(1);", 33) + `"`,
		"a 300-byte heredoc line":       "cat > notes.txt <<'EOF2'\n" + run + "\nEOF2",
	} {
		// The session holding the story is refused a commit before its gates,
		// so a commit is asked from the one that holds none.
		session := working
		if strings.HasPrefix(cmd, "git commit") {
			session = "session-b"
		}
		t.Run("AC-11 "+name, func(t *testing.T) {
			if reason := lbAskIn(t, root, session, cmd); reason != "" {
				t.Errorf("refused:\n%.400s", reason)
			}
		})
	}
}

// lbPastTheLength makes in root a directory first, under it directories of
// 201-byte names until the full path is past limit, and in the last a link l
// to target. Each is made by its name in the one above, as a shell that walks
// a directory at a time makes it, since the system will not take the full
// path. It returns the names, from first down; where the platform will not
// make first, it skips.
func lbPastTheLength(t *testing.T, root, first, target string, limit int) []string {
	t.Helper()
	names := []string{first}
	for full := len(root) + 1 + len(first); full <= limit; full += 202 {
		names = append(names, fmt.Sprintf("d%d", len(names)-1)+strings.Repeat("n", 200))
	}
	at, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range names {
		if err := at.Mkdir(name, 0o755); err != nil {
			_ = at.Close()
			if i == 0 {
				t.Skipf("this platform will not make a directory of a %d-byte name: %v", len(first), err)
			}
			t.Fatal(err)
		}
		next, err := at.OpenRoot(name)
		_ = at.Close()
		if err != nil {
			t.Fatal(err)
		}
		at = next
	}
	defer func() { _ = at.Close() }()
	if err := at.Symlink(target, "l"); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	return names
}

// lbPastTheLengthOrSkip is the system's length on a path below root, or a
// skip where it cannot be found.
func lbPastTheLengthOrSkip(t *testing.T, root string) int {
	t.Helper()
	limit := lbLookupLength(root)
	if limit == 0 {
		t.Skip("no path below the project up to 70,000 bytes is refused as too long here, or this is Windows, whose error for one (206) is not checked on a real system; nameOverLimit's rows in internal/pathrules hold the rule")
	}
	return limit
}

// AC-5 and AC-7 through the hook (SDLC-E0083): a legal name of 130 "é", 260
// bytes, on the way to a path past the system's length. A name counted in bytes
// alone was taken for one the system cannot hold, so the path read as not
// there, and `touch l/active`, through a link into the loop's state, was
// allowed again, and landed (US-035, verifier round 7, scen30). It is refused
// from the directory past the length, and from one part of the way down.
func TestAC5AC7APathPastTheLengthThroughALegalMultibyteNameIsRefusedThroughTheHook(t *testing.T) {
	root := lbProject(t)
	limit := lbPastTheLengthOrSkip(t, root)
	names := lbPastTheLength(t, root, strings.Repeat("é", 130), filepath.Join(root, ".sdlc", "state"), limit)
	deep := filepath.Join(append([]string{root}, names...)...)
	mid := filepath.Join(root, names[0], names[1])
	rest := strings.Join(names[2:], "/") + "/l/active"
	for _, tc := range []struct{ where, cwd, cmd string }{
		{"past the length", deep, "touch l/active"},
		{"past the length", deep, "echo x > l/active"},
		{"part of the way down", mid, "touch " + rest},
		{"part of the way down", mid, "echo x > " + rest},
	} {
		for _, session := range []string{working, "session-b"} {
			t.Run("AC-7 "+strings.Fields(tc.cmd)[0]+" from "+tc.where+", "+session, func(t *testing.T) {
				reason := lbAskIn(t, tc.cwd, session, tc.cmd)
				for _, part := range []string{"SDLC-E0083", "[path-too-long]"} {
					if !strings.Contains(reason, part) {
						t.Errorf("the refusal does not say %q:\n%.400s", part, reason)
					}
				}
			})
		}
	}
}

// AC-11 through the hook: a word that is no path, holding a run of more than
// 255 bytes, is allowed, as before the story. Round 6 took the system's answer
// for a name it cannot hold for a path too long, and refused all of these under
// SDLC-E0083 (US-035, verifier round 6, scen24 and scen25). `touch` of a name
// past the limit is allowed too: it cannot be there.
func TestAC11ALongWordThatIsNoPathIsAllowedThroughTheHook(t *testing.T) {
	root := lbProject(t)
	run := strings.Repeat("x", 300)
	for name, cmd := range map[string]string{
		"python3 -c with a 300-byte literal":      `python3 -c 'print("` + run + `")'`,
		"perl -e with a 300-byte literal":         `perl -e 'print "` + run + `"'`,
		"node -e with a 300-byte literal":         `node -e 'console.log("` + run + `")'`,
		"a python3 heredoc with a 300-byte token": "python3 - <<'EOF'\nprint(\"" + run + "\")\nEOF",
		"a 300-byte comment after a write":        "touch notes.txt # " + run,
		"touch of a 300-byte name":                "touch " + run,
	} {
		for _, session := range []string{working, "session-b"} {
			t.Run("AC-11 "+name+", "+session, func(t *testing.T) {
				if reason := lbAskIn(t, root, session, cmd); reason != "" {
					t.Errorf("refused:\n%.400s", reason)
				}
			})
		}
	}
}

// AC-5 through the hook: from a working directory past the system's length,
// every command is refused, and the refusal says why in words that fit it.
// `touch l/<300 z>`, through one link, was refused for more links than the
// system follows in one lookup, and `python3 -c` code quoted `-c` as the path
// too long (US-035, verifier round 7). Names are ASCII, so this holds wherever
// the length can be found.
func TestAC5FromAWorkingDirectoryPastTheLengthTheRefusalNamesNeitherLinksNorAnOption(t *testing.T) {
	root := lbProject(t)
	limit := lbPastTheLengthOrSkip(t, root)
	names := lbPastTheLength(t, root, strings.Repeat("a", 200), filepath.Join(root, "elsewhere"), limit)
	deep := filepath.Join(append([]string{root}, names...)...)
	const lookup = "more links than this system follows in one lookup"
	for _, tc := range []struct{ cmd, not string }{
		{"touch l/" + strings.Repeat("z", 300), lookup},
		{"touch l/x", lookup},
		{`python3 -c 'print("` + strings.Repeat("x", 300) + `")'`, " -c "},
		{"python3 -c 'print(1)'", " -c "},
	} {
		t.Run("AC-5 "+tc.cmd[:min(len(tc.cmd), 20)], func(t *testing.T) {
			reason := lbAskIn(t, deep, working, tc.cmd)
			if !strings.Contains(reason, "SDLC-E0083") {
				t.Errorf("want it refused under SDLC-E0083, from a working directory of %d bytes, got:\n%.400s", len(deep), reason)
			}
			if strings.Contains(reason, tc.not) {
				t.Errorf("the refusal says %q:\n%.400s", tc.not, reason)
			}
		})
	}
	t.Run("AC-5 the route is a cd to a shorter path", func(t *testing.T) {
		reason := lbAskIn(t, deep, working, "echo ok")
		if !strings.Contains(reason, "the working directory") || !strings.Contains(reason, "cd to a directory with a shorter path") {
			t.Errorf("want the working directory named and a cd to a shorter path as the route, got:\n%.400s", reason)
		}
		if strings.Contains(reason, "renam") {
			t.Errorf("the route still says to rename directories:\n%.400s", reason)
		}
	})
}

// AC-5 and AC-7 through the hook: a cd to the person's home, spelled any of
// the ways a shell reads it, and then a write through a link there into their
// Claude Code configuration is refused under the configuration's rule. The
// word after the cd was placed in the project, where the link is not (US-035,
// landing round, carried from US-120). A write after the same cd that leads
// nowhere protected is allowed. HOME is a scratch directory.
func TestAC5AC7ACdToTheHomeThenAWriteThroughALinkIntoTheConfigurationIsRefused(t *testing.T) {
	config := claudeHome(t)
	home := filepath.Dir(config)
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	lbLink(t, config, filepath.Join(home, "cl1"))
	root := lbProject(t)
	const rule = "[claude-config-is-the-persons]"
	for _, cmd := range []string{
		"cd ~ && touch cl1/settings.json",
		"cd && touch cl1/settings.json",
		"cd $HOME && touch cl1/settings.json",
		`cd "$HOME" && touch cl1/settings.json`,
		"cd ${HOME} && touch cl1/settings.json",
		"cd ~/cl1 && touch settings.json",
	} {
		for _, session := range []string{working, "session-b"} {
			t.Run("AC-7 "+cmd+", "+session, func(t *testing.T) {
				if reason := lbAskIn(t, root, session, cmd); !strings.Contains(reason, rule) {
					t.Errorf("want it refused under %s, got:\n%.400s", rule, reason)
				}
			})
		}
	}
	for _, session := range []string{working, "session-b"} {
		t.Run("AC-11 cd ~ && touch notes.txt, "+session, func(t *testing.T) {
			if reason := lbAskIn(t, root, session, "cd ~ && touch notes.txt"); reason != "" {
				t.Errorf("refused:\n%.400s", reason)
			}
		})
	}
}

// AC-5, AC-7 and AC-11 through the hook: after a cd to the person's home,
// spelt any of the ways the shell reads from HOME, a glob write or a write to
// a held name is refused, as on trunk, because the same command can point
// HOME at the loop's own directory first. Read as the home, `export
// HOME=$PWD/.sdlc && cd && rm -rf state` was allowed, and took the loop's
// state away (US-035, verifier round 9). HOME in the hook's own environment,
// set or unset, does not change it. `ls`, and a write to an ordinary name,
// after the same cd stay allowed.
func TestAC5AC7AC11AfterACdToTheHomeAWriteToAHeldNameIsRefusedThroughTheHook(t *testing.T) {
	root := lbProject(t)
	write(t, root, ".sdlc/stories/A-1/PLAN.md", "plan\n")
	cds := []string{"cd", "cd ~", "cd -- ~", "pushd ~", "builtin cd", "cd $HOME", `cd "$HOME"`, "cd ${HOME}", "cd -P $HOME"}
	sets := []string{"", "export HOME=$PWD/.sdlc && ", "HOME=$PWD/.sdlc; ",
		"export HOME=" + filepath.ToSlash(filepath.Join(root, ".sdlc", "state")) + "; "}
	for _, home := range []string{"set", "unset"} {
		t.Run("AC-11 HOME "+home, func(t *testing.T) {
			scratch := t.TempDir()
			t.Setenv("HOME", scratch)
			t.Setenv("USERPROFILE", scratch)
			if home == "unset" {
				if err := os.Unsetenv("HOME"); err != nil {
					t.Fatal(err)
				}
				if err := os.Unsetenv("USERPROFILE"); err != nil {
					t.Fatal(err)
				}
			}
			for _, cd := range cds {
				for _, set := range sets {
					for _, then := range []string{"rm -f *", "rm -rf state", "touch stories/A-1/PLAN.md"} {
						cmd := set + cd + " && " + then
						reason := lbAskIn(t, root, working, cmd)
						if !strings.Contains(reason, "which the hook cannot place because") {
							t.Errorf("%s: want it refused for a cd the hook cannot place, got:\n%.300s", cmd, reason)
						}
					}
				}
				if reason := lbAskIn(t, root, working, cd+" && ls"); reason != "" {
					t.Errorf("%s && ls: refused:\n%.300s", cd, reason)
				}
			}
			for _, cmd := range []string{"cd ~ && touch notes.txt", "cd && touch notes.txt", "cd ~/sub && touch x"} {
				if reason := lbAskIn(t, root, working, cmd); reason != "" {
					t.Errorf("%s: refused:\n%.300s", cmd, reason)
				}
			}
		})
	}
}

// The rows below are the Gate 7 round 4 fixes (security review B1 and notes
// 2a and 2b, and the verifier's variable and script cds), held to under the
// scout rule in files this story changes. They are on disk through the hook,
// not in internal/shellpolicy: what they turn on is where a link in a scratch
// home leads and which directories are there, which only the hook's resolver
// reads. SECURITY.md applies: each row names the class, not a recipe.

// lbG10 is a project held by the session and a scratch home, laid out for
// these rows. In the home, lc leads to the person's Claude Code configuration,
// la to their ~/.agents, ls to the loop's state, and sub/lc to the
// configuration again; in the project, st leads to the loop's state. tmp and
// vars are scratch directories outside both, for a script to cd into.
type lbG10 struct{ root, home, tmp, vars string }

// lbG10Setup lays out lbG10, or skips where links cannot be made.
func lbG10Setup(t *testing.T) lbG10 {
	t.Helper()
	config := claudeHome(t)
	home := filepath.Dir(config)
	root := lbProject(t)
	for _, d := range []string{config, filepath.Join(home, ".agents"), filepath.Join(home, "sub"), filepath.Join(home, "proj")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"build/.keep", "build/out/.keep", "src/.keep", "web/.keep", "internal/.keep"} {
		write(t, root, f, "")
	}
	state := filepath.Join(root, ".sdlc", "state")
	lbLink(t, config, filepath.Join(home, "lc"))
	lbLink(t, filepath.Join(home, ".agents"), filepath.Join(home, "la"))
	lbLink(t, state, filepath.Join(home, "ls"))
	lbLink(t, config, filepath.Join(home, "sub", "lc"))
	lbLink(t, state, filepath.Join(root, "st"))
	g := lbG10{root: root, home: home, tmp: t.TempDir(), vars: t.TempDir()}
	for _, d := range []string{filepath.Join(g.tmp, "x"), filepath.Join(g.tmp, "scratch")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return g
}

// expand puts the layout's paths in a row's placeholders: <ROOT> the project,
// <LOOP> its .sdlc, <STATE> the loop's state, <HOME> the scratch home, and
// <TMP> and <VAR> the scratch directories, <GIT> the project's git
// directory, and <L> the loop's directory spelt relative to the project.
func (g lbG10) expand(row string) string {
	slash := filepath.ToSlash
	return strings.NewReplacer(
		"<STATE>", slash(filepath.Join(g.root, ".sdlc", "state")),
		"<LOOP>", slash(filepath.Join(g.root, ".sdlc")),
		"<L>", ".sdlc",
		"<GIT>", slash(filepath.Join(g.root, ".git")),
		"<ROOT>", slash(g.root),
		"<HOME>", slash(g.home),
		"<TMP>", slash(g.tmp),
		"<VAR>", slash(g.vars),
	).Replace(row)
}

// rows asks each refused row and each allowed row of a group from cwd, a
// directory relative to the project, in the session holding the story.
func (g lbG10) rows(t *testing.T, cwd string, refused, allowed []string) {
	t.Helper()
	dir := filepath.Join(g.root, filepath.FromSlash(cwd))
	for _, row := range refused {
		t.Run("AC-7 refused: "+row, func(t *testing.T) {
			if reason := lbAskIn(t, dir, working, g.expand(row)); reason == "" {
				t.Errorf("allowed; want it refused")
			}
		})
	}
	for _, row := range allowed {
		t.Run("AC-11 allowed: "+row, func(t *testing.T) {
			if reason := lbAskIn(t, dir, working, g.expand(row)); reason != "" {
				t.Errorf("refused:\n%.400s", reason)
			}
		})
	}
}

// AC-5 and AC-7 (security review round 4, B1): CDPATH set on the line before
// a relative cd is read wherever and however the line sets it. The reader split
// the raw line on spaces, so an assignment ended by `;` or `|` was read with
// the separator in its value, and the cd was placed under a directory that is
// not there: `rm -f active` in the loop's state was allowed, which turns every
// rule off for the rest of the session. An assignment whose value cannot be
// read, appended, from a variable or read in, leaves the cd unplaced. A CDPATH
// that leads nowhere held, or one only echoed, changes nothing.
func TestAC5AC7ACdpathSetOnTheLineBeforeACdIsReadAsTheShellReadsIt(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"CDPATH=<LOOP>;cd state && rm -f active",
		"export CDPATH=<LOOP>; cd state && rm -f session",
		"CDPATH=<LOOP>; pushd state && rm -f active",
		"CDPATH=<LOOP>|cd state && rm -f active",
		`CDPATH="<LOOP>";cd state && rm -f active`,
		"CDPATH='<LOOP>'&&cd state && rm -f active",
		"declare -x CDPATH=<LOOP>; cd state && rm -f active",
		"CDPATH+=:<LOOP>; cd state && rm -f active",
		"CDPATH=$X; cd state && rm -f active",
		"read CDPATH; cd state && rm -f active",
	}, []string{
		"CDPATH=<ROOT> cd build && rm -rf src",
		"CDPATH=<ROOT>;cd build && touch x",
		"export CDPATH=/nonexistent; cd build && rm -f x",
		"echo $CDPATH && cd build && rm -f x",
	})
}

// AC-5 and AC-7 (security review round 4, note 2): a glob below a link in the
// home that leads into the person's configuration or their ~/.agents is
// judged where the link leads, with or without a cd to the home first. The
// glob was read under the home, where nothing is held, unless the line named
// the configuration. A glob in a directory of the home with no such link is
// left alone.
func TestAC5AC7AGlobBelowALinkIntoThePersonsConfigurationIsJudgedWhereItLeads(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"rm -f ~/lc/*",
		"rm -f ~/la/*",
		"cd ~ && rm -f lc/*",
		"cd <HOME> && rm -f lc/*",
	}, []string{
		"rm -f ~/sub/*",
		"cd ~ && rm -f sub/*",
	})
}

// AC-5 and AC-7 (security review round 4, note 2): a cd through a link, in the
// home or in the project, into the loop's state or its directory, followed by
// a bare name or a climb, is judged where the shell is. With -P the shell is
// where the link leads, and `..` climbs from there; without it, `..` climbs
// the path as spelt. The cd was placed under the home by its name, and a bare
// `active` after it was allowed.
func TestAC5AC7ACdThroughALinkIntoTheLoopsStateIsJudgedWhereTheShellIs(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"cd ~/ls && rm -f active",
		"cd -P ~/ls && rm -f active",
		"cd ~/sub && cd ../ls && rm active",
		"cd ~ && cd ls && touch active",
		"cd -P ~/ls/.. && rm -rf state",
		"cd -P ~/ls && cd .. && rm -rf state",
		"cd ~/ls && cd -P .. && rm -rf state",
		"cd -P <HOME>/ls/.. && rm -rf state",
		"cd -P <HOME>/ls && cd .. && rm -rf state",
		"cd st && touch ../config.json",
		"cd ~/ls && rm -rf ../state",
	}, nil)
}

// AC-5 and AC-7 (verifier, Gate 7 round 4): a cd into a directory spelt from
// a variable, from the home a line can set, or back with `cd -`, leaves the
// shell where the hook cannot place it, so a held name after it is refused,
// whatever the variable was set to on the line. A name nothing holds stays
// allowed after the same cd.
func TestAC5AC7AfterACdTheHookCannotPlaceAHeldNameIsRefused(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		`D=<STATE>; cd "$D" && rm -f active`,
		"D=<ROOT>; cd $D/.sdlc/state && rm -f active",
		"export HOME=$PWD/.sdlc && cd ~/state && rm -f active",
		"export HOME=<STATE>; cd && rm -f active",
		"cd - && rm -f active",
	}, []string{
		"cd - && rm -rf build",
		`cd "$D" && rm -rf build`,
	})
}

// AC-5: after a cd below the home, the refusal says the home is a place the
// command can change, which is why the hook does not place it.
func TestAC5AfterACdBelowTheHomeTheRefusalSaysTheCommandCanChangeIt(t *testing.T) {
	g := lbG10Setup(t)
	reason := lbAskIn(t, g.root, working, "cd ~/proj && rm -f *.o")
	if want := "starts at the home directory, which the command can change"; !strings.Contains(reason, want) {
		t.Errorf("AC-5: the refusal does not say %q:\n%.400s", want, reason)
	}
}

// AC-5 and AC-7 (verifier, Gate 7 round 4): a cd inside a script a shell
// runs, as `bash -c`, `sh -c`, `zsh -c`, ssh, watch, sudo, a nested shell or
// eval, is read where the script runs, CDPATH given to it included, so a held
// name after it in the script is refused. A cd in a script does not move the
// shell after it, which is why the rows from internal, through the project's
// link st, are refused; a quote in the line before, a redirect or a subshell
// round the script does not change that. Ordinary scripts stay allowed.
func TestAC5AC7ACdInsideAScriptIsReadWhereTheScriptRuns(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"bash -c 'cd <STATE> && rm -f active'",
		"sh -c 'cd <STATE>; rm -f active'",
		`zsh -c "cd <STATE> && rm -f active"`,
		"bash -c 'cd <LOOP> && rm -rf state'",
		`bash -c 'cd "<STATE>" && rm -f active'`,
		`bash -c 'cd <STATE> && rm -f "active"'`,
		"bash -c $'cd <STATE> && rm -f active'",
		"ssh localhost 'cd <STATE> && rm -f active'",
		"watch -n1 'cd <STATE> && rm -f active'",
		"sudo sh -c 'cd <STATE> && rm -f active'",
		`bash -c "bash -c 'cd <STATE> && rm -f active'"`,
		"eval 'cd <STATE> && rm -f active'",
		"eval cd <STATE>; rm -f active",
		"CDPATH=<LOOP> bash -c 'cd state && rm -f active'",
		"export CDPATH=<LOOP>; bash -c 'cd state && rm -f active'",
		"env CDPATH=<LOOP> bash -c 'cd state && rm -f active'",
		"env CDPATH=<LOOP> sh -c 'cd state; rm -f active'",
	}, []string{
		"bash -c 'cd build && rm -rf out'",
		"sh -c 'cd web && npm install'",
		"bash -c 'cd <TMP> && rm -rf x' && rm -rf build/out",
		`bash -c 'echo "a; b"' && ls`,
		"echo 'x; cd <TMP>' && rm -rf build/out",
		"bash -c 'cd build' && make",
		"zsh -c 'cd build && ls'",
		"bash -lc 'cd build && make test'",
		"eval echo hi",
		"bash -c 'cd <TMP> && rm -rf scratch'",
	})
	g.rows(t, "internal", []string{
		"bash -c 'cd <TMP>' && rm -f ../st/active",
		"(bash -c 'cd <TMP>') && rm -f ../st/active",
		"bash -c 'cd <TMP>' > log.txt && rm -f ../st/active",
		`echo "it's" && bash -c 'cd <TMP>' && rm -f ../st/active`,
		`bash -c 'cd <TMP> && bash -c "cd <VAR>"' && rm -f ../st/active`,
		`bash -c "cd <TMP> && echo \"x\"" && rm -f ../st/active`,
		"bash -c 'cd <TMP> && (cd <VAR>' && rm -f ../st/active",
		"echo 'a; cd <TMP>' && rm -f ../st/active",
		`bash -c 'cd <TMP> && echo \'' && rm -f ../st/active`,
	}, nil)
}

// The rows below are the Gate 4 round 3 fixes (design review round 3), held
// to as the rows above are.

// AC-5 and AC-7 (design review round 3): zsh ties the array cdpath to the
// search path CDPATH names, so a relative cd after `cdpath=(<dir>)` is read as
// one after CDPATH set to it. Read by its capitals alone, the cd was placed
// in the project, and `rm -f active` after it took the loop's state away.
func TestAC5AC7AZshCdpathSetOnTheLineBeforeACdIsReadAsCdpath(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"cdpath=(<LOOP>); cd state && rm -f active",
		"cdpath+=(<LOOP>); cd state && rm -f active",
	}, nil)
}

// AC-5 and AC-7 (design review round 3): where the line sets CDPATH in a way
// the hook does not read, appended to here, a relative cd after it could be
// anywhere, so a relative write after it is refused whatever it names: the
// git directory's objects were taken away by a name nothing holds.
func TestAC5AC7AfterACdpathTheHookDoesNotReadARelativeWriteIsRefused(t *testing.T) {
	g := lbG10Setup(t)
	write(t, g.root, ".git/objects/12/obj", "")
	g.rows(t, ".", []string{
		"export CDPATH+=:<GIT>/objects; cd 12 && rm -f obj",
		"CDPATH+=<GIT>/objects; pushd 12 && rm -f obj",
		"CDPATH+=<GIT>; cd objects && rm -rf 12",
	}, nil)
}

// AC-5 (design review round 3): a refusal for a cd the hook could not place
// says what to change so that it can: the cd out of the eval or the inner
// script, or CDPATH set as one plain assignment. The nested script's refusal
// says the hook does not read it.
func TestAC5ARefusalForACdTheHookCannotPlaceSaysWhatToChange(t *testing.T) {
	g := lbG10Setup(t)
	for _, row := range []struct{ cmd, route, reason string }{
		{"eval cd <STATE>; rm -f active", "move the `cd` out of the eval", ""},
		{`bash -c "true; sh -c 'cd <TMP>; rm -f active'"`, "move the `cd` out of the inner script", "which the hook does not read"},
		{"CDPATH+=:<TMP>; cd x && rm -f notes.txt", "set CDPATH as one plain assignment", ""},
	} {
		t.Run("AC-5 "+row.cmd, func(t *testing.T) {
			reason := lbAskIn(t, g.root, working, g.expand(row.cmd))
			if reason == "" {
				t.Fatal("allowed; want it refused")
			}
			for _, want := range []string{row.route, row.reason} {
				if !strings.Contains(reason, want) {
					t.Errorf("the refusal does not say %q:\n%.600s", want, reason)
				}
			}
		})
	}
}

// The rows below are the Gate 7 round 5 fixes (code review round 5, and the
// security review's advice on CDPATH mentions), held to as the rows above are.
// <L> spells the loop's directory relative to the project; each row is asked
// from the project's root.

// AC-5 and AC-7 (code review round 5): a relative CDPATH entry is searched
// from where the shell is, the project's root included. Dropped there, `CDPATH=
// <L> cd state && rm -f active` was read as a plain relative cd, and took the
// loop's state away; trunk has the same hole. A relative entry that leads
// nowhere held, or a cd with nothing written after it, is left alone.
func TestAC5AC7ARelativeCdpathEntryIsSearchedFromTheProjectRoot(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"CDPATH=<L> cd state && rm -f active",
		"CDPATH=./<L> cd state && rm -f active",
		"CDPATH=:<L> cd state && rm -f active",
		"CDPATH=x:<L> cd state && touch active",
		"CDPATH=<L>; cd state && rm -f active",
		"export CDPATH=<L>; cd state && echo x > active",
		"declare -x CDPATH=<L>; cd state && rm -f active",
		"env CDPATH=<L> bash -c 'cd state && rm -f active'",
		"CDPATH=<L> cd stories && rm -f -r x",
	}, []string{
		"CDPATH=<L> cd build",
		"CDPATH=x cd build && rm -rf out",
		"CDPATH=build cd out && rm -f y",
	})
}

// AC-5 and AC-7 (code review round 5): after a cd the hook cannot place, a cd
// that searches a relative CDPATH entry could be anywhere, so a write after
// it is refused, and the refusal says how to let the hook place it.
func TestAC5AC7AfterACdTheHookCannotPlaceARelativeCdpathEntryLeavesTheCdUnplaced(t *testing.T) {
	g := lbG10Setup(t)
	const row = "D=x; cd $D && CDPATH=<L> cd state && rm -f active"
	reason := lbAskIn(t, g.root, working, g.expand(row))
	if reason == "" {
		t.Fatalf("AC-7: %s was allowed; want it refused", row)
	}
	if want := "spell each CDPATH entry as an absolute path"; !strings.Contains(reason, want) {
		t.Errorf("AC-5: the refusal does not say %q:\n%.600s", want, reason)
	}
	g.rows(t, ".", []string{
		"D=x; cd $D && CDPATH=x cd build && touch y",
	}, []string{
		"D=x; cd $D && CDPATH=x cd build && ls",
	})
}

// AC-5 and AC-7 (SDET, unfreeze 12): a CDPATH with no relative entry, or only
// empty ones and `.`, does not place a cd the hook could not place before it.
// The shell searches each entry and then where it is, so after `cd $D`, `cd
// state` with CDPATH set to a directory that holds no state still lands below
// $D, which could be the loop's directory: as without the CDPATH, a held name
// after it is refused. trunk allows all of these, and so did the tree this
// row was written against: only the plain line, with no CDPATH, was refused.
func TestAC5AC7ACdpathWithNoRelativeEntryDoesNotPlaceACdTheHookCannotPlace(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"D=x; cd $D && cd state && rm -f active",
		"D=x; cd $D && CDPATH=: cd state && rm -f active",
		"D=x; cd $D && CDPATH=. cd state && rm -f active",
		"D=x; cd $D && CDPATH=/nonexistent cd state && rm -f active",
		"D=x; cd $D && CDPATH=<TMP> cd state && rm -f active",
		"cd - && CDPATH=/nonexistent cd state && rm -f active",
	}, []string{
		"D=x; cd $D && CDPATH=/nonexistent cd build && rm -f y",
	})
}

// AC-5 and AC-7 (security review, code review round 5): a mention of CDPATH,
// in any case, that the hook cannot read as a value leaves every relative cd
// after it unplaced, so a write after it is refused: an assigning expansion,
// or a command the hook does not know sets it, a grep included. A read of it,
// an unset of it, or a longer word that holds it, as a file name, changes
// nothing.
func TestAC5AC7AMentionOfCdpathTheHookCannotReadLeavesARelativeCdUnplaced(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"CDPATH=${CDPATH:=<L>} cd state && rm -f active",
		"grep -rn cdpath internal > notes.txt; cd build && touch stamp",
	}, []string{
		"unset CDPATH; cd build && rm -rf out",
		"unset -v CDPATH; cd build && touch y",
		"cd internal/shellpolicy && rm -f cdpath.go.orig",
		"echo ${#CDPATH} && cd build && touch y",
		"echo $CDPATH && cd build && rm -f x",
	})
}

// AC-11 (code review round 5): an escaped dollar before a quote opens a plain
// quote, not a `$'...'` string, and a subshell after a script or a quoted cd
// is read where the shell is. None of these is refused.
func TestAC11AnEscapedDollarQuoteAndASubshellAfterAScriptAreAllowed(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", nil, []string{
		`echo \$'x' && cd build && touch y`,
		"(cd build && make) && rm -rf out",
		"bash -c 'cd build && make' && (cd build && touch y)",
		"echo 'a; cd build' && (cd out && touch y)",
	})
}

// The rows below are the verifier's rounds 12 and 13 (unfreeze 13), held to
// as the rows above are: the bound on the places a cd that searches CDPATH
// may leave the shell in, counted as places and not as entries; the .claude
// tail after an unplaced cd; and the operand a refusal names.

// lbAskWith puts command to the hook from the session holding the story, in
// cwd, with the session's environment holding vars, and returns the reason it
// was refused for, or "" where it was allowed.
func lbAskWith(t *testing.T, cwd, cmd string, vars map[string]string) string {
	t.Helper()
	r := call(t, toolCallFrom(working, cwd, "Bash", map[string]any{"command": cmd}), env(vars))
	if !denied(r) {
		return ""
	}
	return r.HookSpecificOutput.PermissionDecisionReason
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

// lbDistinctCdpathCds is n of `CDPATH=/a:/b cd xI; `, each to a name of its
// own, x1 to xn.
func lbDistinctCdpathCds(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "CDPATH=/a:/b cd x%d; ", i)
	}
	return b.String()
}

// AC-5, AC-6 and AC-7 (verifier round 12): after a cd the hook cannot place,
// each relative cd that searches CDPATH adds the places its entries could
// send the shell, and every segment was judged from all of them: 600 of
// `CDPATH=/a:/b cd x; ` ran to the hook's deadline and was refused for time.
// The places are counted once each and bounded. Each cd to a name of its own
// adds two more (/a/xI and /b/xI) beside where the others lead below, so ten,
// twenty or 600 of them pass the bound, and a relative write after them is
// refused for the places, at once, and not for time.
//
// A cd to the same name again adds none only where every shell searches the
// entries before its own directory. bash does; zsh tries its own directory
// first unless CDPATH holds `.` or an empty entry, so from /a/x, `CDPATH=/a
// cd x` lands in /a/x in bash and /a/x/x in zsh, and repeated it leaves zsh
// ever deeper (measured with bash 5.3 and zsh 5.9, unfreeze 15). The hook
// cannot tell the shells apart: with `:.` after the absolute entries, 600 cds
// to the same name are allowed, as on trunk; without it, 20 cds to build,
// each followed by a write, pass the bound and are refused for the places.
// Three to names of their own, and two to the same one, stay within it.
func TestAC5AC6AC7ManyCdpathCdsAfterAnUnplacedCdAreRefusedForThePlacesNotForTime(t *testing.T) {
	g := lbG10Setup(t)
	refused := []struct{ name, cmd string }{
		{"20 placed CDPATH cds to build with no . entry", strings.Repeat("CDPATH=/a:/b cd build && touch y; ", 20)},
	}
	for _, n := range []int{10, 20, 600} {
		refused = append(refused, struct{ name, cmd string }{
			fmt.Sprintf("%d CDPATH cds to names of their own after an unplaced cd", n),
			"D=x; cd $D; " + lbDistinctCdpathCds(n) + "touch y",
		})
	}
	for _, row := range refused {
		cmd := row.cmd
		t.Run("AC-7 "+row.name, func(t *testing.T) {
			reason := lbAskIn(t, g.root, working, cmd)
			if reason == "" {
				t.Fatalf("%d bytes allowed; want it refused for the places the cds could leave the shell in", len(cmd))
			}
			if want := "more places than the hook follows"; !strings.Contains(reason, want) {
				t.Errorf("AC-6: the refusal does not say %q:\n%.600s", want, reason)
			}
			if strings.Contains(reason, "not-checked-in-time") {
				t.Errorf("AC-6: refused for time, not for the places:\n%.600s", reason)
			}
		})
	}
	g.rows(t, ".", nil, []string{
		"D=x; cd $D; CDPATH=/a:/b cd x; CDPATH=/a:/b cd x; touch y",
		"D=x; cd $D; " + lbDistinctCdpathCds(3) + "touch y",
		"D=x; cd $D; " + strings.Repeat("CDPATH=/a:/b:. cd x; ", 600) + "touch y",
	})
}

// AC-11 (verifier round 13): the bound counts the places a cd could leave
// the shell in, not the entries searched to get there. A cd into a directory
// and back, with CDPATH in the session's environment or set on the line,
// leaves the shell in one of two places however often it is done, into the
// same directory or eight different ones, in bash and zsh alike, since `cd
// ..` searches no CDPATH; so does a cd placed by absolute CDPATH entries
// followed by `.`, which zsh then searches before its own directory.
// Counted with repeats, eight round trips were refused for "more places than
// the hook follows", where trunk allows them.
func TestAC11ACdpathCdRepeatedDoesNotCountItsPlacesAgain(t *testing.T) {
	g := lbG10Setup(t)
	for i := 1; i <= 8; i++ {
		write(t, g.root, fmt.Sprintf("s%d/.keep", i), "")
	}
	write(t, g.root, "a/.keep", "")
	same := func(int) string { return "a" }
	each := func(i int) string { return fmt.Sprintf("s%d", i) }
	for _, row := range []struct{ name, cmd, cdpath string }{
		{"8 round trips, CDPATH in the environment", lbRoundTrips(8, same) + "touch stamp", "/w"},
		{"20 round trips, CDPATH in the environment", lbRoundTrips(20, same) + "touch stamp", "/w"},
		{"8 round trips, CDPATH exported on the line", "export CDPATH=/w; " + lbRoundTrips(8, same) + "touch stamp", ""},
		{"20 round trips, CDPATH exported on the line", "export CDPATH=/w; " + lbRoundTrips(20, same) + "touch stamp", ""},
		{"8 round trips into build, CDPATH set on the line", "CDPATH=/w; " + lbRoundTrips(8, func(int) string { return "build" }) + "touch stamp", ""},
		{"8 different directories, CDPATH in the environment", lbRoundTrips(8, each) + "touch stamp", "/w"},
		{"8 different directories, CDPATH exported on the line", "export CDPATH=/w; " + lbRoundTrips(8, each) + "touch stamp", ""},
		{"8 round trips, CDPATH of . and the home", lbRoundTrips(8, same) + "touch stamp", ".:" + g.home},
		{"7 round trips", lbRoundTrips(7, same) + "touch stamp", "/w"},
		{"20 subshells", strings.Repeat("(cd a && make); ", 20) + "touch stamp", "/w"},
		{"20 placed CDPATH cds", strings.Repeat("CDPATH=/a:/b:. cd build && touch y; ", 20), ""},
	} {
		t.Run("AC-11 "+row.name, func(t *testing.T) {
			vars := map[string]string{}
			if row.cdpath != "" {
				vars["CDPATH"] = row.cdpath
			}
			reason := lbAskWith(t, g.root, row.cmd, vars)
			if lbDriveInCdpath(row.cdpath) {
				lbRefusedForADrivesColon(t, reason, "searches a relative entry of the CDPATH")
				return
			}
			if reason != "" {
				t.Errorf("refused:\n%.600s", reason)
			}
		})
	}
}

// lbDriveInCdpath reports whether a CDPATH value, or a CDPATH assigned in a
// command, holds a path spelt with a drive, as every absolute path of the
// project and the home is on Windows.
func lbDriveInCdpath(text string) bool {
	return lbCdpathDrive.MatchString(filepath.ToSlash(text))
}

// lbCdpathDrive is a drive at the start of a CDPATH entry.
var lbCdpathDrive = regexp.MustCompile(`(?:^|[=:])[A-Za-z]:/`)

// lbRefusedForADrivesColon holds a reason to the refusal of a cd through a
// CDPATH whose entry is spelt with a drive. Bash, Git Bash's too, ends a CDPATH
// entry at every colon, so `C:/p/sub:.` is the entries `C`, `/p/sub` and `.`,
// and `C` is a relative entry the hook rightly cannot place after a cd it
// could not place either. Rows written for a path with no colon in it were
// allowed on macOS and Linux and refused on Windows for this (CI on 38e0ed0);
// there they hold the hook to reading the colon as the shell does.
func lbRefusedForADrivesColon(t *testing.T, reason, want string) {
	t.Helper()
	if !strings.Contains(reason, want) {
		t.Errorf("a CDPATH entry spelt with a drive is two entries to the shell, the first relative: want it refused saying %q, got:\n%.600s", want, reason)
	}
}

// AC-5 and AC-7 (verifier round 13): after a cd the hook cannot place, a cd
// to .claude that searches CDPATH could still be the project's .claude, where
// the shell goes when no entry holds it. Started afresh from the entries, the
// walk knew nothing of the cd's own words, and `python3 tool.py x` there was
// allowed in round 12; trunk refuses it.
func TestAC5AC7AfterAnUnplacedCdACdpathCdToClaudeIsStillReadAsTheProjects(t *testing.T) {
	g := lbG10Setup(t)
	g.rows(t, ".", []string{
		"D=x; cd $D && CDPATH=/tmp cd .claude && python3 tool.py x",
		"D=x; cd $D && CDPATH=/nonexistent cd .claude && python3 tool.py x",
		"D=x; cd $D && CDPATH=<HOME> cd .claude && python3 tool.py x",
	}, nil)
}

// AC-5 and AC-7 (red team, design review round 5, vector 3): a place a CDPATH
// entry sent a lost walk to is carried through the cds after it, a `.` and a
// `./` one included, so a write in the git directory's objects after them is
// refused.
func TestAC5AC7APlaceACdpathEntrySentALostWalkToIsCarriedThroughLaterCds(t *testing.T) {
	g := lbG10Setup(t)
	write(t, g.root, ".git/objects/12/obj", "")
	g.rows(t, ".", []string{
		"D=x; cd $D && CDPATH=<GIT> cd objects && cd 12 && rm -f obj",
		"D=x; cd $D && CDPATH=<GIT> cd objects && cd ./12 && rm -f obj",
		"D=x; cd $D && CDPATH=<GIT> cd objects && cd . && cd ./12 && rm -f obj",
	}, nil)
}

// AC-5 (verifier rounds 12 and 13): a refusal names the operand the command
// writes, read after its options and after `--`, and never an option: `rm
// -rf x` after a cd into the loop's stories was refused naming
// .sdlc/stories/-rf, and in the person's configuration naming `f`.
func TestAC5ARefusalNamesTheOperandNotAnOption(t *testing.T) {
	g := lbG10Setup(t)
	for _, row := range []struct{ cmd, names, notNames string }{
		{"cd <L>/stories && rm -rf x", "<L>/stories/x", "<L>/stories/-"},
		{"CDPATH=<L> cd stories && rm -f -r x", "<L>/stories/x", "<L>/stories/-"},
		{"cd <L>/stories && rm -rf -- -x", "<L>/stories/-x", "<L>/stories/-r"},
		{"cd ~/.claude && rm -rf settings.json", "settings.json", "-rf"},
		{"cd ~/.claude && rm -rf -- -x", "~/.claude/-x", "-rf"},
	} {
		t.Run("AC-5 "+row.cmd, func(t *testing.T) {
			reason := lbAskIn(t, g.root, working, g.expand(row.cmd))
			if reason == "" {
				t.Fatal("allowed; want it refused")
			}
			if names := g.expand(row.names); !strings.Contains(reason, names) {
				t.Errorf("the refusal does not name %q:\n%.600s", names, reason)
			}
			if not := g.expand(row.notNames); strings.Contains(reason, not) {
				t.Errorf("the refusal names an option, %q:\n%.600s", not, reason)
			}
			if strings.HasPrefix(reason, "f ") {
				t.Errorf("the refusal names the option letter f:\n%.600s", reason)
			}
		})
	}
}

// AC-5 (verifier round 12): a line that only names CDPATH, as a grep for it,
// is refused for the relative writes after it, and the reason says the line
// names it and may set it: it said the line sets it, which sent the person
// looking for an assignment that is not there.
func TestAC5ARefusalForAMentionOfCdpathSaysTheLineNamesIt(t *testing.T) {
	g := lbG10Setup(t)
	reason := lbAskIn(t, g.root, working, "grep -rn cdpath internal > notes.txt; cd build && touch stamp")
	if reason == "" {
		t.Fatal("allowed; want it refused")
	}
	if want := "names CDPATH"; !strings.Contains(reason, want) {
		t.Errorf("the refusal does not say %q:\n%.600s", want, reason)
	}
	if strings.Contains(reason, "sets CDPATH") {
		t.Errorf("the refusal says the line sets CDPATH, which it does not:\n%.600s", reason)
	}
}

// AC-5 and AC-7 (verifier round 14, unfreeze 16): after a cd the hook cannot
// place, a place a cd that searches CDPATH certainly leaves for an entry's
// directory is dropped (reachedFirst), and only then: where the entry is in
// every value the cd could search, only absolute entries come before it, an
// empty or `.` entry comes after it, the line neither unsets CDPATH nor
// assigns it empty, and the line, not only the session, sets it. Each row
// breaks one condition, so the shell, standing in <ROOT>/sub/x, can go on to
// <ROOT>/sub/x/x, where lc is a link into the person's Claude Code
// configuration; only the place catches the write, since no name in it is
// held. With conditions 2 or 4 removed, every other test passed and these
// writes were allowed. The session's value alone is never taken as the
// shell's, since the hook reads its own environment. The same CDPATH twice
// drops the place and is allowed, as is one cd: the link is two levels down.
func TestAC5AC7ACdpathPlaceIsDroppedOnlyWhereTheShellCertainlyLeavesIt(t *testing.T) {
	g := lbG10Setup(t)
	if err := os.MkdirAll(filepath.Join(g.root, "sub", "x", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	lbLink(t, filepath.Join(g.home, ".claude"), filepath.Join(g.root, "sub", "x", "x", "lc"))
	const first = "D=x; cd $D; CDPATH=<ROOT>/sub:. cd x; "
	const exported = "D=x; cd $D; export CDPATH=<ROOT>/sub:.; cd x; "
	for _, row := range []struct{ name, cmd, session string }{
		{"S1, the second cd's own CDPATH lacks the entry", first + "CDPATH=/nonexistent:. cd x; touch lc/new.json", ""},
		{"S1, a later export lacks the entry", exported + "export CDPATH=/nonexistent:.; cd x; touch lc/new.json", ""},
		{"S1, a later assignment lacks the entry", "D=x; cd $D; CDPATH=<ROOT>/sub:.; cd x; CDPATH=/nonexistent:.; cd x; touch lc/new.json", ""},
		{"S2, an empty entry first", first + "CDPATH=:<ROOT>/sub:. cd x; touch lc/new.json", ""},
		{"S2b, . first", first + "CDPATH=.:<ROOT>/sub:. cd x; touch lc/new.json", ""},
		{"S3, unset CDPATH between", exported + "unset CDPATH; cd x; touch lc/new.json", ""},
		{"S3b, CDPATH= in front of the second cd", exported + "CDPATH= cd x; touch lc/new.json", ""},
		{"S3b, CDPATH= exported between", exported + "export CDPATH=; cd x; touch lc/new.json", ""},
		// The session's CDPATH the same as the line's, so that only the unset
		// or the empty assignment keeps the place.
		{"S3, unset CDPATH between, the session's CDPATH the same", exported + "unset CDPATH; cd x; touch lc/new.json", "<ROOT>/sub:."},
		{"S3b, CDPATH= in front of the second cd, the session's CDPATH the same", exported + "CDPATH= cd x; touch lc/new.json", "<ROOT>/sub:."},
		{"only the session's CDPATH", "D=x; cd $D; cd x; cd x; touch lc/new.json", "<ROOT>/sub:."},
	} {
		t.Run("AC-7 "+row.name, func(t *testing.T) {
			vars := map[string]string{}
			if row.session != "" {
				vars["CDPATH"] = g.expand(row.session)
			}
			if reason := lbAskWith(t, g.root, g.expand(row.cmd), vars); reason == "" {
				t.Errorf("allowed; the shell can reach lc/new.json in the person's configuration")
			}
		})
	}
	allowed := []string{
		first + "CDPATH=<ROOT>/sub:. cd x; touch lc/new.json",
		first + "touch lc/new.json",
	}
	if !lbDriveInCdpath(g.expand(first)) {
		g.rows(t, ".", nil, allowed)
		return
	}
	for _, row := range allowed {
		t.Run("AC-7 a drive in CDPATH: "+row, func(t *testing.T) {
			lbRefusedForADrivesColon(t, lbAskIn(t, g.root, working, g.expand(row)), "searches a relative CDPATH entry")
		})
	}
}
