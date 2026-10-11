package hook

// US-029.1, where a word of a shell command lands among the repository's
// checkouts. The expectations are the plan's step 13(b) and 13(f), THREATS.md's
// T2 and T12, and the round-3 red-team review: a checkout is found by walking up
// to a .git that belongs to this repository, a .git that does not belong is
// walked past and never stopped at, a pointer copied out of a real worktree
// makes nothing a checkout, and what the main checkout keeps is still found
// when core.bare or commondir says otherwise.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// ckLandings is what the hook asks of every word of a shell command run in
// project.
func ckLandings(t *testing.T, project string) *landings {
	t.Helper()
	l := newLandings(placeOf(project), pathrules.NewResolver(project))
	if l == nil {
		t.Fatalf("%s has other checkouts, and nothing asks where a word lands among them", project)
	}
	return l
}

// ckLands reports whether word, written from l's project, lands at rel in
// another checkout, or in the project's own git directory where own is set.
func ckLands(l *landings, word, rel string, own bool) bool {
	return slices.Contains(l.land(word), shellpolicy.Landing{Rel: rel, Own: own})
}

// ckSecond adds a second linked worktree beside wtCheckouts' own.
func ckSecond(t *testing.T, main string) string {
	t.Helper()
	wt2 := filepath.Join(wtTempDir(t), "wt2")
	wtGit(t, main, "worktree", "add", "--quiet", "-b", "wt2", wt2)
	return wt2
}

func ckRecord(main string) string {
	return filepath.Join(main, ".sdlc", "stories", "A-1", "gate-record.json")
}

// AC-10: a repository with one checkout is held exactly as it always was.
func TestAC10ASingleCheckoutAsksNothingAboutOtherCheckouts(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	dir := wtTempDir(t)
	wtGit(t, dir, "init", "--quiet", "--initial-branch=main")
	if l := newLandings(placeOf(dir), pathrules.NewResolver(dir)); l != nil {
		t.Error("a single checkout is asked where words land among other checkouts")
	}

	main, _ := wtCheckouts(t)
	if l := newLandings(placeOf(main), pathrules.NewResolver(main)); l == nil {
		t.Error("the main checkout of a repository with a linked worktree asks nothing about it")
	}
}

// T2, AC-7 and R2-4: from any checkout, the others' backlog, records, claims
// and loop state are found where they are.
func TestAC7AWordLandsInTheCheckoutOfThisRepositoryItNames(t *testing.T) {
	main, wt := wtCheckouts(t)
	wt2 := ckSecond(t, main)

	for _, tc := range []struct {
		name, from, word, rel string
		own                   bool
	}{
		{"the main checkout's backlog", wt, filepath.Join(main, "user_stories.json"), "user_stories.json", false},
		{"a record in the main checkout", wt, ckRecord(main), ".sdlc/stories/A-1/gate-record.json", false},
		{"a claim in the common git directory", wt, filepath.Join(main, ".git", "sdlc", "claims", "A-1.json"), ".git/sdlc/claims/A-1.json", false},
		{"the main checkout itself", wt, main, ".", false},
		{"another worktree's loop state", wt, filepath.Join(wt2, ".sdlc", "state", "active"), ".sdlc/state/active", false},
		{"a worktree's loop state from the main checkout", main, filepath.Join(wt, ".sdlc", "state", "tests.lock"), ".sdlc/state/tests.lock", false},
		{"this worktree's own git directory", wt, filepath.Join(main, ".git", "worktrees", "wt", "HEAD"), ".git/HEAD", true},
	} {
		t.Run("AC-7 "+tc.name, func(t *testing.T) {
			if l := ckLandings(t, tc.from); !ckLands(l, tc.word, tc.rel, tc.own) {
				t.Errorf("from %s, %s lands at %+v, want %q (own %v)", tc.from, tc.word, l.land(tc.word), tc.rel, tc.own)
			}
		})
	}

	l := ckLandings(t, wt)
	for _, word := range []string{
		filepath.Join(wt, ".sdlc", "state", "active"), // the project's own, which Resolve judges
		"notes.txt",
		filepath.Join(wtTempDir(t), "elsewhere", "x"),
	} {
		if got := l.land(word); len(got) != 0 {
			t.Errorf("%s lands in another checkout, at %+v", word, got)
		}
	}
}

