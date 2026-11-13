package gitx

// US-129 AC-4, for a question git reads on standard input or answers into a
// writer: FileAt asks `cat-file --batch` with the commit and path on stdin,
// HasCommit asks `cat-file --batch-check` the same way, and Fingerprint
// streams `diff-tree -p` to a reader. The arguments are the same for every
// question, so a memo keyed on them alone hands the second question the first
// one's answer: a file compared at two commits reads unchanged, and a streamed
// patch reads empty (US-129 security review, F-1). It passes at the base,
// which keeps no answer.
//
// The plan's memo is made by a function the base does not have, so no row can
// ask for it yet; this one asks every question in one context in one process,
// which meets any memo that is on without being asked for, and the plan holds
// the one it asks for to the same cases.

import (
	"context"
	"strings"
	"testing"
)

func TestAC4TwoQuestionsAskedOnStandardInputWithTheSameArgumentsGetTheirOwnAnswers(t *testing.T) {
	root := t.TempDir()
	us129Run(t, root, nil, "", "init", "--quiet", "--initial-branch=main")
	us129Write(t, root, "x.txt", "first file\n")
	us129Write(t, root, "y.txt", "second file\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "both")
	base := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	us129Write(t, root, "x.txt", "first file, changed\n")
	us129Run(t, root, nil, "", "commit", "--quiet", "-a", "-m", "change")
	tip := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	ctx := context.Background()

	for path, want := range map[string]string{"x.txt": "first file\n", "y.txt": "second file\n"} {
		got, err := FileAt(ctx, root, base, path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("AC-4 FileAt %s at %s is %q, want %q", path, base[:12], got, want)
		}
	}
	if got, err := FileAt(ctx, root, tip, "x.txt"); err != nil || string(got) != "first file, changed\n" {
		t.Errorf("AC-4 FileAt x.txt at %s is %q (%v), want the changed file", tip[:12], got, err)
	}

	absent := strings.Repeat("1", len(base))
	for _, q := range []struct {
		commit string
		want   bool
	}{{base, true}, {absent, false}, {tip, true}} {
		got, err := HasCommit(ctx, root, q.commit)
		if err != nil {
			t.Fatalf("HasCommit %s: %v", q.commit[:12], err)
		}
		if got != q.want {
			t.Errorf("AC-4 HasCommit %s is %v, want %v", q.commit[:12], got, q.want)
		}
	}

	empty, err := Fingerprint(ctx, root, base, base)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Fingerprint(ctx, root, base, tip)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(ctx, root, base, tip)
	if err != nil {
		t.Fatal(err)
	}
	if first == empty {
		t.Fatalf("the change from %s to %s fingerprints as no change at all", base[:12], tip[:12])
	}
	if second != first {
		t.Errorf("AC-4 the same streamed patch asked twice fingerprints as %s and then %s (no change is %s)", first, second, empty)
	}
}
