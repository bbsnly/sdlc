package store

// US-029.2, unit level: the store's half of the story worktree -- where it is
// made and on which branch (AC-1), which checkout is one (AC-5, AC-6), opening
// it with the configuration it reads (AC-1), the lock one start holds on the
// story while it makes the worktree (AC-8), and the .gitignore that keeps
// every story worktree out of the main checkout's git status (AC-2), refused
// where one already there does not (AC-4).
//
// Helpers carry an su prefix, for story worktree unit.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// suWorktreesDir is .sdlc/worktrees in the main checkout.
func suWorktreesDir(main string) string {
	return filepath.Join(main, ".sdlc", "worktrees")
}

// suIgnore is the .gitignore that keeps the story worktrees out of git status.
func suIgnore(main string) string {
	return filepath.Join(suWorktreesDir(main), ".gitignore")
}

// suMain is a main checkout with its backlog committed and nothing under
// .sdlc/worktrees yet.
func suMain(t *testing.T) string {
	t.Helper()
	main := clTempDir(t)
	initGit(t, main)
	clBacklog(t, main, nil)
	commitAll(t, main, "the project")
	return main
}

// suStatus is `git status --porcelain --untracked-files=all` in dir.
func suStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}

// suRefusedNotIgnored holds err to the refusal a .gitignore that does not hide
// the story worktrees gets: SDLC-E0053, naming the file, saying why, and with
// a fix that says what the file needs. AC-3: the file is named slash-separated
// -- filepath.ToSlash of the path, not the platform's own separator doubled
// by quoting it -- so the fragment checked here is joined with "/" rather
// than filepath.Join, which would use "\" on Windows.
func suRefusedNotIgnored(t *testing.T, err error, main, why string) {
	t.Helper()
	e := clError(t, err, sdlcerr.WorktreeNotMade)
	if !strings.Contains(e.What, ".sdlc/worktrees/.gitignore") {
		t.Errorf("what = %q, want it to name %s", e.What, filepath.ToSlash(suIgnore(main)))
	}
	if !strings.Contains(e.Why, why) {
		t.Errorf("why = %q, want it to say %q", e.Why, why)
	}
	if !strings.Contains(e.Fix, `"*"`) || !strings.Contains(e.Fix, "tells the person") {
		t.Errorf("fix = %q, want it to say the file needs a line \"*\", and that an assistant stops", e.Fix)
	}
}

// ---------------------------------------------------------------- AC-1

func TestAC1AStorysBranchIsStorySlashItsID(t *testing.T) {
	for id, want := range map[string]string{"US-001": "story/US-001", "A-1": "story/A-1", "x.y_z": "story/x.y_z"} {
		if got := StoryBranch(id); got != want {
			t.Errorf("StoryBranch(%q) = %q, want %q", id, got, want)
		}
	}
}

// The worktree is .sdlc/worktrees/<ID> of the main checkout whichever checkout
// asks: one path for every start, so that two of them cannot make two.
func TestAC1AStorysWorktreeIsUnderTheMainCheckoutWhicheverCheckoutAsks(t *testing.T) {
	main, wt := swNested(t)
	elsewhere := clWorktree(t, main, "elsewhere")
	want := filepath.Join(suWorktreesDir(main), "US-002")
	for _, from := range []string{main, wt, elsewhere} {
		got, err := clStore(from).StoryWorktree("US-002")
		if err != nil || got != want {
			t.Errorf("StoryWorktree from %s = %q, %v; want %s", from, got, err, want)
		}
	}
	if _, err := os.Lstat(want); err == nil {
		t.Errorf("asking where the worktree goes made %s", want)
	}
}

// An id that could leave .sdlc/worktrees is refused before it becomes a path,
// and a checkout whose main checkout cannot be found has no place for one.
func TestAC1AStoryWorktreeIsNotPlacedForAnUnsafeIDOrWithoutAMainCheckout(t *testing.T) {
	main := suMain(t)
	for _, id := range []string{"", "..", "../escape", "a/b", "-x"} {
		if got, err := clStore(main).StoryWorktree(id); err == nil {
			t.Errorf("StoryWorktree(%q) = %q, want the id refused", id, got)
		} else {
			clError(t, err, sdlcerr.UnsafeStoryID)
		}
	}

	broken := clTempDir(t)
	clWrite(t, filepath.Join(broken, ".git"), "this names no git directory\n")
	got, err := clStore(broken).StoryWorktree("US-001")
	if err == nil {
		t.Fatalf("StoryWorktree with no main checkout = %q, want a refusal", got)
	}
	clError(t, err, sdlcerr.NoMainCheckout)
	if _, ok := clStore(broken).StoryWorktreeOf(); ok {
		t.Error("a checkout with no main checkout reads as a story worktree")
	}
}

