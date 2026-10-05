package parallel

import "context"

// Result is how one story's session ended. Ran is false for a story that was
// never started, because the run was interrupted while it waited.
type Result struct {
	Story string
	Ran   bool
	Exit  int
	Err   error
}

// Progress is how many sessions are running and how many are still to start.
type Progress struct{ Running, Waiting int }

// Schedule runs one session per story, at most limit at once, starting them in
// the order given and starting the next as each ends. It is one goroutine's
// loop and the only caller of progress, so a caller that prints needs no lock.
//
// A session that ends badly is recorded and does not stop the others. When ctx
// ends nothing further is started and what is running is waited for: ctx is
// what stops it. Results are in the order the stories were given.
func Schedule(ctx context.Context, ids []string, limit int,
	run func(ctx context.Context, id string) (exit int, err error),
	progress func(Progress)) []Result {

	results := make([]Result, len(ids))
	for i, id := range ids {
		results[i].Story = id
	}
	type ended struct {
		i    int
		exit int
		err  error
	}
	endings := make(chan ended)
	next, running := 0, 0
	launch := func() {
		i := next
		next++
		running++
		go func() {
			exit, err := run(ctx, ids[i])
			endings <- ended{i, exit, err}
		}()
	}
	for next < len(ids) && running < limit {
		launch()
	}
	progress(Progress{Running: running, Waiting: len(ids) - next})
	for running > 0 {
		e := <-endings
		running--
		results[e.i] = Result{Story: ids[e.i], Ran: true, Exit: e.exit, Err: e.err}
		if next < len(ids) && ctx.Err() == nil {
			launch()
			progress(Progress{Running: running, Waiting: len(ids) - next})
		}
	}
	return results
}
