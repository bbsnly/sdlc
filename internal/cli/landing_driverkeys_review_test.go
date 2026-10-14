package cli

// US-093, what its design reviews showed the first tests do not hold.
//
// A driver whose name holds a byte that is not valid UTF-8 is not listed by
// `git config --get-regexp` when git matches in a UTF-8 locale, so it was neither
// refused nor overridden, and a merge driver the repository configured ran in the
// stop (the security review's block). The tests here set the locale themselves,
// since internal/testenv leaves it as the machine has it, and run in the C locale
// and in a UTF-8 one. Where a name that is not UTF-8 holds neither an equals sign
// nor {sdlc-private}, the plan chooses between refusing and overriding it, and
// what is held is what must be true either way: the driver never runs and the
// repository's setting never decides what lands. A name that also holds one of
// the two is refused in the same words as the others (AC-1, AC-2).
//
// The rest are the red team's vectors that a test can hold without fixing a
// word: a key written during the stop, several keys and a long one, a key in the
// story worktree's own configuration or an included file, a key listed twice,
// and a replay that fails for another reason. Helpers carry a dr prefix, for
// driver review, and use landing_driverkeys_test.go's.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// drLocale is a locale a test runs in. value is empty where the machine has none
// of the kind, and skip says why the test then does not run.
type drLocale struct{ label, value, skip string }

// drHides reports whether git, in the locale loc, leaves a merge driver whose
// name holds a byte that is not UTF-8 out of the keys it lists: the hazard the
// tests are about. A locale the machine does not have behaves as the C locale
// and lists the key, so it is not one.
func drHides(t *testing.T, loc string) bool {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.CommandContext(t.Context(), "git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	drAppendConfig(t, dir, "[merge \"st\xffeer\"]\n\tdriver = true\n")
	cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "config", "-z", "--get-regexp", `^merge\..*\.driver$`)
	cmd.Env = append(os.Environ(), "LC_ALL="+loc, "LANG="+loc)
	out, _ := cmd.Output()
	return !bytes.Contains(out, []byte("st\xffeer"))
}

// drLocales are the C locale, which every machine has, and the first UTF-8 locale
// from a short list in which git does not list such a key. A machine with none
// still runs the C locale; its UTF-8 case is skipped with the reason, because
// without such a locale the hazard cannot be reproduced there.
func drLocales(t *testing.T) []drLocale {
	t.Helper()
	locales := []drLocale{{label: "in the C locale", value: "C"}}
	for _, loc := range []string{"en_US.UTF-8", "C.UTF-8", "en_GB.UTF-8", "en_US.utf8", "C.utf8"} {
		if drHides(t, loc) {
			return append(locales, drLocale{label: "in a UTF-8 locale (" + loc + ")", value: loc})
		}
	}
	return append(locales, drLocale{
		label: "in a UTF-8 locale",
		skip:  "this machine has no UTF-8 locale in which git leaves a key with a byte that is not UTF-8 out of its listing, so there is nothing to reproduce",
	})
}

// drSetLocale runs the rest of the test in loc.
func drSetLocale(t *testing.T, loc drLocale) {
	t.Helper()
	if loc.value == "" {
		t.Skip(loc.skip)
	}
	t.Setenv("LC_ALL", loc.value)
	t.Setenv("LANG", loc.value)
}

