package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/gitx"
)

// The backlog belongs to the people who write it. The tool changes two fields
// of one story -- its status, and when it was updated -- and it changes them in
// the bytes of the file, so that everything else stays as it was written: the
// fields the tool has no name for, the order of the keys, the indentation, the
// escapes. Decoding into model.Backlog and encoding it back would drop the
// first and rewrite the rest, and the backlog is a tracked file whose every
// byte is in the tree the reviewers approved.
//
// Every function here reports failure as false rather than as an error. Backlog
// has already refused anything that is not an object with a stories array, so
// a false is a bug, and the caller says so.

// storyBookkeeping is what sdlc writes into a story as the loop moves it along.
var storyBookkeeping = []string{"status", "updated"}

// ReviewSubject is what a review of the work is stamped with, and what a commit
// is checked against: the working tree, less the loop's own directory, and less
// the status and updated time of every story in the backlog.
//
// Those two are the loop's bookkeeping, written by sdlc as a story starts,
// waits for a person, resumes and finishes. None of that is the work a
// reviewer approved, and counting it sent both reviewers back to work that had
// not changed. Everything else in the backlog -- the acceptance criteria above
// all -- still counts.
//
// also are further files measured as other than they are on disk: the hook
// measures a finished story's CODEMAP.md as HEAD has it.
func (s *Store) ReviewSubject(ctx context.Context, also ...gitx.Override) (string, error) {
	path := s.cfg.BacklogPath(s.root)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return gitx.TreeHash(ctx, s.root, also...)
	}
	raw, err := s.reads.File(path)
	if err != nil {
		return gitx.TreeHash(ctx, s.root, also...)
	}
	// A backlog that does not parse is hashed as it is. That is still the same
	// answer every time for the same bytes, which is all a subject has to be.
	normal, ok := withoutStoryFields(raw, storyBookkeeping...)
	if !ok {
		return gitx.TreeHash(ctx, s.root, also...)
	}
	return gitx.TreeHash(ctx, s.root, append([]gitx.Override{{Path: filepath.ToSlash(rel), Content: normal}}, also...)...)
}

// BacklogSubject is the backlog as ReviewSubject measures it: its path from
// the root, slash-separated, and its bytes less every story's status and
// updated. ok is false where ReviewSubject measures no backlog of its own: one
// outside the root, or one that cannot be read. One that does not parse is
// given as it is, as ReviewSubject hashes it.
//
// It is read only where it is a regular file, by fsx.OpenRegular, and never
// through the invocation's reads: the implementation gate asks it before and
// after the commands, with the project's lock held, and an open that followed
// a link a command swapped in for a named pipe would wait there until an
// interrupt (code review, security F-3). Anything else is not ok, which the
// gate takes by the path's type.
func (s *Store) BacklogSubject() (rel string, normal []byte, ok bool) {
	path := s.cfg.BacklogPath(s.root)
	r, err := filepath.Rel(s.root, path)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", nil, false
	}
	f, _, err := fsx.OpenRegular(path)
	if err != nil {
		return "", nil, false
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	if err != nil {
		return "", nil, false
	}
	if n, parsed := withoutStoryFields(raw, storyBookkeeping...); parsed {
		raw = n
	}
	return filepath.ToSlash(r), raw, true
}

// ReviewSubjectAt is ReviewSubject of commit rather than of the working tree:
// the tree that lands, measured as the implementation gate measured the tree
// it ran on, so that the two compare. The backlog is the one commit holds,
// less the same bookkeeping.
func (s *Store) ReviewSubjectAt(ctx context.Context, commit string) (string, error) {
	path := s.cfg.BacklogPath(s.root)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return gitx.CommitTreeHash(ctx, s.root, commit)
	}
	slash := filepath.ToSlash(rel)
	raw, err := gitx.FileAt(ctx, s.root, commit, slash)
	if errors.Is(err, fs.ErrNotExist) {
		return gitx.CommitTreeHash(ctx, s.root, commit)
	}
	if err != nil {
		return "", err
	}
	normal, ok := withoutStoryFields(raw, storyBookkeeping...)
	if !ok {
		return gitx.CommitTreeHash(ctx, s.root, commit)
	}
	return gitx.CommitTreeHash(ctx, s.root, commit, gitx.Override{Path: slash, Content: normal})
}

// byteOrderMark is what Windows PowerShell 5 starts a file it saves with. The
// file is otherwise fine, and the loop reads it as if the mark were not there.
var byteOrderMark = []byte("\ufeff")

