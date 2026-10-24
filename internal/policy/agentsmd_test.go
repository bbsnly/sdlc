package policy

// US-023 for the file tools: AGENTS.md is held as CLAUDE.md is (AC-5), a
// CLAUDE.md, an AGENTS.md, a .claude and a .agents below the root are held as
// the shell already holds them (AC-6, and the story's second decision, answer
// 3), and the person's ~/.agents has a rule of its own, built like
// claude-config-is-the-persons (AC-11, answer 1).
//
// AC-5 asks for no new list: the rows of policy_test.go that protect a
// spelling of CLAUDE.md (protectedPathRows, caseRows, prefixRows) are respelt
// here for AGENTS.md, letter for letter and case for case, and for
// CLAUDE.local.md (AC-12). AC-13's instruction files above the project have a
// rule of their own, told by the caller through Request.AboveProject.

import (
	"reflect"
	"strings"
	"testing"
)

// agAgentsRule is the rule a write under the person's ~/.agents is refused
// under.
const agAgentsRule = "agents-config-is-the-persons"

// agRespell is s with each claude of CLAUDE.md and of a .claude segment
// written as agents, in the same case: CLAUDE.md as AGENTS.md, Claude.MD as
// Agents.MD, .Claude/settings.json as .Agents/settings.json. .claude-code-version
// and .claude.json are other names, and are left alone. changed is false where
// nothing was rewritten.
func agRespell(s string) (out string, changed bool) {
	const from, to = "claude", "agents"
	lower := strings.ToLower(s)
	b := []byte(s)
	for i := 0; i+len(from) <= len(lower); i++ {
		if lower[i:i+len(from)] != from {
			continue
		}
		rest := lower[i+len(from):]
		file := strings.HasPrefix(rest, ".md")
		dir := i > 0 && lower[i-1] == '.' && (rest == "" || rest[0] == '/')
		if !file && !dir {
			continue
		}
		for j := range len(from) {
			c := to[j]
			if b[i+j] >= 'A' && b[i+j] <= 'Z' {
				c = c - 'a' + 'A'
			}
			b[i+j] = c
		}
		changed = true
		i += len(from) - 1
	}
	return string(b), changed
}

// agWithAgentsConfig is r with Request.AgentsConfig set, the field the story
// adds: the path is in the person's ~/.agents, as the caller resolved it. Set
// by name, so that the package compiles before the field exists; until then r
// comes back as it was, and the rows that need it fail on the rule they get.
func agWithAgentsConfig(r Request) Request {
	f := reflect.ValueOf(&r).Elem().FieldByName("AgentsConfig")
	if f.IsValid() && f.Kind() == reflect.Bool {
		f.SetBool(true)
	}
	return r
}

func agHasAgentsConfigField(t *testing.T) {
	t.Helper()
	f, ok := reflect.TypeFor[Request]().FieldByName("AgentsConfig")
	if !ok || f.Type.Kind() != reflect.Bool {
		t.Fatal("AC-11 policy.Request has no AgentsConfig bool, through which the caller says a path is in the person's ~/.agents, as ClaudeConfig says it of ~/.claude")
	}
}

// AC-5: every policy row that protects a spelling of CLAUDE.md protects the
// same spelling of AGENTS.md, from every agent, under the same rule and with
// the same fix; every row that lets a near name through lets its twin through.
func TestAC5EveryPolicyRowThatProtectsCLAUDEmdProtectsTheSameSpellingOfAGENTSmd(t *testing.T) {
	agEveryRowRespelt(t, "AC-5", agRespell, 5)
}

