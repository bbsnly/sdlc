package e2e

// US-112.3 AC-6: the built binary, through the plugin's launcher, in a real git
// repository with the story in its own worktree. Two plan revisions are stored,
// the first is reviewed, and the diff is read, each command put to the hook
// first: nothing in the hook changes, and the hook must let the new verb through
// to the conversation and to a reviewer (T12). internal/cli/us1123_test.go holds
// every case of the diff and its refusals in process.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

func rvHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// rvKept is what under the story's directory, in the main checkout, is named by
// the SHA-256 of content, or "".
func rvKept(t *testing.T, repo, content string) string {
	t.Helper()
	found := ""
	dir := filepath.Join(repo, ".sdlc", "stories", "US-001")
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if !d.IsDir() && strings.TrimSuffix(name, filepath.Ext(name)) == rvHash(content) {
			found = path
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return found
}

func TestAC6ASandboxStoresTwoPlanRevisionsReviewsTheFirstAndReadsTheDiff(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	tree := sb.tree()
	for gate := sb.nextGate(t); gate != string(model.GateDesignReview); gate = sb.nextGate(t) {
		if gate == "" {
			t.Fatal("the story passed its last gate without stopping at the design review")
		}
		sb.worksGate(t, model.Gate(gate))
	}

	// The first revision is what Gate 4 stored, and the architect reviews it.
	const second = "# Plan\n\nOne step, and a correction to it.\n"
	sb.works(t, tree, "architect", "# architect\n\nThe step is wrong.\n",
		"review", "add", "design_review", "architect", "block", "--important", "1", "--note", "the step is wrong")
	sb.works(t, tree, "implementer", second, "artifact", "write", "plan")

	for _, content := range []string{sandboxPlan, second} {
		path := rvKept(t, sb.repo, content)
		if path == "" {
			t.Fatalf("nothing under the story's directory is named by the SHA-256 of %q", content)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != content {
			t.Errorf("the revision named for %q holds %q", content, raw)
		}
	}

	var list struct {
		Reviews []struct {
			Role            string `json:"role"`
			Stale           bool   `json:"stale"`
			ReviewedSubject string `json:"reviewed_subject"`
		} `json:"reviews"`
	}
	listed := sb.works(t, tree, "", "", "review", "list", "--gate", "design_review", "--json")
	if err := json.Unmarshal([]byte(listed), &list); err != nil {
		t.Fatalf("review list --json printed something that is not JSON: %v\n%s", err, listed)
	}
	named := false
	for _, r := range list.Reviews {
		if r.Role == "architect" {
			named = r.Stale && r.ReviewedSubject == rvHash(sandboxPlan)
		}
	}
	if !named {
		t.Errorf("review list does not name the revision the architect reviewed:\n%s", listed)
	}
	if prose := sb.works(t, tree, "", "", "review", "list", "--gate", "design_review"); !strings.Contains(prose, rvHash(sandboxPlan)) {
		t.Errorf("the list does not name the revision the architect reviewed:\n%s", prose)
	}

	// The conversation reads the diff for the reviewer, and the reviewer can
	// read it again itself.
	for _, agent := range []string{"", "architect"} {
		diff := sb.works(t, tree, agent, "", "artifact", "diff", "plan", "--since", "architect")
		for _, want := range []string{"--- ", "+++ ", "@@ -", "-One step.\n", "+One step, and a correction to it.\n"} {
			if !strings.Contains(diff, want) {
				t.Errorf("the diff read by %q does not have %q:\n%s", agent, want, diff)
			}
		}
		if strings.Contains(diff, sb.repo) {
			t.Errorf("the diff names a path on this machine:\n%s", diff)
		}
	}

	// A reviewer that has not reviewed has nothing to diff from.
	r := sb.does(t, tree, "", "", "artifact", "diff", "plan", "--since", "red-team")
	if r.code == 0 || r.stdout != "" {
		t.Errorf("the diff for a reviewer with no review exited %d and printed %q", r.code, r.stdout)
	}
	said := strings.Join(strings.Fields(r.stderr), " ")
	for _, want := range []string{"SDLC-E0084", "red-team", "whole plan", "this round"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
		}
	}

	// A kept revision a model has edited is refused: nothing in the hook guards
	// the file, and the check against its name has to.
	path := rvKept(t, sb.repo, sandboxPlan)
	if err := os.WriteFile(path, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	r = sb.does(t, tree, "", "", "artifact", "diff", "plan", "--since", "architect")
	if r.code == 0 || r.stdout != "" || !strings.Contains(r.stderr, "SDLC-E0084") {
		t.Errorf("the diff from an edited revision exited %d, printed %q, and said:\n%s", r.code, r.stdout, r.stderr)
	}

	// Reading the diff was no review: the gate waits for every reviewer's
	// verdict on the plan as it now stands, and then it passes.
	if r := sb.does(t, tree, "", "", "gate", "design_review", "pass"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0029") {
		t.Errorf("the design review passed on reviews of a plan that has changed: exit %d\n%s", r.code, r.stderr)
	}
	sb.worksGate(t, model.GateDesignReview)
	if gate := sb.nextGate(t); gate == string(model.GateDesignReview) {
		t.Errorf("the design review did not pass once every reviewer had looked again")
	}
}