// member is one key of a JSON object, located by its offsets in the file.
type member struct {
	key                  string
	keyStart, keyEnd     int
	valueStart, valueEnd int
}

// change sets a key to a value already encoded as JSON.
type change struct {
	key   string
	value []byte
}

// editStory returns the backlog with the given keys of the story id set, and
// every other byte as it was.
func editStory(data []byte, id string, changes ...change) ([]byte, bool) {
	// Windows PowerShell 5 starts a file it saves with a byte order mark. The edit
	// is made after it, and the mark is kept.
	if body, found := bytes.CutPrefix(data, byteOrderMark); found {
		edited, ok := editStory(body, id, changes...)
		return append(bytes.Clone(byteOrderMark), edited...), ok
	}
	top, ok := objectMembers(data, 0, len(data))
	if !ok {
		return nil, false
	}
	stories, ok := lastMember(top, "stories")
	if !ok {
		return nil, false
	}
	elements, ok := arrayElements(data, stories.valueStart, stories.valueEnd)
	if !ok {
		return nil, false
	}
	for _, el := range elements {
		members, ok := objectMembers(data, el[0], el[1])
		if !ok {
			return nil, false
		}
		idMember, ok := lastMember(members, "id")
		if !ok {
			continue
		}
		var storyID string
		if json.Unmarshal(data[idMember.valueStart:idMember.valueEnd], &storyID) != nil || storyID != id {
			continue
		}
		return endLikeFile(applyChanges(data, members, changes), data), true
	}
	return nil, false
}

// endLikeFile ends edited the way the lines of the file it was made from
// already end: a bare newline after a file written with CRLF is a line ending
// nobody wrote.
func endLikeFile(edited, data []byte) []byte {
	if !bytes.HasSuffix(edited, []byte("\n")) {
		if bytes.Contains(data, []byte("\r\n")) {
			edited = append(edited, '\r')
		}
		edited = append(edited, '\n')
	}
	return edited
}

// withoutStoryFields returns the backlog with the given keys taken out of every
// story.
//
// It is the inverse of editStory in the one way that matters: a value replaced
// in place and a key added after the last one both come out again as exactly
// the bytes that were there before, so a backlog reads the same here before and
// after sdlc moves a story.
//
// The whole file is parsed once. An earlier version located one member, cut
// it, and parsed the rest again for the next, which on a 130-story backlog
// meant 260 passes over 400 KB: 740 MB and 0.8 s for every review stamp
// (US-129). The spans cut here are the ones that version would have cut, one
// member at a time, so the bytes come out the same.
func withoutStoryFields(data []byte, keys ...string) ([]byte, bool) {
	data = bytes.TrimPrefix(data, byteOrderMark)
	top, ok := objectMembers(data, 0, len(data))
	if !ok {
		return nil, false
	}
	stories, ok := lastMember(top, "stories")
	if !ok {
		return nil, false
	}
	elements, ok := arrayElements(data, stories.valueStart, stories.valueEnd)
	if !ok {
		return nil, false
	}
	var cuts [][2]int
	for _, el := range elements {
		members, ok := objectMembers(data, el[0], el[1])
		if !ok {
			return nil, false
		}
		spans, kept := storyFieldSpans(members, keys)
		if !kept {
			// Nothing would be left of the story, and an object with no
			// members is not one a story can be: refused, as the pass that
			// parsed the cut story again refused it.
			return nil, false
		}
		cuts = append(cuts, spans...)
	}
	out := make([]byte, 0, len(data))
	at := 0
	for _, c := range cuts {
		// Two spans that start at the same kept member overlap; the later
		// one only extends the cut.
		if c[0] > at {
			out = append(out, data[at:c[0]]...)
		}
		at = max(at, c[1])
	}
	return append(out, data[at:]...), true
}

// storyFieldSpans is the spans that take the members keys name out of one
// story, each as cutting them one at a time would have taken it: the member
// and the comma that joins it to the last member kept before it -- or, with no
// member kept before it, to the one after. kept is whether any member stays.
func storyFieldSpans(members []member, keys []string) (spans [][2]int, kept bool) {
	prevKept := -1
	for i, m := range members {
		if !slices.ContainsFunc(keys, func(k string) bool { return strings.EqualFold(k, m.key) }) {
			prevKept = i
			kept = true
			continue
		}
		switch {
		case prevKept >= 0:
			spans = append(spans, [2]int{members[prevKept].valueEnd, m.valueEnd})
		case i+1 < len(members):
			spans = append(spans, [2]int{m.keyStart, members[i+1].keyStart})
		default:
			spans = append(spans, [2]int{m.keyStart, m.valueEnd})
		}
	}
	return spans, kept
}

