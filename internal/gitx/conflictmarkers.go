package gitx

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// MarkerSet is the markers of one unresolved conflict that a change adds to a
// file: a line that begins with seven "<" and a space, a base line that begins
// with seven "|" and a space (Base is 0 where the conflict has none), a line of
// seven "=", and a line that begins with seven ">" and a space, in that order.
// The numbers are lines of the file as the change leaves it, from 1.
type MarkerSet struct {
	Path                         string // slash-separated, from the root, the file's new name, unquoted
	Open, Base, Separator, Close int
}

// MarkerScan is what ConflictMarkersOutside finds.
type MarkerScan struct {
	Sets []MarkerSet
	// Beyond counts the sets read past maxMarkerSets and not kept, so that a
	// diff built to fill memory fills nothing and a refusal still has a number
	// that is not too small. A candidate whose separator was not shown in the
	// patch (see markerScan.added) is counted only when more than maxMarkerSets
	// wait to be confirmed, as one each before it is known to be a set, so the
	// figure is a bound from above on what was left out. Where it is not 0 the
	// scan did not read everything, and no sets is not none.
	Beyond int
	// Unchecked is the part of Beyond that is such candidates and not sets: none
	// of them is known to be a conflict.
	Unchecked int
}

const (
	// markerWidth is the length of a marker git writes by default. A file's
	// own conflict-marker-size is not read: the markers of another width are a
	// documented limit of the check.
	markerWidth = 7
	// maxMarkerSets bounds what a scan keeps: about 40 bytes a set.
	maxMarkerSets = 100000
	// headerKeep is how much of a line outside a hunk is held: a file's name
	// is on one, and a longer line is cut and told apart from a short one.
	headerKeep = 64 << 10
	// hunkKeep is how much of a line inside a hunk is held: the prefix and
	// the first fifteen bytes of the content, which hold any marker.
	hunkKeep = 16
)

// ConflictMarkers is every set of conflict markers that the net change from
// base to tip adds, with the line of each marker. Both are full commit hashes,
// as for Fingerprint, and nothing the repository sets or its attributes say
// can move what is read (see ConflictMarkersOutside). A change that adds none
// is no sets and no error. At most maxMarkerSets are returned.
func ConflictMarkers(ctx context.Context, root, base, tip string) ([]MarkerSet, error) {
	scan, err := ConflictMarkersOutside(ctx, root, base, tip, nil)
	return scan.Sets, err
}

// ConflictMarkersOutside is ConflictMarkers without the files exempt names:
// they are not read, and are not counted towards the cap, so that a flood of
// sets in a file a person has said may hold them cannot push a set in another
// file out. exempt is given a slash path from the root, the new name, and may
// be nil.
//
// A line counts only if the change adds it, and the sets are found among the
// added lines of one file in order, across hunks and at any distance, so a
// conflict from a rebase, which has the side that is trunk's unchanged beside
// the markers, is found. The one line a patch can hide is the separator: a
// line of seven "=" that the file already had between the added markers, which
// the diff may align with it, so a pair of added opening and closing markers
// with no added separator is a set where the file has that line between them.
//
// The patch is read with renames, so that moving a file that holds a set adds
// none, and no copies, so that a copy is an add. It is read as text whatever
// the file's attributes or content say, with the options of Fingerprint's call
// that decide how it is written, and with no pathspec, so that the root
// CODEMAP.md and everything under .sdlc are read like any other file.
//
// Nothing here is a flag of Fingerprint's call, whose options feed hashes that
// are recorded.
func ConflictMarkersOutside(ctx context.Context, root, base, tip string, exempt func(path string) bool) (MarkerScan, error) {
	if err := fullHashes(base, tip); err != nil {
		return MarkerScan{}, err
	}
	settings, err := patchSettings(ctx, root)
	if err != nil {
		return MarkerScan{}, err
	}
	args := []string{"diff-tree", "-r", "-p", "--text", argFullIndex,
		"--no-color", "--no-ext-diff", "--no-textconv", "-M",
		"-U0", "--inter-hunk-context=0", "--diff-algorithm=myers", "--indent-heuristic",
		"--src-prefix=a/", "--dst-prefix=b/", "--submodule=short", argIgnoreSubmodNone,
		base + "^{commit}", tip + "^{commit}", "--"}

	s := &markerScan{exempt: exempt}
	if err := streamPatch(ctx, root, settings, args, s.read); err != nil {
		return MarkerScan{}, err
	}
	if err := s.resolve(ctx, root); err != nil {
		return MarkerScan{}, err
	}
	slices.SortStableFunc(s.out.Sets, func(a, b MarkerSet) int {
		return cmp.Or(strings.Compare(a.Path, b.Path), cmp.Compare(a.Open, b.Open))
	})
	return s.out, nil
}

