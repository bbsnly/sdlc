package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bbsnly/sdlc/internal/model"
)

// Team configures every agent: whether it runs, whether its block stops a gate,
// how many instances of a reviewer there are, how often an agent may fail, and
// what the project manager may ask for. Every key may be left out, and a nil
// pointer is a key left out, so absent, zero and null stay apart and the file
// round-trips as it was written.
type Team struct {
	Retries *int     `json:"retries,omitempty"`
	Models  []string `json:"models,omitempty"`
	Efforts []string `json:"efforts,omitempty"`
	// Agents is keyed by the name the plugin gives an agent, exactly.
	Agents map[string]AgentEntry `json:"agents,omitempty"`
	// repeated is each key given more than once under agents, which the decode
	// above cannot show, and repeatedMore how many more there were past the
	// ones kept (repeatedAgents).
	repeated     []string
	repeatedMore int
}

// AgentEntry is one agent's settings.
type AgentEntry struct {
	Enabled   *bool    `json:"enabled,omitempty"`
	CanBlock  *bool    `json:"can_block,omitempty"`
	MaxRounds *int     `json:"max_rounds,omitempty"`
	Instances *int     `json:"instances,omitempty"`
	Lowest    *Lowest  `json:"lowest,omitempty"`
	BringBack []string `json:"bring_back,omitempty"`
}

// The bounds of the team's numbers. A file refused for its team keeps its values
// on the hook's path, so the accessors clamp to these too: an instances of two
// billion expanded into reviewers runs out of memory, which in Go no recover
// answers, and retries + 1 at the top of an int wrapped to no budget at all.
const (
	defaultRetries = 2
	maxRetries     = 20
	maxInstances   = 16
)

// The lists a staffing may ask from where the section sets none, lowest first.
func defaultModels() []string  { return []string{"haiku", "sonnet", "opus"} }
func defaultEfforts() []string { return []string{"low", "medium", "high", "xhigh", "max"} }

// The agent whose being disabled turns the staffing off.
const projectManager = "project-manager"

func (t Team) entry(agent string) AgentEntry { return t.Agents[model.BaseRole(agent)] }

// RetriesAllowed is how many times an agent may fail at a gate before the story
// is handed to a person, from 0 to 20.
func (t Team) RetriesAllowed() int {
	if t.Retries == nil {
		return defaultRetries
	}
	return min(max(*t.Retries, 0), maxRetries)
}

// ModelList is the models a staffing may ask for, lowest first.
func (t Team) ModelList() []string {
	if t.Models == nil {
		return defaultModels()
	}
	return t.Models
}

// EffortList is the efforts a staffing may ask for, lowest first.
func (t Team) EffortList() []string {
	if t.Efforts == nil {
		return defaultEfforts()
	}
	return t.Efforts
}

// Enabled reports whether the agent, or the role of an instance, runs.
func (t Team) Enabled(agent string) bool {
	e := t.entry(agent).Enabled
	return e == nil || *e
}

// StaffingOn reports whether the project manager staffs the stories.
func (t Team) StaffingOn() bool { return t.Enabled(projectManager) }

// CanBlock is can_block for every agent the section sets it on, keyed by the
// agent, for model.ReviewPolicy. An agent left out keeps the roster's power.
func (t Team) CanBlock() map[string]bool {
	var out map[string]bool
	for agent, e := range t.Agents {
		if e.CanBlock != nil {
			if out == nil {
				out = map[string]bool{}
			}
			out[agent] = *e.CanBlock
		}
	}
	return out
}

// MaxRounds is the agent's own limit on blocks that stop a gate, if it has one.
func (t Team) MaxRounds(agent string) (int, bool) {
	if m := t.entry(agent).MaxRounds; m != nil {
		return *m, true
	}
	return 0, false
}

// Instances is how many instances of the agent there are, from 1 to 16, as
// written: Seats is what the loop reads.
func (t Team) Instances(agent string) int {
	n := t.entry(agent).Instances
	if n == nil {
		return 1
	}
	return min(max(*n, 1), maxInstances)
}

// LowestFor is the floor the project manager may not staff the agent below;
// an instance's is its role's.
func (t Team) LowestFor(agent string) Lowest {
	if l := t.entry(agent).Lowest; l != nil {
		return *l
	}
	return Lowest{}
}

// BringBackFor is the paths a change to which brings the agent back to the code
// review where the staffing left it out; every instance of a role is brought
// back by the role's paths.
func (t Team) BringBackFor(agent string) []string { return t.entry(agent).BringBack }

