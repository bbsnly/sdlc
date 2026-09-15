package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// acknowledgedIn is what .sdlc/state/acknowledged holds, and whether it is there.
func acknowledgedIn(t *testing.T, root string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".sdlc", "state", "acknowledged"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}

func TestAcknowledgingWritesTheCommitInFull(t *testing.T) {
	root := finishedStory(t)
	mustRun(t, "stop")
	head := headOf(t, root)

	got := decode[ackPayload](t, mustRun(t, "ack", "--through", head[:12], "--json"))
	if got.Through != head || got.Previous != "" || got.Back {
		t.Errorf("ack --through %s = %+v, want through %s and nothing before it", head[:12], got, head)
	}
	if body, _ := acknowledgedIn(t, root); body != head+"\n" {
		t.Errorf(".sdlc/state/acknowledged holds %q, want the full commit %s", body, head)
	}
	payload := decode[logPayload](t, mustRun(t, "log", "--json"))
	if len(payload.Entries) != 0 || payload.Since != head || payload.SinceSource != sinceAcknowledged {
		t.Errorf("after acknowledging HEAD the log lists %d stories after %q (%s), want none",
			len(payload.Entries), payload.Since, payload.SinceSource)
	}

	out := mustRun(t, "ack", "--through", "HEAD").stdout
	if !strings.Contains(out, "Acknowledged through "+head[:12]) || !strings.Contains(out, "It already was.") {
		t.Errorf("acknowledging the same commit again does not say so:\n%s", out)
	}
}

func TestAcknowledgingAnEarlierCommitListsTheStoriesAfterItAgain(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	addStory(t, root, "US-002")
	for range 2 {
		mustRun(t, "start")
		reach(t, root, "")
		mustRun(t, "stop")
	}
	first, second := readRecord(t, root, "US-001").Commit, readRecord(t, root, "US-002").Commit

	mustRun(t, "ack", "--through", second)
	got := decode[ackPayload](t, mustRun(t, "ack", "--through", first, "--json"))
	if got.Through != first || got.Previous != second || !got.Back {
		t.Errorf("moving back from %s to %s = %+v, want it said to be back", second, first, got)
	}
	if entry := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json"))); entry.Story != "US-002" {
		t.Errorf("after moving back the log lists %s, want US-002 again", entry.Story)
	}

	if out := mustRun(t, "ack", "--through", second).stdout; !strings.Contains(out, "It was "+first[:12]+" until now.") {
		t.Errorf("moving forward does not name the commit acknowledged before:\n%s", out)
	}
	if out := mustRun(t, "ack", "--through", first).stdout; !strings.Contains(out, "before "+second[:12]) ||
		!strings.Contains(out, "lists the stories committed in between again") {
		t.Errorf("moving back does not say the log lists stories again:\n%s", out)
	}
}

func TestAcknowledgingNeedsACommitOnThisBranch(t *testing.T) {
	root := finishedStory(t)
	mustRun(t, "stop")
	elsewhere := gitOut(t, root, "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "elsewhere")

	for _, args := range [][]string{
		{"ack"},
		{"ack", "--through", " "},
		{"ack", "--through", "nope"},
		// -x here is the value of --through, and git would read it as an option.
		{"ack", "--through", "-x"},
		{"ack", "--through", elsewhere},
	} {
		if r := run(t, args...); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0034") {
			t.Errorf("sdlc %s was not refused as a bad argument (exit %d):\n%s", strings.Join(args, " "), r.code, r.stderr)
		}
	}
	if r := run(t, "ack", "--through", elsewhere); !strings.Contains(r.stderr, "not on this branch") {
		t.Errorf("a commit off this branch was refused without saying so:\n%s", r.stderr)
	}
	if _, ok := acknowledgedIn(t, root); ok {
		t.Error("a refused ack wrote .sdlc/state/acknowledged")
	}
}

// The log refuses a file that names no commit git has, and ack is how it is put
// right: it has to write over what it cannot read.
func TestAcknowledgingReplacesACommitGitDoesNotHave(t *testing.T) {
	root := finishedStory(t)
	mustRun(t, "stop")
	writeFile(t, root, ".sdlc/state/acknowledged", "0123456789abcdef\n")

	got := decode[ackPayload](t, mustRun(t, "ack", "--through", "HEAD", "--json"))
	if got.Through != headOf(t, root) || got.Previous != "0123456789abcdef" || got.Back {
		t.Errorf("ack over a commit git does not have = %+v, want the file's text as previous", got)
	}
	if r := run(t, "log"); r.code != 0 {
		t.Errorf("the log still refuses after ack put the file right:\n%s", r.stderr)
	}
}

// Doctor is where a person checks the loop's own files, and the log refuses one
// of them when it names no commit git has.
func TestDoctorNamesAnAcknowledgedCommitGitDoesNotHave(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	commitNamed(t, root, "take part in the loop")
	writeFile(t, root, ".sdlc/state/acknowledged", "0123456789abcdef\n")

	loopState := func() check {
		for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
			if c.Name == "loop state" {
				return c
			}
		}
		t.Fatal("doctor ran no loop state check")
		return check{}
	}
	if c := loopState(); c.State != stateProblem || !strings.Contains(c.Detail, ".sdlc/state/acknowledged") ||
		!strings.Contains(c.Fix, "sdlc ack --through") {
		t.Errorf("doctor did not name the acknowledged commit git does not have: %+v", c)
	}

	mustRun(t, "ack", "--through", "HEAD")
	if c := loopState(); c.State != stateOK {
		t.Errorf("doctor still reports the loop state after ack put it right: %+v", c)
	}
}

// Git that does not run says nothing about a commit. Reporting one as missing
// sent people to `sdlc ack`, which needs the same git, and doctor gave a second
// problem for the one cause its git command check already named.
func TestGitThatDoesNotRunIsNotACommitThatIsNotThere(t *testing.T) {
	root := finishedStory(t)
	mustRun(t, "stop")
	writeFile(t, root, ".sdlc/state/acknowledged", "0123456789abcdef\n")
	t.Setenv("PATH", t.TempDir())

	for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
		if strings.Contains(c.Detail, "no commit") || strings.Contains(c.Fix, "sdlc ack") {
			t.Errorf("with git not on PATH, doctor reported the acknowledged commit as missing: %+v", c)
		}
	}
	for _, args := range [][]string{
		{"ack", "--through", "HEAD"},
		{"log"},
		{"log", "--since", "HEAD"},
	} {
		r := run(t, args...)
		if r.code == 0 || strings.Contains(r.stderr, "no commit") ||
			strings.Contains(r.stderr, "SDLC-E0034") || strings.Contains(r.stderr, "SDLC-E0047") {
			t.Errorf("with git not on PATH, sdlc %s said a commit is not there (exit %d):\n%s",
				strings.Join(args, " "), r.code, r.stderr)
		}
	}
}
