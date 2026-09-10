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
	"os/exec"
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
	cmd := exec.CommandContext(ctx, "git",
		"ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root

	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the repository's files could not be listed",
			describeGitFailure(stderr.String())).WithCause(err)
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
