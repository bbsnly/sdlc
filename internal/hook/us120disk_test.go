package hook

// US-120, the disk readers (disk.go): what the hook reads of the disk for the
// shell rules, asked of the reader directly, so that each way a read can end
// is seen to answer as PLAN Steps 9 and 13 list it. A read is complete only
// where it reached the end cleanly; missing, not a directory, not a regular
// file, failed, cut at a bound and malformed each say so (THREATS T11, T12).
// shellpolicy's us120disk_test.go hands each answer to the rules; the rows at
// the end go through the whole hook, for the wiring and the formats a
// refusal names. Every archive is built here, but for bzip2, which the
// standard library cannot write: u120Bzip2 is a fixed one.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// u120Bzip2 is a tar.bz2 holding one member, notes/a.txt: a ustar archive
// made with tar and compressed with `bzip2 -9`.
const u120Bzip2 = "425a6839314159265359d4b3d36300003c5b90d1104001ff84020062019e400400000820005443535064f5006ca34f50" +
	"24a328c8340001f4fb8349d126121faa8192191eccc603be1c78d3ce28da5110181ba60282a49ccdaec86af29d0914dd8a56e" +
	"2d0d58f63e5533fc5dc914e1424352cf4d8c0"

// u120Xz is the same archive compressed with `xz -9`: 156 bytes, shorter than
// the one 512-byte block a tar header takes.
const u120Xz = "fd377a585a000004e6d6b44604c059801021011c00000000000000006efafd7be007ff00515d00371bcaeee65b2ec05934" +
	"0dfc3c84e37470b238a88f615f441ae0b2cffd47e81d04c5aff1c083e2902031b8b9100a07c869c4c77e350d418e96348f9a0" +
	"cec9fb363273115a2b3c14a32f930a3b61f7ef400000000008b14f2793f796e600001758010000000a2af4579b1c467fb020000000004595a"

// u120Nobody is a word under the home of a user there is not, which the
// reader cannot place.
const u120Nobody = "~u120-no-such-user/x"