// BringsBack reports whether any agent has bring_back paths.
func (t Team) BringsBack() bool {
	for _, e := range t.Agents {
		if len(e.BringBack) > 0 {
			return true
		}
	}
	return false
}

// Seats is how many instances of the agent take part: its instances where it is
// an enabled reviewer, and otherwise one. A disabled agent's instances need no
// staffing row and are not waited for, and an agent that reviews nothing has no
// instance a name could be given to.
func (t Team) Seats(agent string) int {
	if !t.Enabled(agent) || !model.IsReviewRole(agent) {
		return 1
	}
	return t.Instances(agent)
}

// Staffable reports whether name is an agent of the plugin, or an instance
// within its role's seats. It is the one check of a name that the staffing's
// commands and `sdlc team failure` make.
func (t Team) Staffable(name string) bool {
	if slices.Contains(model.Agents, name) {
		return true
	}
	role, n, ok := model.InstanceOf(name)
	return ok && n <= t.Seats(role)
}

// Names is every agent and instance a staffing names, in the plugin's order,
// each agent followed by its instances.
func (t Team) Names() []string {
	out := make([]string, 0, len(model.Agents))
	for _, agent := range model.Agents {
		out = append(out, agent)
		for n := 2; n <= t.Seats(agent); n++ {
			out = append(out, instanceName(agent, n))
		}
	}
	return out
}

func instanceName(role string, n int) string { return role + "-" + strconv.Itoa(n) }

// ReviewersFor is the reviews one gate expects in this project: the roster's,
// each expanded by its seats.
func (t Team) ReviewersFor(g model.Gate) []model.Reviewer {
	var out []model.Reviewer
	for _, r := range model.ReviewersFor(g) {
		out = append(out, r)
		for n := 2; n <= t.Seats(r.Role); n++ {
			instance := r
			instance.Role = instanceName(r.Role, n)
			out = append(out, instance)
		}
	}
	return out
}

// StandsDown reports whether the agent or instance takes no part in the story,
// and whether a change to one of its bring_back paths can bring it back. A name
// past its role's seats has no seat at all; a disabled agent is out whatever
// the staffing, the bring_back paths or the floor say, except the verifier and
// the code reviewer, whose gate is then a person's and still wants their
// review; with the project manager disabled the staffing is not read; and
// otherwise the staffing's row, whose floor stays a defence, decides. A seat
// with no row is not left out, so it is waited for.
func (t Team) StandsDown(record *model.Record, name string) (out, returnable bool) {
	role := model.BaseRole(name)
	if _, n, ok := model.InstanceOf(name); ok && n > t.Seats(role) {
		return true, false
	}
	if !t.Enabled(role) {
		if name == role && (role == "verifier" || role == "code-reviewer") {
			return false, false
		}
		return true, false
	}
	if !t.StaffingOn() || record == nil {
		return false, false
	}
	return record.LeftOut(name), true
}

// StoodDown reports whether the agent or instance stands down and was not
// brought back on the record, so that its review holds nothing.
func (t Team) StoodDown(record *model.Record, name string) bool {
	out, returnable := t.StandsDown(record, name)
	if !out {
		return false
	}
	return !returnable || record.Staffing == nil || record.Staffing.BroughtBack[name] == ""
}

// problems collects what is wrong with a configuration, bounded: past a fixed
// size it only counts. A file is free text, and its refusal reaches a person's
// terminal and a session, so a list of a hundred thousand bad names, or a key
// of a megabyte, must still make a short message. The size is read before a
// problem goes in, so a problem's own text is bounded too: a name from the file
// goes in through quoted, and a list of names is never joined into one.
type problems struct {
	old, team, other []string
	// found counts the problems of each list, kept or only counted: which
	// refusal a file gets, and whether the hook may read its values, never
	// turn on what the text had room for. Decided on the kept lists, a file
	// with ten bad agent names beside version 99 or a policy outside the
	// repository was read as one refused only for its team, and the hook took
	// its values.
	oldFound, teamFound, otherFound int
	size, more                      int
	// mapOld is set by validate, so every Load that refuses a file maps the
	// old keys to their new ones, which decodes the old section again. That is
	// the hook's path too, since ForRules starts with Load, so the mapping goes
	// through this collector like every other problem: one problem to an agent,
	// and past the size only counted. Joined into one problem, it made a
	// refusal of 3.4 MB from a staffing.bring_back of 100,000 agents, on every
	// load of every tool call. ForRules' own check leaves it off: it asks only
	// which lists hold problems.
	mapOld bool
}

