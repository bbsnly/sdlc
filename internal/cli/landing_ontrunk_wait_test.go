package cli

// US-088, the turns stories take to land, and a replay that does not come out
// as the reviewed change. A stop that finds another landing in front of it
// says whom it waits for, lands once that one is done, and finds out on the
// way whether its own story landed or changed meanwhile (AC-13). A replay
// whose result is not, line for line, the change the reviewers saw is refused
// and leaves no mark of itself (AC-10).
//
// The landing in front is the test's own: it holds the lock every landing
// holds, through the store, and lets go once the stop has said it waits.
// Nothing waits on a clock. Helpers carry the ot prefix.

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// otPatience bounds a wait for something that has to happen; a deadline, not
// a pause.
const otPatience = 2 * time.Minute

// otLandingInFront is another story's landing, US-002's, holding the lock
// every landing of the project holds until the test lets it go.
func otLandingInFront(t *testing.T, main string) *store.Guard {
	t.Helper()
	cfg, err := config.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	held, err := store.New(&config.Project{Root: main, Config: cfg}).LockLandings("US-002", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(held.Release)
	return held
}

// otHeard is a stop's standard error, and a signal once it has said it waits
// for the landing in front.
type otHeard struct {
	mu      sync.Mutex
	text    bytes.Buffer
	once    sync.Once
	waiting chan struct{}
}

func (h *otHeard) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.text.Write(p)
	if strings.Contains(h.text.String(), "stop US-002") {
		h.once.Do(func() { close(h.waiting) })
	}
	return len(p), nil
}

func (h *otHeard) said() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.text.String()
}

// otWaitingStop is `sdlc stop` begun in dir beside the test, with what it
// says on standard error heard as it says it.
type otWaitingStop struct {
	heard *otHeard
	done  chan result
}

func otStopBeside(dir string, args ...string) *otWaitingStop {
	w := &otWaitingStop{heard: &otHeard{waiting: make(chan struct{})}, done: make(chan result, 1)}
	go func() {
		var out bytes.Buffer
		getenv := func(name string) string {
			if name == store.SessionEnv {
				return ldSession
			}
			return os.Getenv(name)
		}
		code := ExecuteIn(dir, getenv, append([]string{"stop"}, args...), strings.NewReader(""), &out, io.Writer(w.heard))
		w.done <- result{stdout: out.String(), stderr: w.heard.said(), code: code}
	}()
	return w
}

// waits holds the stop to saying it waits for US-002's landing, and to not
// having ended instead.
func (w *otWaitingStop) waits(t *testing.T) {
	t.Helper()
	select {
	case <-w.heard.waiting:
	case r := <-w.done:
		w.done <- r
		t.Fatalf("the stop ended, exiting %d, while another landing held the lock, and did not say it waits for it:\n%s%s", r.code, r.stdout, r.stderr)
	case <-time.After(otPatience):
		t.Fatalf("the stop said nothing of the landing in front of it:\n%s", w.heard.said())
	}
}

func (w *otWaitingStop) ended(t *testing.T) result {
	t.Helper()
	select {
	case r := <-w.done:
		return r
	case <-time.After(otPatience):
		t.Fatalf("the stop never ended once the landing in front of it had:\n%s", w.heard.said())
	}
	return result{}
}

// AC-13: a stop that finds another story landing says which stop it waits for
// and that it can be run again, moves nothing while it waits, and lands its
// story on the trunk that moved once the one in front lets go.
func TestAC13AStopWaitsForTheLandingInFrontOfItAndThenLands(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
	tip := swRev(t, wt, "HEAD")
	moved := ldMoveTrunk(t, main)
	inFront := otLandingInFront(t, main)

	stop := otStopBeside(wt, "--json")
	stop.waits(t)
	said := stop.heard.said()
	for _, want := range []string{"stop US-002", "landing", "run again"} {
		if !strings.Contains(said, want) {
			t.Errorf("the waiting stop does not say %q:\n%s", want, said)
		}
	}
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("trunk moved to %s while the stop was only waiting", got)
	}
	if got := swRev(t, main, "refs/heads/story/US-001"); got != tip {
		t.Errorf("story/US-001 moved to %s while the stop was only waiting", got)
	}

	inFront.Release()
	r := stop.ended(t)
	if r.code != 0 {
		t.Fatalf("the stop exited %d once the landing in front had let go:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := decode[stopPayload](t, r); !got.OK || got.Story != "US-001" || !got.Done {
		t.Errorf("stop --json = %+v, want US-001 done", got)
	}
	landed := ldTrunk(t, main)
	if _, ok := swGitTry(t, main, "merge-base", "--is-ancestor", moved, landed); !ok || landed == moved {
		t.Errorf("trunk is at %s after the stop, want the story landed on top of %s", landed, moved)
	}
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("US-001 is %s after it landed, want done", got)
	}
}

