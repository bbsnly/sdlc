package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

type nextUp struct {
	Story string `json:"story"`
	Title string `json:"title"`
}

type statusPayload struct {
	OK     bool   `json:"ok"`
	Active string `json:"active,omitempty"`
	Title  string `json:"title,omitempty"`
	// NotInBacklog is set when the active story has gone from the backlog, so
	// that an empty title is not mistaken for a story with no title.
	NotInBacklog bool `json:"not_in_backlog,omitempty"`
	// Session is the Claude Code session working the active story, the only
	// one the hook holds to it. Empty, nothing is enforced anywhere until a
	// session picks the story up.
	Session string            `json:"session,omitempty"`
	Gates   map[string]string `json:"gates,omitempty"`
	// NextGate is where a resumed loop picks up. A skill reads this rather
	// than working out the gate order for itself, which is the kind of
	// derivation that drifts from the tool that enforces it.
	NextGate string         `json:"next_gate,omitempty"`
	Backlog  map[string]int `json:"backlog"`
	Next     *nextUp        `json:"next,omitempty"`
	Freeze   *freezeState   `json:"freeze,omitempty"`
	Cost     *costState     `json:"cost,omitempty"`
	// Staffing is "none" or "recorded" for the active story where the project
	// has the staffing on, so a session knows whether to ask for one.
	Staffing string `json:"staffing,omitempty"`
	// Waiting is every story handed to a person and not yet answered. The
	// iteration ended when it was handed over, so nothing else here says so.
	Waiting []waitingStory `json:"waiting,omitempty"`
	// Held is every story a checkout of the repository holds, where the
	// repository has more than one checkout.
	Held []heldStory `json:"held,omitempty"`
}

