package shellpolicy

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// maxAssignedGrowth is how much longer than it was written a line may be read.
const maxAssignedGrowth = 1 << 16

// assignmentBefore is what may stand before an assignment's name: what CDPATH's
// reader takes, and the backtick that opens a substitution.
const assignmentBefore = cdpathBefore + "`"

// inspectAssigned asks the rules about the line with each variable it assigns
// read as its value, after the line as written, so a command refused before
// keeps its reason. `T=<frozen>; echo w > "$T"` was a way past the freeze in one
// assignment (architect, US-115 Gate 4). Any assignment on the line counts, one
// in a branch or a subshell and one a later assignment replaces, because a
// reading that may be wrong refuses. This can only add a refusal.
//
// A worklist, deduplicated and capped, as readings.go is and for its reason: a
// value can hold another variable, `T=x; U=$T`, and a long line of values or of
// uses makes a reading too big to read in the hook's time. Both bounds refuse: a
// line near neither is what a person writes.
func inspectAssigned(command string, s State) (Finding, bool) {
	// A comment ends at its newline whatever comes before it, so a line with one
	// is read in addCommentReadings' spellings as well as joined.
	for _, spelled := range commentLines(command) {
		for _, line := range commentReadings(spelled) {
			if f, refused := inspectAssignedLine(line, s); refused {
				return f, true
			}
		}
	}
	// Joined once for every reading: removed only where values were read, a
	// continuation between the `$` and the name of a use left `rm "$\<newline>T"`
	// unread, which the shell runs as `rm "$T"` (security, US-115 Gate 7).
	return inspectAssignedLine(strings.ReplaceAll(command, "\\\n", ""), s)
}

// inspectAssignedLine is inspectAssigned for one spelling of the line.
func inspectAssignedLine(command string, s State) (Finding, bool) {
	if !strings.Contains(command, "$") || !strings.Contains(command, "=") {
		return Finding{}, false
	}
	limit := len(command) + maxAssignedGrowth
	asked := []string{command}
	for i := 0; i < len(asked); i++ {
		values := assignedValues(asked[i])
		for j := range mostValues(values) {
			line := withValues(asked[i], values, j, limit)
			if slices.Contains(asked, line) {
				continue
			}
			if len(asked) > maxReadings || len(line) > limit {
				return tooManyAssigned(len(line) > limit), true
			}
			asked = append(asked, line)
			s.seen = newInspection()
			s.seen.line = line
			if f, refused := inspectNamed(line, s); refused {
				return readAsAssigned(f, asked[i], values, j), true
			}
		}
	}
	return Finding{}, false
}

// tooManyAssigned refuses a line read more than maxReadings ways by the values
// it assigns, or tooLong, made more than maxAssignedGrowth longer by one: a
// single value used many times does that in one reading. A document can hit
// either, and cannot be split, so the route gives the way to hand one over that
// leaves it out of the line.
func tooManyAssigned(tooLong bool) Finding {
	why := "the variables this line assigns give it more than " + strconv.Itoa(maxReadings) + " readings"
	if tooLong {
		why = "the values this line assigns make it more than " + strconv.Itoa(maxAssignedGrowth>>10) + " KB longer than it is written"
	}
	return Finding{
		Rule:   "command-too-deep-to-read",
		Reason: why + ", and past that what it writes was not checked",
		Route: "split the line into separate commands, in separate calls; a document goes in a file, which " +
			"`sdlc artifact write` and `sdlc review add` read with `--file`",
	}
}

// readAsAssigned says which assignment a refusal came from: the path may be
// nowhere in the command as written, so a reason that names only the path does
// not say what to change.
func readAsAssigned(f Finding, line string, values map[string][]string, i int) Finding {
	var used []string
	for name, v := range values {
		if strings.Contains(line, "$"+name) || strings.Contains(line, "${"+name) {
			used = append(used, "$"+name+" to "+shown(shortened(v[min(i, len(v)-1)])))
		}
	}
	sort.Strings(used)
	f.Reason += "; this line sets " + strings.Join(used, ", ") + " and uses it, so it was read with that value where it is used"
	f.Route += "; where the line only mentions the value, take the assignment or the use out of it"
	return f
}

