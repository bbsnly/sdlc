// Package claudecode is Claude Code's profile at the host's edge: the one
// place that knows the shape of its transcripts. The hook is handed counts
// and never reads a transcript itself (US-112). Only cmd/sdlc imports this
// package, outside its tests.
package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/bbsnly/sdlc/internal/trace"
)

const (
	// maxSpan is the most transcript read past where a read starts. A span
	// over it is not read at all: its counts would take longer than the hook
	// is given, and the reader says how far it went so the next read starts
	// past it.
	maxSpan = 64 << 20
	// maxLine is the longest line decoded. The longest lines in a transcript
	// are tool results, which carry no usage; one over this is passed over a
	// buffer at a time, so a line of tens of megabytes is never held whole.
	maxLine = 4 << 20
)

// transcriptError is a read that gave no counts. Its text is a fixed phrase:
// the path, and anything the file held, stay out of the hook's debug log.
type transcriptError struct {
	class error
}

func (e *transcriptError) Error() string { return "the transcript was not read: " + e.class.Error() }
func (e *transcriptError) Unwrap() error { return e.class }

// entry is the part of a transcript line the reader decodes: its type, and
// for a response, its id, model, usage and content blocks. The usage block is
// read leniently: Claude Code writes strings and objects beside the four
// counts, and only the counts must be whole numbers.
type entry struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

type message struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage struct {
		Input        int64 `json:"input_tokens"`
		CacheRead    int64 `json:"cache_read_input_tokens"`
		CacheWritten int64 `json:"cache_creation_input_tokens"`
		Output       int64 `json:"output_tokens"`
	} `json:"usage"`
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// response is one response's model and tokens, from the last line that
// carries its id: Claude Code writes a line a content block, each repeating
// the response's usage.
type response struct {
	model  string
	tokens trace.Tokens
}

// tally is what a read has counted so far.
type tally struct {
	order     []string
	responses map[string]response
	calls     map[string]bool
	tools     map[string]int
	long      int
}

// ReadTranscript reads Claude Code's transcript at path from the byte offset
// from, and returns the counts of what it holds and the offset after the last
// whole line read. A read that gives no counts gives an error whose class
// says why: fs.ErrNotExist, trace.ErrTranscriptUnreadable,
// trace.ErrTranscriptShape, trace.ErrTranscriptTooLarge, or the context's.
// With the last two of the file's own, the offset is how far a later read
// should start: past the line that is not in the shape, or the end of a span
// too large to read.
func ReadTranscript(ctx context.Context, path string, from int64) (trace.Usage, int64, error) {
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return trace.Usage{}, 0, unreadable(err)
	}
	f, err := os.Open(path)
	var fi fs.FileInfo
	if err == nil {
		defer func() { _ = f.Close() }()
		fi, err = f.Stat()
	}
	if err != nil || !fi.Mode().IsRegular() {
		return trace.Usage{}, 0, unreadable(err)
	}
	size := fi.Size()
	from = min(max(from, 0), size)
	if size-from > maxSpan {
		return trace.Usage{}, size, &transcriptError{trace.ErrTranscriptTooLarge}
	}
	t := &tally{responses: map[string]response{}, calls: map[string]bool{}, tools: map[string]int{}}
	next, err := t.read(ctx, bufio.NewReader(io.NewSectionReader(f, from, size-from)), from)
	if err != nil {
		if errors.Is(err, trace.ErrTranscriptShape) {
			return trace.Usage{}, next, err
		}
		return trace.Usage{}, 0, err
	}
	return t.usage(), next, nil
}

// unreadable is a file that is not there, is not a regular file, or could
// not be opened.
func unreadable(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return &transcriptError{fs.ErrNotExist}
	}
	return &transcriptError{trace.ErrTranscriptUnreadable}
}

// read counts every whole line from offset at, and returns the offset after
// the last one it counted. The bytes after the last newline are a line Claude
// Code is still writing where they do not decode: the read stops before them,
// so the next read takes the line whole. Where they decode, they are counted.
func (t *tally) read(ctx context.Context, r *bufio.Reader, at int64) (int64, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, &transcriptError{err}
		}
		text, n, long, err := readLine(r)
		whole := err == nil
		switch {
		case err != nil && !errors.Is(err, io.EOF):
			return 0, &transcriptError{trace.ErrTranscriptUnreadable}
		case n == 0:
			return at, nil
		case long:
			t.long++
		default:
			if bad := t.count(text); bad {
				if !whole {
					return at, nil
				}
				return at + n, &transcriptError{trace.ErrTranscriptShape}
			}
		}
		at += n
		if !whole {
			return at, nil
		}
	}
}

// readLine reads one line, its newline included, and how many bytes it took.
// A line longer than maxLine is read through a buffer at a time and not kept.
func readLine(r *bufio.Reader) (text []byte, n int64, long bool, err error) {
	for {
		chunk, err := r.ReadSlice('\n')
		n += int64(len(chunk))
		switch {
		case long:
		case len(text)+len(chunk) > maxLine:
			long, text = true, nil
		default:
			text = append(text, chunk...)
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return text, n, long, err
		}
	}
}

// count adds one line to the tally, and reports whether it is not in the
// shape the reader reads: not a JSON object, or a response whose counts are
// not whole numbers of zero or more.
func (t *tally) count(text []byte) (bad bool) {
	var l entry
	if json.Unmarshal(text, &l) != nil {
		return true
	}
	if l.Type != "assistant" || len(l.Message) == 0 {
		return false
	}
	var m message
	if json.Unmarshal(l.Message, &m) != nil {
		return true
	}
	u := m.Usage
	if u.Input < 0 || u.CacheRead < 0 || u.CacheWritten < 0 || u.Output < 0 {
		return true
	}
	// Keyed by message.id, since a response's lines repeat it: responses with
	// no id would merge into one, and Claude Code gives every response one.
	if _, seen := t.responses[m.ID]; !seen {
		t.order = append(t.order, m.ID)
	}
	t.responses[m.ID] = response{model: m.Model, tokens: trace.Tokens{
		Input: u.Input, CacheRead: u.CacheRead, CacheWritten: u.CacheWritten, Output: u.Output,
	}}
	t.countTools(m.Content)
	return false
}

// countTools counts each tool_use block once, by its id and tool: a
// response's two calls are two lines, and a call is never counted twice
// however many lines repeat it. The tool is part of the key so that two calls
// given one id, as a transcript written by hand gives them, stay two. Content
// that is not a list of blocks holds no call.
func (t *tally) countTools(content json.RawMessage) {
	var blocks []block
	if json.Unmarshal(content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		key := b.ID + "\x00" + b.Name
		if b.Type != "tool_use" || (b.ID != "" && t.calls[key]) {
			continue
		}
		if b.ID != "" {
			t.calls[key] = true
		}
		t.tools[b.Name]++
	}
}

// usage is the tally as the trace keeps it.
func (t *tally) usage() trace.Usage {
	u := trace.Usage{Models: map[string]trace.Tokens{}, Tools: t.tools, LongLines: t.long}
	for _, id := range t.order {
		r := t.responses[id]
		sum := u.Models[r.model]
		sum.Input += r.tokens.Input
		sum.CacheRead += r.tokens.CacheRead
		sum.CacheWritten += r.tokens.CacheWritten
		sum.Output += r.tokens.Output
		u.Models[r.model] = sum
		u.PeakContext = max(u.PeakContext, r.tokens.Input+r.tokens.CacheRead+r.tokens.CacheWritten)
	}
	u.Responses = len(t.order)
	return u
}
