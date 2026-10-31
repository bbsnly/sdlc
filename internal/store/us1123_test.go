package store

// US-112.3: what the store does with a plan's revisions. Coverage counts a
// package's statements only from its own tests, so what the change adds here is
// measured here, not by internal/cli's tests of the commands.
//
// With no hook rule behind them, these files can be written by any agent, and
// the check against the name is the whole defence (T1, T2, T3, T4 and T7 of
// THREATS.md, and the person's decision there). Each case below therefore gives
// the store a file a model could have left, and most keep the bytes that would
// satisfy a check of the content alone.
//
// Where a revision lives is the plan's to choose, so it is found by walking the
// story's directory for a file named by the SHA-256 of its bytes. The one name
// the story's design adds to this package is PlanRevision. It is reached through
// rvReader, not called, so that this package's other tests keep compiling and
// running until it exists. Helpers carry an rv prefix: they live in package
// store beside whatever the story adds to it.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// rvNotKept is the code a read of a revision refuses with: AC-4's, pinned in
// internal/sdlcerr/sdlcerr_test.go.
const rvNotKept = "SDLC-E0084"

// rvLimit is the largest document `sdlc artifact write` stores, which a kept
// revision cannot lawfully pass.
const rvLimit = 1 << 20

const rvStory = "A-1"

// rvReader is the seam: the bytes of the revision a review was stamped with,
// checked against subject, or an error and no bytes.
type rvReader interface {
	PlanRevision(story, subject string) ([]byte, error)
}

func rvRead(t *testing.T, s *Store, subject string) ([]byte, error) {
	t.Helper()
	reader, ok := any(s).(rvReader)
	if !ok {
		t.Fatal("the store has no PlanRevision(story, subject string) ([]byte, error), which reads a kept revision back")
	}
	return reader.PlanRevision(rvStory, subject)
}

func rvHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func rvPlan(t *testing.T) model.Artifact {
	t.Helper()
	plan, ok := model.FindArtifact("plan")
	if !ok {
		t.Fatal("this version has no document called plan")
	}
	return plan
}

// rvWrite stores a plan the way `sdlc artifact write plan` does.
func rvWrite(t *testing.T, s *Store, content string) {
	t.Helper()
	if _, err := s.WriteArtifact(rvStory, rvPlan(t), []byte(content)); err != nil {
		t.Fatalf("storing the plan %q: %v", rvShort(content), err)
	}
}

func rvShort(content string) string {
	if len(content) > 40 {
		return content[:40] + "..."
	}
	return content
}

func rvStem(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// rvFind is the path of whatever under the story's directory is named by hash,
// and "" where nothing is. Links and other special files count: this is for
// finding a name, and what is at it is the test's business.
func rvFind(t *testing.T, s *Store, hash string) string {
	t.Helper()
	dir, err := s.StoryDir(rvStory)
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	walk := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && rvStem(d.Name()) == hash {
			found = path
		}
		return nil
	})
	if walk != nil && !errors.Is(walk, fs.ErrNotExist) {
		t.Fatal(walk)
	}
	return found
}

// rvKept is the path of the revision of content, which has to be there.
func rvKept(t *testing.T, s *Store, content string) string {
	t.Helper()
	path := rvFind(t, s, rvHash(content))
	if path == "" {
		t.Fatalf("nothing under the story's directory is named by the SHA-256 of %q: the revision was not kept", rvShort(content))
	}
	return path
}

// rvBeside is where a revision of the hash given would be, going by where one
// of another hash is: the same directory, and the same way of naming.
func rvBeside(path, hash string) string {
	name := filepath.Base(path)
	return filepath.Join(filepath.Dir(path), hash+filepath.Ext(name))
}

// rvPlace puts content at path, as a model's file write would.
func rvPlace(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rvRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func rvStored(t *testing.T, s *Store) string {
	t.Helper()
	dir, err := s.StoryDir(rvStory)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, rvPlan(t).File))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// rvOutside is a file outside the repository, for a link to lead to.
func rvOutside(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	path := filepath.Join(dir, "elsewhere")
	rvPlace(t, path, content)
	return path
}

// rvLink makes path a link to target, or skips where this system will not.
func rvLink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("this system does not make the link this case needs: %v", err)
	}
}