// AC-13: two stops of one story, both waiting for the landing in front. One
// lands the story; the other, when its turn comes, finds it landed and says
// so, exits 0, and lands nothing twice.
func TestAC13AStopWhoseStoryLandedWhileItWaitedSaysSoAndLandsNothingTwice(t *testing.T) {
	t.Parallel()
	for _, json := range []bool{false, true} {
		name := map[bool]string{false: "AC-13 as a person reads it", true: "AC-13 with --json"}[json]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
			moved := ldMoveTrunk(t, main)
			inFront := otLandingInFront(t, main)

			var args []string
			if json {
				args = []string{"--json"}
			}
			stops := []*otWaitingStop{otStopBeside(wt, args...), otStopBeside(wt, args...)}
			for _, s := range stops {
				s.waits(t)
			}
			inFront.Release()
			meanwhile := 0
			for _, s := range stops {
				r := s.ended(t)
				if r.code != 0 {
					t.Errorf("a stop exited %d, want the story landed by one and found landed by the other:\n%s%s", r.code, r.stdout, r.stderr)
					continue
				}
				if json {
					if got := decode[stopPayload](t, r); !got.OK || got.Story != "US-001" || !got.Done {
						t.Errorf("stop --json = %+v, want US-001 done from both stops", got)
					}
				}
				if strings.Contains(r.stdout, "landed while this stop waited") {
					meanwhile++
				}
			}
			if !json && meanwhile != 1 {
				t.Errorf("%d of the two stops said the story landed while it waited, want one", meanwhile)
			}
			landed := ldTrunk(t, main)
			if got := swGit(t, main, "rev-list", "--count", moved+".."+landed); got != "2" {
				t.Errorf("trunk gained %s commits, want the story's two, once", got)
			}
			if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
				t.Errorf("US-001 is %s after both stops, want done", got)
			}
		})
	}
}

// AC-13 and AC-11: the landing asks again, once its turn has come, whether the
// story is still finished. A story sent back to its implementation while its
// stop waited is not landed: the stop is refused with SDLC-E0055, says the
// story changed while it waited, and trunk is where it was.
func TestAC13AStorySentBackWhileItsStopWaitedIsNotLanded(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
	moved := ldMoveTrunk(t, main)
	inFront := otLandingInFront(t, main)

	stop := otStopBeside(wt)
	stop.waits(t)
	wtMust(t, wt, ldSession, "", "gate", "implementation", "fail", "--story", "US-001", "--note", "found wanting while the stop waited")
	inFront.Release()

	r := stop.ended(t)
	out := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(out, ldCheckFailed) || !strings.Contains(out, "while this stop waited") {
		t.Fatalf("the stop exited %d, want SDLC-E0055 saying the story changed while it waited:\n%s", r.code, out)
	}
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("trunk moved to %s with a story that was sent back", got)
	}
	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusDone) {
		t.Error("a story that was sent back is done")
	}
	if !swListed(t, main, wt) {
		t.Error("the refused landing took the story's worktree away")
	}
}

