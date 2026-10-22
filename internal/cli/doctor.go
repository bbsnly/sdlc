package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/scaffold"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
	"github.com/bbsnly/sdlc/internal/store"
)

// state is how one check came out.
type state string

const (
	stateOK      state = "ok"
	stateProblem state = "problem"
	stateSkipped state = "skipped"
)

// check is one thing doctor looked at. A problem always carries a fix: a check
// that only reports leaves the reader to guess, which is how a tool teaches
// people to ignore it.
type check struct {
	Name   string `json:"name"`
	State  state  `json:"state"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

type doctorPayload struct {
	OK       bool    `json:"ok"`
	Problems int     `json:"problems"`
	Checks   []check `json:"checks"`
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check this project and say how to fix what is wrong",
		Long: "doctor looks at the things that stop the loop working, and prints the\n" +
			"command that fixes each one.\n\n" +
			"It changes nothing. Run it after `sdlc init`, after editing\n" +
			".sdlc/config.json, or whenever something behaves in a way you did not\n" +
			"expect.",
		Example: "  sdlc doctor\n  sdlc doctor --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := runChecks(cmd.Context())
			problems := 0
			for _, c := range checks {
				if c.State == stateProblem {
					problems++
				}
			}

			if wantJSON(cmd) {
				if err := emitJSON(cmd.OutOrStdout(), doctorPayload{
					OK: problems == 0, Problems: problems, Checks: checks,
				}); err != nil {
					return err
				}
			} else {
				printChecks(cmd.OutOrStdout(), checks, problems)
			}
			if problems > 0 {
				return quietExitError{code: 1}
			}
			return nil
		},
	}
}

func printChecks(w io.Writer, checks []check, problems int) {
	fmt.Fprintln(w, "sdlc doctor")
	fmt.Fprintln(w)
	for _, c := range checks {
		// A check about several things has one to a line, each with its fix
		// beside it: joined into one line, five worktrees' descriptions and
		// then five fixes left a person matching them up by counting.
		details, fixes := strings.Split(c.Detail, "\n"), strings.Split(c.Fix, "\n")
		paired := len(details) > 1 && len(details) == len(fixes)
		for i, d := range details {
			if i == 0 {
				fmt.Fprintf(w, "  %-8s %-20s %s\n", c.State, c.Name, d)
			} else {
				fmt.Fprintf(w, "           %-20s %s\n", "", d)
			}
			if paired {
				fmt.Fprintf(w, "           %-20s fix: %s\n", "", fixes[i])
			}
		}
		if c.Fix != "" && !paired {
			fmt.Fprintf(w, "           %-20s fix: %s\n", "", c.Fix)
		}
	}
	fmt.Fprintln(w)
	switch problems {
	case 0:
		fmt.Fprintln(w, "Everything checks out.")
	case 1:
		fmt.Fprintln(w, "1 problem. The fix is above it.")
	default:
		fmt.Fprintf(w, "%d problems. The fix is above each one.\n", problems)
	}
}

// The names of the checks, which the report prints and skipRest matches.
const (
	checkGitRepository   = "git repository"
	checkGitCommand      = "git command"
	checkLoopState       = "loop state"
	checkProjectContract = "project contract"
	checkSdlcOnPath      = "sdlc on PATH"
	checkConfiguration   = "configuration"
	checkBacklog         = "backlog"
	checkCommands        = "commands"
)

// worktreeOf joins a story's id to the path of its worktree in a sentence.
const worktreeOf = "'s worktree, "

// runChecks walks from the outside in: each check assumes the ones before it
// passed, and says it was skipped rather than reporting a second failure with
// the same cause.
func runChecks(ctx context.Context) []check {
	var out []check
	add := func(c check) { out = append(out, c) }

	env := envIn(ctx)
	if env.dirErr != nil {
		add(check{Name: "working directory", State: stateProblem,
			Detail: "the current directory could not be read",
			Fix:    "change to a directory that still exists"})
		return out
	}

	root, err := config.FindRoot(env.dir)
	if err != nil {
		add(check{Name: checkGitRepository, State: stateProblem,
			Detail: "this is not inside a Git repository",
			Fix:    `run "git init", or change to a directory inside your repository`})
		return append(out, skipRest(checkGitRepository, checkGitRepository)...)
	}
	add(check{Name: checkGitRepository, State: stateOK, Detail: root})

	cfg, err := config.Load(root)
	// Which git is old enough depends on whether start makes each story a
	// worktree, which only a configuration that reads can say.
	add(gitCheck(ctx, err == nil && cfg.Git.WorktreesOn()))
	if err == nil {
		// What every other command refuses when it opens the project.
		err = staffingRoster(cfg.Staffing)
	}
	if err != nil {
		// The error's own words: its cause alone said nothing of which setting
		// was wrong, and "sdlc init" does not fix a file that is already there.
		detail, fix := err.Error(), `if you want the loop in this repository, run "sdlc init" in `+root+` yourself`
		if e, ok := errors.AsType[*sdlcerr.Error](err); ok {
			detail, fix = e.What, e.Fix
			if e.Why != "" {
				detail += ": " + e.Why
			}
		}
		add(check{Name: checkConfiguration, State: stateProblem, Detail: detail, Fix: fix})
		add(check{Name: checkBacklog, State: stateSkipped,
			Detail: "not checked: configuration has to work first"})
		// The loop's state does not come from the configuration, and a broken
		// configuration is when the hook warns about both and sends you here --
		// so this is the one check that still runs.
		add(stateCheck(ctx, store.New(&config.Project{Root: root, Config: config.Default()}).WithEnv(env.getenv)))
		return append(out, skipRest(checkLoopState, checkConfiguration)...)
	}
	stack, _ := scaffold.Detect(root)
	detail := config.File
	if stack.Name != "" {
		detail += "  (" + stack.Name + " project)"
	}
	if unknown := config.UnknownKeys(root); len(unknown) > 0 {
		add(check{Name: checkConfiguration, State: stateProblem,
			Detail: config.File + " has settings sdlc does not know, which change nothing: " +
				strings.Join(unknown, ", "),
			Fix: "correct the spelling, or remove them — docs/configuration.md lists every setting, " +
				"and a key that starts with _ is a comment"})
	} else {
		add(check{Name: checkConfiguration, State: stateOK, Detail: detail})
	}

	s := store.New(&config.Project{Root: root, Config: cfg}).WithEnv(env.getenv)
	add(backlogCheck(s, root))
	add(stateCheck(ctx, s))
	if c, ok := storyWorktreesCheck(ctx, s); ok {
		add(c)
	}
	add(contractCheck(root))
	out = append(out, commandChecks(root, cfg)...)
	add(binaryCheck(env))
	return out
}

// stateCheck reads the loop's own files: the iteration file, every story's gate
// record and the test freeze, which the hook fails open over when one is there
// and cannot be read, saying so and sending you here -- so this has to be the
// check that knows which one it is. It also reads the commit acknowledged, which
// the hook never does and `sdlc log` refuses when git does not have it.
//
// Every problem at once, not the first: a broken iteration file reported alone
// hid a broken freeze behind it, and a second run of doctor to find it.
func stateCheck(ctx context.Context, s *store.Store) check {
	p := &stateProbe{s: s}
	// Said once: every record is kept where the backlog is, and each of them
	// failing to read for the one reason says nothing more.
	p.shared = s.SharedErr()
	if e, ok := errors.AsType[*sdlcerr.Error](p.shared); ok {
		p.problem(noMainCheckout(e))
	}
	p.readActive()
	p.readOtherRecords()
	p.readFreeze()
	p.readAcknowledged(ctx)
	p.countWorktrees()
	held := describeHolders(s)
	if len(p.details) > 0 {
		return check{Name: checkLoopState, State: stateProblem,
			Detail: strings.Join(p.details, "; ") + held, Fix: strings.Join(p.fixes, "; ")}
	}
	detail := "the iteration, every gate record, the test freeze and the commit acknowledged all read" + held
	// Not a problem: a story started from a terminal is picked up in a session
	// when a person types /sdlc:next. Until then the hook holds no session to it, and says so
	// nowhere, so this does.
	if _, held := s.WorkingSession(); p.active != "" && !held {
		detail += "; no Claude Code session is working " + p.active + ", so nothing is enforced until a " +
			"person picks it up by typing /sdlc:next in Claude Code"
	}
	return check{Name: checkLoopState, State: stateOK, Detail: detail}
}

// stateProbe gathers what stateCheck finds, a problem at a time.
type stateProbe struct {
	s              *store.Store
	details, fixes []string
	shared         error
	active         string
}

func (p *stateProbe) problem(detail, fix string) {
	p.details = append(p.details, detail)
	p.fixes = append(p.fixes, fix)
}

// readActive checks the iteration file and the record of the story it names.
func (p *stateProbe) readActive() {
	s := p.s
	active, err := s.Active()
	if err != nil {
		p.problem(err.Error(), "nothing is enforced until .sdlc/state/active names a story: "+
			`a person runs "sdlc stop" to remove it, then "sdlc start" or types /sdlc:next in Claude Code to begin again`)
		return
	}
	p.active = active
	if active == "" || p.shared != nil {
		return
	}
	// Record starts a fresh record when there is none, which is right for
	// the command and wrong here: `sdlc start` always writes one, so a
	// missing record mid-iteration is damage, and the hook refuses the
	// commit over it and sends people to this check.
	if dir, err := s.StoryDir(active); err == nil {
		if _, statErr := os.Stat(filepath.Join(dir, model.RecordFile)); errors.Is(statErr, fs.ErrNotExist) {
			p.problem(".sdlc/stories/"+active+"/"+model.RecordFile+" is missing", "no commit goes through without it: restore it if you keep a copy, or run "+
				`"sdlc stop" to end the iteration and commit as yourself`)
		}
	}
	if _, err := s.Record(active); err != nil {
		p.problem(err.Error(), "no commit goes through while .sdlc/stories/"+active+"/"+
			model.RecordFile+` cannot be read: restore it if you keep a copy, or run "sdlc stop" `+
			"to end the iteration and commit as yourself")
	}
}

// readOtherRecords reads the records of the stories nobody is working on.
// A story nobody is working on has its record read again when it is next
// started, and a record that does not read stops that start with an error
// that calls itself a bug. So every record is read here, not only the
// active story's.
func (p *stateProbe) readOtherRecords() {
	if p.shared != nil {
		return
	}
	stories, err := p.s.StoriesOnDisk()
	if err != nil {
		p.problem(err.Error(), "no story can be started until .sdlc/stories can be listed: fix its permissions, "+
			"or move aside whatever is there in place of the directory")
	}
	for _, id := range stories {
		if id == p.active {
			continue
		}
		if _, err := p.s.Record(id); err != nil {
			p.problem(err.Error(), `"sdlc start `+id+`" fails until .sdlc/stories/`+id+"/"+model.RecordFile+
				" reads: restore it if you keep a copy, or remove it to take the story through its gates again")
		}
	}
}

// readFreeze reads the test freeze, and sets it against the record's.
func (p *stateProbe) readFreeze() {
	active := p.active
	lock, err := p.s.Lock()
	if err != nil {
		p.problem(err.Error(), "every test is treated as frozen until .sdlc/state/tests.lock reads: "+
			`restore it if you keep a copy, or lift it on the record with "/sdlc:unfreeze", or "sdlc unfreeze --reason ..." `+
			`in your own terminal and run "sdlc freeze", which freezes the tests as they are now`)
		return
	}
	if active == "" || p.shared != nil || (lock != nil && lock.Story == active) {
		return
	}
	// The hook treats every test as frozen over this, and sends people here.
	record, err := p.s.Record(active)
	if err != nil {
		return
	}
	if at, standing := record.StandingFreeze(); standing {
		p.problem("the tests for "+active+" were frozen at "+at+", and .sdlc/state/tests.lock no longer holds that freeze",
			"every test is treated as frozen until it does: restore it if you keep a copy, or lift the "+
				`freeze on the record with "/sdlc:unfreeze", or "sdlc unfreeze --reason ..." in your own terminal and run "sdlc freeze"`)
	}
}

// readAcknowledged checks the commit acknowledged is one git has.
// The log refuses a commit acknowledged that git does not have. No file is
// nothing acknowledged, which is fine. Git that does not run cannot say
// either way, and the git command check has already said why, so the commit
// goes unchecked rather than reported missing for the same cause.
func (p *stateProbe) readAcknowledged(ctx context.Context) {
	named, ok, err := p.s.Acknowledged()
	if err != nil {
		p.problem(err.Error(), `"sdlc log" cannot start where you left off until `+store.AcknowledgedFile+
			" reads: fix its permissions, or remove it to list every story")
		return
	}
	if !ok {
		return
	}
	if _, headErr := gitx.Head(ctx, p.s.Root()); headErr != nil {
		return
	}
	if _, err := gitx.Resolve(ctx, p.s.Root(), named); gitx.NamesNoCommit(err) {
		p.problem(store.AcknowledgedFile+" names "+quote(named)+", which is no commit in this repository",
			`"sdlc log" refuses to start until it does: run "/sdlc:ack", or "sdlc ack --through <commit>" in your own `+
				"terminal, or remove the file to list every story")
	}
}

// countWorktrees warns when there are more linked worktrees than the hook scans.
func (p *stateProbe) countWorktrees() {
	co, err := p.s.Checkout()
	if err != nil && co.Common == "" {
		return
	}
	if n := len(co.Worktrees()); n > store.SessionScanLimit {
		p.problem(fmt.Sprintf("this repository has %d linked worktrees, counting ones never pruned; the hook "+
			"looks through only the first %d for a session whose story no claim records yet, so such a "+
			"session in the rest is not held to the rules protecting the other checkouts until its first "+
			"writing command", n, store.SessionScanLimit),
			`run "git worktree prune", and remove worktrees no longer used`)
	}
}

// checkOrder is every check doctor makes, in the order it makes them. It is
// also what skipRest walks, so the report has the same shape whether or not it
// got all the way through.
var checkOrder = []string{
	checkGitRepository, checkGitCommand, checkConfiguration, checkBacklog, checkLoopState,
	checkProjectContract, checkCommands, checkSdlcOnPath,
}

// skipRest reports the checks after the one named that could not run, so that
// nothing appears to have silently passed. One real problem should not produce a
// cascade of unrelated ones either.
//
// Where the report stops and why it stopped are not always the same check: a
// broken configuration still lets loop state be checked, and the checks after
// it were skipped because of the configuration, not the state that passed.
func skipRest(after, because string) []check {
	var out []check
	seen := false
	for _, name := range checkOrder {
		if !seen {
			seen = name == after
			continue
		}
		out = append(out, check{Name: name, State: stateSkipped,
			Detail: "not checked: " + because + " has to work first"})
	}
	return out
}

// noMainCheckout is E0052 as a check reports it, with the why that says which
// of the layouts this is.
func noMainCheckout(e *sdlcerr.Error) (detail, fix string) {
	return e.What + ": " + e.Why + " (" + e.Code.String() + ")", e.Fix
}

// describeHolders is which checkout holds each story, where the repository has
// more than one.
func describeHolders(s *store.Store) string {
	holders, shown := holdersShown(s)
	if !shown || len(holders) == 0 {
		return ""
	}
	ids := slices.Sorted(maps.Keys(holders))
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id + " " + heldIn(id, holders[id])
	}
	return "; " + strings.Join(parts, "; ")
}

func backlogCheck(s *store.Store, root string) check {
	path := s.BacklogPath()
	backlog, err := s.Backlog()
	if err != nil {
		// "sdlc init" creates a backlog that is not there, and leaves one that is
		// alone, so it is the fix only for a missing one.
		detail, fix := err.Error(), "correct "+relativeTo(root, path)+", or point backlog.path at the file you use"
		var e *sdlcerr.Error
		switch {
		case !errors.As(err, &e):
		case e.Code == sdlcerr.BacklogMissing:
			fix = `run "sdlc init" to create it, or point backlog.path at the file you use`
		case e.Code == sdlcerr.NoMainCheckout:
			detail, fix = noMainCheckout(e)
		}
		return check{Name: checkBacklog, State: stateProblem, Detail: detail, Fix: fix}
	}
	if len(backlog.Stories) == 0 {
		return check{Name: checkBacklog, State: stateProblem,
			Detail: relativeTo(root, path) + " has no stories",
			Fix:    "add a story to it: https://github.com/bbsnly/sdlc/blob/main/docs/guides/writing-stories.md"}
	}
	noun := "stories"
	if len(backlog.Stories) == 1 {
		noun = "story"
	}
	runnable := "none runnable"
	holders, _ := holdersShown(s)
	if _, ok := pickable(backlog, holders).Next(); ok {
		runnable = "one runnable"
	}
	return check{Name: checkBacklog, State: stateOK, Detail: fmt.Sprintf("%s  (%d %s, %s)",
		relativeTo(root, path), len(backlog.Stories), noun, runnable)}
}

// gitCheck asks out loud for the thing two gates depend on. The freeze asks git
// which files are part of the working tree, and a review is stamped with the
// hash of what it reviewed -- both fail at the moment they are needed, which is
// the middle of a story, if git is not on the PATH.
//
// With worktrees on, it also holds git to the oldest version that has every
// worktree command start runs, which otherwise fails the first time a story
// begins.
func gitCheck(ctx context.Context, worktrees bool) check {
	if _, err := exec.LookPath("git"); err != nil {
		return check{Name: checkGitCommand, State: stateProblem,
			Detail: "git is not on your PATH",
			Fix: "install git -- the test freeze and every review are recorded " +
				"against what git says is in the working tree"}
	}
	if worktrees {
		least := gitx.MinimumVersion
		major, minor, err := gitx.Version(ctx)
		if err == nil && (major < least[0] || major == least[0] && minor < least[1]) {
			return check{Name: checkGitCommand, State: stateProblem,
				Detail: fmt.Sprintf("git is %d.%d, and sdlc start makes each story's worktree with commands "+
					"that came in git %d.%d", major, minor, least[0], least[1]),
				Fix: fmt.Sprintf("install git %d.%d or newer, or set git.worktrees to false in .sdlc/config.json "+
					"to begin each story in the checkout sdlc start runs in", least[0], least[1])}
		}
	}
	return check{Name: checkGitCommand, State: stateOK, Detail: "found on PATH"}
}

// storyWorktreesCheck is what start's worktrees leave for a person to do: a
// trunk a new worktree would find no configuration on, one that tracks the
// loop's state, and a story worktree that no checkout holds. It appears only
// where there is one of them, and so is not among the checks a failure earlier
// on reports as skipped. Each is a line of the detail, and its fix the same
// line of the fix.
//
// Each worktree is described by what is true of it, never by a guess at how
// it came to be there, and the fix never forces anything away: what is in it
// may be a start still running, or somebody's work.
func storyWorktreesCheck(ctx context.Context, s *store.Store) (check, bool) {
	co, err := s.Checkout()
	if err != nil {
		return check{}, false
	}
	trunk := trunkOf(s.Config())
	var details, fixes []string
	if s.Config().Git.WorktreesOn() {
		if has, err := gitx.Holds(ctx, co.Main, "refs/heads/"+trunk, config.File); err == nil && !has {
			details = append(details, "trunk, "+quote(trunk)+", has no "+config.File+
				" committed, and a story's worktree has only what trunk commits, so sdlc start refuses to begin one")
			fixes = append(fixes, "commit "+config.File+" and CLAUDE.md to trunk, or set git.worktrees to false in "+
				"this checkout's "+config.File)
		}
		if detail, fix, ok := trackedStateCheck(ctx, co.Main, trunk); ok {
			details, fixes = append(details, detail), append(fixes, fix)
		}
	}
	trees, err := gitx.Worktrees(ctx, co.Main)
	if err != nil {
		trees = nil
	}
	holders, err := s.Holders()
	if err != nil {
		// The backlog check has said why.
		trees = nil
	}
	within := filepath.Join(co.Main, config.Dir, "worktrees")
	for _, w := range trees {
		id := filepath.Base(w.Path)
		if store.CheckID(id) != nil || !config.SamePath(filepath.Dir(w.Path), within) || holdsIt(holders[id], w.Path) {
			continue
		}
		detail, fix := unheldWorktree(ctx, w, id, trunk)
		details, fixes = append(details, detail), append(fixes, fix)
	}
	if len(details) == 0 {
		return check{}, false
	}
	return check{Name: "story worktrees", State: stateProblem,
		Detail: strings.Join(details, "\n"), Fix: strings.Join(fixes, "\n")}, true
}

// trackedStateCheck says, before a story starts, that trunk tracks files under
// the loop's state directory. Every story worktree writes there, and the
// landing refuses a story whose worktree is not clean or whose commits carry
// the loop's files, so no story worked in a worktree could land.
func trackedStateCheck(ctx context.Context, main, trunk string) (detail, fix string, ok bool) {
	state := config.Dir + "/state"
	if tracks, err := gitx.Holds(ctx, main, "refs/heads/"+trunk, state); err != nil || !tracks {
		return "", "", false
	}
	return "trunk, " + quote(trunk) + ", tracks files under " + state + ", the loop's own state, which every " +
			"story worktree writes to, so no story worked in a worktree can land on trunk until trunk stops tracking them",
		`a person stops tracking them on trunk, with "git rm -r --cached ` + state + `" in the main checkout with ` +
			`trunk checked out, adds "/` + state + `/" to .gitignore, and commits both`,
		true
}

// holdsIt reports whether the checkout at dir holds the story, or is one of
// the checkouts naming it, whose tie the loop state check describes.
func holdsIt(h store.Holder, dir string) bool {
	if config.SamePath(h.Checkout, dir) {
		return true
	}
	return slices.ContainsFunc(h.Contenders, func(c store.Contender) bool { return config.SamePath(c.Checkout, dir) })
}

// unheldWorktree says which case a story worktree no checkout holds is, and
// what a person does about it. Both the changes nobody committed and the
// commits trunk lacks are said, in the order a person takes them, as SDLC-E0053
// has them: what is kept is committed on the branch before the branch goes to
// trunk. The loop's own files under .sdlc/state are not somebody's changes:
// every story worktree has them uncommitted, so a finished story's worktree
// read as "may be somebody's work", and its merge was never said.
//
// The commits are counted from the worktree's HEAD, whatever it has checked
// out: one on no branch, as a rebase stopped part-way leaves it, was called
// empty, and removing it as advised lost what only its HEAD had. Removal is
// offered only where both were read, and neither had anything.
//
// Every command in the fix is built from the story's id, which has passed
// CheckID, and trunk, and never from the branch git lists: a session can
// rename its own worktree's branch, git takes shell syntax in a name, and
// doctor had put that name into a merge a person was told to paste. A
// worktree on any other branch is named with the branch as a shell reads it
// back unexpanded, and gets no command built from it.
func unheldWorktree(ctx context.Context, w gitx.Worktree, id, trunk string) (detail, fix string) {
	rel := worktreeIn(id)
	if w.Gone {
		return id + worktreeOf + rel + ", is gone, and git still lists it",
			`"git worktree prune" in the main checkout makes git forget it`
	}
	branch := store.StoryBranch(id)
	own := w.Branch == branch
	changes, loops, err := workChanges(ctx, w.Path)
	n, countErr := gitx.HeadAhead(ctx, w.Path, trunk)
	err = errors.Join(err, countErr)
	var details, fixes []string
	if len(changes) > 0 {
		kept := "there"
		if own {
			kept = "on " + branch
		}
		details = append(details, id+worktreeOf+rel+", has changes nobody committed, and no checkout holds "+id)
		fixes = append(fixes, "the changes nobody committed may be somebody's work: a person keeps what is wanted, "+
			"committed "+kept+" or copied elsewhere, before removing anything")
	}
	switch {
	case w.Branch != "" && !own:
		details = append(details, id+worktreeOf+rel+", is on the branch "+shellWord(w.Branch)+", not "+branch+
			", and no checkout holds "+id)
		fixes = append(fixes, "that is not the branch start made: a person looks at what it holds, with "+
			lookAt(id, trunk)+" in the main checkout, before merging or removing anything")
	case err != nil:
		details = append(details, id+worktreeOf+rel+", is held by no checkout, and git could not read what it "+
			"holds")
		fixes = append(fixes, "a person looks at what it holds, with "+lookAt(id, trunk)+
			" in the main checkout, before removing anything")
	case n == 0:
	case own:
		details = append(details, fmt.Sprintf("%s's worktree, %s, and %s hold %d %s trunk does not have",
			id, rel, branch, n, plural(n, "commit", "commits")))
		fixes = append(fixes, fmt.Sprintf("the %s on %s %s the story's work: a person brings %s to trunk, "+
			`with trunk checked out, with "git merge --ff-only %s" in the main checkout, before removing `+
			"either; if git cannot fast-forward, a person first rebases %s on trunk in its worktree and runs "+
			"the tests", plural(n, "commit", "commits"), branch, plural(n, "is", "are"), plural(n, "it", "them"),
			branch, branch))
	default:
		details = append(details, fmt.Sprintf("%s's worktree, %s, is on no branch, its HEAD has %d %s trunk "+
			"does not have, and no checkout holds %s", id, rel, n, plural(n, "commit", "commits"), id))
		fixes = append(fixes, fmt.Sprintf("%s: a person looks at %s, with %s in the main checkout, and keeps what "+
			"is wanted on a branch, with %s, before merging or removing anything", lostWithIt(n), plural(n, "it", "them"),
			lookAt(id, trunk), keepOn(id)))
	}
	if len(details) > 0 {
		return strings.Join(details, "; "), strings.Join(fixes, thenClause)
	}
	remove := `"git worktree remove ` + rel + `"`
	if own {
		remove += ` and "git branch -d ` + branch + `"`
	}
	detail = id + worktreeOf + rel + ", is held by no checkout, and has nothing trunk does not"
	fix = `if no "sdlc start" is running for ` + id + `, a person can remove it with ` + remove + " in the main checkout"
	if loops {
		detail += " but the loop's own files under .sdlc/state"
		fix += "; those files are safe to lose where git refuses to remove the worktree over them"
	}
	return detail, fix
}

// workChanges are the changes in a checkout that could be somebody's work,
// and whether the loop's own files under .sdlc/state were among its changes.
func workChanges(ctx context.Context, dir string) (changes []string, loops bool, err error) {
	all, err := gitx.Changes(ctx, dir)
	if err != nil {
		return nil, false, err
	}
	for _, p := range all {
		if strings.HasPrefix(p, config.Dir+"/state/") {
			loops = true
			continue
		}
		changes = append(changes, p)
	}
	return changes, loops, nil
}

func contractCheck(root string) check {
	raw, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		return check{Name: checkProjectContract, State: stateProblem,
			Detail: "there is no CLAUDE.md",
			Fix:    `run "sdlc init" -- it writes one without touching anything else`}
	}
	if !scaffold.HasContract(string(raw)) {
		return check{Name: checkProjectContract, State: stateProblem,
			Detail: `CLAUDE.md has no "` + scaffold.ContractHeading + `" section`,
			Fix:    `run "sdlc init" -- it appends the section and leaves the rest alone`}
	}
	return check{Name: checkProjectContract, State: stateOK,
		Detail: `CLAUDE.md carries the "` + scaffold.ContractHeading + `" section`}
}

// commandChecks looks for the programs the configured commands would run. It
// does not run them: a test suite can take minutes, and doctor is meant to be
// the fast answer to "why is this not working".
func commandChecks(root string, cfg config.Config) []check {
	names := make([]string, 0, len(cfg.Commands))
	for name, command := range cfg.Commands {
		if strings.TrimSpace(command) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	if len(names) == 0 {
		return []check{{Name: checkCommands, State: stateProblem,
			Detail: "no commands are configured, so every gate that runs one is skipped",
			Fix:    "fill in commands.test and commands.lint in " + config.File}}
	}

	out := []check{{Name: checkCommands, State: stateOK, Detail: strings.Join(names, ", ")}}
	missing := map[string][]string{}
	for _, name := range names {
		for _, program := range programsIn(cfg.Commands[name]) {
			if _, err := exec.LookPath(program); err != nil {
				missing[program] = append(missing[program], name)
			}
		}
	}
	for _, program := range sortedKeys(missing) {
		out = append(out, check{
			Name:   "commands on PATH",
			State:  stateProblem,
			Detail: program + " is not installed, so " + strings.Join(missing[program], " and ") + " would fail",
			Fix: "install " + program + `, or clear commands.` + missing[program][0] +
				" in " + config.File,
		})
	}

	// The hook judges a command with a story's state, the agent's role and the
	// directory it runs from. None of that is known here, so this is the answer
	// for the main conversation at the project root with no story under way:
	// a rule that turns on the rest can be reported for a command the hook
	// allows (commit-gate on a `git commit`) and missed for one it refuses.
	// Only the key and the rule are printed, never the command or the
	// classifier's reason, which can quote the command and a command can hold
	// a token. fmt_file is run by the hook itself and never reaches the
	// classifier, so it is not judged.
	state := shellpolicy.State{Backlog: relativeTo(root, cfg.BacklogPath(root))}
	for _, name := range names {
		if f, refused := shellpolicy.Inspect(cfg.Commands[name], state); refused && name != "fmt_file" {
			out = append(out, check{
				Name:   "commands and hook",
				State:  stateProblem,
				Detail: commandsPrefix + name + " may be refused by the hook under " + f.Rule + " (judged without a story or a role)",
				Fix: "edit commands." + name + " in " + config.File + " by hand, so that it no longer does what " +
					f.Rule + " refuses; see docs/enforcement.md, under " + f.Rule,
			})
		}
	}
	return out
}

// shellBuiltins never need to be on PATH.
var shellBuiltins = map[string]bool{
	"test": true, "[": true, "echo": true, "printf": true, "true": true,
	"false": true, "cd": true, ":": true, "exit": true, "read": true,
	"esac": true, "trap": true,
}

// programsIn pulls the program names out of a shell command. It splits on the
// operators that start a new command and takes the first word of each, which is
// enough for the commands people actually configure and never guesses at more.
// Text inside quotes is never a command: `awk '/^t/{print $2; exit}' go.mod`
// has no second program to look up. A program named through a variable or a
// substitution cannot be known without running the command, so it is skipped.
func programsIn(command string) []string {
	seen := map[string]bool{}
	var out []string
	for _, segment := range splitCommand(command) {
		// What follows a closed $( ) up to the next operator is its
		// arguments or the rest of a word: `"$(go env GOPATH)"/bin/lint`.
		if segment.argument {
			continue
		}
		fields := strings.Fields(segment.text)
		// `case "$FILE" in *.go) gofmt -w "$FILE" ;; esac` runs gofmt, not
		// case: the program is the word after the pattern.
		if len(fields) > 0 && fields[0] == "case" {
			fields = afterCasePattern(fields)
		}
		// A quote left in the first word is a quoted span the scanner could not
		// read as a plain name: "$TOOL" build, or a quote never closed.
		if len(fields) == 0 || strings.ContainsAny(fields[0], `"'`) {
			continue
		}
		// The ) closes the $( that started the segment: `p=$(mktemp) && ...`.
		program := strings.Trim(fields[0], "$(<>)")
		if program == "" || shellBuiltins[program] || strings.Contains(program, "=") {
			continue
		}
		if !seen[program] {
			seen[program] = true
			out = append(out, program)
		}
	}
	return out
}

