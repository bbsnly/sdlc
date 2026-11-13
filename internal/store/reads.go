package store

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Reads is what one invocation has read of the loop's files -- the backlog, a
// story's gate record, the freeze -- so that the second ask in the same
// command or hook call is answered from the first (US-129, AC-5). It belongs
// to the invocation: every Store opened for it shares one, through New,
// Reading and At, and the hook's place carries the same one. Nothing is kept
// across invocations, since a test process runs many and changes the files
// between them.
//
// A kept answer is used only while the file on disk is the one that was read:
// each ask stats the file and compares its size, modification time and
// identity with the read's, which were taken from the open handle the bytes
// came through, and a file that differs in any of them is read again. The
// store's own writes replace the file by rename, which puts another file at
// the path, and keep what they wrote in its place; a write by another
// process, or by a test's hand, is seen by the stat. The bytes handed out are
// the kept ones, read and never written to by their callers, since a copy of
// a record on every ask cost a hook call more than the read it saved (AC-8);
// the parsed values are made afresh from them on each call, so that no
// caller's edit of a record reaches the next reader.
type Reads struct {
	mu    sync.Mutex
	files map[string]read
}

type read struct {
	raw  []byte
	err  error
	info fs.FileInfo
}

// NewReads is an empty Reads.
func NewReads() *Reads { return &Reads{} }

// File is os.ReadFile(path), read once while the file is unchanged. The error
// is kept too: a record that is not there is asked for as often as one that
// is.
func (r *Reads) File(path string) ([]byte, error) {
	if r == nil {
		return os.ReadFile(path)
	}
	key := filepath.Clean(path)
	info, statErr := os.Stat(path)
	r.mu.Lock()
	defer r.mu.Unlock()
	if got, ok := r.files[key]; ok && got.unchanged(info, statErr) {
		return got.raw, got.err
	}
	raw, opened, err := readThrough(path)
	if r.files == nil {
		r.files = map[string]read{}
	}
	r.files[key] = read{raw: raw, err: err, info: opened}
	return raw, err
}

// readThrough is os.ReadFile(path) and the FileInfo of the handle the bytes
// came through, nil where there was none to ask. The handle is asked before
// it is read, so a write that lands during the read leaves the kept time
// older than the file's, and the identity, size, time and bytes are one
// file's: a FileInfo from a stat of the path before the read could be
// another file's, and on Windows held no identity until its first comparison,
// when it took that of whatever was at the path by then (US-129 design
// review, round 5).
func readThrough(path string) ([]byte, fs.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	// A handle that cannot be asked is read all the same, as os.ReadFile
	// reads it, and info is then nil: the read is not answered from again.
	info, _ := f.Stat()
	// Room for the file and for the read that finds its end, as os.ReadFile
	// sizes it: a record is 190 KB, and grown into it was copied many times.
	size := bytes.MinRead
	if info != nil {
		size += int(min(info.Size(), maxPresized))
	}
	buf := bytes.NewBuffer(make([]byte, 0, size))
	_, err = buf.ReadFrom(f)
	return buf.Bytes(), info, err
}

// maxPresized bounds the room readThrough makes before it has read anything:
// a size is the file system's word, and a file larger than this grows as it
// is read.
const maxPresized = 16 << 20

// keepWritten is what a write of path has just put there, so that the next
// ask is answered from it: a hook call hashes the record it has just written,
// and read it again for that. data is kept as it is, not copied: every writer
// encodes it for the write and lets it go. info is the written file's own,
// from the handle it was written through (fsx.WriteFileAtomicInfo), and
// nothing is kept without it: a stat of the path after the write describes
// whatever is there by then, which another writer may have replaced, beside
// this write's bytes (US-129 code review, security F-3).
func (r *Reads) keepWritten(path string, data []byte, info fs.FileInfo) {
	if r == nil || info == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.files == nil {
		r.files = map[string]read{}
	}
	r.files[filepath.Clean(path)] = read{raw: data, info: info}
}

// keep is keepWritten by a stat of path, for data that is known to be what
// path holds and whose handle is gone; nothing is kept where path is not
// there. The store's writes kept through it until the code review, and none
// does now (keepWritten says why): the Reads' own rows are what still ask it.
func (r *Reads) keep(path string, data []byte) {
	info, _ := os.Stat(path)
	r.keepWritten(path, data, info)
}

// size is how many bytes the kept read of path holds, 0 where none is kept:
// what a write of the same file is sized by.
func (r *Reads) size(path string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.files[filepath.Clean(path)].raw)
}

// unchanged reports whether the file the read came from is still what info
// describes: the same file, by identity, size and modification time, or still
// missing.
func (got read) unchanged(info fs.FileInfo, statErr error) bool {
	if got.info == nil || info == nil {
		return got.info == nil && info == nil && errors.Is(statErr, fs.ErrNotExist) && errors.Is(got.err, fs.ErrNotExist)
	}
	return os.SameFile(got.info, info) && got.info.Size() == info.Size() && got.info.ModTime().Equal(info.ModTime())
}

// Forget drops what is kept of each of paths, and of everything beneath one
// that is a directory; with no path, everything. Every write the store makes
// drops what it wrote, and a caller that has taken the loop's lock drops all,
// since another command may have written anything while it waited.
func (r *Reads) Forget(paths ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(paths) == 0 {
		clear(r.files)
		return
	}
	for _, p := range paths {
		p = filepath.Clean(p)
		delete(r.files, p)
		for kept := range r.files {
			if strings.HasPrefix(kept, p+string(filepath.Separator)) {
				delete(r.files, kept)
			}
		}
	}
}