// drAppendConfig adds text, bytes as they are, to the configuration of the
// repository whose main checkout is main.
func drAppendConfig(t *testing.T, main, text string) {
	t.Helper()
	path := filepath.Join(main, ".git", "config")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, text...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// drNotUTF8 are names that hold a byte no UTF-8 text does, and whether the rule
// refuses them under SDLC-E0055 because they also hold what the command line
// cannot carry. The first is the plan's to refuse or to override.
var drNotUTF8 = []struct {
	label, name string
	refused     bool
}{
	{"a name with a byte that is not UTF-8", "st\xffeer", false},
	{"a name with a byte that is not UTF-8 and an equals sign", "st\xff=eer", true},
	{"a name with a byte that is not UTF-8 and {sdlc-private}", "st\xff" + dkPrivate + "eer", true},
}

// drMergeDriver configures a merge driver of the name, as a script that leaves a
// mark and writes the story's file somewhere else if it runs, and names it for
// billing.go by an attribute nobody committed. Trunk edits billing.go as well,
// so that putting the story onto it is a merge of that file. It returns the
// mark, which exists only if the driver ran.
func drMergeDriver(t *testing.T, main, name string) string {
	t.Helper()
	bin := wtTempDir(t)
	mark, script := filepath.Join(bin, "ran"), filepath.Join(bin, "driver")
	body := "#!/bin/sh\necho ran > " + suSh(mark) + "\nprintf 'package billing\\n' > \"$2\"\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	drAppendConfig(t, main, "[merge \""+name+"\"]\n\tdriver = "+suSh(script)+" %O %A %B\n")
	writeFile(t, main, ".git/info/attributes", "billing.go merge="+name+"\n")
	swCommitFile(t, main, "billing.go", "// trunk's header\n"+ldBilling, "trunk edits billing.go")
	return mark
}

// ---------------------------------------------------------------- the locale

// AC-1 (security review, the block): a merge driver whose name holds a byte that
// is not UTF-8 does not run in the stop, in any locale: not in the C locale and
// not in a UTF-8 one, where git leaves such a key out of what it lists. A name
// that also holds an equals sign or {sdlc-private} is refused under SDLC-E0055
// in the same words as the others, with nothing changed; the one that holds
// neither is refused or overridden, as the plan chooses, and where it is
// refused the words are the same, and where it is not the stop leaves the story
// as it was or lands it without the driver's text.
func TestAC1AMergeDriverNamedWithBytesThatAreNotUTF8NeverRunsAndIsRefusedWhereItCannotBeOverriddenWhateverTheLocale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the driver here is a shell script, and a key with a byte that is not UTF-8 cannot survive Windows' argument conversion")
	}
	for _, loc := range drLocales(t) {
		for _, tc := range drNotUTF8 {
			t.Run("AC-1 "+loc.label+", "+tc.label, func(t *testing.T) {
				drSetLocale(t, loc)
				main, wt := dkFinished(t)
				mark := drMergeDriver(t, main, tc.name)
				key := dkMerge.key(tc.name)
				defer func() {
					if wtExists(mark) {
						t.Error("the repository's merge driver ran in the stop")
					}
				}()

				if tc.refused {
					dkAsDriverRule(t, dkRefused(t, main, wt), key)
					return
				}
				trunk := ldTrunk(t, main)
				before := otSnap(t, main, wt, "billing.go", "refund.go", "CODEMAP.md")
				r := wtRun(t, wt, ldSession, "", "stop")
				if r.code == 0 {
					if got, _ := otAt(t, main, ldTrunk(t, main), "billing.go"); got == "package billing\n" {
						t.Errorf("trunk's billing.go is the driver's text: the repository's driver decided what landed")
					}
					return
				}
				out := r.stdout + r.stderr
				if !strings.Contains(out, ldAny) {
					t.Errorf("stop exited %d with no code:\n%s", r.code, out)
				}
				if strings.Contains(out, ldCheckFailed) {
					dkAsDriverRule(t, dkParse(t, out), key)
				}
				before.unchanged(t, otSnap(t, main, wt, "billing.go", "refund.go", "CODEMAP.md"), "the refused stop")
				if got := ldTrunk(t, main); got != trunk {
					t.Errorf("the refused stop moved trunk to %s", got)
				}
			})
		}
	}
}

// AC-2 (security review, the block): the same for a diff driver. A name with a
// byte that is not UTF-8 and an equals sign or {sdlc-private} is refused on every
// stop, trunk moved or not, in the same words, whatever the locale; the name
// with neither is refused in them or the stop goes on, as the plan chooses.
func TestAC2ADiffDriverNamedWithBytesThatAreNotUTF8IsRefusedWhereItCannotBeOverriddenWhateverTheLocale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a key with a byte that is not UTF-8 cannot survive Windows' argument conversion")
	}
	for _, loc := range drLocales(t) {
		for _, tc := range drNotUTF8 {
			for _, moved := range []bool{false, true} {
				when := "trunk where it was"
				if moved {
					when = "trunk moved"
				}
				t.Run("AC-2 "+loc.label+", "+tc.label+", "+when, func(t *testing.T) {
					drSetLocale(t, loc)
					main, wt := dkFinished(t)
					if moved {
						ldMoveTrunk(t, main)
					}
					drAppendConfig(t, main, "[diff \""+tc.name+"\"]\n\talgorithm = patience\n")
					key := dkDiff.key(tc.name)

					if tc.refused {
						dkAsDriverRule(t, dkRefused(t, main, wt), key)
						return
					}
					r := wtRun(t, wt, ldSession, "", "stop")
					if r.code == 0 {
						return
					}
					out := r.stdout + r.stderr
					if !strings.Contains(out, ldCheckFailed) {
						t.Fatalf("stop exited %d, want the story landed or refused with %s:\n%s", r.code, ldCheckFailed, out)
					}
					dkAsDriverRule(t, dkParse(t, out), key)
				})
			}
		}
	}
}