// agEveryRowRespelt asks every policy row that protects a spelling of
// CLAUDE.md, and every row that lets a near name through, again in the
// spelling respell gives, and fails where fewer than atLeast rows were respelt.
func agEveryRowRespelt(t *testing.T, criterion string, respell func(string) (string, bool), atLeast int) {
	t.Helper()
	asked := 0
	for _, path := range append(append([]string{}, protectedPathRows...), caseRows...) {
		respelt, ok := respell(path)
		if !ok {
			continue
		}
		asked++
		for _, agent := range []string{"", "sdlc-researcher", "sdlc-implementer", "sdlc-sdet"} {
			got := Evaluate(Request{Tool: "Write", Agent: agent, Path: respelt, Story: "A-1"})
			claude := Evaluate(Request{Tool: "Write", Agent: agent, Path: path, Story: "A-1"})
			if got.Rule != "write-protected-path" || got.Route != claude.Route {
				t.Errorf("%s %s by %q: rule %q, allowed %v; want write-protected-path with the fix %s gets", criterion, respelt, agent, got.Rule, got.Allowed, path)
			}
		}
	}
	for _, path := range prefixRows {
		respelt, ok := respell(path)
		if !ok {
			continue
		}
		asked++
		if got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: respelt, Story: "A-1"}); !got.Allowed {
			t.Errorf("%s %s was refused by %s, but it only starts with a protected name", criterion, respelt, got.Rule)
		}
	}
	if asked < atLeast {
		t.Fatalf("only %d rows were respelt, so this asks almost nothing", asked)
	}
}

// agWriters are the tools that write a file.
var agWriters = []string{"Write", "Edit", "MultiEdit", "NotebookEdit"}

func agRefusedAsProtected(t *testing.T, criterion, path string) {
	t.Helper()
	for _, tool := range agWriters {
		got := Evaluate(Request{Tool: tool, Agent: "sdlc-implementer", Path: path, Story: "A-1"})
		if got.Rule != "write-protected-path" || got.Route == "" {
			t.Errorf("%s %s to %s: rule %q, allowed %v; want write-protected-path", criterion, tool, path, got.Rule, got.Allowed)
		}
	}
}

// AC-5 and AC-6: AGENTS.md at any depth, in any case, and CLAUDE.md below the
// root, refused to every file tool, as the shell refuses them.
func TestAC5AndAC6AGENTSmdAndCLAUDEmdAreProtectedAtAnyDepthFromEveryFileTool(t *testing.T) {
	for _, path := range []string{
		"AGENTS.md", "agents.md", "Agents.MD", "AGENTſ.md",
		"sub/AGENTS.md", "a/b/c/agents.md", "docs/AGENTS.md",
	} {
		agRefusedAsProtected(t, "AC-5", path)
	}
	for _, path := range []string{"sub/CLAUDE.md", "a/b/c/claude.md", "internal/Claude.MD", "docs/CLAUDE.md"} {
		agRefusedAsProtected(t, "AC-6", path)
	}
	// The same fix as CLAUDE.md at the root.
	root := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: "CLAUDE.md", Story: "A-1"})
	for _, path := range []string{"AGENTS.md", "sub/AGENTS.md", "sub/CLAUDE.md"} {
		if got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: path, Story: "A-1"}); got.Route != root.Route {
			t.Errorf("AC-5 %s is refused with another fix than CLAUDE.md:\n%s\n%s", path, got.Route, root.Route)
		}
	}
}

// AC-11 and decision 3: the project's .agents, and .claude below the root,
// refused to every file tool at any depth and in any case; .claude/AGENTS.md
// stays refused as part of .claude.
func TestAC11TheProjectsDotAgentsAndADotClaudeBelowTheRootAreProtectedFromEveryFileTool(t *testing.T) {
	for _, path := range []string{
		".agents/skills/x/SKILL.md", "sub/.agents/skills/x/SKILL.md", ".AGENTS/skills/x/SKILL.md",
		".agentſ/skills/x/SKILL.md", ".agents",
	} {
		agRefusedAsProtected(t, "AC-11", path)
	}
	for _, path := range []string{"sub/.claude/skills/x/SKILL.md", "a/b/.Claude/settings.json", ".claude/AGENTS.md"} {
		agRefusedAsProtected(t, "AC-11", path)
	}
}

