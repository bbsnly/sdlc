package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// startPlan is what `sdlc start` is to do: carry on with the iteration this
// checkout is running, or start the story it names, picked up again or begun.
type startPlan struct {
	id string
	// running is the iteration this checkout's active names, carried on.
	running bool
	// fresh is a story begun rather than picked up again, which trunk is
	// checked for first.
	fresh bool
	// refused is why a story named to start cannot begin: dropped, blocked,
	// done or waiting on a dependency. Start reports it after trunk has been
	// checked, where it always has, so that a person sees the same refusal
	// first that they saw before.
	refused error
	// at is the checkout start acts in where it is not this one: the story's
	// own worktree, which holds the story or has the session bound in it.
	at string
	// passedOver is every story start passed over when it picked, because the
	// checkout holding it no longer exists. It is filled even where picking
	// then fails, which is when a person most needs to hear it.
	passedOver []goneHolder
}

// goneHolder is a story held by a checkout that no longer exists.
type goneHolder struct {
	id, checkout string
	// waiting is a story waiting on a person, which carries on with `sdlc
	// approve` rather than `sdlc start`.
	waiting bool
}

// decideStart is the whole of what start decides, from reads alone. It is asked
// twice: without the lock, to know whether trunk has to be checked before a new
// story begins, and again under it, to act. One function, so that the two
// answers cannot drift apart.
func decideStart(s *store.Store, asked, session string) (startPlan, error) {
	if checkout, id := s.SessionHolder(session, 0); checkout != "" {
		// A session working a story in the story's own worktree carries it on
		// from the main checkout as it would from the worktree: the worktree is
		// where start put it, and a person types /sdlc:next in either.
		if (asked == "" || asked == id) && s.IsStoryWorktree(checkout, id) {
			finished, err := refuseIfFinished(s, id)
			if err != nil {
				return startPlan{}, err
			}
			if finished != nil {
				return startPlan{}, finished.WithFix(`run "sdlc stop" in the story's worktree, .sdlc/worktrees/` + id +
					" in the main checkout, to end the iteration -- the story is done, and there is nothing left to resume")
			}
			return startPlan{id: id, running: true, at: checkout}, nil
		}
		return startPlan{}, sdlcerr.New(sdlcerr.StoryHeldElsewhere,
			"this Claude Code session is working "+quote(id)+" in another checkout of this repository",
			"the checkout at "+quote(checkout)+" holds it, and .sdlc/state/session there records this "+
				"session as the one working it: a session works one story, in the checkout that holds it").
			// Not E0050's fix, which moves a story: this one is running, and
			// what the person wants is a second session, not the story here.
			WithFix("a person opens a new Claude Code session in this checkout to work another story here, " +
				"or goes back to the checkout named above to carry on with this one; " +
				"an assistant stops and tells the person")
	}

	active, err := s.Active()
	if err != nil {
		return startPlan{}, err
	}
	if active != "" {
		if of, ok := s.StoryWorktreeOf(); ok && of == active && asked != "" && asked != active &&
			store.CheckID(asked) == nil {
			// Not the single checkout's why and fix: stories run side by side
			// here, each in a worktree of its own, and sdlc stop would end
			// this one. The id is checked because the fix is a command a
			// person pastes.
			return startPlan{}, sdlcerr.New(sdlcerr.IterationAlreadyActive,
				"an iteration is already running on "+quote(active),
				"this checkout is the worktree of "+quote(active)+", and a checkout works one story at a time").
				WithFix(`a person runs "sdlc start ` + asked + `" in the main checkout, which gives it a worktree of ` +
					"its own, and works it there in another Claude Code session; an assistant stops and tells the person")
		}
		if asked != "" && asked != active {
			return startPlan{}, sdlcerr.New(sdlcerr.IterationAlreadyActive,
				"an iteration is already running on "+quote(active),
				"the loop works one story at a time, which is what keeps a diff small "+
					"enough to review honestly")
		}
		finished, err := refuseIfFinished(s, active)
		if err != nil {
			return startPlan{}, err
		}
		if finished != nil {
			return startPlan{}, finished.WithFix(`run "sdlc stop" to end the iteration -- ` +
				"the story is done, and there is nothing left to resume")
		}
		if !s.OthersMayHold() {
			// No other checkout to hold it: carried on, backlog or no backlog.
			return startPlan{id: active, running: true}, nil
		}
		holders, err := s.Holders()
		if err != nil {
			return startPlan{}, err
		}
		h, held := holders[active]
		switch {
		case held && h.Here && !h.Tied():
			return startPlan{id: active, running: true}, nil
		case held && (h.Tied() || !h.Live):
			return startPlan{}, store.HeldElsewhereError(active, h, "")
		case held:
			// An active copied in with the tree, or left behind when the story
			// moved: the story carries on where it is held, not here.
			return startPlan{}, sdlcerr.New(sdlcerr.StoryHeldElsewhere,
				"this checkout's .sdlc/state/active names "+quote(active)+
					", which another checkout of this repository holds",
				"the checkout at "+quote(h.Checkout)+" holds it, and a story is worked in one checkout at a time").
				WithFix(`a person runs "sdlc stop" in this checkout to put its stale copy of the story down, ` +
					`and the story carries on in the checkout named above; an assistant stops and tells the person`)
		case !s.HasLinkedWorktree():
			// A story a person set aside by hand while it ran is carried on, as
			// it always was, where there is no other checkout to be confused with.
			return startPlan{id: active, running: true}, nil
		}
		// With other checkouts about, an active nothing holds by is not a
		// story this checkout is working: start picks as if it named none.
	}

	var plan startPlan
	id := asked
	if id == "" {
		if id, plan.passedOver, err = nextRunnable(s); err != nil {
			return plan, err
		}
	}
	story, backlog, err := s.Story(id)
	if err != nil {
		return plan, err
	}
	finished, err := refuseIfFinished(s, id)
	if err != nil {
		return plan, err
	}
	if finished != nil {
		return plan, finished
	}
	// Named, a story held in its own worktree is picked up there: resumed, or
	// taken over by this session. A worktree a person made keeps its story to
	// itself, as every other checkout does.
	if at, ok, err := heldInItsWorktree(s, id); err != nil || ok {
		if err == nil {
			err = refuseIfWaiting(s, id)
		}
		if err != nil {
			return plan, err
		}
		plan.id, plan.at, plan.running = id, at, store.ActiveIn(at) == id
		return plan, nil
	}
	if err := refuseIfHeldElsewhere(s, id, "a story is worked in one checkout at a time"); err != nil {
		return plan, err
	}
	if err := refuseIfWaiting(s, id); err != nil {
		return plan, err
	}
	plan.id = id
	if resuming(story.Status) {
		return plan, nil
	}
	plan.fresh, plan.refused = true, refuseIfNotRunnable(backlog, story)
	return plan, nil
}

