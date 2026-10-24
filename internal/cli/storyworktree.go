package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// startRounds is how many times start decides what to do before it gives up
// on a backlog that keeps changing under it: once, and once more after
// something changed while it made the story's worktree outside the lock.
// Going round for as long as another start keeps winning would never end.
const startRounds = 2

// retryLine is what start says when it goes round again.
const retryLine = "sdlc: the backlog changed while start was making the worktree; deciding again"

// startInWorktree is start with git.worktrees on. A story picked up again is
// picked up where it is held, as startLocked always has. A new one begins in a
// worktree of its own, made and smoke-checked before start takes the lock,
// because the smoke check is the project's own command and can run for longer
// than a lock is trusted to be held; start then decides again under the lock.
func startInWorktree(cmd *cobra.Command, asked string, said *startSaid) error {
	for round := 1; ; round++ {
		again, err := startRound(cmd, asked, said)
		if err != nil || !again {
			return err
		}
		if round == startRounds {
			return sdlcerr.New(sdlcerr.WorktreeNotMade,
				"the story's worktree could not be made: what start was to begin kept changing",
				fmt.Sprintf("%d times, before start could record the story, another start or an edit to the backlog "+
					"changed what it was to begin, and start made nothing that it left behind", round)).
				WithFix(`run "sdlc start" again`)
		}
		slog.Debug("start decides again", "round", round+1)
	}
}

// startRound decides what start is to do and does it, and reports whether it
// has to go round again because what it decided changed under it.
func startRound(cmd *cobra.Command, asked string, said *startSaid) (again bool, err error) {
	s, _, err := openStore(cmd)
	if err != nil {
		return false, err
	}
	plan, err := decideStart(s, asked, s.CommandSession())
	said.passed = plan.passedOver
	if err != nil {
		return false, err
	}
	if !plan.fresh {
		// Picked up again where it is held, and nothing is made. A story the
		// lock finds new instead goes round, to have its worktree made
		// outside it.
		return startLocked(cmd, asked, false, said)
	}
	return beginInWorktree(cmd, s, asked, plan, said)
}

// beginInWorktree makes the story's worktree, at .sdlc/worktrees/<ID> of the
// main checkout on a branch story/<ID> cut from trunk, checks it, and records
// the story as held there. Whatever refuses the start leaves nothing it made.
//
// Only trunk is held to anything. The main checkout's branch and what it has
// not committed are the person's, and the worktree is cut from trunk, not from
// them.
func beginInWorktree(cmd *cobra.Command, s *store.Store, asked string, plan startPlan, said *startSaid) (bool, error) {
	ctx := cmd.Context()
	trunk := trunkOf(s.Config())
	if err := checkTrunkFirst(cmd, s, trunk, plan); err != nil {
		return false, err
	}
	co, err := s.Checkout()
	if err != nil {
		return false, err
	}
	id, branch := plan.id, store.StoryBranch(plan.id)
	wt, err := s.StoryWorktree(id)
	if err != nil {
		return false, err
	}
	// Held until the story is recorded or what was made is taken away again,
	// on every way out of here, a panic included. A start that had to wait
	// for another one making this story's worktree finds the story held by
	// now, and goes round to begin another or pick this one up there.
	g, err := s.LockStory(id)
	if err != nil {
		return false, err
	}
	defer g.Release()
	if changed, err := decidedAgain(cmd, asked, plan); err != nil || changed {
		return changed, err
	}
	if err := store.CheckWorktreesIgnored(co.Main); err != nil {
		return false, err
	}
	if changed, err := addWorktree(cmd, asked, plan, co.Main, wt, trunk); changed || err != nil {
		return changed, err
	}
	slog.Debug("start made the story's worktree", "story", id, "worktree", wt, "branch", branch, "trunk", trunk)

	// Run whatever happened to the command: a start interrupted mid-smoke
	// still takes away what it made.
	undo := func() error {
		err := gitx.RemoveWorktree(context.WithoutCancel(ctx), co.Main, wt, branch)
		if err != nil {
			slog.Debug("start could not take the story's worktree away again", "story", id, "worktree", wt, "err", err)
		} else {
			slog.Debug("start took the story's worktree away again", "story", id, "worktree", wt)
		}
		return err
	}
	if err := checkNewWorktree(cmd, s, co.Main, wt, id, trunk, undo); err != nil {
		return false, err
	}
	return recordInWorktree(cmd, asked, plan, wt, undo, said)
}

