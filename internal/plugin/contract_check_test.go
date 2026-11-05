package plugin

// US-110: the conformance check for the agent contract.
//
// It lives in a test file because the plan, not the tests, decides production
// code; nothing outside internal/plugin's tests reads an agent today. A later
// story may move it into plugin.go unchanged, and every test here calls it
// through checkAgents, so that move is a rename.
//
// The check returns one breach per rule broken, and a breach names the file,
// the rule of the contract (with the acceptance criterion that set it) and what
// to change, in words a person can act on without reading this file (AC-14).
// The binary's side of the agreement is a binaryView, built in contract_test.go
// from the binary's own lists and accessors, so a fixture can hand the check a
// view of its own without touching the real one.

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
)

// breach is one line of the check's output.
type breach struct{ File, Rule, Change string }

func (b breach) String() string { return b.File + ": breaks " + b.Rule + ": " + b.Change }

// The rules, each named by the criterion that sets it. A test selects the
// breaches of one criterion by the prefix.
const (
	ruleName        = "AC-2 frontmatter: name"
	ruleDescription = "AC-2 frontmatter: description"
	ruleTools       = "AC-2 frontmatter: tools"
	ruleModel       = "AC-2 frontmatter: model"
	ruleMaxTurns    = "AC-2 frontmatter: maxTurns"
	ruleKindKey     = "AC-2 frontmatter: sdlc-kind"
	ruleGatesKey    = "AC-2 frontmatter: sdlc-gates"
	ruleTeamKey     = "AC-2 frontmatter: sdlc-team"
	ruleForbidden   = "AC-2 frontmatter: keys a plugin agent may not set"
	ruleSkills      = "AC-2 frontmatter: skills"
	ruleReadOnly    = "AC-2 frontmatter: a reviewer's and an advisor's tools"

	ruleBinaryEntry = "AC-3 agreement with the binary: the agent's entry"
	ruleBinaryKind  = "AC-3 agreement with the binary: sdlc-kind"
	ruleBinaryGates = "AC-3 agreement with the binary: sdlc-gates"
	ruleBinaryTeam  = "AC-3 agreement with the binary: sdlc-team"

	ruleHeading   = "AC-4 body: the heading"
	ruleGuard     = "AC-4 body: the guard paragraph"
	ruleMissing   = "AC-4 sections: a required section"
	ruleDuplicate = "AC-4 sections: a section given once"
	ruleOrder     = "AC-4 sections: the order"
	ruleExtra     = "AC-4 sections: where an agent's own section may sit"
	ruleEmpty     = "AC-4 sections: a section with something in it"
	ruleRoleBlock = "AC-4 Role: what a reviewer's block does"
	ruleRoleGate  = "AC-4 Role: the gates"

	ruleStaffed       = "AC-5 When it is staffed: what it says"
	ruleStaffedPM     = "AC-5 When it is staffed: the project manager carries each statement"
	ruleOutputsWorker = "AC-6 Outputs: the commands that store a worker's documents"

	ruleInputsWorktree = "AC-7 Inputs: the worktree paragraph"
	ruleInputsShared   = "AC-7 Inputs: one paragraph in every agent"
	ruleRulesShared    = "AC-7 Rules: the shared rules"
	ruleRulesReviewer  = "AC-7 Rules: a reviewer's own rule"

	ruleNever         = "AC-8 Never: what every agent never does"
	ruleNeverReviewer = "AC-8 Never: what a reviewer or an advisor also never does"

	ruleHandBackSays    = "AC-9 Hand-back: what it says"
	ruleHandBackExample = "AC-9 Hand-back: the example"
	ruleHandBackKey     = "AC-9 Hand-back: the keys of the example"

	ruleReviewCommand  = "AC-11 Outputs: the command that records a review"
	ruleReviewDocument = "AC-11 Outputs: the review document"

	ruleNoNote    = "AC-12 verdict: approve or block, never note"
	ruleBlocksOn  = "AC-12 verdict: what the reviewer blocks on"
	ruleNoModels  = "AC-13 body: no model, tier or provider"
	ruleRunbook   = "AC-10 runbook: hand-back fields"
	ruleNeedsKind = "AC-3 agreement with the binary: a kind the binary gives"
)

// binaryView is what the binary says about the agents, which the check holds
// each agent's declarations to.
type binaryView struct {
	Agents []string
	Kind   map[string]string         // "worker" or "reviewer"; none for an agent the binary gives no kind
	Gates  map[string][]string       // gate record keys, sorted
	Team   map[string]map[string]any // the entry the binary applies with no team section, as JSON reads it
	// Commands is each `sdlc` command a worker's Outputs must name, beyond the
	// four that make it a worker's: one `sdlc artifact write` per document the
	// binary's artifact table gives the agent, and the commands for the map and
	// the staffing, which that table does not hold.
	Commands map[string][]string
	Skills   map[string]bool // plugin skills; nil is not checked
}

