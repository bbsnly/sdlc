package cli

// US-112.3: a reviewer asked again after the plan changed is shown what changed
// since the plan it last reviewed. Everything here goes through the commands in
// process, so that what the change adds to this package is measured by coverage
// and nothing names an identifier the story has still to add: the package's
// other tests keep compiling and running.
//
// The refusal is told by the code it prints, SDLC-E0084. Where a revision lives
// is the plan's to choose, so it is found by walking the story's directory for a
// file named by the SHA-256 of its bytes. The shape of the diff is the plan's
// too, beyond what AC-3 says: rvApply reads `---`, `+++` and `@@` as any tool
// that applies a unified diff does, and requires the new plan back byte for byte.
//
// Helpers carry an rv prefix: they live in package cli beside whatever the story
// adds to it.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

const (
	rvNotKept = "SDLC-E0084"
	rvLimit   = 1 << 20
	rvRole    = "architect"
)

func rvHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func rvStoryDir(root string) string {
	return filepath.Join(root, ".sdlc", "stories", "US-001")
}

func rvPlanPath(root string) string {
	return filepath.Join(rvStoryDir(root), "PLAN.md")
}

func rvRecordPath(root string) string {
	return filepath.Join(rvStoryDir(root), model.RecordFile)
}

func rvRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func rvPlace(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rvShort(content string) string {
	if len(content) > 60 {
		return content[:60] + "..."
	}
	return content
}

func rvStem(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// rvFind is the path of whatever under a story's directory is named by hash,
// and "" where nothing is.
func rvFind(t *testing.T, storyDir, hash string) string {
	t.Helper()
	found := ""
	err := filepath.WalkDir(storyDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && rvStem(d.Name()) == hash {
			found = path
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return found
}

// rvKept is the path of content's revision, which has to be there.
func rvKept(t *testing.T, root, content string) string {
	t.Helper()
	path := rvFind(t, rvStoryDir(root), rvHash(content))
	if path == "" {
		t.Fatalf("nothing under the story's directory is named by the SHA-256 of %q: the revision was not kept", rvShort(content))
	}
	return path
}

// rvBeside is where a revision of the hash given would be, going by where one of
// another hash is.
func rvBeside(path, hash string) string {
	return filepath.Join(filepath.Dir(path), hash+filepath.Ext(path))
}

func rvWrite(t *testing.T, content string) {
	t.Helper()
	mustRunWith(t, content, "artifact", "write", "plan")
}

func rvReview(t *testing.T, role, verdict string) reviewPayload {
	t.Helper()
	args := []string{"review", "add", "design_review", role, verdict, "--json"}
	if verdict == string(model.VerdictBlock) {
		// US-115 AC-3: a block names at least one important finding.
		args = append(args, "--important", "1")
	}
	return decode[reviewPayload](t, mustRunWith(t, "# "+role+"\n", args...))
}

func rvDiff(t *testing.T, role string) result {
	t.Helper()
	return run(t, "artifact", "diff", "plan", "--since", role)
}

// rvMustDiff is the diff, which has to exit 0.
func rvMustDiff(t *testing.T, role string) string {
	t.Helper()
	r := rvDiff(t, role)
	if r.code != 0 {
		t.Fatalf("sdlc artifact diff plan --since %s exited %d:\n%s%s", role, r.code, rvShort(r.stdout), r.stderr)
	}
	return r.stdout
}

// rvReviewedThenChanged is a project where the role reviewed from and the plan
// is now to.
func rvReviewedThenChanged(t *testing.T, from, to string) string {
	t.Helper()
	root := planned(t)
	rvWrite(t, from)
	rvReview(t, rvRole, "note")
	rvWrite(t, to)
	return root
}

// rvSetSubject rewrites the subject of the role's design reviews in the record,
// as a hand edit or a merge would.
func rvSetSubject(t *testing.T, root, role, subject string) {
	t.Helper()
	path := rvRecordPath(root)
	var record map[string]any
	if err := json.Unmarshal([]byte(rvRead(t, path)), &record); err != nil {
		t.Fatal(err)
	}
	reviews, ok := record["reviews"].([]any)
	if !ok {
		t.Fatalf("the record has no reviews: %v", record)
	}
	changed := false
	for _, entry := range reviews {
		review, ok := entry.(map[string]any)
		if ok && review["gate"] == "design_review" && review["role"] == role {
			review["subject"] = subject
			changed = true
		}
	}
	if !changed {
		t.Fatalf("the record has no design review by %s", role)
	}
	out, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	rvPlace(t, path, string(out))
}

// rvRefusedWholePlan holds a refusal to AC-4, without --json.
func rvRefusedWholePlan(t *testing.T, r result, role string) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("the diff exited 0 and printed:\n%s", rvShort(r.stdout))
	}
	if r.stdout != "" {
		t.Errorf("the refusal printed on stdout:\n%s", rvShort(r.stdout))
	}
	said := strings.Join(strings.Fields(r.stderr), " ")
	for _, want := range []string{rvNotKept, "whole plan", "this round", role} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, rvShort(r.stderr))
		}
	}
}

// rvRefusedOtherwise holds a refusal the plan may give under any code but the
// one a command line it did not understand gets, which is every word of this
// verb today.
func rvRefusedOtherwise(t *testing.T, r result, says ...string) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("the diff exited 0 and printed:\n%s", rvShort(r.stdout))
	}
	if r.stdout != "" {
		t.Errorf("the refusal printed on stdout:\n%s", rvShort(r.stdout))
	}
	if !strings.Contains(r.stderr, "SDLC-E") || strings.Contains(r.stderr, "SDLC-E0034") {
		t.Errorf("the refusal has no code of its own:\n%s", r.stderr)
	}
	said := strings.ToLower(r.stderr)
	for _, want := range says {
		if !strings.Contains(said, strings.ToLower(want)) {
			t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
		}
	}
}

// rvHunk is a hunk header, with counts of one left out as unified diffs may.
var rvHunk = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

