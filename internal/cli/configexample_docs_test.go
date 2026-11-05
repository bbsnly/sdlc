package cli

// US-109, AC-14, the person's request at Gate 3 (2026-11-03): docs/configuration.md holds
// the whole file once, as an example with every setting at its default, and a `//`
// comment naming the other values a setting can take. The settings it must hold are read
// from the code, never listed here, so a key added later without the example fails these
// rows: the json tags of config.Config, what config.Default() gives, and what `sdlc init`
// spells out where Default leaves a setting empty (the commands, the team).

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// cfxRetired are the settings the team replaced. The code still has a field for each, so
// that it can refuse it (PLAN.md section 7), and the example leaves both out.
// TestAC14TheExampleHoldsTheTeamAndLeavesOutOnlyTheKeysItReplaced holds that the code
// refuses each with SDLC-E0087, so the list cannot hold a live setting.
var cfxRetired = map[string]any{
	"staffing":               map[string]any{"enabled": true},
	"reviews.gate7_advisory": true,
}

func cfxIsRetired(path string) bool {
	for r := range cfxRetired {
		if path == r || strings.HasPrefix(path, r+".") {
			return true
		}
	}
	return false
}

func cfxJoin(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

// cfxExample is the page's one ```jsonc block, the section it stands in and the text
// between that section's heading and the block.
type cfxExample struct{ body, heading, before string }

func cfxTheExample(t *testing.T) cfxExample {
	t.Helper()
	doc := page(t, "configuration.md")
	var blocks []string
	for _, f := range ccmFences(doc) {
		if f.lang == "jsonc" {
			blocks = append(blocks, f.body)
		}
	}
	if len(blocks) != 1 {
		t.Fatalf("AC-14 docs/configuration.md has %d ```jsonc blocks, want one example of the whole file", len(blocks))
	}
	at := strings.Index(doc, "```jsonc")
	start := strings.LastIndex(doc[:at], "\n## ")
	if start < 0 {
		t.Fatal("AC-14 the example of the whole file stands under no `## ` heading of its own")
	}
	heading, before, _ := strings.Cut(doc[start+len("\n## "):at], "\n")
	return cfxExample{body: blocks[0], heading: strings.TrimSpace(heading), before: before}
}

// cfxWithoutComments is the block as the file would be: a `//` outside a string, to the
// end of its line, taken out.
func cfxWithoutComments(body string) string {
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case inString:
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(body) && body[i+1] == '/':
			for i < len(body) && body[i] != '\n' {
				i++
			}
			if i < len(body) {
				b.WriteByte('\n')
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// cfxTypePaths are the settings config.Config's type has, dotted as the documentation
// spells them: a struct's fields by their json tags, a pointer's element, and for a map
// `*` for its key, then a struct value's fields. A list, raw JSON and anything else is
// one setting.
func cfxTypePaths(t reflect.Type, prefix string, out *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == reflect.TypeFor[json.RawMessage]():
		*out = append(*out, prefix)
	case t.Kind() == reflect.Struct:
		for f := range t.Fields() {
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !f.IsExported() || name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			cfxTypePaths(f.Type, cfxJoin(prefix, name), out)
		}
	case t.Kind() == reflect.Map:
		cfxTypePaths(t.Elem(), cfxJoin(prefix, "*"), out)
	default:
		*out = append(*out, prefix)
	}
}

// cfxLeaves are the values of a decoded file by their dotted settings, a list being one.
// A top-level `_` key is a comment, and a null is a setting left to the code's default.
func cfxLeaves(v any, prefix string, out map[string]any) {
	m, ok := v.(map[string]any)
	if !ok {
		if v != nil {
			out[prefix] = v
		}
		return
	}
	for k, x := range m {
		if prefix == "" && strings.HasPrefix(k, "_") {
			continue
		}
		cfxLeaves(x, cfxJoin(prefix, k), out)
	}
}

// cfxHas reports whether the decoded file holds the setting, `*` standing for any key.
func cfxHas(v any, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	if parts[0] == "*" {
		for _, x := range m {
			if cfxHas(x, parts[1:]) {
				return true
			}
		}
		return false
	}
	x, ok := m[parts[0]]
	return ok && cfxHas(x, parts[1:])
}

func cfxDecode(t *testing.T, what string, raw []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("AC-14 %s is not one JSON object: %v", what, err)
	}
	return v
}

// cfxInitWrites is what `sdlc init` writes for a project with no stack: every setting
// spelled out at its default, the commands and the team included.
func cfxInitWrites(t *testing.T) map[string]any {
	t.Helper()
	root := gitProject(t)
	mustRun(t, "init")
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	return cfxDecode(t, "what init writes", raw)
}

// The page holds the whole file once, under a heading of its own that the page's contents
// link to, says before it that the comments are for reading and the file is plain JSON,
// and the block, its comments taken out, loads as it stands, names nothing the code does
// not know, holds every setting the code has, and gives each the value it has by default.
func TestAC14TheConfigurationPageHasOneExampleOfEverySettingAtItsDefault(t *testing.T) {
	t.Parallel()
	ex := cfxTheExample(t)
	contents, _, _ := strings.Cut(page(t, "configuration.md"), "\n## ")
	anchor := strings.Map(func(r rune) rune {
		switch {
		case r == ' ':
			return '-'
		case r == '-', r == '_', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		}
		return -1
	}, strings.ToLower(ex.heading))
	if !strings.Contains(contents, "](#"+anchor+")") {
		t.Errorf("AC-14 the page's contents do not link to the example's section %q (#%s)", ex.heading, anchor)
	}
	before := strings.ToLower(stfFolded(ex.before))
	if !strings.Contains(before, "comment") || !strings.Contains(before, "plain json") {
		t.Errorf("AC-14 the text before the example does not say its comments are for reading and the file is plain JSON: %q", ex.before)
	}

	text := cfxWithoutComments(ex.body)
	root := t.TempDir()
	writeFile(t, root, ".sdlc/config.json", text)
	if _, err := config.Load(root); err != nil {
		t.Fatalf("AC-14 the example, its comments taken out, does not load: %v\n%s", err, text)
	}
	if unknown := config.UnknownKeys(root); len(unknown) > 0 {
		t.Errorf("AC-14 the example names settings the code does not know: %q", unknown)
	}
	example := cfxDecode(t, "the example", []byte(text))

	// What the code has: its type, its defaults, and what init spells out where the
	// defaults leave a setting empty.
	var typed []string
	cfxTypePaths(reflect.TypeFor[config.Config](), "", &typed)
	raw, err := json.Marshal(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	defaults := map[string]any{}
	cfxLeaves(cfxDecode(t, "config.Default()", raw), "", defaults)
	initWrites := map[string]any{}
	cfxLeaves(cfxInitWrites(t), "", initWrites)

	required := slices.Clone(typed)
	for _, leaves := range []map[string]any{defaults, initWrites} {
		for p := range leaves {
			required = append(required, p)
		}
	}
	slices.Sort(required)
	for _, p := range slices.Compact(required) {
		if cfxIsRetired(p) {
			continue
		}
		if !cfxHas(example, strings.Split(p, ".")) {
			t.Errorf("AC-14 the example leaves out %s", p)
		}
	}
	held := map[string]any{}
	cfxLeaves(example, "", held)
	for p, want := range defaults {
		if cfxIsRetired(p) {
			continue
		}
		if got, ok := held[p]; ok && !reflect.DeepEqual(got, want) {
			t.Errorf("AC-14 the example gives %s as %v, and its default is %v", p, got, want)
		}
	}
	// Where Default leaves a setting empty, init's value is the default it spells out.
	for p, want := range initWrites {
		if _, isDefault := defaults[p]; isDefault || cfxIsRetired(p) {
			continue
		}
		if got, ok := held[p]; ok && !reflect.DeepEqual(got, want) {
			t.Errorf("AC-14 the example gives %s as %v, and init writes %v", p, got, want)
		}
	}
}

// cfxCommentFor is the text a reader sees beside a setting in the block: its line, the
// comment lines just above it, and, where its value opens a list or an object the line
// does not close, the lines up to the close.
func cfxCommentFor(lines []string, at int) string {
	var parts []string
	for i := at - 1; i >= 0 && strings.HasPrefix(strings.TrimSpace(lines[i]), "//"); i-- {
		parts = append(parts, lines[i])
	}
	depth := 0
	for i := at; i < len(lines); i++ {
		parts = append(parts, lines[i])
		code := cfxWithoutComments(lines[i])
		depth += strings.Count(code, "{") + strings.Count(code, "[") - strings.Count(code, "}") - strings.Count(code, "]")
		if depth <= 0 {
			break
		}
	}
	return strings.Join(parts, "\n")
}

// cfxNames reports whether text names the value as a word of its own: `high` is not
// named by `xhigh`, nor `low` by `lowest`.
func cfxNames(text, value string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(value) + `($|[^A-Za-z0-9_-])`).MatchString(text)
}

// A setting whose value is one of a set the code holds says, beside it, every value of the
// set: the risk tiers the story schema allows, the gate keys gate_hours takes, and for a
// floor the models and efforts the team lists. Each set is read from the code. Booleans
// are not asked.
func TestAC14TheExampleNamesEveryValueASettingFromAFixedSetCanTake(t *testing.T) {
	t.Parallel()
	ex := cfxTheExample(t)
	lines := strings.Split(ex.body, "\n")

	schemaRaw, err := os.ReadFile(filepath.Join("..", "scaffold", "templates", "story.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tiers []string
	var findTiers func(v any)
	findTiers = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if rt, ok := x["risk_tier"].(map[string]any); ok {
				enum, _ := rt["enum"].([]any)
				for _, e := range enum {
					if tier, ok := e.(string); ok {
						tiers = append(tiers, tier)
					}
				}
			}
			for _, y := range x {
				findTiers(y)
			}
		case []any:
			for _, y := range x {
				findTiers(y)
			}
		}
	}
	findTiers(cfxDecode(t, "the story schema", schemaRaw))
	slices.Sort(tiers)
	tiers = slices.Compact(tiers)
	if len(tiers) < 2 {
		t.Fatalf("the story schema gives no set of risk tiers: %q", tiers)
	}
	var gates []string
	for _, g := range model.Gates {
		gates = append(gates, string(g))
	}
	team, _ := cfxInitWrites(t)["team"].(map[string]any)
	var models, efforts []string
	for _, l := range []struct {
		key string
		out *[]string
	}{{"models", &models}, {"efforts", &efforts}} {
		list, _ := team[l.key].([]any)
		for _, m := range list {
			if name, ok := m.(string); ok {
				*l.out = append(*l.out, name)
			}
		}
		if len(*l.out) == 0 {
			t.Fatalf("AC-14 init writes no team.%s to read the floor's set from", l.key)
		}
	}

	for _, s := range []struct {
		key, what string
		values    []string
	}{
		{"pre_commit_pause_tiers", "risk tier", tiers},
		{"gate_hours", "gate key", gates},
		{"lowest", "model", models},
		{"lowest", "effort", efforts},
	} {
		found := false
		for i, line := range lines {
			if !strings.Contains(cfxWithoutComments(line), `"`+s.key+`"`) {
				continue
			}
			found = true
			text := cfxCommentFor(lines, i)
			for _, v := range s.values {
				if !cfxNames(text, v) {
					t.Errorf("AC-14 beside %s in the example, the %s %q is not named:\n%s", s.key, s.what, v, text)
				}
			}
		}
		if !found {
			t.Errorf("AC-14 the example has no %s", s.key)
		}
	}
}

// The team is in the example with every key an agent's entry takes, and the two settings
// the team replaced are not: each is refused under SDLC-E0087 where the example gives it.
func TestAC14TheExampleHoldsTheTeamAndLeavesOutOnlyTheKeysItReplaced(t *testing.T) {
	t.Parallel()
	text := cfxWithoutComments(cfxTheExample(t).body)
	example := cfxDecode(t, "the example", []byte(text))
	for _, p := range []string{
		"team.retries", "team.models", "team.efforts",
		"team.agents.*.enabled", "team.agents.*.can_block", "team.agents.*.instances",
		"team.agents.*.max_rounds", "team.agents.*.lowest", "team.agents.*.bring_back",
	} {
		if !cfxHas(example, strings.Split(p, ".")) {
			t.Errorf("AC-14 the example leaves out %s", p)
		}
	}
	for p, value := range cfxRetired {
		parts := strings.Split(p, ".")
		if cfxHas(example, parts) {
			t.Errorf("AC-14 the example gives %s, which the team replaced", p)
		}
		withIt := cfxDecode(t, "the example", []byte(text))
		at := withIt
		for _, part := range parts[:len(parts)-1] {
			next, ok := at[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				at[part] = next
			}
			at = next
		}
		at[parts[len(parts)-1]] = value
		raw, err := json.Marshal(withIt)
		if err != nil {
			t.Fatal(err)
		}
		_, err = config.Parse(raw)
		var refusal *sdlcerr.Error
		if !errors.As(err, &refusal) || refusal.Code.String() != "SDLC-E0087" {
			t.Errorf("AC-14 the example with %s set to %v is not refused as a setting the team replaced: %v", p, value, err)
		}
	}
}

// Gate 4 round 7: red team V49 to V51 and the human advocate's points 1 to 3. Each row
// below is read from the code: which settings have no default, what their absence means,
// which of an agent's settings are switches, and what the loader does with a `//` and with
// a top-level `_` key.

// cfxTypes is cfxTypePaths with the type at each setting.
func cfxTypes(t reflect.Type, prefix string, out map[string]reflect.Type) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == reflect.TypeFor[json.RawMessage]():
		out[prefix] = t
	case t.Kind() == reflect.Struct:
		for f := range t.Fields() {
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !f.IsExported() || name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			cfxTypes(f.Type, cfxJoin(prefix, name), out)
		}
	case t.Kind() == reflect.Map:
		// The map is a setting of its own, and so is each of its value's: without the
		// second, team.agents' per-agent switches were never read.
		out[prefix] = t
		cfxTypes(t.Elem(), cfxJoin(prefix, "*"), out)
	default:
		out[prefix] = t
	}
}

