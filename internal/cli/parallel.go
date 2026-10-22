package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/parallel"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellx"
	"github.com/bbsnly/sdlc/internal/store"
)

// parallelTail is how much of a session's last output is kept to show beneath
// a story that ended without anyone waiting on it. In memory and nowhere else:
// a log file would be one more place for a checkout's secrets, and one more
// path for `git add -A` to pick up.
const parallelTail = 2000

type parallelPayload struct {
	OK      bool           `json:"ok"`
	Stories []parallel.Row `json:"stories"`
}

func newParallelCmd() *cobra.Command {
	var limit string
	var unattended bool
	cmd := &cobra.Command{
		Use:   "parallel ID...",
		Short: "Work several stories at once, one headless Claude Code session and one worktree each",
		Long: "parallel starts one Claude Code session for each story named, each running /sdlc:next\n" +
			"for its story from the main checkout, so that each is worked in a worktree of its\n" +
			"own. At most --max run at once; the rest start as a slot frees. Run it again to\n" +
			"resume: a story already under way carries on where it was left.\n\n" +
			"The sessions are not interactive. No permission prompt reaches anyone, so a tool\n" +
			"that default permissions deny ends that story's turn, and the story is shown as\n" +
			"ended early. Each session costs what a full loop costs, and several run at once.\n\n" +
			"With --unattended each session is given the built-in list below to use without\n" +
			"asking, and never a blanket skip of permissions. Anything not on it is still\n" +
			"denied, and the hook's rules still apply in the session. The list is not a\n" +
			"guarantee of safety: a build or a test runs code the session has written, with\n" +
			"your privileges. The built-in list:\n\n" +
			"  " + strings.Join(parallel.Allowlist(), "\n  ") + "\n\n" +
			"parallel.allow in the project's configuration can only narrow the list.\n\n" +
			"Run it in your own terminal: the hook refuses it from a tool call. It reads the\n" +
			"loop's state and writes none of its own; when the sessions have ended it says\n" +
			"which stories wait on you, with the commands to run.",
		Example: "  sdlc parallel US-001 US-002\n" +
			"  sdlc parallel --max 3 --unattended US-001 US-002 US-003",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, ids []string) error {
			return runParallel(cmd, ids, limit, unattended)
		},
	}
	cmd.Flags().StringVar(&limit, "max", "2", "the most sessions to run at once")
	cmd.Flags().BoolVar(&unattended, "unattended", false,
		"give each session the built-in list of tools to use without asking (not a skip of permissions; see --help)")
	return cmd
}

func runParallel(cmd *cobra.Command, ids []string, maxFlag string, unattended bool) error {
	limit, err := parallelLimit(cmd, ids, maxFlag)
	if err != nil {
		return err
	}

	s, _, err := openStore(cmd)
	if err != nil {
		return err
	}
	main, allow, err := parallelMain(s, unattended)
	if err != nil {
		return err
	}

	backlog, err := main.Backlog()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := parallelPreflight(main, backlog, id); err != nil {
			return err
		}
	}

	results, tails := launchSessions(cmd, main, ids, limit, allow, unattended)

	// From here the launcher only reads: what a session did to its story is the
	// session's, and a run never says a story is done.
	rows, failed, err := classifyRuns(main, results)
	if err != nil {
		return err
	}
	if wantJSON(cmd) {
		if err := emitJSON(cmd.OutOrStdout(), parallelPayload{OK: !failed, Stories: rows}); err != nil {
			return err
		}
	} else {
		printParallel(cmd.OutOrStdout(), main, backlog, rows, tails)
	}
	if failed {
		return quietExitError{code: 1}
	}
	return nil
}

// classifyRuns reads each story's record and says how its session went.
func classifyRuns(main *store.Store, results []parallel.Result) (rows []parallel.Row, failed bool, err error) {
	rows = make([]parallel.Row, len(results))
	for i, r := range results {
		rec, err := main.Record(r.Story)
		if err != nil {
			return nil, false, err
		}
		rows[i] = parallel.Classify(r.Story, rec, r.Exit)
		failed = failed || rows[i].State == parallel.StateFailed
	}
	return rows, failed, nil
}

