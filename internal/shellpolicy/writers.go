package shellpolicy

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/model"
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
		written := copyWrites(args, rsyncValued, "--log-file", "--write-batch", "--only-write-batch")
		return slices.Concat(written, relativeLandings(args), takenAway(words))
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
	archives, snapshots, into, operands, extract := tarArguments(args)
	if extract {
		return append(into, operands...)
	}
	return append(archives, snapshots...)
}

// tarArguments are the archives tar is given (-f/--file), the snapshot files
// of an incremental archive (-g/--listed-incremental, which are not archives),
// the directories it is told to change to, its operands, and whether it
// extracts. Archives and snapshots are kept apart because an extraction given
// more than one archive is unreadable (disk.go), where a snapshot beside one
// archive is not: `tar -xg snap -f a.tar -C .` reads one archive.
func tarArguments(args []string) (archives, snapshots, into, operands []string, extract bool) {
	t := tarScan{args: args}
	for t.next < len(args) {
		a := args[t.next]
		t.next++
		switch {
		case a == "--extract" || a == "--get":
			t.extract = true
		case a == "--file":
			t.archives = append(t.archives, t.value(""))
		case a == "--listed-incremental":
			t.snapshots = append(t.snapshots, t.value(""))
		case a == "--directory":
			t.changeTo(t.value(""))
		case a == "--exclude" || a == "--exclude-from" || a == "--files-from":
			t.value("")
		case strings.HasPrefix(a, "--file="):
			_, v, _ := strings.Cut(a, "=")
			t.archives = append(t.archives, v)
		case strings.HasPrefix(a, "--listed-incremental="):
			_, v, _ := strings.Cut(a, "=")
			t.snapshots = append(t.snapshots, v)
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
	return t.archives, t.snapshots, t.into, t.operands, t.extract
}

// tarScan is tarArguments part way through tar's words: what it has read so
// far, and next, the word it reads next.
type tarScan struct {
	args                                []string
	next                                int
	archives, snapshots, into, operands []string
	extract                             bool
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
		case 'f':
			t.archives = append(t.archives, t.value(bundle[i+1:]))
			i = len(bundle)
		case 'g':
			t.snapshots = append(t.snapshots, t.value(bundle[i+1:]))
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

// relativeLandings are where rsync -R puts its sources: each under the
// destination by its whole path, or by what follows a `/./` in it, which
// rsync marks as where the kept path starts. `rsync -R
// /tmp/p/./.sdlc/config.json dst/` writes dst/.sdlc/config.json, which a
// landing by name put at dst/config.json.
func relativeLandings(args []string) []string {
	relative := hasWord(args, "--relative")
	for _, a := range args {
		relative = relative || strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "R")
	}
	operands := copyOperands(args, rsyncValued)
	if !relative || len(operands) < 2 {
		return nil
	}
	to := operands[len(operands)-1]
	var landed []string
	for _, from := range operands[:len(operands)-1] {
		kept := strings.TrimLeft(from, "/")
		if _, after, ok := strings.Cut(from, "/./"); ok {
			kept = after
		}
		landed = append(landed, path.Join(to, kept))
	}
	return landed
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
	for _, name := range localNames(sources) {
		landed = append(landed, path.Join(to, path.Base(name)))
	}
	return landed
}

// localNames are a copy's sources as paths, a remote one's host taken off.
func localNames(sources []string) []string {
	names := make([]string, 0, len(sources))
	for _, from := range sources {
		name := clean(from)
		if remote(from) {
			_, name, _ = strings.Cut(name, ":")
		}
		names = append(names, name)
	}
	return names
}

// copyValued are the copy programs read by their operands, each with the
// options that take a value.
var copyValued = map[string]map[string]bool{"rsync": rsyncValued, "scp": scpValued}

// copyArguments is what a program that lands its sources under its
// destination copies, and where: a cp, mv, ln or install (and their
// PowerShell and cmd names) as linkArguments reads it, and an rsync or scp
// by its operands, the last the destination. false for any other program,
// and for an rsync or scp given one operand, which is a listing.
func copyArguments(words []string) (from []string, to string, target, ok bool) {
	name := base(words[0])
	if copyNames[name] {
		from, to, target = linkArguments(name, words[1:])
		return from, to, target, true
	}
	valued, copies := copyValued[name]
	if !copies {
		return nil, "", false, false
	}
	operands := copyOperands(words[1:], valued)
	if len(operands) < 2 {
		return nil, "", false, false
	}
	return localNames(operands[:len(operands)-1]), operands[len(operands)-1], false, true
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

// copyReading is one reading of what a cp, mv, ln or install copies, and
// where.
type copyReading struct {
	from   []string
	to     string
	target bool
}

// copyReadings are the readings of a cp, mv, ln or install that a rule
// deciding what it fills asks: linkArguments', and bundleTarget's where it
// reads one, whose target is named. Read by linkArguments alone, `cp -rt ..
// /tmp/p/*` took the glob for the destination and `..` for a source, and
// filled nothing above the project, and `cp -pt. <src>/*` filled nothing in
// it. Both readings are asked, which only adds what is filled.
func copyReadings(name string, args []string) []copyReading {
	from, to, target := linkArguments(name, args)
	readings := []copyReading{{from: from, to: to, target: target}}
	if bundled, into, ok := bundleTarget(name, args); ok {
		readings = append(readings, copyReading{from: bundled, to: into, target: true})
	}
	return readings
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
	case "shuf":
		return outputOption(args, "in"), true
	case "split":
		opts, operands := scanOptions(args, "abClnt", splitValued...)
		prefix := "x"
		if len(operands) == 2 {
			prefix = operands[1]
		}
		return suffixed(prefix, lastValue(opts, "", "--additional-suffix")), true
	case "csplit":
		opts, _ := scanOptions(args, "fbn", "--prefix", "--suffix-format", "--digits")
		return suffixed(cmp.Or(lastValue(opts, "f", "--prefix"), "xx"), ""), true
	default:
		return decompressed(name, args)
	}
}

// splitValued are split's long options that take a value, so that one given
// in the next word is not read as the prefix.
var splitValued = []string{
	"--bytes", "--line-bytes", "--lines", "--number", "--suffix-length", "--additional-suffix",
	"--separator", "--filter",
}

// suffixed are the names a split or csplit with this prefix can write: the
// prefix, which a held name given as one is refused as, and any name that
// starts with it and ends with suffix. The program adds the rest, letters,
// digits or a format's, so `split -l 1 x CLAUDE.` wrote neither the prefix
// nor any name it spelt, and was let through where it wrote CLAUDE.md.
func suffixed(prefix, suffix string) []string {
	return []string{prefix, prefix + "?*" + suffix}
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
	return optionValues(args, valued+"o", "o", "--output")
}

// optionValues are the values of the option given as letter, or as a start
// of long, with valued every letter of the program's that takes one.
func optionValues(args []string, valued, letter, long string) []string {
	opts, _ := scanOptions(args, valued, long)
	return valuesOf(opts, letter, long)
}

// lastValue is the value of the last of an option given more than once,
// the one getopt's caller keeps, or "".
func lastValue(opts []option, letter, long string) string {
	values := valuesOf(opts, letter, long)
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// valuesOf are the values opts gives the option named letter or long.
func valuesOf(opts []option, letter, long string) []string {
	var out []string
	for _, o := range opts {
		if (o.name == letter || o.name == long) && o.value != "" {
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

// decompressed is what a compressor or a decompressor writes, and known
// whether name is one of the programs read here. They are named whole, since a
// prefix test misses gunzip and bunzip2. The un- names decompress whatever
// they are given, and the rest where given -d or a start of --decompress or
// --uncompress. Nothing is written where one writes to standard output, tests
// or lists; those long options are matched exactly, so an abbreviation of one
// is read as a decompression that writes, the safe direction. -f is not asked
// for: without it the program does not replace a file that is there, but
// creates one that is not.
//
// A decompression writes each operand without its compressed suffix, and an
// operand with none is itself the name written: gunzip looks for the operand
// with .gz after it and writes the operand, which was read as a word that
// wrote nothing. -N writes the name the header holds, and -r every file below
// a directory, neither of which the words say, so either is read as writing
// any instruction file where it lands. A compression removes each operand once
// it is packed, unless told to keep it, so gzip of a file is the removal rm of
// it is.
//
// The letters that take a value are each program's own, as its manual
// (Apple gzip 487.0.1, xz 5.8, macOS compress, bzip2 1.0.8) names them, -S
// and --suffix the suffix: a letter wrongly read as a flag leaves its value
// as an operand, the safe direction, and one wrongly read as taking a value
// would hide an operand.
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
		return packed(name, args)
	}
	decompress := slices.Contains([]string{"gunzip", "unpigz", "unxz", "unlzma", "bunzip2", "uncompress"}, name)
	opts, operands := scanOptions(args, valued, "--suffix")
	suffix, keep, unnamed := "", false, false
	for _, o := range opts {
		switch o.name {
		case "d":
			decompress = true
		case "c", "t", "l", "--stdout", "--to-stdout", "--test", "--list":
			return nil, true
		case "S", "--suffix":
			suffix = o.value
		case "k", "--keep":
			keep = true
		case "N", "r", "--name", "--recursive":
			unnamed = true
		default:
			decompress = decompress || abbreviates(o.name, "--decompress") || abbreviates(o.name, "--uncompress")
			// xz reads the names it works on from a file the hook does not
			// read, so what it writes could be any file where it runs.
			if strings.HasPrefix(o.name, "--files") {
				operands, unnamed = append(operands, "."), true
			}
		}
	}
	operands = fileOperands(operands)
	if !decompress {
		if keep {
			return nil, true
		}
		return operands, true
	}
	var written []string
	for _, op := range operands {
		w, suffixed := unsuffixed(op, suffix)
		written = append(written, cmp.Or(w, op))
		if unnamed {
			written = append(written, anyInstructionFile(op, suffixed)...)
		}
	}
	return written, true
}

// compressorEnvironment are the variables each compressor reads options from,
// as though they were written first among its words.
var compressorEnvironment = map[string][]string{
	"gzip": {"GZIP"}, "gunzip": {"GZIP"}, "pigz": {"GZIP", "PIGZ"}, "unpigz": {"GZIP", "PIGZ"},
	"bzip2": {"BZIP2", "BZIP"}, "bunzip2": {"BZIP2", "BZIP"},
	"xz": {"XZ_OPT", "XZ_DEFAULTS"}, "unxz": {"XZ_OPT", "XZ_DEFAULTS"},
	"lzma": {"XZ_OPT", "XZ_DEFAULTS"}, "unlzma": {"XZ_OPT", "XZ_DEFAULTS"},
}

// withEnvironmentOptions is a compressor's words with a -d in front of its
// own where a variable it reads options from asks it to decompress: given on
// the line, exported earlier on it, or in the environment the session started
// in. `GZIP=-d gzip -f CLAUDE.md.gz` writes CLAUDE.md, and parse drops the
// assignment, so the reader saw a compression. Only -d is taken, which is the
// refusing direction: a -c from the environment is not trusted to keep the
// write off the disk.
func withEnvironmentOptions(words []string, line string, carried func(string) string) []string {
	if len(words) == 0 {
		return words
	}
	for _, name := range compressorEnvironment[base(words[0])] {
		var values []string
		if carried != nil {
			values = append(values, carried(name))
		}
		for f := range strings.FieldsSeq(line) {
			if v, ok := strings.CutPrefix(f, name+"="); ok {
				values = append(values, v)
			}
		}
		for _, v := range values {
			if asksToDecompress(strings.Fields(strings.Trim(v, `"'`))) {
				return slices.Concat(words[:1], []string{"-d"}, words[1:])
			}
		}
	}
	return words
}

// asksToDecompress reports whether options include -d, in a bundle or as a
// start of --decompress or --uncompress.
func asksToDecompress(options []string) bool {
	for _, o := range options {
		switch {
		case strings.HasPrefix(o, "--"):
			if abbreviates(o, "--decompress") || abbreviates(o, "--uncompress") {
				return true
			}
		case strings.HasPrefix(o, "-") && strings.Contains(o, "d"):
			return true
		}
	}
	return false
}

// packed is decompressed for the newer compressors, whose options differ:
// zstd, brotli and lz4 keep what they pack unless given --rm, zip removes
// what it adds to an archive only with -m, and each names the file it writes
// with -o, or lz4 with a second operand.
func packed(name string, args []string) ([]string, bool) {
	var valued string
	var decompress bool
	switch name {
	case "zstd", "zstdmt", "unzstd":
		valued, decompress = "oD", name == "unzstd"
	case "brotli", "unbrotli":
		valued, decompress = "oqwSD", name == "unbrotli"
	case "lz4", "unlz4":
		decompress = name == "unlz4"
	case "zip":
		return zipWrites(args), true
	default:
		return nil, false
	}
	opts, operands := scanOptions(args, valued, "--output")
	operands = fileOperands(operands)
	removes, multiple := false, false
	var written []string
	for _, o := range opts {
		switch o.name {
		case "c", "t", "l", "--stdout", "--test", "--list":
			return nil, true
		case "d", "--decompress", "--uncompress":
			decompress = true
		case "o", "--output":
			written = append(written, o.value)
		case "m", "--multiple":
			multiple = true
		case "--rm":
			removes = true
		}
	}
	// lz4's second operand is the file it writes, unless every operand is an
	// input.
	if base(name) == "lz4" && !multiple && len(operands) == 2 {
		written, operands = append(written, operands[1]), operands[:1]
	}
	if decompress && len(written) == 0 {
		for _, op := range operands {
			w, _ := unsuffixed(op, "")
			written = append(written, cmp.Or(w, op))
		}
	}
	if removes {
		written = append(written, operands...)
	}
	return written, true
}

// zipWrites are the archive zip writes, its first operand, and with -m or a
// start of --move the files it removes once they are in it. The letters that
// take a value are those zip's manual (Info-ZIP 3.0) gives one; a list after
// -x or -i is read as files, the safe direction.
func zipWrites(args []string) []string {
	opts, operands := scanOptions(args, "bnPZO", "--move")
	operands = fileOperands(operands)
	if len(operands) == 0 {
		return nil
	}
	for _, o := range opts {
		if o.name == "m" || o.name == "--move" {
			return operands
		}
	}
	return operands[:1]
}

// fileOperands are operands without `-`, which is standard input.
func fileOperands(operands []string) []string {
	return slices.DeleteFunc(slices.Clone(operands), func(op string) bool { return op == "-" })
}

// anyInstructionFile is every instruction file a write whose name the words
// do not say could land on: beside op, or below op where op has no compressed
// suffix and so is a directory a recursive run descends.
func anyInstructionFile(op string, suffixed bool) []string {
	dir := clean(op)
	if suffixed {
		dir = path.Dir(dir)
	}
	out := make([]string, 0, len(model.InstructionFiles))
	for _, f := range model.InstructionFiles {
		out = append(out, path.Join(dir, f))
	}
	return out
}

// unsuffixed is the name a decompression gives an operand: without the
// suffix -S gave, or one the programs know, compared folded (`.z` is
// compress's `.Z`), with the tar ones becoming .tar. ok is false for an
// operand with none, which the program leaves alone.
func unsuffixed(op, given string) (string, bool) {
	for _, s := range [][2]string{
		{given, ""}, {".gz", ""}, {"-gz", ""}, {".z", ""}, {"-z", ""}, {"_z", ""}, {".xz", ""}, {".lzma", ""},
		{".bz2", ""}, {".bz", ""}, {".tgz", ".tar"}, {".taz", ".tar"}, {".txz", ".tar"}, {".tlz", ".tar"},
		{".tbz", ".tar"}, {".tbz2", ".tar"}, {".zst", ""}, {".br", ""}, {".lz4", ""},
	} {
		if s[0] != "" && len(op) > len(s[0]) && strings.EqualFold(op[len(op)-len(s[0]):], s[0]) {
			return op[:len(op)-len(s[0])] + s[1], true
		}
	}
	return "", false
}