// rvFifo makes path a FIFO whose writer offers content to whoever opens it, so
// that a reader of any special file gets the very bytes the name promises and
// the case fails on what it printed, not by hanging the suite.
func rvFifo(t *testing.T, path, content string) {
	t.Helper()
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skipf("this system has no mkfifo, which this case needs: %v", err)
	}
	if out, err := exec.CommandContext(t.Context(), mkfifo, path).CombinedOutput(); err != nil {
		t.Skipf("mkfifo could not make the FIFO this case needs: %v\n%s", err, out)
	}
	go func() {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = f.WriteString(content)
	}()
	// Nobody reading is the right answer, and leaves the writer waiting in its
	// open. Opening both ends lets it go.
	t.Cleanup(func() {
		if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
			_ = f.Close()
		}
	})
}

func rvRefuses(t *testing.T, s *Store, subject string) {
	t.Helper()
	got, err := rvRead(t, s, subject)
	if err == nil {
		t.Fatalf("the revision %q was read back as %q", rvShort(subject), rvShort(string(got)))
	}
	if got != nil {
		t.Errorf("the refusal of %q still returned %q", rvShort(subject), rvShort(string(got)))
	}
	if code := codeOf(t, err).String(); code != rvNotKept {
		t.Errorf("the refusal of %q is %s, want %s: %v", rvShort(subject), code, rvNotKept, err)
	}
}

func TestAC1WriteArtifactKeepsAPlansBytesUnderTheStorysDirectoryAndReadsThemBack(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	rvWrite(t, s, first)
	rvWrite(t, s, second)

	for _, content := range []string{first, second} {
		kept, err := os.ReadFile(rvKept(t, s, content))
		if err != nil {
			t.Fatal(err)
		}
		if string(kept) != content {
			t.Errorf("the revision named for %q holds %q", content, kept)
		}
		got, err := rvRead(t, s, rvHash(content))
		if err != nil {
			t.Fatalf("the revision of %q cannot be read back: %v", content, err)
		}
		if string(got) != content {
			t.Errorf("the revision of %q reads back as %q", content, got)
		}
	}
	if got := rvStored(t, s); got != second {
		t.Errorf("PLAN.md is %q, want the plan stored last", got)
	}
}

// Only the plan has revisions: a review of any other document is not stamped
// with its bytes, and each kept file is on the disk for the life of the story.
func TestAC1OnlyThePlanKeepsRevisions(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	const plan = "# Plan\n"
	rvWrite(t, s, plan)
	rvKept(t, s, plan)

	for _, name := range []string{"analysis", "threats", "test_plan", "verification", "retro"} {
		a, ok := model.FindArtifact(name)
		if !ok {
			t.Fatalf("this version has no document called %s", name)
		}
		content := "# " + name + "\n"
		if _, err := s.WriteArtifact(rvStory, a, []byte(content)); err != nil {
			t.Fatal(err)
		}
		if path := rvFind(t, s, rvHash(content)); path != "" {
			t.Errorf("storing the %s kept a revision at %s", name, path)
		}
	}
}