// ---------------------------------------------------------------- the red team's vectors

// AC-2 (vector 3): a diff driver written during the stop is refused. The
// project's test command writes `diff.a=b.algorithm` to the configuration once
// the story is on the trunk that moved, so the key is not there at the first
// check or at the replay: a check made once at the top of the stop does not
// pass. Nothing landed.
func TestAC2ADiffDriverWrittenDuringTheStopIsRefusedByTheCheckAfterTheCommands(t *testing.T) {
	t.Parallel()
	const key = "diff.a=b.algorithm"
	main, wt := dkFinishedWith(t, map[string]string{"test": "test ! -f CHANGES.md || git config diff.a=b.algorithm patience"})
	moved := dkMoved(t, main)
	if _, ok := swGitTry(t, main, "config", "--get", "--", key); ok {
		t.Fatal("precondition: the key is in the configuration before the stop")
	}

	out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
	if _, ok := swGitTry(t, main, "config", "--get", "--", key); !ok {
		t.Fatalf("precondition: the test command did not write the key, so the stop was refused for another reason:\n%s", out)
	}
	dkAsDriverRule(t, dkParse(t, out), key)
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("trunk moved from %s to %s", moved, got)
	}
	ldStillHeld(t, main, wt)
}

// drNumbers are the whole numbers in text that stand on their own: not part of
// a story's id or a commit.
func drNumbers(text string) []int {
	var out []int
	for _, m := range regexp.MustCompile(`(?:^|[\s(,])(\d+)(?:[\s).,;:]|$)`).FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[1])
		out = append(out, n)
	}
	return out
}

// drShown is how many of keys the refusal quotes, and holds the rest to being
// counted: the refusal says how many it left out, as a number.
func drShown(t *testing.T, r dkRefusal, keys []string) int {
	t.Helper()
	shown := 0
	for _, key := range keys {
		shown += min(1, strings.Count(r.what+" "+r.why, strconv.Quote(key)))
	}
	if left := len(keys) - shown; left > 0 {
		var counted bool
		for _, n := range drNumbers(r.what + " " + r.why) {
			counted = counted || n == left
		}
		if !counted {
			t.Errorf("the refusal shows %d of %d keys and does not say that %d more are left out:\n%s", shown, len(keys), left, r.out)
		}
	}
	return shown
}

// drKeys configures n drivers of kind, each under a name with an equals sign,
// and returns their keys in the order the configuration lists them.
func drKeys(t *testing.T, main string, kind dkKind, n int, namer func(i int) string) []string {
	t.Helper()
	var section strings.Builder
	var keys []string
	for i := range n {
		name := namer(i)
		section.WriteString("[" + kind.section + " " + strconv.Quote(name) + "]\n\t" + kind.variable + " = " + kind.value + "\n")
		keys = append(keys, kind.key(name))
	}
	drAppendConfig(t, main, section.String())
	return keys
}

