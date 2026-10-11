package gitx

// US-029.3, one function at a time: the small git reads the landing is made
// of, the checkouts that hold trunk, and what round 3 of PLAN.md added to
// FastForward. landing_test.go holds the fast-forward and the fingerprint
// whole; these hold the rest of the package's new code to what each piece
// promises, so that a piece that is wrong is found here and not only as a
// landing that went wrong.
//
// The functions, as PLAN.md names them:
//
//	IsAncestor(ctx, root, a, b)
//	MergeBase(ctx, root, a, b)
//	HasMerges(ctx, root, base, tip)
//	ChangedUnder(ctx, root, base, tip, dir)
//	FileAt(ctx, root, commit, path)
//	InProgress(ctx, checkout)
//	ResolvesTo(ctx, checkout, name, ref)
//	CheckedOut(ctx, root, branch)
//
// each returning one value and an error. Only that much is held here: what
// each value is read through luSet, luText and luList, so a yes-or-no may be
// a bool or a name, content a string or bytes, and a list of checkouts paths
// or Worktrees. Beside them, Worktree gains Locked and Override gains At.
//
// Every read is sealed (PLAN.md step 2): of the repository's objects, not of
// a graft or a replacement, with no hook, program or configuration the
// environment names, and leaving nothing behind in the temporary directory.
//
// Helpers carry an lu prefix.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// luSet is whether a result says yes: true, a name that is not empty, a list
// that is not.
func luSet(v any) bool {
	if v == nil {
		return false
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Bool:
		return r.Bool()
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return r.Len() > 0
	case reflect.Pointer, reflect.Interface:
		return !r.IsNil()
	default:
		return !r.IsZero()
	}
}

// luText is a result that is content or a name, as text.
func luText(v any) string {
	switch x := v.(type) {
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprint(v)
	}
}

// luList is a result that is a list, each entry as text: a string itself, a
// struct its Path.
func luList(v any) []string {
	r := reflect.ValueOf(v)
	if r.Kind() != reflect.Slice {
		return nil
	}
	var out []string
	for i := range r.Len() {
		e := reflect.Indirect(r.Index(i))
		switch {
		case e.Kind() == reflect.String:
			out = append(out, e.String())
		case e.Kind() == reflect.Struct && e.FieldByName("Path").IsValid():
			out = append(out, e.FieldByName("Path").String())
		default:
			out = append(out, fmt.Sprint(e.Interface()))
		}
	}
	return out
}

// luHas is whether list holds dir, as a path.
func luHas(list []string, dir string) bool {
	return slices.ContainsFunc(list, func(p string) bool {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		return filepath.Clean(p) == filepath.Clean(dir)
	})
}

// luGit runs git in dir with env added and stdin given, and returns what it
// printed, trimmed.
func luGit(t *testing.T, dir string, env []string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// luTry is luGit where git may refuse.
func luTry(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	_ = cmd.Run()
}

// luCommitOf makes a commit holding exactly files, on parent where there is
// one, from git's objects alone: no working tree, so that names differing
// only in case, and names no file system takes, can be in it.
func luCommitOf(t *testing.T, root, parent string, files map[string]string) string {
	t.Helper()
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(lgTempDir(t), "index")}
	for path, body := range files {
		blob := luGit(t, root, nil, body, "hash-object", "-w", "--stdin")
		luGit(t, root, env, "", "update-index", "--add", "--cacheinfo", "100644,"+blob+","+path)
	}
	tree := luGit(t, root, env, "", "write-tree")
	args := []string{"-c", "user.email=t@example.com", "-c", "user.name=Test", "commit-tree", tree, "-m", "commit"}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	return luGit(t, root, nil, "", args...)
}

// luHistory is lgRepo with three more commits on main, each changing
// notes.txt, and every commit from the first.
func luHistory(t *testing.T) (root string, commits []string) {
	t.Helper()
	root, first := lgRepo(t)
	commits = []string{first}
	for i := 1; i <= 3; i++ {
		write(t, root, "notes.txt", fmt.Sprintf("notes %d\n", i))
		commits = append(commits, lgCommit(t, root, fmt.Sprintf("notes %d", i)))
	}
	return root, commits
}

// luForced is a linked worktree, outside the main checkout, with branch
// checked out though another checkout has it too.
func luForced(t *testing.T, root, branch string) string {
	t.Helper()
	dir := filepath.Join(lgTempDir(t), "also-"+strings.ReplaceAll(branch, "/", "-"))
	run(t, root, "worktree", "add", "--quiet", "--force", dir, branch)
	return dir
}

