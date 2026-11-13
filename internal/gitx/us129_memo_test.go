package gitx

// US-129 AC-4 for the invocation's memo and session (invocation.go,
// session.go, lookup.go), held here in their own package: every other row
// reaches them only through the cli and the hook, and none of gitx's own tests
// turned the memo on (US-129 verification, F-8 and gap 7).
//
// What the memo may keep: the answer of a read whose input is refs, the
// configuration or the worktree list, until the invocation itself runs a call
// that could change one, forgets, or closes. What it never keeps: a question
// on standard input, an answer sent to a writer, a read of the index or the
// work tree, or a failure. The session answers object and revision lookups
// with one process, and every answer it gives is the one the lookup's own
// process gives; where it cannot answer, the process is asked as before.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// us129Repo is a repository with two commits on main, a.txt and a directory,
// and returns its root and its two commits.
func us129Repo(t *testing.T) (root, first, second string) {
	t.Helper()
	root = t.TempDir()
	us129Run(t, root, nil, "", "init", "--quiet", "--initial-branch=main")
	us129Write(t, root, "a.txt", "one\n")
	us129Write(t, root, "dir/b.txt", "b\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "one")
	first = strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	us129Write(t, root, "a.txt", "two\n")
	us129Run(t, root, nil, "", "commit", "--quiet", "-a", "-m", "two")
	second = strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	return root, first, second
}

// us129Count is what `git rev-list --count HEAD` says through gitx in ctx.
func us129Count(ctx context.Context, t *testing.T, root string) string {
	t.Helper()
	out, err := git(ctx, root, nil, "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// us129Elsewhere is a commit another process makes in root.
func us129Elsewhere(t *testing.T, root, message string) {
	t.Helper()
	us129Run(t, root, nil, "", "commit", "--quiet", "--allow-empty", "-m", message)
}

func TestAC4AKeptAnswerHoldsUntilTheInvocationWritesForgetsOrCloses(t *testing.T) {
	root, _, _ := us129Repo(t)
	ctx := WithMemo(context.Background())
	if again := WithMemo(ctx); memoOf(again) != memoOf(ctx) {
		t.Fatal("AC-4 WithMemo on a context that has a memo made a second one")
	}

	if got := us129Count(ctx, t, root); got != "2" {
		t.Fatalf("rev-list --count is %s, want 2", got)
	}
	// Nothing this invocation did could change the answer, so the answer
	// kept is the one given: another process's commit is seen after the
	// invocation takes the lock, which forgets (AC-3's one question once).
	us129Elsewhere(t, root, "three")
	if got := us129Count(ctx, t, root); got != "2" {
		t.Errorf("AC-4 asked again with nothing written by the invocation, rev-list --count is %s, want the kept 2", got)
	}
	// A read of the work tree and a write to an index of the call's own
	// leave what is kept alone.
	if _, err := git(ctx, root, nil, "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(t.TempDir(), "index")
	if _, err := git(ctx, root, []string{"GIT_INDEX_FILE=" + index}, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if got := us129Count(ctx, t, root); got != "2" {
		t.Errorf("AC-4 after a status and an add to a private index, rev-list --count is %s, want the kept 2", got)
	}

	Forget(ctx)
	if got := us129Count(ctx, t, root); got != "3" {
		t.Errorf("AC-4 after Forget, rev-list --count is %s, want 3, read afresh", got)
	}

	us129Elsewhere(t, root, "four")
	if _, err := git(ctx, root, nil, "tag", "us129"); err != nil {
		t.Fatal(err)
	}
	if got := us129Count(ctx, t, root); got != "4" {
		t.Errorf("AC-4 after the invocation's own tag, rev-list --count is %s, want 4, read afresh", got)
	}

	Close(ctx)
	us129Elsewhere(t, root, "five")
	if got := us129Count(ctx, t, root); got != "5" {
		t.Errorf("AC-4 after Close, rev-list --count is %s, want 5: nothing is kept once the invocation has ended", got)
	}
	if got := us129Count(ctx, t, root); got != "5" {
		t.Errorf("AC-4 after Close, asked twice, rev-list --count is %s, want 5", got)
	}
	if s := memoOf(ctx).sessionFor(ctx, root); s != nil {
		t.Error("AC-4 a session started after Close")
	}
	// Close of a context with no memo, and Forget of one, do nothing.
	Close(context.Background())
	Forget(context.Background())
}

func TestAC4WithNoMemoEveryReadIsAskedAfresh(t *testing.T) {
	root, _, _ := us129Repo(t)
	ctx := context.Background()
	if got := us129Count(ctx, t, root); got != "2" {
		t.Fatalf("rev-list --count is %s, want 2", got)
	}
	us129Elsewhere(t, root, "three")
	if got := us129Count(ctx, t, root); got != "3" {
		t.Errorf("AC-4 with no memo, rev-list --count is %s after another process's commit, want 3", got)
	}
}

func TestAC4AQuestionOnStandardInputOrAnAnswerSentToAWriterIsNeverKept(t *testing.T) {
	root, first, second := us129Repo(t)
	ctx := WithMemo(context.Background())
	defer Close(ctx)
	for _, commit := range []string{first, second, first} {
		out, err := gitInput(ctx, root, nil, []byte(commit+"\n"), "cat-file", "--batch-check")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(out), commit+" commit ") {
			t.Errorf("AC-4 cat-file --batch-check asked of %s answered %q", commit[:8], out)
		}
	}
	for _, commit := range []string{first, second} {
		var w bytes.Buffer
		if _, err := sealedCall(ctx, root, nil, nil, &w, "rev-parse", commit+"^{tree}"); err != nil {
			t.Fatal(err)
		}
		want := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", commit+"^{tree}"))
		if strings.TrimSpace(w.String()) != want {
			t.Errorf("AC-4 rev-parse sent to a writer answered %q, want %s", w.String(), want)
		}
	}
}

func TestAC4AFailureIsAskedAgainAndAnAnswerFromBeforeADropIsNotKept(t *testing.T) {
	root, _, _ := us129Repo(t)
	ctx := WithMemo(context.Background())
	defer Close(ctx)
	args := []string{"rev-parse", "HEAD"}
	calls := 0
	ask := func(answer string, fail bool, during func()) (string, error) {
		out, err := memoised(ctx, false, root, nil, nil, nil, args, func() ([]byte, error) {
			calls++
			if during != nil {
				during()
			}
			if fail {
				return nil, errors.New("git failed")
			}
			return []byte(answer), nil
		})
		return string(out), err
	}

	if _, err := ask("", true, nil); err == nil {
		t.Fatal("the failing call did not fail")
	}
	if got, err := ask("first", false, nil); err != nil || got != "first" || calls != 2 {
		t.Errorf("AC-4 after a failure the question answered %q (%v) in %d calls, want first, asked again", got, err, calls)
	}
	if got, _ := ask("unused", false, nil); got != "first" || calls != 2 {
		t.Errorf("AC-4 asked a third time the question answered %q in %d calls, want the kept first", got, calls)
	}

	// A read that began before the invocation's own write and ended after the
	// drop that write made: its answer is from before the write.
	Forget(ctx)
	if got, _ := ask("from before the write", false, func() { Forget(ctx) }); got != "from before the write" {
		t.Fatalf("the read answered %q", got)
	}
	if got, _ := ask("after the write", false, nil); got != "after the write" {
		t.Errorf("AC-4 the next read answered %q, which a read that ran across a drop kept; want it asked afresh", got)
	}

	// And none is kept once the invocation is closed while the read ran.
	Forget(ctx)
	if got, _ := ask("while closing", false, func() { Close(ctx) }); got != "while closing" {
		t.Fatalf("the read answered %q", got)
	}
	if got, _ := ask("after the close", false, nil); got != "after the close" {
		t.Errorf("AC-4 after a close during a read, the next read answered %q, want it asked afresh", got)
	}
}

func TestAC4EveryGitCallIsKeptPassedThroughOrDropsWhatIsKept(t *testing.T) {
	t.Parallel()
	keep, pass, drop := classKeep, classPass, classDrop
	cases := []struct {
		args    []string
		stdin   bool
		stdout  bool
		private bool
		want    callClass
	}{
		{args: []string{"rev-parse", "HEAD"}, want: keep},
		{args: []string{"-C", "x", "-c", "a=b", "rev-parse", "HEAD"}, want: keep},
		{args: []string{"--no-pager", "show-ref", "--verify", "refs/heads/main"}, want: keep},
		{args: []string{"rev-list", "--count", "HEAD"}, want: keep},
		{args: []string{"log", "-1"}, want: keep},
		{args: []string{"merge-base", "a", "b"}, want: keep},
		{args: []string{"ls-tree", "HEAD"}, want: keep},
		{args: []string{"for-each-ref"}, want: keep},
		{args: []string{"var", "GIT_AUTHOR_IDENT"}, want: keep},
		{args: []string{"version"}, want: keep},
		{args: []string{"cat-file", "-e", "x"}, want: keep},
		{args: []string{"describe"}, want: keep},
		{args: []string{"name-rev", "x"}, want: keep},
		{args: []string{"diff-tree", "a", "b"}, want: keep},
		{args: []string{"diff-tree", "--stdin"}, want: drop},
		{args: []string{"config", "--get", "user.name"}, want: keep},
		{args: []string{"config", "--get-regexp", "x"}, want: keep},
		{args: []string{"config", "-l"}, want: keep},
		{args: []string{"config", "get", "user.name"}, want: keep},
		{args: []string{"config", "list"}, want: keep},
		{args: []string{"config", "user.name", "x"}, want: drop},
		{args: []string{"config", "set", "user.name", "x"}, want: drop},
		{args: []string{"worktree", "list", "--porcelain"}, want: keep},
		{args: []string{"worktree", "add", "x"}, want: drop},
		{args: []string{"worktree"}, want: drop},
		{args: []string{"symbolic-ref", "-q", "HEAD"}, want: keep},
		{args: []string{"symbolic-ref", "HEAD", "refs/heads/x"}, want: drop},
		{args: []string{"symbolic-ref", "--delete", "HEAD"}, want: drop},
		{args: []string{"symbolic-ref", "-d", "HEAD"}, want: drop},
		{args: []string{"status", "--porcelain"}, want: pass},
		{args: []string{"ls-files"}, want: pass},
		{args: []string{"diff-index", "HEAD"}, want: pass},
		{args: []string{"diff-files"}, want: pass},
		{args: []string{"check-ignore", "x"}, want: pass},
		{args: []string{"check-attr", "x"}, want: pass},
		{args: []string{"hash-object", "x"}, want: pass},
		{args: []string{"hash-object", "-w", "x"}, want: drop},
		{args: []string{"hash-object", "-w", "x"}, private: true, want: pass},
		{args: []string{"add", "-A"}, want: drop},
		{args: []string{"add", "-A"}, private: true, want: pass},
		{args: []string{"update-index", "--add"}, private: true, want: pass},
		{args: []string{"update-index", "--add"}, want: drop},
		{args: []string{"write-tree"}, private: true, want: pass},
		{args: []string{"write-tree"}, want: drop},
		{args: []string{"commit", "-m", "x"}, want: drop},
		{args: []string{"update-ref", "refs/heads/x", "y"}, want: drop},
		{args: []string{"merge", "x"}, want: drop},
		{args: []string{"a-subcommand-nobody-knows"}, want: drop},
		{args: []string{"-c", "a=b"}, want: drop},
		{args: nil, want: drop},
		{args: []string{"cat-file", "--batch-check"}, stdin: true, want: pass},
		{args: []string{"rev-parse", "HEAD"}, stdout: true, want: pass},
		{args: []string{"commit", "-F", "-"}, stdin: true, want: drop},
		{args: []string{"status"}, stdin: true, want: pass},
	}
	for _, c := range cases {
		var stdin []byte
		if c.stdin {
			stdin = []byte("x\n")
		}
		var stdout *bytes.Buffer
		if c.stdout {
			stdout = &bytes.Buffer{}
		}
		var got callClass
		if stdout != nil {
			got = classOf(c.args, stdin, stdout, c.private)
		} else {
			got = classOf(c.args, stdin, nil, c.private)
		}
		if got != c.want {
			t.Errorf("AC-4 git %s (stdin %v, a writer %v, a private index %v) is class %d, want %d (0 drops, 1 is kept, 2 passes through)",
				strings.Join(c.args, " "), c.stdin, c.stdout, c.private, got, c.want)
		}
	}
	if !hasPrivateIndex([]string{"A=b", "GIT_INDEX_FILE=/x"}) || hasPrivateIndex([]string{"GIT_DIR=/x"}) {
		t.Error("AC-4 a private index is not told from another variable")
	}
	if got := failedSubcommand([]string{"-c", "index.version=2", "add", "-A"}); got != "add" {
		t.Errorf("the subcommand a refusal names for -c index.version=2 add is %q, want add", got)
	}
	if got := failedSubcommand([]string{"-c", "a=b"}); got != "-c" {
		t.Errorf("the subcommand a refusal names for -c a=b is %q, want -c", got)
	}
	if got := failedSubcommand(nil); got != "" {
		t.Errorf("the subcommand a refusal names for nothing is %q, want none", got)
	}
}

// us129Answer is one lookup's answer, error included, in a form two runs of
// it can be compared in.
type us129Answer struct {
	value any
	err   string
}

func us129Said(v any, err error) us129Answer {
	a := us129Answer{value: v}
	if err != nil {
		a.err = err.Error()
	}
	return a
}

// us129Lookups is every lookup the session or the memo answers, and the
// questions around them, asked of root in ctx.
func us129Lookups(ctx context.Context, t *testing.T, root, wt, first, second, tree, blob string) map[string]us129Answer {
	t.Helper()
	absent := strings.Repeat("1", len(first))
	out := map[string]us129Answer{}
	ask := func(name string, v any, err error) { out[name] = us129Said(v, err) }
	for _, r := range []string{root, wt} {
		v, err := Head(ctx, r)
		ask("Head "+filepath.Base(r), v, err)
		b, err := Branch(ctx, r)
		ask("Branch "+filepath.Base(r), b, err)
	}
	for _, b := range []string{"main", "story/US-001", "packed", "missing", "broken", "-x"} {
		v, err := BranchTip(ctx, root, b)
		ask("BranchTip "+b, v, err)
	}
	for _, c := range []string{first, second, absent, tree, blob, "short", "-x"} {
		v, err := HasCommit(ctx, root, c)
		ask("HasCommit "+c, v, err)
	}
	for _, q := range [][2]string{{second, "a.txt"}, {first, "a.txt"}, {second, "dir"}, {second, "absent"}, {absent, "a.txt"},
		{tree, "a.txt"}, {second, "dir/b.txt"}, {second, "x\ny"}, {second, "-x"}} {
		v, err := FileAt(ctx, root, q[0], q[1])
		ask("FileAt "+q[0]+":"+q[1], string(v), err)
	}
	for _, rev := range []string{first, second, absent, tree, "refs/heads/main", "refs/heads/packed", "refs/heads/missing",
		"refs/heads/broken", "refs/tags/v1", "refs/tags/light", "refs/tags/missing", "main", "HEAD", "HEAD~1", "-x",
		"refs/heads/main\nx", "refs/remotes/origin/main"} {
		v, err := Resolve(ctx, root, rev)
		ask("Resolve "+rev, v, err)
	}
	for _, q := range [][2]string{{second, "a.txt"}, {second, "dir"}, {second, "absent"}, {"refs/heads/main", "dir/b.txt"},
		{"refs/tags/v1", "a.txt"}, {absent, "a.txt"}, {"main", "a.txt"}} {
		v, err := Holds(ctx, root, q[0], q[1])
		ask("Holds "+q[0]+":"+q[1], v, err)
	}
	for _, q := range [][2]string{{first, first}, {second, second}, {first, second}, {absent, absent}, {tree, tree}} {
		v, err := IsAncestor(ctx, root, q[0], q[1])
		ask("IsAncestor "+q[0]+" "+q[1], v, err)
		m, err := MergeBase(ctx, root, q[0], q[1])
		ask("MergeBase "+q[0]+" "+q[1], m, err)
	}
	gitDir, err := GitDir(ctx, wt)
	ask("GitDir", gitDir, err)
	files, err := Files(ctx, root)
	ask("Files", files, err)
	clean, err := Clean(ctx, root)
	ask("Clean", clean, err)
	changes, err := Changes(ctx, root)
	ask("Changes", changes, err)
	commits, err := CommitsBetween(ctx, root, first, second)
	ask("CommitsBetween", fmt.Sprint(commits), err)
	listed, err := Worktrees(ctx, root)
	ask("Worktrees", fmt.Sprint(listed), err)
	ahead, err := Ahead(ctx, root, "story/US-001", "main")
	ask("Ahead", ahead, err)
	changed, err := ChangedBetween(ctx, root, first, second)
	ask("ChangedBetween", changed, err)
	held, err := CheckedOut(ctx, root, "story/US-001")
	ask("CheckedOut", held, err)
	tree2, err := TreeHash(ctx, root)
	ask("TreeHash", tree2, err)
	ask("Trunk", Trunk(ctx, root), nil)
	for _, q := range [][2]string{{second, "a.txt"}, {second, "dir"}, {second, "absent"}, {absent, "a.txt"}} {
		v, err := ModeAt(ctx, root, q[0], q[1])
		ask("ModeAt "+q[0]+":"+q[1], v, err)
	}
	headAhead, err := HeadAhead(ctx, wt, "main")
	ask("HeadAhead", headAhead, err)
	uncommitted, err := Uncommitted(ctx, root, true)
	ask("Uncommitted", uncommitted, err)
	progress, err := InProgress(ctx, wt)
	ask("InProgress", progress, err)
	resolves, err := ResolvesTo(ctx, wt, "story/US-001", "refs/heads/story/US-001")
	ask("ResolvesTo", resolves, err)
	merges, err := HasMerges(ctx, root, first, second)
	ask("HasMerges", merges, err)
	above, err := FirstAbove(ctx, root, first, second)
	ask("FirstAbove", above, err)
	written, err := Written(ctx, root, first, second)
	ask("Written", written, err)
	under, err := ChangedUnder(ctx, root, first, second, "dir")
	ask("ChangedUnder", under, err)
	stats, err := Numstat(ctx, root, first, second)
	ask("Numstat", fmt.Sprint(stats), err)
	fp, err := Fingerprint(ctx, root, first, second)
	ask("Fingerprint", fp, err)
	return out
}

// us129Layout is us129Repo with a linked worktree on story/US-001, a branch
// that is only packed, a branch whose commit git does not have, an annotated
// and a light tag, and a change in the work tree; and its tree and a blob.
func us129Layout(t *testing.T) (root, wt, first, second, tree, blob string) {
	t.Helper()
	root, first, second = us129Repo(t)
	us129Run(t, root, nil, "", "tag", "-a", "-m", "annotated", "v1", first)
	us129Run(t, root, nil, "", "tag", "light", second)
	us129Run(t, root, nil, "", "branch", "packed", first)
	us129Run(t, root, nil, "", "pack-refs", "--all")
	wt = filepath.Join(t.TempDir(), "story")
	us129Run(t, root, nil, "", "worktree", "add", "--quiet", "-b", "story/US-001", wt, "main")
	us129Write(t, wt, "c.txt", "on the story\n")
	us129Run(t, wt, nil, "", "add", "-A")
	us129Run(t, wt, nil, "", "commit", "--quiet", "-m", "story")
	us129Write(t, root, ".git/refs/heads/broken", strings.Repeat("2", len(first))+"\n")
	us129Write(t, root, "a.txt", "changed in the work tree\n")
	tree = strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", second+"^{tree}"))
	blob = strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", second+":a.txt"))
	return root, wt, first, second, tree, blob
}

func TestAC4EveryLookupTheSessionAnswersIsWhatItsOwnProcessAnswers(t *testing.T) {
	root, wt, first, second, tree, blob := us129Layout(t)
	ctx := WithMemo(context.Background())
	// The memo's run first: the answers kept for the process (memo.go) are
	// then the session's, and the run with no memo is held to them.
	withMemo := us129Lookups(ctx, t, root, wt, first, second, tree, blob)
	m := memoOf(ctx)
	if m.starts != 1 || m.requests == 0 {
		t.Errorf("AC-4 one invocation of one repository started %d sessions and asked them %d times, want one asked at least once",
			m.starts, m.requests)
	}
	Close(ctx)
	without := us129Lookups(context.Background(), t, root, wt, first, second, tree, blob)
	for name, want := range without {
		if got := withMemo[name]; !reflect.DeepEqual(got, want) {
			t.Errorf("AC-4 %s with the memo and session answers %v (%q), with neither %v (%q)",
				name, got.value, got.err, want.value, want.err)
		}
	}
}

func TestAC4ALookupInADirectoryThatIsNoRepositoryIsRefusedAsWithNoMemo(t *testing.T) {
	dir := t.TempDir()
	ctx := WithMemo(context.Background())
	defer Close(ctx)
	_, plainErr := Head(context.Background(), dir)
	_, memoErr := Head(ctx, dir)
	if plainErr == nil || memoErr == nil || plainErr.Error() != memoErr.Error() {
		t.Errorf("AC-4 Head outside a repository: with the memo %v, without %v", memoErr, plainErr)
	}
	if s := memoOf(ctx).sessionFor(ctx, dir); s != nil {
		t.Error("AC-4 a session started for a directory that is no repository")
	}
	if s := plainSessionOf(ctx, dir); s != nil {
		t.Error("AC-4 a plain session was given for a directory that is no repository")
	}
	if refAbsent(dir, "refs/heads/main") {
		t.Error("AC-4 a ref was said to be absent from a directory that is no repository, which is git's to say")
	}
}

func TestAC2TheTreeHashWithEveryOverrideIsTheTreeGitWritesWithAndWithoutTheMemo(t *testing.T) {
	root, first, second := us129Repo(t)
	us129Write(t, root, "run.sh", "#!/bin/sh\n")
	us129Write(t, root, "dir/c.txt", "c\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "update-index", "--chmod=+x", "run.sh")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "three")
	third := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	us129Write(t, root, "a.txt", "in the work tree\n")
	us129Write(t, root, "dir/b.txt", "b, changed\n")
	absent := strings.Repeat("4", len(first))
	cases := map[string][]Override{
		"no override":                          nil,
		"a file as an older commit has it":     {{Path: "a.txt", At: first}},
		"a nested file as a commit has it":     {{Path: "dir/b.txt", At: second}},
		"a file a commit does not have":        {{Path: "run.sh", At: first}},
		"an executable as its commit has it":   {{Path: "run.sh", At: third}},
		"a file in a directory a commit lacks": {{Path: "dir/c.txt", At: first}},
		"content given":                        {{Path: "a.txt", Content: []byte("given\n")}},
		"a path the tree does not have":        {{Path: "nowhere.txt", At: first}},
		"two overrides":                        {{Path: "a.txt", At: first}, {Path: "dir/b.txt", Content: []byte("x\n")}},
		"a directory":                          {{Path: "dir", At: first}},
		"a commit git does not have":           {{Path: "a.txt", At: absent}},
		"a commit by a short name":             {{Path: "a.txt", At: "main"}},
	}
	for name, overrides := range cases {
		t.Run("AC-2 "+name, func(t *testing.T) {
			ctx := WithMemo(context.Background())
			got, gotErr := TreeHash(ctx, root, overrides...)
			Close(ctx)
			want, wantErr := TreeHash(context.Background(), root, overrides...)
			if got != want || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
				t.Errorf("AC-2 the tree hash with the memo is %s (%v), with none %s (%v)", got, gotErr, want, wantErr)
			}
			if wantErr == nil {
				if byHand := us129ByHand(t, root, overrides...); want != byHand {
					t.Errorf("AC-2 the tree hash is %s, git's sequence writes %s", want, byHand)
				}
			}
		})
	}
}