// AC-1 and AC-2 (vectors 5 and 6): more than one key is shown, the rest are
// counted, and a long key is cut and said to be cut. The numbers are the plan's,
// so none is pinned: two keys are both shown; twelve show more than one and
// fewer than twelve and say how many are left; a key of three hundred
// characters is cut well short of three hundred, and the refusal says so, and
// five of three thousand each stay as small as one; a key of twenty characters
// is shown whole and not said to be cut.
func TestAC1AndAC2SeveralKeysAreShownTheRestCountedAndALongKeyCutAndSaid(t *testing.T) {
	t.Parallel()
	cut := regexp.MustCompile(`(?i)\b(cut|truncated|shortened)\b`)
	longest := func(text string) int {
		best := 0
		for _, run := range regexp.MustCompile(`x+`).FindAllString(text, -1) {
			best = max(best, len(run))
		}
		return best
	}
	for _, kind := range dkKinds {
		t.Run("AC-1 and AC-2 two keys, a "+kind.label, func(t *testing.T) {
			t.Parallel()
			main, wt := dkFinished(t)
			dkMoved(t, main)
			keys := drKeys(t, main, kind, 2, func(i int) string { return "two" + strconv.Itoa(i) + "=x" })
			r := dkRefused(t, main, wt)
			for _, key := range keys {
				if !strings.Contains(r.what+" "+r.why, strconv.Quote(key)) {
					t.Errorf("the refusal for two bad keys does not show %s:\n%s", key, r.out)
				}
			}
		})
		t.Run("AC-1 and AC-2 twelve keys, a "+kind.label, func(t *testing.T) {
			t.Parallel()
			main, wt := dkFinished(t)
			dkMoved(t, main)
			keys := drKeys(t, main, kind, 12, func(i int) string { return "k" + strconv.Itoa(100+i) + "=x" })
			r := dkRefused(t, main, wt)
			if shown := drShown(t, r, keys); shown < 2 || shown >= len(keys) {
				t.Errorf("the refusal shows %d of %d keys, want more than one and fewer than all:\n%s", shown, len(keys), r.out)
			}
		})
		t.Run("AC-1 and AC-2 a key of three hundred characters, a "+kind.label, func(t *testing.T) {
			t.Parallel()
			main, wt := dkFinished(t)
			dkMoved(t, main)
			drKeys(t, main, kind, 1, func(int) string { return "st=" + strings.Repeat("x", 300) })
			r := dkRefused(t, main, wt)
			if n := longest(r.out); n < 8 || n > 256 {
				t.Errorf("the refusal shows a run of %d of the key's characters, want it cut well short of 300 and still showing how it begins:\n%s", n, r.out)
			}
			if !cut.MatchString(r.what + " " + r.why) {
				t.Errorf("the refusal does not say the key was cut short:\n%s", r.out)
			}
		})
		t.Run("AC-1 and AC-2 five keys of three thousand characters, a "+kind.label, func(t *testing.T) {
			t.Parallel()
			main, wt := dkFinished(t)
			dkMoved(t, main)
			drKeys(t, main, kind, 5, func(i int) string { return "p" + strconv.Itoa(i) + "=" + strings.Repeat("x", 3000) })
			r := dkRefused(t, main, wt)
			if n := longest(r.out); n < 8 || n > 256 {
				t.Errorf("a run of %d of a key's characters is shown, want a key cut well short of 3000 and still shown:\n%s", n, r.out[:min(len(r.out), 1500)])
			}
			if len(r.out) > 2500 {
				t.Errorf("the output is %d bytes for five long keys, want it as small as for one", len(r.out))
			}
		})
		t.Run("AC-1 and AC-2 a key of twenty characters, a "+kind.label, func(t *testing.T) {
			t.Parallel()
			main, wt := dkFinished(t)
			dkMoved(t, main)
			keys := drKeys(t, main, kind, 1, func(int) string { return "short=" + strings.Repeat("y", 14) })
			r := dkRefused(t, main, wt)
			if !strings.Contains(r.what+" "+r.why, strconv.Quote(keys[0])) {
				t.Errorf("a short key is not shown whole:\n%s", r.out)
			}
			if cut.MatchString(r.what + " " + r.why) {
				t.Errorf("the refusal says a key was cut short when none was:\n%s", r.out)
			}
		})
	}
}

