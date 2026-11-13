package gitx

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // git's own object format names its hashes with SHA-1
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"os"
	"path"
	"strconv"
	"strings"
)

// The tree hash from the private index git writes, in Go: `git add -A` fills
// the index, and what `write-tree` would hash is computed here from it, as is
// each override's entry. Five processes a measurement became two, and one
// where the override comes from a commit the session answers for (US-129,
// round 2, T). The hash is the one git prints: the frozen AC-2 row compares it
// with git's own over every shape the loop meets.
//
// The index is asked for in version 2 with no split, sparse or many-files
// layout, and anything else found in it -- another version, an extended
// entry, a stage, a link or sparse extension, a trailer that does not match --
// sends the measurement back to git's own commands, which answer as they
// always did. Nothing here guesses.

// indexEntry is one entry of the index, as a tree records it.
type indexEntry struct {
	mode uint32
	hash []byte
	path string
}

// errIndexShape is an index this file does not read; the caller asks git.
var errIndexShape = errors.New("the index is not of the shape read here")

// treeFromIndex is the hash of the tree the index at file holds, with
// overrides applied, as `git write-tree` would print it. ok is false where git
// has to be asked instead.
func treeFromIndex(ctx context.Context, root string, env []string, file string, overrides []Override) (tree string, ok bool, err error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", false, nil //nolint:nilerr // an index that cannot be read is git's to read: its own sequence says what is wrong
	}
	hashLen, ok := indexHashLen(raw)
	if !ok {
		return "", false, nil
	}
	entries, err := parseIndex(raw, hashLen)
	if err != nil {
		return "", false, nil //nolint:nilerr // another shape than the one asked for is git's to read, not a fault
	}
	for _, o := range overrides {
		entries, ok, err = overrideEntry(ctx, root, env, entries, hashLen, o)
		if err != nil || !ok {
			return "", ok, err
		}
	}
	sum := newObjectHash(hashLen)
	return hex.EncodeToString(treeHashOf(entries, hashLen, sum)), true, nil
}

// indexHashLen is the width of the hashes in the index, told by its trailer:
// the checksum over everything before it, in the repository's own object
// format. false where neither format checks out.
func indexHashLen(raw []byte) (int, bool) {
	for _, n := range []int{sha1.Size, sha256.Size} {
		if len(raw) < 12+n {
			continue
		}
		h := newObjectHash(n)
		h.Write(raw[:len(raw)-n])
		if bytes.Equal(h.Sum(nil), raw[len(raw)-n:]) {
			return n, true
		}
	}
	return 0, false
}

func newObjectHash(n int) hash.Hash {
	if n == sha256.Size {
		return sha256.New()
	}
	return sha1.New() //nolint:gosec // git's object format
}

// parseIndex is the entries of a version 2 index, in the order git keeps
// them; an index of another shape is errIndexShape.
func parseIndex(raw []byte, hashLen int) ([]indexEntry, error) {
	const header = 12
	if len(raw) < header+hashLen || string(raw[:4]) != "DIRC" || binary.BigEndian.Uint32(raw[4:8]) != 2 {
		return nil, errIndexShape
	}
	count := int(binary.BigEndian.Uint32(raw[8:12]))
	body := raw[:len(raw)-hashLen]
	entries := make([]indexEntry, 0, count)
	at := header
	for range count {
		e, next, err := indexEntryAt(body, at, hashLen)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
		at = next
	}
	// The extensions: a split index keeps its entries in another file, and a
	// sparse index keeps directories; either is git's to read.
	for at+8 <= len(body) {
		sig := string(body[at : at+4])
		size := int(binary.BigEndian.Uint32(body[at+4 : at+8]))
		if sig == "link" || sig == "sdir" {
			return nil, errIndexShape
		}
		at += 8 + size
	}
	if at != len(body) {
		return nil, errIndexShape
	}
	return entries, nil
}

