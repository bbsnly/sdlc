// Package gitx runs the few git commands the loop cannot do without.
//
// It is deliberately thin. Everything else in this tool works on the file
// system directly, because a subprocess is slow, is a dependency, and fails in
// ways that are hard to explain to a user. Git earns its place here for one
// thing only: it already knows which files are part of the project and which
// are build output, vendored code or anything else the ignore rules exclude.
// Walking the tree ourselves would mean reimplementing that, badly.
package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Files lists the repository's working tree: everything git tracks, plus the
// files that are not tracked yet and not ignored. New tests are untracked until
// they are committed, so both halves matter.
//
// Paths come back repository-relative and slash-separated, sorted, with no
// duplicates -- the same shape the rest of the loop uses.
func Files(ctx context.Context, root string) ([]string, error) {
	out, err := git(ctx, root, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	files := make([]string, 0, 64)
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		files = append(files, p)
	}
	sort.Strings(files)
	return files, nil
}

// TreeHash is the hash of the working tree as git would store it: every file
// git tracks or would track, by content, in one identifier.
//
// It is computed in a temporary index so that the repository's own index is
// untouched. The loop records this against a review, because an approval of
// code that has since changed is not an approval, and without recording what
// was in front of the reviewer there is no way to tell the two apart.
//
// The loop's own directory is left out. Recording a review writes a review file
// and a gate record, both under .sdlc/, so a hash that included them would make
// every review stale the instant it was filed. What a reviewer of code is
// looking at is the code.
func TreeHash(ctx context.Context, root string) (string, error) {
	// The directory is created, the index file inside it is not: git writes the
	// index itself and refuses to read an empty file as one.
	dir, err := os.MkdirTemp("", "sdlc-index-")
	if err != nil {
		return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the working tree could not be measured",
			"a temporary directory could not be created").WithCause(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(dir, "index"))
	if _, err := git(ctx, root, env, "add", "-A", "--", ".", ":(exclude).sdlc"); err != nil {
		return "", err
	}
	out, err := git(ctx, root, env, "write-tree")
	if err != nil {
		return "", err
	}
	return firstLine(string(out)), nil
}

// Clean reports whether the working tree has nothing uncommitted, which is what
// the commit gate means by "committed".
func Clean(ctx context.Context, root string) (bool, error) {
	out, err := git(ctx, root, nil, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "", nil
}

func git(ctx context.Context, root string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	cmd.Env = env

	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git "+args[0]+" did not finish",
			describeGitFailure(stderr.String())).WithCause(err)
	}
	return out, nil
}

func describeGitFailure(stderr string) string {
	if line := firstLine(stderr); line != "" {
		return "git said: " + line
	}
	return "git ls-files did not finish; it may not be installed, or this may " +
		"not be a repository git can read"
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}
