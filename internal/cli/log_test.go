package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

func readRecord(t *testing.T, root, id string) model.Record {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", id, "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record model.Record
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

// editRecord rewrites a story's record the way an older sdlc, or a person, can
// leave it: as JSON, with whatever change makes to it.
func editRecord(t *testing.T, root string, change func(map[string]any)) {
	t.Helper()
	path := filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	change(record)
	if data, err = json.Marshal(record); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendTo(record map[string]any, key string, items ...any) {
	list, _ := record[key].([]any)
	record[key] = append(list, items...)
}

func headOf(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// commitNamed commits everything with a message, and returns the commit.
func commitNamed(t *testing.T, root, message string) string {
	t.Helper()
	writeFile(t, root, "notes.txt", message)
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return headOf(t, root)
}

func onlyEntry(t *testing.T, payload logPayload) logEntry {
	t.Helper()
	if len(payload.Entries) != 1 {
		t.Fatalf("the log lists %d stories, want 1: %+v", len(payload.Entries), payload.Entries)
	}
	return payload.Entries[0]
}

func forgetRange(record map[string]any) {
	delete(record, "commit_base")
	delete(record, "commit")
}

func TestTheLogListsACommittedStoryWithWhereItsCommitsAre(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	mustRun(t, "cost", "add", "--story", "US-001", "--usd", "1.50")
	record := readRecord(t, root, "US-001")

	payload := decode[logPayload](t, mustRun(t, "log", "--json"))
	if payload.SinceSource != sinceNone || payload.Since != "" {
		t.Errorf("nothing is acknowledged, and the log starts after %q (%s)", payload.Since, payload.SinceSource)
	}
	e := onlyEntry(t, payload)
	if e.Story != "US-001" || e.Title == "" || e.NotInBacklog {
		t.Errorf("the entry is %q %q (not in backlog: %v)", e.Story, e.Title, e.NotInBacklog)
	}
	if e.RangeSource != rangeFromRecord || e.CommitBase == "" ||
		e.CommitBase != record.CommitBase || e.Commit != record.Commit {
		t.Errorf("the range is %q..%q from %q, want the record's %s..%s",
			e.CommitBase, e.Commit, e.RangeSource, record.CommitBase, record.Commit)
	}
	if e.OffBranch || e.Missing {
		t.Errorf("a commit on HEAD's history was flagged: off branch %v, missing %v", e.OffBranch, e.Missing)
	}
	if e.CommittedAt != record.Gates[model.GateCommit].At {
		t.Errorf("committed at %q, want when the commit gate passed, %q", e.CommittedAt, record.Gates[model.GateCommit].At)
	}
	if e.Rounds["design_review"] != 1 || e.Rounds["code_review"] != 1 {
		t.Errorf("rounds = %v, want one of each review", e.Rounds)
	}
	if len(e.Blocks) != 0 || len(e.Reopened) != 0 || len(e.Unfrozen) != 0 || len(e.Escalations) != 0 {
		t.Errorf("a story that went straight through was flagged: %+v", e)
	}
	if e.SpentUSD != 1.5 {
		t.Errorf("spent = %v, want 1.5", e.SpentUSD)
	}

	out := mustRun(t, "log").stdout
	for _, want := range []string{
		record.CommitBase[:12] + ".." + record.Commit[:12] + "  US-001",
		"rounds     ", "spent      $1.50",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not say %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"unfrozen", "Committed since"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("the log says %q:\n%s", unwanted, out)
		}
	}
}

// A block, a freeze lifted by a person, gates reopened and a question handed
// over are what somebody reading trunk after the story wants to look at again.
func TestTheLogFlagsWhatIsWorthASecondLook(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop") // lifts the finished story's freeze, which is not flagged
	editRecord(t, root, func(r map[string]any) {
		appendTo(r, "reviews", map[string]any{
			"at": "2026-09-15T10:00:00Z", "gate": "design_review", "role": "architect",
			"verdict": "block", "round": 2, "note": "AC-3 has no step",
		})
		appendTo(r, "events",
			map[string]any{"at": "2026-09-15T10:01:00Z", "type": "unfreeze", "message": "AC-2 asserted the old message"},
			map[string]any{"at": "2026-09-15T10:01:00Z", "type": "gates_reopened", "message": "tests_frozen, plan reopened: the freeze was lifted"},
		)
		appendTo(r, "escalations", map[string]any{
			"at": "2026-09-15T10:02:00Z", "type": "spec_unclear", "message": "which currency?", "resolved": true,
		})
	})

	e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json")))
	if e.Rounds["design_review"] != 2 {
		t.Errorf("rounds = %v, want design_review at its second round", e.Rounds)
	}
	if len(e.Blocks) != 1 || e.Blocks[0].Role != "architect" || e.Blocks[0].Round != 2 || e.Blocks[0].Gate != "design_review" {
		t.Errorf("blocks = %+v, want the architect's at round 2", e.Blocks)
	}
	if len(e.Unfrozen) != 1 || e.Unfrozen[0].Message != "AC-2 asserted the old message" {
		t.Errorf("unfrozen = %+v, want only the person's", e.Unfrozen)
	}
	if len(e.Reopened) != 1 {
		t.Errorf("reopened = %+v, want one", e.Reopened)
	}
	if len(e.Escalations) != 1 || !e.Escalations[0].Resolved || e.Escalations[0].Type != "spec_unclear" {
		t.Errorf("escalations = %+v", e.Escalations)
	}

	out := mustRun(t, "log").stdout
	for _, want := range []string{
		"blocks     1: design_review architect (round 2)",
		"reopened   tests_frozen, plan reopened: the freeze was lifted",
		"unfrozen   AC-2 asserted the old message",
		"escalated  spec_unclear: which currency? (answered)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, finishedUnfreeze) {
		t.Errorf("the log flags the finished story's own freeze being lifted:\n%s", out)
	}
}

// An older sdlc kept no range. The commits whose messages name the story stand
// in for it, and the entry says so.
func TestAStoryWhoseRecordNamesNoCommitIsFoundByItsCommitMessages(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	editRecord(t, root, forgetRange)
	before := headOf(t, root)
	commitNamed(t, root, "US-001: the first part")
	commitNamed(t, root, "US-0010 is another story")
	last := commitNamed(t, root, "finishes the work for US-001.")
	commitNamed(t, root, "tidy up after the story")

	e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json")))
	if e.RangeSource != rangeFromMessage || e.CommitBase != before || e.Commit != last {
		t.Errorf("the range is %q..%q from %q, want %s..%s from commit messages",
			e.CommitBase, e.Commit, e.RangeSource, before, last)
	}
	out := mustRun(t, "log").stdout
	for _, want := range []string{before[:12] + ".." + last[:12] + "  US-001", "inferred from commit messages"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not say %q:\n%s", want, out)
		}
	}
}

// A range nothing records is not worked out from anything else, and a story
// that cannot be placed against what was acknowledged is listed anyway.
func TestAStoryNoCommitNamesIsListedWithItsRangeUnknown(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	editRecord(t, root, forgetRange)
	writeFile(t, root, ".sdlc/state/acknowledged", headOf(t, root)+"\n")

	payload := decode[logPayload](t, mustRun(t, "log", "--json"))
	if payload.SinceSource != sinceAcknowledged {
		t.Errorf("since_source = %q, want acknowledged", payload.SinceSource)
	}
	e := onlyEntry(t, payload)
	if e.RangeSource != rangeUnknown || e.Commit != "" || e.CommitBase != "" {
		t.Errorf("the range is %q..%q from %q, want it unknown", e.CommitBase, e.Commit, e.RangeSource)
	}
	out := mustRun(t, "log").stdout
	for _, want := range []string{"(unknown range)  US-001", "range      unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log does not say %q:\n%s", want, out)
		}
	}
}

