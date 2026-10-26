package shellpolicy

// US-023: AGENTS.md is held as CLAUDE.md is, a project's .agents as its .claude
// is, and the person's ~/.agents as ~/.claude is.
//
// AC-5 asks for no new list of evasions: every row in this package that refuses
// a spelling of CLAUDE.md has to refuse the same spelling of AGENTS.md. So the
// rows are not copied here. The harness every table goes through (refused,
// allowed, refusedUnder, allowedGlob, the corpus checks and the outside-the-
// repository loops) asks each row a second time with the name respelt, as a
// subtest named "US-023 AC-5", and a row refused for writing .claude a third
// time with .agents, as a subtest named "US-023 AC-11". AC-12 holds
// CLAUDE.local.md as CLAUDE.md is, so every such row is asked once more with
// the name respelt CLAUDE.local.md, as a subtest named "US-023 AC-12". A row
// that is let through is respelt too, as AGENTS.md and as CLAUDE.local.md, so
// that refusing every mention of either is not a way to pass. The state is respelt with the row: what the disk says a word is
// (Resolve, Shared, Outside) is asked of the name the row was written with and
// answered in the name it is asked in.
//
// The rows that are new are the ones the story names: the first-match gap the
// person's decision 7 fixes, reproduced as `echo x > a/.claudex/.claude/s` and
// tried for every protected name, the writes Gate 2 reproduced as let through,
// and ~/.agents built from the ~/.claude rows of US-108.

import (
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// agAgentsRule is the rule the person's ~/.agents is refused under, from the
// shell and from the file tools: its own id, built like
// claude-config-is-the-persons (the story's second decision, answer 1).
const agAgentsRule = "agents-config-is-the-persons"

// agProtected is the rule a project's protected paths are refused under.
const agProtected = "protected-path-through-the-tool"

// agRespell rewrites each spelling of one six-letter name in s as the same
// spelling of another, letter for letter and case for case: CLAUDE.md as
// AGENTS.md, `Claude.MD` as `Agents.MD`, `[C]LAUDE.md` as `[A]GENTS.md`,
// `'CLAUDE'.md` as `'AGENTS'.md`, and the short name CLAUDE~1 as AGENTS~1. A
// name counts only where `.m`, `.{m` or `~1` follows it, after at most one
// quote, so that Claude Code, `claude -p` and ~/.claude are left as they are.
// changed is false where nothing was rewritten.
func agRespell(s, from, to string) (out string, changed bool) {
	lower := strings.ToLower(s)
	head, tail := from[0], from[1:]
	toHead, toTail := to[0], to[1:]
	b := []byte(s)
	for i := 1; i+len(tail) <= len(lower); i++ {
		if lower[i:i+len(tail)] != tail || !agNamesAFile(lower[i+len(tail):]) {
			continue
		}
		switch {
		case lower[i-1] == head:
			b[i-1] = agCased(b[i-1], toHead)
		case s[i-1] == ']':
			open := strings.LastIndexByte(s[:i-1], '[')
			if open < 0 {
				continue
			}
			for j := open + 1; j < i-1; j++ {
				if lower[j] == head {
					b[j] = agCased(b[j], toHead)
				}
			}
		default:
			continue
		}
		for j := range len(tail) {
			b[i+j] = agCased(b[i+j], toTail[j])
		}
		changed = true
		i += len(tail) - 1
	}
	return string(b), changed
}

// agNamesAFile reports whether what follows a name makes it the .md file or
// its Windows short name.
func agNamesAFile(rest string) bool {
	if rest != "" && strings.ContainsRune("'\"`", rune(rest[0])) {
		rest = rest[1:]
	}
	return strings.HasPrefix(rest, ".m") || strings.HasPrefix(rest, ".{m") || strings.HasPrefix(rest, "~1")
}

// agCased is letter in the case of like.
func agCased(like, letter byte) byte {
	if like >= 'A' && like <= 'Z' {
		return letter - 'a' + 'A'
	}
	return letter
}

// agRespellDir rewrites the directory name from (".claude") as to (".agents")
// where it is a whole segment of a path: .claude-plugin, .claude.json and
// .claudex are other names, and are left alone.
func agRespellDir(s, from, to string) (out string, changed bool) {
	lower := strings.ToLower(s)
	b := []byte(s)
	for i := 0; i+len(from) <= len(lower); i++ {
		if lower[i:i+len(from)] != from {
			continue
		}
		if end := i + len(from); end < len(lower) && !strings.ContainsRune("/\\'\"` \t\n;)|&>", rune(lower[end])) {
			continue
		}
		for j := 1; j < len(from); j++ {
			b[i+j] = agCased(b[i+j], to[j])
		}
		changed = true
		i += len(from) - 1
	}
	return string(b), changed
}

// agAsked reports whether a row may be respelt at all: one that already names
// agents is not, so that respelling the state back is exact.
func agAsked(command string) bool {
	return !strings.Contains(strings.ToLower(command), "agents")
}

// agSpelling is one respelling: the criterion its subtests carry, and the way
// there and back for a word.
type agSpelling struct {
	criterion string
	there     func(string) (string, bool)
	back      func(string) string
}

var (
	agAsAgentsMD = agSpelling{
		criterion: "US-023 AC-5",
		there:     func(s string) (string, bool) { return agRespell(s, "claude", "agents") },
		back:      func(s string) string { out, _ := agRespell(s, "agents", "claude"); return out },
	}
	agAsDotAgents = agSpelling{
		criterion: "US-023 AC-11",
		there:     func(s string) (string, bool) { return agRespellDir(s, ".claude", ".agents") },
		back:      func(s string) string { out, _ := agRespellDir(s, ".agents", ".claude"); return out },
	}
	agAsLocal = agSpelling{
		criterion: "US-023 AC-12",
		there:     agRespellLocal,
		back:      agUnrespellLocal,
	}
)

// respelt is s with what the disk says of a word asked of the word as the row
// wrote it, and answered in this spelling.
func (sp agSpelling) respelt(s State) State {
	there := func(w string) string {
		out, _ := sp.there(w)
		return out
	}
	if resolve := s.Resolve; resolve != nil {
		s.Resolve = func(word string) string { return there(resolve(sp.back(word))) }
	}
	if outside := s.Outside; outside != nil {
		s.Outside = func(path string, below bool) bool { return outside(sp.back(path), below) }
	}
	if shared := s.Shared; shared != nil {
		s.Shared = func(word string) []Landing {
			got := shared(sp.back(word))
			out := make([]Landing, 0, len(got))
			for _, l := range got {
				l.Rel = there(l.Rel)
				out = append(out, l)
			}
			return out
		}
	}
	return s
}

// ask puts a row to check again in this spelling, as a subtest, where the row
// spells the name at all.
func (sp agSpelling) ask(t *testing.T, command string, s State, check func(t *testing.T, command string, s State)) {
	t.Helper()
	if !agAsked(command) {
		return
	}
	respelt, ok := sp.there(command)
	if !ok {
		return
	}
	t.Run(sp.criterion+" "+respelt, func(t *testing.T) { check(t, respelt, sp.respelt(s)) })
}

// agTheDirIsTheProjects reports whether a row's .claude is the project's own,
// and not the person's configuration under the home.
func agTheDirIsTheProjects(command string, s State) bool {
	lower := strings.ToLower(command)
	return s.ClaudeConfig == nil && !strings.Contains(lower, "~") && !strings.Contains(lower, "home")
}

// agLocalAt is where ".local" goes in a spelling of CLAUDE.md whose tail
// "laude" starts at i: after the name and at most one quote, where `.m` or
// `.{m` follows. It is -1 where this is not CLAUDE.md as a file: the short
// name CLAUDE~1 has no CLAUDE.local.md twin.
func agLocalAt(s, lower string, i int) int {
	if lower[i:i+5] != "laude" {
		return -1
	}
	switch {
	case lower[i-1] == 'c':
	case s[i-1] == ']' && strings.LastIndexByte(s[:i-1], '[') >= 0:
	default:
		return -1
	}
	at := i + 5
	if at < len(s) && strings.ContainsRune("'\"`", rune(s[at])) {
		at++
	}
	if rest := lower[at:]; strings.HasPrefix(rest, ".m") || strings.HasPrefix(rest, ".{m") {
		return at
	}
	return -1
}

// agRespellLocal is s with each spelling of CLAUDE.md written as the same
// spelling of CLAUDE.local.md (AC-12): CLAUDE.md as CLAUDE.local.md,
// `[C]LAUDE.md` as `[C]LAUDE.local.md`, `'CLAUDE'.md` as `'CLAUDE'.local.md`
// and CLAUDE.{md,} as CLAUDE.local.{md,}. A row that already names a .local.md
// is not respelt, so that the way back is exact.
func agRespellLocal(s string) (string, bool) {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "local.m") || strings.Contains(lower, "local.{m") {
		return s, false
	}
	var b strings.Builder
	last, changed := 0, false
	for i := 1; i+5 <= len(lower); i++ {
		at := agLocalAt(s, lower, i)
		if at < 0 {
			continue
		}
		b.WriteString(s[last:at])
		b.WriteString(".local")
		last, changed = at, true
		i = at
	}
	b.WriteString(s[last:])
	return b.String(), changed
}

