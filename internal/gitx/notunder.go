package gitx

import (
	"context"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// IsFullHash reports whether s is a full object name, 40 or 64 lower-case hex
// digits: the only form NotUnder takes.
func IsFullHash(s string) bool { return fullHash(s) }

// NotUnder reports, for each commit, whether since's history lacks it: true for
// a commit since does not hold, or one git does not have as a commit at all,
// which no history holds. It is two git calls whatever the number of commits,
// where IsAncestor is one for each: `sdlc status` asks it of every committed
// story that holds a decision, on every run (perf F-1 of the design review).
//
// The first, cat-file --batch-check, says which commits git has. One that is
// missing, or names an object that is not a commit, is answered here and never
// reaches the second, so that one lost commit does not fail the answer for
// every other (perf F-2). The second, rev-list --stdin, lists what the known
// commits reach and since does not. Both read standard input, not argv, so
// that Windows' limit on a command line does not bound the number of commits;
// and every value is held to a full object name first, so that nothing a
// record holds reaches git as an option or a name it resolves.
func NotUnder(ctx context.Context, root, since string, commits []string) (map[string]bool, error) {
	if err := fullHashes(append([]string{since}, commits...)...); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(commits))
	if len(commits) == 0 {
		return out, nil
	}
	checked, err := gitInput(ctx, root, nil, []byte(strings.Join(commits, "\n")+"\n"),
		"cat-file", "--batch-check=%(objectname) %(objecttype)")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(checked), "\n"), "\n")
	if len(lines) != len(commits) {
		return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git cat-file did not answer for every commit it was given",
			msgGitSaid+firstLine(string(checked)))
	}
	var known []string
	for i, c := range commits {
		if fields := strings.Fields(lines[i]); len(fields) == 2 && fields[0] == c && fields[1] == typeCommit {
			known = append(known, c)
			continue
		}
		out[c] = true
	}
	if len(known) == 0 {
		return out, nil
	}
	listed, err := gitInput(ctx, root, nil, []byte(strings.Join(known, "\n")+"\n^"+since+"\n"), "rev-list", "--stdin")
	if err != nil {
		return nil, err
	}
	reached := map[string]bool{}
	for line := range strings.SplitSeq(string(listed), "\n") {
		reached[strings.TrimSpace(line)] = true
	}
	for _, c := range known {
		out[c] = reached[c]
	}
	return out, nil
}
