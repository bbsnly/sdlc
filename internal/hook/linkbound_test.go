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
	gap := lbADirectoryAtATime(t, root, "-g", 50, "elsewhere", "x")
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
		t.Run("AC-7 "+cmd[:16]+", through 50 links", func(t *testing.T) {
			reason := lbAsk(t, root, cmd)
			if !strings.Contains(reason, lbWordCode) || !strings.Contains(reason, lookup) {
				t.Errorf("%.40s..., through 50 links one directory at a time: want it refused under %s saying %q, got:\n%s", cmd, lbWordCode, lookup, reason)
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
// what macOS (32) and Linux (40) follow in one lookup.
func TestAC5AfterACdTheRefusalNamesTheWordAsTypedThroughTheHook(t *testing.T) {
	const lookup = "goes through more links than this system follows in one lookup"
	root := lbProject(t)
	sub := filepath.Join(root, "sub")
	write(t, root, "sub/.keep", "")
	lbAtOneName(t, sub, "lnk65", 65, "../elsewhere", "x")
	lbAtOneName(t, sub, "st41", 41, "../.sdlc/state", "active")
	lbAtOneName(t, root, "e65", 65, "elsewhere", "x")
	lbAtOneName(t, root, "-e65", 65, "elsewhere", "x")
	inSub, inRoot := make([]string, 18), make([]string, 18)
	for i := range 18 {
		inSub[i] = lbAtOneName(t, sub, fmt.Sprintf("w%d", i), 30, "../elsewhere", "n")
		inRoot[i] = lbAtOneName(t, root, fmt.Sprintf("v%d", i), 30, "elsewhere", "n")
	}
	for _, tc := range []struct{ cmd, want string }{
		{"cd sub && touch lnk65/x", "lnk65/x" + lbOver64},
		{"cd sub && touch st41/active", "st41/active " + lookup},
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
// directory, is past what macOS (32) and Linux (40) follow in one lookup; 65
// is past the bound of one word. The chains lead to a directory nothing
// protects, so only the bound can refuse them; and the session holding the
// story and one that does not are held alike.
func TestAC5AC7AWordThroughAVariableForTheHomeIsBoundAsItIsPlaced(t *testing.T) {
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	write(t, home, "elsewhere/.keep", "")
	root := lbProject(t)
	chains := map[int]string{
		41: lbADirectoryAtATime(t, home, "hb41", 41, "elsewhere", "x"),
		65: lbADirectoryAtATime(t, home, "hb65", 65, "elsewhere", "x"),
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
	for _, n := range []int{41, 65} {
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
// holding the story and in one that does not. 41 links, one in each directory,
// is past what macOS (32) and Linux (40) follow in one lookup.
func TestAC5AC7FromAWorkingDirectoryPastTheLookupLimitTheHookNamesIt(t *testing.T) {
	root := lbProject(t)
	deep := filepath.Join(root, filepath.FromSlash(lbADirectoryAtATime(t, root, "wd41", 41, "elsewhere", "")))
	shallow := filepath.Join(root, filepath.FromSlash(lbADirectoryAtATime(t, root, "wd10", 10, "elsewhere", "")))
	for _, session := range []string{working, "session-b"} {
		t.Run("AC-7 echo ok, 41 links deep, "+session, func(t *testing.T) {
			reason := lbAskIn(t, deep, session, "echo ok")
			want := "the working directory goes through more links than this system follows in one lookup"
			if !strings.Contains(reason, want) || !strings.Contains(reason, lbWordCode) {
				t.Errorf("want it refused under %s saying %q, got:\n%s", lbWordCode, want, reason)
			}
		})
		t.Run("AC-11 echo ok, 10 links deep, "+session, func(t *testing.T) {
			if reason := lbAskIn(t, shallow, session, "echo ok"); reason != "" {
				t.Errorf("refused:\n%s", reason)
			}
		})
	}
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
// round 5). 41 links at one name is past macOS (32) and Linux (40).
func TestAC5AC7ACdIntoADirectoryPastTheLookupLimitIsNamedThroughTheHook(t *testing.T) {
	root := lbProject(t)
	lbAtOneName(t, root, "e41", 41, "elsewhere", "x")
	for _, cmd := range []string{"cd e41 && touch x", "cd e41; echo x > y"} {
		t.Run("AC-7 "+cmd, func(t *testing.T) {
			reason := lbAsk(t, root, cmd)
			want := "e41 goes through more links than this system follows in one lookup"
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
}
