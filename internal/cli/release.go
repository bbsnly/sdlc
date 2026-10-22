package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

type releasePayload struct {
	OK    bool   `json:"ok"`
	Story string `json:"story"`
	// Checkout is the checkout that held the story, and empty where none did.
	Checkout string `json:"checkout,omitempty"`
}

func newReleaseCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "release STORY",
		Short: "Let a story go from the checkout holding it, so that any checkout can start it",
		Long: "release takes a story away from the checkout of this repository that holds it.\n\n" +
			"A story is held by the checkout that started it until it is finished, so no\n" +
			"other checkout can work it by mistake. Release it when that checkout is gone,\n" +
			"or when a person wants the story worked in another one; `sdlc start` then\n" +
			"picks it up in any checkout. Nothing is moved: what the checkout that held it\n" +
			"has not committed stays there.\n\n" +
			"Run it in your own terminal: the hook refuses it from a tool call. It refuses\n" +
			"while an iteration is running on the story, and asks for a reason, which goes\n" +
			"on the story's record.",
		Example: `  sdlc release US-001 --reason "the worktree it was started in was deleted"`,
		Args:    cobra.ExactArgs(1),
		RunE:    func(cmd *cobra.Command, args []string) error { return runRelease(cmd, args[0], reason) },
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the story is moving to another checkout")
	return cmd
}

func runRelease(cmd *cobra.Command, id, reason string) error {
	if err := refuseInsideClaudeCode(cmd, "/sdlc:release"); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return sdlcerr.New(sdlcerr.ReasonRequired,
			"releasing a story needs a reason",
			"the story's record says why the story moved from the checkout that held it").
			WithFix("pass --reason with the one line that explains why " + quote(id) +
				" is moving to another checkout")
	}
	if err := store.CheckID(id); err != nil {
		return err
	}
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	if _, _, err := s.Story(id); err != nil {
		return err
	}
	holders, err := s.Holders()
	if err != nil {
		return err
	}
	h, held := holders[id]
	if !held {
		if wantJSON(cmd) {
			return emitJSON(cmd.OutOrStdout(), releasePayload{OK: true, Story: id})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Nothing to release: %s is not held by any checkout.\n", quote(id))
		return nil
	}
	if err := refuseIfRunning(id, h); err != nil {
		return err
	}
	if err := s.ReleaseClaim(id); err != nil {
		return err
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	// The reason and nothing else: the record is committed, and the
	// path of somebody's checkout is nobody else's business.
	record.Append("release", reason, s.Now())
	// What the checkout that held it stored for the map goes with the claim:
	// whoever picks the story up stores its own.
	withdrawMap(s, record, id, "the story was released")
	if err := s.SaveRecord(record); err != nil {
		return err
	}

	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), releasePayload{OK: true, Story: id, Checkout: h.Checkout})
	}
	sayReleased(cmd.OutOrStdout(), s, record, id, h)
	return nil
}

// sayReleased says what a release did and did not move.
func sayReleased(w io.Writer, s *store.Store, record *model.Record, id string, h store.Holder) {
	fmt.Fprintf(w, "Released %s: the checkout at %s no longer holds it. %s picks it up in any checkout.\n",
		id, sdlcerr.QuotePath(h.Checkout), quote("sdlc start "+id))
	if h.Live {
		fmt.Fprintf(w, "Anything the checkout at %s has not committed stays there, and is not moved.\n",
			sdlcerr.QuotePath(h.Checkout))
	}
	// Not where this checkout's own freeze is the story's: it is the checkout the
	// story was released from, moved here, and the freeze is here with it.
	if _, frozen := record.StandingFreeze(); frozen && !frozenHere(s, id) {
		fmt.Fprintln(w, "Its tests were frozen in that checkout, and the freeze does not move with it: "+
			`in the checkout that picks it up, a person lifts it with "/sdlc:unfreeze", or "sdlc unfreeze --reason ..." `+
			`in their own terminal, and the tests are frozen again with "sdlc freeze".`)
	}
}

// refuseIfRunning keeps release off a story an iteration is running on. A
// person moves a story that was put down; one still being worked is ended
// where it is worked first, or the session working it carries on with a story
// that has gone.
func refuseIfRunning(id string, h store.Holder) error {
	var what, why string
	switch {
	case h.Tied():
		named := make([]string, len(h.Contenders))
		for i, c := range h.Contenders {
			named[i] = sdlcerr.QuotePath(c.Checkout)
		}
		what = "iterations are running on " + quote(id) + " in more than one checkout of this repository"
		why = "the .sdlc/state/active of each of these names it: " + strings.Join(named, ", ")
	case !h.Claimed || h.Live && store.ActiveIn(h.Checkout) == id:
		what = "an iteration is running on " + quote(id) + " in the checkout at " + sdlcerr.QuotePath(h.Checkout)
		why = "that checkout's .sdlc/state/active names it, so the story is being worked there now"
	default:
		return nil
	}
	return sdlcerr.New(sdlcerr.IterationAlreadyActive, what, why).
		WithFix(`a person ends the iteration first, with "sdlc stop" run in each checkout named above, ` +
			`or finishes the story there; an assistant stops and tells the person`)
}
