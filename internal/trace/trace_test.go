package trace

// US-112: the trace is a story's telemetry, beside its gate record and never in
// it. These hold the writer and the reader to the format the story names:
// one JSON object a line, at most 4,096 bytes, appended without the store's
// lock, never past 4 MiB, never holding a key outside the one list, and read
// back by a reader that a torn line or a newer kind does not stop.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// traceName is the file's name in a story's directory, as AC-1 gives it.
const traceName = "trace.jsonl"

// storyDir is a story's directory, empty, as a story has before its first
// record.
func storyDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".sdlc", "stories", "A-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func lines(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, traceName))
	if err != nil {
		t.Fatalf("the trace was not written: %v", err)
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		t.Fatalf("the trace does not end in a newline: %q", raw)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func decoded(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("a trace line is not one JSON object: %v\n%s", err, line)
	}
	return m
}

// at is the instant AC-1 asks for: RFC 3339, UTC, to the millisecond.
var at = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

func TestAC1TheFirstRecordCreatesTheTraceAndEachLineCarriesTheVersionTheInstantAndTheKind(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	if _, err := os.Stat(filepath.Join(dir, traceName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the trace is there before anything was recorded: %v", err)
	}
	before := time.Now().UTC().Add(-time.Second)
	if err := Append(dir, Record{"kind": "shell", "class": "git", "program": "git", "duration_ms": 12, "background": false}); err != nil {
		t.Fatalf("the first record was refused: %v", err)
	}
	if err := Append(dir, Record{"kind": "compaction", "trigger": "auto"}); err != nil {
		t.Fatalf("the second record was refused: %v", err)
	}
	got := lines(t, dir)
	if len(got) != 2 {
		t.Fatalf("two records made %d lines:\n%s", len(got), strings.Join(got, "\n"))
	}
	for i, line := range got {
		if len(line)+1 > 4096 {
			t.Errorf("line %d is %d bytes, over 4,096", i+1, len(line)+1)
		}
		m := decoded(t, line)
		if v, ok := m["v"].(float64); !ok || v != 1 {
			t.Errorf("line %d has v = %v, want 1", i+1, m["v"])
		}
		stamp, _ := m["at"].(string)
		if !at.MatchString(stamp) {
			t.Errorf("line %d has at = %q, want RFC 3339 in UTC to the millisecond", i+1, stamp)
			continue
		}
		when, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil || when.Before(before) || when.After(time.Now().UTC().Add(time.Second)) {
			t.Errorf("line %d has at = %q, which is not when it was written", i+1, stamp)
		}
	}
	if m := decoded(t, got[0]); m["kind"] != "shell" || m["class"] != "git" || m["program"] != "git" {
		t.Errorf("the first line does not hold what was recorded: %s", got[0])
	}
	if m := decoded(t, got[1]); m["kind"] != "compaction" || m["trigger"] != "auto" {
		t.Errorf("the second line was not appended after the first: %s", got[1])
	}
}

// AC-23: a line ends in \n on every platform, Windows included.
func TestAC1AC23EveryLineEndsInANewlineAndNeverACarriageReturn(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	for range 3 {
		if err := Append(dir, Record{"kind": "notification", "notification_type": "idle_prompt"}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, traceName))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("\r")) {
		t.Errorf("the trace holds a carriage return on %s: %q", runtime.GOOS, raw)
	}
	if n := bytes.Count(raw, []byte("\n")); n != 3 {
		t.Errorf("three records ended %d lines", n)
	}
}