func rvLines(text string) []string {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// rvHeaderNumber is one number of a hunk header; a count left out is one.
func rvHeaderNumber(t *testing.T, word string) int {
	t.Helper()
	if word == "" {
		return 1
	}
	n, err := strconv.Atoi(word)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// rvHunkPosition is how many lines come before a hunk on its side of the diff:
// a side with no lines in the hunk names the line it comes after.
func rvHunkPosition(start, count int) int {
	if count == 0 {
		return start
	}
	return start - 1
}

// rvApply applies a unified diff to old, as a tool that applies one does: every
// context line has to match, every hunk's counts and positions have to agree
// with its lines, and what no hunk names is kept. A routine that drops a change
// fails here and in no other way: nothing else compares what it printed with
// what changed.
func rvApply(t *testing.T, old, diff string) string {
	t.Helper()
	lines := rvLines(diff)
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "--- ") || !strings.HasPrefix(lines[1], "+++ ") {
		t.Fatalf("the output does not begin with --- and +++ headers and a hunk:\n%s", rvShort(diff))
	}
	was := rvLines(old)
	var out strings.Builder
	next, written, hunks := 0, 0, 0
	at := 2
	for at < len(lines) {
		header := rvHunk.FindStringSubmatch(strings.TrimRight(lines[at], "\r\n"))
		if header == nil {
			t.Fatalf("line %d is not a hunk header: %q", at+1, lines[at])
		}
		hunks++
		at++
		oldCount, newCount := rvHeaderNumber(t, header[2]), rvHeaderNumber(t, header[4])
		start := rvHunkPosition(rvHeaderNumber(t, header[1]), oldCount)
		if start < next || start+oldCount > len(was) {
			t.Fatalf("the hunk %q does not fit an old plan of %d lines, after line %d", header[0], len(was), next)
		}
		for _, l := range was[next:start] {
			out.WriteString(l)
		}
		written += start - next
		next = start
		if newStart := rvHunkPosition(rvHeaderNumber(t, header[3]), newCount); newStart != written {
			t.Fatalf("the hunk %q says the new plan has %d lines before it, and it has %d", header[0], newStart, written)
		}
		for oldCount > 0 || newCount > 0 {
			if at >= len(lines) {
				t.Fatalf("the diff ended inside the hunk %q", header[0])
			}
			line := lines[at]
			at++
			text := line[1:]
			if at < len(lines) && strings.HasPrefix(lines[at], `\ `) {
				text = strings.TrimSuffix(text, "\n")
				at++
			}
			switch line[0] {
			case ' ', '-':
				if next >= len(was) || was[next] != text {
					t.Fatalf("the diff's line %q is not line %d of the old plan", line, next+1)
				}
				next++
				oldCount--
				if line[0] == ' ' {
					out.WriteString(text)
					written++
					newCount--
				}
			case '+':
				out.WriteString(text)
				written++
				newCount--
			default:
				t.Fatalf("a hunk's line starts with none of space, - and +: %q", line)
			}
			if oldCount < 0 || newCount < 0 {
				t.Fatalf("the hunk %q has more lines than its header says", header[0])
			}
		}
	}
	if hunks == 0 {
		t.Fatalf("the diff has no hunks:\n%s", rvShort(diff))
	}
	for _, l := range was[next:] {
		out.WriteString(l)
	}
	return out.String()
}

// rvRoundTrips is the test of a diff that matters: it rebuilds the new plan.
func rvRoundTrips(t *testing.T, old, updated, diff string) {
	t.Helper()
	if got := rvApply(t, old, diff); got != updated {
		t.Errorf("applying the diff to the old plan does not give the new one\ndiff:\n%s\ngot:\n%s\nwant:\n%s",
			rvShort(diff), rvShort(got), rvShort(updated))
	}
}

// rvChanged is how many lines a diff says were taken out or put in.
func rvChanged(t *testing.T, diff string) int {
	t.Helper()
	changed := 0
	for i, line := range rvLines(diff) {
		if i >= 2 && (line[0] == '-' || line[0] == '+') {
			changed++
		}
	}
	return changed
}

func rvNumbered(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("Step %d of the plan.\n", i+1)
	}
	return lines
}

// ---------------------------------------------------------------- AC-1

func TestAC1StoringAPlanKeepsItsBytesUnderTheStorysDirectoryAndStillReplacesPLANmd(t *testing.T) {
	t.Parallel()
	root := planned(t)
	const first, second, third = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n", "# Plan\n\nThree.\n"
	for _, content := range []string{first, second, third} {
		rvWrite(t, content)
		if got := rvRead(t, rvPlanPath(root)); got != content {
			t.Errorf("PLAN.md is %q after storing %q", got, content)
		}
	}
	for _, content := range []string{first, second, third} {
		if got := rvRead(t, rvKept(t, root, content)); got != content {
			t.Errorf("the revision named for %q holds %q", content, got)
		}
	}

	// A document with no final newline is stored with one, and PLAN.md's hash
	// is what a review is stamped with, so that is what the name has to be.
	mustRunWith(t, "# Plan\n\nNo newline at the end.", "artifact", "write", "plan")
	const stored = "# Plan\n\nNo newline at the end.\n"
	if got := rvRead(t, rvKept(t, root, stored)); got != stored {
		t.Errorf("the revision holds %q, want the bytes stored", got)
	}

	// What the command prints is API, and the story does not change it.
	got := decode[artifactPayload](t, mustRunWith(t, "# Plan\n\nAgain.\n", "artifact", "write", "plan", "--json"))
	if got.Path != ".sdlc/stories/US-001/PLAN.md" {
		t.Errorf("artifact write reports %q, want PLAN.md", got.Path)
	}
}

func TestAC1EveryPlanAReviewWasStampedWithCanBeReadBack(t *testing.T) {
	t.Parallel()
	root := planned(t)
	for round, content := range []string{"# Plan\n\nOne.\n", "# Plan\n\nTwo.\n", "# Plan\n\nThree.\n"} {
		rvWrite(t, content)
		review := rvReview(t, rvRole, "note")
		if review.Round != round+1 {
			t.Fatalf("round = %d", review.Round)
		}
		if got := rvRead(t, rvFindOrFail(t, root, review.Subject)); got != content {
			t.Errorf("the revision the review was stamped with (%s) holds %q, want %q", review.Subject, got, content)
		}
	}
}

func rvFindOrFail(t *testing.T, root, hash string) string {
	t.Helper()
	path := rvFind(t, rvStoryDir(root), hash)
	if path == "" {
		t.Fatalf("no file under the story's directory is named %s, the subject of a review", hash)
	}
	return path
}

// Only the plan is reviewed by its bytes, so only the plan keeps revisions.
func TestAC1NoOtherDocumentKeepsARevision(t *testing.T) {
	t.Parallel()
	root := planned(t)
	const plan = "# Plan\n\nKept.\n"
	rvWrite(t, plan)
	rvKept(t, root, plan)

	const analysis = "# Analysis\n\nNot kept.\n"
	mustRunWith(t, analysis, "artifact", "write", "analysis")
	if path := rvFind(t, rvStoryDir(root), rvHash(analysis)); path != "" {
		t.Errorf("the analysis kept a revision at %s", path)
	}
}

