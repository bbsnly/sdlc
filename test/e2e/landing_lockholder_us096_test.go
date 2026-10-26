// US-096 end to end, with the built binary: what `sdlc status` says while one
// stop lands and another waits for it (AC-5 to AC-7), and a stop killed outright
// while it lands, whose locks one later stop takes and lands the story with
// nothing removed by hand, saying first that the holder has gone and the age
// its lock is taken at (AC-1, AC-2). The AC numbers in this file are US-096's.
//
// A landing is held open inside the project's test command on a named pipe, as
// TestAC13AStopThatWaitsForAnotherLandingSaysWhomItWaitsForAndCanBeCutShort
// holds it, and nothing waits on a clock: a lock is aged by setting its time.
// Helpers carry an lk prefix.

package e2e

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// lkStale is the age the code states a lock left by a gone holder is taken
// at: two minutes since it was last refreshed.
const lkStale = 2 * time.Minute

// lkLocks is the project lock of the main checkout main: the landing lock is
// it plus "-landing", a story's own landing lock plus "-landing-<ID>".
func lkLocks(t *testing.T, main checkout) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(main.dir)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(resolved))
	return filepath.Join(cache, "sdlc", "locks", hex.EncodeToString(sum[:16]))
}

// lkHeldOpen is a project of the stories given, worked through every gate,
// whose test command holds the first stop that runs it on a tree with the
// first story's source in it until the test lets it go.
type lkHeldOpen struct {
	main    checkout
	stories []*ontoStory
	started *os.File
	letGo   func()
}

func lkProject(t *testing.T, ids ...string) *lkHeldOpen {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the landing is held open on a named pipe made with mkfifo, which Windows has not")
	}
	binary := build(t)
	var specs []spec
	for i, id := range ids {
		specs = append(specs, spec{id: id, priority: i + 1})
	}
	main := ontoRepository(t, binary, specs...)
	pipes := t.TempDir()
	started, release, once := filepath.Join(pipes, "started"), filepath.Join(pipes, "release"), filepath.Join(pipes, "once")
	for _, p := range []string{started, release} {
		if out, err := exec.CommandContext(t.Context(), "mkfifo", p).CombinedOutput(); err != nil {
			t.Skipf("no named pipe could be made here: %v\n%s", err, out)
		}
	}
	setConfig(t, main.dir, "commands", "test", "if test -f us001.go && test ! -f '"+once+"'; then : > '"+once+
		"'; echo started > '"+started+"'; head -n 1 '"+release+"' > /dev/null; fi")
	commitEverything(t, main.dir, "a test command that can be held open")
	sessions := []string{sessionFirst, sessionSecond}
	h := &lkHeldOpen{main: main}
	for i, id := range ids {
		h.stories = append(h.stories, ontoBegun(t, binary, main, sessions[i], id))
	}
	for _, s := range h.stories {
		s.worked()
	}
	// Trunk moves after the stories were cut, so that the stop puts the first
	// onto it and runs the commands on what that makes.
	writeFile(t, filepath.Join(main.dir, "CHANGES.md"), "a person's work\n")
	commitPaths(t, main.dir, "a person commits", "CHANGES.md")
	var err error
	if h.started, err = os.OpenFile(started, os.O_RDWR, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.started.Close() })
	releasePipe, err := os.OpenFile(release, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = releasePipe.Close() })
	var letGo sync.Once
	h.letGo = func() { letGo.Do(func() { _, _ = releasePipe.WriteString("go\n") }) }
	t.Cleanup(h.letGo)
	return h
}

// lkFirstInCommands begins the first story's stop and returns it once it is
// held open in the test command, with the landing lock and the story's own.
func (h *lkHeldOpen) lkFirstInCommands(t *testing.T) (*exec.Cmd, <-chan error, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	first := h.stories[0].here().command(t, "", "stop")
	first.Stdout, first.Stderr = &out, &out
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- first.Wait() }()
	t.Cleanup(func() { _ = first.Process.Kill() })
	in := make(chan struct{})
	go func() {
		if _, err := bufio.NewReader(h.started).ReadString('\n'); err == nil {
			close(in)
		}
	}()
	select {
	case <-in:
	case err := <-done:
		t.Fatalf("the first stop ended (%v) before its test command ran:\n%s", err, out.String())
	case <-time.After(ontoPatience):
		t.Fatalf("the first stop never reached its test command:\n%s", out.String())
	}
	return first, done, &out
}

// lkEnded is how a stop begun by lkStopSaying ended, and everything it wrote
// on standard error.
type lkEnded struct {
	err  error
	said string
}