// reviewerExtras are the parts of a review document that belong to one role
// (AC-11), by agent: a heading the document carries after its two tables, and
// for the red team the columns of the table under it.
var reviewerExtras = map[string]struct {
	heading string
	columns []string
	mention string
}{
	"architect":     {heading: "Criteria coverage"},
	"code-reviewer": {heading: "Criteria walked"},
	"red-team":      {heading: "Gaming vectors", columns: []string{"Vector", "Test it would fool", "How to tell"}},
	"verifier":      {mention: "VERIFICATION.md"},
}

var (
	guardSentence = "If it does not, stop, and say that this agent only works the gates of an sdlc story " +
		"a person started with `/sdlc:next`. Do nothing else."
	guardStarts = "it starts with `sdlc:next runbook` and names the story"

	guardPhrases = []string{
		"Works only within a story a person started with /sdlc:next",
		"not for general review, security, performance, testing, planning, analysis or retro requests",
	}

	requiredSections = []string{"Role", "When it is staffed", "Inputs", "Procedure", "Outputs", "Never", "Hand-back", "Rules"}

	// The keys a plugin agent may not set, from AC-2.
	forbiddenKeys = []string{"effort", "permissionMode", "hooks", "mcpServers", "initialPrompt"}

	baseHandBackKeys     = []string{"agent", "story", "gate", "status", "documents", "blocked_on", "unchecked", "addressed_me", "details"}
	reviewerHandBackKeys = []string{"verdict", "blocking", "notes"}

	reviewColumns = []string{"Finding", "Where", "Why it matters", "What would fix it"}

	// A model id is claude- after a space, a quote or a bracket: a path such as
	// .claude-plugin is not one.
	modelWords = regexp.MustCompile("(?i)\\b(haiku|sonnet|opus|fable|anthropic|openai|gemini)\\b|(?:^|[\\s\"'`(])claude-[a-z0-9]")

	// A reviewer tells another to record a note: the verdict word in a command,
	// in the backticks the definitions put a verdict in, or in a JSON example.
	noteVerdict = regexp.MustCompile("`note`|\"verdict\":\\s*\"note\"|\\*\\*Verdict:\\*\\*[^\\n]*\\bnote\\b")

	blocksOn = regexp.MustCompile(`(?i)\bblocks? (only )?on\b[^.]{20,}`)

	leftOut = regexp.MustCompile(`(?i)\b(left|leave|leaves)\b[^.]{0,30}\bout\b`)

	useIt = regexp.MustCompile(`(^|[.!?]\s+)Use it\b`)
)

// fold is text with its white space folded, since prose wraps wherever it wraps.
func fold(s string) string { return strings.Join(strings.Fields(s), " ") }

// section is one `## ` part of an agent's body.
type section struct{ Heading, Text string }

// splitBody cuts a body at its `## ` headings, leaving a heading inside a code
// fence where it is: a reviewer's example document has `## Blocking` in it.
func splitBody(body string) (head string, secs []section) {
	var headLines []string
	inFence := false
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		} else if !inFence && strings.HasPrefix(line, "## ") {
			secs = append(secs, section{Heading: strings.TrimSpace(line[3:])})
			continue
		}
		if len(secs) == 0 {
			headLines = append(headLines, line)
		} else {
			secs[len(secs)-1].Text += line + "\n"
		}
	}
	return strings.Join(headLines, "\n"), secs
}

func sectionNamed(secs []section, name string) (section, bool) {
	for _, s := range secs {
		if s.Heading == name {
			return s, true
		}
	}
	return section{}, false
}

// fencedBlocks are the contents of the code fences opened with the info string.
func fencedBlocks(text, info string) []string {
	var out []string
	var cur []string
	open := false
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !open && strings.HasPrefix(trimmed, "```") && strings.TrimSpace(strings.TrimPrefix(trimmed, "```")) == info:
			open, cur = true, nil
		case open && strings.HasPrefix(trimmed, "```"):
			open = false
			out = append(out, strings.Join(cur, "\n"))
		case open:
			cur = append(cur, line)
		}
	}
	return out
}

// paragraphs splits text at blank lines.
func paragraphs(text string) []string {
	var out []string
	for p := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return out
}

// listItems are the `- ` items of a section, each with its continuation lines.
func listItems(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			out = append(out, line[2:])
		case strings.TrimSpace(line) == "":
		case len(out) > 0 && strings.HasPrefix(line, " "):
			out[len(out)-1] += " " + strings.TrimSpace(line)
		}
	}
	return out
}

// The breaches of one file, collected.
type found struct {
	file string
	list []breach
}

func (f *found) addf(rule, format string, args ...any) {
	f.list = append(f.list, breach{File: f.file, Rule: rule, Change: fmt.Sprintf(format, args...)})
}

func fileBase(c Component) string { return strings.TrimSuffix(path.Base(c.Path), ".md") }

// checkAgents holds every agent in the set to the contract, and the set to the
// binary and to itself.
func checkAgents(agents []Component, view binaryView) []breach {
	var out []breach
	shipped := map[string]bool{}
	for _, c := range agents {
		shipped[fileBase(c)] = true
		out = append(out, checkAgent(c, view)...)
	}
	for _, name := range view.Agents {
		if !shipped[name] {
			out = append(out, breach{File: "plugin/agents/" + name + ".md", Rule: ruleBinaryEntry,
				Change: "the binary lists the agent " + name + " (model.Agents) and the plugin ships no such file: add " +
					"plugin/agents/" + name + ".md, or take the agent out of the binary's list"})
		}
	}
	out = append(out, checkShared(agents)...)
	out = append(out, checkStaffedCopies(agents)...)
	return out
}

