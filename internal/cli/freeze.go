package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/testset"
)

type freezePayload struct {
	OK    bool     `json:"ok"`
	Story string   `json:"story"`
	Files []string `json:"files"`
	Count int      `json:"count"`
	Added []string `json:"added,omitempty"`
}

type unfreezePayload struct {
	OK       bool     `json:"ok"`
	Story    string   `json:"story"`
	Reason   string   `json:"reason"`
	Count    int      `json:"count"`
	Reopened []string `json:"reopened,omitempty"`
}

func newFreezeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "freeze",
		Short: "Lock the acceptance tests by content",
		Long: "freeze records what every test file contains, so that a later change to any\n" +
			"of them is visible rather than arguable.\n\n" +
			"This is the hinge the loop turns on. An agent that can edit its own\n" +
			"acceptance tests will eventually edit them -- not out of malice, just by\n" +
			"taking the shortest path to green -- and every gate after that is theatre.\n\n" +
			"Which files count as tests comes from paths.tests in .sdlc/config.json.",
		Example: "  sdlc freeze\n  sdlc freeze --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, p, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()
			id, err := activeStory(s, "freeze locks the tests for the story being worked on")
			if err != nil {
				return err
			}

			lock, err := s.Lock()
			if err != nil {
				return err
			}
			if lock == nil {
				if err := refuseALostFreeze(s, id); err != nil {
					return err
				}
			}
			if lock != nil {
				stale, err := freezeIsALeftover(s, lock, id)
				if err != nil {
					return err
				}
				if !stale {
					// A project that allows new test files after the freeze
					// takes them into it here, so that from now on they are
					// held like the rest. With nothing new to add this is a
					// second freeze over the top, and refused as one.
					if lock.Story == id && p.Config.Freeze.AllowNewTestFiles {
						added, err := addToFreeze(cmd.Context(), s, lock)
						if err != nil {
							return err
						}
						if len(added) > 0 {
							return sayFrozen(cmd, s, id, lock.Paths(), added)
						}
					}
					refusal := sdlcerr.New(sdlcerr.AlreadyFrozen,
						"the tests are already frozen",
						"they were frozen for "+lock.Story+" at "+lock.At+", covering "+
							countFiles(len(lock.Files)))
					// Another story's freeze is not this story's to lift. The
					// usual fix, unfreeze, would take it off a story that is
					// still under way.
					if lock.Story != "" && lock.Story != id {
						return refusal.WithFix(lock.Story + " still holds it: finish that story " +
							`("sdlc stop", then "sdlc start ` + lock.Story + `"), or mark it dropped ` +
							"in the backlog, which releases its freeze")
					}
					return refusal
				}
				if err := s.ClearLock(); err != nil {
					return err
				}
			}

			files, err := findTests(cmd, p, s)
			if err != nil {
				return err
			}
			hashes := make(map[string]string, len(files))
			for _, f := range files {
				sum, err := s.HashFile(f)
				if err != nil {
					return err
				}
				hashes[f] = sum
			}
			if err := s.SaveLock(model.NewLock(id, hashes, s.Now())); err != nil {
				return err
			}
			return sayFrozen(cmd, s, id, files, nil)
		},
	}
}

// sayFrozen puts a freeze on the record and reports it: the files it holds,
// and, when it was extended rather than taken, the ones just added to it.
func sayFrozen(cmd *cobra.Command, s *store.Store, id string, files, added []string) error {
	message, listed := countFiles(len(files))+" frozen", files
	if len(added) > 0 {
		message, listed = countFiles(len(added))+" added to the freeze", added
	}
	if err := appendEvent(s, id, "freeze", message); err != nil {
		return err
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), freezePayload{
			OK: true, Story: id, Files: files, Count: len(files), Added: added,
		})
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s  %s\n", id, message)
	for _, f := range listed {
		fmt.Fprintf(w, "  %s\n", f)
	}
	return nil
}