// commandSegment is one command's text with the inside of its quotes removed.
type commandSegment struct {
	text     string
	argument bool // follows a closed $( ) or backtick pair, so starts no command
}

// scanFrame is a $( ) or backtick pair that is open, and the quote it
// interrupted, which the closing restores.
type scanFrame struct {
	backtick bool
	quote    byte
}

// commandScanner splits a command at its operators the way a shell reads
// them, as far as this check needs. Unquoted, it cuts at && || | ; and a
// newline. Inside '...' nothing means anything, and inside "..." only a
// backslash, $( and a backtick do. $( ) and backticks start a command of their
// own, in double quotes too: `test -z "$(gofmt -l .)"` runs gofmt. The inside
// of a quote is dropped and the quote characters are kept. A quote never
// closed drops the rest of its command; the commands before it are still read.
//
// Not read: (...) grouping and $(( )) arithmetic, a `case` pattern inside
// $( ), $'...', here-documents and # comments. They can make part of a command
// be read as the wrong kind of text, never fail.
type commandScanner struct {
	src      string
	i        int
	segments []commandSegment
	cur      strings.Builder
	argument bool
	frames   []scanFrame
	quote    byte // 0, ' or " : the quote open in the innermost frame
}

func splitCommand(command string) []commandSegment {
	s := &commandScanner{src: command}
	for s.i < len(s.src) {
		c := s.src[s.i]
		s.i++
		switch s.quote {
		case '\'':
			s.single(c)
		case '"':
			s.double(c)
		default:
			s.bare(c)
		}
	}
	s.cut()
	return s.segments
}

