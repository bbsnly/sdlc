package cli

// US-108 AC-19, as amended by the person's option C: docs/enforcement.md shall
// say, for each route to a person-only decision it lists, whether AC-15's
// refusal catches it (any process that inherits Claude Code's environment) or
// that it is not caught (a program that starts with a clean environment or
// removes Claude Code's markers), and shall list no route that inherits the
// environment as not caught. The decision of the same day adds that the clean
// starts and the removal of the markers are a residual the page lists as not
// caught.
//
// ANALYSIS.md 7.10 read every not-caught entry in full and named thirteen that
// are routes to a person's decision: eleven bullets of "Commands the rules do
// not read" and the not-caught paragraphs of two rule sections. These tests
// are keyed on those thirteen, and read the rest of that list besides, so a
// route moved to another bullet is still read.
//
// They read sentences, split at a full stop or a semicolon outside code. A
// sentence that names a route to a person's decision is one of two kinds:
//   - caught: it says the route is refused and nothing that lets it through.
//     In a keyed entry it names what catches it: AC-15's SDLC-E0079, or for a
//     nested session or the hook run on a payload, AC-17's AI_AGENT check and
//     its "did not run". It does not claim SDLC-E0079 for a clean start.
//   - not caught: anything else. It gives the reason, a clean environment or
//     the markers removed, and names none of the spellings that inherit the
//     environment.

import (
	"regexp"
	"strings"
	"testing"
)

// decisionRoute matches what names a person-only decision or a route to one:
// the commands, the slash commands, the hook run on a payload, a nested
// `claude`, and the markers AC-15 and AC-17 read.
var decisionRoute = regexp.MustCompile(strings.Join([]string{
	`sdlc (approve|unfreeze|ack|remap|release|parallel|staffing assign)\b`,
	`"sdlc", "`,
	`sdlc \(`,
	`/sdlc:(approve|unfreeze|ack|remap|release|assign)\b`,
	`UserPromptExpansion`,
	`[Pp]erson's (decision|command)`,
	`[Pp]erson-only (decision|command)`,
	`\b(approve|unfreeze) (A-1|US-[0-9]+|--reason)`,
	`make approve`,
	`runs the hook itself`,
	`the hook run that way`,
	`[Rr]unning the hook there`,
	`hook "\$E"`,
	"`claude`",
	`claude -p`,
	`\b(CLAUDECODE|AI_AGENT|CLAUDE_CODE_SESSION_ID|CLAUDE_CODE_CHILD_SESSION|CLAUDE_PID)\b`,
}, "|"))

// namedCatcher is what catches a route that inherits Claude Code's
// environment: AC-15's code, or AC-17's check and what it says.
var namedCatcher = regexp.MustCompile(`SDLC-E0079|\bAI_AGENT\b|\bdid not run\b`)

// refusedWord is a refusal of any kind, which a bullet ANALYSIS.md 7.10 did
// not name may give by a shell rule.
var refusedWord = regexp.MustCompile(`\brefuse[sd]?\b`)

// letThrough matches a sentence that says the route is not stopped.
var letThrough = regexp.MustCompile(`(?i)not refused|not caught|\b(is|are) not read\b|not read for|\b(gets?|got) (past|through)\b|\b(is|are) allowed\b|\bnot seen\b|never sees|\bnor (is|are|anything)\b|no rule applied|\bescapes?\b|\bmisse[sd]\b`)

// cleanReason is the reason AC-19 allows for a route not caught: a program
// that starts with a clean environment, or one that removes the markers.
var cleanReason = regexp.MustCompile(`(?i)\bclean (environment|start)\b|\bstarts? (with a )?clean\b|empty environment|environment of its own|new environment|UseNewEnvironment|\benv -i\b|` +
	`\b(remov|unset|clear|blank|overrid)\w*\b[^.;]*\b(markers?|CLAUDECODE|AI_AGENT|CLAUDE_CODE_SESSION_ID|CLAUDE_CODE_CHILD_SESSION|CLAUDE_PID)\b|` +
	`\bmarkers? (removed|cleared|unset|blanked)\b`)