// checkTrunkFirst holds start to trunk before anything is made: it is checked
// against its remote where the project has one, and only after that does a
// story that cannot begin say why.
func checkTrunkFirst(cmd *cobra.Command, s *store.Store, trunk string, plan startPlan) error {
	if s.Config().Git.Remote {
		if err := checkRemote(cmd, s.Root(), trunk); err != nil {
			return behindInWorktrees(err, trunk)
		}
	}
	// After trunk, as it always was, and before anything is made.
	return plan.refused
}

// decidedAgain is decidedElsewise, with the line that says start goes round
// where it did.
func decidedAgain(cmd *cobra.Command, asked string, plan startPlan) (bool, error) {
	changed, err := decidedElsewise(cmd, asked, plan)
	if changed && err == nil {
		fmt.Fprintln(cmd.ErrOrStderr(), retryLine)
	}
	return changed, err
}

// addWorktree makes the story's worktree. It reports changed where start would
// now do something else, and goes round.
func addWorktree(cmd *cobra.Command, asked string, plan startPlan, main, wt, trunk string) (changed bool, err error) {
	ctx, id := cmd.Context(), plan.id
	err = gitx.AddWorktree(ctx, main, wt, store.StoryBranch(id), trunk)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, gitx.ErrInTheWay) {
		// A start that got here first has made it, and may have claimed
		// the story by now; what is in the way is then that start's.
		changed, err := decidedElsewise(cmd, asked, plan)
		if err != nil {
			return false, err
		}
		if changed {
			fmt.Fprintln(cmd.ErrOrStderr(), retryLine)
			return true, nil
		}
	}
	return false, notMadeFor(ctx, err, main, wt, id, trunk)
}

// checkNewWorktree holds the new worktree to what start promises of it: it is
// ignored where it has to be, and the project's smoke check passes in it.
// Whatever refuses leaves nothing the start made.
func checkNewWorktree(cmd *cobra.Command, s *store.Store, main, wt, id, trunk string, undo func() error) error {
	if err := store.EnsureWorktreesIgnored(main); err != nil {
		return rolledBack(err, id, undo())
	}
	if err := s.EnsureStateIgnored(wt); err != nil {
		return rolledBack(err, id, undo())
	}
	// The worktree's own configuration, which is trunk's as committed: the
	// loop in the worktree reads it, so the smoke check is the one it names.
	wtCfg, err := config.Load(wt)
	if err != nil {
		return rolledBack(noConfigOnTrunk(err, trunk, contractToCommit(cmd.Context(), main, trunk)...), id, undo())
	}
	// git's own variables, where they are set, name wherever start was run
	// from, and would point the project's command at that repository instead.
	failed, output, err := smoke(cmd, wt, wtCfg.Commands["smoke"], gitx.Environ())
	if err != nil {
		return rolledBack(err, id, undo())
	}
	if failed != nil {
		slog.Debug("commands.smoke failed in the story's new worktree", "story", id, "worktree", wt, "err", failed)
		return smokeFailedInWorktree(wt, id, failed, output, undo())
	}
	slog.Debug("commands.smoke passed in the story's new worktree", "story", id, "worktree", wt)
	return nil
}

