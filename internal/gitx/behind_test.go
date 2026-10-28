package gitx

import (
	"errors"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// clone makes a working copy of origin at dir, so the test can put its own
// history behind origin's without touching origin itself.
func clone(t *testing.T, origin, dir string) {
	t.Helper()
	run(t, "", "clone", "--quiet", origin, dir)
}

// Behind fetches a branch from elsewhere and counts what HEAD does not have,
// which is how the loop tells a person their trunk has moved on without
// prompting them for a password git has not been given.
func TestBehindCountsCommitsOnTheRemoteThatHeadDoesNotHave(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	origin := repo(t)
	run(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	first := commit(t, origin, "first")
	commit(t, origin, "second")
	commit(t, origin, "third")

	local := t.TempDir()
	clone(t, origin, local)
	run(t, local, "reset", "--hard", "--quiet", first)

	got, err := Behind(ctx, local, origin, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("Behind = %d, want 2", got)
	}

	// Level with the remote, there is nothing left to be behind on.
	run(t, local, "reset", "--hard", "--quiet", "origin/main")
	if got, err := Behind(ctx, local, origin, "main"); err != nil || got != 0 {
		t.Errorf("Behind level with the remote = %d, %v; want 0", got, err)
	}
}

// Behind never prompts: a remote it cannot reach is a failure, not a
// password dialogue nobody would see.
func TestBehindFailsWhenTheRemoteCannotBeFetched(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := repo(t)
	commit(t, root, "first")

	_, err := Behind(ctx, root, t.TempDir(), "main")
	wantUnreadable(t, err, "Behind against a remote that is not a repository")
}

// An unborn branch has no HEAD to count the fetched commits against, and that
// failure is reported rather than misread as being behind by none.
func TestBehindFailsOnAnUnbornBranch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	origin := repo(t)
	run(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	commit(t, origin, "first")

	local := t.TempDir()
	run(t, local, "init", "--quiet")

	_, err := Behind(ctx, local, origin, "main")
	wantUnreadable(t, err, "Behind from a branch with no commit to count from")
}

// wantUnreadable holds a git failure to the one code a person can look up: a
// bare error from os/exec would reach them as text with no fix beside it.
func wantUnreadable(t *testing.T, err error, what string) {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.RepositoryUnreadable {
		t.Fatalf("%s = %v, want code %s", what, err, sdlcerr.RepositoryUnreadable)
	}
}