// T2: PLAN.md with no revision is a review that can never be read back, and a
// PLAN.md replaced before the failure is that. A directory where the revision
// goes cannot be written over on any of the three systems, and is found by
// where the last revision went, so this does not fix the location.
func TestAC1APlanThatCannotBeKeptIsRefusedAndPLANmdStaysAsItWas(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	rvWrite(t, s, first)

	in := rvBeside(rvKept(t, s, first), rvHash(second))
	if err := os.MkdirAll(filepath.Join(in, "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := s.WriteArtifact(rvStory, rvPlan(t), []byte(second))
	if err == nil {
		t.Fatal("the plan was stored although its revision could not be kept")
	}
	if code := codeOf(t, err); code != sdlcerr.StateUnwritable {
		t.Errorf("code = %s, want %s: %v", code, sdlcerr.StateUnwritable, err)
	}
	if got := rvStored(t, s); got != first {
		t.Errorf("PLAN.md was replaced by a plan whose revision was not kept: %q", got)
	}
}

// T7: a name is not proof of what is at it. Whatever is there when the plan is
// stored again, either the plan is refused and PLAN.md stays, or the file at
// the name holds the plan. Nothing is written through a link.
func TestAC1WritingAPlanAgainDoesNotTrustWhatIsAlreadyAtItsName(t *testing.T) {
	t.Parallel()
	const content, later = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	for _, c := range []struct {
		name  string
		plant func(t *testing.T, path string) (outside, outsideHolds string)
	}{
		{"nothing changed", func(*testing.T, string) (string, string) { return "", "" }},
		{"other bytes", func(t *testing.T, path string) (string, string) {
			t.Helper()
			rvPlace(t, path, "# Plan\n\nSomething a model preferred.\n")
			return "", ""
		}},
		{"no bytes", func(t *testing.T, path string) (string, string) {
			t.Helper()
			rvPlace(t, path, "")
			return "", ""
		}},
		{"a link out of the repository to other bytes", func(t *testing.T, path string) (string, string) {
			t.Helper()
			const elsewhere = "not the plan\n"
			outside := rvOutside(t, elsewhere)
			rvRemove(t, path)
			rvLink(t, outside, path)
			return outside, elsewhere
		}},
		{"a link out of the repository to the same bytes", func(t *testing.T, path string) (string, string) {
			t.Helper()
			outside := rvOutside(t, content)
			rvRemove(t, path)
			rvLink(t, outside, path)
			return outside, content
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := newStore(t)
			rvWrite(t, s, content)
			rvWrite(t, s, later)
			path := rvKept(t, s, content)
			outside, outsideHolds := c.plant(t, path)

			_, err := s.WriteArtifact(rvStory, rvPlan(t), []byte(content))

			if outside != "" {
				if raw, readErr := os.ReadFile(outside); readErr != nil || string(raw) != outsideHolds {
					t.Errorf("the file the link led to now holds %q (%v), want %q", raw, readErr, outsideHolds)
				}
			}
			if err != nil {
				if got := rvStored(t, s); got != later {
					t.Errorf("the write was refused and PLAN.md changed all the same: %q (%v)", got, err)
				}
				return
			}
			info, statErr := os.Lstat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("the plan was stored and %s is %s, not a regular file", path, info.Mode())
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil || string(raw) != content {
				t.Errorf("the plan was stored and its revision holds %q (%v), want %q", raw, readErr, content)
			}
			if got, readErr := rvRead(t, s, rvHash(content)); readErr != nil || string(got) != content {
				t.Errorf("the revision reads back as %q (%v)", got, readErr)
			}
		})
	}
}

// "Compare, and rewrite if different" can go wrong here: a comparison whose read
// fails has to end in a rewrite or a refusal, never in carrying on.
func TestAC1AnUnreadableFileAtTheNameIsNotTakenForTheRevision(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	const content, later = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	rvWrite(t, s, content)
	rvWrite(t, s, later)
	path := rvKept(t, s, content)
	rvPlace(t, path, "other bytes\n")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	if _, err := s.WriteArtifact(rvStory, rvPlan(t), []byte(content)); err != nil {
		if got := rvStored(t, s); got != later {
			t.Errorf("the write was refused and PLAN.md changed all the same: %q (%v)", got, err)
		}
		return
	}
	got, err := rvRead(t, s, rvHash(content))
	if err != nil || string(got) != content {
		t.Errorf("the plan was stored and its revision reads back as %q (%v)", got, err)
	}
}

// Rewriting identical bytes is what a re-run gate does, and has to leave one
// good revision and every other one.
func TestAC1WritingIdenticalBytesLeavesOneGoodRevision(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	const content, other = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	rvWrite(t, s, content)
	rvWrite(t, s, other)
	rvWrite(t, s, content)
	rvWrite(t, s, content)

	dir, err := s.StoryDir(rvStory)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	err = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			counts[rvStem(d.Name())]++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if counts[rvHash(content)] != 1 || counts[rvHash(other)] != 1 {
		t.Errorf("files by the name of each revision: %d and %d, want one of each", counts[rvHash(content)], counts[rvHash(other)])
	}
	for _, c := range []string{content, other} {
		if got, err := rvRead(t, s, rvHash(c)); err != nil || string(got) != c {
			t.Errorf("the revision of %q reads back as %q (%v)", c, got, err)
		}
	}
}

// The revision directory is written through the same guard as every other
// file of the loop: a clone can bring a link where a directory should be.
func TestAC1ARevisionDirectoryThatLeadsOutOfTheRepositoryIsNotWrittenThrough(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	rvWrite(t, s, first)
	path := rvKept(t, s, first)
	storyDir, err := s.StoryDir(rvStory)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if dir == storyDir {
		t.Skip("revisions sit directly in the story's directory, so no directory of their own can be a link")
	}
	outside := filepath.Dir(rvOutside(t, "something else\n"))
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	rvLink(t, outside, dir)

	_, err = s.WriteArtifact(rvStory, rvPlan(t), []byte(second))
	if err == nil {
		t.Error("the plan was stored through a link out of the repository")
	}
	if got := rvStored(t, s); got != first {
		t.Errorf("PLAN.md was replaced by a plan whose revision was not kept: %q", got)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("something was written outside the repository: %v", entries)
	}
}

// T1 and T7: the name is the check. Every one of these is a file a model could
// leave, and the current plan's bytes are the most useful to it: a diff of
// nothing, for a plan that changed.
func TestAC4AKeptRevisionWhoseBytesDoNotMatchItsNameIsRefused(t *testing.T) {
	t.Parallel()
	const content, later = "# Plan\n\nOne.\nTwo.\n", "# Plan\n\nOne.\nTwo.\nThree.\n"
	for name, bytes := range map[string]string{
		"one byte changed":     "# Plan\n\nOne.\nTwo!\n",
		"a byte short":         "# Plan\n\nOne.\nTwo.",
		"a byte more":          content + "\n",
		"empty":                "",
		"the current plan":     later,
		"line endings changed": "# Plan\r\n\r\nOne.\r\nTwo.\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newStore(t)
			rvWrite(t, s, content)
			rvWrite(t, s, later)
			rvPlace(t, rvKept(t, s, content), bytes)
			rvRefuses(t, s, rvHash(content))
		})
	}
}

