package gitx

// US-129 AC-4 for the session's own checks (session.go) and for the two
// questions that decide whether it may answer at all (lookup.go). A reply
// that is not of the request's shape ends the session, since every reply
// after a split request would belong to an earlier question (security F-10),
// and the lookup is then its own process's, as at the base. The session is
// sealed: it reads no replace ref and no graft, so a lookup that runs as
// plain git today goes to it only where the repository has neither, and a
// reftable store, whose refs cannot be read from the files, never does
// (security F-17, red team F-28).

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// us129Session is a session started for root's repository whose replies are
// read from replies instead of from git: what git is sent still reaches it.
func us129Session(t *testing.T, root, replies string) *session {
	t.Helper()
	_, common, err := GitDirOf(root)
	if err != nil {
		t.Fatal(err)
	}
	s := startSession(context.Background(), common)
	if s == nil {
		t.Fatal("no session could be started")
	}
	t.Cleanup(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.end()
	})
	s.out = bufio.NewReader(strings.NewReader(replies))
	return s
}

func TestAC4AReplyThatIsNotOfTheRequestsShapeEndsTheSession(t *testing.T) {
	root, _, second := us129Repo(t)
	hash := second
	cases := map[string]struct {
		replies string
		info    bool // ask info, not contents
		ok      bool
		dead    bool
		want    reply
		body    string
	}{
		"missing":                   {replies: "x missing\n", info: true, ok: true, want: reply{missing: true}},
		"ambiguous":                 {replies: "x ambiguous\n", info: true, ok: true, want: reply{ambiguous: true}},
		"an object":                 {replies: hash + " commit 12\n", info: true, ok: true, want: reply{oid: hash, typ: "commit", size: 12}},
		"nothing at all":            {replies: "", info: true, dead: true},
		"a line cut off":            {replies: hash + " commit", info: true, dead: true},
		"two fields":                {replies: hash + " commit\n", info: true, dead: true},
		"four fields":               {replies: hash + " commit 12 x\n", info: true, dead: true},
		"a short hash":              {replies: hash[:12] + " commit 12\n", info: true, dead: true},
		"two spaces":                {replies: hash + "  commit 12\n", info: true, dead: true},
		"a size that is no number":  {replies: hash + " commit twelve\n", info: true, dead: true},
		"another name missing":      {replies: "y missing\n", info: true, dead: true},
		"a file":                    {replies: hash + " blob 4\nbody\n", ok: true, want: reply{oid: hash, typ: "blob", size: 4}, body: "body"},
		"an empty file":             {replies: hash + " blob 0\n\n", ok: true, want: reply{oid: hash, typ: "blob"}, body: ""},
		"a file missing":            {replies: "x missing\n", ok: true, want: reply{missing: true}},
		"a file ambiguous":          {replies: "x ambiguous\n", ok: true, want: reply{ambiguous: true}},
		"a negative size":           {replies: hash + " blob -1\n", dead: true},
		"a size over a gigabyte":    {replies: hash + " blob 1073741825\n", dead: true},
		"a body cut off":            {replies: hash + " blob 10\nbody\n", dead: true},
		"a body with no line break": {replies: hash + " blob 4\nbodyX", dead: true},
		"a header and nothing else": {replies: hash + " blob 4\n", dead: true},
	}
	for name, c := range cases {
		t.Run("AC-4 "+name, func(t *testing.T) {
			s := us129Session(t, root, c.replies)
			var r reply
			var ok bool
			var body []byte
			if c.info {
				r, ok = s.info("x")
			} else {
				body, r, ok = s.contents("x")
			}
			if ok != c.ok || s.dead != c.dead || r != c.want || string(body) != c.body {
				t.Errorf("AC-4 the reply %q: answered %v %+v %q, ended %v; want %v %+v %q, ended %v",
					c.replies, ok, r, body, s.dead, c.ok, c.want, c.body, c.dead)
			}
			if c.dead {
				if _, ok := s.info("x"); ok {
					t.Error("AC-4 an ended session answered again")
				}
				if _, _, ok := s.contents("x"); ok {
					t.Error("AC-4 an ended session gave a file")
				}
				if _, err := os.Stat(s.private); !os.IsNotExist(err) {
					t.Errorf("AC-4 the ended session left its directory %s (%v)", s.private, err)
				}
			}
		})
	}
}

