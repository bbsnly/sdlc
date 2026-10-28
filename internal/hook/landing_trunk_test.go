package hook

// US-029.3 (PLAN.md step 13): what the hook hands the classifier about trunk,
// and route R1's one bookkeeper commit.
//
//   - Trunk is the main checkout's configured trunk, as it is on disk there,
//     not the worktree's checked-out copy of the configuration.
//   - IsTrunk is what git reads the name as in the session's checkout: a tag
//     with trunk's short name makes the short name another ref.
//   - reviewsFresh measures the review subject with the root CODEMAP.md as
//     HEAD has it, so the first bookkeeper commit after the retro goes
//     through and a second is refused.

import "testing"

// Red team gaming row 2, for the hook: trunk's name comes from the main
// checkout's configuration on disk. The worktree's copy still says the
// default; the rebase onto the configured trunk goes through, and onto the
// default is refused.
func TestAC9AndQ3TheRebaseIsOntoTheTrunkTheMainCheckoutConfigures(t *testing.T) {
	t.Parallel()
	l := lhFinished(t)
	wtGit(t, l.main, "branch", "develop", "main")
	write(t, l.main, ".sdlc/config.json", `{"version":1,"git":{"trunk_branch":"develop"}}`)
	in := swOpenings(l)[0]
	swAllowed(t, swHeld, []swRow{swBash("Q3 onto the configured trunk", in, "git rebase develop")}, nil)
	swRefused(t, swHeld, []swRow{swBash("Q3 onto the default", in, "git rebase main")}, "", nil)
}

// Red team gaming row 7: a tag named as trunk, made before the command, makes
// the short name the tag; the rebase by it is refused, and by trunk's full
// branch ref it goes through.
func TestAC9AndQ3ARebaseByAShortNameATagAlsoHasIsRefused(t *testing.T) {
	t.Parallel()
	l := lhFinished(t)
	wtGit(t, l.main, "tag", "main", "main")
	in := swOpenings(l)[0]
	swRefused(t, swHeld, []swRow{swBash("Q3 by the short name", in, "git rebase main")}, "", nil)
	swAllowed(t, swHeld, []swRow{swBash("Q3 by the full ref", in, "git rebase refs/heads/main")}, nil)
}

// THREATS T14 and red team: one bookkeeper commit after the retro, not two.
// The first changed CODEMAP.md as HEAD has it, so the reviews no longer
// cover a second.
func TestT14ASecondCodemapCommitAfterTheRetroIsRefused(t *testing.T) {
	t.Parallel()
	l := lhFinished(t)
	write(t, l.wt, "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices.\n")
	wtGit(t, l.wt, "add", "--", "CODEMAP.md")
	wtGit(t, l.wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "docs: map A-1")
	write(t, l.wt, "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices, and more.\n")
	swRefused(t, swHeld, []swRow{
		swBash("R1 a second time", swOpenings(l)[0], "git add CODEMAP.md && git commit -m 'docs: map A-1 again'"),
	}, "", nil)
}