// indexEntryAt is the entry at offset at of the index's body, and the offset
// of the one after it.
func indexEntryAt(body []byte, at, hashLen int) (indexEntry, int, error) {
	fixed := 40 + hashLen + 2
	if at+fixed > len(body) {
		return indexEntry{}, 0, errIndexShape
	}
	mode := binary.BigEndian.Uint32(body[at+24 : at+28])
	sum := body[at+40 : at+40+hashLen]
	flags := binary.BigEndian.Uint16(body[at+40+hashLen : at+fixed])
	// An extended entry belongs to version 3; a stage is a conflict, which
	// write-tree refuses; a directory entry is a sparse index.
	if flags&0x4000 != 0 || (flags>>12)&3 != 0 || mode&0o170000 == 0o040000 {
		return indexEntry{}, 0, errIndexShape
	}
	nameStart := at + fixed
	end := bytes.IndexByte(body[nameStart:], 0)
	if end < 0 {
		return indexEntry{}, 0, errIndexShape
	}
	name := body[nameStart : nameStart+end]
	if n := int(flags & 0xFFF); n < 0xFFF && n != len(name) {
		return indexEntry{}, 0, errIndexShape
	}
	// Padded with NULs to a multiple of eight bytes, at least one.
	return indexEntry{mode: mode, hash: sum, path: string(name)}, at + (fixed+len(name)+8)&^7, nil
}

// overrideEntry is entries with the override o applied as applyOverride
// applies it to the index: nothing where the tree has no file at the path; the
// commit's entry, or none, where o.At names a commit; the content's blob, with
// the entry's own mode, otherwise. ok is false where git has to be asked.
func overrideEntry(ctx context.Context, root string, env []string, entries []indexEntry, hashLen int, o Override) ([]indexEntry, bool, error) {
	i, beneath := entryAt(entries, o.Path)
	if i < 0 && !beneath {
		return entries, true, nil
	}
	if o.At != "" {
		if beneath {
			return nil, false, ErrOverrideNotAFile
		}
		if err := fullHashes(o.At); err != nil {
			return nil, false, err
		}
		mode, sum, found, ok := blobAt(ctx, root, env, o.At, o.Path, hashLen)
		if !ok {
			return nil, false, nil
		}
		if !found {
			return append(entries[:i:i], entries[i+1:]...), true, nil
		}
		entries[i] = indexEntry{mode: mode, hash: sum, path: o.Path}
		return entries, true, nil
	}
	if i < 0 {
		// A file where the tree has a directory: git refuses the entry, in
		// its own words.
		return nil, false, nil
	}
	blob, err := gitInput(ctx, root, env, o.Content, "hash-object", "--stdin", "--path="+o.Path)
	if err != nil {
		return nil, false, err
	}
	sum, err := hex.DecodeString(firstLine(string(blob)))
	if err != nil || len(sum) != hashLen {
		return nil, false, nil //nolint:nilerr // no hash of the repository's width printed: git applies the override itself and says why
	}
	entries[i] = indexEntry{mode: entries[i].mode, hash: sum, path: o.Path}
	return entries, true, nil
}

// entryAt is the index of the entry at p, -1 for none, and whether any entry
// lies beneath p.
func entryAt(entries []indexEntry, p string) (int, bool) {
	at, beneath := -1, false
	for i, e := range entries {
		switch {
		case e.path == p:
			at = i
		case strings.HasPrefix(e.path, p+"/"):
			beneath = true
		}
	}
	return at, beneath
}

// blobAt is the mode and hash of the blob at p as commit has it, and found
// false where the commit has no blob there, as `ls-tree <commit> -- <p>`
// answers it: through the session where there is one, and through ls-tree
// where there is none. ok is false where neither could answer.
func blobAt(ctx context.Context, root string, env []string, commit, p string, hashLen int) (mode uint32, sum []byte, found, ok bool) {
	if entry, found, ok := treeEntryOf(ctx, root, commit, p, hashLen); ok {
		if !found || !isBlobMode(entry.mode) {
			return 0, nil, false, true
		}
		return entry.mode, entry.hash, true, true
	}
	out, err := git(ctx, root, env, "ls-tree", "-z", commit, "--", p)
	if err != nil {
		return 0, nil, false, false
	}
	for entry := range strings.SplitSeq(string(out), "\x00") {
		meta, name, hasName := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !hasName || name != p || len(fields) != 3 || fields[1] != blobType {
			continue
		}
		m, err := strconv.ParseUint(fields[0], 8, 32)
		sum, decodeErr := hex.DecodeString(fields[2])
		if err != nil || decodeErr != nil || len(sum) != hashLen {
			return 0, nil, false, false
		}
		return uint32(m), sum, true, true
	}
	return 0, nil, false, true
}

