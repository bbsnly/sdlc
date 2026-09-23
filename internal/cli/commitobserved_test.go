package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/store"
)

// watched plants what the hook writes when it allows a command that commits:
// the HEAD the session was on at the moment the command went through. These
// tests drive the CLI with no hook running at all, so a story here has watched
// nothing unless it says so -- which is itself one of the cases below.
func watched(t *testing.T, root, id string, heads ...string) {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(&config.Project{Root: root, Config: cfg})
	for _, head := range heads {
		if err := s.ObserveCommit(t.Context(), id, head); err != nil {
			t.Fatal(err)
		}
	}
}

// onTop commits everything, with one more file under .sdlc/ in it. It moves
// HEAD and leaves both things the commit gate looks at where they were: the
// tree it compares with the reviews leaves .sdlc/ out, and `git status` finds
// nothing uncommitted afterwards.
func onTop(t *testing.T, root, name, message string) string {
	t.Helper()
	writeFile(t, root, ".sdlc/"+name, message+"\n")
	gitOut(t, root, "add", "-A")
	gitOut(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit", "--quiet", "-m", message)
	return headOf(t, root)
}

// noRange is what the record and `sdlc log` say when the loop cannot tell which
// commits are the story's: nothing, said deliberately, rather than a pair that
// would look right and be wrong.
func noRange(t *testing.T, root, id string) {
	t.Helper()
	if r := readRecord(t, root, id); r.Commit != "" {
		t.Errorf("the record names %q..%q as the story's, want no range at all",
			r.CommitBase, r.Commit)
	}
	// Said deliberately: without a marker on the record, `sdlc log` falls back
	// to reading the story's id out of the commit messages, which is a guess
	// this record has ruled out.
	if e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json"))); e.RangeSource != rangeUnknown ||
		e.Commit != "" {
		t.Errorf("sdlc log places %s at %q..%q from %q, want it reported as unknown",
			id, e.CommitBase, e.Commit, e.RangeSource)
	}
}

// AC-1, and the security review's third finding. `merge` is one of the commands
// the classifier calls a commit, so a merge the session made is one of the
// story's own -- and the commit before the oldest of the story's commits is the
// merge's FIRST parent, which leaves every commit of the merged side branch
// inside the pair. A range chosen from what was watched, and never checked
// against what it then holds, looks right and is wrong.
//
// So the pair is put back to git before it is believed: what `rev-list` lists
// over it has to be exactly the commits the loop watched, and where it is not,
// the honest answer is none.
func TestAC1ARangeThatWouldSwallowAMergedSideBranchIsNotRecorded(t *testing.T) {
	root, base, ours := landedStory(t)

	// A side branch built with plumbing, so it never touches the working tree:
	// its commits carry the base's own tree, which makes the merge below a
	// merge of somebody else's history rather than a change to the work.
	them := func(parent, message string) string {
		return gitOut(t, root, "-c", "user.email=them@example.com", "-c", "user.name=Somebody",
			"commit-tree", base+"^{tree}", "-p", parent, "-m", message)
	}
	side := them(them(base, "chore: theirs, the first"), "chore: theirs, the second")
	gitOut(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"merge", "--no-ff", "--no-edit", side)

	// The hook watched both of the session's commands: the commit, made on the
	// base, and the merge, made on that commit.
	watched(t, root, "US-001", base, ours)

	passed := decode[gatePayload](t, mustRun(t, "gate", "commit", "pass", "--note", "on trunk", "--json"))
	if passed.Commit != "" {
		t.Errorf("the gate recorded %q..%q, which holds %v; two of those are a side branch "+
			"nobody on this story made", passed.CommitBase, passed.Commit,
			revList(t, root, passed.CommitBase, passed.Commit))
	}
	noRange(t, root, "US-001")
}

// AC-1. The range ends at the last commit the loop watched, not at HEAD. A
// person's commit landing after the story's is the second half of US-004's own
// defect: its record named `fad697d`, a chore made after the work, because the
// gate wrote down whatever HEAD had become by the time it ran.
func TestAC1TheRangeEndsAtTheLastCommitTheLoopWatchedAndNotAtHead(t *testing.T) {
	root, base, first := landedStory(t)
	second := onTop(t, root, "more.md", "US-001: the rest of the work")
	theirs := onTop(t, root, "chore.md", "chore(sdlc): a person's own commit, made afterwards")
	watched(t, root, "US-001", base, first)

	passed := decode[gatePayload](t, mustRun(t, "gate", "commit", "pass", "--note", "on trunk", "--json"))
	held, want := revList(t, root, passed.CommitBase, passed.Commit), []string{second, first}
	if !slices.Equal(held, want) {
		t.Errorf("the gate recorded %q..%q, which holds %v; want the session's two commits %v "+
			"and not %s, a person's", passed.CommitBase, passed.Commit, held, want, theirs)
	}
}

// AC-3. A range is a contiguous stretch of history, so a story whose commits
// somebody else's sits between cannot be named by one pair at all. Anything a
// pair could say here says more than the loop knows, and an honest gap beats a
// plausible lie.
func TestAC3CommitsWatchedWithSomebodyElsesBetweenThemAreRecordedAsNoRange(t *testing.T) {
	root, base, _ := landedStory(t)
	theirs := onTop(t, root, "chore.md", "chore(sdlc): a person's own commit")
	onTop(t, root, "last.md", "US-001: the last of it")
	watched(t, root, "US-001", base, theirs)

	passed := mustRun(t, "gate", "commit", "pass", "--note", "the work is in "+base)
	// N3: a narrower answer is only honest if the pass says so rather than
	// going quiet about it.
	if strings.Contains(passed.stdout, "landed in") {
		t.Errorf("the gate claimed a range it cannot have:\n%s", passed.stdout)
	}
	noRange(t, root, "US-001")
}

// AC-5, and the decision Gate 3 left open in its first round: what the gate
// records for a story the loop never watched at all, with more than one commit
// since the base. A project with the plugin uninstalled, a session run with
// SDLC_ENFORCE=0, or a story that was already in flight when this version
// shipped all arrive here.
//
// It records what the released tool records today. "Unknown" is not a better
// answer where no better answer was available, it is a worse one said with
// confidence -- and a story whose range is unknown is never "already read", so
// answering it wherever the hook is absent would list every story in every
// `sdlc log` for ever. The cost is stated rather than hidden: such a project
// keeps today's over-claiming range.
func TestAC5AStoryTheLoopNeverWatchedRecordsTheWholeWindowAsItAlwaysDid(t *testing.T) {
	root, base, first := landedStory(t)
	second := onTop(t, root, "more.md", "US-001: the rest of the work")

	passed := decode[gatePayload](t, mustRun(t, "gate", "commit", "pass", "--note", "on trunk", "--json"))
	if passed.CommitBase != base || passed.Commit != second {
		t.Errorf("the gate recorded %q..%q, want %s..%s, the window the released tool records",
			passed.CommitBase, passed.Commit, base, second)
	}
	if held, want := revList(t, root, base, second), []string{second, first}; !slices.Equal(held, want) {
		t.Errorf("the range holds %v, want %v", held, want)
	}
	if e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json"))); e.RangeSource != rangeFromRecord {
		t.Errorf("sdlc log places US-001 from %q, want the record", e.RangeSource)
	}
}