// luGitPath is where git keeps name for the checkout at dir.
func luGitPath(t *testing.T, dir, name string) string {
	t.Helper()
	p := wtGitOut(t, dir, "rev-parse", "--git-path", name)
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return p
}

// luRebasing starts a rebase of the branch dir has checked out that stops
// part-way.
func luRebasing(t *testing.T, dir string) {
	t.Helper()
	luTry(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--exec", "false", "HEAD~1")
	if _, err := os.Stat(luGitPath(t, dir, "rebase-merge")); err != nil {
		t.Fatalf("precondition: no rebase is part-way in %s", dir)
	}
}

// luBisecting starts a bisect in dir, which leaves HEAD detached between
// HEAD~3 and HEAD.
func luBisecting(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "bisect", "start", "HEAD", "HEAD~3")
	if _, ok := luSymbolic(t, dir); ok {
		t.Fatalf("precondition: the bisect in %s left HEAD on a branch", dir)
	}
}

func luSymbolic(t *testing.T, dir string) (string, bool) {
	t.Helper()
	cmd := wtExec(t, dir, "symbolic-ref", "--quiet", "HEAD")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err == nil
}

// ---------------------------------------------------------------- sealed

// Security 5, red team H5: what the landing reads is the repository's
// objects. A graft file, one the environment names, or a replacement --
// under refs/replace or wherever the environment moves them -- that says
// otherwise does not change what is an ancestor, what the merge base is, or
// whether a range holds a merge.
func TestSealedReadsAreOfTheObjectsNotAGraftOrAReplacement(t *testing.T) {
	for _, tc := range []struct {
		name string
		lie  func(t *testing.T, root string, c []string)
	}{
		{"a graft file", func(t *testing.T, root string, c []string) {
			t.Helper()
			write(t, filepath.Dir(luGitPath(t, root, "info/grafts")), "grafts", c[3]+" "+c[1]+" "+c[0]+"\n")
		}},
		{"a graft file the environment names", func(t *testing.T, _ string, c []string) {
			t.Helper()
			file := filepath.Join(lgTempDir(t), "grafts")
			write(t, filepath.Dir(file), "grafts", c[3]+" "+c[1]+" "+c[0]+"\n")
			t.Setenv("GIT_GRAFT_FILE", file)
		}},
		{"a replacement", func(t *testing.T, root string, c []string) {
			t.Helper()
			run(t, root, "replace", "--graft", c[3], c[1], c[0])
		}},
		{"a replacement where the environment moves them", func(t *testing.T, root string, c []string) {
			t.Helper()
			run(t, root, "replace", "--graft", c[3], c[1], c[0])
			for line := range strings.SplitSeq(wtGitOut(t, root, "for-each-ref", "--format=%(refname) %(objectname)", "refs/replace/"), "\n") {
				name, obj, _ := strings.Cut(line, " ")
				run(t, root, "update-ref", "refs/elsewhere/"+strings.TrimPrefix(name, "refs/replace/"), obj)
				run(t, root, "update-ref", "-d", name)
			}
			t.Setenv("GIT_REPLACE_REF_BASE", "refs/elsewhere/")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, c := luHistory(t)
			tc.lie(t, root, c)
			ctx := t.Context()

			if ok, err := IsAncestor(ctx, root, c[2], c[3]); err != nil || !ok {
				t.Errorf("IsAncestor(c2, c3) = %v, %v; want true, as the objects have it", ok, err)
			}
			if got, err := MergeBase(ctx, root, c[2], c[3]); err != nil || luText(got) != c[2] {
				t.Errorf("MergeBase(c2, c3) = %v, %v; want c2 %s", got, err, c[2])
			}
			if got, err := HasMerges(ctx, root, c[2], c[3]); err != nil || luSet(got) {
				t.Errorf("HasMerges(c2, c3) = %v, %v; want no merge, as the objects have it", got, err)
			}
		})
	}
}

