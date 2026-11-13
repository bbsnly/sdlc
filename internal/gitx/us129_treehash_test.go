package gitx

// US-129 AC-2, for the review subject: TreeHash, with and without overrides,
// is the tree the base's own sequence of git commands writes, for every shape
// a tree can hold and every setting that changes the blob git stores for a
// file. A review is stamped with this hash, so a binary that computes it
// otherwise reads every review stamped by the other as stale (US-129's design
// reviews: architect F-4, red team F-1, security F-6). It passes at the base,
// whose TreeHash is that sequence.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// us129Run is git in dir with env added, and its output; it fails the test
// on an error.
func us129Run(t *testing.T, dir string, env []string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=Test",
		"-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if e := (*exec.ExitError)(nil); errors.As(err, &e) {
			stderr = string(e.Stderr)
		}
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return string(out)
}

// us129ByHand is the tree the base's TreeHash writes, by the commands it
// runs: add -A into a private index, then for each override that the index
// holds, the blob git stores for the content at that path, or the commit's
// entry, or none, and write-tree.
func us129ByHand(t *testing.T, root string, overrides ...Override) string {
	t.Helper()
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(t.TempDir(), "index")}
	us129Run(t, root, env, "", "add", "-A", "--", ".", ":(exclude).sdlc")
	for _, o := range overrides {
		staged := strings.TrimSpace(us129Run(t, root, env, "", "ls-files", "--stage", "--", ":(literal)"+o.Path))
		mode, _, ok := strings.Cut(staged, " ")
		if !ok {
			continue
		}
		if o.At != "" {
			entry := strings.TrimRight(us129Run(t, root, env, "", "ls-tree", "-z", o.At, "--", o.Path), "\x00")
			meta, name, found := strings.Cut(entry, "\t")
			if f := strings.Fields(meta); found && name == o.Path && len(f) == 3 && f[1] == "blob" {
				us129Run(t, root, env, "", "update-index", "--cacheinfo", f[0]+","+f[2]+","+o.Path)
			} else {
				us129Run(t, root, env, "", "update-index", "--force-remove", "--", o.Path)
			}
			continue
		}
		blob := strings.TrimSpace(us129Run(t, root, env, string(o.Content), "hash-object", "-w", "--stdin", "--path="+o.Path))
		us129Run(t, root, env, "", "update-index", "--cacheinfo", mode+","+blob+","+o.Path)
	}
	return strings.TrimSpace(us129Run(t, root, env, "", "write-tree"))
}

// us129Shapes is a repository in a new directory with every shape a tree
// holds: nested directories, an executable, a symbolic link, a submodule's
// entry, and names that sort otherwise as a file than as a directory (a/c
// against a-b and a0). setup runs first, then everything is committed in two
// commits, the first without gone.txt; it returns the first commit.
func us129Shapes(t *testing.T, format string, setup func(root string)) (string, string) {
	t.Helper()
	root := t.TempDir()
	us129Run(t, root, nil, "", "init", "--quiet", "--initial-branch=main", "--object-format="+format)
	if setup != nil {
		setup(root)
	}
	files := map[string]string{
		"nested/deep/er/a.txt": "deep\n", "a/c": "c\n", "a-b": "a-b\n", "a0": "a0\n",
		"run.sh": "#!/bin/sh\necho run\n", "data.json": "{\"a\": 1}\n", "notes.up": "lower case\n",
	}
	for name, body := range files {
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
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	us129Run(t, sub, nil, "", "init", "--quiet", "--initial-branch=main", "--object-format="+format)
	us129Write(t, sub, "inner.txt", "inner\n")
	us129Run(t, sub, nil, "", "add", "-A")
	us129Run(t, sub, nil, "", "commit", "--quiet", "-m", "inner")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "first")
	first := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	us129Write(t, root, "gone.txt", "taken away by an override at the first commit\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "second")
	// A change on disk not committed, as a story's tree has.
	us129Write(t, root, "a-b", "a-b changed on disk\n")
	return root, first
}

func us129Write(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAC2TheReviewSubjectIsTheTreeGitWritesForEveryShape(t *testing.T) {
	crlf := []byte("one\r\ntwo\r\n")
	cases := []struct {
		name, format string
		setup        func(t *testing.T, root string)
		posix        bool
	}{
		{name: "AC-2 every shape", format: "sha1"},
		{name: "AC-2 every shape in a SHA-256 repository", format: "sha256"},
		{name: "AC-2 text=auto for every file", format: "sha1", setup: func(t *testing.T, root string) {
			t.Helper()
			us129Write(t, root, ".gitattributes", "* text=auto\n")
		}},
		{name: "AC-2 eol=crlf for JSON", format: "sha1", setup: func(t *testing.T, root string) {
			t.Helper()
			us129Write(t, root, ".gitattributes", "*.json eol=crlf\n")
		}},
		{name: "AC-2 a clean filter", format: "sha1", posix: true, setup: func(t *testing.T, root string) {
			t.Helper()
			us129Write(t, root, ".gitattributes", "*.up filter=upper\n")
			us129Run(t, root, nil, "", "config", "filter.upper.clean", "tr a-z A-Z")
		}},
		{name: "AC-2 core.autocrlf=true", format: "sha1", setup: func(t *testing.T, root string) {
			t.Helper()
			us129Run(t, root, nil, "", "config", "core.autocrlf", "true")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.posix && runtime.GOOS == "windows" {
				t.Skip("the clean filter is tr, which Windows does not have")
			}
			var setup func(string)
			if c.setup != nil {
				setup = func(root string) { c.setup(t, root) }
			}
			root, first := us129Shapes(t, c.format, setup)
			ctx := context.Background()
			sets := map[string][]Override{
				"no override": nil,
				"overrides of a file, a nested file, JSON and a filtered file, in CRLF": {
					{Path: "a-b", Content: crlf},
					{Path: "nested/deep/er/a.txt", Content: crlf},
					{Path: "data.json", Content: []byte("{\"a\": 2}\r\n")},
					{Path: "notes.up", Content: []byte("still lower case\n")},
				},
				"an override that takes a path away as the first commit has it": {{Path: "gone.txt", At: first}},
				"an override that takes a path as the first commit has it":      {{Path: "a0", At: first}},
				"an override of a path the index does not hold":                 {{Path: "not-there.txt", Content: crlf}},
				"an override of the executable":                                 {{Path: "run.sh", Content: []byte("#!/bin/sh\r\necho changed\r\n")}},
			}
			for set, overrides := range sets {
				got, err := TreeHash(ctx, root, overrides...)
				if err != nil {
					t.Fatalf("%s: %v", set, err)
				}
				if want := us129ByHand(t, root, overrides...); got != want {
					t.Errorf("AC-2 with %s, TreeHash is %s and the tree git writes is %s", set, got, want)
				}
			}
		})
	}
}
