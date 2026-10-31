// Package trace is a story's measurements: what the loop costs, one JSON
// object a line, in trace.jsonl beside the story's gate record (US-112).
//
// The hook and the commit gate append to it, and "sdlc trace" reads it back.
// Nothing here decides anything: a record that cannot be written is a line in
// the debug log, never a refusal and never a changed answer. The writer takes
// no lock, because each record is one write to a file opened for appending,
// and it never writes a key outside its list, so no free text a person or a
// model wrote can reach the file.
package trace

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
)

// Record is one line of the trace, before the writer stamps it.
type Record map[string]any

// The kinds and the key other packages name as well as this one.
const (
	KindAgentRun  = "agent_run"
	KindTurn      = "turn"
	KindTruncated = "truncated"
	// KeyAgentType is the agent a record is of, left out for the main
	// conversation.
	KeyAgentType = "agent_type"
)

// Reading is what Read found: the records it could read, in order, how many
// lines it could not, and whether the trace stopped at its limit, with when
// the truncated record said it did where there is one.
type Reading struct {
	Records     []Record
	Skipped     int
	Truncated   bool
	TruncatedAt string
}

// Tokens are one model's counts in a transcript.
type Tokens struct {
	Input        int64 `json:"input"`
	CacheRead    int64 `json:"cache_read"`
	CacheWritten int64 `json:"cache_written"`
	Output       int64 `json:"output"`
}

// Usage is what a host's transcript says a span of work cost: tokens by
// model, responses, the largest context, and tool calls by tool.
type Usage struct {
	Models      map[string]Tokens
	Responses   int
	PeakContext int64
	Tools       map[string]int
	// LongLines is how many lines the reader passed over unread for being
	// longer than it reads. The hook puts it in its debug line and never in
	// the trace.
	LongLines int
}

const (
	// version is the format every line carries as "v".
	version = 1
	// maxLine is the longest line, its newline included.
	maxLine = 4096
	// limit is the size the trace stops at. A story that ran long enough to
	// fill it has its last record say so, and nothing is written after that.
	limit = 4 << 20
	// startBuffer is what the reader allocates per trace until a line needs
	// more, so that "sdlc trace --all" does not take 4 MiB for every story.
	startBuffer = 64 << 10
)

// The ways a record is not written or a trace not read. None names a path:
// the hook's and the commit gate's debug lines carry these texts as they are.
var (
	// ErrFull is a record refused because the trace already ends in its
	// truncated record. It is said once, by ErrTruncated, and not again.
	ErrFull = errors.New("the trace has reached 4 MiB and takes no more records")
	// ErrTruncated is a record that would have taken the trace past 4 MiB:
	// the truncated record was written in its place.
	ErrTruncated = errors.New("the trace reached 4 MiB; nothing more is recorded for this story")
	// ErrNotOurs is a name that is not one regular file: a link, a directory,
	// a named pipe, a file with a second name, or one that changed under the
	// open.
	ErrNotOurs = errors.New("the trace's name is not one regular file sdlc made")
	// ErrOutside is a story directory that resolves out of the checkout.
	ErrOutside = errors.New("the story's directory resolves outside the checkout")
	// ErrKey is a record holding a key its kind does not take, or a kind
	// the writer does not know.
	ErrKey = errors.New("the record holds a key its kind does not take")
	// ErrTooLong is a record whose line would be longer than 4,096 bytes.
	ErrTooLong = errors.New("the record would make a line longer than 4,096 bytes")

	// ErrTranscriptUnreadable is a transcript that is not a regular file, or
	// whose read failed.
	ErrTranscriptUnreadable = errors.New("the transcript could not be read")
	// ErrTranscriptShape is a transcript line that is not in the shape the
	// hook reads.
	ErrTranscriptShape = errors.New("a transcript line is not in the shape the hook reads")
	// ErrTranscriptTooLarge is more than 64 MB of transcript past where the
	// read starts.
	ErrTranscriptTooLarge = errors.New("the transcript is larger than 64 MB past where the read starts")
)

// afterLstat, where it is set, is called with the trace's path after each
// Lstat of it and before it is opened. Only the tests set it, to put a name
// in the window between the two.
var afterLstat func(path string)

// knownKind is every kind the writer writes and the reader reads. A switch
// and not a map: the package does no work before it is called (AC-14).
func knownKind(kind string) bool {
	switch kind {
	case KindAgentRun, KindTurn, "refusal", "shell", "diff", "notification", "api_error",
		"compaction", "handback", KindTruncated:
		return true
	}
	return false
}