// recordInWorktree records the story as begun in its new worktree, under the
// lock, if start still decides the same there; and otherwise takes the
// worktree away again and goes round.
func recordInWorktree(cmd *cobra.Command, asked string, plan startPlan, wt string, undo func() error,
	said *startSaid) (bool, error) {
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return false, rolledBack(err, plan.id, undo())
	}
	defer unlock()

	now, err := decideStart(s, asked, s.CommandSession())
	said.passed = now.passedOver
	if err != nil {
		// Not a change, as decidedElsewise says.
		return false, rolledBack(err, plan.id, undo())
	}
	if !now.fresh || now.id != plan.id || now.refused != nil {
		if left := undo(); left != nil {
			return false, rolledBack(sdlcerr.New(sdlcerr.WorktreeNotMade,
				"the story's worktree could not be made: the backlog changed while start was making it",
				"another start, or an edit to the backlog, changed what start was to begin before it could record "+
					"the story").WithFix(`run "sdlc start" again`), plan.id, left)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), retryLine)
		return true, nil
	}

	id := plan.id
	target, err := s.At(wt)
	if err != nil {
		return false, rolledBack(err, id, undo())
	}
	// The worktree's active first. A start killed after it leaves the story
	// held by its worktree, by its active alone, which the next start there
	// picks up; one killed before it leaves a worktree nothing holds and with
	// no commits, which the next start and doctor both describe.
	if err := target.SetActive(id); err != nil {
		return false, rolledBack(err, id, undo())
	}
	session, err := target.BindSession(false)
	if err != nil {
		return false, leftBegun(err, id, false)
	}
	if err := s.SetStoryStatus(id, model.StatusInProgress); err != nil {
		return false, leftBegun(err, id, false)
	}
	record, err := s.Record(id)
	if err != nil {
		return false, leftBegun(err, id, true)
	}
	released := releasedSinceStart(record)
	// The commit the branch was cut from, which the story is measured from,
	// and which its change is measured from at the commit gate however the
	// branch is reworked. The story's list of its own commits starts there too,
	// over any list a story started again had: that one is of another branch.
	if head, err := gitx.Head(cmd.Context(), wt); err == nil {
		record.Base = head
		record.Cut = head
		setForks(record, []model.Fork{{From: head}})
	}
	// A new cut is a new attempt: sections stored under the last one were
	// held to a map this worktree never had.
	withdrawMap(s, record, id, "the story was begun afresh, in a new worktree")
	record.Append("loop_start", "iteration started", s.Now())
	if err := s.SaveRecord(record); err != nil {
		return false, leftBegun(err, id, true)
	}
	if err := target.Claim(id); err != nil {
		return false, leftBegun(err, id, true)
	}
	if released {
		noteRelease(cmd.ErrOrStderr(), target, id)
	}
	said.went = id
	return false, reportStart(cmd, target, id, false, session, true)
}

// decidedElsewise takes the lock and decides again, and reports whether start
// would now do something other than begin plan's story in a new worktree. What
// refuses deciding is start's answer as it stands: taken for a change, start
// said the backlog had changed, went round, and only then refused.
func decidedElsewise(cmd *cobra.Command, asked string, plan startPlan) (bool, error) {
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return false, err
	}
	defer unlock()
	now, err := decideStart(s, asked, s.CommandSession())
	if err != nil {
		return false, err
	}
	return !now.fresh || now.id != plan.id, nil
}

// trunkOf is the branch the configuration names as trunk.
func trunkOf(cfg config.Config) string {
	if trunk := strings.TrimSpace(cfg.Git.TrunkBranch); trunk != "" {
		return trunk
	}
	return config.Default().Git.TrunkBranch
}

// worktreeIn is how a fix names the story's worktree: from the main checkout,
// where the command runs, and never by an absolute path, which a person pastes
// into a shell and which can hold anything.
func worktreeIn(id string) string { return config.Dir + "/worktrees/" + id }

// lookAt is the command, run in the main checkout, that lists the commits the
// story's worktree has on its HEAD that trunk does not. Like every command a
// fix gives, it is built from the id and trunk, never from a branch git lists.
func lookAt(id, trunk string) string {
	return `"git -C ` + worktreeIn(id) + ` log --oneline ` + shellWord(trunk) + `..HEAD"`
}

// keepOn is the command that keeps what the story's worktree has on its HEAD
// on a new branch, which a person names.
func keepOn(id string) string { return `"git -C ` + worktreeIn(id) + ` branch <new name>"` }

// lostWithIt is why a worktree on no branch is never offered for removal, said
// of its n commits, or of commits git could not count where n is 0: git removes
// a clean one without --force, and its HEAD's reflog goes with it.
func lostWithIt(n int) string {
	if n == 1 {
		return "a commit on no branch is lost when its worktree is removed"
	}
	return "commits on no branch are lost when their worktree is removed"
}

// shellWord is s as a person can paste it into a shell and have it read back
// unchanged: bare where it is letters, digits and "-._/+@" alone, and otherwise
// in single quotes, with each ' in it closed, escaped and opened again. Double
// quotes were not enough, since a shell still expands what is in them and git
// takes "$(" in a branch name. A character that does not print is shown as ?:
// some move a terminal's cursor, or reverse the text around them.
func shellWord(s string) string {
	shown := strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return '?'
	}, s)
	if shown != "" && strings.Trim(shown, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._/+@") == "" {
		return shown
	}
	return "'" + strings.ReplaceAll(shown, "'", `'\''`) + "'"
}