// inherits matches the spellings that run in a process inheriting Claude
// Code's environment: those AC-19 lists, and those ANALYSIS.md 7.10 cites in
// the thirteen entries. A sentence that lists one of them as not caught is
// what AC-19 forbids, whatever reason it gives.
var inherits = regexp.MustCompile(strings.Join([]string{
	`(?i)\binherit`,
	"`arch\\b", `do shell script`, `\bsu (-c|[a-z]+ -c)\b`, `\bvim -c\b`, "`awk\\b", `\bsystem\(`,
	`core\.pager`, `core\.editor`, `GIT_EDITOR`, `Invoke-Expression`, "`iex\\b", `\biex '`,
	`sdlc \(`, `hook \(`,
	`\bnice -n\b`, `\btimeout -k\b`, `\bstdbuf\b`, `\benv -S\b`, `\bnohup\b`, `\bexec -a\b`, `\bcommand -p\b`, `\bxargs -I\b`, `\bscript -q\b`,
	`\| ?(sh|bash|zsh|dash|python3?|perl|ruby|node|pwsh|powershell)\b`, `\b(sh|bash|python3?|perl|ruby|node) (<<<|-s\b)`,
	`\bfish -c\b`, `make approve`, `\bstrace\b`, `script file`, `alias\.x=`, `\balias s=`, `\$SDLC_BIN`, `command -v sdlc`, `/tmp/s approve`,
	`subprocess\.run`, `python3 -c`, `/tmp/x hook`, `cd "\$\(cat`,
	`"cl" \+ "aude"`, `base64`, `--check-hash-based-pycs`, `ruby --disable`, `node --title`, `versions/`, "`cla\\*`",
}, "|"))

// cleanStart matches the programs the person's decision names as starting
// with a clean environment. SDLC-E0079 cannot catch them.
var cleanStart = regexp.MustCompile(`launchctl submit|` + "`open\\b|`at\\b|" + `tell application "Terminal"|\bsu -[ ` + "`" + `]|sudo -i|\bssh\b|schtasks|Win32_Process|Invoke-CimMethod|UseNewEnvironment|\benv -i\b|\bexec -c\b`)

// notCaughtEntry is one of the thirteen of ANALYSIS.md 7.10, found by the
// bullet's bold leader in "Commands the rules do not read", or by its rule
// section and the text that starts it there.
type notCaughtEntry struct {
	n int
	// lines is where ANALYSIS.md 7.10 cites it, in docs/enforcement.md as it
	// was read.
	lines string
	// leader is the bullet's bold leader; section and starts find the others.
	leader, section string
	starts          []string
	// mustSay is an entry whose text today names no decision outright, so
	// leaving it as it is would pass: ANALYSIS.md 7.10 says it is a route to
	// one, so it is to say whether the route is caught.
	mustSay bool
}

var ac19Entries = []notCaughtEntry{
	{n: 1, lines: "2324-2330", leader: "A call the hook is killed on before it knows whether it checks it", mustSay: true},
	{n: 2, lines: "2340-2349", leader: "What the persistent shell keeps"},
	{n: 3, lines: "2350-2361", leader: "A program the rules do not read through"},
	{n: 4, lines: "2362-2376", leader: "A command line for another kind of shell", mustSay: true},
	{n: 5, lines: "2428-2444", leader: "Words or a script from standard input, or from a file"},
	{n: 6, lines: "2451-2461", leader: "The shell command a `!` alias runs"},
	{n: 7, lines: "2462-2469", leader: "`sdlc` reached under another name"},
	{n: 8, lines: "2470-2478", leader: "A person's decision run by code or from a string"},
	{n: 9, lines: "2479-2484", leader: "The hook under another name, on an event it cannot read"},
	{n: 10, lines: "2485-2491", leader: "A PowerShell expression in parentheses"},
	{n: 11, lines: "2492-2497", leader: "A session in a project that does not take part"},
	{n: 12, lines: "1528-1535", section: "### `slash-command-is-a-human-decision`", starts: []string{"Not caught"}},
	{n: 13, lines: "1866-1872 and 1908-1940", section: "### `story-stays-in-its-session`",
		starts: []string{"A Claude Code session started from a shell", "Not caught"}},
}

func (e notCaughtEntry) name() string {
	if e.leader != "" {
		return e.leader
	}
	return e.section + " (not caught)"
}

