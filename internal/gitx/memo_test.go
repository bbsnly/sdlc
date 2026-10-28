package gitx

import (
	"reflect"
	"testing"
)

// memoAnswers is every answer kept in answers, asked of base and tip in root.
func memoAnswers(t *testing.T, root, base, tip string) []any {
	t.Helper()
	ctx := t.Context()
	var said []any
	keep := func(v any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("asked with git on PATH: %v", err)
		}
		said = append(said, v)
	}
	keep(IsAncestor(ctx, root, base, tip))
	keep(IsAncestor(ctx, root, tip, base))
	keep(MergeBase(ctx, root, base, tip))
	keep(HasMerges(ctx, root, base, tip))
	keep(FirstAbove(ctx, root, base, tip))
	keep(ChangedBetween(ctx, root, base, tip))
	keep(FileAt(ctx, root, tip, "a.txt"))
	return said
}

// What git says of commits named by their full hashes is kept for the rest of
// the process: asked again with no git to run, each answer is the same. A
// ref is read again each time, and with no git it cannot be.
func TestAnAnswerAboutFullHashesIsAskedOfGitOnceAndARefEveryTime(t *testing.T) {
	root, base := lgRepo(t)
	write(t, root, "a.txt", "changed\n")
	tip := lgCommit(t, root, "second")

	first := memoAnswers(t, root, base, tip)
	if want := []any{true, false, base, false, tip, []string{"a.txt"}, []byte("changed\n")}; !reflect.DeepEqual(first, want) {
		t.Fatalf("the answers are %q, want %q", first, want)
	}
	t.Setenv("PATH", "")
	if again := memoAnswers(t, root, base, tip); !reflect.DeepEqual(again, first) {
		t.Errorf("asked again, the answers are %q, want %q", again, first)
	}
	if _, err := BranchTip(t.Context(), root, "main"); err == nil {
		t.Error("BranchTip answered with no git to ask: a ref was kept")
	}
}

// A failure is not an answer: a commit git did not have is asked about again,
// and once git has it, the answer is git's.
func TestAFailureIsAskedAgainOnceGitHasTheCommit(t *testing.T) {
	t.Parallel()
	root, base := lgRepo(t)
	other, _ := lgRepo(t)
	write(t, other, "b.txt", "b\n")
	elsewhere := lgCommit(t, other, "elsewhere")

	if _, err := IsAncestor(t.Context(), root, elsewhere, elsewhere); err == nil {
		t.Fatal("IsAncestor answered of a commit the repository does not have")
	}
	if _, err := ChangedBetween(t.Context(), root, base, elsewhere); err == nil {
		t.Fatal("ChangedBetween answered of a commit the repository does not have")
	}
	run(t, root, "fetch", "--quiet", other, "main")
	if held, err := IsAncestor(t.Context(), root, elsewhere, elsewhere); err != nil || !held {
		t.Errorf("IsAncestor of a fetched commit and itself = %v, %v; want true", held, err)
	}
	if changed, err := ChangedBetween(t.Context(), root, base, elsewhere); err != nil || len(changed) == 0 {
		t.Errorf("ChangedBetween once the commit is fetched = %q, %v", changed, err)
	}
}

// What a caller is handed is its own: changing it changes no later answer.
func TestAKeptAnswerIsHandedOutAsACopy(t *testing.T) {
	t.Parallel()
	root, base := lgRepo(t)
	write(t, root, "a.txt", "changed\n")
	tip := lgCommit(t, root, "second")

	changed, err := ChangedBetween(t.Context(), root, base, tip)
	if err != nil || len(changed) != 1 {
		t.Fatalf("ChangedBetween = %q, %v", changed, err)
	}
	changed[0] = "elsewhere"
	body, err := FileAt(t.Context(), root, tip, "a.txt")
	if err != nil || len(body) == 0 {
		t.Fatalf("FileAt = %q, %v", body, err)
	}
	body[0] = 'X'
	if again, _ := ChangedBetween(t.Context(), root, base, tip); !reflect.DeepEqual(again, []string{"a.txt"}) {
		t.Errorf("ChangedBetween after its answer was changed = %q", again)
	}
	if again, _ := FileAt(t.Context(), root, tip, "a.txt"); string(again) != "changed\n" {
		t.Errorf("FileAt after its answer was changed = %q", again)
	}
}