// effectiveKind is the kind the rules about a body apply: the binary's where it
// has one, so that a wrong declaration is the one breach and not the cause of
// a dozen, and otherwise the agent's own.
func effectiveKind(c Component, base string, view binaryView) string {
	if k := view.Kind[base]; k != "" {
		return k
	}
	return c.Front["sdlc-kind"]
}

func checkAgent(c Component, view binaryView) []breach {
	f := &found{file: c.Path}
	base := fileBase(c)
	checkFrontmatter(f, c, base, view)
	checkAgreement(f, c, base, view)
	head, secs := splitBody(c.Body)
	checkHead(f, head)
	checkSections(f, secs)
	kind := effectiveKind(c, base, view)
	checkRole(f, secs, kind, c)
	checkStaffed(f, secs, kind)
	checkInputs(f, secs)
	checkNever(f, secs, kind)
	checkHandBack(f, secs, kind)
	checkOutputs(f, secs, kind, base, view)
	checkRulesOwn(f, secs, kind)
	checkVerdicts(f, c, kind)
	if m := modelWords.FindString(c.Body); m != "" {
		f.addf(ruleNoModels, "the body names %q: take out every model, model tier and provider, so that what the agent is "+
			"asked to do does not depend on which model runs it", m)
	}
	return f.list
}

func checkFrontmatter(f *found, c Component, base string, view binaryView) {
	if c.Front["name"] != base {
		f.addf(ruleName, "`name:` is %q and the file is %s.md: set `name: %s`", c.Front["name"], base, base)
	}
	checkDescription(f, c.Front["description"])
	tools := c.List("tools")
	if len(tools) == 0 || c.Front["tools"] == "*" {
		f.addf(ruleTools, "`tools:` is not an explicit list: name each tool the agent uses, such as `tools: Read, Grep, Glob, Bash`")
	}
	if c.Front["model"] != "inherit" {
		f.addf(ruleModel, "`model:` is %q: set `model: inherit`, since which model runs is the project's choice", c.Front["model"])
	}
	if n, err := strconv.Atoi(c.Front["maxTurns"]); err != nil || n <= 0 {
		f.addf(ruleMaxTurns, "`maxTurns:` is %q: set it to a whole number above 0", c.Front["maxTurns"])
	}
	for _, key := range forbiddenKeys {
		if _, ok := c.Front[key]; ok {
			f.addf(ruleForbidden, "`%s:` is set: remove it, since a plugin agent may not set it", key)
		}
	}
	if skills, ok := c.Front["skills"]; ok && view.Skills != nil {
		for _, s := range c.List("skills") {
			if !view.Skills[s] {
				f.addf(ruleSkills, "`skills:` names %q, which is not a skill of the plugin: name one under plugin/skills, or remove it (%q)", s, skills)
			}
		}
	}
	kind := c.Front["sdlc-kind"]
	switch kind {
	case "worker", "reviewer", "advisor":
	case "":
		f.addf(ruleKindKey, "there is no `sdlc-kind:`: add one of worker, reviewer or advisor")
	default:
		f.addf(ruleKindKey, "`sdlc-kind:` is %q: it is one of worker, reviewer or advisor", kind)
	}
	if kind == "reviewer" || kind == "advisor" {
		for _, banned := range []string{"Write", "Edit"} {
			if slices.Contains(tools, banned) {
				f.addf(ruleReadOnly, "a %s lists %s in `tools:`: take it out, since it records only through `sdlc review add`", kind, banned)
			}
		}
	}
	checkGatesKey(f, c)
	checkTeamKey(f, c)
}

func checkDescription(f *found, d string) {
	last := -1
	for _, phrase := range guardPhrases {
		at := strings.Index(d, phrase)
		if at < 0 {
			f.addf(ruleDescription, "the description does not say %q: add it, as the first words of the description", phrase)
		}
		last = max(last, at)
	}
	if loc := useIt.FindStringIndex(d); loc == nil || loc[0] < last {
		f.addf(ruleDescription, "the description has no sentence beginning `Use it` after the two guard phrases: end it with "+
			"one that says when to use the agent")
	}
}

func checkGatesKey(f *found, c Component) {
	gates := c.List("sdlc-gates")
	if len(gates) == 0 {
		f.addf(ruleGatesKey, "there is no `sdlc-gates:`: list the gate record keys at which the runbook delegates to the agent, such as `sdlc-gates: design_review`")
	}
	for _, g := range gates {
		if !model.Gate(g).Valid() {
			f.addf(ruleGatesKey, "`sdlc-gates:` names %q, which is not a gate record key: use one of %s", g, model.GateNames())
		}
	}
}