// single: only the closing quote means anything, a backslash included.
func (s *commandScanner) single(c byte) {
	if c == '\'' {
		s.cur.WriteByte(c)
		s.quote = 0
	}
}

func (s *commandScanner) double(c byte) {
	switch {
	case s.substitution(c):
	case c == '"':
		s.cur.WriteByte(c)
		s.quote = 0
	case c == '\\':
		s.skip()
	}
}

func (s *commandScanner) bare(c byte) {
	switch {
	case c == '`' && s.inFrame(true), c == ')' && s.inFrame(false):
		s.close()
	case s.substitution(c), s.operator(c):
	case c == '\\':
		s.cur.WriteString(s.src[s.i-1 : min(s.i+1, len(s.src))])
		s.skip()
	case c == '\'' || c == '"':
		s.openQuote(c)
	default:
		s.cur.WriteByte(c)
	}
}

// substitution opens a frame at $( or a backtick, which both quote states
// and the unquoted text let start a command.
func (s *commandScanner) substitution(c byte) bool {
	switch {
	case c == '$' && strings.HasPrefix(s.src[s.i:], "("):
		s.i++
		s.open(false)
	case c == '`':
		s.open(true)
	default:
		return false
	}
	return true
}

// operator cuts at && | ; and a newline. || is two |.
func (s *commandScanner) operator(c byte) bool {
	switch c {
	case ';', '|', '\n':
	case '&':
		if !strings.HasPrefix(s.src[s.i:], "&") {
			return false
		}
		s.i++
	default:
		return false
	}
	s.cut()
	return true
}