// heldInItsWorktree is the story's own worktree, at .sdlc/worktrees/<ID>,
// where that worktree is another checkout than this one and holds the story.
func heldInItsWorktree(s *store.Store, id string) (string, bool, error) {
	h, elsewhere, err := s.HeldElsewhere(id)
	if err != nil || !elsewhere || h.Tied() || !h.Live || !s.IsStoryWorktree(h.Checkout, id) {
		return "", false, err
	}
	return h.Checkout, true, nil
}

// refuseIfHeldElsewhere keeps a command off a story another checkout of the
// repository holds. why says what makes the command that checkout's to run.
func refuseIfHeldElsewhere(s *store.Store, id, why string) error {
	h, elsewhere, err := s.HeldElsewhere(id)
	if err != nil || !elsewhere {
		return err
	}
	return store.HeldElsewhereError(id, h, why)
}

// elsewhere is every story another checkout holds, or that more than one
// names, in the order the backlog has them.
func elsewhere(b *model.Backlog, holders map[string]store.Holder) []string {
	var out []string
	for i := range b.Stories {
		if h, ok := holders[b.Stories[i].ID]; ok && (!h.Here || h.Tied()) {
			out = append(out, b.Stories[i].ID)
		}
	}
	return out
}

// pickable is the backlog start picks from here: without the stories other
// checkouts hold.
func pickable(b *model.Backlog, holders map[string]store.Holder) *model.Backlog {
	return b.Without(elsewhere(b, holders)...)
}

// holdersShown is who holds each story, and whether output names them: where
// the repository has another checkout, or a claim names one. A single checkout
// prints what it always has. What cannot be worked out is not shown; the
// command that needs it says why.
func holdersShown(s *store.Store) (map[string]store.Holder, bool) {
	holders, err := s.Holders()
	if err != nil {
		return nil, false
	}
	return holders, s.ShowsHolders(holders)
}

// heldStory is one story a checkout holds, as status reports it.
type heldStory struct {
	Story string `json:"story"`
	// Checkout is the checkout holding the story, and empty where more than
	// one names it and none holds it.
	Checkout string `json:"checkout,omitempty"`
	Here     bool   `json:"here,omitempty"`
	// Gone is a checkout that is no longer one of this repository's, whose
	// story only a person's "sdlc release" lets go.
	Gone bool `json:"gone,omitempty"`
	// Contenders are the checkouts naming a story none of them holds.
	Contenders []string `json:"contenders,omitempty"`
}