// Security 5: configuration handed to git through the environment is not
// the landing's. Hooks named that way run on no write, and a diff program
// named that way runs on no read.
func TestSealedCallsTakeNoConfigurationOrProgramFromTheEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hooks here are shell scripts marked executable, which Windows does not run as such")
	}
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	hooks, marks := lgTempDir(t), lgTempDir(t)
	for _, name := range []string{"post-checkout", "post-index-change", "reference-transaction"} {
		body := "#!/bin/sh\necho ran > '" + filepath.ToSlash(filepath.Join(marks, name)) + "'\n"
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	differ := filepath.Join(hooks, "differ")
	if err := os.WriteFile(differ, []byte("#!/bin/sh\necho ran > '"+filepath.ToSlash(filepath.Join(marks, "differ"))+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_VALUE_0", filepath.ToSlash(hooks))
	t.Setenv("GIT_CONFIG_KEY_1", "diff.external")
	t.Setenv("GIT_CONFIG_VALUE_1", filepath.ToSlash(differ))
	t.Setenv("GIT_EXTERNAL_DIFF", filepath.ToSlash(differ))

	if _, err := Fingerprint(t.Context(), root, from, to); err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if _, err := ChangedUnder(t.Context(), root, from, to, "billing.go"); err != nil {
		t.Fatalf("ChangedUnder: %v", err)
	}
	if err := FastForward(t.Context(), root, "main", from, to); err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	ran, err := os.ReadDir(marks)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ran {
		t.Errorf("%s, named in the environment's configuration, ran", e.Name())
	}
}

// Security note 2: each sealed call's private directory is its own, and gone
// when it returns, whether the call worked or was refused.
func TestSealedCallsLeaveNothingInTheTemporaryDirectory(t *testing.T) {
	root, c := luHistory(t)
	story, to := lgStoryWork(t, root)
	_ = story
	tmp := lgTempDir(t)
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, tmp)
	}
	ctx := t.Context()

	_, _ = IsAncestor(ctx, root, c[0], c[3])
	_, _ = MergeBase(ctx, root, c[0], c[3])
	_, _ = HasMerges(ctx, root, c[0], c[3])
	_, _ = ChangedUnder(ctx, root, c[0], c[3], "notes.txt")
	_, _ = FileAt(ctx, root, c[3], "notes.txt")
	_, _ = InProgress(ctx, root)
	_, _ = ResolvesTo(ctx, root, "main", "refs/heads/main")
	_, _ = CheckedOut(ctx, root, "main")
	if err := FastForward(ctx, root, "main", c[0], to); err == nil {
		t.Fatal("precondition: a fast-forward from a commit trunk is not on went ahead")
	}
	if err := FastForward(ctx, root, "main", c[3], to); err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		t.Errorf("a sealed call left %s in the temporary directory", e.Name())
	}
}

// T10: each small read refuses a value git would read as an option, and a
// commit that is not a full hash, before git sees it.
func TestTheSmallReadsRefuseWhatGitWouldReadAsAnOption(t *testing.T) {
	root, c := luHistory(t)
	ctx := t.Context()
	good := c[3]
	for _, bad := range []string{"", "--all", "-h", "HEAD", "main", good[:12], strings.ToUpper(good), good + "0"} {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			if _, err := IsAncestor(ctx, root, bad, good); err == nil {
				t.Error("IsAncestor took it")
			}
			if _, err := MergeBase(ctx, root, good, bad); err == nil {
				t.Error("MergeBase took it")
			}
			if _, err := HasMerges(ctx, root, bad, good); err == nil {
				t.Error("HasMerges took it")
			}
			if _, err := ChangedUnder(ctx, root, good, bad, ".sdlc"); err == nil {
				t.Error("ChangedUnder took it")
			}
			if _, err := FileAt(ctx, root, bad, "notes.txt"); err == nil {
				t.Error("FileAt took it")
			}
		})
	}
	for _, name := range []string{"", "--all", "-h"} {
		if _, err := ChangedUnder(ctx, root, c[0], good, name); err == nil {
			t.Errorf("ChangedUnder took the directory %q", name)
		}
		if _, err := FileAt(ctx, root, good, name); err == nil {
			t.Errorf("FileAt took the path %q", name)
		}
		if _, err := ResolvesTo(ctx, root, name, "refs/heads/main"); err == nil {
			t.Errorf("ResolvesTo took the name %q", name)
		}
		if _, err := CheckedOut(ctx, root, name); err == nil {
			t.Errorf("CheckedOut took the branch %q", name)
		}
	}
}

// ---------------------------------------------------------------- reads