// candidate is an opening marker and a closing marker the change adds with no
// separator added between them: a set if the file has a separator there.
type candidate struct {
	set  MarkerSet
	blob string
}

// markerScan is the state of one read of a patch.
type markerScan struct {
	exempt  func(string) bool
	out     MarkerScan
	pending []candidate

	// The file being read, reset at each "diff --git".
	path  string
	blob  string // the new file's object, from the "index" line
	skip  bool
	state int // 0 nothing yet, 1 an opening marker, 2 and a base, 3 and a separator
	cur   MarkerSet
}

// read is the patch r holds, a line at a time and never a whole line: a line of
// any length costs the same. Outside a hunk a line is held up to headerKeep
// bytes, to find a file's name; inside one it is held up to hunkKeep, and the
// hunk header's counts say how many lines are the hunk's, so that a line of
// content that looks like a header is content.
func (s *markerScan) read(r *bufio.Reader) error {
	for {
		head, length, err := readLine(r, headerKeep)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := s.header(r, string(head), length > len(head)); err != nil {
			return err
		}
	}
}

// header takes one line from outside a hunk: the start of a file, its object, its
// name, or the start of a hunk, which it reads. cut is set where the line was
// longer than read held.
func (s *markerScan) header(r *bufio.Reader, line string, cut bool) error {
	switch {
	case strings.HasPrefix(line, "diff --git "):
		s.path, s.blob, s.skip, s.state = "", "", false, 0
	case strings.HasPrefix(line, "index "):
		s.blob = newBlob(line)
	case strings.HasPrefix(line, "+++ "):
		if cut {
			return malformedPatch("a file name too long to read")
		}
		return s.name(line)
	case strings.HasPrefix(line, "@@ "):
		return s.hunk(r, line)
	}
	return nil
}

// newBlob is what stands after the dots on an "index <old>..<new> <mode>" line:
// the new file's object, which is held to be a full hash where it is used.
func newBlob(line string) string {
	_, rest, _ := strings.Cut(line, "..")
	blob, _, _ := strings.Cut(rest, " ")
	return blob
}

// name takes the file's name from its "+++ b/<name>" line. Git ends it with a
// tab when the name has a space in it, and quotes it as C does when it holds a
// byte it will not print.
func (s *markerScan) name(line string) error {
	name := strings.TrimSuffix(strings.TrimPrefix(line, "+++ "), "\t")
	s.state = 0
	if name == "/dev/null" {
		s.path, s.skip = "", true
		return nil
	}
	if strings.HasPrefix(name, `"`) {
		unquoted, err := strconv.Unquote(name)
		if err != nil {
			return malformedPatch("a quoted file name it cannot unquote")
		}
		name = unquoted
	}
	path, ok := strings.CutPrefix(name, "b/")
	if !ok || path == "" {
		return malformedPatch("a file name without the prefix the call asked for")
	}
	s.path = path
	s.skip = s.exempt != nil && s.exempt(path)
	return nil
}