// The security review's fourth finding, and the rule this repository already
// states for this exact field at internal/cli/log.go:267-271: what the record
// names is text somebody could have edited, and only what git printed goes back
// to git. A base written by an older sdlc, or edited by hand, is the third
// source of such text after the note and the observation, and it is the one
// that reaches a revision argument.
//
// A base git cannot read is a story whose commits cannot be worked out, which
// is AC-3 -- not a refusal at the last gate, on work already committed and, in
// a paused tier, already approved.
func TestAC3ABaseTheRecordNamesIsResolvedBeforeItReachesGit(t *testing.T) {
	for _, tc := range []struct {
		name, base string
		readable   bool
	}{
		{"AC-3 a base git would read as an option", "--all", false},
		{"AC-3 a base beginning with a dash", "-x", false},
		{"AC-3 a base this repository does not have", strings.Repeat("f", 40), false},
		{"AC-1 a base abbreviated, as a person writes one down", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, base, commit := landedStory(t)
			watched(t, root, "US-001", base)
			from := tc.base
			if tc.readable {
				from = base[:8]
			}
			editRecord(t, root, "US-001", func(r map[string]any) { r["commit_base"] = from })
			// The record is tracked here, as it is in any project that does
			// not ignore .sdlc/, and the commit gate refuses an unclean tree.
			// So the edit arrives the way an older sdlc's record would have:
			// committed, by somebody the loop never watched.
			gitOut(t, root, "add", "-A")
			gitOut(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test",
				"commit", "--quiet", "-m", "chore(sdlc): a record an older sdlc left")

			r := run(t, "gate", "commit", "pass", "--note", "on trunk")
			if r.code != 0 {
				t.Fatalf("the gate was refused over a base it could not read (%d):\n%s", r.code, r.stderr)
			}
			if !tc.readable {
				noRange(t, root, "US-001")
				return
			}
			// Only what git printed: what the record hands the next reader is
			// the full hash, not the eight characters it was given.
			if record := readRecord(t, root, "US-001"); record.CommitBase != base ||
				record.Commit != commit {
				t.Errorf("the record names %q..%q, want %s..%s, in full",
					record.CommitBase, record.Commit, base, commit)
			}
		})
	}
}