// AC-1 and AC-23: the append is one write a line, so writers that do not wait
// for each other never tear one another's lines. Five reviewers ending together
// are five hook processes appending to one file with no lock.
func TestAC1AC23RecordsAppendedAtOnceAreEachAWholeLine(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	const writers, each = 8, 40
	// Long enough that a write split in two would show, short of the bound.
	pad := strings.Repeat("x", 3000)
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := range writers {
		wg.Go(func() {
			for i := range each {
				errs <- Append(dir, Record{"kind": "refusal", "rule": fmt.Sprintf("r-%d-%d-%s", w, i, pad), "tool": "Bash"})
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a record appended alongside others was refused: %v", err)
		}
	}
	got := lines(t, dir)
	if len(got) != writers*each {
		t.Fatalf("%d records made %d lines", writers*each, len(got))
	}
	seen := map[string]bool{}
	for _, line := range got {
		rule, _ := decoded(t, line)["rule"].(string)
		seen[rule] = true
	}
	if len(seen) != writers*each {
		t.Errorf("only %d of %d records came through whole", len(seen), writers*each)
	}
}

// AC-1: no line longer than 4,096 bytes. A record that would make one is not
// written at all, rather than written torn.
func TestAC1ARecordThatWouldMakeALineLongerThan4096BytesIsNotWritten(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	if err := Append(dir, Record{"kind": "refusal", "rule": strings.Repeat("r", 5000), "tool": "Bash"}); err == nil {
		t.Error("a record over 4,096 bytes was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, traceName)); !errors.Is(err, fs.ErrNotExist) {
		raw, _ := os.ReadFile(filepath.Join(dir, traceName))
		t.Errorf("a record over the bound left something on disk: %q", raw)
	}
}

// AC-12: every key a record may hold is named in one list, and the writer
// refuses a record with a key outside it. Each of these names a kind of text
// the story promises never to keep.
func TestAC12TheWriterRefusesARecordWithAKeyOutsideItsList(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		"command", "path", "file_path", "cwd", "dir", "transcript_path", "agent_transcript_path",
		"message", "last_assistant_message", "error_details", "description", "title", "prompt",
		"response", "reason", "summary", "compact_summary", "tool_input", "tool_response", "env",
		"session_id", "args", "stdout", "stderr",
	} {
		t.Run("AC-12 "+key, func(t *testing.T) {
			t.Parallel()
			dir := storyDir(t)
			if err := Append(dir, Record{"kind": "shell", "class": "other", key: "MARKER"}); err == nil {
				t.Errorf("a record holding %q was written", key)
			}
			if raw, err := os.ReadFile(filepath.Join(dir, traceName)); err == nil {
				t.Errorf("a refused record left a trace: %q", raw)
			}
		})
	}
}

// AC-3 to AC-11 and AC-16: every field the story names for every kind is on
// the list, so a record the hook builds is never refused for a key it was
// told to keep. The names here are the trace's format, version 1.
func TestAC12EveryFieldTheStoryNamesIsOnTheWritersList(t *testing.T) {
	t.Parallel()
	tokens := map[string]any{"input": 10, "cache_read": 20, "cache_written": 30, "output": 40}
	for _, r := range []Record{
		{
			"kind": "agent_run", "agent_type": "sdlc:verifier", "agent_id": "a1",
			"start": "2026-10-28T10:00:00.000Z", "end": "2026-10-28T10:05:00.000Z",
			"effort": "high", "asked_model": "opus", "asked_effort": "high", "permission_mode": "acceptEdits",
			"models": map[string]any{"claude-opus-x": tokens}, "responses": 3, "peak_context": 60,
			"tools": map[string]any{"Read": 2, "Bash": 1}, "status": "done",
		},
		{
			"kind": "turn", "prompt_id": "p-1", "effort": "medium",
			"models": map[string]any{"claude-sonnet-x": tokens}, "responses": 2, "peak_context": 60,
			"tools": map[string]any{"Agent": 1}, "background_tasks": map[string]any{"subagent": 1},
		},
		{"kind": "refusal", "rule": "gate-record-is-written-by-the-tool", "agent_type": "sdlc:implementer", "tool": "Write", "code": "SDLC-E0081"},
		{"kind": "shell", "agent_type": "sdlc:sdet", "duration_ms": 1234, "background": true, "class": "test", "program": "go", "outcome": "fail", "exit_code": 1},
		{"kind": "diff", "files": 3, "added": 40, "removed": 2, "test_files": 1, "test_added": 20, "test_removed": 0},
		{"kind": "notification", "agent_type": "sdlc:sdet", "notification_type": "permission_prompt"},
		{"kind": "api_error", "agent_type": "sdlc:sdet", "error": "rate_limit"},
		{"kind": "compaction", "agent_type": "sdlc:sdet", "trigger": "auto"},
	} {
		dir := storyDir(t)
		if err := Append(dir, r); err != nil {
			t.Errorf("AC-12 a %s record holding only the story's fields was refused: %v", r["kind"], err)
			continue
		}
		m := decoded(t, lines(t, dir)[0])
		for k := range r {
			if _, ok := m[k]; !ok {
				t.Errorf("AC-12 a %s record lost its %q on the way to disk", r["kind"], k)
			}
		}
	}
}

// AC-16: past 4 MiB the next record is one `truncated` record holding the time
// and the size, and nothing is appended after it.
func TestAC16ATraceAbout4MiBTakesOneTruncatedRecordAndNothingAfterIt(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	path := filepath.Join(dir, traceName)
	line := `{"v":1,"at":"2026-10-28T10:00:00.000Z","kind":"compaction","trigger":"auto"}` + "\n"
	// Ten bytes short of 4 MiB, so that any record at all would cross it.
	limit := 4<<20 - 10
	full := strings.Repeat(line, limit/len(line))
	if rest := limit - len(full); rest > 0 {
		full += strings.Repeat("x", rest-1) + "\n"
	}
	if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}
	size := len(full)

	if err := Append(dir, Record{"kind": "shell", "class": "git", "program": "git"}); err != nil {
		// Recording never refuses anything; whether this returns an error is
		// the writer's business, so long as the file is right.
		t.Logf("the record past the limit returned %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte(full)) {
		t.Fatal("the trace was rewritten, not appended to")
	}
	tail := strings.Split(strings.TrimSuffix(string(raw[size:]), "\n"), "\n")
	if len(tail) != 1 || tail[0] == "" {
		t.Fatalf("past 4 MiB, want exactly one truncated record, got %q", raw[size:])
	}
	m := decoded(t, tail[0])
	if m["kind"] != "truncated" {
		t.Errorf("the record past 4 MiB is %q, want truncated: %s", m["kind"], tail[0])
	}
	if got, ok := m["size"].(float64); !ok || int(got) < size {
		t.Errorf("the truncated record gives size %v, want the trace's size, at least %d", m["size"], size)
	}
	if !at.MatchString(fmt.Sprint(m["at"])) {
		t.Errorf("the truncated record has no time: %s", tail[0])
	}
	if strings.Contains(tail[0], `"shell"`) {
		t.Errorf("the record that crossed the limit was written: %s", tail[0])
	}

	after := len(raw)
	for range 3 {
		_ = Append(dir, Record{"kind": "compaction", "trigger": "manual"})
	}
	if again, err := os.ReadFile(path); err != nil || len(again) != after {
		t.Errorf("records were appended after the truncated record: %d bytes, then %d", after, len(again))
	}
}

// T2: a link planted at the trace's name is not written through. The hook
// appends without asking, and a link to a frozen test or the person's shell
// profile would take JSON lines there.
func TestAC1ALinkAtTheTracesNameLeavesWhatItPointsAtUnchanged(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("left alone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, traceName)); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	if err := Append(dir, Record{"kind": "compaction", "trigger": "auto"}); err == nil {
		t.Error("a record was written through a link at the trace's name")
	}
	if raw, _ := os.ReadFile(victim); string(raw) != "left alone\n" {
		t.Errorf("the file the link points at was changed: %q", raw)
	}
}

// T2: a link to a directory, or anything but a regular file, at the trace's
// name is not appended through either.
func TestAC1ADirectoryAtTheTracesNameIsNotWrittenAndNothingPanics(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	if err := os.Mkdir(filepath.Join(dir, traceName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Append(dir, Record{"kind": "compaction", "trigger": "auto"}); err == nil {
		t.Error("a record was accepted with a directory at the trace's name")
	}
}

// AC-1: a reader skips a line it cannot parse or whose kind it does not know,
// counts them, and is not stopped by them.
func TestAC1TheReaderSkipsAndCountsLinesItCannotReadAndKindsItDoesNotKnow(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	body := strings.Join([]string{
		`{"v":1,"at":"2026-10-28T10:00:00.000Z","kind":"shell","class":"git","program":"git"}`,
		`{"v":1,"at":"2026-10-28T10:00:01.000Z","kind":"shel`, // torn
		`not json at all`,
		`{"v":1,"at":"2026-10-28T10:00:02.000Z","kind":"a_kind_from_a_newer_sdlc","x":1}`,
		`["an","array"]`,
		`{"v":1,"at":"2026-10-28T10:00:03.000Z","kind":"refusal","rule":"r","tool":"Bash"}`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, traceName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Read(dir)
	if err != nil {
		t.Fatalf("a trace with bad lines in it stopped the reading: %v", err)
	}
	if len(r.Records) != 2 || r.Records[0]["kind"] != "shell" || r.Records[1]["kind"] != "refusal" {
		t.Errorf("records read = %v, want the shell and the refusal, in order", r.Records)
	}
	if r.Skipped != 4 {
		t.Errorf("skipped = %d, want the torn line, the text, the unknown kind and the array: 4", r.Skipped)
	}
	if r.Truncated {
		t.Error("a trace with no truncated record was read as truncated")
	}
}

// AC-21: a story started before tracing has no trace, and that is told apart
// from an empty one.
func TestAC21ReadingAStoryWithNoTraceSaysTheFileIsNotThere(t *testing.T) {
	t.Parallel()
	if _, err := Read(storyDir(t)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("reading a story with no trace gave %v, want an error that is fs.ErrNotExist", err)
	}
}

// AC-16: a truncated trace is read as one, and nothing past 4 MiB is read by
// any command.
func TestAC16TheReaderSaysATraceWasTruncatedAndReadsNothingPast4MiB(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	line := `{"v":1,"at":"2026-10-28T10:00:00.000Z","kind":"compaction","trigger":"auto"}` + "\n"
	var b strings.Builder
	for b.Len() < 4<<20 {
		b.WriteString(line)
	}
	// A person may have appended by hand; the reader stops at 4 MiB anyway.
	b.WriteString(`{"v":1,"at":"2026-10-28T11:00:00.000Z","kind":"notification","notification_type":"idle_prompt"}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, traceName), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range r.Records {
		if rec["kind"] == "notification" {
			t.Fatal("a record past 4 MiB was read")
		}
	}
	if len(r.Records) == 0 {
		t.Error("nothing within the first 4 MiB was read")
	}

	small := storyDir(t)
	if err := os.WriteFile(filepath.Join(small, traceName), []byte(
		`{"v":1,"at":"2026-10-28T10:00:00.000Z","kind":"compaction","trigger":"auto"}`+"\n"+
			`{"v":1,"at":"2026-10-28T10:00:01.000Z","kind":"truncated","size":4194300}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := Read(small); err != nil || !r.Truncated {
		t.Errorf("a trace ending in a truncated record read as truncated = %v (%v)", r.Truncated, err)
	}
}

// AC-1 and AC-16: what the writer writes, the reader reads back whole.
func TestAC1WhatIsAppendedIsReadBack(t *testing.T) {
	t.Parallel()
	dir := storyDir(t)
	for _, r := range []Record{
		{"kind": "shell", "class": "test", "program": "go", "outcome": "pass", "duration_ms": 10},
		{"kind": "api_error", "error": "overloaded"},
	} {
		if err := Append(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Records) != 2 || r.Skipped != 0 {
		t.Fatalf("read %d records and skipped %d, want 2 and 0", len(r.Records), r.Skipped)
	}
	if r.Records[0]["outcome"] != "pass" || r.Records[1]["error"] != "overloaded" {
		t.Errorf("records read back = %v", r.Records)
	}
}

// AC-14: the code that writes the trace does no work in a package init(),
// which is on the latency path of every tool call.
func TestAC14TheTracePackageHasNoInit(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	source := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		source++
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if regexp.MustCompile(`(?m)^func init\(\)`).Match(raw) {
			t.Errorf("%s declares an init()", f)
		}
	}
	if source == 0 {
		t.Error("the trace package has no source yet")
	}
}