// parallelLimit checks the command line, and returns how many sessions may run
// at once.
func parallelLimit(cmd *cobra.Command, ids []string, maxFlag string) (int, error) {
	// The hook is the first barrier, and refuses the command in any session.
	// This is the second, for a spelling it cannot read. It comes before
	// anything is read, so the error cannot be used to ask the backlog a
	// question. A session id is all it looks at, as refuseAnotherSession does,
	// and SDLC-E0079 follows it for every other variable Claude Code sets.
	if envOf(cmd).getenv(store.SessionEnv) != "" {
		return 0, sdlcerr.New(sdlcerr.ParallelFromASession,
			"sdlc parallel was run from inside a Claude Code session",
			"starting model sessions is a person's decision")
	}
	if err := refuseInsideClaudeCode(cmd, ""); err != nil {
		return 0, err
	}
	limit, err := strconv.Atoi(maxFlag)
	if err != nil || limit < 1 {
		return 0, sdlcerr.New(sdlcerr.ParallelBadMax,
			"--max is "+quote(maxFlag)+", which is not a whole number of 1 or more",
			"it is the most sessions to run at the same time")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if err := store.CheckID(id); err != nil {
			return 0, err
		}
		if seen[id] {
			return 0, sdlcerr.New(sdlcerr.ParallelListedTwice,
				quote(id)+" is named more than once",
				"two sessions on one story would work in the same worktree")
		}
		seen[id] = true
	}
	return limit, nil
}

// parallelMain is the main checkout, wherever this ran from: the backlog, the
// records and the claims are read there, and the sessions start there. It also
// narrows the tools the sessions may use without asking, where they may.
func parallelMain(s *store.Store, unattended bool) (*store.Store, []string, error) {
	co, err := s.Checkout()
	if err != nil {
		return nil, nil, err
	}
	main, err := s.At(co.Main)
	if err != nil {
		return nil, nil, err
	}
	if !main.Config().Git.WorktreesOn() {
		return nil, nil, sdlcerr.New(sdlcerr.ParallelNeedsWorktrees,
			"git.worktrees is off in this project",
			"each story is worked in a worktree of its own, and so is not worked at once without them")
	}
	var allow []string
	if unattended {
		if allow, err = parallel.Narrow(main.Config().Parallel.Allow); err != nil {
			return nil, nil, err
		}
	}
	return main, allow, nil
}

// launchSessions runs a session for each story, at most limit at once, and
// returns how each ended with what each printed last.
func launchSessions(cmd *cobra.Command, main *store.Store, ids []string, limit int, allow []string,
	unattended bool,
) ([]parallel.Result, map[string]*shellx.TailWriter) {
	stderr := cmd.ErrOrStderr()
	// Sessions end on goroutines of their own, and the writer behind stderr
	// is not safe for two of them at once.
	var said sync.Mutex
	say := func(format string, args ...any) {
		said.Lock()
		defer said.Unlock()
		fmt.Fprintf(stderr, "sdlc: "+format+"\n", args...)
	}
	if unattended {
		say("each session is given %d tools to use without asking, and nothing else", len(allow))
	} else {
		say("the sessions are headless and cannot ask, so each stops at its first tool that default permissions deny")
	}

	tails := make(map[string]*shellx.TailWriter, len(ids))
	for _, id := range ids {
		tails[id] = shellx.NewTail(parallelTail)
	}
	run := func(ctx context.Context, id string) (int, error) {
		defer say("the session for %s has ended", id)
		return runSession(ctx, main, id, allow, tails[id])
	}
	results := parallel.Schedule(cmd.Context(), ids, limit, run, func(p parallel.Progress) {
		say("running %d, waiting %d", p.Running, p.Waiting)
	})
	return results, tails
}

// runSession runs the headless session for one story and reports how it exited.
func runSession(ctx context.Context, main *store.Store, id string, allow []string, tail *shellx.TailWriter) (int, error) {
	session, err := parallel.Command(ctx, main.Root(), id, allow)
	if err != nil {
		return -1, err
	}
	session.Stdout, session.Stderr = tail, tail
	err = shellx.Run(session)
	var exited *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exited):
		return exited.ExitCode(), nil
	}
	// It never started, or was never waited for: the person reads why
	// beneath the story, with whatever else the session printed.
	fmt.Fprintln(tail, err)
	return -1, err
}

