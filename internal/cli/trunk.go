package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellx"
	"github.com/bbsnly/sdlc/internal/store"
)

// remoteTimeout bounds asking the remote about trunk. A remote that does not
// answer is something to say, not a reason to hang the start of every story.
const remoteTimeout = 60 * time.Second

// smokeOutputLimit is how much of a failed smoke check the refusal carries.
const smokeOutputLimit = 2000

// checkTrunkForNewWork runs checkTrunk when start is about to begin a story
// rather than pick one up again, and reports whether it ran. It runs before
// start takes the lock, because the smoke check is the project's own command
// and can run for longer than a lock is trusted to be held.
//
// Whatever would refuse the start anyway -- nothing runnable, a story finished,
// waiting on a person or held by another checkout -- refuses here first, in the
// words start would use, because it says more than trunk would: it is the same
// decision start makes under the lock. A named story that is not runnable is
// the exception, refused after trunk as it always was. force skips the
// decision, for a start that has already made it under the lock.
func checkTrunkForNewWork(cmd *cobra.Command, asked string, force bool, said *startSaid) (bool, error) {
	s, _, err := openStore(cmd)
	if err != nil {
		return false, err
	}
	if !force {
		plan, err := decideStart(s, asked, s.CommandSession())
		said.passed = plan.passedOver
		if err != nil || !plan.fresh {
			return false, err
		}
	}
	return true, checkTrunk(cmd, s, len(said.passed) > 0)
}

// checkTrunk is what new work asks first: that trunk is somewhere a story can
// start from. A story started on another branch, on top of work nobody
// committed, behind its remote, or on a trunk that is already broken inherits
// all of it -- its commit carries changes that are not its own, and its tests
// cannot tell their failures from the ones that were already there.
//
// passedOver is a start that passed over a story whose checkout no longer
// exists. If this checkout was moved from there, what it has not committed is
// that story's work, and it is not to be discarded to make room for the next.
func checkTrunk(cmd *cobra.Command, s *store.Store, passedOver bool) error {
	ctx := cmd.Context()
	cfg := s.Config()
	trunk := strings.TrimSpace(cfg.Git.TrunkBranch)
	if trunk == "" {
		trunk = config.Default().Git.TrunkBranch
	}

	branch, err := gitx.Branch(ctx, s.Root())
	if err != nil {
		return err
	}
	if branch != trunk {
		where := "HEAD is detached"
		if branch != "" {
			where = "HEAD is on " + quote(branch)
		}
		return sdlcerr.New(sdlcerr.NotOnTrunk,
			"a new story starts from "+quote(trunk)+", and "+where,
			"the loop commits every story straight to trunk, so work started "+
				"anywhere else is not where its commit is recorded as landing")
	}

	changes, err := gitx.Changes(ctx, s.Root())
	if err != nil {
		return err
	}
	if stray := strayChanges(changes, cfg.Backlog.Path); len(stray) > 0 {
		refusal := sdlcerr.New(sdlcerr.UncommittedWork,
			"there is uncommitted work that belongs to no story: "+listPaths(stray),
			"a story is committed with everything in the working tree, so this "+
				"would be reviewed and committed as part of the next one")
		if passedOver {
			return refusal.WithFix("if this checkout was moved from the checkout the note above names, this may be " +
				"the passed-over story's own work, kept here: a person releases that story here and starts it again, " +
				"as the note says; otherwise commit it or stash it, then start again -- the loop's own files under " +
				".sdlc/ and the backlog do not count")
		}
		return refusal
	}

	if cfg.Git.Remote {
		if err := checkRemote(cmd, s.Root(), trunk); err != nil {
			return err
		}
	}
	return runSmoke(cmd, s.Root(), cfg.Commands["smoke"])
}

// strayChanges drops the changes that belong to the loop rather than to the
// project: its own state, and the backlog, whose statuses it writes. A write of
// the backlog that was killed part-way is the loop's too, and the next command
// that takes the lock clears it away.
func strayChanges(changes []string, backlog string) []string {
	backlog = filepath.ToSlash(filepath.Clean(backlog))
	var stray []string
	for _, p := range changes {
		if p == backlog || p == config.Dir+"/" || strings.HasPrefix(p, config.Dir+"/") {
			continue
		}
		if store.InterruptedWriteOf(filepath.Base(p), filepath.Base(backlog)) {
			continue
		}
		stray = append(stray, p)
	}
	return stray
}

// listPaths names a few paths and counts the rest, so that a tree with a
// thousand stray files is a refusal and not a page of them.
func listPaths(paths []string) string {
	const shown = 5
	if len(paths) <= shown {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(paths[:shown], ", "), len(paths)-shown)
}

// checkRemote refuses a trunk that is behind origin. Not being able to ask is
// not the same as being behind: working offline is allowed, and said out loud.
func checkRemote(cmd *cobra.Command, root, trunk string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), remoteTimeout)
	defer cancel()
	behind, err := gitx.Behind(ctx, root, "origin", trunk)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"sdlc: origin could not be asked whether %s is behind it, so this starts without knowing: %v\n",
			trunk, err)
		return nil
	}
	if behind > 0 {
		return sdlcerr.New(sdlcerr.TrunkBehind,
			fmt.Sprintf("%s is %d %s behind origin", quote(trunk), behind, plural(behind, "commit", "commits")),
			"a story started on an old trunk is planned, tested and reviewed against "+
				"code that has already changed, and meets the difference only when it is committed")
	}
	return nil
}

// runSmoke runs commands.smoke. A check that could not run is said, like an
// unreachable remote; only one that ran and failed refuses.
func runSmoke(cmd *cobra.Command, root, command string) error {
	failed, said, err := smoke(cmd, root, command, nil)
	if err != nil || failed == nil {
		return err
	}
	why := "it exited with " + failed.Error() + " before the story has changed anything, " +
		"so every failure the story meets would look like its own"
	if said != "" {
		why += "\n\n" + said
	}
	return sdlcerr.New(sdlcerr.TrunkBroken, "trunk does not pass commands.smoke", why)
}

// smoke runs commands.smoke in dir, with env where it is not nil, and returns
// how it failed and the end of what it printed, or nil where it passed or did
// not run.
func smoke(cmd *cobra.Command, dir, command string, env []string) (failed error, said string, err error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, "", nil
	}
	check, err := shellx.Command(cmd.Context(), dir, command)
	if errors.Is(err, shellx.ErrNoShell) {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"sdlc: commands.smoke was not run, because %v, so this starts without knowing whether trunk works\n", err)
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if env != nil {
		check.Env = env
	}
	var out bytes.Buffer
	check.Stdout, check.Stderr = &out, &out
	if err := shellx.Run(check); err != nil {
		return err, shellx.Tail(out.String(), smokeOutputLimit), nil
	}
	return nil, "", nil
}