// lkStopSaying begins `sdlc stop` in c and returns it once it has printed a
// line on standard error holding every one of want, with that line; the rest
// of what it prints is read in the background, and arrives with its end.
func lkStopSaying(t *testing.T, c checkout, want ...string) (*exec.Cmd, string, <-chan lkEnded) {
	t.Helper()
	cmd := c.command(t, "", "stop")
	cmd.Stdout = &bytes.Buffer{}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	lines := make(chan string)
	go func() {
		defer close(lines)
		scan := bufio.NewScanner(stderr)
		for scan.Scan() {
			lines <- scan.Text()
		}
	}()
	var all strings.Builder
	deadline := time.After(ontoPatience)
	for {
		select {
		case line, open := <-lines:
			if !open {
				t.Fatalf("the stop in %s ended without saying %q:\n%s", c.dir, want, all.String())
			}
			all.WriteString(line + "\n")
			if lkHolds(line, want...) {
				done := make(chan lkEnded, 1)
				go func() {
					for line := range lines {
						all.WriteString(line + "\n")
					}
					done <- lkEnded{cmd.Wait(), all.String()}
				}()
				return cmd, line, done
			}
		case <-deadline:
			t.Fatalf("the stop in %s had not said %q after %s:\n%s", c.dir, want, ontoPatience, all.String())
		}
	}
}

func lkHolds(line string, want ...string) bool {
	for _, w := range want {
		if !strings.Contains(line, w) {
			return false
		}
	}
	return true
}

// lkStatus is `sdlc status --json` in c as a document, and `sdlc status`.
func lkStatus(t *testing.T, c checkout) (map[string]any, string) {
	t.Helper()
	doc := c.document(t, "status")
	return doc, c.run(t, "status")
}

// lkWaits reports whether status's document says story waits to land for.
func lkWaits(doc map[string]any, story, waitsFor string) bool {
	queue, _ := doc["waiting_to_land"].([]any)
	for _, q := range queue {
		if m, ok := q.(map[string]any); ok && m["story"] == story && m["for"] == waitsFor {
			return true
		}
	}
	return false
}

// lkLineWith is the first line of text holding every one of want.
func lkLineWith(text string, want ...string) (string, bool) {
	for line := range strings.SplitSeq(text, "\n") {
		if lkHolds(line, want...) {
			return line, true
		}
	}
	return "", false
}

// lkNothingLeft fails where anything is left beside the project's lock.
func lkNothingLeft(t *testing.T, locks string) {
	t.Helper()
	left, err := filepath.Glob(locks + "*")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("left beside the project's lock once every stop was over: %q", left)
	}
}

// lkPutBack removes, when the test ends, whatever is left beside the project's
// lock: a test that made a killed stop's locks puts them back, so that what it
// found is its own failure, said by lkNothingLeft, and not every test's.
func lkPutBack(t *testing.T, locks string) {
	t.Helper()
	t.Cleanup(func() {
		left, _ := filepath.Glob(locks + "*")
		for _, path := range left {
			_ = os.RemoveAll(path)
		}
	})
}

// lkAge sets the lock at path to have been last refreshed age ago.
func lkAge(t *testing.T, path string, age time.Duration) {
	t.Helper()
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// AC-5, AC-6, AC-7: while US-001's stop lands, `sdlc status` in the main
// checkout and in US-002's worktree names US-001 landing, since when and its
// process; while US-002's stop waits for it, status names US-002 waiting for
// US-001. A waiting stop killed outright is not shown as waiting. Once both have
// landed, status names no landing and no wait, and nothing is left beside the
// project's lock.
func TestAC5AC6AC7StatusNamesTheStoryLandingAndTheStopThatWaitsForItInEveryCheckout(t *testing.T) {
	h := lkProject(t, "US-001", "US-002")
	locks := lkLocks(t, h.main)
	lkPutBack(t, locks)
	first, firstDone, firstOut := h.lkFirstInCommands(t)
	pid := first.Process.Pid
	second := h.stories[1].here()

	for _, c := range []checkout{h.main, second} {
		doc, text := lkStatus(t, c)
		landing, ok := doc["landing"].(map[string]any)
		if !ok {
			t.Errorf("status --json in %s while US-001 lands has no landing object: %v", c.dir, doc)
			continue
		}
		since, _ := landing["since"].(string)
		if _, err := time.Parse(time.RFC3339, since); err != nil || landing["story"] != "US-001" ||
			landing["command"] != "stop" || landing["pid"] != float64(pid) {
			t.Errorf("status --json in %s reads the landing as %v, want US-001's stop, pid %d, since an RFC 3339 time", c.dir, landing, pid)
		}
		if _, ok := lkLineWith(text, "US-001", "landing", strconv.Itoa(pid)); !ok {
			t.Errorf("status in %s while US-001 lands does not name it and its pid %d:\n%s", c.dir, pid, text)
		}
		if doc["waiting"] != nil {
			t.Errorf("status --json in %s puts the landing in `waiting`, which is a story handed to a person: %v", c.dir, doc["waiting"])
		}
	}

	waiter, _, waiterDone := lkStopSaying(t, second, "stop US-001", "landing")
	for _, c := range []checkout{h.main, second} {
		doc, text := lkStatus(t, c)
		if !lkWaits(doc, "US-002", "US-001") {
			t.Errorf("status --json in %s while US-002's stop waits does not say it waits for US-001: %v", c.dir, doc["waiting_to_land"])
		}
		if line, ok := lkLineWith(text, "US-002", "US-001"); !ok || strings.HasPrefix(line, "  waiting  ") {
			t.Errorf("status in %s does not name US-002 waiting for US-001 on a line of its own:\n%s", c.dir, text)
		}
	}
	if err := waiter.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiterDone:
	case <-time.After(ontoPatience):
		t.Fatal("US-002's stop, killed while it waited, was never reaped")
	}
	if doc, _ := lkStatus(t, h.main); lkWaits(doc, "US-002", "US-001") {
		t.Errorf("status --json names a stop killed outright as still waiting: %v", doc["waiting_to_land"])
	}

	h.letGo()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("US-001's stop failed (%v) once let go:\n%s", err, firstOut.String())
		}
	case <-time.After(ontoPatience):
		t.Fatalf("US-001's stop never finished:\n%s", firstOut.String())
	}
	h.stories[1].run("", "stop")
	doc, _ := lkStatus(t, h.main)
	if doc["landing"] != nil || doc["waiting_to_land"] != nil {
		t.Errorf("status --json after both landed still names %v and %v", doc["landing"], doc["waiting_to_land"])
	}
	for id, status := range statuses(t, h.main.backlog()) {
		if status != "done" {
			t.Errorf("%s is %s after both stops, want done", id, status)
		}
	}
	lkNothingLeft(t, locks)
}