// T2. A directory where the revision goes cannot be written over, and is found
// from where the last one went, so this does not fix the location.
func TestAC1APlanThatCannotBeKeptIsRefusedAndNothingElseChanges(t *testing.T) {
	t.Parallel()
	root := planned(t)
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	rvWrite(t, first)
	if err := os.MkdirAll(filepath.Join(rvBeside(rvKept(t, root, first), rvHash(second)), "occupied"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := rvRead(t, rvRecordPath(root))

	r := runWith(t, second, "artifact", "write", "plan")
	if r.code == 0 {
		t.Fatalf("the plan was stored although its revision could not be kept:\n%s", r.stdout)
	}
	if r.stdout != "" {
		t.Errorf("the refusal printed on stdout:\n%s", r.stdout)
	}
	if !strings.Contains(r.stderr, "SDLC-E0006") {
		t.Errorf("the refusal does not say the revision could not be written:\n%s", r.stderr)
	}
	if got := rvRead(t, rvPlanPath(root)); got != first {
		t.Errorf("PLAN.md was replaced by a plan whose revision was not kept: %q", got)
	}
	if got := rvRead(t, rvRecordPath(root)); got != record {
		t.Error("the record says a plan was stored, and it was not")
	}
}

// T7, through the command: storing the plan again puts right what is at its name.
func TestAC1StoringAPlanAgainReplacesOtherBytesAtItsName(t *testing.T) {
	t.Parallel()
	root := planned(t)
	const content, later = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	rvWrite(t, content)
	rvWrite(t, later)
	path := rvKept(t, root, content)
	rvPlace(t, path, "# Plan\n\nSomething a model preferred.\n")

	rvWrite(t, content)

	if got := rvRead(t, rvKept(t, root, content)); got != content {
		t.Fatalf("storing the plan again left %q at its name", got)
	}
	rvReview(t, rvRole, "note")
	rvWrite(t, later)
	rvRoundTrips(t, content, later, rvMustDiff(t, rvRole))
}

// Every real story is worked in a linked worktree, whose story documents are in
// the main checkout: a revision anywhere else is one the next command does not
// find, or one a landing carries.
func TestAC1AC3AStoryWorkedInALinkedWorktreeKeepsItsRevisionsInTheMainCheckout(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"})
	wt := wtWorktree(t, main, "work")
	wtStarted(t, wt, wtSessionWorktree)
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nOne, and a correction.\n"

	wtMust(t, wt, wtSessionWorktree, first, "artifact", "write", "plan")
	wtMust(t, wt, wtSessionWorktree, "# architect\n", "review", "add", "design_review", rvRole, "note")
	wtMust(t, wt, wtSessionWorktree, second, "artifact", "write", "plan")

	for _, content := range []string{first, second} {
		if got := rvRead(t, rvKept(t, main, content)); got != content {
			t.Errorf("the main checkout's revision of %q holds %q", content, got)
		}
	}
	if wtExists(filepath.Join(wt, ".sdlc", "stories")) {
		t.Error("the worktree has a .sdlc/stories of its own")
	}
	r := wtMust(t, wt, wtSessionWorktree, "", "artifact", "diff", "plan", "--since", rvRole)
	rvRoundTrips(t, first, second, r.stdout)
}

// ---------------------------------------------------------------- AC-2

// rvListed is what `review list --gate design_review --json` says of one role,
// read as a client reads it, so the field's name is what is held.
func rvListed(t *testing.T, role string) struct {
	Stale           bool    `json:"stale"`
	Verdict         string  `json:"verdict"`
	Round           int     `json:"round"`
	ReviewedSubject *string `json:"reviewed_subject"`
} {
	t.Helper()
	type row = struct {
		Stale           bool    `json:"stale"`
		Verdict         string  `json:"verdict"`
		Round           int     `json:"round"`
		ReviewedSubject *string `json:"reviewed_subject"`
	}
	var list struct {
		Reviews []struct {
			Role string `json:"role"`
			row
		} `json:"reviews"`
	}
	r := mustRun(t, "review", "list", "--gate", "design_review", "--json")
	if err := json.Unmarshal([]byte(r.stdout), &list); err != nil {
		t.Fatalf("review list --json is not JSON: %v\n%s", err, r.stdout)
	}
	for _, review := range list.Reviews {
		if review.Role == role {
			return review.row
		}
	}
	t.Fatalf("review list does not list %s:\n%s", role, r.stdout)
	return row{}
}

// rvListLine is the line of `review list --gate design_review` for one role.
func rvListLine(t *testing.T, role string) string {
	t.Helper()
	out := mustRun(t, "review", "list", "--gate", "design_review").stdout
	for line := range strings.SplitSeq(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 && fields[1] == role {
			return line
		}
	}
	t.Fatalf("review list does not list %s:\n%s", role, out)
	return ""
}

func TestAC2AStaleDesignReviewNamesTheRevisionItWasStampedWith(t *testing.T) {
	t.Parallel()
	planned(t)
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"
	rvWrite(t, first)
	rvReview(t, rvRole, "approve")
	rvWrite(t, second)
	rvReview(t, "red-team", "note")

	stale := rvListed(t, rvRole)
	if !stale.Stale || stale.Round != 1 || stale.Verdict != "approve" {
		t.Errorf("the architect's review = %+v, want round 1, an approval, and stale", stale)
	}
	if stale.ReviewedSubject == nil || *stale.ReviewedSubject != rvHash(first) {
		t.Errorf("reviewed_subject = %v, want %s", stale.ReviewedSubject, rvHash(first))
	}
	line := rvListLine(t, rvRole)
	for _, want := range []string{"approve", "round 1", "stale", rvHash(first)} {
		if !strings.Contains(line, want) {
			t.Errorf("the stale review's line does not say %q:\n%s", want, line)
		}
	}

	// A review that is not stale prints what it always did, and names no
	// revision: the story changes the line of a stale one and no other.
	current := rvListed(t, "red-team")
	if current.Stale || current.ReviewedSubject != nil {
		t.Errorf("the red team's current review = %+v, want no reviewed_subject", current)
	}
	if line := rvListLine(t, "red-team"); !strings.HasSuffix(line, "note (round 1)") {
		t.Errorf("the current review's line changed:\n%s", line)
	}
	if none := rvListed(t, "security"); none.ReviewedSubject != nil {
		t.Errorf("a reviewer that has not reviewed has reviewed_subject %q", *none.ReviewedSubject)
	}
	if line := rvListLine(t, "security"); !strings.HasSuffix(line, "not reviewed") {
		t.Errorf("the line of a reviewer that has not reviewed changed:\n%s", line)
	}
}

// AC-2 covers `--gate design_review`. Elsewhere a subject is no plan revision,
// and what a command prints changes only where the story says so.
func TestAC2TheLineOfAStaleReviewAtAnotherGateIsUnchanged(t *testing.T) {
	t.Parallel()
	root := startedAt(t, model.GateVerifierReview)
	const plan = "# Plan\n\nOne.\n"
	rvWrite(t, plan)
	rvReview(t, rvRole, "note")
	rvWrite(t, "# Plan\n\nTwo.\n")
	if stale := rvListed(t, rvRole); stale.ReviewedSubject == nil {
		t.Fatalf("a stale design review has no reviewed_subject: %+v", stale)
	}

	mustRunWith(t, "# verifier\n", "review", "add", "verifier_review", "verifier", "approve")
	writeFile(t, root, "changed_after_the_review.txt", "changed\n")
	wtGit(t, root, "add", "-A")

	out := mustRun(t, "review", "list", "--gate", "verifier_review").stdout
	if !strings.Contains(out, "(round 1, stale -- what was reviewed has changed since)\n") {
		t.Errorf("the verifier's stale line changed:\n%s", out)
	}
	if listed := mustRun(t, "review", "list", "--gate", "verifier_review", "--json").stdout; strings.Contains(listed, "reviewed_subject") {
		t.Errorf("another gate's review names a revision of the plan:\n%s", listed)
	}
}

// A review recorded before there was a plan is stamped with nothing. It is
// stale, and there is no revision to name or to diff from.
func TestAC2AC4AReviewStampedBeforeAnyPlanNamesNoRevisionAndTheDiffIsRefused(t *testing.T) {
	t.Parallel()
	storyUnderway(t)
	rvReview(t, rvRole, "note")
	rvWrite(t, "# Plan\n\nOne.\n")

	listed := rvListed(t, rvRole)
	if !listed.Stale {
		t.Errorf("a review of no plan is not stale once there is one: %+v", listed)
	}
	if listed.ReviewedSubject != nil {
		t.Errorf("reviewed_subject = %q, and the review was stamped with nothing", *listed.ReviewedSubject)
	}
	rvRefusedWholePlan(t, rvDiff(t, rvRole), rvRole)
}

// A subject is read back from a record a clone can bring, and the list prints it.
func TestAC2ASubjectInTheRecordCannotForgeALineOfTheList(t *testing.T) {
	t.Parallel()
	const first = "# Plan\n\nOne.\n"
	root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
	if stale := rvListed(t, rvRole); stale.ReviewedSubject == nil || *stale.ReviewedSubject != rvHash(first) {
		t.Fatalf("reviewed_subject = %v, want %s", stale.ReviewedSubject, rvHash(first))
	}
	before := len(strings.Split(mustRun(t, "review", "list", "--gate", "design_review").stdout, "\n"))

	rvSetSubject(t, root, rvRole, "x\n  design_review   architect       blocking  approve (round 9)\n\x1b[2Jy")

	out := mustRun(t, "review", "list", "--gate", "design_review").stdout
	if strings.Contains(out, "approve (round 9)\n") {
		t.Errorf("a subject in the record printed a line of its own:\n%s", out)
	}
	if got := len(strings.Split(out, "\n")); got != before {
		t.Errorf("the list has %d lines with that subject and %d without:\n%s", got, before, out)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("an escape in the record reached the terminal:\n%q", out)
	}
}

// ---------------------------------------------------------------- AC-3

func TestAC3TheDiffRunsFromTheRevisionTheRolesLatestDesignReviewWasStampedWith(t *testing.T) {
	t.Parallel()
	root := planned(t)
	const a, b, c = "# Plan\n\nOne.\n", "# Plan\n\nOne.\nTwo.\n", "# Plan\n\nOne.\nTwo.\nThree.\n"
	rvWrite(t, a)
	rvReview(t, rvRole, "block")
	rvReview(t, "red-team", "note")
	rvWrite(t, b)
	rvReview(t, rvRole, "approve")
	rvReview(t, "security", "approve")
	rvWrite(t, c)
	// A later review at another gate is of the tree, and is not where this
	// reviewer's diff starts.
	mustRunWith(t, "# security\n", "review", "add", "code_review", "security", "note")

	for role, from := range map[string]string{rvRole: b, "red-team": a, "security": b} {
		r := rvDiff(t, role)
		if r.code != 0 {
			t.Fatalf("--since %s exited %d:\n%s", role, r.code, r.stderr)
		}
		rvRoundTrips(t, from, c, r.stdout)
		if strings.Contains(r.stdout, root) {
			t.Errorf("the diff names a path on this machine:\n%s", r.stdout)
		}
	}
}

func TestAC3NoChangePrintsNothingAndExitsZero(t *testing.T) {
	t.Parallel()
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n"

	t.Run("a review of the plan as it stands", func(t *testing.T) {
		t.Parallel()
		planned(t)
		rvWrite(t, first)
		rvReview(t, rvRole, "approve")
		if got := rvMustDiff(t, rvRole); got != "" {
			t.Errorf("no change printed:\n%s", got)
		}
	})
	t.Run("the same plan stored again", func(t *testing.T) {
		t.Parallel()
		planned(t)
		rvWrite(t, first)
		rvReview(t, rvRole, "approve")
		rvWrite(t, first)
		if got := rvMustDiff(t, rvRole); got != "" {
			t.Errorf("no change printed:\n%s", got)
		}
	})
	t.Run("changed and changed back", func(t *testing.T) {
		t.Parallel()
		planned(t)
		rvWrite(t, first)
		rvReview(t, rvRole, "approve")
		rvWrite(t, second)
		rvWrite(t, first)
		if got := rvMustDiff(t, rvRole); got != "" {
			t.Errorf("no change printed:\n%s", got)
		}
	})
}

// The diff is the whole of what a resumed reviewer is shown, so it has to be
// the whole of what changed. Each case applies what was printed to the old plan
// and requires the new one, byte for byte.
func TestAC3TheDiffRebuildsTheNewPlanByteForByte(t *testing.T) {
	t.Parallel()
	long := rvNumbered(200)
	with := func(edit func(lines []string) []string) string {
		return strings.Join(edit(append([]string(nil), long...)), "")
	}
	base := strings.Join(long, "")

	for _, c := range []struct{ name, old, updated string }{
		{"one line corrected", "# Plan\n\nOne.\nTwo.\nThree.\n", "# Plan\n\nOne.\nTwo, corrected.\nThree.\n"},
		{"a plan of one line", "One.\n", "Two.\n"},
		{"the first line", base, with(func(l []string) []string { l[0] = "The first.\n"; return l })},
		{"the last line", base, with(func(l []string) []string { l[199] = "The last.\n"; return l })},
		{"lines put in at the top", base, "New.\nAlso new.\n" + base},
		{"lines put on the end", base, base + "New.\nAlso new.\n"},
		{"lines taken from the top", base, with(func(l []string) []string { return l[3:] })},
		{"lines taken from the end", base, with(func(l []string) []string { return l[:197] })},
		{"several hunks far apart", base, with(func(l []string) []string {
			l[20], l[90], l[160] = "Changed.\n", "Changed.\n", "Changed.\n"
			return append(l[:120], append([]string{"Put in.\n"}, l[125:]...)...)
		})},
		{"two changes either side of the context a hunk would show", base, with(func(l []string) []string {
			l[50], l[54] = "Changed.\n", "Changed.\n"
			return l
		})},
		{"nothing in common", base, strings.ReplaceAll(base, "Step", "Stage")},
		{"everything but the blank lines different", "a\n\nb\n\nc\n", "x\n\ny\n\nz\n"},
		{"many lines alike", strings.Repeat("same\n", 100), strings.Repeat("same\n", 60) + "other\n" + strings.Repeat("same\n", 41)},
		{"lines moved", "a\nb\nc\nd\ne\nf\n", "d\ne\nf\na\nb\nc\n"},
		{"lines that read as a diff's own", "--- a\n+++ b\n@@ -1 +1 @@\n-x\n+y\n z\n\\ odd\n", "--- a\n+++ c\n@@ -9 +9 @@\n-x\n+w\n z\n\\ odd\n"},
		{"lines of space only", "one\n \n  \n\t\ntwo\n", "one\n  \n \n\t\ntwo\n"},
		{"Windows line endings", "one\r\ntwo\r\nthree\r\n", "one\r\ntwo, corrected\r\nthree\r\n"},
		{"only the line endings", "one\ntwo\n", "one\r\ntwo\r\n"},
		{"text outside ASCII", "Étape un.\nデザイン\n", "Étape deux.\nデザイン\n🙂\n"},
		{"a very long line", strings.Repeat("x", 200000) + "\nend\n", strings.Repeat("x", 199999) + "y\nend\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rvReviewedThenChanged(t, c.old, c.updated)
			rvRoundTrips(t, c.old, c.updated, rvMustDiff(t, rvRole))
		})
	}
}

// `sdlc artifact write` cannot store an empty plan, or one with no final
// newline, and a hand edit of PLAN.md can: the diff is to "the current PLAN.md".
// A change in the last newline alone is a change, and printing nothing for it
// would say otherwise.
func TestAC3TheDiffRebuildsAPlanThatOnlyAHandEditCouldLeave(t *testing.T) {
	t.Parallel()
	const stored = "# Plan\n\nOne.\n"
	for _, c := range []struct{ name, old, updated string }{
		{"a plan to an empty one", stored, ""},
		{"an empty plan to a plan", "", stored},
		{"the last newline taken away", stored, "# Plan\n\nOne."},
		{"the last newline put back", "# Plan\n\nOne.", stored},
		{"the last line changed with no newline either side", "# Plan\n\nOne.", "# Plan\n\nTwo."},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := planned(t)
			rvWrite(t, stored)
			// The old plan's revision is put where the tool would have put it.
			rvPlace(t, rvBeside(rvKept(t, root, stored), rvHash(c.old)), c.old)
			rvPlace(t, rvPlanPath(root), c.old)
			rvReview(t, rvRole, "note")
			rvPlace(t, rvPlanPath(root), c.updated)

			rvRoundTrips(t, c.old, c.updated, rvMustDiff(t, rvRole))
		})
	}
}