// Decision 7, for the matcher the file tools gain: every segment counts, not
// the first that starts with the name; and decision 3's directory-awareness:
// a name that only starts with a protected one is another file.
func TestAC5EverySegmentOfAPathIsReadAndANearNameIsAnotherFile(t *testing.T) {
	for _, path := range []string{
		"a/.claudex/.claude/s", "a/CLAUDE.md.d/CLAUDE.md", "a/AGENTS.md.d/AGENTS.md", "a/.agentsx/.agents/s",
		"a/.claude-plugin/b/.claude/settings.json",
	} {
		agRefusedAsProtected(t, "AC-5", path)
	}
	for _, path := range []string{
		".claude-plugin/plugin.json", "plugin/.claude-plugin/marketplace.json", ".claude-code-version",
		"CLAUDE.md.bak", "AGENTS.md.bak", ".agentsrc", ".agents-cache/x", "agents/x", "docs/the-agents.md",
		"my-agents.md", "sub/CLAUDE.md.d/notes.md", "a/.claudex/s",
	} {
		if got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: path, Story: "A-1"}); !got.Allowed {
			t.Errorf("AC-5 %s was refused by %s, but it is not a protected path", path, got.Rule)
		}
	}
}

// AC-11: a write under the person's ~/.agents, which the caller resolves and
// flags, is refused by its own rule, ahead of write-outside-repository, with a
// fix that says other agent tools load skills from there.
func TestAC11AWriteUnderThePersonsDotAgentsIsRefusedByItsOwnRule(t *testing.T) {
	agHasAgentsConfigField(t)
	for _, path := range []string{
		"home/.agents/skills/x/SKILL.md", "home/.agents/skills/review/scripts/run.sh", "home/.agents/AGENTS.md",
	} {
		for _, tool := range agWriters {
			got := Evaluate(agWithAgentsConfig(Request{Tool: tool, Path: path, Outside: true, Story: "A-1"}))
			if got.Rule != agAgentsRule {
				t.Errorf("AC-11 %s to %s: rule %q, allowed %v; want %s", tool, path, got.Rule, got.Allowed, agAgentsRule)
				continue
			}
			said := strings.ToLower(got.Reason + " " + got.Route)
			if !strings.Contains(said, "skills") || !strings.Contains(said, "agent tools") {
				t.Errorf("AC-11 the refusal does not say other agent tools load skills from there: %s", got.Message())
			}
		}
	}
	// Held with no story too: the hook asks it from every session.
	if got := Evaluate(agWithAgentsConfig(Request{Tool: "Write", Path: "home/.agents/skills/x/SKILL.md", Outside: true})); got.Rule != agAgentsRule {
		t.Errorf("AC-11 with no story, rule %q; want %s", got.Rule, agAgentsRule)
	}
	if got := Evaluate(agWithAgentsConfig(Request{Tool: "Read", Path: "home/.agents/skills/x/SKILL.md", Outside: true})); !got.Allowed {
		t.Errorf("AC-11 reading ~/.agents was refused by %s", got.Rule)
	}
	if got := Evaluate(Request{Tool: "Write", Path: "/etc/hosts", Outside: true, Story: "A-1"}); got.Rule != "write-outside-repository" {
		t.Errorf("AC-11 an outside write that is not ~/.agents was refused by %q", got.Rule)
	}
}

// agRespellLocal is s with each CLAUDE.md written as CLAUDE.local.md, the
// case of the rest kept: claude.md as claude.local.md, Claude.MD as
// Claude.local.MD, CLAUDE.md.bak as CLAUDE.local.md.bak (AC-12).
func agRespellLocal(s string) (out string, changed bool) {
	const name = "claude.md"
	lower := strings.ToLower(s)
	var b strings.Builder
	last := 0
	for i := 0; i+len(name) <= len(lower); i++ {
		if lower[i:i+len(name)] != name {
			continue
		}
		b.WriteString(s[last : i+len("claude")])
		b.WriteString(".local")
		last, changed = i+len("claude"), true
		i += len(name) - 1
	}
	b.WriteString(s[last:])
	return b.String(), changed
}

