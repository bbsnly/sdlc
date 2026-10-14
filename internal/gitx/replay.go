package gitx

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Putting a finished story's commits onto a trunk that moved: the one rebase
// the tool runs itself, how it is abandoned, and what it stopped on. Its
// result is never trusted: the landing holds it to the reviewed change from
// git's objects before anything reaches trunk.

// replaySettings are handed to git outright for the replay, so that nothing
// the repository configures runs in it or shapes what it makes. The
// repository's configuration is a session's to write, and a merge driver named
// there once put a story's line ten lines from where its reviewers saw it and
// reported success. The attributes file is one nobody can have written: an
// attribute nobody committed, read through core.attributesFile, made a
// conflict join itself with the built-in union driver, and so would a tree
// nobody committed that attr.tree names.
var replaySettings = []string{
	"rebase.autoStash=false", "rebase.autoSquash=false", "rebase.updateRefs=false",
	"rebase.rebaseMerges=false", "rerere.enabled=false", "rerere.autoUpdate=false",
	"merge.default=text", "commit.gpgSign=false", "core.editor=true", "sequence.editor=true",
	"core.attributesFile=" + privateDir + "/attributes", "attr.tree=",
}

// Replay makes the commits of the branch checked out in checkout that follow
// upstream again on top of onto, and moves that branch to the result. It is
// the second function here that moves a branch, and it moves only the one
// checked out in checkout. Both commits are full hashes.
//
// Where a commit does not apply it returns an error and leaves the rebase
// part-way, so that Unmerged can name the paths in conflict and AbandonReplay
// put the checkout back. It is git's merge backend, asked for by flag: a git
// older than 2.26 does not know the setting, and its other backend can put a
// hunk in the wrong place with no conflict.
//
// Every merge driver the configuration names, the one with an empty name
// among them, is replaced by one that fails, so that a path whose attribute
// names it stops as a conflict. A name the command line cannot be handed
// unchanged is refused before git runs: one that holds an equals sign, one
// that holds the text this package puts its private directory in place of,
// and one that is not UTF-8 (Unoverridable says why each). That refusal's
// cause is a *DriverNamesError, which tells it from a failure of git's own.
// The names are read from checkout, where the rebase runs, so that both read
// one configuration.
func Replay(ctx context.Context, checkout, onto, upstream string) error {
	if err := fullHashes(onto, upstream); err != nil {
		return err
	}
	drivers, err := MergeDrivers.Keys(ctx, checkout)
	if err != nil {
		return err
	}
	// One listing serves the refusal and the overrides, so no driver can be
	// added between the two.
	if bad := Unoverridable(drivers); len(bad) > 0 {
		return sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the merge driver "+strconv.Quote(bad[0])+" cannot be kept out of a rebase",
			"its name holds an equals sign, "+strconv.Quote(privateDir)+" or a byte that is not UTF-8, so git's "+
				"command line cannot be handed its key as it is, and a merge driver the repository configures is "+
				"never run while a story's commits are put onto trunk").
			WithCause(&DriverNamesError{Kind: MergeDrivers, Keys: bad})
	}
	settings := slices.Clip(replaySettings)
	for _, key := range drivers {
		settings = append(settings, key+"=exit 1")
	}
	_, err = sealedCall(ctx, checkout, settings, nil, nil,
		"rebase", "--merge", "--no-fork-point", "--onto", onto, upstream)
	return err
}

// AbandonReplay abandons the rebase part-way in checkout, which puts its
// branch, index and tracked files back as they were before it began.
func AbandonReplay(ctx context.Context, checkout string) error {
	_, err := sealed(ctx, checkout, "rebase", "--abort")
	return err
}

// Unmerged is every path in checkout that a merge left in conflict, as git
// spells it, and none where nothing is in conflict.
func Unmerged(ctx context.Context, checkout string) ([]string, error) {
	out, err := sealed(ctx, checkout, "diff", "--name-only", "-z", argNoRenames, "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	return nulSeparated(out), nil
}

// nulSeparated is the names in what git wrote with -z.
func nulSeparated(out []byte) []string {
	var names []string
	for name := range strings.SplitSeq(string(out), "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
