package shellpolicy

import (
	"cmp"
	"path"
	"slices"
	"strings"
)

// writtenTo are the words of a command that name what it writes. For most
// programs that is every word, which is how the rules have always read them.
// An archiver or a copy to elsewhere is given much that it only reads: `tar
// --exclude=.git -cf - .` writes to standard output, and `rsync -a --exclude
// .sdlc ./ /tmp/snap/` writes only to /tmp/snap.
func writtenTo(words []string) []string {
	args := words[1:]
	switch base(words[0]) {
	case "tar":
		// What an archiver removes once it has copied it, it writes as well.
		return append(tarWrites(args), takenAway(words)...)
	case "rsync":
		return append(copyWrites(args, rsyncValued, "--log-file", "--write-batch", "--only-write-batch"), takenAway(words)...)
	case "scp":
		return copyWrites(args, scpValued)
	case "find":
		// Listing into a file is not a write to what is listed.
		if !hasWord(args, findDelete) && !runsAWriter(args) {
			return printedTo(args)
		}
	case "cp", "copy", "copy-item", "cpi", "mv", "move", "move-item", "mi":
		return append(args, everyLanding(words, false)...)
	case "ln", "install":
		// Every word stays, so a hard link's source, a second name the file
		// can be written through, is read as written; the landings come
		// after, so a command refused by one of its own words is refused in
		// the same words.
		return append(args, everyLanding(words, true)...)
	case "curl":
		return curlWrites(args)
	case "wget":
		return wgetWrites(args)
	}
	if written, known := givenWrites(words); known {
		return written
	}
	return args
}

// tarWrites are what tar writes: the archive it creates, with an incremental
// archive's snapshot file, or where it extracts and the members it is asked for.
func tarWrites(args []string) []string {
	archives, into, operands, extract := tarArguments(args)
	if extract {
		return append(into, operands...)
	}
	return archives
}

// tarArguments are the archives tar is given, the directories it is told to
// change to, its operands, and whether it extracts.
func tarArguments(args []string) (archives, into, operands []string, extract bool) {
	t := tarScan{args: args}
	for t.next < len(args) {
		a := args[t.next]
		t.next++
		switch {
		case a == "--extract" || a == "--get":
			t.extract = true
		case a == "--file" || a == "--listed-incremental":
			v := t.value("")
			t.archives = append(t.archives, v)
		case a == "--directory":
			t.changeTo(t.value(""))
		case a == "--exclude" || a == "--exclude-from" || a == "--files-from":
			t.value("")
		case strings.HasPrefix(a, "--file=") || strings.HasPrefix(a, "--listed-incremental="):
			_, v, _ := strings.Cut(a, "=")
			t.archives = append(t.archives, v)
		case strings.HasPrefix(a, "--directory="):
			t.changeTo(strings.TrimPrefix(a, "--directory="))
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") || t.next == 1:
			// The first word is options whether or not it starts with a dash.
			t.bundle(strings.TrimPrefix(a, "-"))
		default:
			if t.dir != "" {
				a = path.Join(t.dir, a)
			}
			t.operands = append(t.operands, a)
		}
	}
	return t.archives, t.into, t.operands, t.extract
}

// tarScan is tarArguments part way through tar's words: what it has read so
// far, and next, the word it reads next.
type tarScan struct {
	args                     []string
	next                     int
	archives, into, operands []string
	extract                  bool
	// dir is where the last -C sent tar, which the operands after it are
	// below.
	dir string
}

// value is the value of an option: the rest of its bundle, or the next word,
// which old-style bundles such as `tar cvfC a.tar dir` hand out in the order
// their letters are written.
func (t *tarScan) value(rest string) string {
	if rest == "" && t.next < len(t.args) {
		t.next++
		return t.args[t.next-1]
	}
	return rest
}

// changeTo is -C: where tar extracts, and where the operands after it are.
func (t *tarScan) changeTo(to string) {
	t.into = append(t.into, to)
	t.dir = to
}

// bundle reads one bundle of tar's option letters, up to the first that
// takes a value, which is the rest of the bundle or the next word.
func (t *tarScan) bundle(bundle string) {
	for i := 0; i < len(bundle); i++ {
		switch bundle[i] {
		case 'x':
			t.extract = true
		case 'f', 'g':
			v := t.value(bundle[i+1:])
			t.archives = append(t.archives, v)
			i = len(bundle)
		case 'C':
			t.changeTo(t.value(bundle[i+1:]))
			i = len(bundle)
		case 'T', 'X':
			t.value(bundle[i+1:])
			i = len(bundle)
		}
	}
}

