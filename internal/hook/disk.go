package hook

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// The bounds on what the shell rules read of the disk for one command. Each
// read that reaches one answers incomplete, which the rules refuse where the
// command writes, so raising one lets more through only by reading more.
// internal/tools/hooktime leaves these readers out, since they are disk and
// not the CPU it times: a bzip2 archive read to maxArchiveBytes takes about
// two seconds, which no leg sees.
const (
	// maxListed is the directory entries listed for one command, in all.
	maxListed = 20_000
	// maxArchiveBytes is the decompressed bytes read for one command, in
	// all; a plain tar is read by seeking past each member and pays nothing.
	maxArchiveBytes = 64 << 20
	// maxZipBytes is the largest zip whose central directory is read: Go
	// reads the whole directory before a member can be counted.
	maxZipBytes = 64 << 20
	// maxMembers and maxMemberName bound one archive's member names.
	maxMembers    = 20_000
	maxMemberName = 4096
	// tarBlock is one tar header: no tar, even an empty one, is shorter.
	tarBlock = 512
)

// diskReader answers the shell rules' questions of the disk for one command:
// what a directory holds, what an archive holds, and whether a path is a
// directory. Each path is read once, and every answer is cut short at the
// bounds above rather than read on.
type diskReader struct {
	project  string
	listed   int
	read     int64
	lists    map[string]shellpolicy.Listing
	archives map[string]shellpolicy.Listing
}

func newDiskReader(project string) *diskReader {
	return &diskReader{project: project, lists: map[string]shellpolicy.Listing{},
		archives: map[string]shellpolicy.Listing{}}
}

// path is a word of the command on disk, absolute: relative words are the
// project's, as Resolve reads them.
func (d *diskReader) path(word string) (string, bool) {
	word, ok := expandHome(word)
	if !ok || word == "" {
		return "", false
	}
	p := filepath.FromSlash(word)
	if !filepath.IsAbs(p) {
		p = filepath.Join(d.project, p)
	}
	return p, true
}

// isDir is whether a path is a directory, through a link, as cp reads its
// destination.
func (d *diskReader) isDir(word string) bool {
	p, ok := d.path(word)
	if !ok {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// stories reads the names in .sdlc/stories where records says the story
// records are kept: the main checkout, which a story's worktree has no
// .sdlc/stories of its own beside. Where it cannot be found, the listing
// failed, and any name could be a story.
func (d *diskReader) stories(records func() (string, bool)) func() shellpolicy.Listing {
	return func() shellpolicy.Listing {
		dir, found := records()
		if !found {
			return shellpolicy.Listing{Cause: listingFailed}
		}
		return d.entries(filepath.ToSlash(filepath.Join(dir, ".sdlc", "stories")))
	}
}

// entries is the names in a directory. A directory that is not there, or a
// path that is a file, holds nothing, so a glob in it names nothing and is
// complete; any other failure, and a listing past maxListed, is incomplete.
func (d *diskReader) entries(word string) shellpolicy.Listing {
	if l, ok := d.lists[word]; ok {
		return l
	}
	l := d.list(word)
	d.lists[word] = l
	return l
}

func (d *diskReader) list(word string) shellpolicy.Listing {
	p, ok := d.path(word)
	if !ok {
		return shellpolicy.Listing{Cause: listingFailed}
	}
	info, err := os.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
		return shellpolicy.Listing{Complete: true, Missing: true}
	case err != nil:
		return shellpolicy.Listing{Cause: listingFailed}
	case !info.IsDir():
		return shellpolicy.Listing{Complete: true}
	}
	f, err := os.Open(p)
	if err != nil {
		return shellpolicy.Listing{Cause: listingFailed}
	}
	defer func() { _ = f.Close() }()
	// Where earlier listings shared the bound, this one is not blamed for a
	// count of its own: told only "more than 20000 entries", a refusal said
	// `.sdlc/stories` held them when it held one entry and another directory
	// had listed 20000 (red team r5, verifier r4 gap 2).
	cause := "more than " + strconv.Itoa(maxListed) + " entries"
	if d.listed > 0 {
		cause += listedElsewhere
	}
	var names []string
	for {
		chunk, err := f.ReadDir(256)
		for _, e := range chunk {
			names = append(names, e.Name())
		}
		d.listed += len(chunk)
		switch {
		case d.listed > maxListed:
			return shellpolicy.Listing{Names: names, Cause: cause}
		case errors.Is(err, io.EOF):
			return shellpolicy.Listing{Names: names, Complete: true}
		case err != nil:
			return shellpolicy.Listing{Names: names, Cause: listingFailed}
		}
	}
}

// members is the member names of an archive, read by what the file holds
// and never by its name. It is complete only where the archive was read to
// its end cleanly: one that is not there, that is not a regular file (a
// FIFO or a device would hold the decision until the deadline), in a format
// not read here, cut short, malformed, or past a bound, is incomplete.
func (d *diskReader) members(word string) shellpolicy.Listing {
	if l, ok := d.archives[word]; ok {
		return l
	}
	l := d.readArchive(word)
	d.archives[word] = l
	return l
}

// archiveReaders are the formats whose member names are read, each known by
// the bytes it starts with; anything else is read as a plain tar, which has
// none. The refusal of an archive in none of them names these formats
// (archiveFormats), so a reader taken out here leaves the words.
var archiveReaders = []struct {
	format string
	magic  []string
	read   func(d *diskReader, f *os.File, size int64) shellpolicy.Listing
}{
	{"tar.gz", []string{"\x1f\x8b"}, func(d *diskReader, f *os.File, _ int64) shellpolicy.Listing {
		z, err := gzip.NewReader(f)
		if err != nil {
			return shellpolicy.Listing{Cause: err.Error()}
		}
		return tarMembers(&countingReader{r: z, d: d})
	}},
	{"tar.bz2", []string{"BZh"}, func(d *diskReader, f *os.File, _ int64) shellpolicy.Listing {
		return tarMembers(&countingReader{r: bzip2.NewReader(f), d: d})
	}},
	{"zip", []string{"PK\x03\x04", "PK\x05\x06"}, func(_ *diskReader, f *os.File, size int64) shellpolicy.Listing {
		return zipMembers(f, size)
	}},
}

// archiveFormats are the formats the readers read, plain tar first.
func archiveFormats() []string {
	formats := []string{"tar"}
	for _, r := range archiveReaders {
		formats = append(formats, r.format)
	}
	return formats
}

func (d *diskReader) readArchive(word string) shellpolicy.Listing {
	p, ok := d.path(word)
	if !ok || word == "-" {
		return shellpolicy.Listing{}
	}
	info, err := os.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return shellpolicy.Listing{Missing: true}
	case err != nil:
		return shellpolicy.Listing{Cause: "it could not be opened"}
	case !info.Mode().IsRegular():
		return shellpolicy.Listing{Cause: "it is not a regular file"}
	}
	f, err := os.Open(p)
	if err != nil {
		return shellpolicy.Listing{Cause: "it could not be opened"}
	}
	defer func() { _ = f.Close() }()
	magic := make([]byte, 4)
	n, _ := io.ReadFull(f, magic)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return shellpolicy.Listing{Cause: "it could not be read"}
	}
	format, read, matched := archiveReaderFor(string(magic[:n]))
	// A plain tar is read through the file itself, so that tar seeks past each
	// member's contents. xz, zstd and 7z fail at the first header, and are
	// named as a format not read rather than as an archive cut short. A file
	// shorter than one tar header is no tar: a 156-byte tar.xz ended before
	// the header, and was answered "unexpected EOF" as though a tar were cut.
	if !matched && info.Size() < tarBlock {
		return shellpolicy.Listing{}
	}
	l := read(d, f, info.Size())
	// The format read is set on a complete listing, so an extractor that reads
	// its archive by name (unzip) can tell a file read as this format from its
	// own fall-back to another (shellpolicy B4).
	if l.Complete {
		l.Format = format
	}
	return l
}