// The size a refusal's problems are kept to, and the length a name from the
// file is cut to.
const (
	problemsSize = 2048
	nameSize     = 64
)

func (p *problems) full() bool { return p.size >= problemsSize }

func (p *problems) add(list *[]string, found *int, text string) {
	*found++
	if p.full() {
		p.more++
		return
	}
	*list = append(*list, text)
	p.size += len(text) + 2
}

func (p *problems) addOld(text string)   { p.add(&p.old, &p.oldFound, text) }
func (p *problems) addTeam(text string)  { p.add(&p.team, &p.teamFound, text) }
func (p *problems) addOther(text string) { p.add(&p.other, &p.otherFound, text) }

// count counts problems of a list past the size without making their text.
func (p *problems) count(found *int, n int) {
	*found += n
	p.more += n
}

func (p *problems) empty() bool { return p.oldFound+p.teamFound+p.otherFound == 0 }

// text is every problem kept, the old keys first, and how many more there were.
func (p *problems) text() string {
	all := strings.Join(slices.Concat(p.old, p.team, p.other), "; ")
	if p.more > 0 {
		all += fmt.Sprintf("; and %d more", p.more)
	}
	return all
}

// quoted is a name from the file as a refusal shows it: quoted, so no control
// character reaches a terminal and no newline starts a line of the tool's own,
// and cut where it is long, with a mark that it was.
func quoted(name string) string {
	if len(name) <= nameSize {
		return strconv.Quote(name)
	}
	end := nameSize
	for end > 0 && !utf8.RuneStart(name[end]) {
		end--
	}
	cut := name[:end]
	return strconv.Quote(cut) + " (cut, " + strconv.Itoa(len(name)) + " bytes)"
}

// controlIn reports whether a name holds a control character: one below
// U+0020, U+007F, or U+0080 to U+009F. Printed raw, a newline or a carriage
// return made a line of the tool's own in what the project manager reads, and
// an escape a colour or a cleared screen.
func controlIn(name string) bool {
	return strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r >= 0x7F && r <= 0x9F })
}

// check adds what is wrong with the team section. Each repeated name is one
// problem, however often it repeats.
func (t Team) check(p *problems) {
	if r := t.Retries; r != nil && (*r < 0 || *r > maxRetries) {
		p.addTeam(fmt.Sprintf("team.retries is %d: give a whole number from 0 to %d", *r, maxRetries))
	}
	t.checkList(p, "team.models", t.Models)
	t.checkList(p, "team.efforts", t.Efforts)
	for _, agent := range t.repeated {
		p.addTeam(fmt.Sprintf("team.agents has %s more than once; only the last entry would count, "+
			"so keep one entry for it and put what the others say into it", quoted(agent)))
	}
	p.count(&p.teamFound, t.repeatedMore)
	// Past the size an agent is still read, so that "and N more" counts its
	// problems and not the agent: counted whole, every valid agent after the
	// size was one more problem. A file names at most the dozen agents there
	// are, and any number that are not, so only those skip their text.
	for _, agent := range slices.Sorted(maps.Keys(t.Agents)) {
		if !slices.Contains(model.Agents, agent) {
			if p.full() {
				p.count(&p.teamFound, 1)
				continue
			}
			why := "which is not an agent"
			if role, _, ok := model.InstanceOf(agent); ok {
				why = "which is an instance's name: set instances on " + role + " instead"
			}
			p.addTeam(fmt.Sprintf("team.agents has %s, %s; the agents are %s", quoted(agent), why, strings.Join(model.Agents, ", ")))
			continue
		}
		t.checkAgent(p, agent, t.Agents[agent])
	}
}

func (t Team) checkList(p *problems, name string, list []string) {
	if list == nil {
		return
	}
	if len(list) == 0 {
		p.addTeam(name + " is empty: list what a staffing may ask for, lowest first")
		return
	}
	seen := make(map[string]bool, min(len(list), 64))
	reported := map[string]bool{}
	for i, n := range list {
		switch {
		case controlIn(n):
			if !reported[n] {
				p.addTeam(fmt.Sprintf("%s entry %d, %s, holds a control character; remove it", name, i+1, quoted(n)))
			}
			reported[n] = true
		case strings.TrimSpace(n) == "":
			if !reported[n] {
				p.addTeam(fmt.Sprintf("%s entry %d is blank", name, i+1))
			}
			reported[n] = true
		case seen[n]:
			if !reported[n] {
				p.addTeam(fmt.Sprintf("%s has %s more than once", name, quoted(n)))
			}
			reported[n] = true
		}
		seen[n] = true
	}
}

