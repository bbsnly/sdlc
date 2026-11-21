package gitx

import (
	"context"
	"slices"
	"strings"
)

// ChangedSince is every path checkout has changed since base: what its
// commits from base to head change, and what git status lists on top of
// them, staged or not, deleted, and untracked at any depth. A file that moved
// is there under both names, in the commits and in the index, so that a file
// moved out from under a path is still a change to that path (US-133, THREATS
// T3). An empty base, or one that is head, is the uncommitted work alone.
// Sorted, each path once.
func ChangedSince(ctx context.Context, checkout, base, head string) ([]string, error) {
	var changed []string
	if base != "" && base != head {
		committed, err := ChangedBetween(ctx, checkout, base, head)
		if err != nil {
			return nil, err
		}
		changed = committed
	}
	touched, err := Touched(ctx, checkout)
	if err != nil {
		return nil, err
	}
	changed = append(changed, touched...)
	slices.Sort(changed)
	return slices.Compact(changed), nil
}

// Touched is every path git status lists in checkout, untracked files
// included, with both names of an entry staged as a rename or a copy.
// Uncommitted keeps only the new name, which is all its callers need. `git
// status --no-renames` would give both as two entries, and needs git 2.18,
// one release past the floor.
func Touched(ctx context.Context, checkout string) ([]string, error) {
	out, err := sealed(ctx, checkout, "status", "--porcelain", "-z", "--untracked-files=all", argIgnoreSubmodNone)
	if err != nil {
		return nil, err
	}
	return statusPathsBothNames(out), nil
}

// statusPathsBothNames is statusPaths with the name a rename or a copy had
// kept beside the one it has.
func statusPathsBothNames(out []byte) []string {
	return statusNames(out, true)
}

// statusNames is the paths in git status's porcelain output, written with -z,
// with the name a rename or a copy had where withOld. Either column can carry
// the rename: git status reports one in the worktree column, " R new NUL old",
// for a file moved and then added with git add -N. Read by the index column
// alone, the old name came out as an entry of its own, three characters short,
// so a listed file moved away that way paused nothing (US-133, code-reviewer
// F-3, security F-1), and a reviewer a path brings back was not brought back
// (verifier F-4). statusPaths and Changes read the output here for that reason.
func statusNames(out []byte, withOld bool) []string {
	var paths []string
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		paths = append(paths, f[3:])
		if strings.ContainsAny(f[:2], "RC") && i+1 < len(fields) {
			i++
			if withOld && fields[i] != "" {
				paths = append(paths, fields[i])
			}
		}
	}
	return paths
}