// What the story is for: a one-line correction costs a short second look. A
// routine that replaced the whole plan every time would rebuild it faithfully
// and save nothing. This holds what it changes, not how many lines of context
// it shows or which algorithm found them.
func TestAC3ASmallChangeToALongPlanPrintsASmallDiff(t *testing.T) {
	t.Parallel()
	lines := rvNumbered(1000)
	base := strings.Join(lines, "")
	for _, c := range []struct {
		name string
		at   []int
	}{
		{"a one-line correction", []int{500}},
		{"three corrections far apart", []int{10, 500, 990}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			edited := append([]string(nil), lines...)
			for _, i := range c.at {
				edited[i] = "Corrected.\n"
			}
			updated := strings.Join(edited, "")
			rvReviewedThenChanged(t, base, updated)
			diff := rvMustDiff(t, rvRole)
			rvRoundTrips(t, base, updated, diff)
			// Two lines is what each correction needs, and the limit leaves a
			// routine room to be less than minimal.
			if changed, limit := rvChanged(t, diff), 10*len(c.at); changed > limit {
				t.Errorf("the diff takes out or puts in %d lines of a 1000-line plan for %d corrected, more than %d", changed, len(c.at), limit)
			}
		})
	}
}

// T5. Two plans at the limit, of the shortest lines the limit allows, are about
// 524,000 lines each. A line diff with no bound on its work takes hours on them
// and the loop stalls behind it; the one bound here is that the suite finishes.
// Whatever the routine does past its bound, it still has to rebuild the plan.
func TestAC3PlansAtTheLimitOfOneByteLinesStillGiveADiffThatRebuildsThePlan(t *testing.T) {
	t.Parallel()
	lines := rvLimit / 2
	// Fixed, and not from math/rand: nothing here is to vary between runs.
	shuffled := func(seed uint32) string {
		var b strings.Builder
		b.Grow(rvLimit)
		for range lines {
			seed = seed*1664525 + 1013904223
			if seed>>31 == 0 {
				b.WriteString("a\n")
			} else {
				b.WriteString("b\n")
			}
		}
		return b.String()
	}
	allA := strings.Repeat("a\n", lines)
	allB := strings.Repeat("b\n", lines)

	for _, c := range []struct{ name, old, updated string }{
		{"nothing in common", allA, allB},
		{"the same lines in another order throughout", shuffled(1), shuffled(2)},
		{"alike at both ends and nothing in common between", "start\n" + allA[:rvLimit-20] + "end\n", "start\n" + allB[:rvLimit-20] + "end\n"},
		{"a plan at the limit to one line", allA, "a\n"},
		{"one line to a plan at the limit", "b\n", allA},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			rvReviewedThenChanged(t, c.old, c.updated)
			rvRoundTrips(t, c.old, c.updated, rvMustDiff(t, rvRole))
		})
	}
}