// Only git writes a worktree's own entry, and a .git file anybody can write
// says where it leads. A copy of a real worktree's pointer, anywhere else, is
// not that worktree; worktree.useRelativePaths writes the way back relative.
func TestT2APointerCopiedOutOfAWorktreeMakesNoCheckout(t *testing.T) {
	main, wt := wtCheckouts(t)
	wt2 := ckSecond(t, main)
	l := ckLandings(t, wt)

	pointer, err := os.ReadFile(filepath.Join(wt2, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	copied := wtTempDir(t)
	write(t, copied, ".git", string(pointer))
	missing := wtTempDir(t)
	write(t, missing, ".git", "gitdir: "+filepath.Join(main, ".git", "worktrees", "never-made")+"\n")
	for _, dir := range []string{copied, missing} {
		if got := l.land(filepath.Join(dir, ".sdlc", "state", "active")); len(got) != 0 {
			t.Errorf("a directory with a pointer it was not given by git is a checkout: %+v", got)
		}
	}

	entry := filepath.Join(main, ".git", "worktrees", "wt2")
	back, err := filepath.Rel(entry, filepath.Join(wt2, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, entry, "gitdir", filepath.ToSlash(back)+"\n")
	l = ckLandings(t, wt)
	if word := filepath.Join(wt2, ".sdlc", "state", "active"); !ckLands(l, word, ".sdlc/state/active", false) {
		t.Errorf("with the way back written relative, %s lands at %+v", word, l.land(word))
	}
}

// A .git that does not belong to this repository is walked past, never
// stopped at: `git init` over the main checkout's .sdlc, or a pointer file put
// there, would otherwise hide the records beneath it.
func TestT2ADecoyGitDirectoryInTheMainCheckoutIsWalkedPast(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, main string)
	}{
		{"T2 a repository made over .sdlc", func(t *testing.T, main string) {
			t.Helper()
			wtGit(t, filepath.Join(main, ".sdlc"), "init", "--quiet")
		}},
		{"T2 a pointer file in .sdlc leading to the main checkout's own git directory", func(t *testing.T, main string) {
			t.Helper()
			write(t, main, ".sdlc/.git", "gitdir: "+filepath.Join(main, ".git")+"\n")
		}},
		{"T2 a .git file in .sdlc that names nothing", func(t *testing.T, main string) {
			t.Helper()
			write(t, main, ".sdlc/.git", "not a pointer\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt := wtCheckouts(t)
			tc.plant(t, main)

			l := ckLandings(t, wt)
			if !ckLands(l, ckRecord(main), ".sdlc/stories/A-1/gate-record.json", false) {
				t.Errorf("behind the decoy, the record lands at %+v", l.land(ckRecord(main)))
			}
			if got := wtAsk(t, wtHeld, wt, "Bash", map[string]any{"command": "echo '{}' > " + ckRecord(main)}, noEnv); !denied(got) {
				t.Error("the session held in the worktree wrote the main checkout's record past the decoy")
			}
		})
	}
}

// T12 and B1: core.bare, or a rewritten commondir, makes the main checkout
// unfindable to the CLI and leaves everything in it protected.
func TestT12WhatTheMainCheckoutKeepsIsFoundWhenItCannotBeEstablished(t *testing.T) {
	for _, tc := range []struct {
		name   string
		damage func(t *testing.T, main string)
	}{
		{"T12 core.bare", func(t *testing.T, main string) {
			t.Helper()
			wtGit(t, main, "config", "core.bare", "true")
		}},
		{"T12 commondir rewritten", func(t *testing.T, main string) {
			t.Helper()
			write(t, main, ".git/worktrees/wt/commondir", filepath.Join(wtTempDir(t), "nowhere")+"\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt := wtCheckouts(t)
			tc.damage(t, main)
			l := ckLandings(t, wt)
			for word, rel := range map[string]string{
				ckRecord(main):                                            ".sdlc/stories/A-1/gate-record.json",
				filepath.Join(main, "user_stories.json"):                  "user_stories.json",
				filepath.Join(main, ".git", "sdlc", "claims", "A-1.json"): ".git/sdlc/claims/A-1.json",
			} {
				if !ckLands(l, word, rel, false) {
					t.Errorf("%s lands at %+v, want %s", word, l.land(word), rel)
				}
			}
		})
	}
}

// Where not one candidate can be read, every word outside the checkout might
// be the main checkout's, and is refused by name, with its own reason.
func TestT12AWorktreeWhosePointerLeadsNowhereTreatsEveryOutsideWordAsUnplaced(t *testing.T) {
	main, wt := wtCheckouts(t)
	write(t, wt, ".git", "gitdir: "+filepath.Join(wtTempDir(t), "nowhere")+"\n")

	l := ckLandings(t, wt)
	backlog := filepath.Join(main, "user_stories.json")
	if got := l.land(backlog); !slices.Contains(got, shellpolicy.Landing{Rel: filepath.ToSlash(backlog)}) {
		t.Errorf("%s lands at %+v, want itself, absolute", backlog, got)
	}
	if got := l.land("notes.txt"); len(got) != 0 {
		t.Errorf("a word in the checkout itself lands at %+v", got)
	}

	got := wtAsk(t, wtHeld, wt, "Bash", map[string]any{"command": "echo '{}' > " + backlog}, noEnv)
	if !denied(got) {
		t.Fatal("with the main checkout unplaceable, the held session wrote a backlog outside its checkout")
	}
	if reason := got.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "sdlc doctor") {
		t.Errorf("the refusal does not send the reader to sdlc doctor: %s", reason)
	}
}

// R2-5: `..` after a link is taken where the link leads, as the filesystem
// takes it, not beside the link.
func TestT2DotDotThroughALinkLandsWhereTheLinkLeads(t *testing.T) {
	main, wt := wtCheckouts(t)
	reviews := filepath.Join(main, ".sdlc", "stories", "A-1", "reviews")
	if err := os.MkdirAll(reviews, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(reviews, filepath.Join(wt, "r")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}

	l := ckLandings(t, wt)
	if word := "r/../gate-record.json"; !ckLands(l, word, ".sdlc/stories/A-1/gate-record.json", false) {
		t.Errorf("%s lands at %+v, want the record in the main checkout", word, l.land(word))
	}
	if got := wtAsk(t, wtHeld, wt, "Bash", map[string]any{"command": "echo '{}' > r/../gate-record.json"}, noEnv); !denied(got) {
		t.Error("the held session wrote the main checkout's record through a link and ..")
	}
}

// 13(f): a commit made in another checkout records work its gates never saw,
// whatever the gates say. Reading another checkout is not refused.
func TestT2ACommitInAnotherCheckoutIsRefusedAndReadingItIsNot(t *testing.T) {
	main, wt := wtCheckouts(t)
	wt2 := ckSecond(t, main)

	for _, command := range []string{
		"git -C " + main + " commit -m elsewhere",
		"cd " + main + " && git commit -m elsewhere",
		"git --work-tree=" + main + " commit -m elsewhere",
		"git --work-tree " + main + " commit -m elsewhere",
		"git --git-dir=" + filepath.Join(main, ".git") + " commit -m elsewhere",
		"git -C " + wt2 + " commit -m elsewhere",
	} {
		got := wtAsk(t, wtHeld, wt, "Bash", map[string]any{"command": command}, noEnv)
		if !denied(got) || !strings.Contains(got.HookSpecificOutput.PermissionDecisionReason, "[commit-in-another-checkout]") {
			t.Errorf("%s: %s %s, want refused as a commit in another checkout", command,
				got.HookSpecificOutput.PermissionDecision, got.HookSpecificOutput.PermissionDecisionReason)
		}
	}
	for _, command := range []string{
		"cat " + filepath.Join(main, "user_stories.json"),
		"git -C " + main + " log --oneline",
		"ls " + filepath.Join(wt2, ".sdlc", "state"),
	} {
		if got := wtAsk(t, wtHeld, wt, "Bash", map[string]any{"command": command}, noEnv); denied(got) {
			t.Errorf("%s was refused: %s", command, got.HookSpecificOutput.PermissionDecisionReason)
		}
	}
}
