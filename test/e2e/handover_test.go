// US-008, in the sandbox: a gate failure or a reviewer's block that reaches
// the loop's limit hands the story to a person or changes nothing. The built
// binary is run in the story's own worktree, by the session working it, with
// git taken off PATH for the one command whose hand-over is to be refused;
// internal/cli holds the other things that stop a hand-over, which need a
// directory's permissions changed under the command.

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// withoutGit is a command of the session under test, put to the hook like any
// other and then run with nothing on PATH but the sandbox's own sdlc.
func (sb sandbox) withoutGit(t *testing.T, agent, stdin string, args ...string) result {
	t.Helper()
	dir := sb.tree()
	sb.allows(t, dir, agent, "Bash", map[string]any{"command": "sdlc " + strings.Join(args, " ")})
	cmd := exec.CommandContext(t.Context(), sb.sdlc, args...)
	cmd.Dir = dir
	for _, v := range sb.env(t, sandboxSession) {
		if strings.HasPrefix(strings.ToUpper(v), "PATH=") {
			v = "PATH=" + filepath.Dir(sb.sdlc)
		}
		cmd.Env = append(cmd.Env, v)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	code := 0
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("sdlc %s could not be run: %v", strings.Join(args, " "), err)
	}
	return result{out.String(), errOut.String(), code}
}