// agUnrespellLocal is agRespellLocal undone.
func agUnrespellLocal(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	last := 0
	for i := 1; i+5 <= len(lower); i++ {
		if lower[i:i+5] != "laude" {
			continue
		}
		at := i + 5
		if at < len(s) && strings.ContainsRune("'\"`", rune(s[at])) {
			at++
		}
		if !strings.HasPrefix(lower[at:], ".local.m") && !strings.HasPrefix(lower[at:], ".local.{m") {
			continue
		}
		b.WriteString(s[last:at])
		last = at + len(".local")
		i = last
	}
	b.WriteString(s[last:])
	return b.String()
}

// alsoRefusedAsAgents asks a refused row again for AGENTS.md (AC-5), and, where
// it is refused as a protected path the project holds, for .agents (AC-11).
// The harness calls it; a table needs nothing of its own.
func alsoRefusedAsAgents(t *testing.T, command string, s State, rules []string,
	check func(t *testing.T, command string, s State)) {
	t.Helper()
	agAsAgentsMD.ask(t, command, s, check)
	agAsLocal.ask(t, command, s, check)
	if len(rules) == 1 && rules[0] == agProtected && agTheDirIsTheProjects(command, s) {
		agAsDotAgents.ask(t, command, s, check)
	}
}

// alsoAllowedAsAgents asks a row that is let through again for AGENTS.md: the
// name is held, not every mention of it.
func alsoAllowedAsAgents(t *testing.T, command string, s State, check func(t *testing.T, command string, s State)) {
	t.Helper()
	agAsAgentsMD.ask(t, command, s, check)
	agAsLocal.ask(t, command, s, check)
}

