package parallel

// US-058 AC-2, AC-6 and AC-8 at the level of the scheduler and of what a story
// reads as when its session ends. The scheduler is given the function that
// runs one session, so these need no process and no clock: a session that has
// not ended is a channel nobody has closed. The package has to give them:
//
//	type Result struct { Story string; Ran bool; Exit int; Err error }
//	type Progress struct { Running, Waiting int }
//	func Schedule(ctx context.Context, ids []string, max int,
//		run func(ctx context.Context, id string) (exit int, err error),
//		progress func(Progress)) []Result
//
//	type State string // StateWaiting "waiting", StateStopped "stopped", StateFailed "failed"
//	type Row struct { Story, Gate string; State State; Command string; Exit int }
//	func Classify(story string, rec *model.Record, exit int) Row

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// sessions is a run whose sessions end when the test says so.
type sessions struct {
	release map[string]chan struct{}
	started chan string
	running atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
}

func newSessions(ids ...string) *sessions {
	s := &sessions{release: map[string]chan struct{}{}, started: make(chan string)}
	for _, id := range ids {
		s.release[id] = make(chan struct{})
	}
	return s
}

func (s *sessions) run(ctx context.Context, id string) (int, error) {
	s.calls.Add(1)
	n := s.running.Add(1)
	for {
		p := s.peak.Load()
		if n <= p || s.peak.CompareAndSwap(p, n) {
			break
		}
	}
	defer s.running.Add(-1)
	select {
	case s.started <- id:
	case <-ctx.Done():
		return -1, ctx.Err()
	}
	select {
	case <-s.release[id]:
		return 0, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// progressLog collects what the scheduler says, from whichever goroutine says it.
type progressLog struct {
	mu  sync.Mutex
	got []Progress
}

func (l *progressLog) add(p Progress) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, p)
}

func (l *progressLog) all() []Progress {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.got)
}

func start(ctx context.Context, t *testing.T, ids []string, limit int, s *sessions, l *progressLog) <-chan []Result {
	t.Helper()
	done := make(chan []Result, 1)
	go func() { done <- Schedule(ctx, ids, limit, s.run, l.add) }()
	return done
}

func TestAC2NoMoreThanMaxRunAtOnceAndTheRestStartAsASlotFrees(t *testing.T) {
	ids := []string{"US-1", "US-2", "US-3", "US-4", "US-5"}
	s, l := newSessions(ids...), &progressLog{}
	done := start(t.Context(), t, ids, 2, s, l)

	first := []string{<-s.started, <-s.started}
	slices.Sort(first)
	if !slices.Equal(first, []string{"US-1", "US-2"}) {
		t.Fatalf("the first two sessions are %q, want the first two named", first)
	}
	// Each release frees one slot, and the next in the order given takes it.
	for i, next := range []string{"US-3", "US-4", "US-5"} {
		close(s.release[ids[i]])
		if got := <-s.started; got != next {
			t.Fatalf("after %s ended the next session was %s, want %s", ids[i], got, next)
		}
	}
	for _, id := range ids[3:] {
		close(s.release[id])
	}

	results := <-done
	if len(results) != len(ids) {
		t.Fatalf("%d results for %d stories", len(results), len(ids))
	}
	for i, r := range results {
		if r.Story != ids[i] || !r.Ran || r.Exit != 0 || r.Err != nil {
			t.Errorf("result %d = %+v, want %s run and ended cleanly, in the order named", i, r, ids[i])
		}
	}
	if peak := s.peak.Load(); peak > 2 {
		t.Errorf("%d sessions ran at once with --max 2", peak)
	}
	got := l.all()
	if len(got) == 0 || got[0] != (Progress{Running: 2, Waiting: 3}) {
		t.Fatalf("the first count is %+v, want running 2, waiting 3", got)
	}
	for _, want := range []Progress{{2, 2}, {2, 1}, {2, 0}} {
		if !slices.Contains(got, want) {
			t.Errorf("the count never said running %d, waiting %d: %+v", want.Running, want.Waiting, got)
		}
	}
	for _, p := range got {
		if p.Running > 2 || p.Running < 0 || p.Waiting < 0 {
			t.Errorf("the count says running %d, waiting %d", p.Running, p.Waiting)
		}
	}
}