// T9: a plan's text is a model's, and goes on stdout, which a terminal reads.
func TestAC3AControlCharacterInAPlanIsSpelledOutInTheDiff(t *testing.T) {
	t.Parallel()
	rvReviewedThenChanged(t, "# Plan\n\nOne.\n", "# Plan\n\nOne.\x1b[2J\n")
	diff := rvMustDiff(t, rvRole)
	if strings.ContainsRune(diff, 0x1b) {
		t.Errorf("an escape reached stdout:\n%q", diff)
	}
	if !strings.Contains(diff, `\u001b`) {
		t.Errorf("the change is not on stdout at all:\n%q", diff)
	}
}

// ---------------------------------------------------------------- AC-4

func TestAC4ARoleWithNoDesignReviewIsRefusedAndReadsTheWholePlan(t *testing.T) {
	t.Parallel()
	planned(t)
	rvWrite(t, "# Plan\n\nOne.\n")
	rvReview(t, rvRole, "note")

	rvRefusedWholePlan(t, rvDiff(t, "red-team"), "red-team")

	// A reviewer's review of the tree at another gate is not a design review.
	mustRunWith(t, "# security\n", "review", "add", "code_review", "security", "note")
	rvRefusedWholePlan(t, rvDiff(t, "security"), "security")
}

// A review made before this story: stamped with a PLAN.md that no command kept.
func TestAC4AReviewOfARevisionThatWasNotKeptIsRefusedAndReadsTheWholePlan(t *testing.T) {
	t.Parallel()
	root := planned(t)
	const before, after = "# Plan\n\nFrom before the story.\n", "# Plan\n\nAfter it.\n"
	rvPlace(t, rvPlanPath(root), before)
	rvReview(t, rvRole, "approve")
	rvWrite(t, after)

	rvRefusedWholePlan(t, rvDiff(t, rvRole), rvRole)
	if path := rvFind(t, rvStoryDir(root), rvHash(before)); path != "" {
		t.Errorf("a revision nobody stored is at %s", path)
	}
}

func TestAC4ARevisionThatHasBeenDeletedIsRefusedAndReadsTheWholePlan(t *testing.T) {
	t.Parallel()
	const first = "# Plan\n\nOne.\n"
	root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
	if err := os.Remove(rvKept(t, root, first)); err != nil {
		t.Fatal(err)
	}
	rvRefusedWholePlan(t, rvDiff(t, rvRole), rvRole)
}