func TestAC4ANameThatCannotBeOneRequestIsNeverSentAndLeavesTheSessionAsItWas(t *testing.T) {
	root, _, _ := us129Repo(t)
	s := us129Session(t, root, "")
	for _, name := range []string{"", "a\nb", "a\rb", "a\x00b"} {
		if _, ok := s.info(name); ok {
			t.Errorf("AC-4 the session answered %q", name)
		}
		if _, _, ok := s.contents(name); ok {
			t.Errorf("AC-4 the session gave a file for %q", name)
		}
	}
	if s.dead {
		t.Error("AC-4 a name that was never sent ended the session")
	}
	var none *session
	if _, ok := none.info("x"); ok {
		t.Error("AC-4 no session answered")
	}
	if _, _, ok := none.contents("x"); ok {
		t.Error("AC-4 no session gave a file")
	}
}

func TestAC4ASessionClosedWhileARequestHoldsItAnswersNothingAfter(t *testing.T) {
	root, _, _ := us129Repo(t)
	s := us129Session(t, root, "")
	s.mu.Lock()
	s.close()
	s.mu.Unlock()
	if _, ok := s.info("refs/heads/main"); ok {
		t.Error("AC-4 a session closed under a request answered the next one")
	}
	if !s.dead {
		t.Error("AC-4 a session closed under a request was not ended by the next one")
	}
	// Closed when no request holds it, it ends at once.
	other := us129Session(t, root, "")
	other.close()
	if !other.dead {
		t.Error("AC-4 a session closed with no request in progress was not ended")
	}
}

func TestAC4ALookupWhoseSessionEndsIsAnsweredByItsOwnProcess(t *testing.T) {
	root, first, second := us129Repo(t)
	ctx := WithMemo(context.Background())
	defer Close(ctx)
	_, common, err := GitDirOf(root)
	if err != nil {
		t.Fatal(err)
	}
	m := memoOf(ctx)
	m.sessions = map[string]*session{common: us129Session(t, root, "not a reply\n")}
	head, err := Head(ctx, root)
	if err != nil || head != second {
		t.Errorf("AC-4 Head after the session's reply was not of its shape is %s (%v), want %s", head, err, second)
	}
	if !m.sessions[common].dead {
		t.Error("AC-4 the session that replied out of shape was not ended")
	}
	for c, want := range map[string]bool{first: true, strings.Repeat("3", len(first)): false} {
		if has, err := HasCommit(ctx, root, c); err != nil || has != want {
			t.Errorf("AC-4 HasCommit %s with the session ended is %v (%v), want %v", c[:8], has, err, want)
		}
	}
	if body, err := FileAt(ctx, root, first, "a.txt"); err != nil || string(body) != "one\n" {
		t.Errorf("AC-4 FileAt with the session ended is %q (%v)", body, err)
	}
}

