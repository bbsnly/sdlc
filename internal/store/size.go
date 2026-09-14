package store

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/bbsnly/sdlc/internal/gitx"
)

// ChangeSize is how many lines the work changes against the last commit, as
// `git diff --numstat` counts them: the working tree, untracked files included,
// less the loop's own directory and the backlog.
//
// The backlog is left out whole, rather than less its bookkeeping as
// ReviewSubject leaves it. The story is not the change, and a tree with the
// bookkeeping taken out would count every story's status as a line removed.
func (s *Store) ChangeSize(ctx context.Context) (int, error) {
	tree, err := gitx.TreeHash(ctx, s.root)
	if err != nil {
		return 0, err
	}
	var exclude []string
	rel, err := filepath.Rel(s.root, s.cfg.BacklogPath(s.root))
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		exclude = append(exclude, filepath.ToSlash(rel))
	}
	return gitx.ChangedLines(ctx, s.root, tree, exclude...)
}
