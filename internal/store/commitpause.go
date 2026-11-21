package store

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// ChangeBase is where a story's change is measured from: the commit its
// branch was cut from wherever head still holds it, unless the story sits on
// a later trunk commit, which head then holds with the cut behind it.
// Anything reachable from that commit is reachable from trunk, so none of the
// story's own commits is ever outside the change, even where trunk came to
// hold some of them first; what is left out is what trunk holds, and the
// landing leaves out the same. Only where a person's rebase took the cut out
// of head is it trunk's merge base with head.
//
// The commit gate, the landing and the pause all measure from here: two
// answers to where the story left trunk would drift apart.
func ChangeBase(ctx context.Context, root, cut, head, trunkTip string) (string, error) {
	if cut != "" {
		held, err := gitx.IsAncestor(ctx, root, cut, head)
		if err != nil {
			return "", err
		}
		if held {
			if on := laterTrunkCommit(ctx, root, cut, head, trunkTip); on != "" {
				return on, nil
			}
			return cut, nil
		}
	}
	base, err := gitx.MergeBase(ctx, root, trunkTip, head)
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the story's change could not be measured",
			"HEAD, "+head[:min(len(head), 12)]+", shares no history with trunk, "+trunkTip[:min(len(trunkTip), 12)])
	}
	return base, nil
}

// laterTrunkCommit is the trunk commit head sits on where it is a later commit
// than the cut, which head holds; "" otherwise, and wherever it cannot be told.
// It asks what the freeze check asks of where head meets trunk (SitsPastCut),
// and one thing more, that the cut is an ancestor of it, so that a trunk gone
// back behind the cut leaves the start where it was. Only sealed calls answer
// it: ancestry a replacement object could forge would start the change and the
// range too late, and hide commits of the story's own.
func laterTrunkCommit(ctx context.Context, root, cut, head, trunkTip string) string {
	on, err := gitx.MergeBase(ctx, root, trunkTip, head)
	if err != nil || !SitsPastCut(on, cut, head) {
		return ""
	}
	if later, err := gitx.IsAncestor(ctx, root, cut, on); err != nil || !later {
		return ""
	}
	return on
}

// SitsPastCut reports whether on is a trunk commit the story's commits sit on,
// and not the commit the branch was cut from or the story's own tip (tip is
// the story's, not trunk's): the one answer to "was the story put onto a later
// commit" that the freeze check, the commit gate and the pause all give.
func SitsPastCut(on, cut, tip string) bool {
	return on != "" && on != cut && on != tip
}

// PauseGates is what pauses the commit for a person, from every configuration
// a story cannot narrow, taken together: this checkout's; the main
// checkout's, where this is a linked worktree; and, in one checkout, the
// configuration committed where the story started, its tiers as well as its
// paths. A git operation that rewrites the file on disk without naming it
// (git apply, cherry-pick -n, merge --squash) does not take that one back
// (US-133, red-team F-3; verifier F-1 found the tiers read from disk alone).
// What did read is returned with the first error of what did not, and no
// failure is read as the defaults, which would be a pause taken away.
func (s *Store) PauseGates(ctx context.Context, record *model.Record) (config.HumanGates, error) {
	gates := s.cfg.HumanGates
	co, err := s.checkout()
	if err != nil {
		return gates, err
	}
	if co.Linked() {
		main, err := s.configAt(co.Main)
		if err != nil {
			return gates, err
		}
		return gates.With(main.HumanGates), nil
	}
	committed, found, err := s.committedConfig(ctx, record)
	if err != nil || !found {
		return gates, err
	}
	return gates.With(committed.HumanGates), nil
}

// committedConfig is the configuration as the commit the story started from
// commits it, for a story in one checkout: the start of its commits, or, where
// the record names none git can read, the commit its iteration began on. A
// record that names neither, from an sdlc before either was kept, has no such
// commit, and HEAD would be no stand-in: it holds whatever the story
// committed. Nor has a project that did not commit the file there. It is the
// one git call the pause adds in one checkout where no path is listed, a
// cat-file asked once per process (FileAt keeps it), on every decision: the
// cost of a pause a rewrite of the file on disk cannot take away.
func (s *Store) committedConfig(ctx context.Context, record *model.Record) (config.Config, bool, error) {
	if record == nil {
		return config.Config{}, false, nil
	}
	var raw []byte
	at, err := "", error(nil)
	for _, c := range []string{record.CommitBase, record.Base} {
		if c == "" || c == gitx.NoCommits {
			continue
		}
		at = c
		if raw, err = gitx.FileAt(ctx, s.root, c, config.File); err == nil || errors.Is(err, fs.ErrNotExist) {
			break
		}
	}
	switch {
	case at == "", errors.Is(err, fs.ErrNotExist):
		return config.Config{}, false, nil
	case err != nil:
		return config.Config{}, false, err
	}
	cfg, err := config.Parse(raw)
	if err != nil {
		return config.Config{}, false, sdlcerr.New(sdlcerr.ConfigInvalid,
			config.File+" as "+at[:min(len(at), 12)]+" commits it could not be read",
			"the story's pause is read from it too").WithCause(err)
	}
	return cfg, true, nil
}