// addToFreeze adds the test files written since the freeze was taken to it, and
// returns them. A freeze whose own files have changed gets nothing added: the
// new files would go in, and the change would go through with them.
func addToFreeze(ctx context.Context, s *store.Store, lock *model.Lock) ([]string, error) {
	if changed := verifyFreeze(s, lock); len(changed) > 0 {
		return nil, brokenFreeze(changed)
	}
	added, err := unfrozenTests(ctx, s, lock)
	if err != nil {
		return nil, err
	}
	if len(added) == 0 {
		return nil, nil
	}
	for _, f := range added {
		sum, err := s.HashFile(f)
		if err != nil {
			return nil, err
		}
		lock.Files[f] = sum
	}
	if err := s.SaveLock(lock); err != nil {
		return nil, err
	}
	return added, nil
}

// refuseALostFreeze refuses to freeze a story whose freeze was taken and is
// gone without ever being lifted.
//
// Freezing again would lock the tests as they are now, whatever happened to
// them since. Removing .sdlc/state/tests.lock and running `sdlc freeze` was the
// whole of the way to edit a frozen test and have every later gate agree: the
// check that notices a changed test compares it against the freeze, and the
// freeze had just been taken again.
func refuseALostFreeze(s *store.Store, id string) error {
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	at, standing := record.StandingFreeze()
	if !standing {
		return nil
	}
	return sdlcerr.New(sdlcerr.FreezeBroken,
		"the tests for "+id+" were frozen and the freeze is gone",
		"it was taken at "+at+" and never lifted, and a freeze taken now would hold the "+
			"tests as they are today, whatever happened to them since").
		WithFix(`if a person removed it on purpose, they lift it on the record with ` +
			`"sdlc unfreeze --reason ..." in their own terminal, and then run "sdlc freeze"; ` +
			`otherwise hand it over with "sdlc escalate freeze_broken --message ..."`)
}

// freezeIsALeftover reports whether an existing freeze belongs to a story that
// has finished, which makes it this story's to replace rather than to refuse
// over.
//
// A finished story's freeze is normally lifted when the story is settled, so
// this is the second line of defence: a project that ran a story before that
// existed, or one whose last iteration ended some other way, would otherwise
// be stuck at Gate 3 forever with the only way out an override meant for
// something else.
//
// A dropped story is finished too, as far as its freeze is concerned: nobody
// is coming back to it, and its freeze is how "drop it" was offered as the way
// past one.
//
// A freeze naming a story that is still in progress is not a leftover, and
// still refuses. So does one naming a story that is not in the backlog at all:
// that is a state nobody should walk past.
func freezeIsALeftover(s *store.Store, lock *model.Lock, active string) (bool, error) {
	if lock.Story == "" || lock.Story == active {
		return false, nil
	}
	story, _, err := s.Story(lock.Story)
	if err != nil {
		var known *sdlcerr.Error
		if errors.As(err, &known) && known.Code == sdlcerr.StoryNotFound {
			return false, nil
		}
		return false, err
	}
	return story.Status == model.StatusDone || story.Status == model.StatusDropped, nil
}

func newUnfreezeCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "unfreeze",
		Short: "Lift the test freeze, on the record",
		Long: "unfreeze lets the acceptance tests change again.\n\n" +
			"It asks for a reason because that is the whole point: lifting the freeze is\n" +
			"sometimes right -- a test encoded the wrong behaviour -- and it is exactly\n" +
			"the move an agent would make to get to green. Recording why turns one into\n" +
			"a decision somebody can review and the other into evidence.",
		Example: `  sdlc unfreeze --reason "AC-2's test asserted the old error message"`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(reason) == "" {
				return sdlcerr.New(sdlcerr.ReasonRequired,
					"lifting the freeze needs a reason",
					"the freeze is what stops an agent editing its way to green, so "+
						"lifting it belongs on the record")
			}
			s, _, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()
			id, err := activeStory(s, "unfreeze lifts the freeze on the story being worked on")
			if err != nil {
				return err
			}
			// A freeze that will not read, or one that went missing without
			// being lifted, is lifted here like any other. Freezing again is
			// refused over both, and this is the one way past them that puts a
			// reason on the record.
			lock, err := s.Lock()
			var unreadable *sdlcerr.Error
			if err != nil && (!errors.As(err, &unreadable) || unreadable.Code != sdlcerr.StateUnreadable) {
				return err
			}
			if err == nil && lock == nil {
				record, err := s.Record(id)
				if err != nil {
					return err
				}
				if _, standing := record.StandingFreeze(); !standing {
					return sdlcerr.New(sdlcerr.NotFrozen,
						"there is no freeze to lift",
						"the acceptance tests have not been frozen for this story")
				}
			}
			// The freeze belongs to the story it was taken for. Lifting another
			// story's freeze from this one put the event on the wrong record and
			// left that story's tests editable when it was picked up again --
			// and "already frozen" on this story used to send people here.
			if lock != nil && lock.Story != "" && lock.Story != id {
				return sdlcerr.New(sdlcerr.NotFrozen,
					"there is no freeze on "+quote(id)+" to lift",
					"the tests are frozen for "+lock.Story+", which is not the story being worked on").
					WithFix("finish " + lock.Story + ` ("sdlc stop", then "sdlc start ` + lock.Story +
						`") and lift it there if it has to be lifted, or mark it dropped in the backlog`)
			}
			if err := s.ClearLock(); err != nil {
				return err
			}
			// The tests are about to change, so Gate 3 no longer stands, and
			// nor does anything passed on top of it. Left passed, the loop
			// carried on from where it was: nobody was sent back to change the
			// test, the design review passed on tests nobody had frozen, and the
			// first gate to notice said to freeze the wrong test again.
			record, err := s.Record(id)
			if err != nil {
				return err
			}
			record.Append("unfreeze", reason, s.Now())
			reopened := record.Reopen(model.GateTestsFrozen, "the freeze was lifted: "+reason, s.Now())
			if err := s.SaveRecord(record); err != nil {
				return err
			}
			held := 0
			if lock != nil {
				held = len(lock.Files)
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), unfreezePayload{
					OK: true, Story: id, Reason: reason, Count: held, Reopened: reopened,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  freeze lifted on %s\n  %s\n",
				id, countFiles(held), reason)
			if len(reopened) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  reopened: %s\n", strings.Join(reopened, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the frozen tests have to change")
	return cmd
}

// findTests lists the project's test files as git sees the working tree, so
// that build output and vendored code cannot be mistaken for acceptance tests.
func findTests(cmd *cobra.Command, p *config.Project, s *store.Store) ([]string, error) {
	m := testset.New(p.Config.Paths.Tests)
	if !m.Configured() {
		return nil, sdlcerr.New(sdlcerr.NoTestsFound,
			"this project has not said what a test file looks like",
			"paths.tests in "+config.File+" names no directories and no patterns, "+
				"so there is nothing to freeze")
	}
	all, err := gitx.Files(cmd.Context(), s.Root())
	if err != nil {
		return nil, err
	}
	files := m.Filter(all)
	if len(files) == 0 {
		return nil, sdlcerr.New(sdlcerr.NoTestsFound,
			"there are no test files to freeze",
			"nothing in the repository matches paths.tests in "+config.File)
	}
	return files, nil
}

// verifyFreeze reports the frozen files that no longer match what was recorded.
func verifyFreeze(s *store.Store, lock *model.Lock) []string {
	var changed []string
	for _, path := range lock.Paths() {
		sum, err := s.HashFile(path)
		switch {
		case err != nil:
			changed = append(changed, path+" (gone)")
		case sum != lock.Files[path]:
			changed = append(changed, path)
		}
	}
	return changed
}

// brokenFreeze is the refusal for frozen files that are not what was frozen.
func brokenFreeze(changed []string) error {
	return sdlcerr.New(sdlcerr.FreezeBroken,
		"the frozen tests are not what was frozen",
		oneLine(strings.Join(changed, ", "))+" changed after the freeze was taken")
}

// unfrozenTests lists the project's test files that the freeze does not hold,
// found the way the freeze found the ones it does.
func unfrozenTests(ctx context.Context, s *store.Store, lock *model.Lock) ([]string, error) {
	m := testset.New(s.Config().Paths.Tests)
	if !m.Configured() {
		return nil, nil
	}
	all, err := gitx.Files(ctx, s.Root())
	if err != nil {
		return nil, err
	}
	var unheld []string
	for _, f := range m.Filter(all) {
		if !lock.Holds(f) {
			unheld = append(unheld, f)
		}
	}
	return unheld, nil
}

func countFiles(n int) string {
	if n == 1 {
		return "1 test file"
	}
	return strconv.Itoa(n) + " test files"
}
