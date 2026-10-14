package cli

// US-092 AC-2, the parts of the stop's comparison that no in-process landing
// test reaches: PLAN.md names `parseWritten(out string) []written`, which reads
// what gitx.Written returned, and `sameWords(was, now []written) bool`, which
// says whether a replay kept its commits one for one -- the same count and,
// for each pair by position, the same author name, author email and whole
// message; the hash, the committer and the dates are not words.
//
// The input is what gitx.Written says about real commits in a real repository,
// so the framing is git's and not a guess at it: messages with blank lines
// and trailers kept verbatim, an empty message, another author. The type
// `written` is used only as parseWritten returns it. The tests do not compile
// until the functions exist, which is the right reason for them to fail.
//
// Helpers carry a cu prefix, for commit cover unit.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/gitx"
)

// cuMessages are the messages of the story's commits: blank runs and trailers
// kept as written, one line, and another author's.
const (
	cuFirst  = "feat: one\n\nbody line\n\n\nafter two blank lines\n\nCo-Authored-By: Dave <dave@example.org>\nSigned-off-by: Carol <carol@example.org>\n"
	cuSecond = "feat: two"
	cuThird  = "fix: three\n\n  indented body  \n"
	cuFourth = "chore: four\n\nReviewed-by: Erin <erin@example.org>\n"
)