// The respelling itself, since every US-023 AC-5 and AC-11 subtest stands on it: each
// spelling of CLAUDE.md the tables use becomes the same spelling of AGENTS.md,
// .claude becomes .agents only as a whole segment, and what is not the file is
// left alone.
func TestAC5TheRespellingTurnsEachSpellingOfCLAUDEmdIntoTheSameSpellingOfAGENTSmd(t *testing.T) {
	for in, want := range map[string]string{
		"echo x > CLAUDE.md":                   "echo x > AGENTS.md",
		"echo x > claude.md":                   "echo x > agents.md",
		"echo x > Claude.MD":                   "echo x > Agents.MD",
		"rm CLAUDE.m?":                         "rm AGENTS.m?",
		"rm [C]LAUDE.md":                       "rm [A]GENTS.md",
		"rm [!x]LAUDE.md":                      "rm [!x]GENTS.md",
		"find . -name '[]C]LAUDE.md' -delete":  "find . -name '[]A]GENTS.md' -delete",
		"echo x > CLAUDE.{md,}":                "echo x > AGENTS.{md,}",
		"echo x > 'CLAUDE'.md":                 "echo x > 'AGENTS'.md",
		"Remove-Item CLAUDE`.md":               "Remove-Item AGENTS`.md",
		"echo {} > CLAUDE~1":                   "echo {} > AGENTS~1",
		"rsync -a CLAUDE.md CLAUDE.md.bak":     "rsync -a AGENTS.md AGENTS.md.bak",
		"rsync -a /tmp/snap/CLAUDE.md .github": "rsync -a /tmp/snap/AGENTS.md .github",
	} {
		got, ok := agAsAgentsMD.there(in)
		if !ok || got != want {
			t.Errorf("AC-5 respelt %q as %q (%v), want %q", in, got, ok, want)
		}
		if back := agAsAgentsMD.back(got); back != in {
			t.Errorf("AC-5 %q respelt back is %q, want %q", got, back, in)
		}
	}
	for _, in := range []string{
		`claude -p "/sdlc:approve US-001"`, "echo x > ~/.claude/settings.json", "Claude Code", "echo x > .claude/s",
	} {
		if got, ok := agAsAgentsMD.there(in); ok {
			t.Errorf("AC-5 %q names no CLAUDE.md and was respelt as %q", in, got)
		}
	}
	for in, want := range map[string]string{
		"rm -rf .claude":                              "rm -rf .agents",
		"echo {} > .claude/settings.local.json":       "echo {} > .agents/settings.local.json",
		"cd .Claude && echo {} > settings.local.json": "cd .Agents && echo {} > settings.local.json",
	} {
		got, ok := agAsDotAgents.there(in)
		if !ok || got != want {
			t.Errorf("AC-11 respelt %q as %q (%v), want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"echo x > .claude-plugin/plugin.json", "cp x ~/.claude.json", "echo x > .claudex/s"} {
		if got, ok := agAsDotAgents.there(in); ok {
			t.Errorf("AC-11 %q names no .claude directory and was respelt as %q", in, got)
		}
	}
}

// AC-5: the writes Gate 2 reproduced as let through, refused as the same write
// to CLAUDE.md is, at any depth and in any case, by every writer.
func TestAC5AShellWriteToAGENTSmdIsRefusedAsTheSameWriteToCLAUDEmdIs(t *testing.T) {
	for _, command := range []string{
		"echo x > AGENTS.md",
		"echo x > sub/AGENTS.md",
		"echo x > a/b/agents.md",
		"echo x > Agents.MD",
		"rm AGENTS.md",
		"git checkout -- AGENTS.md",
		"cp /tmp/x AGENTS.md",
		"tee sub/AGENTS.md < /tmp/x",
		"ln -s /tmp/x AGENTS.md",
		"sed -i s/a/b/ AGENTS.md",
	} {
		t.Run("AC-5 "+command, func(t *testing.T) {
			f := refused(t, command, ready, agProtected)
			if claude, ok := Inspect(agAsAgentsMD.back(command), ready); ok && f.Route != claude.Route {
				t.Errorf("AC-5 %s is refused with another fix than the same write to CLAUDE.md:\n%s\n%s", command, f.Route, claude.Route)
			}
		})
	}
	// A word the disk knows as AGENTS.md is AGENTS.md, as one it knows as
	// CLAUDE.md is CLAUDE.md (TestAWordIsReadAsTheFileItIsOnDisk).
	s := ready
	s.Resolve = func(word string) string {
		return map[string]string{"docs/brief.md": "AGENTS.md", "AGENTS~1": "AGENTS.md"}[word]
	}
	for _, command := range []string{"cd docs && rm brief.md", "echo {} > AGENTS~1"} {
		t.Run("AC-5 "+command, func(t *testing.T) { refused(t, command, s, agProtected) })
	}
	// Reading it, and names that only start with the same letters, are not
	// writes to it; and docs/the-agents.md is the page the story renames the
	// agents page to, because docs/agents.md is one.
	for _, command := range []string{
		"cat AGENTS.md",
		"grep -n Contract AGENTS.md",
		"echo x > AGENTS.md.bak",
		"echo x > .agentsrc",
		"echo x > docs/the-agents.md",
		"echo x > my-agents.md",
	} {
		allowed(t, command, ready)
	}
}

// AC-11: a write under a .agents directory in the project, at any depth and in
// any case, refused as the same write under .claude is.
func TestAC11AShellWriteUnderTheProjectsDotAgentsIsRefusedAsUnderDotClaude(t *testing.T) {
	for _, command := range []string{
		"echo x > .agents/skills/x/SKILL.md",
		"echo x > sub/.agents/skills/x/SKILL.md",
		"mkdir -p .agents/skills/x && echo x > .agents/skills/x/SKILL.md",
		"echo x > .AGENTS/skills/x/SKILL.md",
		"rm -rf .agents",
		"cp -r /tmp/skills .agents/skills",
		"cd .agents && echo x > notes.md",
	} {
		t.Run("AC-11 "+command, func(t *testing.T) { refused(t, command, ready, agProtected) })
	}
	for _, command := range []string{
		"cat .agents/skills/x/SKILL.md",
		"echo x > .agents-cache/x",
		"echo x > agents/x",
	} {
		allowed(t, command, ready)
	}
}

// The person's decision 7: the shell read only the first /<name> in a path, so
// `echo x > a/.claudex/.claude/s` went through and `a/.claude/s` did not. The
// reproduced row, and the same shape for every name the shell protects: each
// occurrence counts, not the first.
func TestAC5EveryOccurrenceOfAProtectedNameInAPathIsReadNotOnlyTheFirst(t *testing.T) {
	s := ready
	s.Backlog = "user_stories.json"
	for command, rule := range map[string]string{
		"echo x > a/.claudex/.claude/s":                                  agProtected,
		"echo x > a/.gitx/.git/config":                                   agProtected,
		"echo x > a/CLAUDE.md.d/CLAUDE.md":                               agProtected,
		"echo x > a/AGENTS.md.d/AGENTS.md":                               agProtected,
		"echo x > a/.agentsx/.agents/s":                                  agProtected,
		"echo x > a/user_stories.json.d/user_stories.json":               agProtected,
		"echo x > a/.sdlc/statex/.sdlc/state/active":                     "loop-state-through-the-tool",
		"echo x > a/.sdlc/config.jsonx/.sdlc/config.json":                "loop-state-through-the-tool",
		"echo x > a/.sdlc/storiesx/.sdlc/stories/A-1/gate-record.json":   "loop-state-through-the-tool",
		"echo x > a/.sdlc/storiesx/.sdlc/stories/A-1/reviews/r.md":       "loop-state-through-the-tool",
		"echo x > a/.claude-plugin/b/.claude/settings.json":              agProtected,
		"echo x > a/.claudex/b/.claudey/.claude/hooks/hooks.json":        agProtected,
		"echo x > .claudex/.claude/s":                                    agProtected,
		"echo x > " + strings.Repeat("a/.claudex/", 50) + ".claude/s":    agProtected,
		"echo x > " + strings.Repeat("a/AGENTS.md.d/", 50) + "AGENTS.md": agProtected,
	} {
		t.Run("AC-5 "+command, func(t *testing.T) { refused(t, command, s, rule) })
	}
	// The near names stay what they are.
	for _, command := range []string{
		"echo x > a/.claudex/s",
		"echo x > a/.claudex/.claudey/s",
		"echo x > a/CLAUDE.md.d/notes.md",
		"echo x > a/.sdlc/statex/notes.md",
	} {
		allowed(t, command, s)
	}
}

// agHomeState is ready, with the person's home at home and HOME and USERPROFILE
// pointing there, the project's own words resolved, ~/.claude placed as US-108
// places it, and ~/.agents placed through the two fields the story adds:
// AgentsConfig, a word under ~/.agents, and AgentsConfigHeld, a directory that
// holds it. They are set by name, so that the package compiles before they
// exist; until they do, this says so and stops.
func agHomeState(t *testing.T) (State, string) {
	t.Helper()
	s, home := tempHomeState(t)
	agents := home + "/.agents"
	under := func(word string) bool {
		c := clean(word)
		return c == "~/.agents" || hasDir(c, "~/.agents") || c == agents || hasDir(c, agents)
	}
	held := func(word string) bool {
		c := clean(word)
		return c == "~" || c == home
	}
	v := reflect.ValueOf(&s).Elem()
	for name, f := range map[string]func(string) bool{"AgentsConfig": under, "AgentsConfigHeld": held} {
		field := v.FieldByName(name)
		if !field.IsValid() || field.Type() != reflect.TypeOf(under) {
			t.Fatalf("AC-11 shellpolicy.State has no field %s of type func(word string) bool, through which the "+
				"caller says where the person's ~/.agents is, as ClaudeConfig and ClaudeConfigHeld say it for ~/.claude", name)
		}
		field.Set(reflect.ValueOf(f))
	}
	return s, home
}

// agAsDotAgentsInTheHome is a row of US-108's ~/.claude tables with the
// person's .claude respelt .agents, or false for a row that is about Claude
// Code's own commands or ~/.claude.json, which have no .agents twin.
func agAsDotAgentsInTheHome(command string) (string, bool) {
	lower := strings.ToLower(command)
	if strings.Contains(lower, ".claude.json") || strings.HasPrefix(lower, "claude ") {
		return "", false
	}
	if !strings.Contains(lower, ".claude") {
		return command, true
	}
	return agRespellDir(command, ".claude", ".agents")
}

// agHeldBy checks that a refusal is the ~/.agents rule's, and that its words
// say why: other agent tools load skills from there.
func agHeldBy(t *testing.T, command string, f Finding, ok bool) {
	t.Helper()
	if !ok || f.Rule != agAgentsRule {
		t.Errorf("AC-11 %q = %+v, %v; want it refused as %s", command, f, ok, agAgentsRule)
		return
	}
	said := strings.ToLower(f.Reason + " " + f.Route)
	if f.Route == "" || !strings.Contains(said, "skills") || !strings.Contains(said, "agent tools") {
		t.Errorf("AC-11 the refusal of %q does not say that other agent tools load skills from there:\n%s\n%s",
			command, f.Reason, f.Route)
	}
}

// AC-11 and the second decision, answer 2: a write under the person's
// ~/.agents is refused in every session, as one under ~/.claude is. The rows
// are US-108's ~/.claude rows (claudeConfigWritten), respelt; ClaudeConfigWrites
// is the shell's one entry point for a session that holds no story, and Inspect
// for the session that holds one. What only reads ~/.agents, or copies out of
// it, stays free (claudeConfigRead, respelt).
func TestAC11AShellWriteUnderThePersonsDotAgentsIsRefusedInEverySession(t *testing.T) {
	s, home := agHomeState(t)
	asked := 0
	for _, written := range claudeConfigWritten(home) {
		command, ok := agAsDotAgentsInTheHome(written)
		if !ok {
			continue
		}
		asked++
		t.Run("US-023 AC-11 "+command, func(t *testing.T) {
			f, refusedNow := ClaudeConfigWrites(command, s)
			agHeldBy(t, command, f, refusedNow)
			f, refusedNow = Inspect(command, s)
			agHeldBy(t, command, f, refusedNow)
		})
	}
	if asked < 10 {
		t.Fatalf("only %d of US-108's rows were respelt, so this asks almost nothing", asked)
	}
	for _, read := range claudeConfigRead(home) {
		command, ok := agAsDotAgentsInTheHome(read)
		if !ok {
			continue
		}
		if f, refusedNow := ClaudeConfigWrites(command, s); refusedNow {
			t.Errorf("AC-11 %q only reads, and was refused: %+v", command, f)
		}
	}
	// The person's ~/.claude stays its own rule's.
	if f, ok := ClaudeConfigWrites("echo x > ~/.claude/settings.json", s); !ok || f.Rule != ruleClaudeConfig {
		t.Errorf("AC-11 ~/.claude was refused as %+v, %v; want %s", f, ok, ruleClaudeConfig)
	}
	// Placed nowhere, ~/.agents is nobody's: the caller says where it is.
	if f, ok := ClaudeConfigWrites("echo x > ~/.agents/skills/x/SKILL.md", tempHomeStateOnly(t)); ok {
		t.Errorf("AC-11 with no way to place ~/.agents, a write to it was refused: %+v", f)
	}
}

// tempHomeStateOnly is tempHomeState with nothing said of ~/.agents.
func tempHomeStateOnly(t *testing.T) State {
	t.Helper()
	s, _ := tempHomeState(t)
	return s
}

// The AC-12 respelling, since every US-023 AC-12 subtest stands on it: each
// spelling of CLAUDE.md the tables use becomes the same spelling of
// CLAUDE.local.md, and back.
func TestAC12TheRespellingTurnsEachSpellingOfCLAUDEmdIntoTheSameSpellingOfCLAUDElocalmd(t *testing.T) {
	for in, want := range map[string]string{
		"echo x > CLAUDE.md":                  "echo x > CLAUDE.local.md",
		"echo x > sub/claude.md":              "echo x > sub/claude.local.md",
		"echo x > Claude.MD":                  "echo x > Claude.local.MD",
		"rm CLAUDE.m?":                        "rm CLAUDE.local.m?",
		"rm [C]LAUDE.md":                      "rm [C]LAUDE.local.md",
		"rm [!x]LAUDE.md":                     "rm [!x]LAUDE.local.md",
		"find . -name '[]C]LAUDE.md' -delete": "find . -name '[]C]LAUDE.local.md' -delete",
		"echo x > CLAUDE.{md,}":               "echo x > CLAUDE.local.{md,}",
		"echo x > 'CLAUDE'.md":                "echo x > 'CLAUDE'.local.md",
		"Remove-Item CLAUDE`.md":              "Remove-Item CLAUDE`.local.md",
		"rsync -a CLAUDE.md CLAUDE.md.bak":    "rsync -a CLAUDE.local.md CLAUDE.local.md.bak",
	} {
		got, ok := agAsLocal.there(in)
		if !ok || got != want {
			t.Errorf("AC-12 respelt %q as %q (%v), want %q", in, got, ok, want)
		}
		if back := agAsLocal.back(got); back != in {
			t.Errorf("AC-12 %q respelt back is %q, want %q", got, back, in)
		}
	}
	for _, in := range []string{
		`claude -p "/sdlc:approve US-001"`, "echo x > ~/.claude/settings.local.json", "echo {} > CLAUDE~1",
		"echo x > CLAUDE.local.md", "echo x > .claude/s",
	} {
		if got, ok := agAsLocal.there(in); ok {
			t.Errorf("AC-12 %q names no CLAUDE.md file to respell and was respelt as %q", in, got)
		}
	}
}

// AC-12: CLAUDE.local.md, at any depth and in any case, refused through the
// shell as the same write to CLAUDE.md is, with the same fix. Claude Code
// reads it as project instructions, and where it is there Claude Code does not
// read AGENTS.md.
func TestAC12AShellWriteToCLAUDElocalmdIsRefusedAsTheSameWriteToCLAUDEmdIs(t *testing.T) {
	for _, command := range []string{
		"echo x > CLAUDE.local.md",
		"echo x > sub/CLAUDE.local.md",
		"echo x > a/b/claude.local.md",
		"echo x > Claude.Local.MD",
		"printf x >> CLAUDE.local.md",
		"rm CLAUDE.local.md",
		"cp /tmp/x CLAUDE.local.md",
		"cp notes.md CLAUDE.local.md",
		"mv /tmp/x sub/CLAUDE.local.md",
		"tee CLAUDE.local.md < /tmp/x",
		"ln -s /tmp/x CLAUDE.local.md",
		"sed -i s/a/b/ CLAUDE.local.md",
		"touch CLAUDE.local.md",
	} {
		t.Run("AC-12 "+command, func(t *testing.T) {
			f := refused(t, command, ready, agProtected)
			if claude, ok := Inspect(agUnrespellLocal(command), ready); ok && f.Route != claude.Route {
				t.Errorf("AC-12 %s is refused with another fix than the same write to CLAUDE.md:\n%s\n%s", command, f.Route, claude.Route)
			}
		})
	}
	// A word the disk knows as CLAUDE.local.md is CLAUDE.local.md.
	s := ready
	s.Resolve = func(word string) string { return map[string]string{"docs/brief.md": "CLAUDE.local.md"}[word] }
	t.Run("AC-12 cd docs && rm brief.md", func(t *testing.T) { refused(t, "cd docs && rm brief.md", s, agProtected) })
	// Reading it, and names that only start with the same letters, are not
	// writes to it.
	for _, command := range []string{
		"cat CLAUDE.local.md",
		"grep -n Contract sub/CLAUDE.local.md",
		"echo x > CLAUDE.local.md.bak",
		"echo x > CLAUDE.localx.md",
		"echo x > my-claude.local.md",
	} {
		allowed(t, command, ready)
	}
}

// Decision 7, the other way round (design review round 1, red team): where
// the protected name comes first and a near name after it, the path is still
// under the protected name. A matcher that reads only the last occurrence
// (strings.LastIndex in place of strings.Index) passed every row of
// TestAC5EveryOccurrenceOfAProtectedNameInAPathIsReadNotOnlyTheFirst, which
// puts the near name first; these are the rows it lets through.
func TestAC5AProtectedNameIsReadWhereANearNameComesAfterIt(t *testing.T) {
	s := ready
	s.Backlog = "user_stories.json"
	for command, rule := range map[string]string{
		"echo x > sub/.claude/x/.claudex/s":                               agProtected,
		"echo x > sub/.claude/skills/.claude-plugin/s":                    agProtected,
		"echo x > .claude/x/.claudex/s":                                   agProtected,
		"echo x > sub/.agents/skills/.agents-cache/s":                     agProtected,
		"echo x > sub/.agents/x/.agentsx/s":                               agProtected,
		"echo x > .agents/x/.agentsx/s":                                   agProtected,
		"echo x > sub/.git/x/.gitx/s":                                     agProtected,
		"echo x > sub/CLAUDE.md/x/CLAUDE.md.bak":                          agProtected,
		"echo x > sub/AGENTS.md/x/AGENTS.md.bak":                          agProtected,
		"echo x > sub/CLAUDE.local.md/x/CLAUDE.local.md.bak":              agProtected,
		"echo x > a/.sdlc/state/x/.sdlc/statex/s":                         "loop-state-through-the-tool",
		"echo x > sub/.claude/" + strings.Repeat("x/.claudex/", 50) + "s": agProtected,
	} {
		t.Run("AC-5 "+command, func(t *testing.T) { refused(t, command, s, rule) })
	}
}

// agAboveRule is the rule a write to an instruction file above the project is
// refused under, from the shell and from the file tools (AC-13).
const agAboveRule = "instructions-above-the-project"

// agAboveRoot is the project these rows are held in; /work and / are above it.
const agAboveRoot = "/work/project"

// agAboveState is ready for a session held in /work/project, with the disk
// answering as the hook's onDisk does: a relative word is the project's,
// cleaned, and a path that cleans to somewhere outside the project is "".
func agAboveState() State { return agAboveStateAt(agAboveRoot) }

// agAboveStateAt is agAboveState for a project held at root, so that a
// directory above it can have a dot in its name (/src/github.com/project).
func agAboveStateAt(root string) State {
	s := ready
	s.Root = root
	s.Resolve = func(word string) string {
		p := word
		if !strings.HasPrefix(p, "/") {
			p = root + "/" + p
		}
		p = path.Clean(p)
		switch rest, ok := strings.CutPrefix(p, root); {
		case !ok:
			return ""
		case rest == "":
			return "."
		case rest[0] == '/':
			return rest[1:]
		}
		return ""
	}
	return s
}

// agSaysAbove checks that a refusal says why an instruction file above the
// project is held: Claude Code loads them from every directory above the one
// it starts in.
func agSaysAbove(t *testing.T, command string, f Finding) {
	t.Helper()
	said := strings.ToLower(f.Reason + " " + f.Route)
	if !strings.Contains(said, "every directory above") || !strings.Contains(said, "claude code") {
		t.Errorf("AC-13 the refusal of %q does not say that Claude Code loads instruction files from every "+
			"directory above the one it starts in:\n%s\n%s", command, f.Reason, f.Route)
	}
}

// AC-13: during a story, a shell write to a CLAUDE.md, CLAUDE.local.md or
// AGENTS.md in a directory above the project is refused, with a fix that says
// why. Today a word placed outside the project skips every rule about the
// project's files, so `echo x > /work/CLAUDE.md` goes through, and
// `../CLAUDE.md` is refused only because a relative word is matched by name.
// A directory beside the project, or one whose name only starts with the
// project's, is not above it.
func TestAC13AShellWriteToAnInstructionFileAboveTheProjectIsRefused(t *testing.T) {
	s := agAboveState()
	for _, command := range []string{
		"echo x > /work/CLAUDE.md",
		"echo x > /CLAUDE.md",
		"echo x > /work/AGENTS.md",
		"echo x > /work/CLAUDE.local.md",
		"printf x >> /work/AGENTS.md",
		"cp /tmp/x /work/CLAUDE.md",
		"tee /work/CLAUDE.local.md < /tmp/x",
		"rm /work/AGENTS.md",
		"echo x > /work/project/sub/../../CLAUDE.md",
		"echo x > ../CLAUDE.md",
		"echo x > ../../AGENTS.md",
		"cd .. && echo x > CLAUDE.local.md",
		"cd /work && echo x > AGENTS.md",
	} {
		t.Run("AC-13 "+command, func(t *testing.T) {
			f := refused(t, command, s, agAboveRule)
			agSaysAbove(t, command, f)
		})
	}
	// Beside the project, or a name that only starts with the project's: not
	// above it, so not this rule.
	for _, command := range []string{
		"echo x > /work/other/CLAUDE.md",
		"echo x > /work/project2/AGENTS.md",
		"echo x > /work/proj/CLAUDE.md",
		"echo x > /tmp/out/CLAUDE.local.md",
		"echo x > ../other/AGENTS.md",
		"echo x > /work/notes.md",
	} {
		t.Run("AC-13 "+command, func(t *testing.T) {
			if f, ok := Inspect(command, s); ok && f.Rule == agAboveRule {
				t.Errorf("AC-13 %q is not above the project and was refused as if it were: %s", command, f.Reason)
			}
		})
	}
	// The project's own CLAUDE.md stays the project's rule's, and reading one
	// above is not writing it.
	refused(t, "echo x > /work/project/CLAUDE.md", s, agProtected)
	for _, command := range []string{"cat /work/CLAUDE.md", "grep -n Contract ../AGENTS.md"} {
		allowed(t, command, s)
	}
}

// agNotRefusedAs checks that command, if it is refused at all, is not refused
// under any of rules: the row is about another file, or no file at all.
func agNotRefusedAs(t *testing.T, criterion, command string, s State, rules ...string) {
	t.Helper()
	if f, ok := inspectNoPanic(t, command, s); ok && slices.Contains(rules, f.Rule) {
		t.Errorf("%s %q names no file these rules hold and was refused under %s: %s", criterion, command, f.Rule, f.Reason)
	}
}

// AC-13, design review round 3 (security B1): the name of an instruction file
// above the project spelt with a glob, a brace list or a backslash is the
// name, as the same spelling of the project's own CLAUDE.md already is; a
// directory above spelt with a glob is still above; and the name is read in
// any case, as the shell reads the project's own. Today a word placed outside
// the project skips every rule about the project's files, so each of these is
// allowed, or refused by name under the project's rule where it is relative.
func TestAC13AnInstructionFileAboveTheProjectIsReadThroughEverySpellingOfItsName(t *testing.T) {
	s := agAboveState()
	for _, command := range []string{
		"echo x >> /work/CLAUDE.m?",
		"echo x | tee /work/CLAUDE.{md,x}",
		"echo x > /work/[C]LAUDE.md",
		`echo x > /work/CLAUDE.m\d`,
		"sed -i s/a/b/ /work/CLAUDE.m*",
		"rm /work/AGENTS.m?",
		"echo x > /work/CLAUDE.local.m?",
		"echo x > ../CLAUDE.m?",
		"echo x | tee /{work,x}/AGENTS.md",
		"echo x > /wor*/CLAUDE.md",
		"echo x > /w?rk/AGENTS.md",
		"echo x > /[w]ork/CLAUDE.local.md",
		"echo x > /work/claude.md",
		"echo x > /Agents.MD",
		"echo x > /work/Claude.Local.MD",
		"echo x > /work/.claude/AGENTS.md",
		"echo x > /work/.clau*/CLAUDE.md",
		"cd $D && echo x > /work/CLAUDE.md",
	} {
		t.Run("AC-13 "+command, func(t *testing.T) {
			f, ok := inspectNoPanic(t, command, s)
			if !ok || f.Rule != agAboveRule {
				t.Errorf("AC-13 %q = %+v, %v; want it refused as %s", command, f, ok, agAboveRule)
				return
			}
			agSaysAbove(t, command, f)
		})
	}
	// A glob that cannot reach an instruction file above the project, or
	// reaches one only beside it, is not this rule's.
	for _, command := range []string{
		"echo x > /work/CLAUDE.md.ba?",
		"echo x > /wor*/notes.md",
		"echo x > /work/other/CLAUDE.m?",
		"echo x > /work/proj/AGENTS.{md,x}",
		"echo x > /tmp/out/[C]LAUDE.md",
	} {
		t.Run("AC-13 "+command, func(t *testing.T) { agNotRefusedAs(t, "AC-13", command, s, agAboveRule) })
	}
}

// AC-13 and AC-15, design review round 3 (security B2, red team hostile 2
// and 3): ln, install, cp and mv that land an instruction file in a directory
// above the project, by its name under the destination, with -t, with one
// operand, and where the destination's name has a dot. writtenTo has a
// landing only for cp and mv, and reads a last name with a dot as a file, so
// today all of these are allowed.
func TestAC13AndAC15ALinkOrCopyThatLandsAnInstructionFileAboveTheProjectIsRefused(t *testing.T) {
	ask := func(t *testing.T, command string, s State) {
		t.Helper()
		t.Run("AC-13 "+command, func(t *testing.T) {
			f, ok := inspectNoPanic(t, command, s)
			if !ok || f.Rule != agAboveRule {
				t.Errorf("AC-13 %q = %+v, %v; want it refused as %s", command, f, ok, agAboveRule)
				return
			}
			agSaysAbove(t, command, f)
		})
	}
	s := agAboveState()
	for _, command := range []string{
		"ln -s /tmp/p/CLAUDE.md /work",
		"ln -s /tmp/p/CLAUDE.md ..",
		"ln /tmp/p/CLAUDE.md /work/",
		"ln -sf /tmp/p/AGENTS.md /",
		"ln -t .. /tmp/p/CLAUDE.local.md",
		"cd .. && ln -s /tmp/p/CLAUDE.md",
		"install /tmp/p/CLAUDE.md /work",
		"install -m 644 /tmp/p/AGENTS.md ..",
		"install -t /work /tmp/p/CLAUDE.md",
		"cp /tmp/p/CLAUDE.md /work",
		"mv /tmp/p/AGENTS.md ..",
	} {
		ask(t, command, s)
	}
	// A directory above whose name has a dot: a GOPATH layout, and a user
	// directory named <user>.site.
	dotted := agAboveStateAt("/src/github.com/project")
	for _, command := range []string{
		"cp /tmp/p/CLAUDE.md /src/github.com",
		"ln -s /tmp/p/CLAUDE.md /src/github.com",
		"ln /tmp/p/AGENTS.md /src/github.com/",
		"install /tmp/p/CLAUDE.md /src/github.com",
		"install -m 644 /tmp/p/CLAUDE.local.md /src/github.com",
		"mv /tmp/p/CLAUDE.md /src/github.com",
		"cp /tmp/p/CLAUDE.local.md ..",
	} {
		ask(t, command, dotted)
	}
	ask(t, "cp /tmp/p/CLAUDE.md /srv/dev.site", agAboveStateAt("/srv/dev.site/code/project"))
	// What lands beside the project, or is no instruction file, is not this
	// rule's.
	for _, command := range []string{
		"ln -s /tmp/p/notes.md /work",
		"ln -s /tmp/p/CLAUDE.md /work/other",
		"install /tmp/p/CLAUDE.md /tmp/out",
		"cp /tmp/p/notes.md /work",
	} {
		t.Run("AC-13 "+command, func(t *testing.T) { agNotRefusedAs(t, "AC-13", command, s, agAboveRule) })
	}
}

// AC-13, design review round 3 (red team hostile 4): a copy or an extraction
// that fills a directory above the project with whatever files it holds,
// instruction files among them, by shapes the plan's step 8b did not read: the
// working directory after a cd above, a glob copy that is not recursive, and
// writers outside copiesWhole and intoDirectories. All allowed today.
func TestAC13ACopyOrExtractionThatFillsADirectoryAboveTheProjectIsRefused(t *testing.T) {
	s := agAboveState()
	for _, command := range []string{
		"cd .. && tar -xf /tmp/p.tar",
		"cd /work && tar -xf /tmp/p.tar",
		"cd .. && unzip /tmp/p.zip",
		"cp /tmp/p/* ..",
		"cp /tmp/p/* /work/",
		"ditto /tmp/p ..",
		"ditto /tmp/p /work",
		"bsdtar -xf /tmp/p.tar -C ..",
		"7z x /tmp/p.7z -o..",
		"7z x /tmp/p.7z -o/work",
	} {
		t.Run("AC-13 "+command, func(t *testing.T) {
			f, ok := inspectNoPanic(t, command, s)
			if !ok || f.Rule != agAboveRule {
				t.Errorf("AC-13 %q = %+v, %v; want it refused as %s", command, f, ok, agAboveRule)
				return
			}
			agSaysAbove(t, command, f)
		})
	}
	// git checking out into a directory above is refused today, as
	// loop-state-through-the-tool, because the checkout's pathspec reaches
	// the loop's state; either rule keeps it refused.
	for _, command := range []string{"git --work-tree=.. checkout HEAD -- .", "git --work-tree=/work checkout HEAD -- ."} {
		t.Run("AC-13 "+command, func(t *testing.T) { refusedUnder(t, command, s, agAboveRule, ruleLoopState) })
	}
	// The same programs filling a directory in the project are not this
	// rule's.
	for _, command := range []string{
		"tar -xf /tmp/p.tar -C build",
		"cd build && tar -xf /tmp/p.tar",
		"cp /tmp/p/* build/",
		"ditto /tmp/p build",
		"bsdtar -xf /tmp/p.tar -C build",
		"7z x /tmp/p.7z -obuild",
	} {
		t.Run("AC-13 "+command, func(t *testing.T) { agNotRefusedAs(t, "AC-13", command, s, agAboveRule) })
	}
}

// Decision 7 and AC-13 (red team, rounds 2 and 3): a path with two whole
// matches of one name. A matcher that walks every match and ignores yield's
// false return panics with "range function continued iteration", and the
// hook fails open; each must be refused, without a panic.
func TestAC5AndAC13APathWithTwoWholeMatchesOfOneNameIsRefusedWithoutAPanic(t *testing.T) {
	s := ready
	s.Backlog = "user_stories.json"
	for command, rule := range map[string]string{
		"echo x > a/.claude/b/.claude/c":                               agProtected,
		"echo x > .claude/a/.claude/b/.claude/c":                       agProtected,
		"echo x > .agents/a/.agents/b":                                 agProtected,
		"echo x > a/CLAUDE.md/b/CLAUDE.md":                             agProtected,
		"echo x > a/AGENTS.md/b/AGENTS.md":                             agProtected,
		"echo x > a/CLAUDE.local.md/b/CLAUDE.local.md":                 agProtected,
		"echo x > a/.git/b/.git/c":                                     agProtected,
		"echo x > a/.sdlc/state/b/.sdlc/state/c":                       ruleLoopState,
		"rm -rf .sdlc/stories/US-1/y/.sdlc/stories/US-1":               ruleLoopState,
		"echo x > sub/.claude/" + strings.Repeat(".claude/", 50) + "s": agProtected,
	} {
		t.Run("AC-5 "+command, func(t *testing.T) { refusedUnder(t, command, s, rule) })
	}
}

// agFrozenTest is the frozen test the AC-14 to AC-16 rows aim at.
const agFrozenTest = "internal/invoice_test.go"

// agHeldState is agAboveState with a frozen test, one in a directory whose
// name has a dot, and the backlog, so that every kind of file a story holds
// is in the project.
func agHeldState() State {
	s := agAboveState()
	s.Frozen = []string{agFrozenTest, "pkg.v2/invoice_test.go"}
	s.Backlog = "user_stories.json"
	return s
}

// AC-14 (design review round 3, security note 3): a glob or a brace list in
// an absolute directory, or in the name, that could name a held file in the
// project reaches it, as the same command spelt from the project root does.
// memo.outside asks Resolve, which reads the glob as a literal name and
// answers outside, so today each of these is allowed. The rule is the one the
// root's spelling is refused under; where the name is an instruction file,
// the AC-13 rule is an answer too, since the same glob reaches a directory
// beside the project.
func TestAC14AGlobInAnAbsoluteDirectoryReachesTheProjectsHeldFiles(t *testing.T) {
	s := agHeldState()
	for command, twin := range map[string]string{
		"echo x > /work/proj*/.sdlc/state/tests.lock":      "echo x > .sdlc/state/tests.lock",
		"echo '{}' > /wor?/project/.sdlc/state/tests.lock": "echo '{}' > .sdlc/state/tests.lock",
		"rm /work/proj*/.sdlc/state/tests.lock":            "rm .sdlc/state/tests.lock",
		"rm /wor?/project/.sdlc/config.json":               "rm .sdlc/config.json",
		"rm /work/*/.sdlc/config.json":                     "rm .sdlc/config.json",
		"echo x > /work/[p]roject/.sdlc/state/active":      "echo x > .sdlc/state/active",
		"echo x > /work/{project,x}/.sdlc/config.json":     "echo x > .sdlc/config.json",
		"rm -rf /work/proj*/.sdlc":                         "rm -rf .sdlc",
		"echo x > /*/project/user_stories.json":            "echo x > user_stories.json",
		"cp /tmp/x /work/p*/.git/config":                   "cp /tmp/x .git/config",
		"echo x > /work/proj*/.claude/settings.json":       "echo x > .claude/settings.json",
		"echo x > /work/proj*/" + agFrozenTest:             "echo x > " + agFrozenTest,
		"rm /wor*/project/" + agFrozenTest:                 "rm " + agFrozenTest,
		"echo x > /work/proj*/.sdlc/state/tests.loc?":      "echo x > .sdlc/state/tests.lock",
		"echo x > /work/proj*/CLAUDE.md":                   "echo x > CLAUDE.md",
		"echo x > /work/{project,x}/AGENTS.md":             "echo x > AGENTS.md",
		"rm /wor?/project/CLAUDE.m?":                       "rm CLAUDE.md",
		"echo x > /work/proj*/sub/CLAUDE.local.md":         "echo x > sub/CLAUDE.local.md",
	} {
		t.Run("AC-14 "+command, func(t *testing.T) {
			// The rule the root's spelling is refused under. AGENTS.md and
			// CLAUDE.local.md are this story's own names, held under
			// CLAUDE.md's rule (AC-5, AC-12), so their twin is not asked.
			rule := agProtected
			if lower := strings.ToLower(twin); !strings.Contains(lower, "agents") && !strings.Contains(lower, ".local.") {
				want, held := inspectNoPanic(t, twin, s)
				if !held {
					t.Fatalf("AC-14 the row's own twin %q is allowed, so the row asks nothing", twin)
				}
				rule = want.Rule
			}
			rules := []string{rule}
			// Every instruction file in these rows ends in .md.
			if strings.HasSuffix(twin, ".md") {
				rules = append(rules, agAboveRule)
			}
			f, ok := inspectNoPanic(t, command, s)
			if !ok || !slices.Contains(rules, f.Rule) {
				t.Errorf("AC-14 %q = %+v, %v; want it refused as %s is, under %s", command, f, ok, twin, strings.Join(rules, " or "))
			}
		})
	}
	// A glob that cannot name a held file in the project is not refused by
	// these rules.
	held := []string{ruleLoopState, ruleFrozenTest, agProtected}
	for _, command := range []string{
		"echo x > /work/proj*/notes.md",
		"rm /work/proj*/build/out.txt",
		"echo x > /tmp/*/tests.lock",
		"echo x > /work/other*/.sdlc/config.json",
		"echo x > /work/proj*/CLAUDE.md.bak",
		"echo x > /work/proj*/internal/other_test.go",
	} {
		t.Run("AC-14 "+command, func(t *testing.T) { agNotRefusedAs(t, "AC-14", command, s, held...) })
	}
}

// AC-15 (design review round 3, security note 3 and red team hostile 2): ln,
// symbolic or hard, with or without -f, -t or a destination directory, and
// install, are refused where what they create or replace is held, as cp to
// the same place is, a destination directory whose name has a dot included.
// writtenTo returns their arguments as given, with no landing, so today
// `ln -sf /tmp/x/CLAUDE.md .` replaces the root's CLAUDE.md with a link and is
// allowed.
func TestAC15LnAndInstallAreRefusedWhereCpToTheSamePlaceIs(t *testing.T) {
	s := agHeldState()
	for command, rule := range map[string]string{
		"ln -sf /tmp/x/CLAUDE.md .":                agProtected,
		"ln -s /tmp/x/CLAUDE.md .":                 agProtected,
		"ln /tmp/x/CLAUDE.md .":                    agProtected,
		"ln -f /tmp/x/CLAUDE.md ./":                agProtected,
		"ln -t . /tmp/x/CLAUDE.md":                 agProtected,
		"ln -sf /tmp/x/CLAUDE.md":                  agProtected,
		"ln -s /tmp/x/AGENTS.md sub/":              agProtected,
		"ln -s /tmp/x/CLAUDE.local.md .":           agProtected,
		"install /tmp/x/CLAUDE.md .":               agProtected,
		"install -m 644 /tmp/x/AGENTS.md .":        agProtected,
		"install -t . /tmp/x/CLAUDE.md":            agProtected,
		"ln -sf /tmp/x/tests.lock .sdlc/state":     ruleLoopState,
		"ln -s /tmp/x/active .sdlc/state/":         ruleLoopState,
		"ln -t .sdlc/state /tmp/x/tests.lock":      ruleLoopState,
		"install /tmp/x/config.json .sdlc":         ruleLoopState,
		"ln -sf /tmp/x/config.json .sdlc":          ruleLoopState,
		"ln -sf /tmp/x/invoice_test.go internal":   ruleFrozenTest,
		"install /tmp/x/invoice_test.go internal/": ruleFrozenTest,
		"ln -t internal /tmp/x/invoice_test.go":    ruleFrozenTest,
		"ln -s /tmp/x/invoice_test.go pkg.v2":      ruleFrozenTest,
		"install /tmp/x/invoice_test.go pkg.v2":    ruleFrozenTest,
		"cp /tmp/x/invoice_test.go pkg.v2":         ruleFrozenTest,
		"ln -s /tmp/x/CLAUDE.md docs.v2":           agProtected,
		"cp /tmp/x/AGENTS.md docs.v2":              agProtected,
		"ln -sf /tmp/x/CLAUDE.md ..":               agAboveRule,
		"install /tmp/x/AGENTS.md /work":           agAboveRule,
	} {
		t.Run("AC-15 "+command, func(t *testing.T) {
			f, ok := inspectNoPanic(t, command, s)
			if !ok || f.Rule != rule {
				t.Errorf("AC-15 %q = %+v, %v; want it refused as %s", command, f, ok, rule)
			}
		})
	}
	// The cp twins of the plain rows are refused today under the same rule:
	// that is what "as cp to the same place is" compares with.
	for _, command := range []string{"cp /tmp/x/CLAUDE.md .", "cp /tmp/x/tests.lock .sdlc/state/", "cp /tmp/x/invoice_test.go internal/"} {
		if _, ok := inspectNoPanic(t, command, s); !ok {
			t.Errorf("AC-15 the twin %q is allowed, so the rows above compare with nothing", command)
		}
	}
	// A link or an install of an ordinary file in the project stays allowed.
	for _, command := range []string{
		"ln -s /tmp/x/notes.md docs/notes.md",
		"ln -s README.md docs/readme.md",
		"ln -sf /tmp/x/tool bin",
		"install -m 755 /tmp/x/tool bin/",
		"ln -t docs /tmp/x/guide.md",
	} {
		t.Run("AC-15 "+command, func(t *testing.T) { allowed(t, command, s) })
	}
}

// agWritingPrograms are the programs AC-16 names, each with %s where the
// file it writes goes: an option's value, an operand, or the name a
// decompressor writes by taking the suffix off.
var agWritingPrograms = []string{
	"sort -o %s /tmp/x",
	"sort -o%s /tmp/x",
	"sort --output=%s /tmp/x",
	"sort /tmp/x -o %s",
	"uniq /tmp/x %s",
	"uniq -c /tmp/x %s",
	"gzip -df %s.gz",
	"gzip -d -f %s.gz",
	"gunzip -f %s.gz",
	"xz -df %s.xz",
	"unxz -f %s.xz",
	"base64 -d -o %s /tmp/x",
	"iconv -f utf-8 -t utf-8 -o %s /tmp/x",
	"iconv -f utf-8 -t utf-8 --output=%s /tmp/x",
}

// AC-16 (design review round 3, security note 3 and red team hostile 5): a
// program that writes a file it is given by an option or an operand is
// refused where that file is held, under the rule a redirect to it is refused
// under. changesFiles does not count these programs, so today every row is
// allowed.
func TestAC16AProgramThatWritesAFileItIsGivenIsRefusedAsARedirectToItIs(t *testing.T) {
	s := agHeldState()
	for target, rule := range map[string]string{
		"CLAUDE.md":              agProtected,
		"sub/AGENTS.md":          agProtected,
		".sdlc/config.json":      ruleLoopState,
		".sdlc/state/tests.lock": ruleLoopState,
		agFrozenTest:             ruleFrozenTest,
		"../CLAUDE.md":           agAboveRule,
		"/work/AGENTS.md":        agAboveRule,
	} {
		// A redirect to each target the released tool already holds is
		// refused under the rule given, so the rows compare like with like.
		if rule != agAboveRule && !strings.Contains(target, "AGENTS") {
			if f, ok := inspectNoPanic(t, "echo x > "+target, s); !ok || f.Rule != rule {
				t.Errorf("AC-16 a redirect to %s is refused as %+v, %v, not %s, so the rows compare with nothing", target, f, ok, rule)
			}
		}
		for _, program := range agWritingPrograms {
			command := strings.ReplaceAll(program, "%s", target)
			t.Run("AC-16 "+command, func(t *testing.T) {
				f, ok := inspectNoPanic(t, command, s)
				if !ok || f.Rule != rule {
					t.Errorf("AC-16 %q = %+v, %v; want it refused as a redirect to %s is, under %s", command, f, ok, target, rule)
				}
			})
		}
	}
	// Each program writing an ordinary file, and each only reading a held
	// one, stays allowed.
	for _, program := range agWritingPrograms {
		command := strings.ReplaceAll(program, "%s", "out/notes.txt")
		t.Run("AC-16 "+command, func(t *testing.T) { allowed(t, command, s) })
	}
	for _, command := range []string{
		"sort CLAUDE.md",
		"sort -o out.txt CLAUDE.md",
		"uniq CLAUDE.md",
		"uniq CLAUDE.md out.txt",
		"gzip -dc CLAUDE.md.gz",
		"base64 CLAUDE.md",
		"iconv -f utf-8 -t ascii CLAUDE.md",
		"sort .sdlc/config.json",
	} {
		t.Run("AC-16 "+command, func(t *testing.T) { allowed(t, command, s) })
	}
}
