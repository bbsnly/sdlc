package shellpolicy

import (
	"path"
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
		if !hasWord(args, "-delete") && !runsAWriter(args) {
			return printedTo(args)
		}
	case "cp", "copy", "copy-item", "cpi", "mv", "move", "move-item", "mi":
		return append(args, landing(args)...)
	case "curl":
		return curlWrites(args)
	case "wget":
		return wgetWrites(args)
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
	// value is the value of an option: the rest of its bundle, or the next word,
	// which old-style bundles such as `tar cvfC a.tar dir` hand out in the order
	// their letters are written.
	next := 0
	value := func(rest string) string {
		if rest == "" && next < len(args) {
			next++
			return args[next-1]
		}
		return rest
	}
	// changeTo is -C: where tar extracts, and where the operands after it are.
	dir := ""
	changeTo := func(to string) {
		into = append(into, to)
		dir = to
	}
	for next < len(args) {
		a := args[next]
		next++
		switch {
		case a == "--extract" || a == "--get":
			extract = true
		case a == "--file" || a == "--listed-incremental":
			archives = append(archives, value(""))
		case a == "--directory":
			changeTo(value(""))
		case a == "--exclude" || a == "--exclude-from" || a == "--files-from":
			value("")
		case strings.HasPrefix(a, "--file=") || strings.HasPrefix(a, "--listed-incremental="):
			_, v, _ := strings.Cut(a, "=")
			archives = append(archives, v)
		case strings.HasPrefix(a, "--directory="):
			changeTo(strings.TrimPrefix(a, "--directory="))
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") || next == 1:
			// The first word is options whether or not it starts with a dash.
			bundle := strings.TrimPrefix(a, "-")
			for i := 0; i < len(bundle); i++ {
				switch bundle[i] {
				case 'x':
					extract = true
				case 'f', 'g':
					archives = append(archives, value(bundle[i+1:]))
					i = len(bundle)
				case 'C':
					changeTo(value(bundle[i+1:]))
					i = len(bundle)
				case 'T', 'X':
					value(bundle[i+1:])
					i = len(bundle)
				}
			}
		default:
			if dir != "" {
				a = path.Join(dir, a)
			}
			operands = append(operands, a)
		}
	}
	return archives, into, operands, extract
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
	// A single operand is a listing, not a copy.
	if operands := copyOperands(args, valued); len(operands) > 1 {
		to := operands[len(operands)-1]
		written = append(written, to)
		// Into a directory, a file lands under its own name: `scp
		// host:proj/CLAUDE.md .` writes CLAUDE.md. Not on another machine.
		if !remote(to) && intoADirectory(to, len(operands)-1) {
			for _, from := range operands[:len(operands)-1] {
				name := clean(from)
				if remote(from) {
					_, name, _ = strings.Cut(name, ":")
				}
				written = append(written, path.Join(to, path.Base(name)))
			}
		}
	}
	return written
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

// landing are the names a cp or mv writes in the directory it copies or moves
// into, as a copy's: `cp /tmp/snap/CLAUDE.md .` writes CLAUDE.md. A directory given
// as an option is one whatever its name.
func landing(args []string) []string {
	from, to, target := moveArguments(args)
	if !target && !intoADirectory(to, len(from)) {
		return nil
	}
	names := make([]string, 0, len(from))
	for _, f := range from {
		names = append(names, path.Join(to, path.Base(clean(f))))
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