func TestTheLogStartsAfterTheLastCommitAcknowledged(t *testing.T) {
	t.Parallel()
	root := twoStoriesDone(t)
	first, second := readRecord(t, root, "US-001").Commit, readRecord(t, root, "US-002").Commit
	stories := func(p logPayload) []string {
		var ids []string
		for _, e := range p.Entries {
			ids = append(ids, e.Story)
		}
		return ids
	}
	acknowledge := func(sha string) { writeFile(t, root, ".sdlc/state/acknowledged", sha+"\n") }

	if got := stories(decode[logPayload](t, mustRun(t, "log", "--json"))); strings.Join(got, " ") != "US-001 US-002" {
		t.Errorf("with nothing acknowledged the log lists %v, want both, oldest first", got)
	}

	acknowledge(first)
	payload := decode[logPayload](t, mustRun(t, "log", "--json"))
	if got := stories(payload); strings.Join(got, " ") != "US-002" || payload.Since != first ||
		payload.SinceSource != sinceAcknowledged {
		t.Errorf("after acknowledging %s the log lists %v after %q (%s)", first, got, payload.Since, payload.SinceSource)
	}
	if out := mustRun(t, "log").stdout; !strings.Contains(out, "Committed since "+first[:12]+", the last commit acknowledged:") {
		t.Errorf("the log does not say where it starts:\n%s", out)
	}

	acknowledge(second[:12])
	empty := mustRun(t, "log", "--json")
	if !strings.Contains(empty.stdout, `"entries":[]`) {
		t.Errorf("an empty log is not an empty list: %s", empty.stdout)
	}
	if out := mustRun(t, "log").stdout; !strings.Contains(out, "No story has been committed since "+second[:12]) ||
		!strings.Contains(out, "sdlc log --all") {
		t.Errorf("an empty log does not say why, or how to see the rest:\n%s", out)
	}

	all := decode[logPayload](t, mustRun(t, "log", "--all", "--json"))
	if got := stories(all); len(got) != 2 || all.SinceSource != sinceAll || all.Since != "" {
		t.Errorf("--all lists %v after %q (%s), want both", got, all.Since, all.SinceSource)
	}

	acknowledge(second)
	since := decode[logPayload](t, mustRun(t, "log", "--since", first, "--json"))
	if got := stories(since); strings.Join(got, " ") != "US-002" || since.SinceSource != sinceFlag || since.Since != first {
		t.Errorf("--since %s lists %v after %q (%s), want US-002", first, got, since.Since, since.SinceSource)
	}
}