// rsyncValued and scpValued are the options that take the next word as their
// value, which is not a path the copy is given.
var rsyncValued = map[string]bool{
	"-e": true, "-f": true, "-T": true, "-B": true, "--rsh": true, "--filter": true,
	"--exclude": true, "--include": true, "--exclude-from": true, "--include-from": true,
	"--files-from": true, "--temp-dir": true, "--partial-dir": true, "--backup-dir": true,
	"--compare-dest": true, "--copy-dest": true, "--link-dest": true, "--suffix": true,
	"--chmod": true, "--chown": true, "--password-file": true, "--rsync-path": true,
	"--log-file": true, "--write-batch": true, "--only-write-batch": true, "--out-format": true,
	"--timeout": true, "--port": true, "--bwlimit": true, "--max-size": true, "--min-size": true,
	"-M": true, "-@": true, "--info": true, "--debug": true, "--stderr": true, "--remote-option": true,
	"--usermap": true, "--groupmap": true, "--copy-as": true, "--contimeout": true, "--modify-window": true,
	"--compress-choice": true, "--zc": true, "--compress-level": true, "--zl": true, "--skip-compress": true,
	"--checksum-choice": true, "--cc": true, "--checksum-seed": true, "--block-size": true, "--max-delete": true,
	"--max-alloc": true, "--address": true, "--sockopts": true, "--outbuf": true, "--log-file-format": true,
	"--early-input": true, "--stop-after": true, "--stop-at": true, "--read-batch": true, "--protocol": true,
	"--iconv": true,
}

var scpValued = map[string]bool{
	"-i": true, "-P": true, "-o": true, "-F": true, "-c": true, "-l": true, "-S": true, "-J": true, "-D": true, "-X": true,
}

// copyWrites is the destination of a copy, its last operand, and the values of
// the options named in writes, which are files it writes as well.
func copyWrites(args []string, valued map[string]bool, writes ...string) []string {
	written := writtenByOptions(args, writes)
	// A single operand is a listing, not a copy.
	operands := copyOperands(args, valued)
	if len(operands) <= 1 {
		return written
	}
	to := operands[len(operands)-1]
	written = append(written, to)
	return append(written, landedUnder(to, operands[:len(operands)-1])...)
}

// writtenByOptions are the values of the options named in writes, glued on
// with `=` or in the word after.
func writtenByOptions(args, writes []string) []string {
	var written []string
	for i, a := range args {
		name, v, glued := strings.Cut(a, "=")
		for _, w := range writes {
			if name == w {
				if !glued && i+1 < len(args) {
					v = args[i+1]
				}
				written = append(written, v)
			}
		}
	}
	return written
}

// landedUnder are the names a copy's sources take in its destination.
func landedUnder(to string, sources []string) []string {
	// Into a directory, a file lands under its own name: `scp
	// host:proj/CLAUDE.md .` writes CLAUDE.md. Not on another machine.
	if remote(to) || !intoADirectory(to, len(sources)) {
		return nil
	}
	var landed []string
	for _, from := range sources {
		name := clean(from)
		if remote(from) {
			_, name, _ = strings.Cut(name, ":")
		}
		landed = append(landed, path.Join(to, path.Base(name)))
	}
	return landed
}

// intoADirectory reports whether a copy's destination is a directory the files land
// in, rather than the file one of them becomes: more than one source, a
// trailing slash, `..`, or a name with no extension. `rsync -a CLAUDE.md
// CLAUDE.md.bak` writes the backup, not CLAUDE.md.bak/CLAUDE.md.
func intoADirectory(to string, sources int) bool {
	if sources > 1 || strings.HasSuffix(to, "/") {
		return true
	}
	name := path.Base(clean(to))
	return name == ".." || !strings.Contains(name[1:], ".")
}

// everyLanding are the names a cp, mv, ln or install (or their PowerShell and
// cmd names) writes in its destination: each source under its own name. With
// anyName set, that is whatever the destination's name looks like: a link
// and an install are made into a directory as often as onto a file, and a
// name with a dot is no sign of which. Read as a file, `ln -sf
// <outside>/CLAUDE.md .` replaced the root's CLAUDE.md with a link, and
// `install <outside>/CLAUDE.md ~/src/github.com` was let through. Without
// it, a destination intoADirectory reads as a file, and not named by an
// option, lands nothing, as the release reads a cp or mv: `cp x
// internal/calc.v2` writes the file. A one-operand ln makes the link where
// the command runs.
//
// Where bundleTarget reads a target as well, every source of that reading
// lands under it too, after the release's reading.
func everyLanding(words []string, anyName bool) []string {
	name := base(words[0])
	from, to, target := linkArguments(name, words[1:])
	var landed []string
	if anyName || target || intoADirectory(to, len(from)) {
		landed = landedIn(to, from)
	}
	if bundled, into, ok := bundleTarget(name, words[1:]); ok {
		landed = append(landed, landedIn(into, bundled)...)
	}
	return landed
}