// assistantStops ends every fix that removes or moves somebody's work.
const assistantStops = "an assistant stops and tells the person, and removes nothing"

// notMadeFor is AddWorktree's refusal with a fix for its cause. The catalogue's
// fix stays for a cause nothing here describes.
func notMadeFor(ctx context.Context, err error, main, wt, id, trunk string) error {
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		return err
	}
	switch {
	case errors.Is(err, gitx.ErrInTheWay):
		return e.WithFix(inTheWayFix(ctx, main, wt, id, trunk))
	case errors.Is(err, gitx.ErrBranchName) && (trunk == "" || strings.HasPrefix(trunk, "-")):
		return e.WithFix("set git.trunk_branch in .sdlc/config.json to the name of the trunk branch, then start " +
			"again; an assistant stops and tells the person")
	case errors.Is(err, gitx.ErrBranchName):
		return e.WithFix("rename the story so that " + quote(store.StoryBranch(id)) + ` is a name git takes for a ` +
			`branch -- "git check-ref-format --branch" says whether it is -- or set git.worktrees to false in ` +
			".sdlc/config.json; an assistant stops and tells the person")
	case errors.Is(err, gitx.ErrNoTrunk):
		return e.WithFix("in the main checkout, create the branch git.trunk_branch names, " + quote(trunk) +
			", with a commit on it, or set git.trunk_branch to the branch that is trunk, then start again; " +
			"an assistant stops and tells the person")
	case errors.Is(err, gitx.ErrNotADirectory):
		return e.WithFix("move the link or the file named above out of the way, so that " + worktreeIn(id) +
			" can be made as a directory in the main checkout, then start again; " + assistantStops)
	}
	return err
}

// thenClause joins the steps of a fix, in the order a person takes them.
const thenClause = "; then "

// inTheWayFix says what to do about a worktree, a branch or a directory
// already where the story's worktree goes. What is there may be a start still
// running, or somebody's work, so the fix says to look before anything goes,
// says what would be lost, and never forces anything away. A worktree on no
// branch is counted from its HEAD and, unless that has nothing trunk does not,
// gets no command that removes it: its removal had been given, and lost the
// commits only that HEAD had.
func inTheWayFix(ctx context.Context, main, wt, id, trunk string) string {
	w := &inTheWay{main: main, wt: wt, id: id, trunk: trunk, branch: store.StoryBranch(id)}
	w.find(ctx)
	if !w.listed && !w.hasBranch {
		return "move what is in " + worktreeIn(id) + " in the main checkout somewhere else, then start again: " +
			"start never writes over or removes what it did not make; " + assistantStops
	}

	fix := `a person first checks that no "sdlc start" is still running for ` + id + `, and if one is, lets it ` +
		`finish and runs "sdlc start" again; otherwise, in the main checkout: `
	// Every step that applies, in the order a person takes them: what is kept
	// is committed on the branch before the branch goes to trunk. The loop's
	// own files under .sdlc/state are in every story worktree, and are not
	// somebody's changes.
	switch {
	case w.gone:
		w.steps = append(w.steps, `its directory is gone, and "git worktree prune" makes git forget it`)
	case w.listed:
		w.stepsForWorktree(ctx)
	}
	w.stepForBranch(ctx)
	w.stepToRemove()
	return fix + strings.Join(w.steps, thenClause) + "; and start again; " + assistantStops
}

// inTheWay is what stands where a story's worktree goes, and the steps a
// person takes about it.
type inTheWay struct {
	main, wt, id, trunk     string
	branch                  string
	listed, gone, hasBranch bool
	on                      string
	steps                   []string
	loops, mayLose          bool
}

// find reads whether git lists the worktree, and whether the branch exists.
func (w *inTheWay) find(ctx context.Context) {
	if trees, err := gitx.Worktrees(ctx, w.main); err == nil {
		for _, t := range trees {
			if config.SamePath(t.Path, w.wt) {
				w.listed, w.gone, w.on = true, t.Gone, t.Branch
			}
		}
	}
	_, err := gitx.Resolve(ctx, w.main, "refs/heads/"+w.branch)
	w.hasBranch = err == nil
}