func (t Team) checkAgent(p *problems, agent string, e AgentEntry) {
	key := "team.agents." + agent
	if m := e.MaxRounds; m != nil && *m < 1 {
		p.addTeam(fmt.Sprintf("%s.max_rounds is %d: give 1 or more, or leave it out to use loop.max_review_rounds", key, *m))
	}
	if n := e.Instances; n != nil && (*n < 1 || *n > maxInstances) {
		p.addTeam(fmt.Sprintf("%s.instances is %d: give a whole number from 1 to %d", key, *n, maxInstances))
	}
	if l := e.Lowest; l != nil {
		t.checkLowest(p, key, *l)
	}
	if len(e.BringBack) > 0 {
		checkBringBack(p, agent, key, e.BringBack)
	}
}

// checkLowest reads a floor against the lists in force, the defaults where the
// section sets none.
func (t Team) checkLowest(p *problems, key string, l Lowest) {
	if l.Model != "" && !slices.Contains(t.ModelList(), l.Model) {
		p.addTeam(fmt.Sprintf("%s.lowest has the model %s, which team.models does not list: it lists %s",
			key, quoted(l.Model), strings.Join(t.ModelList(), ", ")))
	}
	if l.Effort != "" && !slices.Contains(t.EffortList(), l.Effort) {
		p.addTeam(fmt.Sprintf("%s.lowest has the effort %s, which team.efforts does not list: it lists %s",
			key, quoted(l.Effort), strings.Join(t.EffortList(), ", ")))
	}
}

// checkBringBack reads an agent's bring_back paths, each bad one once.
func checkBringBack(p *problems, agent, key string, paths []string) {
	if !reviewsCode(agent) {
		p.addTeam(fmt.Sprintf("%s.bring_back is only for an agent that reviews the code; the reviewers there are %s",
			key, model.ReviewerNames(model.GateCodeReview)))
		return
	}
	reported := map[string]bool{}
	for _, path := range paths {
		// A path is matched segment by segment against what git reports, so one
		// git never reports matches nothing, and an empty one everything.
		if bringBackPathOK(path) || reported[path] {
			continue
		}
		reported[path] = true
		if p.full() {
			p.count(&p.teamFound, 1)
			continue
		}
		p.addTeam(fmt.Sprintf("%s.bring_back has %s: give a file or a directory relative to the repository root, "+
			"with / between its parts and no pattern", key, quoted(path)))
	}
}

// decode reads a configuration file over the defaults, and the one thing that
// decode cannot show: a key repeated under team.agents. encoding/json keeps the
// last of a repeated key and says nothing, so `"perf": {"enabled": false},
// "perf": {"enabled": true}` loaded with perf enabled, and the line a person
// wrote to turn it off was read in silence. Parse and ForRules both decode
// here, so neither reads the file differently from the other.
func decode(raw []byte) (Config, error) {
	cfg := Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	if len(cfg.Team.Agents) > 0 {
		cfg.Team.repeated, cfg.Team.repeatedMore = repeatedAgents(raw)
	}
	return cfg, nil
}

// agentObjects keeps each team.agents object as the decoder hands it over, in
// document order: a repeated team or agents key gives one object each, where
// the main decode merges them into one map.
type agentObjects [][]byte

func (o *agentObjects) UnmarshalJSON(b []byte) error {
	*o = append(*o, bytes.Clone(b))
	return nil
}

// repeatedKept is how many repeated names are kept; past it they are counted.
// The problems collector keeps fewer than this, so nothing it would show is
// lost, and a file repeating a hundred thousand names builds no list of them.
const repeatedKept = 32