// linkArguments is moveArguments for a cp, mv, ln or install, but that a
// one-operand ln makes the link where the command runs, as though given -t:
// `ln -sf /tmp/x/CLAUDE.md` makes ./CLAUDE.md.
func linkArguments(name string, args []string) (from []string, to string, target bool) {
	from, to, target = moveArguments(args)
	if name == "ln" && !target && len(from) == 0 {
		return []string{to}, ".", true
	}
	return from, to, target
}

// landedIn are the sources under to, each under its own name. Where to has
// a `**` with a `..` after it, each is spelt under to as well, uncleaned,
// after the cleaned ones, so that a refusal the released tool made keeps its
// words: path.Join reads `**/..` as one name and drops both, and `cp
// /tmp/p/CLAUDE.md /tmp/**/../work` landed only `/tmp/work/CLAUDE.md`.
func landedIn(to string, from []string) []string {
	// clean takes the trailing slash off, and with it all of `/`.
	dir := cmp.Or(clean(to), path.Clean(to))
	names := make([]string, 0, len(from))
	var spelt []string
	for _, f := range from {
		name := path.Base(clean(f))
		names = append(names, path.Join(dir, name))
		spelt = append(spelt, dir+"/"+name)
	}
	if climbsPastGlobstar(to) {
		names = append(names, spelt...)
	}
	return names
}

// remote reports whether a copy's operand is on another machine, as rsync and
// scp read one: a host and a colon before any slash. Not a Windows drive.
func remote(operand string) bool {
	host, _, ok := strings.Cut(operand, ":")
	return ok && len(host) > 1 && !strings.Contains(host, "/")
}

// copyOperands are the paths a copy is given: what it copies, and last, where
// it copies to.
func copyOperands(args []string, valued map[string]bool) []string {
	var operands []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case valued[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			operands = append(operands, a)
		}
	}
	return operands
}

// givenWrites are the files a program writes that it is given by an option or
// an operand, and known whether the program is one of those read here. Each
// gets a reader that returns only what it writes: counted as changing every
// file it is given, `sort CLAUDE.md` and `uniq CLAUDE.md out.txt` would be
// refused as writes, and counted as changing none, `sort -o CLAUDE.md x`
// rewrote the contract as a read.
func givenWrites(words []string) (written []string, known bool) {
	args := words[1:]
	switch name := base(words[0]); name {
	case "sort":
		return outputOption(args, "kotST"), true
	case "base64":
		// It writes the file whether it encodes or decodes.
		return outputOption(args, "biw"), true
	case "iconv":
		return outputOption(args, "fto"), true
	case "uniq":
		return uniqWrites(args), true
	default:
		return decompressed(name, args)
	}
}

// option is one option as scanOptions read it: a letter, or a long option's
// name, with the value it took.
type option struct{ name, value string }

// scanOptions reads a program's words as getopt does. A word of one dash is a
// bundle of letters; at a letter in valued, the rest of the bundle, or the
// next word where the bundle ends there, is its value. A long word that is a
// start, three bytes or more, of one of long takes its `=` value or the next
// word, under that name; any other long word is passed over alone, and a
// value it leaves behind is read as an operand. `--` ends the options.
func scanOptions(args []string, valued string, long ...string) (opts []option, operands []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		var read []option
		switch {
		case a == "--":
			return opts, append(operands, args[i+1:]...)
		case strings.HasPrefix(a, "--"):
			read, i = longOption(args, i, long)
		case strings.HasPrefix(a, "-") && a != "-":
			read, i = optionBundle(args, i, valued)
		default:
			operands = append(operands, a)
		}
		opts = append(opts, read...)
	}
	return opts, operands
}

// longOption is the long option at args[i], under the name of the one of long
// it starts, and the index of the last word it took.
func longOption(args []string, i int, long []string) ([]option, int) {
	name, value, glued := strings.Cut(args[i], "=")
	for _, l := range long {
		if abbreviates(name, l) {
			name = l
			if !glued && i+1 < len(args) {
				value, i = args[i+1], i+1
			}
		}
	}
	return []option{{name, value}}, i
}