// stepsForWorktree is what to do about changes in the worktree, and about
// commits its HEAD holds on no branch.
func (w *inTheWay) stepsForWorktree(ctx context.Context) {
	changes, own, err := workChanges(ctx, w.wt)
	w.loops = own
	if err == nil && len(changes) > 0 {
		where := "there"
		if w.on == w.branch {
			where = "on " + w.branch
		}
		w.steps = append(w.steps, "the worktree has changes nobody committed, which may be somebody's work: "+
			"keep what is wanted, committed "+where+" or copied elsewhere")
	}
	if w.on != "" {
		return
	}
	id, trunk := w.id, w.trunk
	n, err := gitx.HeadAhead(ctx, w.wt, trunk)
	switch {
	case err != nil:
		w.steps = append(w.steps, "the worktree is on no branch, and git could not count the commits its HEAD has "+
			"that trunk does not: "+lostWithIt(0)+", so look at them with "+lookAt(id, trunk)+", and keep what is "+
			"wanted on a branch, with "+keepOn(id)+", before merging or removing anything")
		w.mayLose = true
	case n > 0:
		w.steps = append(w.steps, fmt.Sprintf("the worktree is on no branch, and its HEAD has %d %s trunk does not "+
			"have: %s, so look at %s with %s, and keep what is wanted on a branch, with %s, before merging or "+
			"removing anything", n, plural(n, "commit", "commits"), lostWithIt(n), plural(n, "it", "them"),
			lookAt(id, trunk), keepOn(id)))
		w.mayLose = true
	}
}

// stepForBranch is what to do about commits only the story's branch has.
func (w *inTheWay) stepForBranch(ctx context.Context) {
	if !w.hasBranch {
		return
	}
	branch := w.branch
	if n, err := gitx.Ahead(ctx, w.main, branch, w.trunk); err == nil && n > 0 {
		w.steps = append(w.steps, fmt.Sprintf("%s has %d %s trunk does not have, which may be the story's work: "+
			"bring %s to trunk, with trunk checked out, with \"git merge --ff-only %s\" (if git cannot "+
			"fast-forward, first rebase %s on trunk in its worktree and run the tests), or keep the branch "+
			"under another name with \"git branch -m %s <new name>\"",
			branch, n, plural(n, "commit", "commits"), plural(n, "it", "them"), branch, branch, branch))
	}
}

// stepToRemove is the last step: taking away what is in the way. A worktree
// whose HEAD may hold commits on no branch is a person's to take away once
// they have looked, and so is the branch that goes with it.
func (w *inTheWay) stepToRemove() {
	var remove []string
	if w.listed && !w.gone && !w.mayLose {
		remove = append(remove, `"git worktree remove `+worktreeIn(w.id)+`"`)
	}
	if w.hasBranch && !w.mayLose {
		remove = append(remove, `"git branch -d `+w.branch+`"`)
	}
	if len(remove) > 0 {
		step := "remove what is in the way with " + strings.Join(remove, " and then ")
		if w.loops {
			step += ", where the loop's own files under .sdlc/state are safe to lose if git refuses over them"
		}
		w.steps = append(w.steps, step)
	}
}

// behindInWorktrees is SDLC-E0040 as it reads where the story is cut from
// trunk's branch, which need not be checked out anywhere: git pull brings the
// branch a checkout has checked out up to date, and nothing else.
func behindInWorktrees(err error, trunk string) error {
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.TrunkBehind {
		return err
	}
	return e.WithFix("bring trunk up to date, then start again: in a checkout that has " + quote(trunk) +
		` checked out, "git pull --ff-only"; where none has, "git fetch origin ` + trunk + ":" + trunk + `"`)
}

// noConfigOnTrunk is a worktree whose configuration, the one trunk commits,
// cannot be read. contract is the files the contract is in, as
// contractToCommit names them, which the one caller passes, so that doctor
// and this name the same ones. It is variadic so that a test that asks for the
// refusal alone still compiles; with no names the fix names only the
// configuration, so a new caller that skips the lookup tells the person less.
func noConfigOnTrunk(err error, trunk string, contract ...string) error {
	var e *sdlcerr.Error
	if errors.As(err, &e) && e.Code == sdlcerr.NotInitialised {
		return sdlcerr.New(sdlcerr.WorktreeNotMade,
			"the story's worktree could not be made: trunk, "+quote(trunk)+", has no .sdlc/config.json committed",
			"a new worktree has only what trunk commits, and the loop in it reads its configuration from there").
			WithCause(err).
			WithFix(commitList(contract) + ", then start again, or set git.worktrees to " +
				"false in this checkout's .sdlc/config.json; an assistant stops and tells the person")
	}
	what, why := err.Error(), ""
	if errors.As(err, &e) {
		what, why = e.What, e.Why
	}
	return sdlcerr.New(sdlcerr.WorktreeNotMade,
		"the story's worktree could not be made: the .sdlc/config.json trunk commits cannot be read: "+what, why).
		WithCause(err).
		WithFix("correct .sdlc/config.json and commit it to trunk, then start again, or set git.worktrees to false " +
			"in this checkout's .sdlc/config.json; an assistant stops and tells the person")
}