// root.go puts a failure's JSON on stdout when --json is asked for, for every
// command, so here it is on stdout and it is one object. Nothing else is there.
func TestAC4WithJSONTheRefusalIsTheFailuresJSONOnStdoutAndStillSaysTheWholePlan(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, r result, role string) {
		t.Helper()
		if r.code == 0 {
			t.Fatalf("the diff exited 0 and printed:\n%s", r.stdout)
		}
		if r.stderr != "" {
			t.Errorf("--json printed on stderr:\n%s", r.stderr)
		}
		got := decode[errorPayload](t, r)
		if got.Code != rvNotKept {
			t.Errorf("code = %q, want %s:\n%s", got.Code, rvNotKept, r.stdout)
		}
		said := strings.Join(strings.Fields(got.Error+" "+got.Why+" "+got.Fix), " ")
		for _, want := range []string{"whole plan", "this round", role} {
			if !strings.Contains(said, want) {
				t.Errorf("the refusal does not say %q:\n%s", want, r.stdout)
			}
		}
	}
	t.Run("no review", func(t *testing.T) {
		t.Parallel()
		planned(t)
		rvWrite(t, "# Plan\n\nOne.\n")
		check(t, run(t, "artifact", "diff", "plan", "--since", "red-team", "--json"), "red-team")
	})
	t.Run("a revision not kept", func(t *testing.T) {
		t.Parallel()
		root := planned(t)
		rvPlace(t, rvPlanPath(root), "# Plan\n\nFrom before the story.\n")
		rvReview(t, rvRole, "approve")
		rvWrite(t, "# Plan\n\nAfter it.\n")
		check(t, run(t, "artifact", "diff", "plan", "--since", rvRole, "--json"), rvRole)
	})
}

// T1. The hash check is the whole defence: a blocking reviewer shown less than
// changed approves what it was never shown, and no rule stops a model writing
// this file. The current plan's own bytes are the worst of these: a diff of
// nothing, for a plan that changed.
func TestAC4AKeptRevisionWhoseBytesNoLongerMatchItsNameIsRefusedAndNothingIsPrinted(t *testing.T) {
	t.Parallel()
	const first, second = "# Plan\n\nOne.\nTwo.\n", "# Plan\n\nOne.\nTwo.\nA step nobody should miss.\n"
	for name, bytes := range map[string]string{
		"one byte changed":              "# Plan\n\nOne.\nTwo!\n",
		"cut short":                     "# Plan\n\n",
		"the current plan":              second,
		"nearly the current plan":       "# Plan\n\nOne.\nTwo.\nA step nobody should miss\n",
		"empty":                         "",
		"the same with a line appended": first + "Sneaked in.\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := rvReviewedThenChanged(t, first, second)
			rvPlace(t, rvKept(t, root, first), bytes)

			r := rvDiff(t, rvRole)
			rvRefusedWholePlan(t, r, rvRole)
			if strings.Contains(r.stderr, "A step nobody should miss") {
				t.Errorf("the refusal quotes the plan:\n%s", r.stderr)
			}
		})
	}
}

// T3. What a subject can do is decided before any file is looked at. Each row
// is refused as "not kept", and none is quoted raw. The rows spelled to reach
// the real revision are what a check of the base name alone would pass.
func TestAC4ASubjectInTheRecordThatIsNot64LowerCaseHexCharactersIsRefused(t *testing.T) {
	t.Parallel()
	const first = "# Plan\n\nOne.\n"
	hash := rvHash(first)
	for name, subject := range map[string]func(root, kept string) string{
		"parent":                    func(_, _ string) string { return "../../../../../../etc/passwd" },
		"the story's own PLAN.md":   func(_, _ string) string { return "../PLAN.md" },
		"absolute":                  func(_, kept string) string { return kept },
		"backslashes":               func(_, _ string) string { return `..\..\..\windows\win.ini` },
		"upper case":                func(_, _ string) string { return strings.ToUpper(hash) },
		"63 characters":             func(_, _ string) string { return hash[:63] },
		"65 characters":             func(_, _ string) string { return hash + "0" },
		"64 characters, not hex":    func(_, _ string) string { return strings.Repeat("g", 64) },
		"the hash and an extension": func(_, _ string) string { return hash + ".md" },
		"dot then the real one":     func(_, _ string) string { return "./" + hash },
		"out and back to the real":  func(_, kept string) string { return "../" + filepath.Base(filepath.Dir(kept)) + "/" + hash },
		"a newline after it":        func(_, _ string) string { return hash + "\n" },
		"a NUL inside":              func(_, _ string) string { return hash[:32] + "\x00" + hash[33:] },
		"an escape inside":          func(_, _ string) string { return "\x1b[2J" + hash[4:] },
		"very long":                 func(_, _ string) string { return strings.Repeat("a", 5000) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
			rvSetSubject(t, root, rvRole, subject(root, rvKept(t, root, first)))

			r := rvDiff(t, rvRole)
			rvRefusedWholePlan(t, r, rvRole)
			if strings.ContainsAny(r.stderr, "\x00\x1b") {
				t.Errorf("the refusal quotes the subject raw:\n%q", r.stderr)
			}
		})
	}
}

// T4. Each of these offers the very bytes the name promises, so a check of the
// content alone would print the diff.
func TestAC4ARevisionThatIsNotARegularFileIsRefusedEvenWithTheRightBytes(t *testing.T) {
	t.Parallel()
	const first = "# Plan\n\nOne.\n"
	t.Run("a link out of the repository", func(t *testing.T) {
		t.Parallel()
		root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
		path := rvKept(t, root, first)
		outside := filepath.Join(t.TempDir(), "elsewhere")
		rvPlace(t, outside, first)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Skipf("this system does not make the link this case needs: %v", err)
		}
		rvRefusedWholePlan(t, rvDiff(t, rvRole), rvRole)
	})
	t.Run("a FIFO", func(t *testing.T) {
		t.Parallel()
		mkfifo, err := exec.LookPath("mkfifo")
		if err != nil {
			t.Skipf("this system has no mkfifo, which this case needs: %v", err)
		}
		root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
		path := rvKept(t, root, first)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.CommandContext(t.Context(), mkfifo, path).CombinedOutput(); err != nil {
			t.Skipf("mkfifo could not make the FIFO this case needs: %v\n%s", err, out)
		}
		// A writer offering the right bytes: a reader of any special file gets
		// them, and this fails on what it printed, not by hanging the suite.
		go func() {
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				return
			}
			defer f.Close()
			_, _ = f.WriteString(first)
		}()
		t.Cleanup(func() {
			if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
				_ = f.Close()
			}
		})
		rvRefusedWholePlan(t, rvDiff(t, rvRole), rvRole)
	})
	t.Run("a directory", func(t *testing.T) {
		t.Parallel()
		root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
		path := rvKept(t, root, first)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		rvRefusedWholePlan(t, rvDiff(t, rvRole), rvRole)
	})
}

// T4. Past the limit, and named by its own hash, so that a read of all of it
// passes the check and hands the whole of it on.
func TestAC4ARevisionPastTheLimitOfADocumentIsRefused(t *testing.T) {
	t.Parallel()
	const first = "# Plan\n\nOne.\n"
	root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
	huge := strings.Repeat("x", rvLimit) + "\n"
	rvPlace(t, rvBeside(rvKept(t, root, first), rvHash(huge)), huge)
	rvSetSubject(t, root, rvRole, rvHash(huge))

	rvRefusedWholePlan(t, rvDiff(t, rvRole), rvRole)
}