// archiveReaderFor is the reader for the format a file's magic bytes name,
// with the format's name and whether a known format matched; a plain tar
// reader and "tar" where none did.
func archiveReaderFor(magic string) (string, func(d *diskReader, f *os.File, size int64) shellpolicy.Listing, bool) {
	for _, r := range archiveReaders {
		for _, m := range r.magic {
			if strings.HasPrefix(magic, m) {
				return r.format, r.read, true
			}
		}
	}
	return "tar", func(_ *diskReader, f *os.File, _ int64) shellpolicy.Listing { return tarMembers(f) }, false
}

// zipMembers is a zip's member names, from its central directory.
func zipMembers(f *os.File, size int64) shellpolicy.Listing {
	if size > maxZipBytes {
		return shellpolicy.Listing{Cause: "a zip of more than " + strconv.Itoa(maxZipBytes>>20) + " MiB"}
	}
	z, err := zip.NewReader(f, size)
	switch {
	case err != nil:
		return shellpolicy.Listing{Cause: err.Error()}
	case len(z.File) > maxMembers:
		return shellpolicy.Listing{Cause: tooManyMembers}
	}
	names := make([]string, 0, len(z.File))
	for _, m := range z.File {
		if len(m.Name) > maxMemberName {
			return shellpolicy.Listing{Names: names, Cause: longMemberName}
		}
		names = append(names, m.Name)
	}
	return shellpolicy.Listing{Names: names, Complete: true}
}

var (
	tooManyMembers = "more than " + strconv.Itoa(maxMembers) + " members"
	longMemberName = "a member's name is longer than " + strconv.Itoa(maxMemberName) + " bytes"
)

// tarMembers is a tar's member names, read to the end of the archive.
func tarMembers(r io.Reader) shellpolicy.Listing {
	t := tar.NewReader(r)
	var names []string
	for {
		h, err := t.Next()
		switch {
		case errors.Is(err, io.EOF):
			return shellpolicy.Listing{Names: names, Complete: true}
		case errors.Is(err, tar.ErrHeader) && len(names) == 0:
			// Not a tar at all: no cause, so the refusal names the formats.
			return shellpolicy.Listing{}
		case err != nil:
			return shellpolicy.Listing{Names: names, Cause: err.Error()}
		case len(names) >= maxMembers:
			return shellpolicy.Listing{Names: names, Cause: tooManyMembers}
		case len(h.Name) > maxMemberName:
			return shellpolicy.Listing{Names: names, Cause: longMemberName}
		}
		names = append(names, h.Name)
	}
}

// countingReader is a decompressed stream that fails once the command has
// read maxArchiveBytes from all its archives together.
type countingReader struct {
	r io.Reader
	d *diskReader
}

// archiveBoundError is the error a countingReader fails with.
type archiveBoundError struct{}

func (archiveBoundError) Error() string {
	return "more than " + strconv.Itoa(maxArchiveBytes>>20) + " MiB once decompressed"
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.d.read >= maxArchiveBytes {
		return 0, archiveBoundError{}
	}
	n, err := c.r.Read(p)
	c.d.read += int64(n)
	return n, err
}

// listingFailed is the cause a listing that failed for any reason but the
// bound gives. listedElsewhere is added to the bound's cause where the
// command had listed other directories first, since the bound is counted
// across all of them.
const (
	listingFailed   = "the listing failed"
	listedElsewhere = ", counting those the command listed in other directories first"
)