// handOverRecord is the story's gate record, as bytes and read.
func (sb sandbox) handOverRecord(t *testing.T) ([]byte, model.Record) {
	t.Helper()
	path := filepath.Join(sb.repo, ".sdlc", "stories", "US-001", model.RecordFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record model.Record
	readJSON(t, path, &record)
	return raw, record
}

// handOverReviews is every file in the story's reviews directory, by name.
func (sb sandbox) handOverReviews(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(sb.repo, ".sdlc", "stories", "US-001", "reviews"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// refusedAndNothingRecorded holds a refused hand-over to AC-1 and AC-5: a
// non-zero exit, nothing printed as done, the record byte for byte, the
// backlog still calling the story in_progress, and a refusal that says the
// failure was not recorded and that the command is safe to run again.
func (sb sandbox) refusedAndNothingRecorded(t *testing.T, r result, before []byte) {
	t.Helper()
	if r.code == 0 || r.stdout != "" {
		t.Fatalf("the hand-over was not refused: exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, "git add did not finish") {
		t.Fatalf("the command was refused for something other than the hand-over it could not make:\n%s", r.stderr)
	}
	if after, _ := sb.handOverRecord(t); !bytes.Equal(after, before) {
		t.Errorf("the refused command changed the gate record.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := storiesOf(t, sb.repo); got["US-001"] != "in_progress" {
		t.Errorf("the refused command left the story %q, want in_progress", got["US-001"])
	}
	said := strings.ToLower(strings.Join(strings.Fields(r.stderr), " "))
	if !strings.Contains(said, "not recorded") || !strings.Contains(said, "again") || !strings.Contains(said, "safe") {
		t.Errorf("the refusal does not say that the failure was not recorded and that running the command again is safe:\n%s", r.stderr)
	}
}

func TestUS008AGateFailureAtTheLimitHandsTheStoryOverOrChangesNothingInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	failed := model.GateEvent(model.GateDoR, model.GateFail)
	fail := []string{"gate", "dor", "fail", "--note", "AC-2 is not testable"}

	// `sdlc init` sets loop.max_rework_rounds to 3.
	sb.works(t, tree, "", "", fail...)

	t.Run("AC-4 a failure below the limit is recorded without git", func(t *testing.T) {
		r := sb.withoutGit(t, "", "", fail...)
		want := "US-001  dor  fail\n  AC-2 is not testable\n"
		if r.code != 0 || r.stdout != want || r.stderr != "" {
			t.Fatalf("the second failure of three allowed: exit %d\nstdout %q\nstderr %q\nwant stdout %q", r.code, r.stdout, r.stderr, want)
		}
		if _, record := sb.handOverRecord(t); record.SinceDecision(model.EventGate, failed) != 2 || len(record.Escalations) != 0 {
			t.Errorf("the record counts %d dor failures and %d escalations, want 2 and none",
				record.SinceDecision(model.EventGate, failed), len(record.Escalations))
		}
	})

	t.Run("AC-1 AC-5 the third failure, with git gone, is refused and changes nothing", func(t *testing.T) {
		before, _ := sb.handOverRecord(t)
		for range 2 {
			sb.refusedAndNothingRecorded(t, sb.withoutGit(t, "", "", fail...), before)
		}
	})

	t.Run("AC-2 the same command with git back is counted once and hands the story over", func(t *testing.T) {
		out := sb.works(t, tree, "", "", fail...)
		for _, want := range []string{"US-001 is handed to a person", "dor has failed 3 times", "sdlc approve US-001"} {
			if !strings.Contains(out, want) {
				t.Errorf("the hand-over does not say %q:\n%s", want, out)
			}
		}
		_, record := sb.handOverRecord(t)
		if n := record.SinceDecision(model.EventGate, failed); n != 3 {
			t.Errorf("the record counts %d dor failures, want 3: the limit, with the last counted once", n)
		}
		pending, ok := record.PendingEscalation()
		if !ok || pending.Type != "gate_failing" || len(record.Escalations) != 1 {
			t.Errorf("the record holds %+v, want one pending gate_failing question", record.Escalations)
		}
		if got := storiesOf(t, sb.repo); got["US-001"] != "awaiting_human" {
			t.Errorf("the story is %q, want awaiting_human", got["US-001"])
		}
	})
}

func TestUS008ABlockAtTheLimitHandsTheStoryOverOrChangesNothingInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GateDesignReview); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}
	blocked := model.ReviewEvent(model.GateDesignReview, "architect", model.VerdictBlock)
	block := []string{"review", "add", "design_review", "architect", "block", "--note", "AC-3 has no step"}

	// `sdlc init` sets loop.max_review_rounds to 2.
	t.Run("AC-4 a block below the limit is recorded without git", func(t *testing.T) {
		r := sb.withoutGit(t, "architect", "# architect\n\nAC-3 has no step.\n", block...)
		if r.code != 0 || r.stderr != "" || !strings.Contains(r.stdout, "block (round 1)") {
			t.Fatalf("the first block of two allowed: exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
		}
	})

	t.Run("AC-3 AC-5 the second block, with git gone, is refused and leaves no review file", func(t *testing.T) {
		before, _ := sb.handOverRecord(t)
		files := sb.handOverReviews(t)
		for range 2 {
			sb.refusedAndNothingRecorded(t, sb.withoutGit(t, "architect", "# architect\n\nrefused.\n", block...), before)
			if after := sb.handOverReviews(t); !slices.Equal(after, files) {
				t.Errorf("the refused review left a file the record does not list: reviews/ held %v and holds %v", files, after)
			}
		}
	})

	t.Run("AC-3 the same command with git back is counted once and hands the story over", func(t *testing.T) {
		out := sb.works(t, tree, "architect", "# architect\n\nAC-3 still has no step.\n", block...)
		for _, want := range []string{"block (round 2)", "US-001 is handed to a person", "architect has blocked design_review 2 times"} {
			if !strings.Contains(out, want) {
				t.Errorf("the hand-over does not say %q:\n%s", want, out)
			}
		}
		_, record := sb.handOverRecord(t)
		if n := record.SinceDecision(model.EventReview, blocked); n != 2 {
			t.Errorf("the record counts %d blocks by the architect, want 2: the limit, with the last counted once", n)
		}
		pending, ok := record.PendingEscalation()
		if !ok || pending.Type != "review_not_converging" || len(record.Escalations) != 1 {
			t.Errorf("the record holds %+v, want one pending review_not_converging question", record.Escalations)
		}
		if got := storiesOf(t, sb.repo); got["US-001"] != "awaiting_human" {
			t.Errorf("the story is %q, want awaiting_human", got["US-001"])
		}
		var listed []string
		for _, rev := range record.Reviews {
			if rev.Gate == model.GateDesignReview && rev.Role == "architect" {
				listed = append(listed, filepath.Base(filepath.FromSlash(rev.File)))
			}
		}
		var kept []string
		for _, name := range sb.handOverReviews(t) {
			if strings.HasPrefix(name, "design_review-architect-") {
				kept = append(kept, name)
			}
		}
		slices.Sort(listed)
		if !slices.Equal(kept, listed) {
			t.Errorf("reviews/ holds the architect's %v and the record lists %v", kept, listed)
		}
	})
}