// T2: a missing file refuses, and so does every other subject with none.
func TestAC4ARevisionNotThereIsRefused(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	const content = "# Plan\n\nOne.\n"
	rvWrite(t, s, content)
	rvRemove(t, rvKept(t, s, content))
	rvRefuses(t, s, rvHash(content))
}

func TestAC4NoRevisionHasEverBeenKeptIsARefusal(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	rvRefuses(t, s, rvHash("a plan nobody stored\n"))
}

// T3: a subject is read from a record a clone can bring, and only 64 lower-case
// hex characters become a file name. The rows that name a real revision are the
// ones a check of the content alone would pass.
func TestAC4ASubjectThatIsNot64LowerCaseHexCharactersNeverBecomesAFileName(t *testing.T) {
	t.Parallel()
	const content = "# Plan\n\nOne.\n"
	hash := rvHash(content)
	for name, subject := range map[string]func(path string) string{
		"empty":                     func(string) string { return "" },
		"parent":                    func(string) string { return "../../../../../../etc/passwd" },
		"the story's own PLAN.md":   func(string) string { return "../PLAN.md" },
		"absolute":                  func(path string) string { return path },
		"backslashes":               func(string) string { return `..\..\..\windows\win.ini` },
		"upper case":                func(string) string { return strings.ToUpper(hash) },
		"63 characters":             func(string) string { return hash[:63] },
		"65 characters":             func(string) string { return hash + "0" },
		"64 characters, not hex":    func(string) string { return strings.Repeat("g", 64) },
		"the hash and an extension": func(string) string { return hash + ".md" },
		"dot then the real one":     func(string) string { return "./" + hash },
		"out and back to the real":  func(path string) string { return "../" + filepath.Base(filepath.Dir(path)) + "/" + hash },
		"a newline after it":        func(string) string { return hash + "\n" },
		"a NUL inside":              func(string) string { return hash[:32] + "\x00" + hash[33:] },
		"very long":                 func(string) string { return strings.Repeat("a", 5000) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newStore(t)
			rvWrite(t, s, content)
			rvRefuses(t, s, subject(rvKept(t, s, content)))
		})
	}
}

