package shellpolicy

// What configWordFrom and writtenConfig keep of a word, or of a whole line's
// words, that found nothing (configAsk and configLineAsk in claudeconfig.go)
// answers every later segment whose key is the same. A field left out of a key
// lets one segment answer another that differs in it alone: with dir taken out
// of both, every other test of this package still passed, while `cd sub/l`
// into a link to ~/.claude let the second rm of the configuration through.
// Each row pairs a segment that finds nothing with a later one that differs
// from it in one field of a key, and must be refused, so that a key without
// that field fails a row.
//
// configLineAsk's lost has no row of its own. The line's words are passed over
// only where none of them names the place, and then nothing of the line names
// it either, so lost can matter only through the directory a lost walk spells
// from ~, which from holds: a walk with a from is always lost, and one that is
// not lost has none.

import (
	"strings"
	"testing"
)

// linkedConfigState is claudeConfigState with the person's ~/.agents beside
// ~/.claude, the home as the directory that holds both, and links: each key a
// path the command can name, a link to the directory it maps to, so that what
// is under the link is under that directory. The link itself is the link.
func linkedConfigState(links map[string]string) State {
	s := claudeConfigState()
	base := s.Resolve
	s.Resolve = func(word string) string {
		c := clean(word)
		for link, to := range links {
			if rest, ok := strings.CutPrefix(c, link+"/"); ok {
				return to + "/" + rest
			}
		}
		return base(word)
	}
	s.AgentsConfig = func(word string) bool {
		c := clean(word)
		return c == "~/.agents" || hasDir(c, "~/.agents") || c == "/home/dev/.agents" || hasDir(c, "/home/dev/.agents")
	}
	s.ClaudeConfigHeld = func(word string) bool {
		c := clean(word)
		return c == "~" || c == "/home/dev"
	}
	return s
}

func TestEachFieldOfTheConfigMemosKeysKeepsOneSegmentFromAnsweringAnother(t *testing.T) {
	const claude, agents = ruleClaudeConfig, ruleAgentsConfig
	toClaude := map[string]string{"l": "/home/dev/.claude"}
	for _, row := range []struct {
		field, command string
		links          map[string]string
		// session is whether the row is asked of Inspect, the session holding
		// the story, rather than ClaudeConfigWrites.
		session bool
		rule    string
	}{
		// ~/.agents is asked first, and finds nothing in a word that is in
		// ~/.claude only by a link: its answer is not ~/.claude's.
		{field: "configAsk rule", command: "touch l/settings.json", links: toClaude, rule: claude},
		{field: "configLineAsk rule", command: "echo l/settings.json | xargs rm", links: toClaude, rule: claude},
		{
			field:   "configAsk dir",
			command: "echo x | tee settings.json; cd l; echo x | tee settings.json", links: toClaude, rule: claude,
		},
		{
			field:   "configAsk and configLineAsk dir",
			command: "echo settings.json | xargs rm; cd sub/l && echo settings.json | xargs rm",
			links:   map[string]string{"sub/l": "/home/dev/.claude"}, rule: claude,
		},
		// Both walks are lost and at the same dir; only where each is, spelt
		// from ~, tells them apart.
		{
			field:   "configAsk from",
			command: "cd ~/x && touch l/settings.json; cd ~ && touch l/settings.json",
			links:   map[string]string{"~/l": "/home/dev/.claude"}, rule: claude,
		},
		{
			field:   "configLineAsk from",
			command: "cd ~/x && echo l/settings.json | xargs rm; cd ~ && echo l/settings.json | xargs rm",
			links:   map[string]string{"~/l": "/home/dev/.claude"}, rule: claude,
		},
		// The same relative word from the same dir is placed until the walk is
		// lost, and then it could be anywhere the line names.
		{field: "configAsk lost", command: `ls ~/.claude; touch settings.json; cd "$D" && touch settings.json`, rule: claude},
		// The home written into is nothing of the configuration; the home
		// taken away holds it.
		{field: "configAsk held", command: "touch ~; rm -rf ~", rule: claude},
		// The same word, quoted and then bare: only the shell's glob reaches
		// .claude.
		{field: "configAsk glob", command: `touch "$HOME/.cla*"; touch $HOME/.cla*`, rule: claude},
		// checkAgentsNamedWord asks only the words that spell .agents, and
		// none here does; checkPersonsConfig then asks them all, and one is
		// in ~/.agents by a link.
		{
			field: "configLineAsk spelt", command: "echo a/x | xargs rm",
			links: map[string]string{"a": "/home/dev/.agents"}, session: true, rule: agents,
		},
	} {
		s := linkedConfigState(row.links)
		var f Finding
		var ok bool
		if row.session {
			f, ok = Inspect(row.command, s)
		} else {
			f, ok = ClaudeConfigWrites(row.command, s)
		}
		if !ok || f.Rule != row.rule {
			t.Errorf("%s: %s = %+v, %v; want it refused as %s", row.field, row.command, f, ok, row.rule)
		}
	}
}

// The words of a line are its own: a memo asked for a second line answers it
// with that line's words, not the first one's.
func TestTheWordsOfALineAreWorkedOutForThatLine(t *testing.T) {
	m := &memo{}
	if got := m.words("echo a/b"); strings.Join(got, " ") != "echo a/b" {
		t.Fatalf("words(echo a/b) = %q", got)
	}
	if got := m.words("rm c/d"); strings.Join(got, " ") != "rm c/d" {
		t.Errorf("words(rm c/d), after echo a/b = %q; want the words of rm c/d", got)
	}
}