// ---------------------------------------------------------------- AC-5, AC-6

// Only the worktree start makes, at .sdlc/worktrees/<ID> of the main checkout
// and named for a story, is a story's own worktree. The main checkout, a
// worktree a person made elsewhere, and one under .sdlc/worktrees that is not
// named for a story or not directly in it are not.
func TestAC5OnlyTheWorktreeStartMakesIsAStorysOwnWorktree(t *testing.T) {
	main, wt := swNested(t)
	if id, ok := clStore(wt).StoryWorktreeOf(); !ok || id != "US-001" {
		t.Errorf("the story worktree reads as %q, %v; want US-001's", id, ok)
	}
	nested := filepath.Join(suWorktreesDir(main), "group", "US-003")
	runGit(t, main, "worktree", "add", "--quiet", "-b", "nested", nested)
	unsafe := filepath.Join(suWorktreesDir(main), "not a story")
	runGit(t, main, "worktree", "add", "--quiet", "-b", "unsafe", unsafe)
	for name, dir := range map[string]string{
		"the main checkout":                         main,
		"a worktree a person made elsewhere":        clWorktree(t, main, "US-002"),
		"a worktree one level too deep":             nested,
		"a worktree whose name is not a story's id": unsafe,
	} {
		if id, ok := clStore(dir).StoryWorktreeOf(); ok {
			t.Errorf("%s reads as %s's story worktree", name, id)
		}
	}

	s := clStore(main)
	for _, tc := range []struct {
		dir, id string
		want    bool
	}{
		{wt, "US-001", true},
		{filepath.Join(wt, "sub", ".."), "US-001", true},
		{wt, "US-002", false},
		{main, "US-001", false},
		{nested, "US-003", false},
		{wt, "../US-001", false},
	} {
		if got := s.IsStoryWorktree(tc.dir, tc.id); got != tc.want {
			t.Errorf("IsStoryWorktree(%s, %q) = %v, want %v", tc.dir, tc.id, got, tc.want)
		}
	}
}

// A start from the main checkout resumes a story where its worktree runs it,
// and that is read from the worktree's own active.
func TestAC5ActiveInIsTheStoryTheCheckoutAtDirIsRunning(t *testing.T) {
	main, wt := swNested(t)
	clState(t, wt, "US-001", "sess-wt")
	if got := ActiveIn(wt); got != "US-001" {
		t.Errorf("ActiveIn(the story worktree) = %q, want US-001", got)
	}
	if got := ActiveIn(main); got != "" {
		t.Errorf("ActiveIn(the main checkout) = %q, want nothing: the story is the worktree's", got)
	}
	clState(t, main, "../escape", "")
	if got := ActiveIn(main); got != "" {
		t.Errorf("ActiveIn read an id that is not a story's: %q", got)
	}
}

// ---------------------------------------------------------------- At