// mostValues is how many values the name with the most has.
func mostValues(values map[string][]string) int {
	most := 0
	for _, v := range values {
		most = max(most, len(v))
	}
	return most
}

// assignedValues is each name the line assigns, with the values it gives it. A
// word of the shape anywhere counts, since `export`, `env`, `declare` and the
// like take it as an operand. A value only the running line knows is left out,
// and a name stops collecting past maxReadings values, which is already a
// refusal: built first and counted after, 16,000 values cost 5.9 GB. A value
// given again moves to the end, where the shell has it last: kept where it was
// first given, `T=a; T=P1; U=P2; U=x; U=P2; rm "$T$U"` never read P1 with P2.
func assignedValues(command string) map[string][]string {
	values := map[string][]string{}
	for i := 0; i < len(command); i++ {
		if !nameByte(command[i]) || '0' <= command[i] && command[i] <= '9' || i > 0 && strings.IndexByte(assignmentBefore, command[i-1]) < 0 {
			continue
		}
		end := nameRunEnd(command, i)
		if end == len(command) || command[end] != '=' {
			continue
		}
		v, next, known := valueOnLine(command, end+1)
		name := command[i:end]
		if known && len(values[name]) <= maxReadings {
			values[name] = append(slices.DeleteFunc(values[name], func(seen string) bool { return seen == v }), v)
		}
		i = next - 1
	}
	return values
}

// nameRunEnd is where the run of a name's characters at from stops.
func nameRunEnd(command string, from int) int {
	for from < len(command) && nameByte(command[from]) {
		from++
	}
	return from
}

// valueOnLine reads the word at from as the shell assigns it, with its quotes
// gone, and where it ends. assignedValue, which reads CDPATH's, stops at a word
// that runs on past a quote; here `T=REV'IEW'.md` is still a value the line holds.
// `$'...'` and `$"..."` are read as their quotes, with no escape decoded. A
// backtick ends the word, so what the substitution assigns is read as well, and
// the value before one is not known.
func valueOnLine(command string, from int) (value string, end int, known bool) {
	var b strings.Builder
	var quote byte
	known = true
	for end = from; end < len(command); end++ {
		c := command[end]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == '\'':
			b.WriteByte(c)
			known = known && c != '$'
		case c == '$' && quote == 0 && end+1 < len(command) && (command[end+1] == '\'' || command[end+1] == '"'):
		case c == '$' || c == '`' && quote != 0:
			known = false
		case c == '\\' && end+1 < len(command):
			end++
			b.WriteByte(command[end])
		case quote == '"':
			b.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
		case strings.IndexByte(wordEnds+"`", c) >= 0:
			return b.String(), end, known && b.Len() > 0 && c != '`'
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), end, known && b.Len() > 0
}

// withValues is the line with `$NAME` and `${NAME...}` read as the name's i-th
// value, or its last where it has fewer. The rest of a brace is dropped: read
// as the value whole, `${T:-x}` and `${T%.bak}` are still the path they may be.
// It stops once past limit, and the caller refuses what comes back that long.
func withValues(command string, values map[string][]string, i, limit int) string {
	var b strings.Builder
	for at := 0; at < len(command) && b.Len() <= limit; at++ {
		if command[at] != '$' {
			b.WriteByte(command[at])
			continue
		}
		start, brace := at+1, -1
		if start < len(command) && command[start] == '{' {
			start++
			brace = strings.IndexByte(command[start:], '}')
		}
		end := nameRunEnd(command, start)
		v, known := values[command[start:end]]
		if !known || start == at+2 && brace < 0 {
			b.WriteByte('$')
			continue
		}
		b.WriteString(v[min(i, len(v)-1)])
		if start == at+2 {
			end = start + brace + 1
		}
		at = end - 1
	}
	return b.String()
}
