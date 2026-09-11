package cli

import (
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
}

type unfreezePayload struct {
	OK     bool   `json:"ok"`
	Story  string `json:"story"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
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
			s, p, err := openStore()
			if err != nil {
				return err
			}
			id, err := activeStory(s, "freeze locks the tests for the story being worked on")
			if err != nil {
				return err
			}

			lock, err := s.Lock()
			if err != nil {
				return err
			}
			if lock != nil {
				stale, err := freezeIsALeftover(s, lock, id)
				if err != nil {
					return err
				}
				if !stale {
					return sdlcerr.New(sdlcerr.AlreadyFrozen,
						"the tests are already frozen",
						"they were frozen for "+lock.Story+" at "+lock.At+", covering "+
							countFiles(len(lock.Files)))
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
			if err := appendEvent(s, id, "freeze", countFiles(len(files))+" frozen"); err != nil {
				return err
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), freezePayload{
					OK: true, Story: id, Files: files, Count: len(files),
				})
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s  %s frozen\n", id, countFiles(len(files)))
			for _, f := range files {
				fmt.Fprintf(w, "  %s\n", f)
			}
			return nil
		},
	}
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
	return story.Status == model.StatusDone, nil
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
			s, _, err := openStore()
			if err != nil {
				return err
			}
			id, err := activeStory(s, "unfreeze lifts the freeze on the story being worked on")
			if err != nil {
				return err
			}
			lock, err := s.Lock()
			if err != nil {
				return err
			}
			if lock == nil {
				return sdlcerr.New(sdlcerr.NotFrozen,
					"there is no freeze to lift",
					"the acceptance tests have not been frozen for this story")
			}
			if err := s.ClearLock(); err != nil {
				return err
			}
			if err := appendEvent(s, id, "unfreeze", reason); err != nil {
				return err
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), unfreezePayload{
					OK: true, Story: id, Reason: reason, Count: len(lock.Files),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  freeze lifted on %s\n  %s\n",
				id, countFiles(len(lock.Files)), reason)
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

func countFiles(n int) string {
	if n == 1 {
		return "1 test file"
	}
	return strconv.Itoa(n) + " test files"
}