// optionBundle is the letters of the bundle at args[i], up to the first that
// takes a value, and the index of the last word they took.
func optionBundle(args []string, i int, valued string) ([]option, int) {
	a := args[i]
	var opts []option
	for j := 1; j < len(a); j++ {
		o := option{name: a[j : j+1]}
		if strings.IndexByte(valued, a[j]) < 0 {
			opts = append(opts, o)
			continue
		}
		o.value = a[j+1:]
		if o.value == "" && i+1 < len(args) {
			o.value, i = args[i+1], i+1
		}
		return append(opts, o), i
	}
	return opts, i
}

// outputOption is the file -o or a start of --output names, with valued the
// program's other letters that take a value: `-o X`, `-oX`, `-uo X` and
// `sort x -o X` all write X. These programs write no operand.
func outputOption(args []string, valued string) []string {
	opts, _ := scanOptions(args, valued+"o", "--output")
	var out []string
	for _, o := range opts {
		if (o.name == "o" || o.name == "--output") && o.value != "" {
			out = append(out, o.value)
		}
	}
	return out
}

// uniqWrites are uniq's operands after the first, which it writes, but for
// `-`, standard output. -f, -s and -w take a value.
func uniqWrites(args []string) []string {
	_, operands := scanOptions(args, "fsw")
	var out []string
	for i, w := range operands {
		if i > 0 && w != "-" {
			out = append(out, w)
		}
	}
	return out
}

// decompressed is what a decompression writes: each operand without its
// compressed suffix, and known whether name is one of the programs read here.
// They are named whole, since a prefix test misses gunzip and bunzip2. The
// un- names decompress whatever they are given, and the rest where given -d
// or a start of --decompress or --uncompress. Nothing is written where one
// writes to standard output, tests or lists; those long options are matched
// exactly, so an abbreviation of one is read as a decompression that writes,
// the safe direction. -f is not asked for: without it the program does not
// replace a file that is there, but creates one that is not.
//
// The letters that take a value are each program's own, as its manual
// (Apple gzip 487.0.1, xz 5.8, macOS compress, bzip2 1.0.8) names them, -S
// and --suffix the suffix: a letter wrongly read as a flag leaves its value
// as an operand with no compressed suffix, which writes nothing, and one
// wrongly read as taking a value would hide an operand.
func decompressed(name string, args []string) ([]string, bool) {
	valued := ""
	switch name {
	case "gzip", "gunzip", "pigz", "unpigz":
		valued = "S"
	case "xz", "unxz", "lzma", "unlzma":
		valued = "SFCTM"
	case "compress", "uncompress":
		valued = "b"
	case "bzip2", "bunzip2":
	default:
		return nil, false
	}
	decompress := slices.Contains([]string{"gunzip", "unpigz", "unxz", "unlzma", "bunzip2", "uncompress"}, name)
	opts, operands := scanOptions(args, valued, "--suffix")
	suffix := ""
	for _, o := range opts {
		switch o.name {
		case "d":
			decompress = true
		case "c", "t", "l", "--stdout", "--to-stdout", "--test", "--list":
			return nil, true
		case "S", "--suffix":
			suffix = o.value
		default:
			decompress = decompress || abbreviates(o.name, "--decompress") || abbreviates(o.name, "--uncompress")
		}
	}
	var written []string
	for _, op := range operands {
		if w, ok := unsuffixed(op, suffix); ok && decompress {
			written = append(written, w)
		}
	}
	return written, true
}

// unsuffixed is the name a decompression gives an operand: without the
// suffix -S gave, or one the programs know, compared folded (`.z` is
// compress's `.Z`), with the tar ones becoming .tar. ok is false for an
// operand with none, which the program leaves alone.
func unsuffixed(op, given string) (string, bool) {
	for _, s := range [][2]string{
		{given, ""}, {".gz", ""}, {"-gz", ""}, {".z", ""}, {"-z", ""}, {"_z", ""}, {".xz", ""}, {".lzma", ""},
		{".bz2", ""}, {".bz", ""}, {".tgz", ".tar"}, {".taz", ".tar"}, {".txz", ".tar"}, {".tlz", ".tar"},
		{".tbz", ".tar"}, {".tbz2", ".tar"},
	} {
		if s[0] != "" && len(op) > len(s[0]) && strings.EqualFold(op[len(op)-len(s[0]):], s[0]) {
			return op[:len(op)-len(s[0])] + s[1], true
		}
	}
	return "", false
}
