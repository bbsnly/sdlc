package cli

// US-129 rows found by the verifier (VERIFICATION.md, F-1 and F-6), run in
// this process as cmd/sdlc runs a command.
//
// AC-2: a gate record cut off just after its events array opens. The base
// read it with json.Unmarshal, and trace and doctor said SDLC-E0005 with its
// fix; the record decoder's size scan indexed past the white space and every
// command that read the record crashed instead.
//
// AC-4: what a command read before it took the loop's lock, under an earlier
// take of it or under none, is read afresh once it holds it. The lock is what
// makes another process's write and this one's a sequence; a read kept from
// before the take is that other process's write lost. The kept read is checked
// against the file's identity, size and modification time, so the other
// process here writes in place, the same number of bytes, and puts the time
// back, as an editor that keeps the file's times does: nothing the stat sees
// changes, and only dropping what was kept when the lock is taken reads the
// new bytes.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// us129OpenEvents is a record that ends where its events array opens.
const us129OpenEvents = `{"story":"US-001","events":[ ` + "\n"

// us129Survive is run with a panic turned into a failed result, so that one
// crash fails its row and not the package's run.
func us129Survive(t *testing.T, args ...string) (r result) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("sdlc %s crashed: %v", strings.Join(args, " "), p)
			r = result{code: -1, stderr: fmt.Sprint(p)}
		}
	}()
	return run(t, args...)
}

func TestAC2TraceAndDoctorOnARecordCutOffAfterItsEventsOpenSayTheRecordIsNotValidJSON(t *testing.T) {
	t.Parallel()
	for _, body := range []string{us129OpenEvents, "{\"story\":\"US-001\",\"events\":[\r\n", "{\"events\":[\t"} {
		t.Run(fmt.Sprintf("AC-2 %q", body), func(t *testing.T) {
			t.Parallel()
			root := startedAt(t, model.GateImplementation)
			writeFile(t, root, ".sdlc/stories/US-001/gate-record.json", body)

			r := us129Survive(t, "trace", "US-001")
			if r.code != 1 || !strings.Contains(r.stderr, `the record for "US-001" is not valid JSON`) ||
				!strings.Contains(r.stderr, "SDLC-E0005") {
				t.Errorf("AC-2 sdlc trace US-001 exited %d:\n%s%s\nwant 1, saying the record is not valid JSON with SDLC-E0005, as at the base",
					r.code, r.stdout, r.stderr)
			}

			r = us129Survive(t, "doctor")
			var line string
			for l := range strings.Lines(r.stdout) {
				if strings.Contains(l, "loop state") {
					line = l
				}
			}
			if r.code != 1 || !strings.Contains(line, "problem") ||
				!strings.Contains(line, `the record for "US-001" is not valid JSON (SDLC-E0005)`) {
				t.Errorf("AC-2 sdlc doctor exited %d, its loop state line %q:\n%s%s\nwant 1, naming the record as not valid JSON with SDLC-E0005, as at the base",
					r.code, line, r.stdout, r.stderr)
			}
		})
	}
}

// us129WriterClock is the lock clock of a command that takes the loop's lock:
// as each take begins, another process writes, so that whatever the command
// read before a take, under an earlier one or under none, was read before
// that write. The lock is free, so take reads the clock once a take and never
// sleeps; Now does not move.
type us129WriterClock struct {
	mu    sync.Mutex
	takes int
	write func(take int)
}

func (c *us129WriterClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.takes++
	c.write(c.takes)
	return time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
}

func (*us129WriterClock) Sleep(time.Duration) {}

// us129InPlace rewrites path with replace applied, in place and to the same
// length, and puts its modification time back.
func us129InPlace(t *testing.T, path, old, replacement string) {
	t.Helper()
	if len(old) != len(replacement) {
		t.Fatalf("%q and %q are not the same length", old, replacement)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(old)) {
		t.Fatalf("%s holds no %q", path, old)
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(bytes.Replace(raw, []byte(old), []byte(replacement), 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		t.Fatalf("the rewrite of %s changed what a stat sees: %v %d %v, then %v %d %v",
			path, info.ModTime(), info.Size(), info.Name(), after.ModTime(), after.Size(), after.Name())
	}
}

// start with new work takes the lock, reads the backlog and decides, lets the
// lock go to check trunk, and takes it again to decide once more and write the
// story's status (the base decided outside the lock and took it once).
// Another process edits US-001's title as each take begins, and the title
// start leaves is the last edit: a read kept from before a take writes an
// earlier one back.
func TestAC4AStartReadsTheBacklogAnotherProcessWroteBeforeEachTakeOfTheLock(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	backlog := filepath.Join(root, "user_stories.json")
	title := "Example: reject invoices"
	clock := &us129WriterClock{write: func(take int) {
		edited := fmt.Sprintf("Edit%03d: reject invoices", take)
		us129InPlace(t, backlog, title, edited)
		title = edited
	}}

	var out, errb bytes.Buffer
	inv := invocationOf(t)
	code := executeAt(commandEnv{dir: root, getenv: inv.getenv, lockClock: clock}, []string{"start"},
		strings.NewReader(""), &out, &errb)
	if clock.takes == 0 {
		t.Fatalf("start never took the lock (exit %d):\n%s%s", code, out.String(), errb.String())
	}
	if code != 0 {
		t.Fatalf("start exited %d:\n%s%s", code, out.String(), errb.String())
	}
	raw, err := os.ReadFile(backlog)
	if err != nil {
		t.Fatal(err)
	}
	var b model.Backlog
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	story, ok := b.Find("US-001")
	switch {
	case !ok:
		t.Fatalf("the backlog lost US-001:\n%s", raw)
	case story.Title != title+" with a non-positive total":
		t.Errorf("AC-4 after %d takes of the lock, the backlog start wrote has US-001's title %q: the edit another process made "+
			"before the last take is lost, written over from what start read before it; want %q",
			clock.takes, story.Title, title+" with a non-positive total")
	}
	if story.Status != model.StatusInProgress {
		t.Errorf("AC-4 US-001 is %q after start, want %q", story.Status, model.StatusInProgress)
	}
}