// openQuote starts a quoted span, unless it is a whole plain word at the start
// of a command: `'go' build` still runs go, so the word is kept as it is.
func (s *commandScanner) openQuote(q byte) {
	if word, ok := s.plainWord(q); ok {
		s.cur.WriteString(word)
		return
	}
	s.cur.WriteByte(q)
	s.quote = q
}

// plainWord reads a quoted span that is the first word of a command and has
// nothing in it for the shell to expand or split. A word that goes on after
// the quote (`"$(go env GOPATH)"/bin/lint`) is not a name to look up.
func (s *commandScanner) plainWord(q byte) (string, bool) {
	if strings.TrimSpace(s.cur.String()) != "" {
		return "", false
	}
	end := strings.IndexByte(s.src[s.i:], q)
	if end <= 0 {
		return "", false
	}
	word := s.src[s.i : s.i+end]
	rest := s.src[s.i+end+1:]
	if strings.ContainsAny(word, " \t\n$`\\") || (rest != "" && !strings.ContainsRune(" \t\n;|&)<>", rune(rest[0]))) {
		return "", false
	}
	s.i += end + 1
	return word, true
}

func (s *commandScanner) skip() { s.i = min(s.i+1, len(s.src)) }

func (s *commandScanner) inFrame(backtick bool) bool {
	return len(s.frames) > 0 && s.frames[len(s.frames)-1].backtick == backtick
}