// knownKey is each kind's own list of keys. It allows; nothing is forbidden
// by name, so a key nobody thought of is refused as surely as "command", and
// a field of one kind is refused on every other (red team, V8).
func knownKey(kind, key string) bool {
	switch key {
	case "kind", "v", "at":
		return true
	}
	switch kind {
	case KindAgentRun:
		return runKey(key)
	case KindTurn:
		return turnKey(key)
	case "refusal":
		return key == "rule" || key == KeyAgentType || key == "tool" || key == "code"
	case "shell":
		return shellKey(key)
	case "diff":
		return diffKey(key)
	case "notification":
		return key == KeyAgentType || key == "notification_type"
	case "api_error":
		return key == KeyAgentType || key == "error"
	case "compaction":
		return key == KeyAgentType || key == "trigger"
	case "handback":
		return key == KeyAgentType || key == "agent_id" || key == "status"
	case KindTruncated:
		return key == "size"
	}
	return false
}

func runKey(key string) bool {
	switch key {
	case KeyAgentType, "agent_id", "start", "end", "effort", "asked_model", "asked_effort",
		"permission_mode", "models", "responses", "peak_context", "tools", "status":
		return true
	}
	return false
}

func turnKey(key string) bool {
	switch key {
	case "prompt_id", "effort", "models", "responses", "peak_context", "tools", "background_tasks":
		return true
	}
	return false
}

func shellKey(key string) bool {
	switch key {
	case KeyAgentType, "duration_ms", "background", "class", "program", "outcome", "exit_code":
		return true
	}
	return false
}

func diffKey(key string) bool {
	switch key {
	case "files", "added", "removed", "test_files", "test_added", "test_removed":
		return true
	}
	return false
}

// Stamp is the trace's form of an instant: RFC 3339, UTC, to the millisecond.
func Stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// line is r as one line of the trace, stamped, or why it is not one.
func line(r Record, now time.Time) ([]byte, error) {
	kind, _ := r["kind"].(string)
	if !knownKind(kind) {
		return nil, ErrKey
	}
	for key := range r {
		if !knownKey(kind, key) {
			return nil, ErrKey
		}
	}
	out := make(Record, len(r)+2)
	maps.Copy(out, r)
	out["v"], out["at"] = version, Stamp(now)
	// Marshal sorts the keys and escapes control characters, so a line holds
	// no carriage return on any platform (AC-23).
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(raw) > maxLine {
		return nil, ErrTooLong
	}
	return raw, nil
}

// Append adds r to the trace of the story whose directory is storyDir,
// creating the trace with its first record. It writes one whole line or
// nothing.
func Append(storyDir string, r Record) error {
	raw, err := line(r, time.Now())
	if err != nil {
		return err
	}
	if err := inCheckout(storyDir); err != nil {
		return err
	}
	f, fi, err := openToAppend(filepath.Join(storyDir, model.TraceFile))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// Past this size any line that passed the length check could cross 4 MiB,
	// and a small record after a large one could still fit, so the test is
	// "near the limit", not "past it": the one truncated record is written in
	// r's place, where the trace does not end in one already, and nothing is
	// ever written after it.
	var full error
	if size := fi.Size(); size >= limit-maxLine {
		if endsTruncated(f, size) {
			return ErrFull
		}
		// Two known keys and a number: it is always one line.
		raw, _ = line(Record{"kind": KindTruncated, "size": size}, time.Now())
		full = ErrTruncated
	}
	if _, err := f.Write(raw); err != nil {
		return plain("the trace could not be written", err)
	}
	return full
}

// endsTruncated reports whether the trace's last line is a truncated record.
// Only that line is read, at most one line's length from the end. A read that
// fails leaves zeros where the line was, which parse as no record.
func endsTruncated(f *os.File, size int64) bool {
	from := max(size-maxLine-1, 0)
	tail := make([]byte, size-from)
	_, _ = f.ReadAt(tail, from)
	last := strings.TrimSuffix(string(tail), "\n")
	if i := strings.LastIndexByte(last, '\n'); i >= 0 {
		last = last[i+1:]
	}
	var r struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal([]byte(last), &r) == nil && r.Kind == KindTruncated
}

// inCheckout refuses a story directory that, with every link on the way to it
// followed, is outside the checkout three levels above it: StoryDir puts a
// story at <checkout>/.sdlc/stories/<ID>, and the trace writer is the one
// writer outside the store, which refuses the same through the same resolver
// (store.leaves, THREATS T3).
func inCheckout(storyDir string) error {
	root := filepath.Dir(filepath.Dir(filepath.Dir(storyDir)))
	if _, outside := pathrules.Rel(root, storyDir); outside {
		return ErrOutside
	}
	return nil
}

