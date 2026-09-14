package store

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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
func (s *Store) ReviewSubject(ctx context.Context) (string, error) {
	path := s.cfg.BacklogPath(s.root)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return gitx.TreeHash(ctx, s.root)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return gitx.TreeHash(ctx, s.root)
	}
	// A backlog that does not parse is hashed as it is. That is still the same
	// answer every time for the same bytes, which is all a subject has to be.
	normal, ok := withoutStoryFields(raw, storyBookkeeping...)
	if !ok {
		return gitx.TreeHash(ctx, s.root)
	}
	return gitx.TreeHash(ctx, s.root, gitx.Override{Path: filepath.ToSlash(rel), Content: normal})
}

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
		edited := applyChanges(data, members, changes)
		if !bytes.HasSuffix(edited, []byte("\n")) {
			edited = append(edited, '\n')
		}
		return edited, true
	}
	return nil, false
}

// withoutStoryFields returns the backlog with the given keys taken out of every
// story.
//
// It is the inverse of editStory in the one way that matters: a value replaced
// in place and a key added after the last one both come out again as exactly
// the bytes that were there before, so a backlog reads the same here before and
// after sdlc moves a story.
func withoutStoryFields(data []byte, keys ...string) ([]byte, bool) {
	out := bytes.Clone(data)
	for {
		start, end, found, ok := firstStoryField(out, keys)
		if !ok {
			return nil, false
		}
		if !found {
			return out, true
		}
		out = append(out[:start:start], out[end:]...)
	}
}

// firstStoryField locates the first member of any story that keys names, as
// the span removing it takes out: the member, and the comma that joins it to
// the member before it -- or, for a first member, to the one after.
func firstStoryField(data []byte, keys []string) (start, end int, found, ok bool) {
	top, ok := objectMembers(data, 0, len(data))
	if !ok {
		return 0, 0, false, false
	}
	stories, ok := lastMember(top, "stories")
	if !ok {
		return 0, 0, false, false
	}
	elements, ok := arrayElements(data, stories.valueStart, stories.valueEnd)
	if !ok {
		return 0, 0, false, false
	}
	for _, el := range elements {
		members, ok := objectMembers(data, el[0], el[1])
		if !ok {
			return 0, 0, false, false
		}
		for i, m := range members {
			if !slices.ContainsFunc(keys, func(k string) bool { return strings.EqualFold(k, m.key) }) {
				continue
			}
			switch {
			case i > 0:
				return members[i-1].valueEnd, m.valueEnd, true, true
			case len(members) > 1:
				return m.keyStart, members[1].keyStart, true, true
			default:
				return m.keyStart, m.valueEnd, true, true
			}
		}
	}
	return 0, 0, false, true
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

	// Later offsets first, so each splice leaves the ones still to come where
	// they were.
	sort.SliceStable(splices, func(i, j int) bool { return splices[i].start > splices[j].start })
	out := bytes.Clone(data)
	for _, s := range splices {
		out = append(out[:s.start:s.start], append(bytes.Clone(s.text), out[s.end:]...)...)
	}
	return out
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
	_ = enc.Encode(s) // a string always encodes
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// lastMember finds the member encoding/json would read a key from: the last
// one whose name matches without regard to case.
func lastMember(members []member, key string) (member, bool) {
	for i := len(members) - 1; i >= 0; i-- {
		if strings.EqualFold(members[i].key, key) {
			return members[i], true
		}
	}
	return member{}, false
}