// PauseFor decides whether the story's commit waits for a person, with the
// work as this checkout has it and head as the commit the decision is made on
// ("" reads HEAD). It is an error only where the story cannot be read from the
// backlog. Anything else that does not read, git or a configuration, is in
// the decision's Unread, which pauses.
func (s *Store) PauseFor(ctx context.Context, id string, record *model.Record, head string) (config.CommitPause, error) {
	var unread []string
	note := func(err error) {
		if err != nil && !slices.Contains(unread, err.Error()) {
			unread = append(unread, err.Error())
		}
	}
	gates, err := s.PauseGates(ctx, record)
	note(err)
	tier, err := s.pauseTier(ctx, id, record, gates, note)
	if err != nil {
		return config.CommitPause{}, err
	}
	var changed []string
	if len(gates.PreCommitPausePaths) > 0 {
		changed, err = s.pausePaths(ctx, id, record, head)
		note(err)
	}
	pause := gates.PauseFor(tier, changed)
	pause.Unread = strings.Join(unread, "; ")
	return pause, nil
}

// pauseTier is the story's risk tier, as the backlog the main checkout's
// configuration names has it: the story's own copy of the configuration could
// name a backlog the session wrote. In one checkout the backlog the committed
// configuration names is read as well, and the tier of the two that pauses is
// the one that counts.
func (s *Store) pauseTier(ctx context.Context, id string, record *model.Record, gates config.HumanGates,
	note func(error)) (string, error) {
	from := s
	co, coErr := s.checkout()
	found := coErr == nil
	linked := found && co.Linked()
	if linked {
		cfg, err := s.configAt(co.Main)
		note(err)
		if err == nil {
			from = s.atWith(co.Main, cfg)
		}
	}
	story, _, err := from.Story(id)
	switch {
	case err != nil:
		return "", err
	case !found || linked || gates.PausesBeforeCommit(story.RiskTier):
		return story.RiskTier, nil
	}
	if other := s.committedTier(ctx, id, record, note); gates.PausesBeforeCommit(other) {
		return other, nil
	}
	return story.RiskTier, nil
}

// committedTier is the story's tier in the backlog the configuration committed
// where the story started names, where that is another file than the one this
// checkout's names, and "" otherwise. What does not read is noted, and
// PauseGates has noted the committed configuration itself.
func (s *Store) committedTier(ctx context.Context, id string, record *model.Record, note func(error)) string {
	committed, found, err := s.committedConfig(ctx, record)
	if err != nil || !found || committed.BacklogPath(s.root) == s.cfg.BacklogPath(s.root) {
		return ""
	}
	story, _, err := s.atWith(s.root, committed).Story(id)
	if err != nil {
		note(err)
		return ""
	}
	return story.RiskTier
}

// pausePaths is every path the story has changed, where this checkout has
// it. In the story's own worktree that is from where its branch left trunk
// (ChangeBase), its commits from before code review included. In one
// checkout nothing is committed before code review passes, and after it the
// story's commits start where the record says; a record that does not say
// cannot be measured.
func (s *Store) pausePaths(ctx context.Context, id string, record *model.Record, head string) ([]string, error) {
	if head == "" {
		var err error
		if head, err = gitx.Head(ctx, s.root); err != nil {
			return nil, err
		}
	}
	base := head
	var err error
	switch sid, own := s.StoryWorktreeOf(); {
	case own && sid == id:
		if base, err = s.worktreeBase(ctx, record, head); err != nil {
			return nil, err
		}
	case head == gitx.NoCommits || !record.Pass(model.GateCodeReview) && record.CommitBase == "":
	case record.CommitBase == "":
		return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the story's change could not be measured",
			"code review has passed and the record does not say where the story's commits start")
	default:
		base = record.CommitBase
	}
	if base == gitx.NoCommits {
		base = head
	}
	return gitx.ChangedSince(ctx, s.root, base, head)
}

// worktreeBase is where a story in its own worktree left trunk, with trunk as
// the main checkout's configuration names it.
func (s *Store) worktreeBase(ctx context.Context, record *model.Record, head string) (string, error) {
	co, err := s.checkout()
	if err != nil {
		return "", err
	}
	main, err := s.configAt(co.Main)
	if err != nil {
		return "", err
	}
	trunk := strings.TrimSpace(main.Git.TrunkBranch)
	if trunk == "" {
		trunk = config.Default().Git.TrunkBranch
	}
	tip, err := gitx.BranchTip(ctx, s.root, trunk)
	if err != nil {
		return "", err
	}
	return ChangeBase(ctx, s.root, record.Cut, head, tip)
}

// PauseForStory is PauseFor asked where the story's work is, for a caller
// that may run elsewhere: in the story's worktree where it has one and this
// is not it, and here otherwise. measured is false where the decision could
// not be made on the work, and says nothing of a listed path.
func (s *Store) PauseForStory(ctx context.Context, id string, record *model.Record) (pause config.CommitPause, measured bool, err error) {
	at, measurable := s, true
	if sid, own := s.StoryWorktreeOf(); !own || sid != id {
		if wt, err := s.StoryWorktree(id); err == nil && fsx.Exists(filepath.Join(wt, ".git")) {
			if worktree, err := s.At(wt); err == nil {
				at = worktree
			} else {
				measurable = false
			}
		}
	}
	pause, err = at.PauseFor(ctx, id, record, "")
	return pause, measurable && err == nil && pause.Unread == "", err
}