// applyChanges replaces the value of every key a change names, and adds the
// keys the story does not have after its last one, spaced the way that one is.
//
// Keys are matched without regard to case, because that is how encoding/json
// reads them into model.Story: a "Status" the tool left alone beside a
// "status" it added would leave the read to whichever came last.
func applyChanges(data []byte, members []member, changes []change) []byte {
	type splice struct {
		start, end int
		text       []byte
	}
	var splices []splice

	last := members[len(members)-1]
	indentStart := last.keyStart
	for indentStart > 0 && isSpace(data[indentStart-1]) {
		indentStart--
	}
	indent := data[indentStart:last.keyStart]
	separator := data[last.keyEnd:last.valueStart]

	var added []byte
	for _, c := range changes {
		found := false
		for _, m := range members {
			if strings.EqualFold(m.key, c.key) {
				splices = append(splices, splice{m.valueStart, m.valueEnd, c.value})
				found = true
			}
		}
		if !found {
			added = append(added, ',')
			added = append(added, indent...)
			added = append(added, jsonString(c.key)...)
			added = append(added, separator...)
			added = append(added, c.value...)
		}
	}
	if added != nil {
		splices = append(splices, splice{last.valueEnd, last.valueEnd, added})
	}

	// The spans never overlap: each is one member's value, or the end of the
	// last one, and the two keys a status change names are distinct. So the
	// file is written once, in order; splicing each in turn copied the rest
	// of a 400 KB backlog twice per change (US-129, round 6).
	sort.SliceStable(splices, func(i, j int) bool { return splices[i].start < splices[j].start })
	out := make([]byte, 0, len(data)+len(added))
	at := 0
	for _, s := range splices {
		out = append(out, data[at:s.start]...)
		out = append(out, s.text...)
		at = s.end
	}
	return append(out, data[at:]...)
}

// objectMembers locates the members of the JSON object at data[start:end].
// An object with no members is not one a story can be, and is refused.
func objectMembers(data []byte, start, end int) ([]member, bool) {
	dec := json.NewDecoder(bytes.NewReader(data[start:end]))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	var members []member
	for dec.More() {
		before := start + int(dec.InputOffset())
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok {
			return nil, false
		}
		m := member{key: key, keyEnd: start + int(dec.InputOffset())}
		// Only spaces and a comma come between the last value and this key, so
		// the first quote after it opens the key.
		m.keyStart = before + bytes.IndexByte(data[before:m.keyEnd], '"')

		if m.valueStart, m.valueEnd, ok = nextValue(dec, data, start); !ok {
			return nil, false
		}
		members = append(members, m)
	}
	return members, len(members) > 0
}

// arrayElements locates the elements of the JSON array at data[start:end].
func arrayElements(data []byte, start, end int) ([][2]int, bool) {
	dec := json.NewDecoder(bytes.NewReader(data[start:end]))
	if t, err := dec.Token(); err != nil || t != json.Delim('[') {
		return nil, false
	}
	var elements [][2]int
	for dec.More() {
		valueStart, valueEnd, ok := nextValue(dec, data, start)
		if !ok {
			return nil, false
		}
		elements = append(elements, [2]int{valueStart, valueEnd})
	}
	return elements, true
}

// nextValue reads one value and says where in data it is. It checks the
// offsets against the bytes the decoder read, so that an edit is never made
// at a place it only believes the value to be.
func nextValue(dec *json.Decoder, data []byte, base int) (start, end int, ok bool) {
	var value json.RawMessage
	if err := dec.Decode(&value); err != nil {
		return 0, 0, false
	}
	end = base + int(dec.InputOffset())
	start = end - len(value)
	if start < base || !bytes.Equal(data[start:end], value) {
		return 0, 0, false
	}
	return start, end, true
}

// jsonString encodes s as JSON, leaving <, > and & as they are: the file is
// read by people, not embedded in a page.
func jsonString(s string) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		// Unreachable, since a string always encodes; Go's own quoting is the
		// nearest valid JSON string should it ever happen.
		return []byte(strconv.Quote(s))
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// lastMember finds the member encoding/json would read a key from: the last
// one whose name matches without regard to case.
func lastMember(members []member, key string) (member, bool) {
	for _, member := range slices.Backward(members) {
		if strings.EqualFold(member.key, key) {
			return member, true
		}
	}
	return member{}, false
}