func TestAC2AMaxAboveTheNumberOfStoriesRunsThemAllAndNoneWaits(t *testing.T) {
	ids := []string{"US-1", "US-2"}
	s, l := newSessions(ids...), &progressLog{}
	done := start(t.Context(), t, ids, 9, s, l)
	<-s.started
	<-s.started
	for _, id := range ids {
		close(s.release[id])
	}
	<-done
	if got := l.all(); len(got) == 0 || got[0] != (Progress{Running: 2, Waiting: 0}) {
		t.Errorf("counts = %+v, want running 2, waiting 0 first", got)
	}
}

func TestAC2AMaxOfOneRunsThemOneAfterAnotherInTheOrderNamed(t *testing.T) {
	ids := []string{"US-1", "US-2", "US-3"}
	s, l := newSessions(ids...), &progressLog{}
	done := start(t.Context(), t, ids, 1, s, l)
	for _, id := range ids {
		if got := <-s.started; got != id {
			t.Fatalf("session %s started where %s was next", got, id)
		}
		close(s.release[id])
	}
	<-done
	if s.peak.Load() != 1 {
		t.Errorf("%d sessions ran at once with --max 1", s.peak.Load())
	}
	if got := l.all(); len(got) == 0 || got[0] != (Progress{Running: 1, Waiting: 2}) {
		t.Errorf("counts = %+v, want running 1, waiting 2 first", got)
	}
}

// One session ending badly is the person's to hear about at the end, and is
// not a reason to leave the rest unworked.
func TestAC6ASessionThatFailsOrCannotStartDoesNotStopTheOthers(t *testing.T) {
	broken := errors.New("claude: not found")
	run := func(_ context.Context, id string) (int, error) {
		switch id {
		case "US-2":
			return 7, nil
		case "US-3":
			return -1, broken
		}
		return 0, nil
	}
	results := Schedule(t.Context(), []string{"US-1", "US-2", "US-3", "US-4"}, 2, run, func(Progress) {})
	want := []Result{
		{Story: "US-1", Ran: true},
		{Story: "US-2", Ran: true, Exit: 7},
		{Story: "US-3", Ran: true, Exit: -1, Err: broken},
		{Story: "US-4", Ran: true},
	}
	if len(results) != len(want) {
		t.Fatalf("results = %+v", results)
	}
	for i := range want {
		if results[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, results[i], want[i])
		}
	}
}

// AC-8: an interrupt reaches the scheduler as the end of its context. What is
// running is told to stop, and what waits is never started.
func TestAC8AnInterruptEndsWhatRunsAndStartsNothingThatWaited(t *testing.T) {
	ids := []string{"US-1", "US-2", "US-3", "US-4"}
	s, l := newSessions(ids...), &progressLog{}
	ctx, cancel := context.WithCancel(t.Context())
	done := start(ctx, t, ids, 1, s, l)
	if got := <-s.started; got != "US-1" {
		t.Fatalf("first session = %s", got)
	}
	cancel()
	var results []Result
	select {
	case results = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Schedule did not return after its context ended")
	}
	if n := s.calls.Load(); n != 1 {
		t.Errorf("%d sessions were started, want only the one running when the context ended", n)
	}
	if len(results) != len(ids) {
		t.Fatalf("%d results for %d stories", len(results), len(ids))
	}
	if !results[0].Ran {
		t.Error("the session that was running is not reported as run")
	}
	for _, r := range results[1:] {
		if r.Ran {
			t.Errorf("%s is reported as run: it waited when the run was interrupted", r.Story)
		}
	}
}

// ------------------------------------------------------------ what a story reads as

func recordAt(t *testing.T, id string, passed ...model.Gate) *model.Record {
	t.Helper()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	r := model.NewRecord(id, at)
	for _, g := range passed {
		r.SetGate(g, model.GatePass, "", at)
	}
	return r
}

func passedAll() []model.Gate { return model.Gates }