// routeFault is what is wrong with a sentence that names a route to a
// person's decision, or "" where it says whether the route is caught as
// AC-19 asks. keyed is a sentence of one of the thirteen entries, which
// names what catches it.
func routeFault(s string, keyed bool) string {
	if !decisionRoute.MatchString(s) {
		return ""
	}
	caught := saysCaught(s)
	switch {
	case caught && keyed && !namedCatcher.MatchString(s):
		return "says the route is refused without saying that AC-15's refusal (SDLC-E0079) or AC-17's check catches it"
	case caught && strings.Contains(s, "SDLC-E0079") && cleanStart.MatchString(s):
		return "says SDLC-E0079 catches a program that starts with a clean environment"
	case caught:
		return ""
	case inherits.MatchString(s):
		return "lists a route that inherits Claude Code's environment as not caught"
	case !cleanReason.MatchString(s):
		return "lists a route as not caught without the reason AC-19 allows, a clean environment or the markers removed"
	}
	return ""
}

// saysCaught reports whether a sentence says its route is refused, and says
// nothing that lets it through.
func saysCaught(s string) bool {
	return !letThrough.MatchString(s) && (namedCatcher.MatchString(s) || refusedWord.MatchString(s))
}

func TestAC19EnforcementDocSaysForEachRouteToAPersonOnlyDecisionWhetherAC15CatchesIt(t *testing.T) {
	doc := page(t, "enforcement.md")
	list, ok := docSection(doc, "#### Commands the rules do not read")
	if !ok {
		t.Fatal(`AC-19: docs/enforcement.md has no "#### Commands the rules do not read"; this test reads it for the routes it lists, so it must be told where they went`)
	}
	bullets := docBullets(list)
	keyed := map[string]bool{}
	for _, e := range ac19Entries {
		texts := e.texts(t, doc, bullets)
		if len(texts) == 0 {
			if e.mustSay {
				t.Errorf("AC-19: entry %d (%q, docs/enforcement.md:%s when ANALYSIS.md 7.10 read it) is gone; it holds more than the decision, and is to say whether the decision is caught",
					e.n, e.name(), e.lines)
			}
			continue
		}
		says := false
		for _, text := range texts {
			keyed[text] = true
			for _, s := range docSentences(text) {
				if fault := routeFault(s, true); fault != "" {
					t.Errorf("AC-19: entry %d (%q, docs/enforcement.md:%s when ANALYSIS.md 7.10 read it) %s: %q",
						e.n, e.name(), e.lines, fault, clip(s))
				} else if decisionRoute.MatchString(s) {
					says = true
				}
			}
		}
		if e.mustSay && !says {
			t.Errorf("AC-19: entry %d (%q, docs/enforcement.md:%s when ANALYSIS.md 7.10 read it) does not say whether the person's decision it lets through is caught by AC-15's refusal (SDLC-E0079) or not caught, and why",
				e.n, e.name(), e.lines)
		}
	}
	// A route moved out of a keyed bullet into another one of the list is
	// still a route the list gives.
	for _, b := range bullets {
		if keyed[b] {
			continue
		}
		for _, s := range docSentences(b) {
			if fault := routeFault(s, false); fault != "" {
				t.Errorf("AC-19: a bullet of \"Commands the rules do not read\" %s: %q", fault, clip(s))
			}
		}
	}
}

