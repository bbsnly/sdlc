package gitx

// US-093, what its design reviews showed the first tests do not hold: the keys of
// the merge and diff drivers a repository configures are found whatever the
// locale of the process that asks.
//
// `git config --get-regexp` matches in the caller's locale, so in a UTF-8 locale
// a key whose name holds a byte that is not valid UTF-8 was left out of the
// listing. It was then neither refused nor overridden, and the merge driver ran
// in the replay. internal/testenv leaves the locale as the machine has it, so
// these tests set it themselves, in the C locale and in a UTF-8 one. Where the
// name also holds an equals sign or {sdlc-private} the replay refuses it; where
// it holds neither, the plan chooses between refusing and overriding it, and
// what is held is that the driver never runs and the replay does not go on
// without the override (AC-1). Both measures hand git the override for the diff
// key, read off the call as the empty name's test does (AC-2). Helpers carry a
// dl prefix, for driver locale.

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// dlLocale is a locale a test runs in; value is empty where the machine has no
// locale of the kind, and skip says why the test then does not run.
type dlLocale struct{ label, value, skip string }

// dlAppendConfig adds text, bytes as they are, to root's repository
// configuration.
func dlAppendConfig(t *testing.T, root, text string) {
	t.Helper()
	path := filepath.Join(root, ".git", "config")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, text...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dlHides reports whether git, in the locale loc, leaves a merge driver with a
// byte that is not UTF-8 in its name out of the keys it lists. A locale the
// machine does not have behaves as the C locale and lists it.
func dlHides(t *testing.T, loc string) bool {
	t.Helper()
	root := repo(t)
	dlAppendConfig(t, root, "[merge \"st\xffeer\"]\n\tdriver = true\n")
	cmd := exec.CommandContext(t.Context(), "git", "-C", root, "config", "-z", "--get-regexp", `^merge\..*\.driver$`)
	cmd.Env = append(os.Environ(), "LC_ALL="+loc, "LANG="+loc)
	out, _ := cmd.Output()
	return !bytes.Contains(out, []byte("st\xffeer"))
}

// dlLocales are the C locale and the first UTF-8 locale from a short list in
// which git does not list such a key. A machine with none still runs the C
// locale, and its UTF-8 case is skipped with the reason.
func dlLocales(t *testing.T) []dlLocale {
	t.Helper()
	locales := []dlLocale{{label: "in the C locale", value: "C"}}
	for _, loc := range []string{"en_US.UTF-8", "C.UTF-8", "en_GB.UTF-8", "en_US.utf8", "C.utf8"} {
		if dlHides(t, loc) {
			return append(locales, dlLocale{label: "in a UTF-8 locale (" + loc + ")", value: loc})
		}
	}
	return append(locales, dlLocale{
		label: "in a UTF-8 locale",
		skip:  "this machine has no UTF-8 locale in which git leaves a key with a byte that is not UTF-8 out of its listing, so there is nothing to reproduce",
	})
}

func dlSetLocale(t *testing.T, loc dlLocale) {
	t.Helper()
	if loc.value == "" {
		t.Skip(loc.skip)
	}
	t.Setenv("LC_ALL", loc.value)
	t.Setenv("LANG", loc.value)
}

// dlHoldsRefusal holds err to a refusal a person can read that names the key of
// the merge driver called name, and the checkout wt to nothing part-way.
func dlHoldsRefusal(t *testing.T, err error, wt, name string) {
	t.Helper()
	var refusal *sdlcerr.Error
	if !errors.As(err, &refusal) {
		t.Fatalf("Replay returned %v, want a refusal a person can read, naming the driver", err)
	}
	if key := "merge." + name + ".driver"; !strings.Contains(refusal.What+" "+refusal.Why, strconv.Quote(key)) {
		t.Errorf("the refusal says %q / %q, and does not name the key %s", refusal.What, refusal.Why, strconv.Quote(key))
	}
	if got := rpPartWay(t, wt); got != "" {
		t.Errorf("%s is part-way after a replay that was refused before git ran", got)
	}
}

// AC-1: the replay does not run a merge driver whose name holds a byte that is
// not UTF-8, in the C locale or in a UTF-8 one. A name that also holds an equals
// sign or {sdlc-private} is refused before git runs, naming its key; one that
// holds neither is refused or overridden, and either way the replay does not
// finish and nothing is left part-way. Each case ends with the rebase git would
// run by hand in the same locale, which does run the driver: git does read the
// attribute as naming it, so the absence of the mark means something.
func TestAC1TheReplayNeverRunsAMergeDriverNamedWithBytesThatAreNotUTF8WhateverTheLocale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the driver here is a shell script, and a key with a byte that is not UTF-8 cannot survive Windows' argument conversion")
	}
	for _, loc := range dlLocales(t) {
		for _, tc := range []struct {
			label, name string
			refused     bool
		}{
			{"a byte that is not UTF-8", "st\xffeer", false},
			{"a byte that is not UTF-8 and an equals sign", "st\xff=eer", true},
			{"a byte that is not UTF-8 and {sdlc-private}", "st\xff" + privateDir + "eer", true},
		} {
			t.Run("AC-1 "+loc.label+", "+tc.label, func(t *testing.T) {
				dlSetLocale(t, loc)
				root, fork := rpRepo(t)
				wt, tip := rpStory(t, root)
				trunk := rpTrunkMoves(t, root)
				bin, marks := lgTempDir(t), lgTempDir(t)
				script := rpScript(t, bin, marks, "driver", rcSteered)
				dlAppendConfig(t, root, "[merge \""+tc.name+"\"]\n\tdriver = '"+script+"' %O %A %B\n")
				write(t, root, ".git/info/attributes", "billing.go merge="+tc.name+"\n")
				before := rpSnap(t, wt)

				err := Replay(t.Context(), wt, trunk, fork)
				rpNoneRan(t, marks)
				if err == nil {
					t.Fatalf("Replay reported no error for a path whose merge driver was to be kept out of it; billing.go is\n%s", lgRead(t, wt, "billing.go"))
				}
				if tc.refused {
					dlHoldsRefusal(t, err, wt, tc.name)
				} else if rpPartWay(t, wt) != "" {
					if err := AbandonReplay(t.Context(), wt); err != nil {
						t.Fatalf("AbandonReplay: %v", err)
					}
					rpNoneRan(t, marks)
				}
				if got := rpSnap(t, wt); got != before {
					t.Errorf("the replay left the checkout changed:\nbefore\n%s\nafter\n%s", before, got)
				}
				if got := lgRev(t, wt, "HEAD"); got != tip {
					t.Errorf("the story's tip moved from %s to %s", tip, got)
				}
				if got := lgRev(t, root, "refs/heads/main"); got != trunk {
					t.Errorf("trunk moved from %s to %s", trunk, got)
				}

				rcUnsealed(t, wt, trunk, fork)
				rcRan(t, marks)
			})
		}
	}
}