func TestMergeBaseIsTheNearestCommitBothHold(t *testing.T) {
	root, c := luHistory(t)
	side := luCommitOf(t, root, c[1], map[string]string{"side.txt": "side\n"})
	ctx := t.Context()
	for _, tc := range []struct{ a, b, want string }{
		{c[3], side, c[1]},
		{side, c[3], c[1]},
		{c[2], c[3], c[2]},
		{c[3], c[3], c[3]},
	} {
		got, err := MergeBase(ctx, root, tc.a, tc.b)
		if err != nil || luText(got) != tc.want {
			t.Errorf("MergeBase(%.7s, %.7s) = %v, %v; want %.7s", tc.a, tc.b, got, err, tc.want)
		}
	}
	unrelated := luCommitOf(t, root, "", map[string]string{"other.txt": "other\n"})
	if got, err := MergeBase(ctx, root, c[3], unrelated); err == nil && luSet(got) {
		t.Errorf("MergeBase of unrelated histories = %v, want none", got)
	}
}

// AC-8: a merge commit in base..tip is found, and one before base is not.
func TestHasMergesSaysWhetherTheRangeHoldsAMergeCommit(t *testing.T) {
	root, c := luHistory(t)
	side := luCommitOf(t, root, c[1], map[string]string{"side.txt": "side\n"})
	merge := luGit(t, root, nil, "", "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit-tree", c[3]+"^{tree}", "-p", c[3], "-p", side, "-m", "merge")
	after := luGit(t, root, nil, "", "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit-tree", c[3]+"^{tree}", "-p", merge, "-m", "after the merge")
	ctx := t.Context()
	for _, tc := range []struct {
		name, base, tip string
		want            bool
	}{
		{"a straight line", c[0], c[3], false},
		{"a merge in the range", c[2], after, true},
		{"a merge at the tip", c[3], merge, true},
		{"a merge before the base", merge, after, false},
		{"an empty range", c[3], c[3], false},
	} {
		got, err := HasMerges(ctx, root, tc.base, tc.tip)
		if err != nil || luSet(got) != tc.want {
			t.Errorf("%s: HasMerges = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

// Security B1, B2 and red team D5: what changed at a directory or under it,
// compared case-folded; a file named exactly as the directory counts, and a
// name that only begins the same does not. Names are read whole, whatever
// they hold.
//
// US-055 AC-1: Git for Windows refuses a path holding a control byte or any of
// : < > " | ? * while core.protectNTFS is on, its default there, and
// update-index --cacheinfo stops at the first such path with "error: Invalid
// path" (CI run 36964608101, a quote; US-050 AC-6, a newline). Those names
// are committed only where git takes them, and the rows that read them skip on
// Windows with the reason. The rows that fold case hold none of them, so a
// ChangedUnder that stopped folding case still fails here on every platform,
// and "src/naïve name.go" -- a name git quotes whenever it writes names
// without -z -- keeps "read whole" live on Windows.
func TestChangedUnderIsWhatChangedAtOrUnderAPathCaseFolded(t *testing.T) {
	root, _ := lgRepo(t)
	base := luCommitOf(t, root, "", map[string]string{
		".sdlc/config.json": "{}\n",
		".sdlc/old.json":    "{}\n",
		"user_stories.json": "{\"stories\":[]}\n",
		"src/a.go":          "package a\n",
	})
	tipFiles := map[string]string{
		".sdlc/config.json":      "{\"changed\":true}\n",
		".SDLC/notes.json":       "{}\n",
		".sdlc-other/x":          "x\n",
		"docs/.sdlc/y":           "y\n",
		"user_stories.json":      "{\"stories\":[{}]}\n",
		"User_Stories.JSON":      "{}\n",
		"user_stories.json.orig": "{}\n",
		"src/a.go":               "package a\n",
		"src/naïve name.go":      "package a\n",
	}
	// Only where git takes them: each is read by a row below that skips on
	// Windows, saying why.
	windowsRefuses := map[string]string{
		".Sdlc/state/run\nnext":  "Git for Windows refuses a path holding a newline (update-index: Invalid path)",
		"src/\"quoted\" name.go": "Git for Windows refuses a path holding a double quote (update-index: Invalid path)",
	}
	if runtime.GOOS != "windows" {
		for path := range windowsRefuses {
			tipFiles[path] = "package a\n"
		}
	}
	tip := luCommitOf(t, root, base, tipFiles)
	ctx := t.Context()
	for _, tc := range []struct {
		name, dir string
		want, not []string
	}{
		{"AC-1 .sdlc", ".sdlc",
			[]string{".sdlc/config.json", ".sdlc/old.json", ".SDLC/notes.json"},
			[]string{".sdlc-other/x", "docs/.sdlc/y", "user_stories.json"}},
		{"AC-1 AC-6 .sdlc, a name holding a newline", ".sdlc",
			[]string{".Sdlc/state/run\nnext"},
			[]string{".sdlc-other/x", "docs/.sdlc/y"}},
		{"AC-1 .SDLC", ".SDLC",
			[]string{".sdlc/config.json", ".SDLC/notes.json"},
			nil},
		{"AC-1 user_stories.json", "user_stories.json",
			[]string{"user_stories.json", "User_Stories.JSON"},
			[]string{"user_stories.json.orig", ".sdlc/config.json"}},
		{"AC-1 src, a name git quotes", "src",
			[]string{"src/naïve name.go"},
			[]string{"src/a.go"}},
		{"AC-1 src, a name holding a quote", "src",
			[]string{"src/\"quoted\" name.go"},
			[]string{"src/a.go"}},
		{"AC-1 nowhere", "nowhere", nil, []string{".sdlc/config.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, w := range tc.want {
				if reason, ok := windowsRefuses[w]; ok && runtime.GOOS == "windows" {
					t.Skip(reason)
				}
			}
			got, err := ChangedUnder(ctx, root, base, tip, tc.dir)
			if err != nil {
				t.Fatalf("ChangedUnder: %v", err)
			}
			list := luList(got)
			for _, w := range tc.want {
				if !slices.Contains(list, w) {
					t.Errorf("%q is not among what changed under %s: %q", w, tc.dir, list)
				}
			}
			for _, n := range tc.not {
				if slices.Contains(list, n) {
					t.Errorf("%q is listed as changed under %s", n, tc.dir)
				}
			}
			if len(tc.want) == 0 && len(list) > 0 {
				t.Errorf("ChangedUnder(%s) = %q, want nothing", tc.dir, list)
			}
		})
	}
}

// T5: a file as a commit has it, whatever the working tree holds; a path the
// commit does not have is fs.ErrNotExist, which the landing tells apart from
// a read that failed.
func TestFileAtIsTheFileAsTheCommitHasIt(t *testing.T) {
	root, first := lgRepo(t)
	write(t, root, ".sdlc/config.json", "{\"commands\":{\"test\":\"exit 1\"}}\n")
	committed := lgCommit(t, root, "configuration")
	write(t, root, ".sdlc/config.json", "{\"commands\":{\"test\":\"exit 0\"}}\n")
	ctx := t.Context()

	got, err := FileAt(ctx, root, committed, ".sdlc/config.json")
	if err != nil || luText(got) != "{\"commands\":{\"test\":\"exit 1\"}}\n" {
		t.Errorf("FileAt = %q, %v; want the committed configuration", luText(got), err)
	}
	if _, err := FileAt(ctx, root, first, ".sdlc/config.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("FileAt of a path the commit does not have = %v, want fs.ErrNotExist", err)
	}
	if got, err := FileAt(ctx, root, first, "notes.txt"); err != nil || luText(got) != "notes\n" {
		t.Errorf("FileAt(first, notes.txt) = %q, %v", luText(got), err)
	}
}

// E0055 and E0056: an operation part-way in a checkout -- a rebase, a merge,
// a cherry-pick, a revert, a bisect -- is said, and none is said once it is
// over.
func TestInProgressSaysWhenAnOperationIsPartWay(t *testing.T) {
	conflict := func(t *testing.T) (root, other string, c []string) {
		t.Helper()
		root, c = luHistory(t)
		run(t, root, "switch", "--quiet", "-c", "other", c[2])
		write(t, root, "notes.txt", "the other side\n")
		other = lgCommit(t, root, "the other side")
		run(t, root, "switch", "--quiet", "main")
		return root, other, c
	}
	for _, tc := range []struct {
		name  string
		start func(t *testing.T, root, other string, c []string)
		abort []string
	}{
		{"a rebase", func(t *testing.T, root, _ string, _ []string) { t.Helper(); luRebasing(t, root) }, []string{"rebase", "--abort"}},
		{"a merge", func(t *testing.T, root, other string, _ []string) {
			t.Helper()
			luTry(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "merge", "--no-edit", other)
		}, []string{"merge", "--abort"}},
		{"a cherry-pick", func(t *testing.T, root, other string, _ []string) {
			t.Helper()
			luTry(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "cherry-pick", other)
		}, []string{"cherry-pick", "--abort"}},
		{"a revert", func(t *testing.T, root, _ string, c []string) {
			t.Helper()
			luTry(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "revert", "--no-edit", c[2])
		}, []string{"revert", "--abort"}},
		{"a bisect", func(t *testing.T, root, _ string, _ []string) { t.Helper(); luBisecting(t, root) }, []string{"bisect", "reset"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, other, c := conflict(t)
			ctx := t.Context()
			if got, err := InProgress(ctx, root); err != nil || luSet(got) {
				t.Fatalf("before %s, InProgress = %v, %v; want nothing part-way", tc.name, got, err)
			}
			tc.start(t, root, other, c)
			if got, err := InProgress(ctx, root); err != nil || !luSet(got) {
				t.Errorf("with %s part-way, InProgress = %v, %v", tc.name, got, err)
			}
			run(t, root, tc.abort...)
			if got, err := InProgress(ctx, root); err != nil || luSet(got) {
				t.Errorf("after %s was abandoned, InProgress = %v, %v; want nothing part-way", tc.name, got, err)
			}
		})
	}
}

// Red team gaming row 7: a name is trunk only where git reads it as trunk's
// branch ref. A tag with trunk's short name makes the short name another
// ref, and the full name still is trunk.
func TestResolvesToIsTheRefGitReadsTheNameAs(t *testing.T) {
	root, c := luHistory(t)
	run(t, root, "branch", "other", c[1])
	ctx := t.Context()
	check := func(name string, want bool) {
		t.Helper()
		got, err := ResolvesTo(ctx, root, name, "refs/heads/main")
		if (err == nil && luSet(got)) != want {
			t.Errorf("ResolvesTo(%q, refs/heads/main) = %v, %v; want %v", name, got, err, want)
		}
	}
	check("main", true)
	check("refs/heads/main", true)
	check("heads/main", true)
	check("other", false)
	check("missing", false)
	run(t, root, "tag", "main", c[1])
	check("main", false)
	check("refs/heads/main", true)
}

// PLAN.md step 3: the checkouts that hold trunk are those with it checked
// out, and those detached while a rebase or bisect of it is part-way. A
// checkout on another branch, detached with nothing part-way, or gone, does
// not hold it.
func TestCheckedOutIsEveryCheckoutThatHoldsTheBranch(t *testing.T) {
	root, c := luHistory(t)
	ctx := t.Context()
	list := func() []string {
		t.Helper()
		got, err := CheckedOut(ctx, root, "main")
		if err != nil {
			t.Fatalf("CheckedOut: %v", err)
		}
		return luList(got)
	}
	if got := list(); len(got) != 1 || !luHas(got, root) {
		t.Fatalf("CheckedOut = %q, want the main checkout alone", got)
	}

	forced := luForced(t, root, "main")
	rebasing := luForced(t, root, "main")
	luRebasing(t, rebasing)
	bisecting := luForced(t, root, "main")
	luBisecting(t, bisecting)
	gone := luForced(t, root, "main")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	run(t, root, "branch", "other", c[2])
	elsewhere := luForced(t, root, "other")
	detached := filepath.Join(lgTempDir(t), "detached")
	run(t, root, "worktree", "add", "--quiet", "--detach", detached, "main")

	got := list()
	for _, dir := range []string{root, forced, rebasing, bisecting} {
		if !luHas(got, dir) {
			t.Errorf("%s holds main and is not in %q", dir, got)
		}
	}
	for _, dir := range []string{gone, elsewhere, detached} {
		if luHas(got, dir) {
			t.Errorf("%s does not hold main and is in %q", dir, got)
		}
	}
}

// T8: a locked worktree is said to be, so that the landing keeps it.
func TestWorktreesSaysWhichAreLocked(t *testing.T) {
	root, _ := lgRepo(t)
	locked := lgStory(t, root)
	run(t, root, "worktree", "lock", "--reason", "a person's", locked)
	open := filepath.Join(lgTempDir(t), "open")
	run(t, root, "worktree", "add", "--quiet", "-b", "story/US-002", open, "main")

	all, err := Worktrees(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range all {
		want := luHas([]string{w.Path}, locked)
		if w.Locked != want {
			t.Errorf("%s: Locked = %v, want %v", w.Path, w.Locked, want)
		}
	}
}

// R1 (T14): an override at a commit hashes the path as that commit has it,
// so the review subject can take the bookkeeper's CODEMAP.md as HEAD has it,
// whatever is on disk.
func TestTreeHashWithAnOverrideAtACommitHashesThePathAsThatCommitHasIt(t *testing.T) {
	root, _ := lgRepo(t)
	write(t, root, "CODEMAP.md", "# one\n")
	one := lgCommit(t, root, "map one")
	ctx := t.Context()
	asOne, err := TreeHash(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "CODEMAP.md", "# two\n")
	two := lgCommit(t, root, "map two")
	asTwo, err := TreeHash(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "CODEMAP.md", "# three, on disk only\n")

	for _, tc := range []struct{ at, want string }{{one, asOne}, {two, asTwo}} {
		got, err := TreeHash(ctx, root, Override{Path: "CODEMAP.md", At: tc.at})
		if err != nil || got != tc.want {
			t.Errorf("TreeHash with CODEMAP.md at %.7s = %s, %v; want %s", tc.at, got, err, tc.want)
		}
	}
	if got, _ := TreeHash(ctx, root); got == asTwo {
		t.Error("precondition: the tree on disk hashes as the commit does")
	}
}

// ---------------------------------------------------------------- FastForward, round 3

// Red team O1, security note 3: a checkout that already holds the landing's
// content at a path -- a stop killed between its two writes, or the same
// change made by hand -- is not in the way there, for a changed path or an
// added one, staged or not. The landing takes it in and the checkout ends
// clean at the story's tip.
func TestO1FastForwardTakesInWhatAlreadyHoldsTheTargetContent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		paths []string
		stage bool
	}{
		{"a changed path", []string{"billing.go"}, false},
		{"an added path", []string{"invoice.go"}, false},
		{"both, staged", []string{"billing.go", "invoice.go", "a.txt"}, true},
		{"both, not staged", []string{"billing.go", "invoice.go", "a.txt"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, from := lgRepo(t)
			wt, to := lgStoryWork(t, root)
			for _, p := range tc.paths {
				write(t, root, p, lgRead(t, wt, p))
				if tc.stage {
					run(t, root, "add", "--", p)
				}
			}

			if err := FastForward(t.Context(), root, "main", from, to); err != nil {
				t.Fatalf("FastForward: %v", err)
			}
			if got := lgRev(t, root, "refs/heads/main"); got != to {
				t.Errorf("trunk is at %s, want %s", got, to)
			}
			if got := lgStatus(t, root); got != "" {
				t.Errorf("the main checkout is not clean at the story's tip:\n%s", got)
			}
		})
	}
}

// E0056's own variants: a checkout holding trunk with an operation part-way,
// or rebasing or bisecting trunk, or with its index locked, is refused, named,
// and nothing anywhere is written.
func TestAC6FastForwardRefusesACheckoutPartWayThroughSomethingAndWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   func(t *testing.T, root, trunk string) (named string)
		spike bool
	}{
		{"a merge part-way", func(t *testing.T, root, trunk string) string {
			t.Helper()
			write(t, filepath.Dir(luGitPath(t, root, "MERGE_HEAD")), "MERGE_HEAD", trunk+"\n")
			return filepath.Base(root)
		}, false},
		{"a cherry-pick part-way", func(t *testing.T, root, trunk string) string {
			t.Helper()
			write(t, filepath.Dir(luGitPath(t, root, "CHERRY_PICK_HEAD")), "CHERRY_PICK_HEAD", trunk+"\n")
			return filepath.Base(root)
		}, false},
		{"the index locked", func(t *testing.T, root, _ string) string {
			t.Helper()
			write(t, filepath.Dir(luGitPath(t, root, "index.lock")), "index.lock", "")
			return "index.lock"
		}, false},
		{"another checkout rebasing trunk", func(t *testing.T, root, _ string) string {
			t.Helper()
			dir := luForced(t, root, "main")
			luRebasing(t, dir)
			return filepath.Base(dir)
		}, true},
		{"another checkout bisecting trunk", func(t *testing.T, root, _ string) string {
			t.Helper()
			dir := luForced(t, root, "main")
			luBisecting(t, dir)
			return filepath.Base(dir)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := luHistory(t)
			from := lgRev(t, root, "refs/heads/main")
			_, to := lgStoryWork(t, root)
			if tc.spike {
				run(t, root, "switch", "--quiet", "-c", "spike")
			}
			named := tc.set(t, root, from)
			refs, status := lgRefs(t, root), lgStatus(t, root)
			index := wtGitOut(t, root, "ls-files", "--stage")

			err := FastForward(t.Context(), root, "main", from, to)
			if err == nil {
				t.Fatal("FastForward went ahead")
			}
			lgSays(t, err, named)
			if got := lgRefs(t, root); got != refs {
				t.Errorf("the refused fast-forward moved a ref:\nbefore\n%s\nafter\n%s", refs, got)
			}
			if got := lgStatus(t, root); got != status {
				t.Errorf("the refused fast-forward changed the main checkout:\nbefore\n%s\nafter\n%s", status, got)
			}
			if got := wtGitOut(t, root, "ls-files", "--stage"); got != index {
				t.Error("the refused fast-forward wrote the main checkout's index")
			}
		})
	}
}

