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
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bbsnly/sdlc/internal/fsx"
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
	// keyKind and keyAt are what every record is, and when a truncated one
	// was written.
	keyKind = "kind"
	keyAt   = "at"
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

// kinds is every kind the writer writes and the reader reads. An array of
// constants and not a map: it is laid out by the linker, so the package does
// no work before it is called (AC-14).
var kinds = [...]string{KindAgentRun, KindTurn, "refusal", "shell", "diff", "notification", "api_error",
	"compaction", "handback", KindTruncated}

func knownKind(kind string) bool {
	for _, k := range kinds {
		if kind == k {
			return true
		}
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
	kind, _ := r[keyKind].(string)
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
			f, err = os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_EXCL|fsx.OpenFlags, 0o644) //nolint:gosec // G302: the person reads it, as git and the editor read the gate record
			if errors.Is(err, fs.ErrExist) && !again {
				continue
			}
		case err == nil && !seen.Mode().IsRegular():
			return nil, nil, ErrNotOurs
		case err == nil:
			f, err = os.OpenFile(path, os.O_RDWR|os.O_APPEND|fsx.OpenFlags, 0)
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
	if err != nil || !fi.Mode().IsRegular() || seen != nil && !os.SameFile(seen, fi) || !fsx.OneName(f, fi) {
		_ = f.Close()
		return nil, nil, ErrNotOurs
	}
	return f, fi, nil
}

// Read reads the trace of the story whose directory is storyDir, never past
// 4 MiB and never past the first truncated record. A trace that is not there
// is an error that is fs.ErrNotExist; one that is there and is not a regular
// file is ErrNotOurs, and the reader never waits on it.
func Read(storyDir string) (Reading, error) { return read(storyDir, "") }

// ReadKind is Read with only the records of one kind kept. A line of another
// kind is told apart by its kind alone and never decoded whole: the hook asks
// for one agent's newest hand-back at every SubagentStop, and reading the
// whole trace into maps for it was 3 MB and 58,000 allocations at 2,000 lines
// (US-129, AC-8). Skipped, Truncated and TruncatedAt are what Read would say.
func ReadKind(storyDir, kind string) (Reading, error) { return read(storyDir, kind) }

func read(storyDir, only string) (Reading, error) {
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
		f, err = os.OpenFile(path, os.O_RDONLY|fsx.OpenFlags, 0)
	}
	if err != nil {
		return Reading{}, plain("the trace could not be read", err)
	}
	defer func() { _ = f.Close() }()
	if fi, err = f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return Reading{}, ErrNotOurs
	}
	r, err := scan(io.LimitReader(f, limit), only)
	if err != nil {
		return Reading{}, plain("the trace could not be read", err)
	}
	r.Truncated = r.Truncated || fi.Size() > limit
	return r, nil
}

// scan reads the records in what is left of the first 4 MiB. A line that is
// not an object, or whose kind is not known, is skipped and counted, and so is
// a line the limit cut. With only set, a record of another kind is passed over
// after its kind is read, and nothing else of it is decoded, where the line is
// one kindOf is sure of; any other line is decoded whole, as Read decodes it.
func scan(in io.Reader, only string) (Reading, error) {
	var r Reading
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, startBuffer), limit+1)
	for sc.Scan() {
		if only != "" {
			kind, at, object, sure := kindOf(sc.Bytes())
			switch {
			case !sure:
			case !object || !knownKind(kind):
				r.Skipped++
				continue
			case kind == KindTruncated:
				r.Truncated, r.TruncatedAt = true, at
				return r, nil
			case kind != only:
				continue
			}
		}
		var rec Record
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			r.Skipped++
			continue
		}
		kind, _ := rec[keyKind].(string)
		switch {
		case !knownKind(kind):
			r.Skipped++
		case kind == KindTruncated:
			// Two appenders at the limit can each see room; the lines after the
			// first truncated record are never read, whatever the race left.
			r.Truncated = true
			r.TruncatedAt, _ = rec[keyAt].(string)
			return r, nil
		case only == "" || kind == only:
			r.Records = append(r.Records, rec)
		}
	}
	return r, sc.Err()
}

// kindOf is a line's kind and, for a truncated record, when it was written,
// read as the map Read decodes the line into reads them, with nothing of the
// line decoded: object is false where the map would not take the line (not
// JSON, or not an object; null reads as an empty map), and a kind or an at
// that is not a string is "". The map matches a key by its exact bytes and
// keeps the last of a key given twice, which a struct's field does not: it
// matched KIND and Kind too, and `{"kind":"handback","KIND":"shell"}` read as
// a shell record (US-129 verification, F-3). sure is false where the fast
// reading cannot be certain of the map's: a key with an escape in it, a
// number with an exponent or over three hundred digits, which json.Unmarshal
// may refuse as out of range; the caller then decodes the line whole.
func kindOf(line []byte) (kind, at string, object, sure bool) {
	if !json.Valid(line) {
		return "", "", false, true
	}
	i := skipSpace(line, 0)
	if i >= len(line) || line[i] != '{' {
		return "", "", false, true
	}
	var kindRaw, atRaw []byte
	for i++; ; {
		i = skipSpace(line, i)
		if i >= len(line) {
			return "", "", true, false
		}
		if line[i] == '}' {
			break
		}
		key, value, next, ok := member(line, i)
		if !ok {
			return "", "", true, false
		}
		switch string(key) {
		case keyKind:
			kindRaw = value
		case keyAt:
			atRaw = value
		}
		i = next
	}
	kind = kindText(kindRaw)
	if kind == KindTruncated {
		// Only a truncated record's at is read; every record carries one, and
		// its string was the one allocation left per line passed over.
		at = text(atRaw)
	}
	return kind, at, true, true
}