func (s *commandScanner) open(backtick bool) {
	s.cut()
	s.frames = append(s.frames, scanFrame{backtick: backtick, quote: s.quote})
	s.quote = 0
}

func (s *commandScanner) close() {
	s.cut()
	s.quote = s.frames[len(s.frames)-1].quote
	s.frames = s.frames[:len(s.frames)-1]
	s.argument = true
}

func (s *commandScanner) cut() {
	s.segments = append(s.segments, commandSegment{text: s.cur.String(), argument: s.argument})
	s.cur.Reset()
	s.argument = false
}

// afterCasePattern is what follows the first pattern of a case command, which
// is where its first program is.
func afterCasePattern(fields []string) []string {
	for i, f := range fields {
		if i > 0 && strings.HasSuffix(f, ")") {
			return fields[i+1:]
		}
	}
	return nil
}

// binaryCheck asks the question the hook asks. A hook that cannot find the
// binary allows the tool call and says so in a system message -- which is the right
// behaviour and easy to miss, so doctor asks it out loud.
//
// The hook's launcher takes SDLC_BIN before PATH, and PATH before where the
// installers put the binary, so doctor does too: a binary SDLC_BIN points at is
// one the hooks find. The plugin's own bin directory is the launcher's other
// place to look, and only a hook knows where that is.
//
// A relative SDLC_BIN is taken from the directory the command runs in, which
// is not always the process's.
func binaryCheck(env commandEnv) check {
	bin := env.getenv("SDLC_BIN")
	named, _ := pathrules.Abs(env.dir, bin)
	named = filepath.Clean(named)
	if bin != "" && runnable(named) {
		return check{Name: checkSdlcOnPath, State: stateOK, Detail: "SDLC_BIN: " + named}
	}
	path, err := exec.LookPath("sdlc")
	offPath := false
	if err != nil {
		if at := installedWhereTheInstallersPutIt(env.getenv); at != "" {
			path, err, offPath = at, nil, true
		}
	}
	if bin != "" {
		// The hooks pass over an SDLC_BIN that names nothing runnable and run
		// whatever else they find, so a build being tried out is quietly not
		// the one enforcing. Only doctor is in a position to say so.
		instead := "find no sdlc at all, so nothing is enforced"
		if err == nil {
			instead = "run " + path + " instead"
		}
		return check{Name: checkSdlcOnPath, State: stateProblem,
			Detail: "SDLC_BIN names " + bin + ", which is not a program, so the hooks " + instead,
			Fix:    "point SDLC_BIN at the sdlc binary, or unset it"}
	}
	if err != nil {
		return check{Name: checkSdlcOnPath, State: stateProblem,
			Detail: "the hooks cannot find sdlc, so nothing is enforced",
			Fix: `install it with "npx @bbsnly/sdlc install", which puts it where the hooks find it ` +
				`without PATH; from a checkout "./task build" prints the line to add to PATH. A desktop app ` +
				`such as Claude Desktop keeps the PATH it started with, so quit it fully and reopen it ` +
				`after changing PATH, or set SDLC_BIN to the binary`}
	}
	if offPath {
		return check{Name: checkSdlcOnPath, State: stateOK,
			Detail: path + ", where the installers put it: not on PATH, and the hooks look there anyway"}
	}
	return check{Name: checkSdlcOnPath, State: stateOK, Detail: path}
}

// installedWhereTheInstallersPutIt is the binary in the directory the install
// scripts and npx use, which the launchers look at after PATH, or "" when there
// is none. A desktop app keeps the PATH it started with, so without this the
// hooks missed a binary every new terminal found.
func installedWhereTheInstallersPutIt(getenv func(string) string) string {
	var at string
	if runtime.GOOS == goosWindows {
		local := getenv("LOCALAPPDATA")
		if local == "" {
			return ""
		}
		at = filepath.Join(local, "Programs", "sdlc", "bin", "sdlc.exe")
	} else {
		home := getenv("HOME")
		if home == "" {
			return ""
		}
		at = filepath.Join(home, ".local", "bin", "sdlc")
	}
	if !runnable(at) {
		return ""
	}
	return at
}

// runnable is the launchers' test for SDLC_BIN: a file at that path, read from
// where the hook runs, and executable where that means anything. Not
// exec.LookPath, which finds a bare name on PATH that the launchers never look
// up, so doctor said ok while the hooks ran another sdlc.
func runnable(bin string) bool {
	info, err := os.Stat(bin)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == goosWindows || info.Mode().Perm()&0o111 != 0
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}