// treeEntry is one entry of a tree object.
type treeEntry struct {
	mode uint32
	name string
	hash []byte
}

// treeEntryOf is the entry at p in commit's tree, asked of the session: the
// tree that holds p is read whole and the entry found in it. found is false
// where the commit has nothing there; ok is false where the session could not
// answer.
func treeEntryOf(ctx context.Context, root, commit, p string, hashLen int) (entry treeEntry, found, ok bool) {
	s := sessionOf(ctx, root)
	if s == nil || !fullHash(commit) {
		return treeEntry{}, false, false
	}
	dir, name := path.Split(p)
	spec := commit + "^{tree}"
	if dir != "" {
		spec = commit + ":" + strings.TrimSuffix(dir, "/")
	}
	body, r, ok := s.contents(spec)
	switch {
	case !ok:
		return treeEntry{}, false, false
	case r.missing:
		// Nothing there is ls-tree printing nothing for the path -- where
		// the commit itself is there. A commit git no longer has is
		// ls-tree failing, and the fallback has to be the one to say so: the
		// commit gate refuses "the override could not be measured" for a
		// reviewed commit put beyond reach, not "the work has changed".
		if c, ok := s.info(commit); ok && c.found(typeCommit) {
			return treeEntry{}, false, true
		}
		return treeEntry{}, false, false
	case !r.found("tree"):
		return treeEntry{}, false, true
	}
	entries, err := parseTree(body, hashLen)
	if err != nil {
		return treeEntry{}, false, false
	}
	for _, e := range entries {
		if e.name == name {
			return e, true, true
		}
	}
	return treeEntry{}, false, true
}

// parseTree is the entries of a tree object's bytes: `<mode> <name>\0<hash>`
// each.
func parseTree(body []byte, hashLen int) ([]treeEntry, error) {
	var out []treeEntry
	for len(body) > 0 {
		sp := bytes.IndexByte(body, ' ')
		if sp < 0 {
			return nil, errIndexShape
		}
		nul := bytes.IndexByte(body[sp:], 0)
		if nul < 0 || sp+nul+1+hashLen > len(body) {
			return nil, errIndexShape
		}
		mode, err := strconv.ParseUint(string(body[:sp]), 8, 32)
		if err != nil {
			return nil, errIndexShape
		}
		name := string(body[sp+1 : sp+nul])
		sum := body[sp+nul+1 : sp+nul+1+hashLen]
		out = append(out, treeEntry{mode: uint32(mode), name: name, hash: sum})
		body = body[sp+nul+1+hashLen:]
	}
	return out, nil
}

// isBlobMode reports whether mode is one ls-tree lists as a blob: a file, an
// executable or a link. A submodule is a commit and a directory a tree.
func isBlobMode(mode uint32) bool {
	return mode&0o170000 == 0o100000 || mode&0o170000 == 0o120000
}

// treeHashOf is the hash of the tree holding entries, in index order, which
// is the order a tree lists its names in: a directory sorts as its name with
// a slash after it, which is how its entries' paths sort in the index.
func treeHashOf(entries []indexEntry, hashLen int, sum hash.Hash) []byte {
	var content bytes.Buffer
	for i := 0; i < len(entries); {
		e := entries[i]
		name, _, isDir := strings.Cut(e.path, "/")
		if !isDir {
			content.WriteString(strconv.FormatUint(uint64(e.mode), 8))
			content.WriteByte(' ')
			content.WriteString(name)
			content.WriteByte(0)
			content.Write(e.hash)
			i++
			continue
		}
		// The run of entries under name/, with the prefix taken off.
		j := i
		var sub []indexEntry
		for ; j < len(entries) && strings.HasPrefix(entries[j].path, name+"/"); j++ {
			sub = append(sub, indexEntry{mode: entries[j].mode, hash: entries[j].hash, path: entries[j].path[len(name)+1:]})
		}
		content.WriteString("40000 ")
		content.WriteString(name)
		content.WriteByte(0)
		content.Write(treeHashOf(sub, hashLen, newObjectHash(hashLen)))
		i = j
	}
	sum.Reset()
	sum.Write([]byte("tree " + strconv.Itoa(content.Len())))
	sum.Write([]byte{0})
	sum.Write(content.Bytes())
	return sum.Sum(nil)
}