// waitingStory is one question the loop stopped to ask a person.
type waitingStory struct {
	Story   string `json:"story"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Message string `json:"message"`
	At      string `json:"at"`
}

// costState is what the story has cost so far, against what it was expected to.
// A runner reads this to decide whether to start another iteration; a person
// reads it to find out before the invoice does.
type costState struct {
	SpentUSD  float64 `json:"spent_usd"`
	BudgetUSD float64 `json:"budget_usd,omitempty"`
	Fraction  float64 `json:"fraction,omitempty"`
}

// freezeState is what the freeze looks like from outside: how many acceptance
// tests it covers, and whether they are still what was frozen. A skill reads
// this to know whether Gate 3 has actually happened.
type freezeState struct {
	Story   string   `json:"story"`
	At      string   `json:"at"`
	Files   int      `json:"files"`
	Intact  bool     `json:"intact"`
	Changed []string `json:"changed,omitempty"`
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what the loop is working on and where it has got to",
		Long: "status answers three questions: which story is being worked on, which\n" +
			"gates it has passed, and what is waiting in the backlog.\n\n" +
			"It changes nothing, so it is safe to run at any point.",
		Example: "  sdlc status\n  sdlc status --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, p, err := openStore(cmd)
			if err != nil {
				return err
			}
			active, err := s.Active()
			if err != nil {
				return err
			}
			backlog, err := s.Backlog()
			if err != nil {
				return err
			}

			waiting, err := waitingStories(s, backlog)
			if err != nil {
				return err
			}

			holders, shown := holdersShown(s)
			payload := statusPayload{OK: true, Active: active, Backlog: countStatuses(backlog), Waiting: waiting}
			if shown {
				payload.Held = heldStories(backlog, holders)
			}
			var record *model.Record
			if active != "" {
				if story, ok := backlog.Find(active); ok {
					payload.Title = story.Title
				} else {
					payload.NotInBacklog = true
				}
				if record, err = s.Record(active); err != nil {
					return err
				}
				payload.Gates = map[string]string{}
				for gate, result := range record.Gates {
					payload.Gates[string(gate)] = string(result.Status)
				}
				if next, ok := record.NextGate(); ok {
					payload.NextGate = string(next)
				}
				if payload.Freeze, err = describeFreeze(cmd.Context(), s, active, record); err != nil {
					return err
				}
				payload.Cost = describeCost(p.Config.Budget, record)
				if p.Config.Staffing.Enabled {
					payload.Staffing = "none"
					if staffed(record) {
						payload.Staffing = "recorded"
					}
				}
				if id, held := s.WorkingSession(); held {
					payload.Session = id
				}
			} else if sel, ok := pickable(backlog, holders).Next(); ok && !isWaiting(waiting, sel.Story.ID) {
				// A waiting story is the one start would pick, and start refuses
				// it, so offering it as next up would send the reader into a wall.
				payload.Next = &nextUp{Story: sel.Story.ID, Title: sel.Story.Title}
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), payload)
			}

			// An active story with no gate left is finished but not yet put
			// down, and calling that "in progress" would invite a session to
			// carry on working something that has nothing left to work.
			finished := active != "" && payload.NextGate == ""
			notHere, notHereNext := "", ""
			if active != "" {
				notHere, notHereNext = notHeldHere(active, finished, holders, shown, s.HasLinkedWorktree())
			}

			w := cmd.OutOrStdout()
			if active == "" {
				fmt.Fprintln(w, "No iteration running.")
			} else {
				state := "in progress"
				if finished {
					state = "finished   "
				}
				title := oneLine(payload.Title)
				if payload.NotInBacklog {
					title = "(not in the backlog)"
				}
				fmt.Fprintf(w, "%s  %s  %s%s\n\n", active, state, title, notHere)
				printGates(w, record)
				printFreeze(w, payload.Freeze)
				printCost(w, payload.Cost)
				if payload.Staffing != "" {
					fmt.Fprintf(w, "\n  staffing  %s\n", describeStaffing(record))
				}
				if payload.NextGate != "" {
					fmt.Fprintf(w, "\n  next   %s\n", payload.NextGate)
				}
				// Started from a terminal, or with the record of the session
				// spoiled, the story holds no session, and nothing tells the
				// sessions in the repository so: the hook speaks only in the one
				// working it. Not for a story that cannot be carried on, where it
				// would send the reader towards a commit that is refused.
				if payload.Session == "" && !finished && !payload.NotInBacklog && notHereNext == "" {
					fmt.Fprint(w, "\n  No Claude Code session is working it, so nothing is enforced "+
						"until a person picks it up by typing /sdlc:next in Claude Code.\n")
				}
			}
			fmt.Fprintf(w, "\n  backlog  %s\n", describeCounts(payload.Backlog))
			for _, ws := range payload.Waiting {
				fmt.Fprintf(w, "  waiting  %s  %s: %s\n", ws.Story, oneLine(ws.Type), oneLine(ws.Message))
			}
			for _, hs := range payload.Held {
				fmt.Fprintf(w, "  held     %s  %s\n", hs.Story, strings.TrimPrefix(heldIn(hs.Story, holders[hs.Story]), "held "))
			}
			switch {
			case payload.NotInBacklog:
				// Carrying on would end at the commit gate, which refuses a story
				// whose risk tier it cannot read.
				fmt.Fprintf(w, "\n%s is not in %s any more, so it cannot be committed. "+
					"Put it back, or run `sdlc stop` to end the iteration.\n", active, s.Config().Backlog.Path)
			case finished && inItsWorktree(s, active):
				// Not done until it lands, and the stop here is what lands it.
				fmt.Fprintf(w, "\nEvery gate has passed. Run `sdlc stop` here to land it on %s.\n", trunkName(s))
			case finished:
				fmt.Fprint(w, "\nEvery gate has passed. Run `sdlc stop` to end the iteration.\n")
			case notHereNext != "":
				fmt.Fprintf(w, "\n%s\n", notHereNext)
			case active == "" && payload.Next == nil && len(payload.Waiting) > 0:
				fmt.Fprintf(w, "\nA person answers first, in their own terminal: `sdlc approve %s`, "+
					"or `sdlc approve %s --reject \"why\"`.\n", payload.Waiting[0].Story, payload.Waiting[0].Story)
			case active != "":
				fmt.Fprint(w, "\nA person types /sdlc:next in Claude Code to carry on, "+
					"or runs `sdlc stop` to put it down.\n")
			case payload.Next != nil:
				fmt.Fprintf(w, "  next up  %s  %s\n", payload.Next.Story, oneLine(payload.Next.Title))
				fmt.Fprint(w, "\nA person runs `sdlc start`, or types /sdlc:next in Claude Code, to begin.\n")
			default:
				fmt.Fprint(w, nothingToStart(backlog, s.Config().Backlog.Path, elsewhere(backlog, holders)))
			}
			return nil
		},
	}
}

// notHeldHere is what the story line adds about an iteration this checkout is
// not the one to carry on: its active names a story another checkout holds, or
// one nobody holds, which start passes over where there is another checkout.
// next is what status then closes with instead of sending the reader to carry
// on, which start here would refuse. Nothing for a finished story, which only
// "sdlc stop" is left to do.
//
// linked is whether the repository has a linked worktree. Without one, start
// carries on an active nothing holds, as decideStart does, so status says
// nothing about it: a claim naming a gone checkout shows holders, and status
// said start would not resume a story it did.
func notHeldHere(active string, finished bool, holders map[string]store.Holder, shown, linked bool) (mark, next string) {
	if !shown || finished {
		return "", ""
	}
	h, ok := holders[active]
	switch {
	case !ok && !linked:
		return "", ""
	case !ok:
		return `  (held by no checkout: "sdlc start" here does not resume it, and picks from the backlog)`,
			"`sdlc start` here will not carry " + active + " on; a person runs `sdlc stop` to put it down."
	case h.Tied():
		return "  (" + heldIn(active, h) + ")",
			"More than one checkout names " + active + ": a person keeps the one whose session they recognise, " +
				"and runs `sdlc stop` in each of the others."
	case !h.Live:
		return "  (" + heldIn(active, h) + ")",
			"The checkout holding " + active + " is gone: a person moves it here with " +
				"`sdlc release " + active + " --reason \"...\"` in their own terminal, then `sdlc start " + active + "`."
	case !h.Here:
		return "  (" + heldIn(active, h) + ")",
			"This checkout's copy of " + active + " is stale: a person runs `sdlc stop` here to put it down, " +
				"and the story carries on in the checkout named above."
	}
	return "", ""
}