// The decision of option C names the programs that start with a clean
// environment, and the removal of the markers, as the residual the page lists
// as not caught. Each is to be there, under Not held, in a sentence that says
// a person's decision run that way is not caught and why.
func TestAC19EnforcementDocListsTheCleanStartsAndTheMarkersRemovedAsNotCaught(t *testing.T) {
	doc := page(t, "enforcement.md")
	notHeld, ok := docSection(doc, "### Not held")
	if !ok {
		t.Fatal(`AC-19: docs/enforcement.md has no "### Not held", where the residual is listed`)
	}
	var residual []string
	for _, s := range docSentences(notHeld) {
		if decisionRoute.MatchString(s) && cleanReason.MatchString(s) && !saysCaught(s) && routeFault(s, false) == "" {
			residual = append(residual, s)
		}
	}
	said := strings.Join(residual, " ")
	for _, c := range []struct{ name, pattern string }{
		{"Claude Code's markers removed", `(?i)\b(remov|unset|clear|blank)\w*\b[^.;]*\b(markers?|CLAUDECODE|AI_AGENT)\b|\bmarkers? (removed|cleared|unset|blanked)\b`},
		{"launchctl submit", `launchctl submit`},
		{"open", "`open\\b"},
		{"at", "`at\\b"},
		{"Terminal by AppleEvents", `Terminal`},
		{"a running tmux server", `tmux`},
		{"su -", `\bsu -[ ` + "`" + `]`},
		{"sudo -i", `sudo -i`},
		{"ssh", `\bssh\b`},
		{"schtasks", `schtasks`},
		{"WMI", `WMI|Win32_Process|Invoke-CimMethod`},
		{"Start-Process -UseNewEnvironment", `UseNewEnvironment`},
	} {
		if !regexp.MustCompile(c.pattern).MatchString(said) {
			t.Errorf("AC-19: Not held does not list %s as a route to a person's decision that is not caught, with the reason (a clean environment or the markers removed)", c.name)
		}
	}
}

// texts is the entry's text in doc: its bullet, or in its rule section each
// part that starts with one of starts, to the end of that paragraph or, for
// "Not caught", to the end of the section.
func (e notCaughtEntry) texts(t *testing.T, doc string, bullets []string) []string {
	t.Helper()
	if e.leader != "" {
		for _, b := range bullets {
			if strings.HasPrefix(b, "- **"+e.leader) {
				return []string{b}
			}
		}
		return nil
	}
	body, ok := docSection(doc, e.section)
	if !ok {
		t.Fatalf("AC-19: docs/enforcement.md has no %s, which entry %d is in", e.section, e.n)
	}
	var out []string
	for _, start := range e.starts {
		i := strings.Index(body, start)
		if i < 0 {
			continue
		}
		part := body[i:]
		if start != "Not caught" {
			if end := strings.Index(part, "\n\n"); end >= 0 {
				part = part[:end]
			}
		}
		out = append(out, part)
	}
	return out
}

// docSection is the text under heading, to the next heading of its level or
// above, with fenced code left out of the search for headings.
func docSection(doc, heading string) (string, bool) {
	level := strings.Index(heading, " ")
	lines := strings.Split(doc, "\n")
	start := -1
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if start < 0 {
			if strings.TrimRight(line, " ") == heading {
				start = i + 1
			}
			continue
		}
		if hashes := len(line) - len(strings.TrimLeft(line, "#")); hashes > 0 && hashes <= level && strings.HasPrefix(line[hashes:], " ") {
			return strings.Join(lines[start:i], "\n"), true
		}
	}
	if start < 0 {
		return "", false
	}
	return strings.Join(lines[start:], "\n"), true
}

// docBullets is each top-level bullet of a list, its continuation lines
// joined to it.
func docBullets(section string) []string {
	var out []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for line := range strings.SplitSeq(section, "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			flush()
			cur = []string{line}
		case len(cur) > 0 && (strings.HasPrefix(line, " ") || line == ""):
			if line == "" {
				flush()
				continue
			}
			cur = append(cur, line)
		default:
			flush()
		}
	}
	flush()
	return out
}

// docSentences splits Markdown prose into sentences: at a full stop or a
// semicolon outside code that ends a word, with emphasis taken out so that a
// bold leader ends its own sentence. A semicolon ends one so that a caught
// route and a route not caught can share a line of the page.
func docSentences(text string) []string {
	text = strings.Join(strings.Fields(strings.ReplaceAll(text, "**", "")), " ")
	var out []string
	code := false
	begin := 0
	for i := range len(text) {
		switch text[i] {
		case '`':
			code = !code
		case '.', ';':
			if !code && (i+1 == len(text) || text[i+1] == ' ') {
				out = append(out, strings.TrimSpace(text[begin:i+1]))
				begin = i + 1
			}
		}
	}
	if rest := strings.TrimSpace(text[begin:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

// clip keeps a quoted sentence short enough to read in a failure.
func clip(s string) string {
	const most = 240
	if len(s) <= most {
		return s
	}
	return s[:most] + "..."
}
