package claudecode

// US-112 AC-3, AC-4, AC-6 and AC-14: Claude Code's transcript, read into the
// counts the trace keeps and nothing else. The shape is Claude Code's and is
// not a documented interface, so it is read here, at the host's edge, and the
// hook is handed counts.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// usage is one response's usage block, as Claude Code writes it.
func usage(input, cacheRead, cacheWritten, output int64) map[string]any {
	return map[string]any{
		"input_tokens": input, "cache_read_input_tokens": cacheRead,
		"cache_creation_input_tokens": cacheWritten, "output_tokens": output,
	}
}

// assistant is one transcript line of a response: Claude Code writes a line a
// content block, each repeating the response's id, model and usage.
func assistant(id, model string, u map[string]any, content ...map[string]any) string {
	if content == nil {
		content = []map[string]any{{"type": "text", "text": "MARKER-response-text"}}
	}
	raw, err := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": "2026-10-28T10:00:00.000Z",
		"message": map[string]any{"id": id, "model": model, "role": "assistant", "usage": u, "content": content},
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func toolUse(id, name string) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{"command": "MARKER-command"}}
}

// user is a line of the person's or a tool's, which carries no usage.
func user(text string) string {
	raw, err := json.Marshal(map[string]any{
		"type": "user", "timestamp": "2026-10-28T10:00:00.000Z",
		"message": map[string]any{"role": "user", "content": text},
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func transcript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-a1.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	opus   = "claude-opus-x-20261001"
	sonnet = "some-other-provider-model-7b"
)

// AC-3: tokens by model and by kind, each response counted once however many
// lines repeat it, the number of responses, the peak context and the tool
// calls by name.
func TestAC3AResponseSpreadOverSeveralLinesIsCountedOnceByModel(t *testing.T) {
	t.Parallel()
	path := transcript(t,
		user("MARKER-prompt"),
		assistant("msg_1", opus, usage(10, 100, 5, 7), map[string]any{"type": "text", "text": "thinking"}),
		assistant("msg_1", opus, usage(10, 100, 5, 7), toolUse("tu_1", "Read")),
		assistant("msg_1", opus, usage(10, 100, 5, 7), toolUse("tu_2", "Read")),
		user("MARKER-tool-result"),
		assistant("msg_2", opus, usage(3, 300, 40, 11), toolUse("tu_3", "Bash")),
		assistant("msg_3", sonnet, usage(1, 2, 3, 4)),
	)
	u, next, err := ReadTranscript(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("a well-formed transcript was not read: %v", err)
	}
	if u.Responses != 3 {
		t.Errorf("responses = %d, want 3: msg_1 spans three lines and is one response", u.Responses)
	}
	if got := u.Models[opus]; got.Input != 13 || got.CacheRead != 400 || got.CacheWritten != 45 || got.Output != 18 {
		t.Errorf("%s tokens = %+v, want input 13, cache read 400, cache written 45, output 18", opus, got)
	}
	if got := u.Models[sonnet]; got.Input != 1 || got.CacheRead != 2 || got.CacheWritten != 3 || got.Output != 4 {
		t.Errorf("the second model's tokens = %+v, kept under its own name", got)
	}
	if len(u.Models) != 2 {
		t.Errorf("models = %v, want exactly the two the transcript names", u.Models)
	}
	// msg_2: 3 + 300 + 40.
	if u.PeakContext != 343 {
		t.Errorf("peak context = %d, want 343, the largest input plus cache in one response", u.PeakContext)
	}
	if u.Tools["Read"] != 2 || u.Tools["Bash"] != 1 || len(u.Tools) != 2 {
		t.Errorf("tool calls = %v, want Read 2 and Bash 1", u.Tools)
	}
	if info, err := os.Stat(path); err != nil || next != info.Size() {
		t.Errorf("read to %d, want the end of the file", next)
	}
}

// AC-6: the main conversation's turn counts the responses since the previous
// turn, from where that one stopped.
func TestAC6ReadingFromWhereTheLastReadStoppedCountsOnlyTheNewResponses(t *testing.T) {
	t.Parallel()
	path := transcript(t,
		assistant("msg_1", opus, usage(10, 0, 0, 1), toolUse("tu_1", "Agent")),
	)
	first, next, err := ReadTranscript(context.Background(), path, 0)
	if err != nil || first.Responses != 1 {
		t.Fatalf("first read: %+v, %v", first, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(user("again") + "\n" + assistant("msg_2", opus, usage(20, 0, 0, 2), toolUse("tu_2", "Bash")) + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	second, after, err := ReadTranscript(context.Background(), path, next)
	if err != nil {
		t.Fatal(err)
	}
	if second.Responses != 1 || second.Models[opus].Input != 20 || second.Tools["Agent"] != 0 || second.Tools["Bash"] != 1 {
		t.Errorf("the second read counted what the first had: %+v", second)
	}
	if after <= next {
		t.Errorf("the second read stopped at %d, not past %d", after, next)
	}
	// THREATS D2: a transcript rewritten shorter than where the last read
	// stopped. Whether that reads from the start again or gives up is the
	// reader's to choose; it ends, and it does not count past the end.
	if u, _, err := ReadTranscript(context.Background(), path, after+1<<20); err == nil && u.Responses > 2 {
		t.Errorf("an offset past the end counted %d responses from a file holding two", u.Responses)
	}
}

// AC-4: missing, unreadable, not in the shape the hook reads, or larger than
// 64 MB: there are no counts, never zero ones.
func TestAC4ATranscriptThatCannotBeReadGivesAnErrorAndNoCounts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	big := filepath.Join(dir, "big.jsonl")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	// Sparse where the filesystem allows it, so the test costs no 64 MB.
	if err := f.Truncate(64<<20 + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	notJSON := filepath.Join(dir, "notjson.jsonl")
	if err := os.WriteFile(notJSON, []byte("this is not a transcript\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	negative := transcript(t, assistant("msg_1", opus, usage(-5, 0, 0, 1)))
	notNumbers := filepath.Join(dir, "strings.jsonl")
	if err := os.WriteFile(notNumbers, []byte(`{"type":"assistant","message":{"id":"m","model":"x","usage":{"input_tokens":"ten"}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"missing": filepath.Join(dir, "nowhere.jsonl"),
		"a directory, which cannot be read as one": dir,
		"larger than 64 MB":                        big,
		"not JSON lines":                           notJSON,
		"a count below zero":                       negative,
		"a count that is not a number":             notNumbers,
	} {
		t.Run("AC-4 "+name, func(t *testing.T) {
			t.Parallel()
			u, _, err := ReadTranscript(context.Background(), path, 0)
			if err == nil {
				t.Errorf("a transcript %s was read as %+v", name, u)
			}
			if u.Responses != 0 || len(u.Models) != 0 || len(u.Tools) != 0 || u.PeakContext != 0 {
				t.Errorf("a transcript %s still gave counts: %+v", name, u)
			}
		})
	}
}

// AC-14: the read stops when it is told to, which is how the hook bounds it to
// five seconds.
func TestAC14AReadWhoseTimeIsUpGivesNoCounts(t *testing.T) {
	t.Parallel()
	lines := make([]string, 0, 2000)
	for i := range 2000 {
		lines = append(lines, assistant("msg_"+strconv.Itoa(i), opus, usage(1, 1, 1, 1)))
	}
	path := transcript(t, lines...)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if u, _, err := ReadTranscript(ctx, path, 0); err == nil {
		t.Errorf("a read whose time was up gave %+v", u)
	}
}

// AC-12: the counts hold model names and tool names, and no other text of the
// transcript: a prompt, a response, a tool's input or its output.
func TestAC12NothingButNamesAndCountsComesOutOfATranscript(t *testing.T) {
	t.Parallel()
	path := transcript(t,
		user("MARKER-prompt"),
		assistant("msg_1", opus, usage(1, 2, 3, 4), toolUse("tu_1", "Bash")),
		user("MARKER-tool-output"),
	)
	u, _, err := ReadTranscript(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(fmt.Sprintf("%+v", u))
	if strings.Contains(string(raw), "MARKER") || strings.Contains(string(raw), path) {
		t.Errorf("text of the transcript, or its path, came out of the reading: %s", raw)
	}
	if u.Responses != 1 {
		t.Errorf("the reading counted %d responses, want 1", u.Responses)
	}
}

// AC-14: the transcript reader does no work in a package init().
func TestAC14TheTranscriptReaderHasNoInit(t *testing.T) {
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
		t.Error("the package has no source yet")
	}
}