func TestAC4PlainLookupsGoToTheSessionOnlyWhereTheyReadAsASealedOneReads(t *testing.T) {
	cases := map[string]struct {
		setup func(t *testing.T, root, common string)
		want  bool
	}{
		"a repository as git makes it": {func(*testing.T, string, string) {}, true},
		"an empty refs/replace": {func(t *testing.T, _, common string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(common, "refs", "replace"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, true},
		"a replace ref": {func(t *testing.T, root, _ string) {
			t.Helper()
			first := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD~1"))
			us129Run(t, root, nil, "", "replace", first, "HEAD")
		}, false},
		"a replace ref, packed": {func(t *testing.T, root, common string) {
			t.Helper()
			first := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD~1"))
			us129Run(t, root, nil, "", "replace", first, "HEAD")
			us129Run(t, root, nil, "", "pack-refs", "--all")
			if entries, _ := os.ReadDir(filepath.Join(common, "refs", "replace")); len(entries) > 0 {
				t.Fatalf("pack-refs left %d loose replace refs", len(entries))
			}
		}, false},
		"packed refs and no replace ref": {func(t *testing.T, root, _ string) {
			t.Helper()
			us129Run(t, root, nil, "", "pack-refs", "--all")
		}, true},
		"a packed-refs line longer than the scan takes": {func(t *testing.T, _, common string) {
			t.Helper()
			us129Write(t, common, "packed-refs", "# pack-refs with: peeled\n"+strings.Repeat("x", 2<<20)+"\n")
		}, false},
		"a graft file": {func(t *testing.T, _, common string) {
			t.Helper()
			us129Write(t, common, "info/grafts", "")
		}, false},
		"a reftable": {func(t *testing.T, _, common string) {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(common, "reftable"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, false},
	}
	for name, c := range cases {
		t.Run("AC-4 "+name, func(t *testing.T) {
			root, _, _ := us129Repo(t)
			_, common, err := GitDirOf(root)
			if err != nil {
				t.Fatal(err)
			}
			c.setup(t, root, common)
			if got := plainLookupsReadAlike(common); got != c.want {
				t.Errorf("AC-4 with %s, plain lookups may go to the sealed session: %v, want %v", name, got, c.want)
			}
		})
	}
	for _, v := range []string{"GIT_REPLACE_REF_BASE", "GIT_NO_REPLACE_OBJECTS", "GIT_GRAFT_FILE"} {
		t.Run("AC-4 "+v+" set", func(t *testing.T) {
			root, _, _ := us129Repo(t)
			_, common, err := GitDirOf(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv(v, "")
			if plainLookupsReadAlike(common) {
				t.Errorf("AC-4 with %s set, plain lookups may go to the sealed session", v)
			}
		})
	}
}

// A replace ref changes what a commit holds for plain git and not for the
// sealed session: Holds, which runs as plain git at the base, answers as it
// did there, the replacement read.
func TestAC4HoldsInARepositoryWithAReplaceRefAnswersAsPlainGitDoes(t *testing.T) {
	root, first, _ := us129Repo(t)
	us129Write(t, root, "only-in-the-third.txt", "x\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "three")
	third := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	us129Run(t, root, nil, "", "replace", first, third)
	ctx := WithMemo(context.Background())
	defer Close(ctx)
	want, wantErr := Holds(context.Background(), root, first, "only-in-the-third.txt")
	got, err := Holds(ctx, root, first, "only-in-the-third.txt")
	if got != want || (err == nil) != (wantErr == nil) || !want {
		t.Errorf("AC-4 Holds of the replaced commit with the memo is %v (%v), with none %v (%v), plain git reads the replacement",
			got, err, want, wantErr)
	}
	if allowed := memoOf(ctx).plain; len(allowed) != 1 {
		t.Errorf("AC-4 the repository was judged %d times for plain lookups, want once: %v", len(allowed), allowed)
	}
}

func TestAC4ARefIsAbsentOnlyWhereNeitherItsFileNorAPackedLineNamesIt(t *testing.T) {
	root, _, _ := us129Repo(t)
	_, common, err := GitDirOf(root)
	if err != nil {
		t.Fatal(err)
	}
	us129Run(t, root, nil, "", "branch", "packed")
	us129Run(t, root, nil, "", "pack-refs", "--all")
	us129Run(t, root, nil, "", "branch", "loose")
	for ref, want := range map[string]bool{
		"refs/heads/loose":   false,
		"refs/heads/packed":  false,
		"refs/heads/main":    false,
		"refs/heads/missing": true,
		"refs/heads/pack":    true,
	} {
		if got := refAbsent(root, ref); got != want {
			t.Errorf("AC-4 %s is absent: %v, want %v", ref, got, want)
		}
	}
	if err := os.Remove(filepath.Join(common, "packed-refs")); err != nil {
		t.Fatal(err)
	}
	if !refAbsent(root, "refs/heads/packed") {
		t.Error("AC-4 with no packed-refs, a ref with no file is not absent")
	}
	if err := os.Mkdir(filepath.Join(common, "packed-refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if refAbsent(root, "refs/heads/packed") {
		t.Error("AC-4 a packed-refs that cannot be read says a ref is absent, which is git's to say")
	}
	if err := os.Remove(filepath.Join(common, "packed-refs")); err != nil {
		t.Fatal(err)
	}
	us129Write(t, common, "packed-refs", strings.Repeat("y", 2<<20)+"\n")
	if refAbsent(root, "refs/heads/packed") {
		t.Error("AC-4 a packed-refs whose line is longer than the scan takes says a ref is absent")
	}
	if err := os.Mkdir(filepath.Join(common, "reftable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if refAbsent(root, "refs/heads/missing") {
		t.Error("AC-4 in a reftable store a ref is said to be absent, which only git can say")
	}
}

// A reftable repository, made by git, is read through its own processes
// alike with and without the memo.
func TestAC4AReftableRepositoryAnswersAsWithNoMemo(t *testing.T) {
	root := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init", "--quiet", "--ref-format=reftable", "--initial-branch=main", root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("this git makes no reftable repository: %v\n%s", err, out)
	}
	us129Write(t, root, "a.txt", "a\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "one")
	ctx := WithMemo(context.Background())
	defer Close(ctx)
	for _, rev := range []string{"refs/heads/main", "refs/heads/missing"} {
		want, wantErr := Resolve(context.Background(), root, rev)
		got, err := Resolve(ctx, root, rev)
		if got != want || (err == nil) != (wantErr == nil) {
			t.Errorf("AC-4 Resolve %s in a reftable repository with the memo is %q (%v), with none %q (%v)", rev, got, err, want, wantErr)
		}
	}
	for _, b := range []string{"main", "missing"} {
		want, wantErr := BranchTip(context.Background(), root, b)
		got, err := BranchTip(ctx, root, b)
		if got != want || (err == nil) != (wantErr == nil) {
			t.Errorf("AC-4 BranchTip %s in a reftable repository with the memo is %q (%v), with none %q (%v)", b, got, err, want, wantErr)
		}
	}
	head, err := Head(ctx, root)
	want := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	if err != nil || head != want {
		t.Errorf("AC-4 Head in a reftable repository with the memo is %q (%v), want %s", head, err, want)
	}
}

// HEAD as git itself leaves it -- on a branch, detached, on a branch with no
// commits, on a tag, on a branch whose commit git does not have -- reads alike
// through the session and through Head's own process.
func TestAC4HeadOfEveryHEADGitWritesIsTheSameWithTheSession(t *testing.T) {
	root, first, _ := us129Repo(t)
	us129Run(t, root, nil, "", "tag", "-a", "-m", "t", "v1")
	headPath := filepath.Join(root, ".git", "HEAD")
	us129Write(t, root, ".git/refs/heads/broken", strings.Repeat("5", len(first))+"\n")
	for _, body := range []string{"ref: refs/heads/main\n", first + "\n", "ref: refs/heads/unborn\n", "ref: refs/tags/v1\n",
		"ref: refs/heads/broken\n", "ref: refs/remotes/origin/main\n", strings.ToUpper(first) + "\n", "garbage\n"} {
		if err := os.WriteFile(headPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		want, wantErr := Head(context.Background(), root)
		wantBranch, wantBranchErr := Branch(context.Background(), root)
		ctx := WithMemo(context.Background())
		got, err := Head(ctx, root)
		branch, branchErr := Branch(ctx, root)
		Close(ctx)
		if got != want || fmt.Sprint(err) != fmt.Sprint(wantErr) || branch != wantBranch || fmt.Sprint(branchErr) != fmt.Sprint(wantBranchErr) {
			t.Errorf("AC-4 with HEAD %q the session reads Head %q (%v), Branch %q (%v); its own process %q (%v), %q (%v)",
				body, got, err, branch, branchErr, want, wantErr, wantBranch, wantBranchErr)
		}
	}
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := WithMemo(context.Background())
	defer Close(ctx)
	wt := filepath.Join(t.TempDir(), "wt")
	for _, branch := range []string{"broken", "main", "fresh"} {
		plain := AddWorktree(context.Background(), root, wt+"-plain-"+branch, branch, "main")
		memo := AddWorktree(ctx, root, wt+"-memo-"+branch, branch+"-memo", "main")
		if branch == "fresh" {
			if plain != nil || memo != nil {
				t.Errorf("AC-4 a new branch's worktree: with the session %v, without %v", memo, plain)
			}
			continue
		}
		again := AddWorktree(ctx, root, wt+"-again-"+branch, branch, "main")
		if fmt.Sprint(errors.Unwrap(again)) != fmt.Sprint(errors.Unwrap(plain)) || again == nil {
			t.Errorf("AC-4 a worktree on the branch %s that is there: with the session %v, without %v", branch, again, plain)
		}
	}
}