// repeatedAgents is each key given more than once across the team.agents
// objects, once however often it repeats, and how many more there were past
// repeatedKept. The decoder finds the objects itself, so it skips every other
// section unbuilt and folds the case of team and agents as the main decode
// does. A null clears what came before, as the decoder sets the map to nil
// there. The bytes were decoded already, so a token error only ends the walk.
func repeatedAgents(raw []byte) (names []string, more int) {
	var doc struct {
		Team struct {
			Agents agentObjects `json:"agents"`
		} `json:"team"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil, 0
	}
	seen := map[string]int{}
	for _, object := range doc.Team.Agents {
		if bytes.Equal(object, []byte("null")) {
			clear(seen)
			continue
		}
		eachKey(object, func(key string) {
			seen[key]++
			switch {
			case seen[key] != 2:
			case len(names) < repeatedKept:
				names = append(names, key)
			default:
				more++
			}
		})
	}
	return names, more
}

// eachKey hands over the keys of a JSON object, in order, as the decoder
// decodes them, so `"perf"` is perf. Each value is skipped unbuilt.
func eachKey(object []byte, key func(string)) {
	d := json.NewDecoder(bytes.NewReader(object))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return
	}
	for d.More() {
		t, err := d.Token()
		name, ok := t.(string)
		var value json.RawMessage
		if err != nil || !ok || d.Decode(&value) != nil {
			return
		}
		key(name)
	}
}

// reviewsCode reports whether the agent reviews the code.
func reviewsCode(agent string) bool {
	_, ok := model.FindReviewer(model.GateCodeReview, agent)
	return ok
}

// checkOld adds the settings the team section replaced. Each says where it
// went, and staffing.enabled says what each of its values asks of a person:
// true is the default now, and false is the project manager disabled.
func (c Config) checkOld(p *problems) {
	if c.Reviews.Gate7Advisory {
		p.addOld("reviews.gate7_advisory is true: set team.agents.code-reviewer.can_block to false instead, " +
			"and delete reviews.gate7_advisory")
	}
	if len(c.Staffing) == 0 {
		return
	}
	if !p.mapOld {
		p.addOld("staffing is the section the team replaced")
		return
	}
	oldStaffing(p, c.Staffing)
}

// oldStaffing maps a staffing section, of any shape, to what replaced each of
// its keys. Every name from the file is quoted, so none can end a line of the
// refusal.
func oldStaffing(p *problems, raw json.RawMessage) {
	var section map[string]json.RawMessage
	switch {
	case json.Unmarshal(raw, &section) != nil || section == nil:
		p.addOld("staffing is the section the team replaced, and holds nothing it could move to: delete it")
	case len(section) == 0:
		p.addOld("staffing is the section the team replaced, and is empty: delete it")
	}
	for _, key := range slices.Sorted(maps.Keys(section)) {
		value := section[key]
		switch strings.ToLower(key) {
		case "enabled":
			p.addOld(oldEnabled(value))
		case "models", "efforts":
			p.addOld(fmt.Sprintf("staffing.%s moved to team.%s: move the list there", strings.ToLower(key), strings.ToLower(key)))
		case "lowest", "bring_back":
			oldPerAgent(p, strings.ToLower(key), value)
		default:
			if p.full() {
				p.count(&p.oldFound, 1)
				continue
			}
			p.addOld(fmt.Sprintf("staffing has %s, which the team section has no place for: delete it", quoted(key)))
		}
	}
}

func oldEnabled(value json.RawMessage) string {
	var on bool
	if json.Unmarshal(value, &on) == nil && !on && string(bytes.TrimSpace(value)) == "false" {
		return "staffing.enabled is false: set team.agents.project-manager.enabled to false instead, and delete staffing.enabled"
	}
	return "staffing.enabled is now the default, since the project manager staffs every story unless " +
		"team.agents.project-manager.enabled is false: delete staffing.enabled"
}

// oldPerAgent maps staffing.lowest and staffing.bring_back, keyed by agent, to
// each agent's own entry, one problem to an agent.
func oldPerAgent(p *problems, key string, value json.RawMessage) {
	var byAgent map[string]json.RawMessage
	if json.Unmarshal(value, &byAgent) != nil || len(byAgent) == 0 {
		p.addOld(fmt.Sprintf("staffing.%s moved to team.agents.<agent>.%s: move each agent's there", key, key))
		return
	}
	for _, agent := range slices.Sorted(maps.Keys(byAgent)) {
		if p.full() {
			p.count(&p.oldFound, 1)
			continue
		}
		name := agent
		if !slices.Contains(model.Agents, agent) {
			name = quoted(agent)
		}
		p.addOld(fmt.Sprintf("staffing.%s.%s moved to team.agents.%s.%s: move it there", key, name, name, key))
	}
}

// LeftoverGate7 reports whether the file holds reviews.gate7_advisory, in any
// case, which loads only at false and does nothing there. One walk of the
// document, which only doctor asks.
func LeftoverGate7(root string) bool {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(File)))
	if err != nil {
		return false
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimPrefix(raw, []byte("\ufeff")), &doc) != nil {
		return false
	}
	for key, value := range doc {
		var reviews map[string]json.RawMessage
		if !strings.EqualFold(key, "reviews") || json.Unmarshal(value, &reviews) != nil {
			continue
		}
		for inner := range reviews {
			if strings.EqualFold(inner, "gate7_advisory") {
				return true
			}
		}
	}
	return false
}