// heldStories is every story a checkout holds, in the order the backlog has
// them.
func heldStories(b *model.Backlog, holders map[string]store.Holder) []heldStory {
	var out []heldStory
	for i := range b.Stories {
		h, ok := holders[b.Stories[i].ID]
		if !ok {
			continue
		}
		row := heldStory{Story: b.Stories[i].ID, Checkout: h.Checkout, Here: h.Here, Gone: !h.Tied() && !h.Live}
		for _, c := range h.Contenders {
			row.Contenders = append(row.Contenders, c.Checkout)
		}
		out = append(out, row)
	}
	return out
}

// heldIn is where story id is held, as a person reads it. A gone holder names
// the whole release command: run as `sdlc release` alone, it is refused for
// want of a reason.
func heldIn(id string, h store.Holder) string {
	switch {
	case h.Tied():
		named := make([]string, len(h.Contenders))
		for i, c := range h.Contenders {
			named[i] = quote(c.Checkout)
		}
		return "held by none: more than one checkout names it (" + strings.Join(named, ", ") + ")"
	case !h.Live:
		return "held in " + quote(h.Checkout) + `, gone: see sdlc release ` + id + ` --reason "..."`
	case h.Here:
		return "held in " + quote(h.Checkout) + ", this checkout"
	}
	return "held in " + quote(h.Checkout)
}

// releaseNote is what start and approve say about a story a person moved
// here with `sdlc release`. The record names no checkout, so neither does it,
// and it cannot tell a checkout moved here from any other.
const releaseNote = "sdlc: %s was released from the checkout that held it. Work the old checkout had " +
	"not committed stayed with that checkout. If this checkout is that one, moved here, the work is here.\n"

// releasedSinceStart reports whether the story was released since its
// iteration last started.
func releasedSinceStart(record *model.Record) bool {
	for i := len(record.Events) - 1; i >= 0; i-- {
		switch record.Events[i].Type {
		case "release":
			return true
		case "loop_start":
			return false
		}
	}
	return false
}

// noteRelease says nothing in a checkout whose freeze is the story's: that is
// the checkout the story was released from, moved here, and its work is here.
// Told the work was somewhere else, a person went looking for what they had.
func noteRelease(w io.Writer, s *store.Store, id string) {
	if frozenHere(s, id) {
		return
	}
	fmt.Fprintf(w, releaseNote, id)
}

// frozenHere reports whether this checkout's .sdlc/state/tests.lock is the
// story's freeze.
func frozenHere(s *store.Store, id string) bool {
	lock, err := s.Lock()
	return err == nil && lock != nil && lock.Story == id
}

// startSaid is what start says once it knows how it ended: the stories it
// passed over, and the one it went on to.
type startSaid struct {
	passed []goneHolder
	went   string
}

// notePassedOver tells the person about each story start passed over because
// the checkout holding it no longer exists. A single checkout moved while its
// story was stopped is one: its claim still names the old path, and start then
// began the next story, or refused over the stopped story's own work, with
// nothing said about either. Start cannot tell a checkout that was deleted from
// one that was moved, so the note says what to do in both. Where start carries
// a story on, or refuses one it was asked for, it picks nothing and passes
// nothing over.
func notePassedOver(w io.Writer, said *startSaid) {
	for _, g := range said.passed {
		on := ""
		if said.went != "" {
			on = " and went on to " + said.went
		}
		last := "sdlc start " + g.id
		if g.waiting {
			last = "sdlc approve " + g.id
		}
		fmt.Fprintf(w, "sdlc: %s is held by a checkout that no longer exists, %s, so start passed it over%s. "+
			"If this checkout was moved from there, the work this checkout has not committed is %s's own and is kept; "+
			"an assistant stops and tells the person, who carries %s on here with `sdlc release %s --reason \"...\"` "+
			"and then `%s`.\n",
			g.id, quote(g.checkout), on, g.id, g.id, g.id, last)
	}
}

// goneHolders is every story a checkout that no longer exists holds, in the
// order of their ids.
func goneHolders(b *model.Backlog, holders map[string]store.Holder) []goneHolder {
	var out []goneHolder
	for _, id := range slices.Sorted(maps.Keys(holders)) {
		if h := holders[id]; !h.Tied() && !h.Live {
			story, _ := b.Find(id)
			out = append(out, goneHolder{id: id, checkout: h.Checkout,
				waiting: story != nil && story.Status == model.StatusAwaitingHuman})
		}
	}
	return out
}

// heldNote is the part of "nothing to start" that is about stories other
// checkouts hold.
func heldNote(held []string) string {
	if len(held) == 0 {
		return ""
	}
	slices.Sort(held)
	return "; " + strings.Join(held, ", ") + plural(len(held), " is", " are") +
		" held by another checkout of this repository"
}