func u120Bytes(t *testing.T, h string) []byte {
	t.Helper()
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// u120TarOf is a tar archive holding each member, with a short body.
func u120TarOf(t *testing.T, members ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, name := range members {
		body := []byte("x\n")
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func u120Gzip(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// u120ZipOf is a zip archive holding each member, stored, with no body.
func u120ZipOf(t *testing.T, members ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, name := range members {
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func u120File(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// u120Read checks a listing: its names, in any order, whether it is complete
// and missing, and that its cause holds cause ("" for none).
func u120Read(t *testing.T, criterion, word string, got shellpolicy.Listing, names []string, complete, missing bool, cause string) {
	t.Helper()
	have := slices.Sorted(slices.Values(got.Names))
	want := slices.Sorted(slices.Values(names))
	switch {
	case !slices.Equal(have, want):
		t.Errorf("%s %s lists %q; want %q", criterion, word, have, want)
	case got.Complete != complete || got.Missing != missing:
		t.Errorf("%s %s = %+v; want complete %v, missing %v", criterion, word, got, complete, missing)
	case cause == "" && got.Cause != "":
		t.Errorf("%s %s gives the cause %q; want none, so that the refusal names the formats read", criterion, word, got.Cause)
	case !strings.Contains(got.Cause, cause):
		t.Errorf("%s %s gives the cause %q; want one that says %q", criterion, word, got.Cause, cause)
	}
}

// u120Unprivileged skips where a mode cannot keep the test out of a file:
// Windows does not read the Unix bits, and root reads what it likes.
func u120Unprivileged(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not take a file's Unix mode bits to refuse a read, which the row is about")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root, which reads a file whatever its mode, so no read can be made to fail")
	}
}

// u120Lock takes every permission off dir for the rest of the test, and puts
// them back so that the temp directory can be removed.
func u120Lock(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// AC-6 (PLAN Step 9, exits 1, 3, 5 and 6): the directory reader lists a
// directory to its end as complete; a directory that is not there, or a path
// under a file, holds nothing and is complete and missing; a file holds
// nothing and is complete. A word it cannot place on the disk is a listing
// that failed. Each directory is read once for a command.
func TestAC6TheDirectoryReaderSaysWhetherItListedADirectoryToItsEnd(t *testing.T) {
	root := t.TempDir()
	write(t, root, "build/a.txt", "x\n")
	write(t, root, "build/b.txt", "x\n")
	write(t, root, "notes.md", "x\n")
	d := newDiskReader(root)
	for _, row := range []struct {
		word              string
		names             []string
		complete, missing bool
		cause             string
	}{
		{"build", []string{"a.txt", "b.txt"}, true, false, ""},
		{slash(filepath.Join(root, "build")), []string{"a.txt", "b.txt"}, true, false, ""},
		{"gone", nil, true, true, ""},
		{"notes.md/x", nil, true, true, ""},
		{"notes.md", nil, true, false, ""},
		{u120Nobody, nil, false, false, listingFailed},
	} {
		t.Run("AC-6 "+row.word, func(t *testing.T) {
			u120Read(t, "AC-6", row.word, d.entries(row.word), row.names, row.complete, row.missing, row.cause)
		})
	}
	// Read once: a name made after the first read is not seen by the same
	// command, which asks each of its rules of the same listing.
	write(t, root, "build/c.txt", "x\n")
	u120Read(t, "AC-6", "build, asked again", d.entries("build"), []string{"a.txt", "b.txt"}, true, false, "")
}

// AC-10 (PLAN Step 9, exit 5; THREATS T11): the entries listed for one
// command are bounded in all, and a listing that goes past the bound is cut
// short and incomplete, with the names read before the cut and the bound as
// its cause. The bound is counted across every directory the command lists.
func TestAC10TheDirectoryReaderStopsAtTheBoundForTheWholeCommand(t *testing.T) {
	root := t.TempDir()
	write(t, root, "build/a.txt", "x\n")
	write(t, root, "build/b.txt", "x\n")
	write(t, root, "other/c.txt", "x\n")
	bound := "more than 20000 entries"
	d := newDiskReader(root)
	d.listed = maxListed - 1
	u120Read(t, "AC-10", "build past the bound", d.entries("build"), []string{"a.txt", "b.txt"}, false, false, bound)
	shared := newDiskReader(root)
	shared.listed = maxListed - 2
	u120Read(t, "AC-10", "build up to the bound", shared.entries("build"), []string{"a.txt", "b.txt"}, true, false, "")
	u120Read(t, "AC-10", "other after it", shared.entries("other"), []string{"c.txt"}, false, false, bound)
}

// AC-10 (PLAN Step 9, exits 2 and 4): a directory that cannot be read, or a
// path under one, is a listing that failed, never an empty one.
func TestAC10ADirectoryThatCannotBeReadIsAListingThatFailed(t *testing.T) {
	u120Unprivileged(t)
	root := t.TempDir()
	write(t, root, "locked/x.txt", "x\n")
	u120Lock(t, filepath.Join(root, "locked"))
	d := newDiskReader(root)
	u120Read(t, "AC-10", "locked", d.entries("locked"), nil, false, false, listingFailed)
	u120Read(t, "AC-10", "locked/inner", d.entries("locked/inner"), nil, false, false, listingFailed)
}

// AC-4 (PLAN Step 4): the reader says whether a word is a directory as cp
// reads its destination: through a link, and false for a file, for nothing
// there, and for a word it cannot place.
func TestAC4TheDiskReaderSaysWhetherADestinationIsADirectory(t *testing.T) {
	root := t.TempDir()
	write(t, root, "pkg.v2/x.go", "package x\n")
	write(t, root, "notes.v2", "x\n")
	d := newDiskReader(root)
	for word, want := range map[string]bool{
		"pkg.v2": true, slash(filepath.Join(root, "pkg.v2")): true,
		"notes.v2": false, "gone.v2": false, u120Nobody: false,
	} {
		if got := d.isDir(word); got != want {
			t.Errorf("AC-4 isDir(%q) = %v; want %v", word, got, want)
		}
	}
	t.Run("AC-4 through a link", func(t *testing.T) {
		u120Link(t, filepath.Join(root, "pkg.v2"), filepath.Join(root, "link.v2"))
		if !d.isDir("link.v2") {
			t.Error("AC-4 a link to a directory is not read as one, as cp reads it")
		}
	})
}

// AC-10 (PLAN Step 13, exit 4): the archive reader chooses the format by the
// bytes the file starts with, never by its name, and reads plain tar,
// tar.gz, tar.bz2 and zip to their end. Each file here is named for another
// format than the one it holds. A plain tar is read by seeking past each
// member, so it is not counted against the bytes a command may decompress;
// a compressed one is.
func TestAC10TheArchiveReaderReadsEachFormatByWhatTheFileHolds(t *testing.T) {
	side := t.TempDir()
	tarred := u120TarOf(t, "CLAUDE.md", "notes/a.txt")
	u120File(t, filepath.Join(side, "plain.zip"), tarred)
	u120File(t, filepath.Join(side, "gzipped.zip"), u120Gzip(t, tarred))
	u120File(t, filepath.Join(side, "bzipped.tar"), u120Bytes(t, u120Bzip2))
	u120File(t, filepath.Join(side, "zipped.tar"), u120ZipOf(t, "CLAUDE.md", "notes/"))
	u120File(t, filepath.Join(side, "empty.zip"), u120ZipOf(t))
	u120File(t, filepath.Join(side, "empty.tar"), u120TarOf(t))
	if got, want := archiveFormats(), []string{"tar", "tar.gz", "tar.bz2", "zip"}; !slices.Equal(got, want) {
		t.Errorf("AC-10 archiveFormats() = %q; want %q, which the refusal of an archive in none of them names", got, want)
	}
	d := newDiskReader(side)
	at := func(name string) string { return slash(filepath.Join(side, name)) }
	u120Read(t, "AC-10", "a plain tar", d.members(at("plain.zip")), []string{"CLAUDE.md", "notes/a.txt"}, true, false, "")
	if d.read != 0 {
		t.Errorf("AC-10 a plain tar counted %d bytes against the decompression bound; it is read by seeking", d.read)
	}
	u120Read(t, "AC-10", "a tar.gz", d.members(at("gzipped.zip")), []string{"CLAUDE.md", "notes/a.txt"}, true, false, "")
	gzipped := d.read
	if gzipped == 0 {
		t.Error("AC-10 a tar.gz was read without counting what it decompressed")
	}
	u120Read(t, "AC-10", "a tar.bz2", d.members(at("bzipped.tar")), []string{"notes/a.txt"}, true, false, "")
	if d.read == gzipped {
		t.Error("AC-10 a tar.bz2 was read without counting what it decompressed")
	}
	u120Read(t, "AC-10", "a zip", d.members(at("zipped.tar")), []string{"CLAUDE.md", "notes/"}, true, false, "")
	u120Read(t, "AC-10", "an empty zip", d.members(at("empty.zip")), nil, true, false, "")
	u120Read(t, "AC-10", "an empty tar", d.members(at("empty.tar")), nil, true, false, "")
	// Read once for a command: gone from the disk, it is still what was read.
	if err := os.Remove(filepath.Join(side, "plain.zip")); err != nil {
		t.Fatal(err)
	}
	u120Read(t, "AC-10", "a plain tar, asked again", d.members(at("plain.zip")), []string{"CLAUDE.md", "notes/a.txt"}, true, false, "")
}

// AC-10 (PLAN Step 13, exits 1 to 6; THREATS T11): an archive the reader
// could not read to its end cleanly is incomplete, whatever it did read: one
// that is not there, standard input, a word it cannot place, a file in no
// format it reads (no cause, so that the refusal names the formats), one cut
// short, one with a bad header after its first member, and a gzip, bzip2 or
// zip that is malformed or cut short.
func TestAC10AnArchiveTheReaderCannotReadToItsEndIsIncomplete(t *testing.T) {
	side := t.TempDir()
	two := u120TarOf(t, "a.txt", "b.txt")
	u120File(t, filepath.Join(side, "garbage.bin"), bytes.Repeat([]byte("x"), 1024))
	// Cut part way through the second member's header, so that b.txt is lost.
	u120File(t, filepath.Join(side, "cut.tar"), two[:1024+100])
	u120File(t, filepath.Join(side, "badheader.tar"), append(u120TarOf(t, "a.txt")[:1024], bytes.Repeat([]byte("x"), 512)...))
	u120File(t, filepath.Join(side, "badheader.tgz"), append([]byte{0x1f, 0x8b}, bytes.Repeat([]byte{0xff}, 30)...))
	gz := u120Gzip(t, two)
	u120File(t, filepath.Join(side, "cut.tgz"), gz[:len(gz)/2])
	bz := u120Bytes(t, u120Bzip2)
	u120File(t, filepath.Join(side, "cut.tbz"), bz[:len(bz)/2])
	u120File(t, filepath.Join(side, "garbage.tbz"), append([]byte("BZh9"), bytes.Repeat([]byte{0xff}, 60)...))
	u120File(t, filepath.Join(side, "garbage.zip"), append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0xff}, 100)...))
	d := newDiskReader(side)
	at := func(name string) string { return slash(filepath.Join(side, name)) }
	for _, row := range []struct {
		name, word string
		names      []string
		missing    bool
		cause      string
	}{
		{"not there", at("gone.tar"), nil, true, ""},
		{"standard input", "-", nil, false, ""},
		{"a word it cannot place", u120Nobody, nil, false, ""},
		{"in no format it reads", at("garbage.bin"), nil, false, ""},
		{"a tar cut short", at("cut.tar"), []string{"a.txt"}, false, "EOF"},
		{"a bad header after a member", at("badheader.tar"), []string{"a.txt"}, false, "invalid tar header"},
		{"a gzip with a bad header", at("badheader.tgz"), nil, false, "gzip"},
		{"a gzip cut short", at("cut.tgz"), nil, false, "EOF"},
		{"a bzip2 cut short", at("cut.tbz"), nil, false, "EOF"},
		{"a bzip2 that is malformed", at("garbage.tbz"), nil, false, "bzip2"},
		{"a zip that is malformed", at("garbage.zip"), nil, false, "zip"},
	} {
		t.Run("AC-10 "+row.name, func(t *testing.T) {
			u120Read(t, "AC-10", row.name, d.members(row.word), row.names, false, row.missing, row.cause)
		})
	}
}

// AC-10 (PLAN Step 13, exit 4; New refusal wording): a tar.xz is in no
// format the reader reads, and disk.go says xz, zstd and 7z are named as such
// rather than as an archive cut short. A small one is shorter than a tar
// block, so the plain-tar reading of it ends part way through its first
// header; it is still an xz, and has to be answered with no cause, so that
// the refusal names the formats the hook reads instead of saying the archive
// could not be read to its end.
func TestAC10ASmallXzArchiveIsAnsweredAsAFormatTheReaderDoesNotRead(t *testing.T) {
	side := t.TempDir()
	u120File(t, filepath.Join(side, "p.tar.xz"), u120Bytes(t, u120Xz))
	d := newDiskReader(side)
	u120Read(t, "AC-10", "a 156-byte tar.xz", d.members(slash(filepath.Join(side, "p.tar.xz"))), nil, false, false, "")
}

// AC-10 (PLAN Step 13, exits 4 to 6; THREATS T11): the bounds on one
// archive's members and names, the bound on a zip's size, and the bound on
// what a command decompresses in all each cut the read short, as incomplete,
// with the names read before the cut.
func TestAC10AnArchivePastABoundIsIncomplete(t *testing.T) {
	side := t.TempDir()
	at := func(name string) string { return slash(filepath.Join(side, name)) }
	many := make([]string, maxMembers+1)
	for i := range many {
		many[i] = "m/" + strings.Repeat("x", 1+i%7) + "-" + string(rune('a'+i%26)) + "-" + itoa(i)
	}
	long := strings.Repeat("n", maxMemberName+1)
	u120File(t, filepath.Join(side, "many.tar"), u120TarOf(t, many...))
	u120File(t, filepath.Join(side, "many.zip"), u120ZipOf(t, many...))
	u120File(t, filepath.Join(side, "long.tar"), u120TarOf(t, "ok.txt", long))
	u120File(t, filepath.Join(side, "long.zip"), u120ZipOf(t, "ok.txt", long))
	u120File(t, filepath.Join(side, "p.tgz"), u120Gzip(t, u120TarOf(t, "a.txt")))
	u120File(t, filepath.Join(side, "p.tbz"), u120Bytes(t, u120Bzip2))
	// A zip past the size bound, kept sparse: only its first bytes are written.
	big, err := os.Create(filepath.Join(side, "big.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := big.WriteString("PK\x03\x04"); err != nil {
		t.Fatal(err)
	}
	if err := big.Truncate(maxZipBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := big.Close(); err != nil {
		t.Fatal(err)
	}
	d := newDiskReader(side)
	t.Run("AC-10 more members than the bound, in a tar", func(t *testing.T) {
		got := d.members(at("many.tar"))
		if got.Complete || got.Cause != tooManyMembers || len(got.Names) != maxMembers {
			t.Errorf("AC-10 a tar of %d members = complete %v, cause %q, %d names; want incomplete, %q, %d names",
				len(many), got.Complete, got.Cause, len(got.Names), tooManyMembers, maxMembers)
		}
	})
	t.Run("AC-10 more members than the bound, in a zip", func(t *testing.T) {
		u120Read(t, "AC-10", "many.zip", d.members(at("many.zip")), nil, false, false, tooManyMembers)
	})
	t.Run("AC-10 a member's name past the bound", func(t *testing.T) {
		u120Read(t, "AC-10", "long.tar", d.members(at("long.tar")), []string{"ok.txt"}, false, false, longMemberName)
		u120Read(t, "AC-10", "long.zip", d.members(at("long.zip")), []string{"ok.txt"}, false, false, longMemberName)
	})
	t.Run("AC-10 a zip past the size bound", func(t *testing.T) {
		u120Read(t, "AC-10", "big.zip", d.members(at("big.zip")), nil, false, false, "a zip of more than 64 MiB")
	})
	// What the command has decompressed already counts against the next read.
	for _, name := range []string{"p.tgz", "p.tbz"} {
		t.Run("AC-10 the decompression bound "+name, func(t *testing.T) {
			spent := newDiskReader(side)
			spent.read = maxArchiveBytes
			u120Read(t, "AC-10", name, spent.members(at(name)), nil, false, false, "more than 64 MiB once decompressed")
		})
	}
}

// itoa is a member's number as text, so that each name is its own.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

// AC-10 (PLAN Step 13, exit 2; THREATS T12): an archive word that names a
// directory, a named pipe or a device is not a regular file, and is answered
// incomplete without being opened: a pipe would hold the decision until the
// deadline, and a device such as /dev/null would read as an empty archive.
func TestAC10AnArchiveThatIsNotARegularFileIsNotOpened(t *testing.T) {
	side := t.TempDir()
	d := newDiskReader(side)
	notRegular := "it is not a regular file"
	u120Read(t, "AC-10", "a directory", d.members(slash(side)), nil, false, false, notRegular)
	t.Run("AC-10 a named pipe", func(t *testing.T) {
		pipe := filepath.Join(side, "p.tar")
		u120Fifo(t, pipe)
		u120Read(t, "AC-10", "a named pipe", d.members(slash(pipe)), nil, false, false, notRegular)
	})
	t.Run("AC-10 a device", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows has no device in the file system for an archive word to name, which the row is about")
		}
		u120Read(t, "AC-10", os.DevNull, d.members(os.DevNull), nil, false, false, notRegular)
	})
}

// AC-10 (PLAN Step 13, exits 2 and 3): an archive whose directory cannot be
// searched, or which cannot be opened, could not be read.
func TestAC10AnArchiveThatCannotBeOpenedIsIncomplete(t *testing.T) {
	u120Unprivileged(t)
	side := t.TempDir()
	u120File(t, filepath.Join(side, "locked/p.tar"), u120TarOf(t, "CLAUDE.md"))
	u120File(t, filepath.Join(side, "secret.tar"), u120TarOf(t, "CLAUDE.md"))
	u120Lock(t, filepath.Join(side, "locked"))
	u120Lock(t, filepath.Join(side, "secret.tar"))
	d := newDiskReader(side)
	for _, name := range []string{"locked/p.tar", "secret.tar"} {
		u120Read(t, "AC-10", name, d.members(slash(filepath.Join(side, name))), nil, false, false, "it could not be opened")
	}
}

// u120Says checks the hook refuses cmd under rule with a reason that holds
// each of want.
func u120Says(t *testing.T, criterion, root, cmd, rule string, want ...string) {
	t.Helper()
	r := call(t, command(root, "sdlc-implementer", cmd), noEnv)
	if got := agRuleOf(r); got != rule {
		t.Errorf("%s %q is answered %q; want %s:\n%s", criterion, cmd, got, rule, reasonOf(r))
		return
	}
	for _, w := range append(want, "Instead:") {
		if !strings.Contains(reasonOf(r), w) {
			t.Errorf("%s the refusal of %q does not say %q:\n%s", criterion, cmd, w, reasonOf(r))
		}
	}
}

// AC-10 (PLAN Step 13): through the hook, an extraction into the project is
// judged by the members the reader reads from each format, and one whose
// members the reader cannot read is refused as archive-members-unknown,
// naming the formats it reads. The tar.bz2 holds only notes/a.txt, so it is
// let through.
func TestAC10ThroughTheHookAnExtractionIsJudgedByWhatTheReaderReads(t *testing.T) {
	root, home, base, payload, side := u120World(t)
	u120File(t, filepath.Join(side, "p.tgz"), u120Gzip(t, u120TarOf(t, "CLAUDE.md")))
	u120File(t, filepath.Join(side, "p.zip"), u120ZipOf(t, "CLAUDE.md"))
	u120File(t, filepath.Join(side, "p.tar"), u120TarOf(t, "CLAUDE.md"))
	u120File(t, filepath.Join(side, "n.tbz"), u120Bytes(t, u120Bzip2))
	u120File(t, filepath.Join(side, "x.tar.xz"), append([]byte("\xfd7zXZ\x00"), bytes.Repeat([]byte{0xff}, 1024)...))
	for row, rule := range map[string]string{
		"tar -xzf {side}/p.tgz -C .":   u120Protected,
		"unzip {side}/p.zip -d .":      u120Protected,
		"bsdtar -xf {side}/p.tar -C .": u120Protected,
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-10 "+row, func(t *testing.T) { u120Refused(t, "AC-10", root, cmd, noEnv, rule) })
	}
	t.Run("AC-10 a tar.bz2 of an ordinary file", func(t *testing.T) {
		u120Allowed(t, "AC-10", root, u120Expand("tar -xjf {side}/n.tbz -C .", root, home, base, payload, side))
	})
	formats := "(it reads tar, tar.gz, tar.bz2 and zip)"
	for row, want := range map[string]string{
		"tar -xf {side}/gone.tar -C .":  "does not exist yet",
		"tar -xJf {side}/x.tar.xz -C .": "is an archive the hook cannot read the member names of " + formats,
		"tar -xf {side} -C .":           "could not be read to its end (it is not a regular file)",
		"7z x {side}/p.7z":              "is extracted by `7z`, whose archives the hook cannot read the member names of " + formats,
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-10 "+row, func(t *testing.T) { u120Says(t, "AC-10", root, cmd, "archive-members-unknown", want) })
	}
}