// parallelPreflight is what `sdlc start <ID>` would decide about a story, from
// reads alone and in the same order, so that the two cannot disagree: a story
// is refused here that start would refuse, and every story is looked at before
// any session is started. The refusals are the loop's own, re-coded.
func parallelPreflight(s *store.Store, b *model.Backlog, id string) error {
	story, ok := b.Find(id)
	if !ok {
		return sdlcerr.New(sdlcerr.ParallelUnknownStory,
			quote(id)+" is not in the backlog",
			"sdlc parallel works stories the backlog has")
	}
	at, inItsWorktree, err := heldInItsWorktree(s, id)
	if err != nil {
		return err
	}
	finished, err := refuseIfFinished(s, id)
	if err != nil {
		return err
	}
	if finished != nil {
		if inItsWorktree {
			return recode(waitingToLand(id, trunkName(s), store.ActiveIn(at) == id), sdlcerr.ParallelFinished, "")
		}
		return recode(finished, sdlcerr.ParallelFinished, "")
	}
	// Its own worktree is not another checkout: the story resumes there.
	if !inItsWorktree {
		held := refuseIfHeldElsewhere(s, id, "a story is worked in one checkout at a time")
		if err := recode(held, sdlcerr.ParallelHeldElsewhere, ""); err != nil {
			return err
		}
	}
	// A run again would carry on past the question the loop stopped to ask, and
	// so act as the person's answer.
	if err := recode(refuseIfWaiting(s, id), sdlcerr.ParallelAwaitingPerson,
		`run "/sdlc:approve `+id+`", or "sdlc approve `+id+`" in `+worktreeIn(id)+` (or its --reject), then name the story again; `+
			"an assistant stops and tells the person"); err != nil {
		return err
	}
	if resuming(story.Status) {
		return nil
	}
	return recode(refuseIfNotRunnable(b, story), sdlcerr.ParallelNotRunnable, "")
}

// recode is the loop's refusal under this command's code, in the loop's own
// words, so that the two commands cannot say different things about one story.
// A fix of its own replaces the original where the original aims at another
// command. What is not a refusal, an unreadable file say, passes as it is.
func recode(err error, code sdlcerr.Code, fix string) error {
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		return err
	}
	out := sdlcerr.New(code, e.What, e.Why).WithFix(e.Fix)
	if fix != "" {
		out = out.WithFix(fix)
	}
	return out
}

// printParallel is the account a person reads when the sessions have ended: one
// line per story, in the order named, what a session that ended without anyone
// waiting on it printed last, and, last of all, in one place, what waits on the
// person and where to run it.
func printParallel(w io.Writer, main *store.Store, b *model.Backlog, rows []parallel.Row, tails map[string]*shellx.TailWriter) {
	var early, waiting []parallel.Row
	for _, row := range rows {
		title := ""
		if story, ok := b.Find(row.Story); ok {
			title = "  " + oneLine(story.Title)
		}
		where := "at " + row.Gate
		if row.Gate == "" {
			where = "every gate passed"
		}
		switch row.State {
		case parallel.StateWaiting:
			fmt.Fprintf(w, "%s  waiting on you, %s%s\n", row.Story, where, title)
			waiting = append(waiting, row)
		case parallel.StateFailed:
			fmt.Fprintf(w, "%s  failed with exit %d, %s%s\n", row.Story, row.Exit, where, title)
			indent(w, tails[row.Story].String())
		default:
			fmt.Fprintf(w, "%s  ended early, %s%s\n", row.Story, where, title)
			indent(w, tails[row.Story].String())
			early = append(early, row)
		}
	}
	if len(early) > 0 {
		fmt.Fprintln(w, "\nEnded early with nothing waiting on you. The usual cause is a tool that default")
		fmt.Fprintln(w, "permissions deny, which a session with nobody to ask cannot get past. Run it again to")
		fmt.Fprintln(w, "resume where it stopped, or look at where each story is:")
		for _, row := range early {
			fmt.Fprintf(w, "  sdlc parallel %s\n", row.Story)
		}
		fmt.Fprintln(w, "  sdlc status")
	}
	if len(waiting) > 0 {
		fmt.Fprintln(w, "\nWaiting on you:")
		for _, row := range waiting {
			// Cannot fail: the id was checked and the checkout read above.
			wt, _ := main.StoryWorktree(row.Story)
			fmt.Fprintf(w, "  cd %s && %s\n", shellWord(wt), row.Command)
		}
	}
}

// indent is a session's last output beneath the story it belongs to.
func indent(w io.Writer, text string) {
	for line := range strings.Lines(text) {
		fmt.Fprintf(w, "    %s\n", strings.TrimRight(line, "\r\n"))
	}
}