func TestAC6ASessionThatEndedWithTheStoryUnfinishedAndNothingPendingIsStoppedNotFailed(t *testing.T) {
	row := Classify("US-1", recordAt(t, "US-1", model.GateDoR), 0)
	if row.Story != "US-1" || row.State != StateStopped || row.Gate != string(model.GateAnalysis) || row.Command != "" {
		t.Errorf("row = %+v, want US-1 stopped at the analysis gate with nothing for a person to run", row)
	}
	// The headless denied-tool case: the story is where it was, and nobody was told.
	if row.State == StateWaiting || row.State == StateFailed {
		t.Errorf("a quiet end read as %s", row.State)
	}
}

func TestAC6AStoryNobodyHasStartedReadsFromTheFirstGate(t *testing.T) {
	for name, rec := range map[string]*model.Record{"no record": nil, "an empty one": recordAt(t, "US-1")} {
		row := Classify("US-1", rec, 3)
		if row.State != StateFailed || row.Exit != 3 || row.Gate != string(model.GateDoR) {
			t.Errorf("%s: row = %+v, want failed with exit 3 at the first gate", name, row)
		}
	}
}

func TestAC6ANonZeroExitWithNothingPendingIsFailedAndCarriesTheStatus(t *testing.T) {
	row := Classify("US-1", recordAt(t, "US-1", model.GateDoR, model.GateAnalysis), 137)
	if row.State != StateFailed || row.Exit != 137 || row.Gate != string(model.GateTestsFrozen) || row.Command != "" {
		t.Errorf("row = %+v", row)
	}
}

// A session that escalates ends its turn normally, and one that is stopped
// after it did is no less waiting: the record says a person is wanted, and the
// exit status says nothing about that.
func TestAC6AStoryWaitingOnAPersonIsWaitingWhateverTheExitStatusWas(t *testing.T) {
	for _, exit := range []int{0, 1, 137} {
		rec := recordAt(t, "US-1", model.GateDoR)
		rec.Escalate("pre_commit_approval", "the migration has no down step", "", time.Now())
		row := Classify("US-1", rec, exit)
		if row.State != StateWaiting {
			t.Errorf("exit %d: state = %s, want waiting: a session ended by a person's decision is not a failure", exit, row.State)
		}
		if !strings.Contains(row.Command, "sdlc approve US-1") {
			t.Errorf("exit %d: command = %q, want the approval a person runs", exit, row.Command)
		}
	}
}

func TestAC6AnEscalationAPersonHasAnsweredIsNotWaiting(t *testing.T) {
	rec := recordAt(t, "US-1", model.GateDoR)
	rec.Escalate("loop_stalled", "stuck", "", time.Now())
	rec.Decide(true, "carry on", "", time.Now())
	if row := Classify("US-1", rec, 0); row.State != StateStopped || row.Command != "" {
		t.Errorf("row = %+v, want stopped: nothing is pending any more", row)
	}
}

func TestAC6ABrokenFreezeNamesTheUnfreezeAsWellAsTheApproval(t *testing.T) {
	rec := recordAt(t, "US-1", model.GateDoR, model.GateAnalysis)
	rec.Escalate("freeze_broken", "TestAdd asserts 0; AC-1 says 3", "", time.Now())
	row := Classify("US-1", rec, 0)
	if row.State != StateWaiting || !strings.Contains(row.Command, "sdlc approve US-1") || !strings.Contains(row.Command, "sdlc unfreeze --reason") {
		t.Errorf("row = %+v, want waiting with the approval and the unfreeze a person runs", row)
	}
}

func TestAC6AStoryWithEveryGatePassedWaitsToBeLandedByAPerson(t *testing.T) {
	for _, exit := range []int{0, 2} {
		row := Classify("US-1", recordAt(t, "US-1", passedAll()...), exit)
		if row.State != StateWaiting || !strings.Contains(row.Command, "sdlc stop") || row.Gate != "" {
			t.Errorf("exit %d: row = %+v, want waiting to land with sdlc stop and no gate left", exit, row)
		}
	}
}
