package model

import (
	"slices"
	"testing"
)

// TodoToStart names the todo stories that only their status keeps from being picked,
// every dependency done, in the order Next would offer them: by priority, then by id
// read as a person reads it.
func TestTodoToStartNamesTheTodoStoriesWhoseDependenciesAreDoneInTheOrderNextOffersThem(t *testing.T) {
	one, two := 1, 2
	b := &Backlog{Stories: []Story{
		{ID: "A-10", Status: StatusTodo, Priority: &one},
		{ID: "A-2", Status: StatusTodo, Priority: &one},
		{ID: "A-1", Status: StatusTodo, Priority: &two},
		{ID: "A-3", Status: StatusTodo, DependsOn: []string{"A-4"}},
		{ID: "A-4", Status: StatusInProgress},
		{ID: "A-5", Status: StatusTodo, DependsOn: []string{"A-6"}, Priority: &two},
		{ID: "A-6", Status: StatusDone},
		{ID: "A-7", Status: StatusReady},
	}}
	if got, want := b.TodoToStart(), []string{"A-2", "A-10", "A-1", "A-5"}; !slices.Equal(got, want) {
		t.Errorf("TodoToStart = %q, want %q", got, want)
	}
	if got := (&Backlog{}).TodoToStart(); got == nil || len(got) != 0 {
		t.Errorf("TodoToStart of an empty backlog = %#v, want an empty list", got)
	}
}

// StateFiles is every file the store keeps in the state directory, and a caller that
// changes its copy changes no other caller's.
func TestStateFilesListsEveryStateFileInACopyOfItsOwn(t *testing.T) {
	want := []string{ActiveFile, SessionFile, FreezeFile, StopBlocksFile, AcknowledgedFile, TraceMarkFile}
	got := StateFiles()
	if !slices.Equal(got, want) {
		t.Fatalf("StateFiles = %q, want %q", got, want)
	}
	got[0] = "changed"
	if StateFiles()[0] != ActiveFile {
		t.Error("a caller's change to StateFiles reached the next caller")
	}
}