func checkTeamKey(f *found, c Component) {
	raw, ok := c.Front["sdlc-team"]
	if !ok || raw == "" {
		f.addf(ruleTeamKey, "there is no `sdlc-team:`: add the agent's default entry in `team.agents`, as one line of JSON")
		return
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var entry config.AgentEntry
	if err := dec.Decode(&entry); err != nil {
		f.addf(ruleTeamKey, "`sdlc-team:` is not a JSON object in the shape of `team.agents.<name>` (%v): write it on one line, "+
			"with only the keys that section takes", err)
	}
}

// checkAgreement holds the declarations to what the binary applies.
func checkAgreement(f *found, c Component, base string, view binaryView) {
	if !slices.Contains(view.Agents, base) {
		f.addf(ruleBinaryEntry, "the binary has no agent called %s (model.Agents): add its entry to the binary, or take the file out of plugin/agents", base)
		return
	}
	// A declaration that is missing is a declaration that disagrees: the agent
	// says nothing where the binary says something.
	kind := c.Front["sdlc-kind"]
	switch want := view.Kind[base]; {
	case want == "":
		f.addf(ruleNeedsKind, "the binary gives %s no kind (it is neither in the review roster nor a gate doer): add its row to the binary", base)
	case kind != want:
		f.addf(ruleBinaryKind, "agent %s: `sdlc-kind` is %q and the binary says %q: set `sdlc-kind: %s`", base, kind, want, want)
	}
	got := slices.Clone(c.List("sdlc-gates"))
	sort.Strings(got)
	got = slices.Compact(got)
	if want := view.Gates[base]; !slices.Equal(got, want) {
		f.addf(ruleBinaryGates, "agent %s: `sdlc-gates` is [%s] and the binary says [%s]: set `sdlc-gates: %s`",
			base, strings.Join(got, ", "), strings.Join(want, ", "), strings.Join(want, ", "))
	}
	var declared map[string]any
	have := c.Front["sdlc-team"]
	if json.Unmarshal([]byte(have), &declared) == nil {
		have = compactJSON(declared)
	}
	if want := compactJSON(view.Team[base]); want != have {
		f.addf(ruleBinaryTeam, "agent %s: `sdlc-team` is %s and the binary applies %s when the project has no team section: set `sdlc-team: %s`",
			base, have, want, want)
	}
}

// compactJSON is the value as one line of JSON with its keys sorted, which is
// how two spellings of the same object are compared.
func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func checkHead(f *found, head string) {
	paras := paragraphs(head)
	if len(paras) == 0 || !strings.HasPrefix(paras[0], "# ") || strings.HasPrefix(paras[0], "## ") ||
		strings.Contains(paras[0], "\n") || strings.TrimSpace(paras[0][2:]) == "" {
		f.addf(ruleHeading, "the body does not begin with a `# ` heading that names the role: start it with one, such as `# Code review`")
		return
	}
	guard := ""
	if len(paras) > 1 {
		guard = fold(paras[1])
	}
	if !strings.Contains(guard, guardSentence) || !strings.Contains(guard, guardStarts) {
		f.addf(ruleGuard, "the paragraph after the heading is not the guard: it must say the brief starts with `sdlc:next runbook` "+
			"and names the story, and that the agent otherwise stops (%q)", guardSentence)
	}
	if len(paras) > 2 {
		f.addf(ruleGuard, "there is text between the guard paragraph and the first `## ` section: move it into the `Role` section")
	}
}

func words(s string) int { return len(strings.Fields(s)) }

func checkSections(f *found, secs []section) {
	index := map[string][]int{}
	for i, s := range secs {
		index[s.Heading] = append(index[s.Heading], i)
	}
	last, lastName := -1, ""
	for _, name := range requiredSections {
		at := index[name]
		switch {
		case len(at) == 0:
			f.addf(ruleMissing, "there is no `## %s` section: add it, in the order %s", name, strings.Join(requiredSections, ", "))
			continue
		case len(at) > 1:
			f.addf(ruleDuplicate, "`## %s` appears %d times: keep one", name, len(at))
		}
		if at[0] < last {
			f.addf(ruleOrder, "`## %s` comes before `## %s`: the order is %s", name, lastName, strings.Join(requiredSections, ", "))
		} else {
			last, lastName = at[0], name
		}
		if words(secs[at[0]].Text) < 3 {
			f.addf(ruleEmpty, "`## %s` is empty: say what the agent's %s is", name, strings.ToLower(name))
		}
	}
	procedure, outputs := firstIndex(index, "Procedure"), firstIndex(index, "Outputs")
	for i, s := range secs {
		if slices.Contains(requiredSections, s.Heading) {
			continue
		}
		if procedure < 0 || outputs < 0 || i < procedure || i > outputs {
			f.addf(ruleExtra, "`## %s` is not one of the contract's sections and sits where only those may: move it between `## Procedure` and `## Outputs`", s.Heading)
		}
	}
}

func firstIndex(index map[string][]int, name string) int {
	if at := index[name]; len(at) > 0 {
		return at[0]
	}
	return -1
}

func checkRole(f *found, secs []section, kind string, c Component) {
	role, ok := sectionNamed(secs, "Role")
	if !ok {
		return
	}
	text := strings.ToLower(fold(role.Text))
	if !strings.Contains(text, "gate") {
		f.addf(ruleRoleGate, "`## Role` does not say at which gates the agent works: name them")
	}
	if kind != "reviewer" {
		return
	}
	var lacks []string
	for _, need := range []string{"block", "stop", "by default", "`team`"} {
		if !strings.Contains(text, need) {
			lacks = append(lacks, need)
		}
	}
	if len(lacks) > 0 {
		f.addf(ruleRoleBlock, "`## Role` of %s does not say whether its block stops the gate by default and that the project's `team` setting "+
			"decides it in the end (it lacks %s)", c.Name, strings.Join(lacks, ", "))
	}
}

func checkStaffed(f *found, secs []section, kind string) {
	s, ok := sectionNamed(secs, "When it is staffed")
	if !ok {
		return
	}
	text := strings.ToLower(fold(s.Text))
	var lacks []string
	if !leftOut.MatchString(text) {
		lacks = append(lacks, "when the project manager leaves the agent out, or that it is never left out")
	}
	if !strings.Contains(text, "project manager") || !strings.Contains(text, "disabled") || !strings.Contains(text, "every enabled agent") {
		lacks = append(lacks, "that with the project manager disabled every enabled agent takes part")
	}
	if kind == "worker" && (!strings.Contains(text, "by hand") || !strings.Contains(text, "person")) {
		lacks = append(lacks, "that with the agent disabled the person does its gate by hand")
	}
	if len(lacks) > 0 {
		f.addf(ruleStaffed, "`## When it is staffed` does not say %s", strings.Join(lacks, ", nor "))
	}
}

func checkInputs(f *found, secs []section) {
	s, ok := sectionNamed(secs, "Inputs")
	if !ok {
		return
	}
	first := inputsParagraph(s)
	text := strings.ToLower(first)
	for _, need := range []string{"worktree", "backlog", ".sdlc/stories/<id>/"} {
		if !strings.Contains(text, need) {
			f.addf(ruleInputsWorktree, "the first paragraph of `## Inputs` does not say where the code is and where the backlog and the story's "+
				"documents are when the story has a worktree (it lacks %q): begin with that paragraph", need)
		}
	}
}

func inputsParagraph(s section) string {
	paras := paragraphs(s.Text)
	if len(paras) == 0 {
		return ""
	}
	return fold(paras[0])
}

// neverItems are what every agent never does, and the words that say it.
var neverItems = []struct{ label, needle string }{
	{"`sdlc approve`", "sdlc approve"},
	{"`sdlc unfreeze`", "sdlc unfreeze"},
	{"`sdlc ack`", "sdlc ack"},
	{"`sdlc remap`", "sdlc remap"},
	{"`sdlc staffing assign`", "sdlc staffing assign"},
	{"committing", "commit"},
	{"editing a frozen test", "frozen test"},
	{"editing `.sdlc/config.json`", ".sdlc/config.json"},
	{"editing `CLAUDE.md`", "CLAUDE.md"},
	{"editing anything under `.sdlc/state/`", ".sdlc/state/"},
}

func checkNever(f *found, secs []section, kind string) {
	s, ok := sectionNamed(secs, "Never")
	if !ok {
		return
	}
	text := fold(s.Text)
	if !strings.Contains(strings.ToLower(text), "never") {
		f.addf(ruleNever, "`## Never` does not say \"never\": state what the agent never does")
	}
	for _, item := range neverItems {
		if !strings.Contains(text, item.needle) {
			f.addf(ruleNever, "`## Never` does not say the agent never does %s: add it", item.label)
		}
	}
	if kind != "reviewer" && kind != "advisor" {
		return
	}
	lower := strings.ToLower(text)
	need := []string{"code", "tests", "documents"}
	if kind == "reviewer" {
		need = append(need, "own review")
	}
	for _, w := range need {
		if !strings.Contains(lower, w) {
			f.addf(ruleNeverReviewer, "`## Never` of a %s does not say it never changes code, tests or documents other than its own review (it lacks %q)", kind, w)
		}
	}
}

func checkHandBack(f *found, secs []section, kind string) {
	s, ok := sectionNamed(secs, "Hand-back")
	if !ok {
		return
	}
	lower := strings.ToLower(fold(s.Text))
	if !strings.Contains(lower, "one json object") || !strings.Contains(lower, "nothing else") {
		f.addf(ruleHandBackSays, "`## Hand-back` does not say that the final message is one JSON object and nothing else: say so")
	}
	examples := fencedBlocks(s.Text, "json")
	if len(examples) == 0 {
		f.addf(ruleHandBackExample, "`## Hand-back` has no example in a ```json block: give one that parses")
	}
	for _, ex := range examples {
		var got map[string]any
		if err := json.Unmarshal([]byte(ex), &got); err != nil {
			f.addf(ruleHandBackExample, "the example in `## Hand-back` does not parse as one JSON object (%v): fix it", err)
			continue
		}
		checkHandBackKeys(f, got, kind)
	}
}

func checkHandBackKeys(f *found, got map[string]any, kind string) {
	want := slices.Clone(baseHandBackKeys)
	if kind == "reviewer" {
		want = append(want, reviewerHandBackKeys...)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			f.addf(ruleHandBackKey, "the example has no `%s`: add it", key)
		}
	}
	for key := range got {
		if !slices.Contains(want, key) {
			f.addf(ruleHandBackKey, "`%s` sits outside `details`: move every field particular to the role into `details`", key)
		}
	}
	kinds := map[string]string{"agent": "string", "story": "string", "gate": "string", "status": "string",
		"documents": "array", "unchecked": "array", "addressed_me": "array", "details": "object"}
	for key, want := range kinds {
		if v, ok := got[key]; ok && jsonKind(v) != want {
			f.addf(ruleHandBackKey, "`%s` is %s in the example and must be %s", key, jsonKind(v), want)
		}
	}
	status, _ := got["status"].(string)
	if _, ok := got["status"]; ok && status != "done" && status != "blocked" {
		f.addf(ruleHandBackKey, "`status` is %q in the example: it is done or blocked", status)
	}
	if v, ok := got["blocked_on"]; ok {
		if k := jsonKind(v); k != "null" && k != "string" {
			f.addf(ruleHandBackKey, "`blocked_on` is %s in the example: it is null, or what stopped the agent", k)
		} else if (k == "null") != (status == "done") && status != "" {
			f.addf(ruleHandBackKey, "`blocked_on` and `status` disagree in the example: `done` goes with null, `blocked` with what stopped it")
		}
	}
	if kind == "reviewer" {
		checkReviewerKeys(f, got)
	}
}