// waitingStories is every story handed to a person that nobody has answered.
func waitingStories(s *store.Store, b *model.Backlog) ([]waitingStory, error) {
	var waiting []waitingStory
	for i := range b.Stories {
		story := &b.Stories[i]
		if story.Status != model.StatusAwaitingHuman {
			continue
		}
		record, err := s.Record(story.ID)
		if err != nil {
			return nil, err
		}
		if e, ok := record.PendingEscalation(); ok {
			waiting = append(waiting, waitingStory{
				Story: story.ID, Title: story.Title, Type: e.Type, Message: e.Message, At: e.At,
			})
		}
	}
	return waiting, nil
}

func isWaiting(waiting []waitingStory, id string) bool {
	for _, ws := range waiting {
		if ws.Story == id {
			return true
		}
	}
	return false
}

// describeFreeze reports the freeze as it applies to this story. A freeze taken
// for a different story is stale, and reporting it here would be worse than
// reporting nothing: a skill would read it as this story's tests being locked.
//
// In the story's own worktree a frozen test that only trunk changed is not one
// that changed since, as no gate holds it against the story.
func describeFreeze(ctx context.Context, s *store.Store, story string, record *model.Record) (*freezeState, error) {
	lock, err := s.Lock()
	if err != nil || lock == nil || lock.Story != story {
		return nil, err
	}
	changed := verifyFreeze(s, lock)
	if len(changed) > 0 && record.Cut != "" && inItsWorktree(s, story) {
		if h, err := heldTests(ctx, s, story, lock, freezeAt{cut: record.Cut}); err == nil {
			changed = h.broken
		}
	}
	return &freezeState{
		Story: lock.Story, At: lock.At, Files: len(lock.Files),
		Intact: len(changed) == 0, Changed: changed,
	}, nil
}

func printFreeze(w io.Writer, f *freezeState) {
	switch {
	case f == nil:
		fmt.Fprintf(w, "\n  tests  not frozen\n")
	case f.Intact:
		fmt.Fprintf(w, "\n  tests  %s frozen at %s\n", countFiles(f.Files), oneLine(f.At))
	default:
		fmt.Fprintf(w, "\n  tests  %s frozen at %s, and %s changed since\n",
			countFiles(f.Files), oneLine(f.At), oneLine(strings.Join(f.Changed, ", ")))
	}
}

func countStatuses(b *model.Backlog) map[string]int {
	counts := map[string]int{}
	for i := range b.Stories {
		counts[string(b.Stories[i].Status)]++
	}
	return counts
}

// describeCounts renders the backlog in the order a story moves through it, so
// the same backlog always reads the same way.
func describeCounts(counts map[string]int) string {
	var parts []string
	seen := map[string]bool{}
	for _, s := range model.Statuses {
		if n := counts[string(s)]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ReplaceAll(string(s), "_", " ")))
			seen[string(s)] = true
		}
	}
	// A status the backlog invented still gets counted rather than vanishing.
	var extra []string
	for name := range counts {
		if !seen[name] {
			extra = append(extra, fmt.Sprintf("%d %s", counts[name], name))
		}
	}
	sort.Strings(extra)
	parts = append(parts, extra...)

	if len(parts) == 0 {
		return "empty"
	}
	return strings.Join(parts, " · ")
}

// describeCost is nil until something has been spent or a budget is set, so a
// project that does not track cost never has to look at a line about it.
func describeCost(b config.Budget, record *model.Record) *costState {
	spent := record.SpentUSD()
	if spent == 0 && b.PerStoryUSD <= 0 {
		return nil
	}
	c := &costState{SpentUSD: spent, BudgetUSD: b.PerStoryUSD}
	if b.PerStoryUSD > 0 {
		c.Fraction = spent / b.PerStoryUSD
	}
	return c
}

func printCost(w io.Writer, c *costState) {
	switch {
	case c == nil:
	case c.BudgetUSD > 0:
		fmt.Fprintf(w, "  cost   %s of %s (%s)\n", money(c.SpentUSD), money(c.BudgetUSD), percent(c.Fraction))
	default:
		fmt.Fprintf(w, "  cost   %s\n", money(c.SpentUSD))
	}
}