// T4: the name can hold what the content check alone would let through. Each of
// these offers the right bytes, and only a read of a regular file refuses them.
func TestAC4AReadIsOfARegularFileOnly(t *testing.T) {
	t.Parallel()
	const content = "# Plan\n\nOne.\n"
	t.Run("a link out of the repository to the same bytes", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		rvWrite(t, s, content)
		path := rvKept(t, s, content)
		rvRemove(t, path)
		rvLink(t, rvOutside(t, content), path)
		rvRefuses(t, s, rvHash(content))
	})
	t.Run("a link inside the repository to the same bytes", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		rvWrite(t, s, content)
		path := rvKept(t, s, content)
		dir, err := s.StoryDir(rvStory)
		if err != nil {
			t.Fatal(err)
		}
		beside := filepath.Join(dir, "beside")
		rvPlace(t, beside, content)
		rvRemove(t, path)
		rvLink(t, beside, path)
		rvRefuses(t, s, rvHash(content))
	})
	t.Run("a FIFO offering the same bytes", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		rvWrite(t, s, content)
		path := rvKept(t, s, content)
		rvRemove(t, path)
		rvFifo(t, path, content)
		rvRefuses(t, s, rvHash(content))
	})
	t.Run("a directory", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		rvWrite(t, s, content)
		path := rvKept(t, s, content)
		rvRemove(t, path)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		rvRefuses(t, s, rvHash(content))
	})
}

// A file the store cannot read is one it cannot vouch for. The safe answer is
// the same as for any other: this reviewer reads the whole plan.
func TestAC4ARevisionThatCannotBeOpenedIsRefused(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	const content = "# Plan\n\nOne.\n"
	rvWrite(t, s, content)
	path := rvKept(t, s, content)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	rvRefuses(t, s, rvHash(content))
}

// T4: the read is bounded at what `sdlc artifact write` can store. The file past
// the limit is named by its own hash, so a read that hashed all of it would
// pass the check and hand the whole of it on.
func TestAC4AReadIsBoundedAtTheLimitOfADocument(t *testing.T) {
	t.Parallel()
	const seed = "# Plan\n\nOne.\n"
	atLimit := strings.Repeat("x", rvLimit-1) + "\n"
	pastIt := strings.Repeat("x", rvLimit) + "\n"

	t.Run("at the limit it is read", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		rvWrite(t, s, atLimit)
		got, err := rvRead(t, s, rvHash(atLimit))
		if err != nil {
			t.Fatalf("a revision of exactly the limit was refused: %v", err)
		}
		if string(got) != atLimit {
			t.Errorf("a revision of exactly the limit came back %d bytes long, want %d", len(got), len(atLimit))
		}
	})
	t.Run("past the limit it is refused", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		rvWrite(t, s, seed)
		rvPlace(t, rvBeside(rvKept(t, s, seed), rvHash(pastIt)), pastIt)
		rvRefuses(t, s, rvHash(pastIt))
	})
}

// T4 again, for what a check of the length after the read would not hold: a
// file far past the limit is refused without being read into memory first. The
// file holds nothing on the disk, and is named by its own hash so that a read of
// all of it would pass the check. Allocation is what is measured, not time, and
// this runs alone, not in parallel, so that nothing else allocates beside it.
func TestAC4ARevisionFarPastTheLimitIsNotReadIntoMemory(t *testing.T) {
	const size = 64 * rvLimit
	s := newStore(t)
	const seed = "# Plan\n\nOne.\n"
	rvWrite(t, s, seed)

	zeroes := sha256.New()
	if _, err := zeroes.Write(make([]byte, size)); err != nil {
		t.Fatal(err)
	}
	hash := hex.EncodeToString(zeroes.Sum(nil))
	f, err := os.Create(rvBeside(rvKept(t, s, seed), hash))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, readErr := rvRead(t, s, hash)
	runtime.ReadMemStats(&after)

	if readErr == nil {
		t.Error("a revision far past the limit of a document was read back")
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > size/4 {
		t.Errorf("refusing a file of %d bytes allocated %d: it was read before it was refused", size, allocated)
	}
}

// A story id reaches the path as well, and goes through the check every other
// path of the store does.
func TestAC4ARevisionOfAStoryWithAnUnsafeIDIsRefused(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	reader, ok := any(s).(rvReader)
	if !ok {
		t.Fatal("the store has no PlanRevision(story, subject string) ([]byte, error), which reads a kept revision back")
	}
	got, err := reader.PlanRevision("../../etc", rvHash("x\n"))
	if err == nil {
		t.Fatalf("a revision was read for an id that escapes the stories directory: %q", got)
	}
	if code := codeOf(t, err); code != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %s, want %s", code, sdlcerr.UnsafeStoryID)
	}
}