func checkReviewerKeys(f *found, got map[string]any) {
	if v, ok := got["verdict"]; ok && v != "approve" && v != "block" {
		f.addf(ruleHandBackKey, "`verdict` is %v in the example: a reviewer records approve or block", v)
	}
	for _, key := range []string{"blocking", "notes"} {
		if v, ok := got[key]; ok && jsonKind(v) != "number" {
			f.addf(ruleHandBackKey, "`%s` is %s in the example and must be a count", key, jsonKind(v))
		}
	}
}

func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	default:
		return "object"
	}
}

func checkOutputs(f *found, secs []section, kind, base string, view binaryView) {
	s, ok := sectionNamed(secs, "Outputs")
	if !ok {
		return
	}
	text := fold(s.Text)
	if kind == "worker" {
		stores := false
		for _, cmd := range []string{"sdlc artifact write", "sdlc staffing set", "sdlc review add", "sdlc codemap write"} {
			stores = stores || strings.Contains(text, cmd)
		}
		if !stores {
			f.addf(ruleOutputsWorker, "`## Outputs` names no `sdlc artifact write`, `sdlc staffing set`, `sdlc review add` or `sdlc codemap write` "+
				"command: name each document the gate needs and the command that stores it, so that a person can do the gate by hand")
		}
	}
	for _, cmd := range view.Commands[base] {
		if !strings.Contains(text, cmd) {
			f.addf(ruleOutputsWorker, "`## Outputs` does not name `%s`: name the command that stores each document and record the gate needs", cmd)
		}
	}
	if kind == "reviewer" {
		checkReviewExample(f, s, base)
	}
}