// AC-2: both measures hand git the algorithm for a diff driver whose name holds
// a byte that is not UTF-8, so the repository's own algorithm for it does not
// decide what a fingerprint is, in the C locale and in a UTF-8 one. It is read
// off the call, as the empty name's is, because on git 2.42 and later the
// fingerprint is the same with or without it. (A name that also holds an equals
// sign is the stop's to refuse, and git itself rejects the key the measures hand
// it, so it is not measured here.)
func TestAC2BothMeasuresOverrideADiffDriverNamedWithABytethatIsNotUTF8WhateverTheLocale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a key with a byte that is not UTF-8 cannot survive Windows' argument conversion")
	}
	for _, loc := range dlLocales(t) {
		t.Run("AC-2 "+loc.label, func(t *testing.T) {
			dlSetLocale(t, loc)
			root, from := lgRepo(t)
			_, to := lgStoryWork(t, root)
			dlAppendConfig(t, root, "[diff \"custom\"]\n\talgorithm = patience\n[diff \"st\xffeer\"]\n\talgorithm = patience\n")
			log := dnLoggingGit(t)

			for _, measure := range []struct {
				name string
				run  func() error
			}{
				{"Fingerprint", func() error { _, err := Fingerprint(t.Context(), root, from, to); return err }},
				{"LinesFingerprint", func() error { _, err := LinesFingerprint(t.Context(), root, from, to); return err }},
			} {
				dnCalls(t, log)
				if err := measure.run(); err != nil {
					t.Fatalf("%s: %v", measure.name, err)
				}
				calls := dnCalls(t, log)
				if !strings.Contains(calls, " -c diff.custom.algorithm=myers ") {
					t.Fatalf("precondition: %s does not override diff.custom.algorithm, so the log shows nothing about the others:\n%s", measure.name, calls)
				}
				if !strings.Contains(calls, " -c diff.st\xffeer.algorithm=myers ") {
					t.Errorf("%s was not handed the override for the diff driver with a byte that is not UTF-8, so the repository's own algorithm for it stays:\n%s", measure.name, calls)
				}
			}
		})
	}
}

// AC-2 (red team, vector 7): with no diff driver of an empty name configured,
// git is not handed one. The empty name's override comes from the configuration
// and not from a constant.
func TestAC2NoEmptyNameOverrideIsHandedToGitWhereNoneIsConfigured(t *testing.T) {
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	run(t, root, "config", "diff.custom.algorithm", "patience")
	log := dnLoggingGit(t)

	for _, measure := range []func() error{
		func() error { _, err := Fingerprint(t.Context(), root, from, to); return err },
		func() error { _, err := LinesFingerprint(t.Context(), root, from, to); return err },
	} {
		dnCalls(t, log)
		if err := measure(); err != nil {
			t.Fatal(err)
		}
		calls := dnCalls(t, log)
		if !strings.Contains(calls, " -c diff.custom.algorithm=myers ") {
			t.Fatalf("precondition: the call does not override diff.custom.algorithm:\n%s", calls)
		}
		if strings.Contains(calls, "diff..algorithm") {
			t.Errorf("git was handed an override for a diff driver with an empty name where none is configured:\n%s", calls)
		}
	}
}