// Red team O2: where git refuses in a later checkout, every checkout already
// written is put back, and trunk does not move.
func TestO2AFastForwardRefusedInALaterCheckoutPutsTheEarlierOnesBack(t *testing.T) {
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	later := luForced(t, root, "main")
	write(t, filepath.Dir(luGitPath(t, later, "index.lock")), "index.lock", "")
	billing := lgRead(t, root, "billing.go")

	if err := FastForward(t.Context(), root, "main", from, to); err == nil {
		t.Fatal("FastForward went ahead with a checkout of trunk whose index is locked")
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
	for _, dir := range []string{root, later} {
		if got := lgRead(t, dir, "billing.go"); got != billing {
			t.Errorf("%s holds billing.go as the story has it, left written", dir)
		}
		if got := lgRead(t, dir, "invoice.go"); got != "" {
			t.Errorf("%s holds the story's invoice.go, left written", dir)
		}
	}
	if got := lgStatus(t, root); got != "" {
		t.Errorf("the main checkout is not as it was:\n%s", got)
	}
}

// PLAN.md step 3: an untracked name that differs only in case from one the
// landing adds is in the way where the file system folds case, and not where
// it does not.
func TestAC6FastForwardFindsAnUntrackedNameThatDiffersOnlyInCase(t *testing.T) {
	root, from := lgRepo(t)
	wt := lgStory(t, root)
	write(t, wt, "Invoice.go", "package billing\n")
	to := lgCommit(t, wt, "US-001: Invoice.go")
	write(t, root, "invoice.go", "package billing // a person's own\n")
	_, err := os.Stat(filepath.Join(root, "INVOICE.GO"))
	folds := err == nil

	err = FastForward(t.Context(), root, "main", from, to)
	if folds {
		if !errors.Is(err, ErrWouldOverwrite) {
			t.Fatalf("FastForward = %v on a file system that folds case, want ErrWouldOverwrite", err)
		}
		if got := lgRev(t, root, "refs/heads/main"); got != from {
			t.Errorf("trunk moved to %s", got)
		}
		return
	}
	if err != nil {
		t.Fatalf("FastForward = %v where the file system keeps case apart", err)
	}
	if got := lgRead(t, root, "invoice.go"); got != "package billing // a person's own\n" {
		t.Error("the person's own invoice.go was written over")
	}
}

// PLAN.md step 3: an untracked link where the landing adds a directory is in
// the way, and nothing is written through it.
func TestAC6FastForwardRefusesAnUntrackedLinkInPlaceOfADirectoryItAdds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("making a symbolic link needs a privilege Windows does not give by default")
	}
	root, from := lgRepo(t)
	wt := lgStory(t, root)
	write(t, wt, "docs/guide.md", "# Guide\n")
	to := lgCommit(t, wt, "US-001: a guide")
	outside := lgTempDir(t)
	if err := os.Symlink(outside, filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}

	err := FastForward(t.Context(), root, "main", from, to)
	if !errors.Is(err, ErrWouldOverwrite) {
		t.Fatalf("FastForward = %v, want ErrWouldOverwrite", err)
	}
	lgSays(t, err, "docs")
	if left, _ := os.ReadDir(outside); len(left) != 0 {
		t.Errorf("the landing wrote through the link: %v", left)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
}