// checkReviewExample reads the command that records a review, and the document
// it is given, as AC-11 lays them out.
func checkReviewExample(f *found, s section, base string) {
	lines := strings.Split(s.Text, "\n")
	var verdict string
	var doc []string
	seen := false
	for i, line := range lines {
		w := strings.Fields(line)
		if len(w) < 3 || w[0] != "sdlc" || w[1] != "review" || w[2] != "add" {
			continue
		}
		seen = true
		v := checkReviewCommand(f, w)
		if strings.Contains(line, "<<'SDLC_DOCUMENT'") && doc == nil {
			verdict = v
			doc = []string{}
			for _, l := range lines[i+1:] {
				if strings.TrimSpace(l) == "SDLC_DOCUMENT" {
					break
				}
				doc = append(doc, l)
			}
		}
	}
	if !seen {
		f.addf(ruleReviewCommand, "`## Outputs` has no `sdlc review add <gate> <name> <verdict>` command: give the one that records the review")
		return
	}
	if doc == nil {
		f.addf(ruleReviewDocument, "the `sdlc review add` command in `## Outputs` is not given the review document on a <<'SDLC_DOCUMENT' heredoc: show the document")
		return
	}
	for _, problem := range reviewDocumentProblems(doc, verdict, base) {
		f.addf(ruleReviewDocument, "the example review document %s", problem)
	}
}

// checkReviewCommand holds the command line to AC-11 and the flags the binary
// refuses a block without, and returns the verdict word it records.
func checkReviewCommand(f *found, w []string) string {
	if len(w) < 6 {
		f.addf(ruleReviewCommand, "`sdlc review add` has no `<gate> <name> <verdict>` after it: write `sdlc review add <gate> <name> <verdict>`")
		return ""
	}
	if !regexp.MustCompile(`^<[a-z-]+>$`).MatchString(w[4]) {
		f.addf(ruleReviewCommand, "`sdlc review add` records under %q: record under `<name>`, the name the brief gives the reviewer", w[4])
	}
	if v := w[5]; v != "approve" && v != "block" && v != "<verdict>" {
		f.addf(ruleReviewCommand, "`sdlc review add` records the verdict %q: it is approve or block", v)
	}
	for _, flag := range []string{"--important", "--nits"} {
		if !slices.Contains(w, flag) {
			f.addf(ruleReviewCommand, "`sdlc review add` has no %s: add it, since a block without --important is refused", flag)
		}
	}
	return w[5]
}