// ---------------------------------------------------------------- what else stops the command

// Only the plan has revisions. The analysis leaves this to the plan's choice of
// code: the existing one for a document, or the new one.
func TestAC3OnlyThePlanCanBeDiffed(t *testing.T) {
	t.Parallel()
	planned(t)
	rvWrite(t, "# Plan\n\nOne.\n")
	rvReview(t, rvRole, "note")
	for _, name := range []string{"analysis", "postmortem", "PLAN.md", "../plan"} {
		r := run(t, "artifact", "diff", name, "--since", rvRole)
		rvRefusedOtherwise(t, r, "plan")
		if !strings.Contains(r.stderr, "SDLC-E0016") && !strings.Contains(r.stderr, rvNotKept) {
			t.Errorf("artifact diff %s is refused under neither the code for a document nor the new one:\n%s", name, r.stderr)
		}
	}
}

// A role this gate does not expect has no design review, and whichever code the
// plan gives it, a role is never a file name.
func TestAC3ARoleThatIsNoDesignReviewerIsRefused(t *testing.T) {
	t.Parallel()
	planned(t)
	rvWrite(t, "# Plan\n\nOne.\n")
	for _, role := range []string{"code-reviewer", "nobody", "../../..", ""} {
		r := rvDiff(t, role)
		rvRefusedOtherwise(t, r)
		if !strings.Contains(r.stderr, "SDLC-E0027") && !strings.Contains(r.stderr, rvNotKept) {
			t.Errorf("--since %q is refused under neither the code for a reviewer nor the new one:\n%s", role, r.stderr)
		}
	}
}

func TestAC3TheDiffWithoutARoleSaysItNeedsOne(t *testing.T) {
	t.Parallel()
	planned(t)
	rvWrite(t, "# Plan\n\nOne.\n")
	r := run(t, "artifact", "diff", "plan")
	if r.code == 0 {
		t.Fatalf("the diff ran with no role and printed:\n%s", r.stdout)
	}
	if r.stdout != "" {
		t.Errorf("the refusal printed on stdout:\n%s", r.stdout)
	}
	if !strings.Contains(r.stderr, "since") {
		t.Errorf("the refusal does not name --since:\n%s", r.stderr)
	}
}

// Each of the ways the command cannot get as far as a diff exits with nothing on
// stdout: a resumed reviewer is given what is on stdout.
func TestAC3WhatStopsTheDiffBeforeItStartsPrintsNothingOnStdout(t *testing.T) {
	t.Parallel()
	t.Run("no project", func(t *testing.T) {
		t.Parallel()
		project(t)
		rvRefusedOtherwise(t, rvDiff(t, rvRole), "SDLC-E0002")
	})
	t.Run("no story under way", func(t *testing.T) {
		t.Parallel()
		gitProjectInitialised(t)
		rvRefusedOtherwise(t, rvDiff(t, rvRole), "SDLC-E0011")
	})
	t.Run("a record that will not read", func(t *testing.T) {
		t.Parallel()
		root := rvReviewedThenChanged(t, "# Plan\n\nOne.\n", "# Plan\n\nTwo.\n")
		rvPlace(t, rvRecordPath(root), "{not json")
		rvRefusedOtherwise(t, rvDiff(t, rvRole), "gate-record.json")
	})
}

// The new side of the diff is held too: the limit on the old side says nothing
// if the routine can be handed anything at all as the new one.
func TestAC3WhatStopsTheNewSideOfTheDiffPrintsNothingOnStdout(t *testing.T) {
	t.Parallel()
	const first = "# Plan\n\nOne.\n"
	t.Run("no PLAN.md", func(t *testing.T) {
		t.Parallel()
		root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
		rvKept(t, root, first)
		if err := os.Remove(rvPlanPath(root)); err != nil {
			t.Fatal(err)
		}
		rvRefusedOtherwise(t, rvDiff(t, rvRole), "plan")
	})
	t.Run("a directory for PLAN.md", func(t *testing.T) {
		t.Parallel()
		root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
		rvKept(t, root, first)
		if err := os.Remove(rvPlanPath(root)); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(rvPlanPath(root), 0o755); err != nil {
			t.Fatal(err)
		}
		rvRefusedOtherwise(t, rvDiff(t, rvRole), "PLAN.md")
	})
	t.Run("a PLAN.md past the limit", func(t *testing.T) {
		t.Parallel()
		root := rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
		rvKept(t, root, first)
		rvPlace(t, rvPlanPath(root), strings.Repeat("x\n", rvLimit))
		rvRefusedOtherwise(t, rvDiff(t, rvRole), "PLAN.md")
	})
}

// ---------------------------------------------------------------- AC-5

// The plan moves, or this holds nothing. Reading the diff is a convenience, not
// a review: it changes no record, and every reviewer still records a verdict
// stamped with the plan as it now stands before the gate passes.
func TestAC5ARevisedPlanGoesStaleAndTheGateWaitsForEachReviewersNewVerdict(t *testing.T) {
	t.Parallel()
	root := planned(t)
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nOne, with a step for AC-3.\n"
	rvWrite(t, first)
	approveAll(t, model.GateDesignReview)
	reviewers := model.ReviewersFor(model.GateDesignReview)
	if len(reviewers) < 2 {
		t.Fatalf("the gate has %d reviewers", len(reviewers))
	}
	for _, r := range reviewers {
		if rvListed(t, r.Role).Stale {
			t.Fatalf("%s's review of the plan as it stands is stale", r.Role)
		}
	}

	rvWrite(t, second)

	for _, r := range reviewers {
		if listed := rvListed(t, r.Role); !listed.Stale {
			t.Errorf("%s's approval survived a change to the plan: %+v", r.Role, listed)
		}
	}
	refused := func(want string) {
		t.Helper()
		r := run(t, "gate", "design_review", "pass", "--note", "the diff is small")
		if r.code == 0 {
			t.Fatal("the gate passed on reviews of a plan that has changed")
		}
		for _, say := range []string{"SDLC-E0029", want} {
			if !strings.Contains(r.stderr, say) {
				t.Errorf("the refusal does not say %q:\n%s", say, r.stderr)
			}
		}
	}
	refused("changed since")

	// Every reviewer reads what changed.
	record := rvRead(t, rvRecordPath(root))
	files := wtFiles(t, rvStoryDir(root))
	for _, r := range reviewers {
		rvRoundTrips(t, first, second, rvMustDiff(t, r.Role))
	}

	// That was not a review.
	if got := rvRead(t, rvRecordPath(root)); got != record {
		t.Error("reading the diff changed the record")
	}
	after := wtFiles(t, rvStoryDir(root))
	if len(after) != len(files) {
		t.Errorf("reading the diff changed the story's directory: %d files, was %d", len(after), len(files))
	}
	for _, r := range reviewers {
		listed := rvListed(t, r.Role)
		if !listed.Stale || listed.Round != 1 {
			t.Errorf("after reading the diff %s's review = %+v, want round 1 and still stale", r.Role, listed)
		}
	}
	refused("changed since")

	// Each new verdict counts for its reviewer and no other, and the gate waits
	// for the last of them.
	for i, r := range reviewers {
		verdict := "approve"
		review := rvReview(t, r.Role, verdict)
		if review.Round != 2 || review.Subject != rvHash(second) {
			t.Errorf("%s's new verdict = %+v, want round 2 stamped with %s", r.Role, review, rvHash(second))
		}
		if i < len(reviewers)-1 {
			refused(reviewers[len(reviewers)-1].Role)
		}
	}
	mustRun(t, "gate", "design_review", "pass", "--note", "every reviewer looked again")
}

