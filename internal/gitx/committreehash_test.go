package gitx

// US-134 AC-6: a story lands on the result the implementation gate recorded
// for its tree only where the tree that lands is that tree. The gate measures
// the working tree with TreeHash; the landing measures the commit with
// CommitTreeHash. The two have to agree for a checkout that holds the commit's
// files, or every landing runs the commands again (or, worse, one that
// differs is taken for a match). What is held here is the agreement, against
// the tree git itself writes for the same files, and that work nobody
// committed is not in the commit's hash.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// cthRepo is a repository in a new directory, of the object format given, in
// two commits: the first holds nested directories, names that sort otherwise
// as a file than as a directory, an executable, a link and files under .sdlc;
// the second changes data.json and adds gone.txt. The working tree is left
// clean at the second.
func cthRepo(t *testing.T, format string) (root, first, second string) {
	t.Helper()
	root = t.TempDir()
	us129Run(t, root, nil, "", "init", "--quiet", "--initial-branch=main", "--object-format="+format)
	for name, body := range map[string]string{
		"nested/deep/er/a.txt": "deep\n", "a/c": "c\n", "a-b": "a-b\n", "a0": "a0\n",
		"run.sh": "#!/bin/sh\necho run\n", "data.json": "{\"a\": 1}\n",
		".sdlc/config.json": "{}\n", ".sdlc/stories/US-1/PLAN.md": "# plan\n",
	} {
		us129Write(t, root, name, body)
	}
	if err := os.Chmod(filepath.Join(root, "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("nested", filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
	}
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "first")
	first = strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	us129Write(t, root, "data.json", "{\"a\": 2}\n")
	us129Write(t, root, "gone.txt", "added by the second commit\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "second")
	second = strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	return root, first, second
}

func cthHash(t *testing.T, root, commit string, overrides ...Override) string {
	t.Helper()
	got, err := CommitTreeHash(t.Context(), root, commit, overrides...)
	if err != nil {
		t.Fatalf("CommitTreeHash(%s): %v", commit[:12], err)
	}
	return got
}

func TestAC6TheTreeOfACommitIsTheTreeTreeHashMeasuresOfACheckoutHoldingItsFiles(t *testing.T) {
	for _, format := range []string{"sha1", "sha256"} {
		t.Run("AC-6 "+format, func(t *testing.T) {
			root, first, second := cthRepo(t, format)
			got := cthHash(t, root, second)
			if want := us129ByHand(t, root); got != want {
				t.Errorf("CommitTreeHash of HEAD = %s, want %s, the tree git writes for the clean checkout less .sdlc", got, want)
			}
			measured, err := TreeHash(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			if got != measured {
				t.Errorf("CommitTreeHash of HEAD = %s, TreeHash of the clean checkout = %s: the landing would never match the gate", got, measured)
			}
			if want := map[string]int{"sha1": 40, "sha256": 64}[format]; len(got) != want {
				t.Errorf("the hash is %d hex digits, want %d for %s", len(got), want, format)
			}

			// A checkout of the first commit, elsewhere: what the first
			// commit holds, not what HEAD does.
			wt := filepath.Join(t.TempDir(), "first")
			us129Run(t, root, nil, "", "worktree", "add", "--quiet", "--detach", wt, first)
			atFirst, err := TreeHash(t.Context(), wt)
			if err != nil {
				t.Fatal(err)
			}
			if got := cthHash(t, root, first); got != atFirst {
				t.Errorf("CommitTreeHash of the first commit = %s, TreeHash of its checkout = %s", got, atFirst)
			}
			if atFirst == measured {
				t.Errorf("two commits with different files measure the same, %s", atFirst)
			}
		})
	}
}

func TestAC6WorkACheckoutHoldsAndNobodyCommittedIsNotInTheCommitsTree(t *testing.T) {
	root, _, second := cthRepo(t, "sha1")
	before := cthHash(t, root, second)
	us129Write(t, root, "data.json", "{\"a\": \"changed on disk\"}\n")
	us129Write(t, root, "untracked.txt", "never added\n")
	if got := cthHash(t, root, second); got != before {
		t.Errorf("a change on disk moved the commit's hash from %s to %s", before, got)
	}
	onDisk, err := TreeHash(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk == before {
		t.Errorf("the working tree with uncommitted work measures as the commit, %s: work nobody committed would land on its result", before)
	}
}

func TestAC6WhatACommitHoldsUnderTheLoopsDirectoryIsLeftOut(t *testing.T) {
	root, _, second := cthRepo(t, "sha1")
	before := cthHash(t, root, second)
	us129Write(t, root, ".sdlc/stories/US-1/PLAN.md", "# plan, revised\n")
	us129Write(t, root, ".sdlc/state/gates.json", "{}\n")
	us129Run(t, root, nil, "", "add", "-A", "-f", ".sdlc")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "loop state only")
	third := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	if got := cthHash(t, root, third); got != before {
		t.Errorf("a commit that changed only .sdlc measures %s, want %s as before it", got, before)
	}
	us129Write(t, root, "a0", "a0, changed\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "code")
	fourth := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	if got := cthHash(t, root, fourth); got == before {
		t.Errorf("a commit that changed a file outside .sdlc measures as before it, %s", got)
	}
}

func TestAC6AnOverrideIsAppliedToTheCommitsTreeAsTreeHashAppliesIt(t *testing.T) {
	root, first, second := cthRepo(t, "sha1")
	plain := cthHash(t, root, second)
	cases := []struct {
		name string
		o    Override
		same bool
	}{
		{name: "AC-6 other content for a file", o: Override{Path: "data.json", Content: []byte("{\"a\": 1, \"bookkeeping\": false}\n")}},
		{name: "AC-6 an executable keeps its mode", o: Override{Path: "run.sh", Content: []byte("#!/bin/sh\necho other\n")}},
		{name: "AC-6 the file as an earlier commit has it", o: Override{Path: "data.json", At: first}},
		{name: "AC-6 a file an earlier commit does not have is taken out", o: Override{Path: "gone.txt", At: first}},
		{name: "AC-6 a path the tree has no file at changes nothing", o: Override{Path: "absent.txt", Content: []byte("x\n")}, same: true},
		{name: "AC-6 a path under .sdlc changes nothing", o: Override{Path: ".sdlc/config.json", Content: []byte("{\"x\": 1}\n")}, same: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cthHash(t, root, second, c.o)
			if want := us129ByHand(t, root, c.o); got != want {
				t.Errorf("with the override, CommitTreeHash = %s, want %s, the tree git writes with it", got, want)
			}
			if measured, err := TreeHash(t.Context(), root, c.o); err != nil || got != measured {
				t.Errorf("with the override, CommitTreeHash = %s, TreeHash of the checkout = %s (%v)", got, measured, err)
			}
			if (got == plain) != c.same {
				t.Errorf("with the override the hash is %s, without it %s; want them the same: %v", got, plain, c.same)
			}
		})
	}
}

func TestAC6AnOverrideThatCannotStandInForOneFileRefusesAsUnmeasurable(t *testing.T) {
	root, first, second := cthRepo(t, "sha1")
	_, err := CommitTreeHash(t.Context(), root, second, Override{Path: "nested", Content: []byte("a file where a directory is\n")})
	wantUnreadable(t, err, "CommitTreeHash with content over a directory")
	if e := (*sdlcerr.Error)(nil); !errors.As(err, &e) || !strings.Contains(e.Why, `"nested"`) {
		t.Errorf("the refusal %v does not say which override could not be applied", err)
	}
	_, err = CommitTreeHash(t.Context(), root, second, Override{Path: "nested", At: first})
	if !errors.Is(err, ErrOverrideNotAFile) {
		t.Errorf("CommitTreeHash with a commit's file over a directory = %v, want ErrOverrideNotAFile", err)
	}
	_, err = CommitTreeHash(t.Context(), root, second, Override{Path: "data.json", At: first[:12]})
	wantUnreadable(t, err, "CommitTreeHash with an override at a short hash")
}

func TestAC6ACommitNamedOtherwiseThanByItsFullHashIsRefusedBeforeGitIsAsked(t *testing.T) {
	root, _, second := cthRepo(t, "sha1")
	for _, name := range []string{"HEAD", "main", second[:12], "--output=" + filepath.Join(t.TempDir(), "x"), strings.ToUpper(second), ""} {
		_, err := CommitTreeHash(t.Context(), root, name)
		wantUnreadable(t, err, "CommitTreeHash of "+name)
	}
}

func TestAC6AFullHashOfNoCommitIsAnErrorNotAHash(t *testing.T) {
	root, _, _ := cthRepo(t, "sha1")
	got, err := CommitTreeHash(t.Context(), root, strings.Repeat("0", 40))
	if err == nil {
		t.Errorf("CommitTreeHash of a hash the repository does not hold = %q, want an error", got)
	}
}

func TestAC6EachItemOfTheCommitsListingIsReadOrRefused(t *testing.T) {
	sum := strings.Repeat("ab", 20)
	cases := []struct {
		name, item string
		ok         bool
		want       indexEntry
	}{
		{name: "AC-6 a file", item: "100644 blob " + sum + "\tsrc/a.go", ok: true,
			want: indexEntry{mode: 0o100644, hash: []byte(strings.Repeat("\xab", 20)), path: "src/a.go"}},
		{name: "AC-6 an executable", item: "100755 blob " + sum + "\trun.sh", ok: true,
			want: indexEntry{mode: 0o100755, hash: []byte(strings.Repeat("\xab", 20)), path: "run.sh"}},
		{name: "AC-6 a link", item: "120000 blob " + sum + "\tlink", ok: true,
			want: indexEntry{mode: 0o120000, hash: []byte(strings.Repeat("\xab", 20)), path: "link"}},
		{name: "AC-6 a submodule", item: "160000 commit " + sum + "\tsub", ok: true,
			want: indexEntry{mode: 0o160000, hash: []byte(strings.Repeat("\xab", 20)), path: "sub"}},
		{name: "AC-6 a name with a space and a tab", item: "100644 blob " + sum + "\tdir/a b\tc", ok: true,
			want: indexEntry{mode: 0o100644, hash: []byte(strings.Repeat("\xab", 20)), path: "dir/a b\tc"}},
		{name: "AC-6 the loop's directory is read with no hash", item: "100644 blob " + sum + "\t.sdlc", ok: true,
			want: indexEntry{path: ".sdlc"}},
		{name: "AC-6 a file under the loop's directory is read with no hash", item: "100644 blob zz\t.sdlc/state/x", ok: true,
			want: indexEntry{path: ".sdlc/state/x"}},
		{name: "AC-6 a name that only starts as the loop's directory is read", item: "100644 blob " + sum + "\t.sdlcrc", ok: true,
			want: indexEntry{mode: 0o100644, hash: []byte(strings.Repeat("\xab", 20)), path: ".sdlcrc"}},
		{name: "AC-6 no tab", item: "100644 blob " + sum + " src/a.go"},
		{name: "AC-6 two fields", item: "100644 " + sum + "\tsrc/a.go"},
		{name: "AC-6 four fields", item: "100644 blob " + sum + " 12\tsrc/a.go"},
		{name: "AC-6 a mode that is not octal", item: "100648 blob " + sum + "\tsrc/a.go"},
		{name: "AC-6 a hash that is not hex", item: "100644 blob " + strings.Repeat("zz", 20) + "\tsrc/a.go"},
		{name: "AC-6 a hash of another width", item: "100644 blob " + strings.Repeat("ab", 32) + "\tsrc/a.go"},
		{name: "AC-6 an odd number of hex digits", item: "100644 blob " + sum + "a\tsrc/a.go"},
		{name: "AC-6 empty", item: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := lsTreeEntry(c.item, 20)
			if ok != c.ok {
				t.Fatalf("lsTreeEntry(%q) read = %v, want %v", c.item, ok, c.ok)
			}
			if got.mode != c.want.mode || string(got.hash) != string(c.want.hash) || got.path != c.want.path {
				t.Errorf("lsTreeEntry(%q) = {%o %x %q}, want {%o %x %q}", c.item, got.mode, got.hash, got.path,
					c.want.mode, c.want.hash, c.want.path)
			}
			if c.want.hash == nil && got.hash != nil {
				t.Errorf("lsTreeEntry(%q) gave a hash, want none", c.item)
			}
		})
	}
}