// AC-1 and AC-2 (red team F5): a key that the configuration lists twice, in two
// files with another key between, is counted once. Seven distinct bad keys, the
// first also set again at the end: whatever number is shown, it and the number
// left out make seven, and the repeated key is shown once.
func TestAC1AndAC2AKeyTheConfigurationListsTwiceIsCountedOnce(t *testing.T) {
	t.Parallel()
	for _, kind := range dkKinds {
		t.Run("AC-1 and AC-2 a "+kind.label, func(t *testing.T) {
			t.Parallel()
			main, wt := dkFinished(t)
			dkMoved(t, main)
			name := func(i int) string { return "d" + strconv.Itoa(i) + "=x" }
			included := filepath.Join(wtTempDir(t), "included")
			writeFile(t, filepath.Dir(included), filepath.Base(included),
				"["+kind.section+" "+strconv.Quote(name(0))+"]\n\t"+kind.variable+" = "+kind.value+"\n")
			config := filepath.Join(main, ".git", "config")
			raw, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			head := "[include]\n\tpath = " + filepath.ToSlash(included) + "\n"
			if err := os.WriteFile(config, append([]byte(head), raw...), 0o644); err != nil {
				t.Fatal(err)
			}
			keys := drKeys(t, main, kind, 7, name)
			drAppendConfig(t, main, "["+kind.section+" "+strconv.Quote(name(0))+"]\n\t"+kind.variable+" = "+kind.value+"\n")

			r := dkRefused(t, main, wt)
			drShown(t, r, keys)
			if n := strings.Count(r.what+" "+r.why, strconv.Quote(keys[0])); n != 1 {
				t.Errorf("%s is shown %d times, want once:\n%s", keys[0], n, r.out)
			}
		})
	}
}

// AC-1 and AC-2 (red team vector 8): a bad key that only the story worktree's own
// configuration (extensions.worktreeConfig) holds, or only a file the
// configuration includes, is refused: the stop reads what the worktree it runs in
// reads, as the measure and the replay do.
func TestAC1AndAC2ADriverOnlyTheStoryWorktreesConfigurationOrAnIncludedFileHoldsIsRefused(t *testing.T) {
	t.Parallel()
	for _, kind := range dkKinds {
		t.Run("AC-1 and AC-2 the worktree's own configuration, a "+kind.label, func(t *testing.T) {
			t.Parallel()
			main, wt := dkFinished(t)
			dkMoved(t, main)
			swGit(t, main, "config", "extensions.worktreeConfig", "true")
			key := kind.key("st=eer")
			swGit(t, wt, "config", "--worktree", "--", key, kind.value)
			if _, ok := swGitTry(t, main, "config", "--get", "--", key); ok {
				t.Fatal("precondition: the main checkout reads the key too, so nothing shows the stop reads the worktree's own")
			}
			dkAsDriverRule(t, dkRefused(t, main, wt), key)
		})
		t.Run("AC-1 and AC-2 an included file, a "+kind.label, func(t *testing.T) {
			t.Parallel()
			main, wt := dkFinished(t)
			dkMoved(t, main)
			dir := wtTempDir(t)
			writeFile(t, dir, "included", "["+kind.section+" \"st=eer\"]\n\t"+kind.variable+" = "+kind.value+"\n")
			swGit(t, main, "config", "include.path", filepath.ToSlash(filepath.Join(dir, "included")))
			dkAsDriverRule(t, dkRefused(t, main, wt), kind.key("st=eer"))
		})
	}
}

// AC-1 (red team vector 10): a replay that fails for a reason of git's own, with
// no bad key, is not taken for sdlc's rule. With no committer to make the story's
// commits again with, the stop that has to put the story onto a trunk that moved
// is refused with the text it had before this story: git's own reason, quoted as
// git said it, and the committer in the fix.
func TestAC1AReplayThatFailsForAReasonOfGitsOwnKeepsTheTextItHadBefore(t *testing.T) {
	t.Parallel()
	main, wt, _ := ldWalkedFrom(t, ldWalk{finished: true})
	swGit(t, main, "config", "user.useConfigOnly", "true")
	ldBookkeeping(t, wt)
	ldMoveTrunk(t, main)

	r := wtRun(t, wt, ldSession, "", "stop")
	if r.code == 0 {
		t.Skip("this machine's git makes the commits again with no committer configured, so there is no failure of git's own to hold")
	}
	refusal := dkParse(t, r.stdout+r.stderr)
	if !strings.Contains(refusal.why, "git would not make them again there, saying") {
		t.Errorf("the refusal does not quote what git said:\n%s", refusal.out)
	}
	if !strings.Contains(refusal.fix, "committer") || !strings.Contains(refusal.fix, "user.name") {
		t.Errorf("the fix does not talk of a committer:\n%s", refusal.out)
	}
	if strings.Contains(refusal.all(), "sdlc's rule") || strings.Contains(refusal.all(), "configured under names") {
		t.Errorf("a failure of git's own is put down to a driver's name:\n%s", refusal.out)
	}
}