// reviewDocumentProblems reads the document in the order AC-11 sets: the title,
// the verdict, an optional preamble, the two tables, then the role's parts.
func reviewDocumentProblems(doc []string, verdict, base string) []string {
	var out []string
	next := func(from int) int {
		for ; from < len(doc); from++ {
			if strings.TrimSpace(doc[from]) != "" {
				return from
			}
		}
		return len(doc)
	}
	i := next(0)
	if i == len(doc) || !regexp.MustCompile(`^# .+ review — <ID>$`).MatchString(strings.TrimSpace(doc[i])) {
		out = append(out, "does not begin with `# <role> review — <ID>`")
	}
	if i < len(doc) {
		i = next(i + 1)
	}
	if i == len(doc) || !strings.HasPrefix(doc[i], "**Verdict:** ") {
		out = append(out, "has no `**Verdict:** ` line straight after the title")
	} else if got := strings.TrimSpace(strings.TrimPrefix(doc[i], "**Verdict:** ")); got != verdict {
		out = append(out, fmt.Sprintf("says `**Verdict:** %s` and the command records %s: write the same verdict", got, verdict))
	}
	from := 0
	for _, name := range []string{"## Blocking", "## Notes"} {
		at := slices.IndexFunc(doc[from:], func(l string) bool { return strings.TrimSpace(l) == name })
		if at < 0 {
			out = append(out, fmt.Sprintf("has no `%s` after the verdict, or has it out of order (the order is Blocking, then Notes)", name))
			continue
		}
		from += at + 1
		if !tableHasColumns(doc[from:], reviewColumns) {
			out = append(out, fmt.Sprintf("has no table under `%s` with the columns %s", name, strings.Join(reviewColumns, ", ")))
		}
	}
	return append(out, roleExtraProblems(doc[from:], base)...)
}

func roleExtraProblems(rest []string, base string) []string {
	extra, ok := reviewerExtras[base]
	if !ok {
		return nil
	}
	var out []string
	if extra.mention != "" && !strings.Contains(strings.Join(rest, "\n"), extra.mention) {
		out = append(out, "does not point to "+extra.mention+" after its two tables")
	}
	if extra.heading != "" {
		at := slices.IndexFunc(rest, func(l string) bool {
			return strings.HasPrefix(l, "## ") && strings.Contains(l, extra.heading)
		})
		switch {
		case at < 0:
			out = append(out, "has no `## "+extra.heading+"` section after its two tables")
		case extra.columns != nil && !tableHasColumns(rest[at+1:], extra.columns):
			out = append(out, "has no table under `## "+extra.heading+"` with the columns "+strings.Join(extra.columns, ", "))
		}
	}
	return out
}

// tableHasColumns reports whether the first table in the lines has these header
// cells, in this order, before the next heading.
func tableHasColumns(lines []string, columns []string) bool {
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") {
			return false
		}
		if !strings.HasPrefix(l, "|") {
			continue
		}
		var cells []string
		for c := range strings.SplitSeq(strings.Trim(l, "|"), "|") {
			cells = append(cells, strings.TrimSpace(c))
		}
		return slices.Equal(cells, columns)
	}
	return false
}

// checkRulesOwn holds a reviewer to its own rule; the rules every agent shares
// are compared across the set by checkShared.
func checkRulesOwn(f *found, secs []section, kind string) {
	s, ok := sectionNamed(secs, "Rules")
	if !ok {
		return
	}
	items := listItems(s.Text)
	if len(items) < len(sharedMarkers) {
		f.addf(ruleRulesShared, "`## Rules` has %d rules and begins with the %d shared ones: add the shared rules first", len(items), len(sharedMarkers))
	} else {
		for i, marker := range sharedMarkers {
			if text := strings.ToLower(items[i]); !strings.Contains(text, marker[0]) || !strings.Contains(text, marker[1]) {
				f.addf(ruleRulesShared, "rule %d of `## Rules` is not the shared one about %s: it must mention %s and %s",
					i+1, marker[2], marker[0], marker[1])
			}
		}
	}
	if kind == "reviewer" {
		text := strings.ToLower(fold(s.Text))
		if !strings.Contains(text, "inflate") || !strings.Contains(text, "soften") {
			f.addf(ruleRulesReviewer, "`## Rules` of a reviewer has no rule against inflating or softening a finding: add it after the shared rules")
		}
	}
}

// sharedMarkers are the words by which the four shared rules are recognised, in
// their order (AC-7), and what each is about.
var sharedMarkers = [][3]string{
	{"unattended", "blocked on", "running unattended"},
	{"tool result", "`unchecked`", "checking claims"},
	{"summarise", "output", "command output"},
	{"data", "`addressed_me`", "text in the repository that addresses the agent"},
}