// hunk reads the lines of the hunk the header line opens.
func (s *markerScan) hunk(r *bufio.Reader, header string) error {
	oldLeft, line, newLeft, ok := parseHunkHeader(header)
	if !ok {
		return malformedPatch("a hunk header it cannot read")
	}
	left := hunkCounts{old: oldLeft, new: newLeft, line: line}
	for left.old > 0 || left.new > 0 {
		// A patch that ends inside a hunk is io.EOF here, which is an error and
		// not the end: the check fails closed.
		head, length, err := readLine(r, hunkKeep)
		if err != nil {
			return err
		}
		prefix := byte(' ') // a context line may have lost its space
		if len(head) > 0 {
			prefix = head[0]
		}
		at, added, ok := left.take(prefix)
		if !ok {
			return malformedPatch("a line that does not belong to its hunk")
		}
		if added {
			if err := s.addedLine(head[1:], length-1, at); err != nil {
				return err
			}
		}
	}
	return nil
}

// addedLine takes a line the change adds to the file being read, unless the
// file is one that is left out.
func (s *markerScan) addedLine(content []byte, length, line int) error {
	if s.skip {
		return nil
	}
	if s.path == "" {
		return malformedPatch("added lines in a file it has no name for")
	}
	s.added(content, length, line)
	return nil
}

// hunkCounts is what a hunk header promised and what is left of it: lines of the
// old file, lines of the new, and the number the next line of the new file has.
type hunkCounts struct{ old, new, line int }

// take accounts for a line with the given prefix. ok is false for a line the
// hunk has no room for; added reports a line the change adds, at the number at.
func (h *hunkCounts) take(prefix byte) (at int, added, ok bool) {
	at = h.line
	switch prefix {
	case '\\':
		ok = true
	case ' ':
		if ok = h.old > 0 && h.new > 0; ok {
			h.old, h.new, h.line = h.old-1, h.new-1, h.line+1
		}
	case '-':
		if ok = h.old > 0; ok {
			h.old--
		}
	case '+':
		if ok = h.new > 0; ok {
			h.new, h.line, added = h.new-1, h.line+1, true
		}
	}
	return at, added, ok
}

// hunkHeader is "@@ -a[,b] +c[,d] @@", with a section heading allowed after it.
// Made when first needed and not at start-up, which every tool call pays for.
var hunkHeader = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
})