// AC-12: every policy row that protects a spelling of CLAUDE.md protects the
// same spelling of CLAUDE.local.md, from every agent, under the same rule and
// with the same fix; every row that lets a near name through lets its twin
// through. Claude Code reads CLAUDE.local.md as project instructions, and does
// not read AGENTS.md where it is there.
func TestAC12EveryPolicyRowThatProtectsCLAUDEmdProtectsTheSameSpellingOfCLAUDElocalmd(t *testing.T) {
	agEveryRowRespelt(t, "AC-12", agRespellLocal, 4)
}

// AC-12: CLAUDE.local.md at any depth and in any case, and in the spellings
// NTFS reads as the same file, refused to every file tool as CLAUDE.md is,
// with CLAUDE.md's fix; a name that only starts with it is another file.
func TestAC12CLAUDElocalmdIsProtectedAtAnyDepthFromEveryFileTool(t *testing.T) {
	for _, path := range []string{
		"CLAUDE.local.md", "claude.local.md", "Claude.Local.MD", "CLAUDE.local.md.", "CLAUDE.local.md::$DATA",
		"sub/CLAUDE.local.md", "a/b/c/claude.local.md", "docs/CLAUDE.local.md",
	} {
		agRefusedAsProtected(t, "AC-12", path)
	}
	root := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: "CLAUDE.md", Story: "A-1"})
	for _, path := range []string{"CLAUDE.local.md", "sub/CLAUDE.local.md"} {
		if got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: path, Story: "A-1"}); got.Route != root.Route {
			t.Errorf("AC-12 %s is refused with another fix than CLAUDE.md:\n%s\n%s", path, got.Route, root.Route)
		}
	}
	for _, path := range []string{"CLAUDE.local.md.bak", "CLAUDE.localx.md", "my-claude.local.md", "CLAUDE.local.mdx", "docs/claude-local.md"} {
		if got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: path, Story: "A-1"}); !got.Allowed {
			t.Errorf("AC-12 %s was refused by %s, but it is not a protected path", path, got.Rule)
		}
	}
}

// Decision 7 the other way round (design review round 1, red team): the
// protected name first and a near name after it is still under the protected
// name. A matcher that reads only the last segment that starts with a name
// passes every row of TestAC5EverySegmentOfAPathIsReadAndANearNameIsAnotherFile
// and fails these.
func TestAC5AProtectedNameIsReadWhereANearNameComesAfterIt(t *testing.T) {
	for _, path := range []string{
		".claude/x/.claudex/s", "sub/.claude/x/.claudex/s", "sub/.claude/skills/.claude-plugin/s",
		"sub/.agents/skills/.agents-cache/s", ".agents/x/.agentsx/s",
		"sub/CLAUDE.md/x/CLAUDE.md.bak", "sub/AGENTS.md/x/AGENTS.md.bak", "sub/CLAUDE.local.md/x/CLAUDE.local.md.bak",
	} {
		agRefusedAsProtected(t, "AC-5", path)
	}
}

// agAboveRule is the rule a write to an instruction file in a directory above
// the project is refused under (AC-13).
const agAboveRule = "instructions-above-the-project"

// agWithAboveProject is r with Request.AboveProject set, the field this story
// adds for AC-13: the path is in a directory above the project (the one the
// project is in, or one above that, the person's home among them), as the
// caller resolved it. Which names there are instruction files is the rule's to
// say. Set by name, so that the package compiles before the field exists;
// until then r comes back as it was.
func agWithAboveProject(r Request) Request {
	f := reflect.ValueOf(&r).Elem().FieldByName("AboveProject")
	if f.IsValid() && f.Kind() == reflect.Bool {
		f.SetBool(true)
	}
	return r
}

