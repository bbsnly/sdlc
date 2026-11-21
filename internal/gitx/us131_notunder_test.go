package gitx

import (
	"strings"
	"testing"
)

// US-131 AC-9: `sdlc status` counts a decision as unread until a person's ack
// covers the commit it was recorded on, and asks NotUnder which commits a
// history does not hold yet. These hold it to that one question: what it
// answers for a commit since holds, one it does not, one git has as another
// kind of object or not at all, and what it refuses before git is asked.

func TestAC9IsFullHashTakesOnlyAFullLowerCaseObjectName(t *testing.T) {
	t.Parallel()
	sha1 := strings.Repeat("0123456789", 4)
	for _, c := range []struct {
		name, s string
		want    bool
	}{
		{"AC-9 forty hex digits", sha1, true},
		{"AC-9 sixty-four hex digits", sha1 + strings.Repeat("abcdef", 4), true},
		{"AC-9 nothing", "", false},
		{"AC-9 a short hash", sha1[:12], false},
		{"AC-9 one digit short", sha1[:39], false},
		{"AC-9 one digit over", sha1 + "a", false},
		{"AC-9 upper case", strings.ToUpper(strings.Repeat("abcdef0123", 4)), false},
		{"AC-9 a letter past f", sha1[:39] + "g", false},
		{"AC-9 an option", "-" + sha1[:39], false},
		{"AC-9 a name", "HEAD", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := IsFullHash(c.s); got != c.want {
				t.Errorf("IsFullHash(%q) = %v, want %v", c.s, got, c.want)
			}
		})
	}
}

func TestAC9NotUnderSaysWhichCommitsAHistoryDoesNotHoldYet(t *testing.T) {
	t.Parallel()
	root, commits := luHistory(t)
	// A commit off the first, which main never reached.
	side := luCommitOf(t, root, commits[0], map[string]string{"side.txt": "side\n"})
	blob := luGit(t, root, nil, "not a commit\n", "hash-object", "-w", "--stdin")
	missing := strings.Repeat("0", 39) + "1"
	since := commits[2]

	got, err := NotUnder(t.Context(), root, since, []string{commits[0], commits[1], since, commits[3], side, blob, missing})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		what, commit string
		want         bool
	}{
		{"an ancestor of since", commits[0], false},
		{"the parent of since", commits[1], false},
		{"since itself", since, false},
		{"a commit after since", commits[3], true},
		{"a commit on another line", side, true},
		{"a blob", blob, true},
		{"a commit git does not have", missing, true},
	} {
		if got[c.commit] != c.want {
			t.Errorf("AC-9 NotUnder for %s = %v, want %v", c.what, got[c.commit], c.want)
		}
	}
	if len(got) != 7 {
		t.Errorf("AC-9 NotUnder answered for %d commits, want 7: %v", len(got), got)
	}

	// Only commits git does not have: the second call is never needed.
	if got, err := NotUnder(t.Context(), root, since, []string{missing, blob}); err != nil || !got[missing] || !got[blob] || len(got) != 2 {
		t.Errorf("AC-9 NotUnder for no known commit = %v, %v, want both not held", got, err)
	}
	// No commits at all is an empty answer, not a failure.
	if got, err := NotUnder(t.Context(), root, since, nil); err != nil || len(got) != 0 {
		t.Errorf("AC-9 NotUnder of no commits = %v, %v, want an empty answer", got, err)
	}
}

// Every value is held to a full object name before git sees it, the history's
// tip too: a value that starts with a dash would be read as an option, and a
// name or a short hash as whatever it resolves to.
func TestAC9NotUnderRefusesAnythingButAFullHashBeforeGitIsAsked(t *testing.T) {
	t.Parallel()
	root, commits := luHistory(t)
	for _, c := range []struct {
		name, since string
		commits     []string
	}{
		{"AC-9 an option as a commit", commits[3], []string{"--all"}},
		{"AC-9 a name as a commit", commits[3], []string{"HEAD"}},
		{"AC-9 a short hash as a commit", commits[3], []string{commits[0][:12]}},
		{"AC-9 a name as since", "main", []string{commits[0]}},
		{"AC-9 an option as since, with no commits", "--all", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := NotUnder(t.Context(), root, c.since, c.commits)
			if got != nil {
				t.Errorf("NotUnder answered %v for a value that is not a full hash", got)
			}
			wantUnreadable(t, err, "NotUnder")
		})
	}
}

// A git that answers for fewer commits than it was given, or fails on the
// second call, fails the answer whole rather than reading a commit as held or
// not by a line that belongs to another.
func TestAC9NotUnderFailsWholeWhereGitsAnswerCannotBeRead(t *testing.T) {
	commits := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	since := strings.Repeat("c", 40)
	for _, c := range []struct{ name, script string }{
		{"AC-9 cat-file answers for one commit of two", "#!/bin/sh\nprintf '%s commit\\n' " + commits[0] + "\n"},
		{"AC-9 rev-list fails", "#!/bin/sh\ncase \"$2\" in\ncat-file) while read -r c; do printf '%s commit\\n' \"$c\"; done ;;\n*) exit 3 ;;\nesac\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fgFakeGit(t, c.script)
			got, err := NotUnder(t.Context(), t.TempDir(), since, commits)
			if got != nil {
				t.Errorf("NotUnder answered %v from an answer it could not read", got)
			}
			wantUnreadable(t, err, "NotUnder")
		})
	}
}

// A directory that is not a repository cannot be asked, and says so with the
// code a person can look up.
func TestAC9NotUnderOutsideARepositoryIsTheRepositoryUnreadable(t *testing.T) {
	t.Parallel()
	_, commits := luHistory(t)
	got, err := NotUnder(t.Context(), t.TempDir(), commits[3], []string{commits[0]})
	if got != nil {
		t.Errorf("NotUnder answered %v outside a repository", got)
	}
	wantUnreadable(t, err, "NotUnder outside a repository")
}