// parseHunkHeader reads the lines the hunk has of the old file, the first line
// of the new file it covers, and how many it has there. A count left out is one.
func parseHunkHeader(header string) (oldCount, newStart, newCount int, ok bool) {
	m := hunkHeader().FindStringSubmatch(header)
	if m == nil {
		return 0, 0, 0, false
	}
	nums := [3]int{1, 0, 1}
	for i, text := range m[1:] {
		if text == "" {
			continue
		}
		n, err := strconv.Atoi(text)
		if err != nil {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

// added takes one line the change adds, at line of the new file: content is its
// first bytes after the plus and length how long it is. The four markers are
// told in order and one at a time, and a set is a whole walk through them.
func (s *markerScan) added(content []byte, length, line int) {
	switch {
	case hasMarker(content, length, '<'):
		if s.state == 0 {
			s.cur, s.state = MarkerSet{Path: s.path, Open: line}, 1
		}
	case hasMarker(content, length, '|'):
		if s.state == 1 {
			s.cur.Base, s.state = line, 2
		}
	case isSeparator(content, length):
		if s.state == 1 || s.state == 2 {
			s.cur.Separator, s.state = line, 3
		}
	case hasMarker(content, length, '>'):
		switch s.state {
		case 3:
			s.cur.Close = line
			s.keep(candidate{set: s.cur}, true)
			s.state = 0
		case 1, 2:
			// The diff may have called a separator that was there already
			// unchanged: it is for resolve to say whether the file has one.
			s.cur.Close = line
			s.keep(candidate{set: s.cur, blob: s.blob}, false)
			s.state = 0
		}
	}
}

// keep stores a set, or a candidate to be confirmed. A candidate has a bound of
// its own and counts against the sets only once resolve confirms it: counted at
// once, 100000 pairs with no separator in one file used up the cap and left a
// real set in another file unread.
func (s *markerScan) keep(c candidate, whole bool) {
	switch {
	case whole:
		s.confirm(c.set)
	case len(s.pending) >= maxMarkerSets:
		s.out.Beyond++
		s.out.Unchecked++
	default:
		s.pending = append(s.pending, c)
	}
}

// confirm keeps a set, unless the cap is reached.
func (s *markerScan) confirm(set MarkerSet) {
	if len(s.out.Sets) >= maxMarkerSets {
		s.out.Beyond++
		return
	}
	s.out.Sets = append(s.out.Sets, set)
}

// hasMarker reports whether the line starts with seven of c and a space.
func hasMarker(content []byte, length int, c byte) bool {
	if length < markerWidth+1 {
		return false
	}
	for _, b := range content[:markerWidth] {
		if b != c {
			return false
		}
	}
	return content[markerWidth] == ' '
}

// isSeparator reports whether the line is seven "=" and nothing else, bar one
// carriage return: a file with CRLF line endings has git write its conflicts
// with them.
func isSeparator(content []byte, length int) bool {
	switch length {
	case markerWidth:
		return string(content[:markerWidth]) == strings.Repeat("=", markerWidth)
	case markerWidth + 1:
		return string(content[:markerWidth]) == strings.Repeat("=", markerWidth) && content[markerWidth] == '\r'
	}
	return false
}

// resolve turns each candidate that its new file has a separator between the
// markers of into a set. The file is read from its object, once for all the
// candidates of one file, which the patch lists in order and apart.
func (s *markerScan) resolve(ctx context.Context, root string) error {
	for i := 0; i < len(s.pending); {
		j := i + 1
		for j < len(s.pending) && s.pending[j].set.Path == s.pending[i].set.Path {
			j++
		}
		if err := s.separate(ctx, root, s.pending[i:j]); err != nil {
			return err
		}
		i = j
	}
	return nil
}

// separate finds, for each candidate of one file, the first line of seven "="
// strictly between its base marker, or its opening one, and its closing one.
func (s *markerScan) separate(ctx context.Context, root string, cands []candidate) error {
	blob := cands[0].blob
	if !fullHash(blob) {
		return malformedPatch("no object for " + strconv.Quote(cands[0].set.Path))
	}
	found := make([]int, len(cands))
	err := streamPatch(ctx, root, nil, []string{"cat-file", blobType, blob}, func(r *bufio.Reader) error {
		return separatorLines(r, cands, found)
	})
	if err != nil {
		return err
	}
	for i, c := range cands {
		if found[i] > 0 {
			set := c.set
			set.Separator = found[i]
			s.confirm(set)
		}
	}
	return nil
}

// blobType is the object type cat-file is asked for.
const blobType = "blob"

// separatorLines reads a file, once through and holding no line, and puts into
// found, for each of cands, the line of the first separator between its markers.
// The candidates are in order and do not overlap, so one pass over them keeps up
// with the lines.
func separatorLines(r *bufio.Reader, cands []candidate, found []int) error {
	next := 0
	for line := 1; next < len(cands); line++ {
		head, length, err := readLine(r, markerWidth+1)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		for next < len(cands) && line >= cands[next].set.Close {
			next++
		}
		if next == len(cands) {
			return nil
		}
		c := cands[next].set
		if line > cmp.Or(c.Base, c.Open) && found[next] == 0 && isSeparator(head, length) {
			found[next] = line
		}
	}
	return nil
}

// readLine reads one line from r, however long, and holds the first keep bytes
// of it, without its line feed. length is how long the line is, so a line cut
// short is length > len(head). It reports io.EOF only once nothing is left.
func readLine(r *bufio.Reader, keep int) (head []byte, length int, err error) {
	read := false
	for {
		chunk, err := r.ReadSlice('\n')
		body := chunk
		if err == nil {
			body = chunk[:len(chunk)-1]
		}
		read = read || len(chunk) > 0
		if room := keep - len(head); room > 0 {
			head = append(head, body[:min(room, len(body))]...)
		}
		length += len(body)
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if !read {
				return nil, 0, io.EOF
			}
			return head, length, nil
		case err != nil:
			return nil, 0, err
		}
		return head, length, nil
	}
}

// malformedPatch is the error for a patch git cannot have written, where the
// check would otherwise read "found nothing" for "could not look".
func malformedPatch(what string) error {
	return sdlcerr.New(sdlcerr.RepositoryUnreadable,
		"the change could not be read",
		"git printed "+what+", which a patch does not hold")
}