// cuMessage is the file holding message byte for byte, outside the repository.
// `git commit -m` completes the last line itself, even with --cleanup=verbatim
// (RAN: a commit made with -m "feat: two" ends in a newline), so a message
// that must lack its final newline is handed to git with -F.
func cuMessage(t *testing.T, message string) string {
	t.Helper()
	path := filepath.Join(wtTempDir(t), "message.txt")
	if err := os.WriteFile(path, []byte(message), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// cuAmendWith is the arguments that make the tip's message exactly message.
func cuAmendWith(t *testing.T, message string) []string {
	t.Helper()
	return []string{"commit", "--quiet", "--amend", "--allow-empty-message", "--cleanup=verbatim", "-F", cuMessage(t, message)}
}

// cuCommit commits file with body, as author where one is given, the message
// byte for byte, and returns the commit.
func cuCommit(t *testing.T, dir, file, author, message string) string {
	t.Helper()
	writeFile(t, dir, file, file+"\n")
	ccGit(t, dir, "add", "--", file)
	args := []string{"commit", "--quiet", "--allow-empty-message", "--cleanup=verbatim", "-F", cuMessage(t, message)}
	if author != "" {
		args = append(args, "--author", author)
	}
	ccGit(t, dir, args...)
	return swRev(t, dir, "HEAD")
}

// cuWritten is gitx.Written for base..tip, parsed.
func cuWritten(t *testing.T, dir, base, tip string) []written {
	t.Helper()
	out, err := gitx.Written(t.Context(), dir, base, tip)
	if err != nil {
		t.Fatalf("gitx.Written(%s..%s): %v", base[:12], tip[:12], err)
	}
	return parseWritten(out)
}

// cuStory is a base commit on main, a story branch of four commits on it, and
// trunk's own commit on a branch, moved, that the story is to sit on.
type cuStory struct {
	dir, base, moved, oldTip string
}

func cuMake(t *testing.T) cuStory {
	t.Helper()
	dir := wtTempDir(t)
	swGit(t, dir, "init", "--quiet", "-b", "main")
	s := cuStory{dir: dir, base: cuCommit(t, dir, "base.txt", "", "base")}
	ccGit(t, dir, "switch", "--quiet", "-c", "story")
	cuCommit(t, dir, "one.txt", "Carol <carol@example.org>", cuFirst)
	second := cuCommit(t, dir, "two.txt", "", cuSecond)
	if raw := swGitRaw(t, dir, "cat-file", "commit", second); !strings.HasSuffix(raw, "\n\n"+cuSecond) {
		t.Fatalf("precondition: the second commit ends %q, want it without a final newline", raw[max(0, len(raw)-12):])
	}
	cuCommit(t, dir, "three.txt", "", cuThird)
	s.oldTip = cuCommit(t, dir, "four.txt", "", cuFourth)
	ccGit(t, dir, "switch", "--quiet", "main")
	ccGit(t, dir, "switch", "--quiet", "-c", "moved")
	s.moved = cuCommit(t, dir, "trunk.txt", "", "another story lands")
	ccGit(t, dir, "switch", "--quiet", "story")
	return s
}

// replay makes the story's commits again onto moved, as the stop's replay
// does, and returns the new tip.
func (s cuStory) replay(t *testing.T) string {
	t.Helper()
	ccGit(t, s.dir, "rebase", "--quiet", "--merge", "--no-fork-point", "--onto", s.moved, s.base)
	return swRev(t, s.dir, "HEAD")
}

// AC-2: one entry per commit whatever its message holds -- blank lines,
// trailers, none at all -- and none for a range with no commit.
func TestAC2ParseWrittenGivesOneEntryPerCommitWhateverTheMessageHolds(t *testing.T) {
	t.Parallel()
	s := cuMake(t)
	empty := cuCommit(t, s.dir, "empty.txt", "", "")

	all := cuWritten(t, s.dir, s.base, empty)
	if len(all) != 5 {
		t.Fatalf("parseWritten gave %d entries for 5 commits, the last with an empty message", len(all))
	}
	if !sameWords(all, all) {
		t.Error("sameWords says a list of commits is not the same as itself")
	}
	if got := cuWritten(t, s.dir, s.base, s.oldTip); len(got) != 4 {
		t.Errorf("parseWritten gave %d entries for the 4 commits of the story, want 4", len(got))
	}
	none := cuWritten(t, s.dir, empty, empty)
	if len(none) != 0 || !sameWords(none, nil) {
		t.Errorf("parseWritten gave %d entries for a range with no commit, and sameWords(none, nil) is %v, want 0 and true", len(none), sameWords(none, nil))
	}
}

// AC-2: what git wrote that does not come in fours -- a field left over after
// the last commit -- is not what was asked for. It is never read as a shorter
// list: a list with such an entry is the same as nothing, not even itself.
// (PLAN.md step 3: the leftover fields make one more entry, marked torn.)
func TestAC2ParseWrittenNeverReadsATornListAsAShorterOne(t *testing.T) {
	t.Parallel()
	s := cuMake(t)
	out, err := gitx.Written(t.Context(), s.dir, s.base, s.oldTip)
	if err != nil {
		t.Fatal(err)
	}
	good := parseWritten(out)
	torn := parseWritten(out + "left over\x00")

	if len(good) != 4 || !sameWords(good, good) {
		t.Fatalf("precondition: %d entries for 4 commits, or a list differs from itself", len(good))
	}
	if sameWords(torn, torn) || sameWords(good, torn) || sameWords(torn, good) {
		t.Error("a list with a torn entry is the same as another under sameWords")
	}
}

// AC-2: the stop's own replay keeps the words, so a replay is the same under
// sameWords though every hash and the committer's are new; and a replay that
// lost a commit, or kept one with other words, is not.
func TestAC2SameWordsHoldsAReplayToItsCommitsOneForOne(t *testing.T) {
	t.Parallel()
	s := cuMake(t)
	was := cuWritten(t, s.dir, s.base, s.oldTip)
	newTip := s.replay(t)
	if newTip == s.oldTip {
		t.Fatal("precondition: the replay made no new commit")
	}
	now := cuWritten(t, s.dir, s.moved, newTip)

	if len(now) != 4 || !sameWords(was, now) || !sameWords(now, was) {
		t.Fatalf("a replay that kept every message and author is not the same under sameWords (%d entries)", len(now))
	}

	t.Run("AC-2 a commit lost", func(t *testing.T) {
		short := cuWritten(t, s.dir, s.moved, swRev(t, s.dir, newTip+"~1"))
		if len(short) != 3 || sameWords(was, short) || sameWords(short, was) {
			t.Errorf("a replay of 3 commits for 4 is the same under sameWords (%d entries)", len(short))
		}
	})
	t.Run("AC-2 a commit gained", func(t *testing.T) {
		cuCommit(t, s.dir, "five.txt", "", "feat: five")
		more := cuWritten(t, s.dir, s.moved, swRev(t, s.dir, "HEAD"))
		ccGit(t, s.dir, "reset", "--quiet", "--hard", newTip)
		if len(more) != 5 || sameWords(was, more) || sameWords(more, was) {
			t.Errorf("a replay of 5 commits for 4 is the same under sameWords (%d entries)", len(more))
		}
	})
	t.Run("AC-2 nothing for something", func(t *testing.T) {
		if sameWords(was, nil) || sameWords(nil, was) {
			t.Error("no commits at all is the same as four under sameWords")
		}
	})

	// Each of these changes the tip of the replay after it was made, in one
	// thing sameWords compares. Pairs are by position, so the tip stands for any.
	author := func(who string) []string {
		return []string{"commit", "--quiet", "--amend", "--no-edit", "--author", who}
	}
	for _, tc := range []struct {
		name string
		// message is the tip's new message byte for byte, where it is the message
		// that changes; args are the arguments of the amend otherwise.
		message string
		args    []string
	}{
		{name: "AC-2 the message", message: cuFourth + "x\n"},
		{name: "AC-2 a trailer", message: "chore: four\n\nReviewed-by: Mallory <erin@example.org>\n"},
		{name: "AC-2 a blank line", message: "chore: four\n\n\nReviewed-by: Erin <erin@example.org>\n"},
		// cuFourth ends in a newline; this one does not, which -m cannot make.
		{name: "AC-2 no trailing newline", message: strings.TrimSuffix(cuFourth, "\n")},
		{name: "AC-2 the author's name", args: author("Mallory <t@example.com>")},
		// "Test" + "t@example.com" and "Testt" + "@example.com" are the same text
		// once joined: the name and the email are compared apart.
		{name: "AC-2 the boundary between the author's name and email moved", args: author("Testt <@example.com>")},
		{name: "AC-2 the author's email", args: author("Test <mallory@example.com>")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer ccGit(t, s.dir, "reset", "--quiet", "--hard", newTip)
			args := tc.args
			if args == nil {
				args = cuAmendWith(t, tc.message)
			}
			ccGit(t, s.dir, args...)
			changed := cuWritten(t, s.dir, s.moved, swRev(t, s.dir, "HEAD"))
			if len(changed) != 4 {
				t.Fatalf("precondition: the amended replay has %d commits, want 4", len(changed))
			}
			if sameWords(was, changed) || sameWords(changed, was) {
				t.Error("a replay whose tip differs in this is the same under sameWords")
			}
		})
	}

	// The committer, the committer's date and the hash are not words: a tip
	// made again by another committer, with the same author and message, is
	// still the same commit as far as the approval goes.
	t.Run("AC-2 the committer alone", func(t *testing.T) {
		defer ccGit(t, s.dir, "reset", "--quiet", "--hard", newTip)
		swGit(t, s.dir, "-c", "user.email=o@example.org", "-c", "user.name=Other", "commit", "--quiet", "--amend", "--no-edit")
		same := cuWritten(t, s.dir, s.moved, swRev(t, s.dir, "HEAD"))
		if !sameWords(was, same) {
			t.Error("a tip made again by another committer, with its author and message kept, differs under sameWords")
		}
	})
}
