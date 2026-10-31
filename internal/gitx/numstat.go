package gitx

import (
	"context"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// FileStat is one file's share of a change: the lines added and removed, or
// none for a file git reads as binary.
type FileStat struct {
	Path           string
	Added, Removed int
	Binary         bool
}

// git's words for a diff between two trees, as every reading of one here
// gives them.
const (
	argDiffTree   = "diff-tree"
	argNoExtDiff  = "--no-ext-diff"
	argNoTextconv = "--no-textconv"
)

// Numstat is the change from base's tree to end's, file by file, with the
// paths in leftOut, slash paths from the root, left out, and everything under
// a directory among them: the change the landing fingerprints, measured
// (US-112, AC-10). Paths are as git names them, slash-separated, so that they
// can be matched against the freeze's list. A commit measured against itself
// is no files.
//
// A failure is the runner's: its one-line text is the summary, the code and
// git's exit status. Git's own words, which can name the repository, sit in
// the error's detail, which that text leaves out.
func Numstat(ctx context.Context, root, base, end string, leftOut ...string) ([]FileStat, error) {
	if err := fullHashes(base, end); err != nil {
		return nil, err
	}
	// -z writes each path as it is, so core.quotepath has nothing to quote.
	args := []string{argDiffTree, "-r", "--numstat", "-z", argNoRenames, argNoExtDiff, argNoTextconv,
		base + "^{commit}", end + "^{commit}", "--"}
	for _, p := range leftOut {
		args = append(args, ":(top,exclude,literal)"+p)
	}
	out, err := git(ctx, root, nil, args...)
	if err != nil {
		return nil, err
	}
	return parseNumstat(string(out))
}

// parseNumstat reads diff-tree's -z numstat: for each file, "added TAB removed
// TAB path NUL", with "-" for both counts where git reads the file as binary.
func parseNumstat(out string) ([]FileStat, error) {
	stats := []FileStat{}
	for rec := range strings.SplitSeq(out, "\x00") {
		if rec == "" {
			continue
		}
		// Where the first tab is missing, so is the second.
		added, rest, _ := strings.Cut(rec, "\t")
		removed, path, ok := strings.Cut(rest, "\t")
		f := FileStat{Path: path, Binary: added == "-" && removed == "-"}
		var addedErr, removedErr error
		if !f.Binary {
			f.Added, addedErr = strconv.Atoi(added)
			f.Removed, removedErr = strconv.Atoi(removed)
		}
		if !ok || path == "" || addedErr != nil || removedErr != nil {
			// In words of its own, naming no path.
			return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable, "git diff-tree --numstat printed a line sdlc cannot read",
				"each line is two counts and a path; a git that prints another shape is not one sdlc measures with")
		}
		stats = append(stats, f)
	}
	return stats, nil
}