func TestTheLogRefusesAStartItCannotPlace(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)

	if r := run(t, "log", "--since", "HEAD", "--all"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0034") {
		t.Errorf("--since with --all was not refused as a usage error (exit %d):\n%s", r.code, r.stderr)
	}
	// --all here is the value of --since, and git would read it as an option.
	if r := run(t, "log", "--since", "--all"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0034") {
		t.Errorf("--since --all was not refused (exit %d):\n%s", r.code, r.stderr)
	}
	r := run(t, "log", "--since", "nope")
	if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0034") || !strings.Contains(r.stderr, "nope") {
		t.Errorf("a --since git cannot read was not refused (exit %d):\n%s", r.code, r.stderr)
	}

	writeFile(t, root, ".sdlc/state/acknowledged", "0123456789abcdef\n")
	r = run(t, "log", "--json")
	failure := decode[errorPayload](t, r)
	if r.code == 0 || failure.Code != "SDLC-E0047" || !strings.Contains(failure.Error, ".sdlc/state/acknowledged") {
		t.Errorf("an acknowledged commit git does not have was not refused (exit %d): %+v", r.code, failure)
	}
	if r := run(t, "log", "--all"); r.code != 0 {
		t.Errorf("--all was refused over the acknowledged commit it ignores:\n%s", r.stderr)
	}
}

