package gitx

// US-092 AC-2: what the stop compares between a story's commits before and
// after its own replay -- each commit's author name, author email and whole
// message -- is read by Written, which PLAN.md gives as
//
//	Written(ctx, root, base, tip string) (string, error)
//
// It returns what git wrote for the commits of base..tip, oldest first, fields
// separated by NUL, four to a commit (hash, author name, author email, the
// whole message); the parsing is in package cli. These hold the part only a
// repository shows: the message and the author as the commit has them, in
// order; a replace ref that would show another commit's words; an empty
// message; and a value that is not a full hash being refused before git runs.
//
// They do not compile until Written exists, which is the right reason for
// them to fail. Helpers carry a wr prefix, for written.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wrCommit commits everything in dir with the message verbatim, as author where
// one is given, and returns the commit.
func wrCommit(t *testing.T, dir, author, message string) string {
	t.Helper()
	run(t, dir, "add", "-A")
	args := []string{"-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet",
		"--allow-empty", "--allow-empty-message", "--cleanup=verbatim", "-m", message}
	if author != "" {
		args = append(args, "--author", author)
	}
	run(t, dir, args...)
	return lgRev(t, dir, "HEAD")
}

// AC-2: each commit's author and whole message, oldest first, trailers and
// blank lines as the commit has them.
func TestAC2WrittenSaysEachCommitsAuthorAndWholeMessageOldestFirst(t *testing.T) {
	t.Parallel()
	root, base := lgRepo(t)
	const first = "feat: one\n\nbody line\n\n\nafter two blank lines\n\nCo-Authored-By: Dave <dave@example.org>\nSigned-off-by: Carol <carol@example.org>\n"
	write(t, root, "one.txt", "1\n")
	one := wrCommit(t, root, "Carol <carol@example.org>", first)
	write(t, root, "two.txt", "2\n")
	two := wrCommit(t, root, "", "feat: two\n")

	out, err := Written(t.Context(), root, base, two)
	if err != nil {
		t.Fatalf("Written: %v", err)
	}
	for _, want := range []string{one, two, "Carol", "carol@example.org", first, "feat: two\n", "t@example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("Written does not say %q:\n%q", want, out)
		}
	}
	if strings.Index(out, one) > strings.Index(out, two) || strings.Index(out, "feat: one") > strings.Index(out, "feat: two") {
		t.Errorf("Written is not oldest first:\n%q", out)
	}
	if !strings.Contains(out, "\x00") {
		t.Errorf("Written separates its fields with NUL, which a message cannot hold, and there is none in:\n%q", out)
	}
}

// AC-2 (THREATS T5): a replace ref cannot show the comparison another
// commit's message or author. The real commit's words are what Written says.
func TestAC2WrittenReadsTheCommitItselfNotAReplaceRefsWords(t *testing.T) {
	t.Parallel()
	root, base := lgRepo(t)
	write(t, root, "one.txt", "1\n")
	actual := wrCommit(t, root, "Carol <carol@example.org>", "the real message\n")
	fake := strings.TrimSpace(wtGitOut(t, root, "-c", "user.email=m@example.org", "-c", "user.name=Mallory",
		"commit-tree", "-p", base, "-m", "REPLACED message", actual+"^{tree}"))
	run(t, root, "replace", actual, fake)
	if shown := wtGitOut(t, root, "log", "-1", "--format=%an %s", actual); !strings.Contains(shown, "Mallory") {
		t.Fatalf("precondition: plain git log shows %q for the replaced commit, want the replacement's words", shown)
	}

	out, err := Written(t.Context(), root, base, actual)
	if err != nil {
		t.Fatalf("Written: %v", err)
	}
	for _, want := range []string{"the real message", "Carol", "carol@example.org"} {
		if !strings.Contains(out, want) {
			t.Errorf("Written does not say %q of the real commit:\n%q", want, out)
		}
	}
	for _, not := range []string{"REPLACED", "Mallory", "m@example.org"} {
		if strings.Contains(out, not) {
			t.Errorf("Written says %q, the words of a replace ref:\n%q", not, out)
		}
	}
}

// AC-2: the author is the commit's own, never the one a .mailmap in the
// repository says the person is: a mailmap that renamed Carol would show the
// comparison a name nobody committed as.
func TestAC2WrittenSaysTheAuthorAsTheCommitHasItNotAsTheMailmapDoes(t *testing.T) {
	t.Parallel()
	root, base := lgRepo(t)
	write(t, root, ".mailmap", "Mapped Name <mapped@example.org> <carol@example.org>\n")
	write(t, root, "one.txt", "1\n")
	one := wrCommit(t, root, "Carol <carol@example.org>", "feat: one\n")
	if shown := wtGitOut(t, root, "log", "-1", "--format=%aN <%aE>", one); !strings.Contains(shown, "Mapped Name") {
		t.Fatalf("precondition: the mailmap does not rename the author: %q", shown)
	}

	out, err := Written(t.Context(), root, base, one)
	if err != nil {
		t.Fatalf("Written: %v", err)
	}
	if !strings.Contains(out, "Carol") || !strings.Contains(out, "carol@example.org") ||
		strings.Contains(out, "Mapped") || strings.Contains(out, "mapped@example.org") {
		t.Errorf("Written does not say the author as the commit has it:\n%q", out)
	}
}

// AC-2: a commit with no message is a commit, not an error and not a gap.
func TestAC2WrittenAnEmptyMessageIsOneCommitNotAnError(t *testing.T) {
	t.Parallel()
	root, base := lgRepo(t)
	write(t, root, "one.txt", "1\n")
	empty := wrCommit(t, root, "", "")

	out, err := Written(t.Context(), root, base, empty)
	if err != nil {
		t.Fatalf("Written on a commit with an empty message: %v", err)
	}
	if !strings.Contains(out, empty) || !strings.Contains(out, "Test") {
		t.Errorf("Written does not say the empty-message commit and its author:\n%q", out)
	}
}

// AC-2: only a full hash goes to git. Anything else -- an option, a name, an
// abbreviation -- is refused before a process runs, and nothing is written.
func TestAC2WrittenRefusesAnythingButFullHashes(t *testing.T) {
	t.Parallel()
	root, base := lgRepo(t)
	target := filepath.Join(root, "x")
	for _, bad := range []string{"--output=" + target, "HEAD", base[:12], ""} {
		if _, err := Written(t.Context(), root, bad, base); err == nil {
			t.Errorf("Written took %q as the base", bad)
		}
		if _, err := Written(t.Context(), root, base, bad); err == nil {
			t.Errorf("Written took %q as the tip", bad)
		}
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("Written ran an option handed to it as a hash: it wrote a file")
	}
}