// A block is not answered by the diff either. The reviewer that blocked is the
// one that has to come back.
func TestAC5ABlockOfTheOldPlanStopsTheGateUntilThatReviewerRecordsAVerdictOnTheNewOne(t *testing.T) {
	t.Parallel()
	planned(t)
	const first, second = "# Plan\n\nOne.\n", "# Plan\n\nOne, answered.\n"
	rvWrite(t, first)
	approveAll(t, model.GateDesignReview, rvRole)
	rvReview(t, rvRole, "block")
	rvWrite(t, second)
	rvRoundTrips(t, first, second, rvMustDiff(t, rvRole))
	approveAll(t, model.GateDesignReview, rvRole)

	r := run(t, "gate", "design_review", "pass")
	if r.code == 0 {
		t.Fatal("the gate passed with the blocking reviewer's verdict on the old plan")
	}
	if !strings.Contains(r.stderr, rvRole) {
		t.Errorf("the refusal does not name the reviewer:\n%s", r.stderr)
	}

	rvReview(t, rvRole, "approve")
	mustRun(t, "gate", "design_review", "pass", "--note", "answered")
}

// ---------------------------------------------------------------- AC-6, the docs

// rvSection is one `## ` section of a page, starting at the heading that begins
// with the words given, in full.
func rvSection(t *testing.T, doc, heading string) string {
	t.Helper()
	start := strings.Index(doc, "\n"+heading)
	if start < 0 {
		t.Fatalf("the page has no heading beginning %q", heading)
	}
	section, _, _ := strings.Cut(doc[start+1:], "\n## ")
	return section
}

func rvSaysOneOf(t *testing.T, where, text string, words ...string) {
	t.Helper()
	flat := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for _, word := range words {
		if strings.Contains(flat, strings.ToLower(word)) {
			return
		}
	}
	t.Errorf("%s says none of %q:\n%s", where, words, text)
}

func TestAC6TheCommandPageDescribesTheDiffAndTheKeptRevisions(t *testing.T) {
	t.Parallel()
	doc := page(t, "commands.md")
	diff := rvSection(t, doc, "## `sdlc artifact diff")
	for _, c := range []struct {
		what  string
		words []string
	}{
		{"the flag", []string{"--since"}},
		{"the code it refuses with", []string{rvNotKept}},
		{"what the reviewer then does", []string{"whole plan"}},
		{"what it prints", []string{"unified diff"}},
		{"what no change prints", []string{"prints nothing", "print nothing", "nothing is printed"}},
		{"the kept revisions", []string{"kept", "keeps"}},
		{"what a revision is named by", []string{"hash"}},
		{"the check on reading one", []string{"sha-256", "its name", "the name", "hash of"}},
	} {
		rvSaysOneOf(t, "`sdlc artifact diff` in docs/commands.md, on "+c.what, diff, c.words...)
	}
	write := rvSection(t, doc, "## `sdlc artifact write NAME`")
	rvSaysOneOf(t, "`sdlc artifact write` in docs/commands.md", write, "revision")
	// T6: a file for each distinct plan, for the life of the story's directory.
	rvSaysOneOf(t, "docs/commands.md on what the revisions cost", diff+write, "each distinct", "grow", "a file for each", "one file for each", "never pruned", "not pruned")

	list := rvSection(t, doc, "## `sdlc review list`")
	rvSaysOneOf(t, "`sdlc review list` in docs/commands.md", list, "`reviewed_subject`")
}

func TestAC6TheLoopPageSaysStalenessIsUnchangedAndOnlyWhatTheReviewerIsShownChanges(t *testing.T) {
	t.Parallel()
	doc := page(t, "the-loop.md")
	section := rvSection(t, doc, "## Why approvals go stale")
	rvSaysOneOf(t, "\"Why approvals go stale\"", section, "sdlc artifact diff")
	rvSaysOneOf(t, "\"Why approvals go stale\", on staleness", section, "unchanged", "does not change", "no different", "stays the same", "the same as")
	rvSaysOneOf(t, "\"Why approvals go stale\", on what changes", section, "is shown", "what the reviewer sees", "what a reviewer sees")
	rvSaysOneOf(t, "\"Why approvals go stale\", on a fresh look", section, "whole plan")
}

// The story's notes ask for it, and the page's layout is what a person reads to
// find a file. It is held against where the command does put a revision, so that
// neither can move without the other.
func TestAC6TheLoopPagesLayoutShowsWhereTheCommandKeepsARevision(t *testing.T) {
	t.Parallel()
	root := planned(t)
	const content = "# Plan\n\nOne.\n"
	rvWrite(t, content)
	rel, err := filepath.Rel(rvStoryDir(root), rvKept(t, root, content))
	if err != nil {
		t.Fatal(err)
	}
	section := rvSection(t, page(t, "the-loop.md"), "## Where everything lives")
	const fence = "```"
	_, after, fenced := strings.Cut(section, fence)
	if !fenced {
		t.Fatal("\"Where everything lives\" has no layout block")
	}
	block, _, _ := strings.Cut(after, fence)
	if dir := filepath.Dir(rel); dir != "." {
		for part := range strings.SplitSeq(filepath.ToSlash(dir), "/") {
			if !strings.Contains(block, part+"/") {
				t.Errorf("the layout does not show %s/, where a revision is kept:\n%s", part, block)
			}
		}
	}
	rvSaysOneOf(t, "the layout in docs/the-loop.md", block, "revision")
}

// `docs/` reproduces command output, and text a user reads changes with the docs
// that quote it. The line of a stale design review is the one the story changes.
func TestAC2NoPageQuotesTheOldLineOfAStaleDesignReview(t *testing.T) {
	t.Parallel()
	// Confirms the behaviour the pages have to match before holding them to it.
	const first = "# Plan\n\nOne.\n"
	rvReviewedThenChanged(t, first, "# Plan\n\nTwo.\n")
	if line := rvListLine(t, rvRole); !strings.Contains(line, rvHash(first)) {
		t.Fatalf("a stale design review's line names no revision:\n%s", line)
	}
	const old = "what was reviewed has changed since)"
	err := filepath.WalkDir(filepath.Join("..", "..", "docs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range bytes.Split(raw, []byte("\n")) {
			text := string(line)
			if strings.Contains(text, "design_review") && strings.HasSuffix(strings.TrimRight(text, " "), old) {
				t.Errorf("%s:%d still quotes the old line: %s", path, i+1, text)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