// gitOut runs git in the project and returns what it printed.
func gitOut(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

// A record is text, and the entry carries the commits git reads it as.
func TestTheLogNamesTheCommitsInFull(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	record := readRecord(t, root, "US-001")
	editRecord(t, root, func(r map[string]any) {
		r["commit_base"], r["commit"] = record.CommitBase[:12], record.Commit[:12]
	})

	r := mustRun(t, "log", "--json")
	e := onlyEntry(t, decode[logPayload](t, r))
	if e.CommitBase != record.CommitBase || e.Commit != record.Commit || e.Missing || e.OffBranch {
		t.Errorf("the range is %q..%q (missing %v, off branch %v), want %s..%s in full",
			e.CommitBase, e.Commit, e.Missing, e.OffBranch, record.CommitBase, record.Commit)
	}
	if !strings.Contains(r.stdout, `"spent_usd":0`) {
		t.Errorf("spent_usd is left out of a story with nothing spent: %s", r.stdout)
	}
}

// A commit the repository does not have, or one somebody wrote as an option,
// cannot be placed, so the story is listed whatever was acknowledged.
func TestAStoryWhoseCommitIsNotInTheRepositoryIsListed(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	writeFile(t, root, ".sdlc/state/acknowledged", headOf(t, root)+"\n")

	for _, commit := range []string{strings.Repeat("f", 40), "-x"} {
		editRecord(t, root, func(r map[string]any) { r["commit"] = commit })
		e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json")))
		if !e.Missing || e.OffBranch || e.Commit != commit {
			t.Errorf("a record naming %q gave %q (missing %v, off branch %v)", commit, e.Commit, e.Missing, e.OffBranch)
		}
		if out := mustRun(t, "log").stdout; !strings.Contains(out, "range      not in this repository") {
			t.Errorf("the log does not say %q is not in this repository:\n%s", commit, out)
		}
	}
}

// A commit this branch does not hold is listed whatever the log starts after,
// even when it starts after that very commit.
func TestAStoryWhoseCommitIsNotOnThisBranchIsListed(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	elsewhere := gitOut(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "made on another branch")
	editRecord(t, root, func(r map[string]any) { r["commit"] = elsewhere })
	writeFile(t, root, ".sdlc/state/acknowledged", headOf(t, root)+"\n")

	for _, args := range [][]string{{"log", "--json"}, {"log", "--since", elsewhere, "--json"}} {
		e := onlyEntry(t, decode[logPayload](t, mustRun(t, args...)))
		if !e.OffBranch || e.Missing || e.Commit != elsewhere {
			t.Errorf("sdlc %s gave %q (off branch %v, missing %v)", strings.Join(args, " "), e.Commit, e.OffBranch, e.Missing)
		}
	}
	if out := mustRun(t, "log").stdout; !strings.Contains(out, "range      not on this branch") {
		t.Errorf("the log does not say the commit is not on this branch:\n%s", out)
	}

	// A branch with no commits yet holds none of them.
	gitOut(t, root, "checkout", "--quiet", "--orphan", "fresh")
	e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--all", "--json")))
	if !e.OffBranch || e.Missing {
		t.Errorf("on a branch with no commits the entry is off branch %v, missing %v", e.OffBranch, e.Missing)
	}
}

// Only a story whose commit gate stands passed is in the log.
func TestAStoryNotCommittedIsNotInTheLog(t *testing.T) {
	t.Parallel()
	root := startedAt(t, model.GateCommit)
	if out := mustRun(t, "log").stdout; !strings.Contains(out, "No story has been committed yet.") {
		t.Errorf("a story short of its commit gate is in the log:\n%s", out)
	}

	satisfy(t, root, model.GateCommit)
	mustRun(t, "gate", "commit", "pass", "--note", "on trunk")
	mustRun(t, "gate", "code_review", "fail", "--note", "one more look")
	if payload := decode[logPayload](t, mustRun(t, "log", "--json")); len(payload.Entries) != 0 {
		t.Errorf("a story whose commit gate was reopened is in the log: %+v", payload.Entries)
	}
}

// .sdlc/stories cannot be listed when something else occupies the name, and
// the log has nowhere left to read a committed story's record from.
func TestLogRefusesWhenTheStoriesDirectoryCannotBeListed(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	writeFile(t, root, ".sdlc/stories", "not a directory")

	if r := run(t, "log"); r.code == 0 {
		t.Fatal("log read a project whose stories directory is a plain file")
	}
}

// A record among many that will not read stops the whole log, rather than
// silently leaving that story out of it.
func TestLogRefusesWhenAStorysRecordCannotBeRead(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	writeFile(t, root, ".sdlc/stories/US-001/gate-record.json", "{not json")

	r := run(t, "log")
	if r.code == 0 {
		t.Fatal("log read a record that is not valid JSON")
	}
	if !strings.Contains(r.stderr, "gate-record.json") {
		t.Errorf("the refusal does not name the record:\n%s", r.stderr)
	}
}

// A story taken out of the backlog after it landed is still in the log: its
// commits are already on trunk, and the entry says it is not in the backlog
// rather than showing an empty title.
func TestALogEntryForAStoryNoLongerInTheBacklogSaysSo(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	writeFile(t, root, "user_stories.json", `{"stories":[]}`)

	e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--json")))
	if !e.NotInBacklog || e.Title != "" {
		t.Errorf("entry = %+v, want not_in_backlog with no title", e)
	}
	out := mustRun(t, "log").stdout
	if !strings.Contains(out, "(not in the backlog)") {
		t.Errorf("the prose does not say the story left the backlog:\n%s", out)
	}
}

// An older record with no commit named, on a branch that itself has no
// commits, cannot be placed by its messages either: there is nothing in
// history to search.
func TestALogEntryWithNoRangeOnABranchWithNoCommitsIsUnknownRatherThanSearched(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	editRecord(t, root, forgetRange)
	gitOut(t, root, "checkout", "--quiet", "--orphan", "fresh")

	e := onlyEntry(t, decode[logPayload](t, mustRun(t, "log", "--all", "--json")))
	if e.RangeSource != rangeUnknown || e.Commit != "" || e.CommitBase != "" {
		t.Errorf("entry = %+v, want an unknown range", e)
	}
}

// --since with nothing after it, and with something, each say so in their own
// words: the log is not always following the last commit acknowledged.
func TestLogSaysWhatSinceFlagFoundInItsOwnWords(t *testing.T) {
	t.Parallel()
	root, before := startedAtFrom(t, "")

	withEntries := mustRun(t, "log", "--since", before).stdout
	if !strings.Contains(withEntries, "Committed since "+before[:12]+":") {
		t.Errorf("--since with a story after it does not say so:\n%s", withEntries)
	}

	after := headOf(t, root)
	empty := mustRun(t, "log", "--since", after).stdout
	if !strings.Contains(empty, "No story has been committed since "+after[:12]+".") {
		t.Errorf("--since with nothing after it does not say so:\n%s", empty)
	}
}

// Two stories in one log read as two paragraphs, not one run-on block.
func TestTwoLogEntriesArePrintedWithABlankLineBetweenThem(t *testing.T) {
	t.Parallel()
	twoStoriesDone(t)

	out := mustRun(t, "log").stdout
	if !strings.Contains(out, "US-001") || !strings.Contains(out, "US-002") {
		t.Fatalf("both stories are not in the log:\n%s", out)
	}
	if idx := strings.Index(out, "US-001"); !strings.Contains(out[idx:], "\n\n") {
		t.Errorf("the two entries are not separated by a blank line:\n%s", out)
	}
}

// A record that names a commit but no base -- an older sdlc's, or one edited
// by hand -- prints as a single commit, not a range with nothing before it.
func TestALogEntryWithACommitButNoBaseHasNoRange(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	mustRun(t, "stop")
	record := readRecord(t, root, "US-001")
	editRecord(t, root, func(r map[string]any) { delete(r, "commit_base") })

	out := mustRun(t, "log").stdout
	want := shortCommit(record.Commit) + "  US-001"
	if !strings.Contains(out, want) {
		t.Errorf("the log does not print the commit alone:\n%s", out)
	}
	if strings.Contains(out, ".."+shortCommit(record.Commit)) {
		t.Errorf("the log invented a range with no base:\n%s", out)
	}
}
