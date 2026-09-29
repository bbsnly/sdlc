package shellpolicy

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// decoded is a POSIX command as the shell reads two spellings the other
// readings leave as written: `$'...'`, whose escapes the shell decodes, and a
// backslash before an ordinary character outside quotes, which the shell takes
// out. `git $'worktree' prune`, `g\it worktree prune` and `git com\mit` were
// read as programs and subcommands nobody had heard of, and ran as the ones
// they spell.
//
// It is a reading of its own, added to the others and never in place of one:
// the released reading is kept as it was, so that the readings together can
// only add refusals (see readings). A backslash is kept where taking it out
// would change more than the word it is in: before white space, a separator,
// a quote, `$`, a backtick, `#`, a glob character, a brace, `~` and a newline.
//
// Quoting is followed as far as telling these spellings apart needs, and no
// further: a single-quoted string is copied as it is, a double-quoted one
// keeps its backslashes as the shell does, and a comment is copied to the end
// of its line.
func decoded(command string) string {
	if !strings.ContainsAny(command, `\$`) {
		return command
	}
	var b strings.Builder
	b.Grow(len(command))
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\\' && i+1 < len(command):
			if strings.IndexByte(" \t\r\n;&|<>()'\"$`#*?[{}~\\", command[i+1]) >= 0 {
				b.WriteString(command[i : i+2])
			} else {
				b.WriteByte(command[i+1])
			}
			i++
		case c == '\'':
			end := strings.IndexByte(command[i+1:], '\'')
			if end < 0 {
				b.WriteString(command[i:])
				return b.String()
			}
			b.WriteString(command[i : i+end+2])
			i += end + 1
		case c == '"':
			i = copyDoubleQuoted(&b, command, i)
		case c == '$' && i+1 < len(command) && command[i+1] == '\'':
			value, end := ansiC(command, i+2)
			b.WriteString(quoted(value))
			i = end
		case c == '$' && i+1 < len(command) && command[i+1] == '"':
			// A string translated for the locale is, untranslated, the string.
			i = copyDoubleQuoted(&b, command, i+1)
		case c == '#' && (i == 0 || strings.IndexByte(" \t\r\n;&|()", command[i-1]) >= 0):
			end := strings.IndexByte(command[i:], '\n')
			if end < 0 {
				b.WriteString(command[i:])
				return b.String()
			}
			b.WriteString(command[i : i+end])
			i += end - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// copyDoubleQuoted copies a double-quoted string that opens at i, as it is,
// and returns where it closes.
func copyDoubleQuoted(b *strings.Builder, command string, i int) int {
	b.WriteByte('"')
	for i++; i < len(command); i++ {
		c := command[i]
		b.WriteByte(c)
		switch {
		case c == '\\' && i+1 < len(command):
			i++
			b.WriteByte(command[i])
		case c == '"':
			return i
		}
	}
	return i
}

// ansiC decodes a `$'...'` string from just after its opening quote, as bash
// does, and returns it and the index of its closing quote.
func ansiC(command string, i int) (string, int) {
	var b strings.Builder
	for ; i < len(command); i++ {
		c := command[i]
		if c == '\'' {
			return b.String(), i
		}
		if c != '\\' || i+1 == len(command) {
			b.WriteByte(c)
			continue
		}
		i++
		switch e := command[i]; e {
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'e', 'E':
			b.WriteByte(0x1b)
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '\\', '\'', '"', '?':
			b.WriteByte(e)
		case 'c':
			// A control character: \cA is 1. \c? is 0x1f here, as bash 3.2
			// makes it; bash 5 makes it DEL. Neither names a program or a path.
			if i+1 < len(command) {
				i++
				b.WriteByte(command[i] & 0x1f)
			}
		case 'x', 'u', 'U':
			width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
			n, used := digits(command[i+1:], width, 16)
			if used == 0 {
				b.WriteString(`\` + string(e))
				continue
			}
			i += used
			if e == 'x' {
				b.WriteByte(byte(n))
			} else if utf8.ValidRune(rune(n)) {
				b.WriteRune(rune(n))
			}
		case '0', '1', '2', '3', '4', '5', '6', '7':
			n, used := digits(command[i:], 3, 8)
			i += used - 1
			b.WriteByte(byte(n))
		default:
			b.WriteString(`\` + string(e))
		}
	}
	return b.String(), i
}

// digits reads up to width digits of base from the front of s.
func digits(s string, width, base int) (int, int) {
	used := 0
	for used < width && used < len(s) {
		if _, err := strconv.ParseInt(s[used:used+1], base, 8); err != nil {
			break
		}
		used++
	}
	if used == 0 {
		return 0, 0
	}
	n, _ := strconv.ParseInt(s[:used], base, 64)
	return int(n), used
}

// quoted is a decoded string written back as one quoted word the lexer and the
// path rules read as that string. A NUL ends it, as it ends the string the
// shell hands a program.
func quoted(value string) string {
	value, _, _ = strings.Cut(value, "\x00")
	if !strings.Contains(value, "'") {
		return "'" + value + "'"
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(value) + `"`
}

// bracesExpanded is a POSIX command as bash reads it once it has expanded the
// brace lists in its words: `git {worktree,} prune` is `git worktree prune`,
// with the empty word dropped as bash drops it, and `{git,commit,-m,x}` is
// four words. The lexer ends a command at a brace, which is how PowerShell and
// a `{ ...; }` group read one, so to the other readings git's subcommand was
// `worktree,`, which git has never heard of, and the command ran the one it
// spells.
//
// A list is expanded inside quotes as well, where bash leaves it: a script
// handed to `bash -c` expands it when it runs, and a reading can only add
// refusals. A word with more lists than maxBraces expands is kept whole and
// quoted, so the rules read it as the one word they cannot finish.
func bracesExpanded(command string) string {
	if !strings.Contains(command, "{") {
		return command
	}
	var b strings.Builder
	b.Grow(len(command))
	// open is the quotes and substitutions the scan is inside, innermost
	// last: a word ends only at white space or a separator outside them all.
	var open []byte
	start := 0
	for i := 0; i < len(command); i++ {
		c, top := command[i], byte(0)
		if len(open) > 0 {
			top = open[len(open)-1]
		}
		switch {
		case top == '\'':
			if c == '\'' {
				open = open[:len(open)-1]
			}
		case c == '\\':
			i++
		case top == '"' && c == '"', top == '`' && c == '`', top == '(' && c == ')', top == '{' && c == '}':
			open = open[:len(open)-1]
		case c == '$' && i+1 < len(command) && (command[i+1] == '(' || command[i+1] == '{'):
			i++
			open = append(open, command[i])
		case top == '"':
		case c == '\'' || c == '"' || c == '`' || top == '(' && c == '(':
			open = append(open, c)
		case top == 0 && strings.IndexByte(" \t\r\n;&|<>()", c) >= 0:
			b.WriteString(expandedWord(command[start:i]))
			b.WriteByte(c)
			start = i + 1
		}
	}
	b.WriteString(expandedWord(command[min(start, len(command)):]))
	return b.String()
}

// expandedWord is one word with its brace lists expanded, as the words bash
// makes of it.
func expandedWord(w string) string {
	if open, _, _ := braceGroup(w); open < 0 {
		return w
	}
	var kept []string
	for _, e := range braces(w) {
		if open, _, _ := braceGroup(e); open >= 0 {
			return quoted(w)
		}
		if e != "" {
			kept = append(kept, e)
		}
	}
	return strings.Join(kept, " ")
}
