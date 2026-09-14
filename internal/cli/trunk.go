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
// rather than pick one up again. It runs before start takes the lock, because
// the smoke check is the project's own command and can run for longer than a
// lock is trusted to be held.
//
// Whatever would refuse the start anyway -- nothing runnable, a story finished
// or waiting on a person -- refuses here first, in the words start would use,
// because it says more than trunk would. A running iteration is start's own to
// resume or refuse.
func checkTrunkForNewWork(cmd *cobra.Command, asked string) error {
	s, _, err := openStore()
	if err != nil {
		return err
	}
	active, err := s.Active()
	if err != nil {
		return err
	}
	if active != "" {
		return nil
	}
	id := asked
	if id == "" {
		if id, err = nextRunnable(s); err != nil {
			return err
		}
	}
	story, _, err := s.Story(id)
	if err != nil {
		return err
	}
	if resuming(story.Status) {
		return nil
	}
	finished, err := refuseIfFinished(s, id)
	if err != nil {
		return err
	}
	if finished != nil {
		return finished
	}
	if err := refuseIfWaiting(s, id); err != nil {
		return err
	}
	return checkTrunk(cmd, s)
}

// checkTrunk is what new work asks first: that trunk is somewhere a story can
// start from. A story started on another branch, on top of work nobody
// committed, behind its remote, or on a trunk that is already broken inherits
// all of it -- its commit carries changes that are not its own, and its tests
// cannot tell their failures from the ones that were already there.
func checkTrunk(cmd *cobra.Command, s *store.Store) error {
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
		return sdlcerr.New(sdlcerr.UncommittedWork,
			"there is uncommitted work that belongs to no story: "+listPaths(stray),
			"a story is committed with everything in the working tree, so this "+
				"would be reviewed and committed as part of the next one")
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
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	smoke, err := shellx.Command(cmd.Context(), root, command)
	if errors.Is(err, shellx.ErrNoShell) {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"sdlc: commands.smoke was not run, because %v, so this starts without knowing whether trunk works\n", err)
		return nil
	}
	if err != nil {
		return err
	}
	var out bytes.Buffer
	smoke.Stdout, smoke.Stderr = &out, &out
	if err := smoke.Run(); err != nil {
		why := "it exited with " + err.Error() + " before the story has changed anything, " +
			"so every failure the story meets would look like its own"
		if said := shellx.Tail(out.String(), smokeOutputLimit); said != "" {
			why += "\n\n" + said
		}
		return sdlcerr.New(sdlcerr.TrunkBroken, "trunk does not pass commands.smoke", why)
	}
	return nil
}