// AC-13: during a story, a file tool's write to a CLAUDE.md, CLAUDE.local.md
// or AGENTS.md in a directory above the project is refused by its own rule,
// ahead of write-outside-repository, with a fix that says Claude Code loads
// instruction files from every directory above the one it starts in. Another
// file up there, and an instruction file in a directory that is not above the
// project, stay write-outside-repository's.
func TestAC13AFileToolsWriteToAnInstructionFileAboveTheProjectIsRefused(t *testing.T) {
	if f, ok := reflect.TypeFor[Request]().FieldByName("AboveProject"); !ok || f.Type.Kind() != reflect.Bool {
		t.Fatal("AC-13 policy.Request has no AboveProject bool, through which the caller says a path is in a " +
			"directory above the project, as ClaudeConfig says a path is in ~/.claude")
	}
	for _, path := range []string{
		"/home/u/CLAUDE.md", "/home/u/code/AGENTS.md", "/home/u/CLAUDE.local.md", "/CLAUDE.md", "C:/Users/u/AGENTS.md",
	} {
		for _, tool := range agWriters {
			got := Evaluate(agWithAboveProject(Request{Tool: tool, Agent: "sdlc-implementer", Path: path, Outside: true, Story: "A-1"}))
			if got.Rule != agAboveRule {
				t.Errorf("AC-13 %s to %s: rule %q, allowed %v; want %s", tool, path, got.Rule, got.Allowed, agAboveRule)
				continue
			}
			said := strings.ToLower(got.Reason + " " + got.Route)
			if !strings.Contains(said, "every directory above") || !strings.Contains(said, "claude code") {
				t.Errorf("AC-13 the refusal does not say Claude Code loads instruction files from every directory above the one it starts in: %s", got.Message())
			}
		}
	}
	for _, r := range []Request{
		agWithAboveProject(Request{Tool: "Write", Path: "/home/u/notes.md", Outside: true, Story: "A-1"}),
		agWithAboveProject(Request{Tool: "Write", Path: "/home/u/CLAUDE.md.bak", Outside: true, Story: "A-1"}),
		{Tool: "Write", Path: "/home/u/other/CLAUDE.md", Outside: true, Story: "A-1"},
		{Tool: "Write", Path: "/home/u/code/project2/AGENTS.md", Outside: true, Story: "A-1"},
	} {
		if got := Evaluate(r); got.Rule != "write-outside-repository" {
			t.Errorf("AC-13 %s is not an instruction file above the project and was refused by %q", r.Path, got.Rule)
		}
	}
	if got := Evaluate(agWithAboveProject(Request{Tool: "Read", Path: "/home/u/CLAUDE.md", Outside: true, Story: "A-1"})); !got.Allowed {
		t.Errorf("AC-13 reading an instruction file above the project was refused by %s", got.Rule)
	}
}

// AC-13, design review round 3 (red team's gaming vectors): the name of an
// instruction file above the project is compared folded, as the project's own
// is, so <home>/Agents.MD and claude.md count; and one in the .claude of a
// directory above counts as in that directory, as the caller marks it. A near
// name in another case is still another file.
func TestAC13AnInstructionFileAboveTheProjectIsKnownInAnyCase(t *testing.T) {
	if f, ok := reflect.TypeFor[Request]().FieldByName("AboveProject"); !ok || f.Type.Kind() != reflect.Bool {
		t.Fatal("AC-13 policy.Request has no AboveProject bool, through which the caller says a path is in a " +
			"directory above the project")
	}
	for _, path := range []string{
		"/home/u/Agents.MD", "/home/u/code/claude.md", "/home/u/code/Claude.Local.MD", "/home/u/code/.claude/AGENTS.md",
		"C:/Users/u/agents.md",
	} {
		got := Evaluate(agWithAboveProject(Request{Tool: "Write", Agent: "sdlc-implementer", Path: path, Outside: true, Story: "A-1"}))
		if got.Rule != agAboveRule {
			t.Errorf("AC-13 Write to %s: rule %q, allowed %v; want %s", path, got.Rule, got.Allowed, agAboveRule)
		}
	}
	for _, path := range []string{"/home/u/Claude.md.bak", "/home/u/my-agents.md", "/home/u/code/.claude/settings.json"} {
		got := Evaluate(agWithAboveProject(Request{Tool: "Write", Agent: "sdlc-implementer", Path: path, Outside: true, Story: "A-1"}))
		if got.Rule == agAboveRule {
			t.Errorf("AC-13 %s is no instruction file and was refused as one", path)
		}
	}
}
