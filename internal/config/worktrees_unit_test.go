package config

// US-029.2, unit level: the switch start reads (AC-9), and the two helpers the
// hook's view of every checkout is built from -- a key that each spelling of a
// directory shares, and the git directory a linked worktree's .git file names.
// worktrees_test.go holds the switch to the file's spelling; these hold the
// accessor the rest of the code asks.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAC9WorktreesOnIsOnForAnAbsentOrTrueSettingAndOffOnlyForFalse(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name string
		git  Git
		on   bool
	}{
		{"AC-9 absent", Git{TrunkBranch: "main"}, true},
		{"AC-9 true", Git{TrunkBranch: "main", Worktrees: &yes}, true},
		{"AC-9 false", Git{TrunkBranch: "main", Worktrees: &no}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.git.WorktreesOn(); got != tc.on {
				t.Errorf("WorktreesOn() = %v, want %v", got, tc.on)
			}
		})
	}
	if !Default().Git.WorktreesOn() {
		t.Error("the defaults have story worktrees off, and a project that never names the key has them on")
	}
}

// The switch as start reads it from a checkout: null is no setting, and a
// false in the file is the only thing that turns worktrees off.
func TestAC9WorktreesOnReadsTheSettingTheCheckoutsFileHas(t *testing.T) {
	for _, tc := range []struct {
		name, git string
		on        bool
	}{
		{"AC-9 null", `{"trunk_branch": "main", "worktrees": null}`, true},
		{"AC-9 false", `{"trunk_branch": "main", "worktrees": false}`, false},
		{"AC-9 true", `{"trunk_branch": "main", "worktrees": true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, `{"version": 1, "git": `+tc.git+`}`)
			cfg, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Git.WorktreesOn(); got != tc.on {
				t.Errorf("with git %s, WorktreesOn() = %v, want %v", tc.git, got, tc.on)
			}
		})
	}
}

// The hook keeps every checkout of the repository, and each directory above
// one, in a set: a path spelled through a link, or with a step back in it, is
// the same directory and has to be the same key, or the set misses it.
func TestPathKeyIsOneKeyForEverySpellingOfADirectoryAndNoneForNothing(t *testing.T) {
	if got := PathKey(""); got != "" {
		t.Errorf(`PathKey("") = %q, want no key`, got)
	}
	root := coTempDir(t)
	dir := filepath.Join(root, "checkout")
	other := filepath.Join(root, "another")
	for _, d := range []string{dir, other, filepath.Join(dir, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	key := PathKey(dir)
	if key == "" {
		t.Fatalf("PathKey(%s) is empty", dir)
	}
	for _, spelling := range []string{
		filepath.Join(dir, "sub", ".."),
		dir + string(filepath.Separator),
		filepath.Join(root, ".", "checkout"),
	} {
		if got := PathKey(spelling); got != key {
			t.Errorf("PathKey(%s) = %q, want %q, the key of %s", spelling, got, key, dir)
		}
	}
	if PathKey(other) == key {
		t.Errorf("%s and %s share the key %q", dir, other, key)
	}
	// A directory that is not there yet is keyed by its path.
	if missing := filepath.Join(root, "not-yet"); PathKey(missing) == key || PathKey(missing) == "" {
		t.Errorf("PathKey(%s) = %q", missing, PathKey(missing))
	}

	link := filepath.Join(root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Logf("no symbolic link here (%v), so the link case is not checked", err)
	} else if got := PathKey(link); got != key {
		t.Errorf("PathKey through a link to %s = %q, want %q", dir, got, key)
	}

	// Where the file system does not tell names apart by case, neither does
	// the key.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		upper := filepath.Join(root, "CHECKOUT")
		if got := PathKey(upper); got != key {
			t.Errorf("PathKey(%s) = %q, want %q on %s", upper, got, key, runtime.GOOS)
		}
	}
}

// A linked worktree's .git is a file naming its git directory, absolute or
// relative to the worktree; one that names none is no answer.
func TestGitdirOfIsTheGitDirectoryALinkedWorktreesDotGitFileNames(t *testing.T) {
	main := coRepository(t)
	wt := coWorktree(t, main, "US-001")
	got, ok := GitdirOf(filepath.Join(wt, ".git"))
	if want := filepath.Join(main, ".git", "worktrees", "US-001"); !ok || !SamePath(got, want) {
		t.Errorf("GitdirOf(%s) = %q, %v; want %s", filepath.Join(wt, ".git"), got, ok, want)
	}

	dir := coTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "gits", "one"), 0o755); err != nil {
		t.Fatal(err)
	}
	coWrite(t, filepath.Join(dir, "checkout", ".git"), "gitdir: ../gits/one\n")
	if got, ok := GitdirOf(filepath.Join(dir, "checkout", ".git")); !ok || !SamePath(got, filepath.Join(dir, "gits", "one")) {
		t.Errorf("a relative gitdir read as %q, %v; want %s", got, ok, filepath.Join(dir, "gits", "one"))
	}

	for name, body := range map[string]string{
		"no gitdir line": "not a pointer\n",
		"an empty one":   "gitdir:   \n",
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-"), ".git")
		coWrite(t, path, body)
		if got, ok := GitdirOf(path); ok {
			t.Errorf("a .git file with %s names %q", name, got)
		}
	}
	if got, ok := GitdirOf(filepath.Join(dir, "nothing", ".git")); ok {
		t.Errorf("a .git file that is not there names %q", got)
	}
}