// kindText is text for a kind, with a known kind returned as its own
// constant: passing a record over then allocates nothing, where the string
// made for every line was the most numerous allocation of a stop that reads
// the trace (US-129, round 6).
func kindText(raw []byte) string {
	if len(raw) >= 2 && raw[0] == '"' {
		inner := raw[1 : len(raw)-1]
		for _, k := range kinds {
			if string(inner) == k {
				return k
			}
		}
	}
	return text(raw)
}

// member is the key and the raw value of the object member at i, a comma
// before it skipped, and the index after the value; false where the key holds
// an escape or the value a number the map decode could refuse.
func member(line []byte, i int) (key, value []byte, next int, ok bool) {
	if line[i] == ',' {
		i = skipSpace(line, i+1)
	}
	end, escaped := stringEnd(line, i)
	if end < 0 || escaped {
		return nil, nil, 0, false
	}
	key = line[i+1 : end-1]
	i = skipSpace(line, end)
	if i >= len(line) || line[i] != ':' {
		return nil, nil, 0, false
	}
	i = skipSpace(line, i+1)
	after, ok := valueEnd(line, i)
	if !ok {
		return nil, nil, 0, false
	}
	return key, line[i:after], after, true
}

// skipSpace is the index of the first byte at or after i that is not JSON
// white space.
func skipSpace(line []byte, i int) int {
	for i < len(line) && (line[i] == ' ' || line[i] == '\t' || line[i] == '\n' || line[i] == '\r') {
		i++
	}
	return i
}

// stringEnd is the index after the closing quote of the string that opens at
// i, and whether it holds an escape; -1 where it does not close.
func stringEnd(line []byte, i int) (end int, escaped bool) {
	if i >= len(line) || line[i] != '"' {
		return -1, false
	}
	for j := i + 1; j < len(line); j++ {
		switch line[j] {
		case '\\':
			escaped = true
			j++
		case '"':
			return j + 1, escaped
		}
	}
	return -1, escaped
}

// valueEnd is the index after the value that starts at i in a line json.Valid
// has passed, and false where the value holds a number the map decode could
// refuse: one with an exponent, or longer than a float64 can hold.
func valueEnd(line []byte, i int) (int, bool) {
	if i >= len(line) {
		return i, false
	}
	if line[i] == '{' || line[i] == '[' {
		return nestedEnd(line, i)
	}
	return tokenEnd(line, i)
}

// nestedEnd is valueEnd for the object or array that opens at i.
func nestedEnd(line []byte, i int) (int, bool) {
	depth := 0
	for j := i; j < len(line); j++ {
		switch c := line[j]; {
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			if depth--; depth == 0 {
				return j + 1, true
			}
		case c == '"' || c == '-' || '0' <= c && c <= '9':
			end, ok := tokenEnd(line, j)
			if !ok {
				return j, false
			}
			j = end - 1
		}
	}
	return len(line), false
}

// tokenEnd is the index after the string, number or literal at i, and false
// where the string does not close or the number is one numberOK refuses.
func tokenEnd(line []byte, i int) (int, bool) {
	if line[i] == '"' {
		end, _ := stringEnd(line, i)
		return end, end > 0
	}
	k := i
	for k < len(line) && !endsValue(line[k]) {
		k++
	}
	return k, numberOK(line[i:k])
}

// endsValue reports whether c ends a literal or a number: a comma, a closing
// bracket or white space.
func endsValue(c byte) bool {
	return c == ',' || c == '}' || c == ']' || c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// numberOK is false where tok is a number json.Unmarshal could refuse as out
// of range: one with an exponent, or over three hundred digits. A literal is
// never refused.
func numberOK(tok []byte) bool {
	if len(tok) == 0 || tok[0] != '-' && (tok[0] < '0' || tok[0] > '9') {
		return true
	}
	return len(tok) <= 300 && !bytes.ContainsAny(tok, "eE")
}

// text is the string a JSON value holds, as the map decode reads it, and ""
// where the value is not a string. A string with no escape and only valid
// UTF-8 is its own bytes; any other goes through json.Unmarshal, which
// unescapes and replaces what is not UTF-8 as the map decode does.
func text(raw []byte) string {
	if len(raw) < 2 || raw[0] != '"' {
		return ""
	}
	if bytes.IndexByte(raw, '\\') < 0 && utf8.Valid(raw) {
		return string(raw[1 : len(raw)-1])
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
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