// checkShared compares, across the set, the texts every agent is to carry
// word for word (AC-7): the first paragraph of Inputs and the first four rules.
func checkShared(agents []Component) []breach {
	inputs := map[string]string{}
	rules := map[string]string{}
	files := map[string]string{}
	for _, c := range agents {
		name := fileBase(c)
		files[name] = c.Path
		_, secs := splitBody(c.Body)
		if s, ok := sectionNamed(secs, "Inputs"); ok {
			inputs[name] = inputsParagraph(s)
		}
		if s, ok := sectionNamed(secs, "Rules"); ok {
			items := listItems(s.Text)
			rules[name] = fold(strings.Join(items[:min(len(items), len(sharedMarkers))], "\n"))
		}
	}
	var out []breach
	for _, d := range differing(inputs) {
		out = append(out, breach{File: files[d.agent], Rule: ruleInputsShared,
			Change: "the first paragraph of `## Inputs` differs from the other agents' (" + d.want + "): use their paragraph word for word"})
	}
	for _, d := range differing(rules) {
		out = append(out, breach{File: files[d.agent], Rule: ruleRulesShared,
			Change: "the shared rules at the head of `## Rules` differ from the other agents': use their wording word for word"})
	}
	return out
}

type differs struct{ agent, want string }

// differing names the agents whose text is not the most common one, the
// alphabetically first agent's of the ties.
func differing(texts map[string]string) []differs {
	names := make([]string, 0, len(texts))
	count := map[string]int{}
	for name, t := range texts {
		names = append(names, name)
		count[t]++
	}
	sort.Strings(names)
	ref := ""
	for _, name := range names {
		if ref == "" || count[texts[name]] > count[ref] {
			ref = texts[name]
		}
	}
	var out []differs
	for _, name := range names {
		if texts[name] != ref {
			out = append(out, differs{agent: name, want: quoteStart(ref)})
		}
	}
	return out
}

func quoteStart(s string) string {
	if len(s) > 60 {
		s = s[:60] + "..."
	}
	return strconv.Quote(s)
}

// checkStaffedCopies holds the project manager's definition to carrying each
// agent's `When it is staffed` text, word for word, under the agent's name.
func checkStaffedCopies(agents []Component) []breach {
	var pm *Component
	for i := range agents {
		if fileBase(agents[i]) == "project-manager" {
			pm = &agents[i]
		}
	}
	if pm == nil {
		return nil
	}
	var out []breach
	for _, c := range agents {
		_, secs := splitBody(c.Body)
		s, ok := sectionNamed(secs, "When it is staffed")
		if !ok || strings.TrimSpace(s.Text) == "" {
			continue
		}
		name := fileBase(c)
		scope, found := scopeUnder(pm.Body, name)
		switch {
		case !found:
			out = append(out, breach{File: pm.Path, Rule: ruleStaffedPM,
				Change: "the project manager's definition has no heading for " + name + ": add one named " + name +
					" that carries the agent's `When it is staffed` text"})
		case !strings.Contains(fold(scope), fold(s.Text)):
			out = append(out, breach{File: pm.Path, Rule: ruleStaffedPM,
				Change: "under " + name + " the project manager's definition does not carry the `When it is staffed` text of " +
					c.Path + " word for word: copy it from there"})
		}
	}
	return out
}

var headingLine = regexp.MustCompile("^(#{2,6}) +(.+?) *$")

// scopeUnder is the text under the heading that names the agent, to the next
// heading as high or higher.
func scopeUnder(body, name string) (string, bool) {
	var scope []string
	level := 0
	inFence := false
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		m := headingLine.FindStringSubmatch(line)
		if inFence || m == nil {
			if level > 0 {
				scope = append(scope, line)
			}
			continue
		}
		if level > 0 && len(m[1]) <= level {
			break
		}
		if level == 0 {
			title := strings.TrimPrefix(strings.Trim(m[2], "`"), "sdlc:")
			if title == name {
				level = len(m[1])
			}
			continue
		}
		scope = append(scope, line)
	}
	return strings.Join(scope, "\n"), level > 0
}

// checkVerdicts holds a reviewer to approve and block: no definition tells it
// to record a note, and each says what it blocks on (AC-12).
func checkVerdicts(f *found, c Component, kind string) {
	if kind != "reviewer" {
		return
	}
	for line := range strings.SplitSeq(c.Body, "\n") {
		if w := strings.Fields(line); len(w) > 5 && w[0] == "sdlc" && w[1] == "review" && w[2] == "add" && w[5] == "note" {
			f.addf(ruleNoNote, "the command %q records a note: record approve, or block, and take `note` out", strings.TrimSpace(line))
		}
	}
	if m := noteVerdict.FindString(c.Body); m != "" {
		f.addf(ruleNoNote, "the definition tells the reviewer to record a note (%q): a reviewer records approve or block, whether or not "+
			"the project lets its block stop the gate", strings.TrimSpace(m))
	}
	body := fold(c.Body)
	if !blocksOn.MatchString(body) {
		f.addf(ruleBlocksOn, "the definition does not say what the reviewer blocks on: add a sentence such as \"You block on ...\" that "+
			"names the findings its rules make blocking, since a person may set `can_block` true for any reviewer")
	}
	if !strings.Contains(body, "`approve`") || !strings.Contains(body, "`block`") {
		f.addf(ruleBlocksOn, "the definition does not say the reviewer records `block` for a blocking finding and `approve` otherwise")
	}
}
