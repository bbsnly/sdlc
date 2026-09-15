package shellpolicy

import "strings"

// writtenTo are the words of a command that name what it writes. For most
// programs that is every word, which is how the rules have always read them.
// An archiver or a copy to elsewhere is given much that it only reads: `tar
// --exclude=.git -cf - .` writes to standard output, and `rsync -a --exclude
// .sdlc ./ /tmp/snap/` writes only to /tmp/snap.
func writtenTo(words []string) []string {
	args := words[1:]
	switch base(words[0]) {
	case "tar":
		return tarWrites(args)
	case "rsync":
		return copyWrites(args, rsyncValued, "--log-file", "--write-batch", "--only-write-batch")
	case "scp":
		return copyWrites(args, scpValued)
	}
	return args
}

// tarWrites are what tar writes: the archive it creates, with an incremental
// archive's snapshot file, or where it extracts and the members it is asked for.
func tarWrites(args []string) []string {
	var archives, into, operands []string
	extract := false
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
	for next < len(args) {
		a := args[next]
		next++
		switch {
		case a == "--extract" || a == "--get":
			extract = true
		case a == "--file" || a == "--listed-incremental":
			archives = append(archives, value(""))
		case a == "--directory":
			into = append(into, value(""))
		case a == "--exclude" || a == "--exclude-from" || a == "--files-from":
			value("")
		case strings.HasPrefix(a, "--file=") || strings.HasPrefix(a, "--listed-incremental="):
			_, v, _ := strings.Cut(a, "=")
			archives = append(archives, v)
		case strings.HasPrefix(a, "--directory="):
			into = append(into, strings.TrimPrefix(a, "--directory="))
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
					into = append(into, value(bundle[i+1:]))
					i = len(bundle)
				case 'T', 'X':
					value(bundle[i+1:])
					i = len(bundle)
				}
			}
		default:
			operands = append(operands, a)
		}
	}
	if extract {
		return append(into, operands...)
	}
	return archives
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
}

var scpValued = map[string]bool{
	"-i": true, "-P": true, "-o": true, "-F": true, "-c": true, "-l": true, "-S": true, "-J": true,
}

// copyWrites is the destination of a copy, its last operand, and the values of
// the options named in writes, which are files it writes as well.
func copyWrites(args []string, valued map[string]bool, writes ...string) []string {
	var operands, written []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, v, glued := strings.Cut(a, "=")
		for _, w := range writes {
			if name == w {
				if !glued && i+1 < len(args) {
					v = args[i+1]
				}
				written = append(written, v)
			}
		}
		switch {
		case valued[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			operands = append(operands, a)
		}
	}
	// A single operand is a listing, not a copy.
	if len(operands) > 1 {
		written = append(written, operands[len(operands)-1])
	}
	return written
}