// openToAppend opens the trace for appending, and only where its name is one
// regular file. Where the Lstat found nothing, the open creates exclusively,
// which fails on any name already there, a link or a named pipe included, on
// every platform; a name that appeared in between is Lstat once more, since a
// regular file there is another appender's first record. Where the Lstat
// found a regular file, the open does not create, and the descriptor must be
// the file the Lstat saw, with no other name. The file is returned with what
// its descriptor says of it.
func openToAppend(path string) (*os.File, fs.FileInfo, error) {
	for again := false; ; again = true {
		seen, err := os.Lstat(path)
		if afterLstat != nil {
			afterLstat(path)
		}
		var f *os.File
		switch {
		case errors.Is(err, fs.ErrNotExist):
			seen = nil
			f, err = os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_EXCL|openFlags, 0o644) //nolint:gosec // G302: the person reads it, as git and the editor read the gate record
			if errors.Is(err, fs.ErrExist) && !again {
				continue
			}
		case err == nil && !seen.Mode().IsRegular():
			return nil, nil, ErrNotOurs
		case err == nil:
			f, err = os.OpenFile(path, os.O_RDWR|os.O_APPEND|openFlags, 0)
		}
		if err != nil {
			return nil, nil, plain("the trace could not be opened", err)
		}
		return checked(f, seen)
	}
}

// checked keeps f only where it is a regular file with one name and, where
// seen is set, the file the Lstat saw. A hard link at the name is a regular
// file to Lstat, and an append would go to whatever file it also names
// (security, B2). A descriptor that will not say what it is is not kept
// either.
func checked(f *os.File, seen fs.FileInfo) (*os.File, fs.FileInfo, error) {
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || seen != nil && !os.SameFile(seen, fi) || !oneName(f, fi) {
		_ = f.Close()
		return nil, nil, ErrNotOurs
	}
	return f, fi, nil
}

// Read reads the trace of the story whose directory is storyDir, never past
// 4 MiB and never past the first truncated record. A trace that is not there
// is an error that is fs.ErrNotExist; one that is there and is not a regular
// file is ErrNotOurs, and the reader never waits on it.
func Read(storyDir string) (Reading, error) {
	if err := inCheckout(storyDir); err != nil {
		return Reading{}, err
	}
	path := filepath.Join(storyDir, model.TraceFile)
	fi, err := os.Lstat(path)
	if err == nil && !fi.Mode().IsRegular() {
		return Reading{}, ErrNotOurs
	}
	var f *os.File
	if err == nil {
		f, err = os.OpenFile(path, os.O_RDONLY|openFlags, 0)
	}
	if err != nil {
		return Reading{}, plain("the trace could not be read", err)
	}
	defer func() { _ = f.Close() }()
	if fi, err = f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return Reading{}, ErrNotOurs
	}
	r, err := scan(io.LimitReader(f, limit))
	if err != nil {
		return Reading{}, plain("the trace could not be read", err)
	}
	r.Truncated = r.Truncated || fi.Size() > limit
	return r, nil
}

// scan reads the records in what is left of the first 4 MiB. A line that is
// not an object, or whose kind is not known, is skipped and counted, and so is
// a line the limit cut.
func scan(in io.Reader) (Reading, error) {
	var r Reading
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, startBuffer), limit+1)
	for sc.Scan() {
		var rec Record
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			r.Skipped++
			continue
		}
		kind, _ := rec["kind"].(string)
		switch {
		case !knownKind(kind):
			r.Skipped++
		case kind == KindTruncated:
			// Two appenders at the limit can each see room; the lines after the
			// first truncated record are never read, whatever the race left.
			r.Truncated = true
			r.TruncatedAt, _ = rec["at"].(string)
			return r, nil
		default:
			r.Records = append(r.Records, rec)
		}
	}
	return r, sc.Err()
}

// plainError is an error from the file system with its path taken out: the
// debug lines that carry it name no path (THREATS I4).
type plainError struct {
	what string
	err  error
}

func (e *plainError) Error() string { return e.what + ": " + e.err.Error() }
func (e *plainError) Unwrap() error { return e.err }

// plain is err under a fixed phrase, with an *fs.PathError taken down to its
// inner error, so that errors.Is still sees fs.ErrNotExist.
func plain(what string, err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		err = pe.Err
	}
	return &plainError{what: what, err: err}
}
