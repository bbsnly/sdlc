package model

import "testing"

// US-112.1: a bare start takes only a story marked ready. Backlog.Next serves
// start, status, story list and doctor, so the rule is held here once and the
// commands' own tests hold what each says when it finds nothing.

// AC-1: a todo story is never the pick, whatever its priority number or id. The
// cases vary the numbers and the ids, so that a pick hard-coded to one backlog
// fails the others.
func TestAC1NextNeverPicksATodoStoryAndOrdersTheReadyOnesByPriorityThenID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stories []Story
		want    string
	}{
		{"a ready story beats a todo one with a lower priority number", []Story{
			story("T-1", StatusTodo, new(1)),
			story("R-2", StatusReady, new(5)),
		}, "R-2"},
		{"the same with the ids and numbers changed", []Story{
			story("R-9", StatusReady, new(40)),
			story("A-1", StatusTodo, new(2)),
			story("A-2", StatusTodo, nil),
		}, "R-9"},
		{"priority 0 still wins among ready stories", []Story{
			story("A-1", StatusTodo, new(0)),
			story("R-4", StatusReady, new(3)),
			story("R-7", StatusReady, new(0)),
			story("R-5", StatusReady, nil),
		}, "R-7"},
		{"a todo story ahead of a ready one on id and on priority is passed over", []Story{
			story("A-0", StatusTodo, new(0)),
			story("B-1", StatusReady, new(0)),
		}, "B-1"},
		{"ready ids tie on priority and are read as numbers", []Story{
			story("A-0", StatusTodo, new(2)),
			story("R-10", StatusReady, new(2)),
			story("R-9", StatusReady, new(2)),
		}, "R-9"},
		{"no priority is last among ready stories, and a todo story is not ready", []Story{
			story("T-1", StatusTodo, new(1)),
			story("R-1", StatusReady, nil),
		}, "R-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := (&Backlog{Stories: tc.stories}).Next()
			if !ok {
				t.Fatalf("Next found nothing, want %s", tc.want)
			}
			if got.Story.ID != tc.want || got.Resume {
				t.Errorf("Next = %s (resume %v), want %s", got.Story.ID, got.Resume, tc.want)
			}
		})
	}
}

// AC-1 and AC-2: with only todo stories left there is no pick at all, however
// many there are, so that start has something to refuse and status has nothing
// to name.
func TestAC1NextFindsNothingWhenOnlyTodoStoriesAreLeft(t *testing.T) {
	for _, stories := range [][]Story{
		{story("T-1", StatusTodo, new(1))},
		{story("T-1", StatusTodo, new(1)), story("T-2", StatusTodo, new(0)), story("T-3", StatusTodo, nil)},
		{story("D-1", StatusDone, new(1)), story("B-2", StatusBlocked, new(2)), story("T-3", StatusTodo, new(3), "D-1")},
	} {
		if got, ok := (&Backlog{Stories: stories}).Next(); ok {
			t.Errorf("Next picked %s from a backlog of todo stories and ones that cannot start", got.Story.ID)
		}
	}
}

// AC-1: a ready story waits for its dependencies like any other, and a todo
// story is not a dependency that is done.
func TestAC1AReadyStoryWaitingOnATodoStoryIsNotPickedAndTheTodoStoryIsNotEither(t *testing.T) {
	b := &Backlog{Stories: []Story{
		story("R-2", StatusReady, new(1), "T-1"),
		story("T-1", StatusTodo, new(5)),
	}}
	if got, ok := b.Next(); ok {
		t.Errorf("Next = %s, want nothing: R-2 waits on a story that is not done, and T-1 is not ready", got.Story.ID)
	}

	b.Stories[1].Status = StatusDone
	if got, ok := b.Next(); !ok || got.Story.ID != "R-2" {
		t.Errorf("Next = %v, %v, want R-2 once its dependency is done", got.Story, ok)
	}

	// A ready story that waits does not hand the pick to a todo one beside it.
	c := &Backlog{Stories: []Story{
		story("R-2", StatusReady, new(1), "R-9"),
		story("R-9", StatusBlocked, new(2)),
		story("T-3", StatusTodo, new(3)),
	}}
	if got, ok := c.Next(); ok {
		t.Errorf("Next = %s, want nothing", got.Story.ID)
	}
}

// AC-4 (a guard, which passes before the change): a story under way is resumed
// first, whatever status it had before it started, and a todo story that was
// started by name is resumed ahead of a ready one.
func TestAC4NextResumesAStoryUnderWayAheadOfEveryReadyStory(t *testing.T) {
	for _, status := range []Status{StatusInProgress, StatusAwaitingHuman} {
		b := &Backlog{Stories: []Story{
			story("R-1", StatusReady, new(1)),
			story("T-9", status, new(9)),
		}}
		got, ok := b.Next()
		if !ok || got.Story.ID != "T-9" || !got.Resume {
			t.Errorf("with T-9 %s Next = %+v, %v, want T-9 resumed", status, got, ok)
		}
	}
	onlyTodoAndOneUnderWay := &Backlog{Stories: []Story{
		story("T-1", StatusTodo, new(1)),
		story("T-2", StatusInProgress, new(8)),
	}}
	if got, ok := onlyTodoAndOneUnderWay.Next(); !ok || got.Story.ID != "T-2" || !got.Resume {
		t.Errorf("Next = %+v, %v, want the story under way", got, ok)
	}
}

// AC-1: what another checkout holds is left out before the pick, and a todo
// story is no more picked for it.
func TestAC1WithoutAStoryTheNextPickIsStillAReadyOne(t *testing.T) {
	b := (&Backlog{Stories: []Story{
		story("R-1", StatusReady, new(1)),
		story("T-2", StatusTodo, new(2)),
		story("R-3", StatusReady, new(3)),
	}}).Without("R-1")
	if got, ok := b.Next(); !ok || got.Story.ID != "R-3" {
		t.Errorf("Next = %v, %v, want R-3, not the todo T-2", got.Story, ok)
	}
	if got, ok := b.Without("R-3").Next(); ok {
		t.Errorf("Next = %s, want nothing: only a todo story is left", got.Story.ID)
	}
}