// Start writes a story's own state in its worktree and the record in the main
// checkout in one command, so the worktree is opened with the configuration
// it reads and the same clock.
func TestAC1AtOpensAnotherCheckoutWithItsOwnConfigurationAndTheSameClock(t *testing.T) {
	main, wt := swNested(t)
	clWrite(t, filepath.Join(wt, ".sdlc", "config.json"), `{"version": 1, "git": {"trunk_branch": "trunk-of-the-worktree"}}`)

	s := clStore(main)
	got, err := s.At(wt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root() != wt {
		t.Errorf("At(%s).Root() = %s", wt, got.Root())
	}
	if trunk := got.Config().Git.TrunkBranch; trunk != "trunk-of-the-worktree" {
		t.Errorf("the worktree was opened with trunk %q, want the one its own configuration names", trunk)
	}
	if !got.Now().Equal(fixedTime) || !got.Now().Equal(s.Now()) {
		t.Errorf("At's clock reads %s, want the store's own, %s", got.Now(), s.Now())
	}
	// It is a checkout of the same repository: the backlog is still the main
	// checkout's.
	if !config.SamePath(got.BacklogPath(), filepath.Join(main, "user_stories.json")) {
		t.Errorf("the worktree opened with At reads the backlog at %s", got.BacklogPath())
	}
	if !config.SamePath(got.CheckoutPath(), wt) {
		t.Errorf("the worktree opened with At is the checkout %s", got.CheckoutPath())
	}
}

func TestAtRefusesACheckoutWhoseConfigurationDoesNotRead(t *testing.T) {
	main, wt := swNested(t)
	if _, err := clStore(main).At(wt); err == nil {
		t.Error("At opened a checkout with no .sdlc/config.json")
	} else {
		clError(t, err, sdlcerr.NotInitialised)
	}
	clWrite(t, filepath.Join(wt, ".sdlc", "config.json"), `{"version": 1, "git": `)
	if _, err := clStore(main).At(wt); err == nil {
		t.Error("At opened a checkout whose .sdlc/config.json is not JSON")
	} else {
		clError(t, err, sdlcerr.ConfigUnreadable)
	}
}

// ---------------------------------------------------------------- AC-8

// One start holds a story from making its worktree to recording it, and a
// second start of the same story -- from any checkout -- waits on it, while
// other stories and every other command go on. The lock makes nothing in the
// repository.
func TestAC8LockStoryHoldsOneStoryFromEveryCheckoutAndNothingElse(t *testing.T) {
	cacheDir(t)
	guardWait = 50 * time.Millisecond
	t.Cleanup(func() { guardWait = 10 * time.Second })
	main, wt := clRepository(t, map[string]model.Status{})
	s := clStore(main)

	held, err := s.LockStory("US-002")
	if err != nil {
		t.Fatal(err)
	}
	for name, from := range map[string]*Store{"the main checkout": s, "a linked worktree": clStore(wt)} {
		g, err := from.LockStory("US-002")
		if err == nil {
			g.Release()
			t.Fatalf("from %s, a second start took US-002's lock while the first held it", name)
		}
		e := clError(t, err, sdlcerr.StateUnwritable)
		if !strings.Contains(e.Why, "start US-002") {
			t.Errorf("from %s, why = %q, want it to name the start holding the story", name, e.Why)
		}
	}

	other, err := clStore(wt).LockStory("US-003")
	if err != nil {
		t.Fatalf("another story's lock waited on US-002's: %v", err)
	}
	other.Release()
	project, err := Lock(s.LockRoot(), "gate")
	if err != nil {
		t.Fatalf("the project's lock waited on a story's: %v", err)
	}
	project.Release()

	held.Release()
	again, err := clStore(wt).LockStory("US-002")
	if err != nil {
		t.Fatalf("US-002's lock, let go of, could not be taken again: %v", err)
	}
	again.Release()
	if _, err := os.Lstat(suWorktreesDir(main)); err == nil {
		t.Error("taking a story's lock made .sdlc/worktrees in the main checkout")
	}
}

// A second start waiting on the story's lock takes it as soon as the first
// lets go, rather than failing.
func TestAC8ASecondStartWaitingOnTheStoryTakesItsLockWhenTheFirstLetsGo(t *testing.T) {
	cacheDir(t)
	main, wt := clRepository(t, map[string]model.Status{})
	first, err := clStore(main).LockStory("US-001")
	if err != nil {
		t.Fatal(err)
	}
	type taken struct {
		g   *Guard
		err error
	}
	second := make(chan taken, 1)
	go func() {
		g, err := clStore(wt).LockStory("US-001")
		second <- taken{g, err}
	}()
	first.Release()
	got := <-second
	if got.err != nil {
		t.Fatalf("the waiting start failed instead of taking the lock: %v", got.err)
	}
	got.g.Release()
}

func TestAC8LockStoryRefusesAnIDThatCannotNameAWorktree(t *testing.T) {
	cacheDir(t)
	main := suMain(t)
	for _, id := range []string{"", "../x", "a/b"} {
		g, err := clStore(main).LockStory(id)
		if err == nil {
			g.Release()
			t.Errorf("LockStory(%q) took a lock", id)
			continue
		}
		clError(t, err, sdlcerr.UnsafeStoryID)
	}
}

// ---------------------------------------------------------------- AC-2

// The file start writes hides every story worktree, and itself, from the main
// checkout's git status, and changes neither .gitignore nor info/exclude.
func TestAC2EnsureWorktreesIgnoredHidesEveryStoryWorktreeFromTheMainCheckout(t *testing.T) {
	main := suMain(t)
	clWrite(t, filepath.Join(suWorktreesDir(main), "US-001", "work.go"), "package work\n")
	if !strings.Contains(suStatus(t, main), ".sdlc/worktrees/") {
		t.Fatal("the test's own worktree directory is not in git status to begin with")
	}
	exclude := filepath.Join(main, ".git", "info", "exclude")
	excludeBefore, _ := os.ReadFile(exclude)

	if err := EnsureWorktreesIgnored(main); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(suIgnore(main)); err != nil || string(raw) != "*\n" {
		t.Errorf("%s holds %q, %v; want a line \"*\"", suIgnore(main), raw, err)
	}
	if status := suStatus(t, main); strings.Contains(status, ".sdlc/worktrees") {
		t.Errorf("git status in the main checkout still reports the worktrees:\n%s", status)
	}
	if _, err := os.Lstat(filepath.Join(main, ".gitignore")); err == nil {
		t.Error("the project's own .gitignore was written")
	}
	if after, _ := os.ReadFile(exclude); string(after) != string(excludeBefore) {
		t.Error(".git/info/exclude was changed")
	}

	// The second start finds the file there, and leaves it as it is.
	if err := EnsureWorktreesIgnored(main); err != nil {
		t.Errorf("a second start refused the file the first wrote: %v", err)
	}
	if raw, _ := os.ReadFile(suIgnore(main)); string(raw) != "*\n" {
		t.Errorf("the file was rewritten: %q", raw)
	}
}

// A .gitignore that is already there is read, never rewritten: every spelling
// that hides everything passes, and one that does not is refused and kept.
func TestAC4AGitignoreAlreadyThereIsKeptAndRefusedUnlessItHidesEverything(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		hides      bool
	}{
		{"AC-4 a star", "*\n", true},
		{"AC-4 a rooted star", "/*\n", true},
		{"AC-4 two stars", "**\n", true},
		{"AC-4 a star among comments and blank lines", "# story worktrees\n\n   *   \r\n", true},
		{"AC-4 empty", "", false},
		{"AC-4 only a comment", "# nothing here\n", false},
		{"AC-4 a pattern that hides some names", "*.go\nUS-*\n", false},
		{"AC-4 a star, then one name shown again", "*\n!keep\n", false},
		{"AC-4 a name shown before the star", "!US-001\n*\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := suMain(t)
			clWrite(t, suIgnore(main), tc.body)
			for name, check := range map[string]func(string) error{
				"EnsureWorktreesIgnored": EnsureWorktreesIgnored,
				"CheckWorktreesIgnored":  CheckWorktreesIgnored,
			} {
				err := check(main)
				switch {
				case tc.hides && err != nil:
					t.Errorf("%s refused %q: %v", name, tc.body, err)
				case !tc.hides && err == nil:
					t.Errorf("%s took %q as hiding every story worktree", name, tc.body)
				case !tc.hides:
					suRefusedNotIgnored(t, err, main, `no line "*"`)
				}
			}
			if raw, _ := os.ReadFile(suIgnore(main)); string(raw) != tc.body {
				t.Errorf("the file was rewritten from %q to %q", tc.body, raw)
			}
		})
	}
}