// smokeFailedInWorktree is SDLC-E0041 for a smoke check that failed in the
// story's new worktree. Most often that is a check that needs something trunk
// does not commit, which a checkout that has been worked in has and a new one
// does not.
func smokeFailedInWorktree(wt, id string, failed error, output string, left error) error {
	why := "it exited with " + failed.Error() + " in " + sdlcerr.QuotePath(wt) + ", the story's new worktree, cut from " +
		"trunk. A new worktree has only what trunk commits: no installed dependencies, such as node_modules or " +
		"a virtual environment, and no generated files"
	if left == nil {
		why += ". Start took the worktree and its branch away again"
	} else {
		why += ". The worktree start made could not all be taken away again: " + describe(left)
	}
	if output != "" {
		why += "\n\n" + output
	}
	fix := `fix trunk, or revert what broke it; if trunk works where its dependencies are installed, make ` +
		`commands.smoke install them first, for example "smoke": "npm ci && npm run build --silent", and commit ` +
		`that to trunk, which is where a new worktree reads .sdlc/config.json from; then start again, or set ` +
		`git.worktrees to false in this checkout's .sdlc/config.json`
	if left != nil {
		fix = leftBehind(id) + thenClause + fix
	}
	return sdlcerr.New(sdlcerr.TrunkBroken, "commands.smoke failed in the story's new worktree", why).WithFix(fix)
}

// rolledBack is err, for which start took away the worktree it made, with
// what could not be taken away, where anything could not.
func rolledBack(err error, id string, left error) error {
	if left == nil {
		return err
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		e = sdlcerr.New(sdlcerr.WorktreeNotMade, "the story's worktree could not be made: "+err.Error(), "")
	}
	out := *e
	if out.Why != "" {
		out.Why += "; "
	}
	out.Why += "and the worktree start made could not all be taken away again: " + describe(left)
	out.Fix = leftBehind(id) + thenClause + e.Fix
	return &out
}

// leftBegun is err for a start that failed once the story's new worktree
// named the story as its own, which leaves the worktree and its branch: said
// bare, the error left a person to find both. held is whether the backlog had
// the story in progress by then, which makes the worktree hold it for the
// next start to pick up; before that, a start of the story finds them in the
// way, and says what to do about them. The story is named, since a bare start
// can begin another.
func leftBegun(err error, id string, held bool) error {
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		e = sdlcerr.New(sdlcerr.StateUnwritable, "the story could not be recorded as begun: "+err.Error(), "").
			WithCause(err)
	}
	out := *e
	if out.Why != "" {
		out.Why += "; "
	}
	out.Why += "start had made the story's worktree, " + worktreeIn(id) + ", on " + store.StoryBranch(id) +
		", and leaves both"
	next := `"sdlc start ` + id + `" finds them in the way, and says what to do about them`
	if held {
		out.Why += ", with the story begun there"
		next = `"sdlc start ` + id + `" picks the story up in that worktree`
	}
	if out.Fix == "" {
		out.Fix = "once what is said above is put right, " + next
	} else {
		out.Fix += thenClause + next
	}
	return &out
}

// leftBehind is what a person does about a worktree start could not take away
// again. On Windows that is most often a file something still has open in it.
func leftBehind(id string) string {
	return "first take away what start left: a person closes anything that has a file open in " + worktreeIn(id) +
		`, then runs "git worktree remove ` + worktreeIn(id) + `" and "git branch -d ` + store.StoryBranch(id) +
		`" in the main checkout`
}

// describe is an error as the why of another reads it.
func describe(err error) string {
	if e, ok := errors.AsType[*sdlcerr.Error](err); ok {
		if e.Why != "" {
			return e.What + ": " + e.Why
		}
		return e.What
	}
	return err.Error()
}