// cfxMatches reports whether a setting's dotted path is the typed one, `*` standing for any key.
func cfxMatches(typed, path string) bool {
	tp, pp := strings.Split(typed, "."), strings.Split(path, ".")
	if len(pp) < len(tp) {
		return false
	}
	for i := range tp {
		if tp[i] != "*" && tp[i] != pp[i] {
			return false
		}
	}
	return true
}

// cfxNodes are the values the decoded file holds at the setting, `*` standing for any key.
func cfxNodes(v any, parts []string, path string, out map[string]any) {
	if len(parts) == 0 {
		out[path] = v
		return
	}
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, x := range m {
		if parts[0] == "*" || parts[0] == k {
			cfxNodes(x, parts[1:], cfxJoin(path, k), out)
		}
	}
}

// cfxComment is the text after `//` on the lines given.
func cfxComment(text string) string {
	var parts []string
	for line := range strings.SplitSeq(text, "\n") {
		code := cfxWithoutComments(line)
		if len(code) < len(line) {
			parts = append(parts, strings.TrimPrefix(strings.TrimSpace(line[len(strings.TrimRight(code, "\n")):]), "//"))
		}
	}
	return strings.Join(parts, "\n")
}

// Red team V49 and V50. A setting the code gives no default, and init does not write, is
// shown at the value its absence means, as the code reads that absence: an empty list or
// object; gate_hours giving the hours it gives when left out; worktrees as WorktreesOn reads
// none; reviews.policy as ReviewPolicyPath reads none; an agent's max_rounds as
// loop.max_review_rounds; and lowest as the first model and effort init lists. A setting
// with no rule here fails, so a new one is not left unread.
// And nothing in the example is null.
func TestAC14ASettingWithNoDefaultIsShownAtTheValueItsAbsenceMeansAndNoneIsNull(t *testing.T) {
	t.Parallel()
	example := cfxDecode(t, "the example", []byte(cfxWithoutComments(cfxTheExample(t).body)))

	var nulls func(v any, path string)
	nulls = func(v any, path string) {
		switch x := v.(type) {
		case nil:
			t.Errorf("AC-14 the example gives %s as null", path)
		case map[string]any:
			for k, y := range x {
				nulls(y, cfxJoin(path, k))
			}
		case []any:
			for i, y := range x {
				nulls(y, cfxJoin(path, strconv.Itoa(i)))
			}
		}
	}
	nulls(example, "")

	types := map[string]reflect.Type{}
	cfxTypes(reflect.TypeFor[config.Config](), "", types)
	raw, err := json.Marshal(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	defaults := map[string]any{}
	cfxLeaves(cfxDecode(t, "config.Default()", raw), "", defaults)
	initDoc := cfxInitWrites(t)
	initWrites := map[string]any{}
	cfxLeaves(initDoc, "", initWrites)
	team, _ := initDoc["team"].(map[string]any)
	first := func(key string) any {
		list, _ := team[key].([]any)
		if len(list) == 0 {
			t.Fatalf("AC-14 init writes no team.%s", key)
		}
		return list[0]
	}
	absent := map[string]func(got any) (want any, ok bool){
		"budget.gate_hours": func(got any) (any, bool) {
			given, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			wantHours, wantProblems := config.Budget{}.GateHoursSet()
			hours, problems := config.Budget{GateHours: given}.GateHoursSet()
			return "the hours an absent gate_hours gives", maps.Equal(hours, wantHours) && slices.Equal(problems, wantProblems)
		},
		"git.worktrees": func(got any) (any, bool) {
			want := config.Git{}.WorktreesOn()
			return want, got == want
		},
		// Policy is omitempty and init writes none, so its absence is what
		// ReviewPolicyPath reads from an empty one.
		"reviews.policy": func(got any) (any, bool) {
			want := config.Config{}.ReviewPolicyPath()
			return want, got == want
		},
		"team.agents.*.max_rounds": func(got any) (any, bool) {
			want := defaults["loop.max_review_rounds"]
			return want, want != nil && got == want
		},
		"team.agents.*.lowest.model":  func(got any) (any, bool) { want := first("models"); return want, got == want },
		"team.agents.*.lowest.effort": func(got any) (any, bool) { want := first("efforts"); return want, got == want },
	}

	for typed, typ := range types {
		if cfxIsRetired(typed) {
			continue
		}
		hasDefault := false
		for _, leaves := range []map[string]any{defaults, initWrites} {
			for p := range leaves {
				if cfxMatches(typed, p) {
					hasDefault = true
				}
			}
		}
		if hasDefault {
			continue
		}
		nodes := map[string]any{}
		cfxNodes(example, strings.Split(typed, "."), "", nodes)
		for path, got := range nodes {
			if rule, ok := absent[typed]; ok {
				if want, same := rule(got); !same {
					t.Errorf("AC-14 the example gives %s as %v, which the code has no default for; left out it means %v", path, got, want)
				}
				continue
			}
			switch typ.Kind() {
			case reflect.Slice:
				if list, ok := got.([]any); !ok || len(list) != 0 {
					t.Errorf("AC-14 the example gives %s as %v, which the code has no default for; left out it is an empty list", path, got)
				}
			case reflect.Map:
				if m, ok := got.(map[string]any); !ok || len(m) != 0 {
					t.Errorf("AC-14 the example gives %s as %v, which the code has no default for; left out it is an empty object", path, got)
				}
			default:
				t.Errorf("AC-14 the example gives %s as %v, and nothing here says what the code takes where it is left out", path, got)
			}
		}
	}
}

// Red team V51 and the human advocate's point 4: every switch an agent's entry takes, read
// from the type, has a comment beside it in the example that names it and what false does.
func TestAC14EverySwitchOfAnAgentHasACommentSayingWhatFalseDoes(t *testing.T) {
	t.Parallel()
	lines := strings.Split(cfxTheExample(t).body, "\n")
	types := map[string]reflect.Type{}
	cfxTypes(reflect.TypeFor[config.Config](), "", types)
	var switches []string
	for typed, typ := range types {
		if key, ok := strings.CutPrefix(typed, "team.agents.*."); ok && !strings.Contains(key, ".") && typ.Kind() == reflect.Bool {
			switches = append(switches, key)
		}
	}
	if len(switches) == 0 {
		t.Fatal("AC-14 an agent's entry has no switch to read")
	}
	for _, key := range switches {
		said := false
		for i, line := range lines {
			if !strings.Contains(cfxWithoutComments(line), `"`+key+`"`) {
				continue
			}
			comment := cfxComment(cfxCommentFor(lines, i))
			if cfxNames(comment, key) && cfxNames(comment, "false") {
				said = true
			}
		}
		if !said {
			t.Errorf("AC-14 no %s in the example has a comment that names it and says what false does", key)
		}
	}
}

// The human advocate's points 1 to 3. The text before the example says a pasted `//` is
// refused as SDLC-E0004, and that a person's own file takes its comments as a top-level key
// starting with `_`; the code is asked that both are so. It names each of an agent's
// settings the example shows that init writes for none, so the sentence that every setting
// is at its default is true of the entry that shows them.
func TestAC14TheTextBeforeTheExampleNamesTheRefusalOfAPastedCommentAndWhereCommentsGo(t *testing.T) {
	t.Parallel()
	ex := cfxTheExample(t)
	before := stfFolded(ex.before)
	if !strings.Contains(before, "SDLC-E0004") {
		t.Errorf("AC-14 the text before the example does not name SDLC-E0004 for a pasted comment: %q", ex.before)
	}
	lower := strings.ToLower(before)
	if !strings.Contains(before, "`_") || !strings.Contains(lower, "top-level") && !strings.Contains(lower, "top level") {
		t.Errorf("AC-14 the text before the example does not point to a top-level key starting with `_` for a comment: %q", ex.before)
	}
	root := t.TempDir()
	writeFile(t, root, ".sdlc/config.json", ex.body)
	_, err := config.Load(root)
	var refusal *sdlcerr.Error
	if !errors.As(err, &refusal) || refusal.Code.String() != "SDLC-E0004" {
		t.Errorf("AC-14 the example pasted with its comments is not refused as SDLC-E0004: %v", err)
	}
	withNote := cfxDecode(t, "the example", []byte(cfxWithoutComments(ex.body)))
	withNote["_note"] = "a comment of the person's own"
	raw, err := json.Marshal(withNote)
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	writeFile(t, other, ".sdlc/config.json", string(raw))
	if _, err := config.Load(other); err != nil {
		t.Errorf("AC-14 the example with a top-level _note does not load: %v", err)
	}
	if unknown := config.UnknownKeys(other); len(unknown) > 0 {
		t.Errorf("AC-14 a top-level _note is reported as unknown: %q", unknown)
	}

	initWrites := map[string]any{}
	cfxLeaves(cfxInitWrites(t), "", initWrites)
	shown := map[string]any{}
	cfxNodes(cfxDecode(t, "the example", []byte(cfxWithoutComments(ex.body))), []string{"team", "agents", "*"}, "", shown)
	for _, entry := range shown {
		m, _ := entry.(map[string]any)
		for key := range m {
			written := false
			for p := range initWrites {
				if cfxMatches("team.agents.*."+key, p) {
					written = true
				}
			}
			if !written && !strings.Contains(before, key) {
				t.Errorf("AC-14 the example shows an agent's %s, which init writes for none, and the text before it does not name it", key)
			}
		}
	}
}