// Before the first worktree there is nothing to check; where .sdlc/worktrees
// is not a directory, making the worktree refuses it, and nothing is written.
func TestAC4CheckWorktreesIgnoredLeavesWhatIsNotThereYetToTheStart(t *testing.T) {
	main := suMain(t)
	if err := CheckWorktreesIgnored(main); err != nil {
		t.Errorf("with no .sdlc/worktrees yet: %v", err)
	}
	if err := os.MkdirAll(suWorktreesDir(main), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CheckWorktreesIgnored(main); err != nil {
		t.Errorf("with no .gitignore yet: %v", err)
	}
	if _, err := os.Lstat(suIgnore(main)); err == nil {
		t.Error("checking wrote the .gitignore")
	}

	file := suMain(t)
	clWrite(t, suWorktreesDir(file), "not a directory\n")
	if err := CheckWorktreesIgnored(file); err != nil {
		t.Errorf("with .sdlc/worktrees a file, which making the worktree refuses: %v", err)
	}
}

// The file is written only in a directory of the main checkout itself: not
// where there is none, not into a file, and not through a link to somewhere
// else.
func TestAC4EnsureWorktreesIgnoredWritesOnlyInARealDirectoryOfTheMainCheckout(t *testing.T) {
	t.Run("AC-4 no directory", func(t *testing.T) {
		main := suMain(t)
		suRefusedNotIgnored(t, EnsureWorktreesIgnored(main), main, "not a directory")
		if _, err := os.Lstat(suWorktreesDir(main)); err == nil {
			t.Error(".sdlc/worktrees was made")
		}
	})
	t.Run("AC-4 a file", func(t *testing.T) {
		main := suMain(t)
		clWrite(t, suWorktreesDir(main), "not a directory\n")
		suRefusedNotIgnored(t, EnsureWorktreesIgnored(main), main, "not a directory")
		if raw, _ := os.ReadFile(suWorktreesDir(main)); string(raw) != "not a directory\n" {
			t.Errorf("the file at .sdlc/worktrees was changed to %q", raw)
		}
	})
	t.Run("AC-4 a link", func(t *testing.T) {
		main := suMain(t)
		elsewhere := clTempDir(t)
		if err := os.MkdirAll(filepath.Join(main, ".sdlc"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere, suWorktreesDir(main)); err != nil {
			t.Skipf("no symbolic link here: %v", err)
		}
		suRefusedNotIgnored(t, EnsureWorktreesIgnored(main), main, "not a directory")
		if err := CheckWorktreesIgnored(main); err != nil {
			t.Errorf("CheckWorktreesIgnored read through the link: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(elsewhere, ".gitignore")); err == nil {
			t.Error("a .gitignore was written through the link")
		}
	})
	t.Run("AC-4 a directory where the file goes", func(t *testing.T) {
		main := suMain(t)
		if err := os.MkdirAll(suIgnore(main), 0o755); err != nil {
			t.Fatal(err)
		}
		suRefusedNotIgnored(t, EnsureWorktreesIgnored(main), main, "not a file")
		suRefusedNotIgnored(t, CheckWorktreesIgnored(main), main, "not a file")
	})
}

// What the file system refuses is said, as the file that could not be kept.
func TestAC4AGitignoreThatCannotBeReadOrWrittenIsRefused(t *testing.T) {
	skipIfPermissionsDoNotBind(t)
	t.Run("AC-4 the directory cannot be written", func(t *testing.T) {
		main := suMain(t)
		dir := suWorktreesDir(main)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		suRefusedNotIgnored(t, EnsureWorktreesIgnored(main), main, "could not be written")
	})
	t.Run("AC-4 the file cannot be read", func(t *testing.T) {
		main := suMain(t)
		clWrite(t, suIgnore(main), "*\n")
		if err := os.Chmod(suIgnore(main), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(suIgnore(main), 0o644) })
		suRefusedNotIgnored(t, EnsureWorktreesIgnored(main), main, "could not be read")
	})
	t.Run("AC-4 the directory cannot be searched", func(t *testing.T) {
		main := suMain(t)
		clWrite(t, suIgnore(main), "*\n")
		dir := suWorktreesDir(main)
		if err := os.Chmod(dir, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		err := CheckWorktreesIgnored(main)
		if err == nil {
			t.Fatal("a .gitignore in a directory that cannot be searched passed as hiding everything")
		}
		suRefusedNotIgnored(t, err, main, "could not be read")
	})
}

// The refusal names the file by a path a person can find, and is the one code
// every worktree that could not be made has. AC-3: the path is slash-separated,
// so what the refusal names is filepath.ToSlash(main), not main's own spelling
// (which on Windows uses "\" and would otherwise be doubled by quoting it).
func TestAC4ARefusedGitignoreIsSDLCE0053(t *testing.T) {
	main := suMain(t)
	clWrite(t, suIgnore(main), "!keep\n")
	err := CheckWorktreesIgnored(main)
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.WorktreeNotMade {
		t.Fatalf("err = %v, want SDLC-E0053", err)
	}
	if !strings.Contains(e.What, filepath.ToSlash(main)) {
		t.Errorf("what = %q, want it to name the file in %s", e.What, filepath.ToSlash(main))
	}
	if !strings.Contains(e.Why, "git add -A") {
		t.Errorf("why = %q, want it to say what an unhidden worktree does to git add -A", e.Why)
	}
}