// AC-9, where a project leaves a reviewer out until a path of theirs changes:
// what trunk gained under a story that was rebased onto it is trunk's change
// and not the story's. A file another story landed under a path that brings
// the security reviewer back does not bring them back to this story's code
// review, and the story lands without their review; a file the story itself
// commits there afterwards does bring them back.
func TestAC9WhatTrunkGainedUnderARebasedStoryBringsNoReviewerBack(t *testing.T) {
	t.Parallel()
	main := ldRepository(t, func(root string) { setConfigOnDisk(t, root, "staffing", stfSection()) })
	otIdentity(t, main)
	wt := swCheckStarted(t, main, "US-001", ldSession, swStarted(t, main, ldSession))
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	stfSet(t, stfFull())
	stfAnalysed(t, false)
	mustRun(t, "staffing", "amend", "security", "--left-out", "--reason", "nothing here crosses a trust boundary")
	stfThrough(t, wt, model.GateImplementation, "security")
	ldWork(t, wt)
	stfThrough(t, wt, "", "security")
	if by := stfShow(t, "--gate", "code_review").agent(t, "security").BroughtBackBy; by != "" {
		t.Fatalf("precondition: the security reviewer is brought back by %q before trunk moved", by)
	}

	moved := swCommitFile(t, main, "internal/hook/hook.go", "package hook\n", "another story's hook")
	ldRebase(t, wt)
	if by := stfShow(t, "--gate", "code_review").agent(t, "security").BroughtBackBy; by != "" {
		t.Errorf("the security reviewer is brought back by %q, which trunk gained and the story did not change", by)
	}

	// The other side: a commit of the story's own there is asked about, and
	// then put aside again, so that what lands below is the story as reviewed.
	tip := swRev(t, wt, "HEAD")
	swCommitFile(t, wt, "internal/hook/own.go", "package hook\n", "the story's own hook")
	by := stfShow(t, "--gate", "code_review").agent(t, "security").BroughtBackBy
	swGit(t, wt, "reset", "--quiet", "--hard", tip)
	if by != "internal/hook/own.go" {
		t.Errorf("the security reviewer is brought back by %q, want the story's own internal/hook/own.go", by)
	}

	landed := otLands(t, main, wt, moved)
	if _, ok := otAt(t, main, landed.tip, "internal/hook/hook.go"); !ok {
		t.Error("trunk lost the file another story landed")
	}
	for _, r := range swRecord(t, main, "US-001").Reviews {
		if r.Role == "security" {
			t.Errorf("the record has a security review, and nothing brought that reviewer back: %+v", r)
		}
	}
}

// AC-10: trunk renamed a file the story edits. Git puts the story's commits
// onto that trunk without a conflict, with the story's lines in the file under
// its new name: not, line for line, the change the reviewers saw. The stop
// refuses with SDLC-E0029 and lands nothing; the record carries no mark of a
// replay and no event of one, so nothing later takes that tip for the reviewed
// change; and a second stop is refused the same way. Reviewed again as it now
// is, the story lands.
func TestAC10AReplayThatIsNotLineForLineTheReviewedChangeIsRefusedAndLeavesNoMark(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
	tip := swRev(t, wt, "HEAD")
	swGit(t, main, "mv", "billing.go", "accounts.go")
	swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "another story renames billing.go")
	moved := ldTrunk(t, main)

	for _, stop := range []string{"the stop", "a second stop"} {
		out := swRefused(t, ldStale, wt, ldSession, "stop")
		said, fix := ldRefusal(t, out, ldStale)
		suSays(t, said+fix, "story/US-001", "review")
		if got := ldTrunk(t, main); got != moved {
			t.Fatalf("%s moved trunk to %s with a change that is not the reviewed one", stop, got)
		}
		record := lxRecord(t, main)
		if mark, ok := record["replay"]; ok {
			t.Errorf("after %s the record carries a replay mark, %v, for a result that was refused", stop, mark)
		}
		for _, e := range swRecord(t, main, "US-001").Events {
			if e.Type == "replayed" {
				t.Errorf("after %s the record has a replayed event for a result that was refused: %s", stop, e.Message)
			}
		}
	}
	if got := swRev(t, wt, "HEAD"); got == tip {
		t.Errorf("the story's tip is still %s, and the refusal is for what the replay made", tip)
	}
	if got, _ := otAt(t, wt, "HEAD", "accounts.go"); !strings.Contains(got, "type Invoice") {
		t.Errorf("accounts.go in the worktree is %q, want the story's lines under the name trunk gave the file", got)
	}
	ldStillHeld(t, main, wt)

	ldReviewAgain(t, wt)
	landed := otLands(t, main, wt, moved)
	if got, _ := otAt(t, main, landed.tip, "accounts.go"); !strings.Contains(got, "type Invoice") {
		t.Errorf("trunk's accounts.go is %q after the landing, want the story's lines", got)
	}
}