// AC-1, AC-2: US-001's stop is killed outright inside its test command, holding
// the landing lock and its story's own; the project's lock it held before is
// left too, as a stop killed a moment earlier leaves it. `sdlc stop` run again
// says the landing lock's holder is not running and the age the lock is taken
// at, not that it waits up to 8 minutes. Once the locks are that old, the
// story's own a second younger than the landing lock, that one stop lands the
// story, with nothing removed by hand and nothing left behind.
func TestAC1AC2AStopKilledWhileItLandsIsLandedByOneLaterStopThatSaysItsHolderHasGone(t *testing.T) {
	h := lkProject(t, "US-001")
	locks := lkLocks(t, h.main)
	lkPutBack(t, locks)
	first, firstDone, _ := h.lkFirstInCommands(t)
	pid := first.Process.Pid
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	// The test command the killed stop started is still running, in a group
	// of its own, and holds the stop's output open: let it end, so that the
	// stop can be waited for and nothing writes the tree meanwhile.
	h.letGo()
	select {
	case <-firstDone:
	case <-time.After(ontoPatience):
		t.Fatal("the killed stop's test command never ended once let go")
	}

	for _, path := range []string{locks + "-landing", locks + "-landing-US-001"} {
		raw, err := os.ReadFile(filepath.Join(path, "owner.json"))
		var o map[string]any
		if err != nil || json.Unmarshal(raw, &o) != nil || o["pid"] != float64(pid) {
			t.Fatalf("the stop killed while it landed did not leave %s naming pid %d: %v %s", path, pid, err, raw)
		}
	}
	if err := os.MkdirAll(locks, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := `{"pid":` + strconv.Itoa(pid) + `,"at":"` + time.Now().UTC().Format(time.RFC3339) + `","what":"stop","token":"left"}`
	if err := os.WriteFile(filepath.Join(locks, "owner.json"), []byte(owner), 0o644); err != nil {
		t.Fatal(err)
	}
	lkAge(t, locks, lkStale+5*time.Second)

	again, line, done := lkStopSaying(t, h.stories[0].here(), "landing lock")
	for _, want := range []string{"pid " + strconv.Itoa(pid), "not running", "2 minutes"} {
		if !strings.Contains(line, want) {
			t.Errorf("the waiting line does not say %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "up to 8 minutes") {
		t.Errorf("the waiting line says it waits up to 8 minutes for a holder that is gone:\n%s", line)
	}
	lkAge(t, locks+"-landing", lkStale+5*time.Second)
	lkAge(t, locks+"-landing-US-001", lkStale-time.Second)
	select {
	case ended := <-done:
		if ended.err != nil {
			t.Fatalf("the stop run after the killed one failed (%v):\n%s%s", ended.err, again.Stdout, ended.said)
		}
	case <-time.After(ontoPatience):
		t.Fatal("the stop run after the killed one never finished")
	}
	if status := statuses(t, h.main.backlog())["US-001"]; status != "done" {
		t.Errorf("US-001 is %s after one stop, want done", status)
	}
	if !ontoHolds(t, h.main, h.stories[0].source) {
		t.Errorf("trunk does not hold %s after the stop", h.stories[0].source)
	}
	lkNothingLeft(t, locks)
}
