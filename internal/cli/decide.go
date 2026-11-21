package cli

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// The flags of `sdlc decide`, which shellpolicy.ValueFlags names too.
const (
	flagQuestion = "question"
	flagOption   = "option"
	flagChosen   = "chosen"
	flagBy       = "by"
)

type decidePayload struct {
	OK     bool   `json:"ok"`
	Story  string `json:"story"`
	Gate   string `json:"gate"`
	By     string `json:"by"`
	Number int    `json:"number"`
	Path   string `json:"path"`
}

// decideFlags are the flags `sdlc decide` was given.
type decideFlags struct {
	question, chosen, reason, by string
	options                      []string
}

func newDecideCmd() *cobra.Command {
	var f decideFlags
	cmd := &cobra.Command{
		Use:   "decide <ID>",
		Short: "Record an implementation decision the loop made, and why",
		Long: "decide records an implementation decision on the story's record, as a\n" +
			"decision event, and in .sdlc/stories/<ID>/DECISIONS.md, in the order made.\n" +
			"A product decision is a person's, and goes to them with `sdlc escalate`.\n\n" +
			"Give the question, every option weighed (--option once for each, at least\n" +
			"two), the one chosen, word for word, the reason, and --by with the agent\n" +
			"whose recommendation or ruling was applied. Each is one line. A decision\n" +
			"changes no gate, review, freeze, staffing or approval, and answers nothing\n" +
			"a person was asked; `sdlc log` lists it, and `sdlc status` counts it until\n" +
			"a person's `sdlc ack` covers its story's commit.",
		Example: `  sdlc decide US-001 --question "Which total does AC-2 mean?" --option "the gross total" \` + "\n" +
			`      --option "the net total" --chosen "the net total" --reason "AC-2 says what is charged" --by researcher`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runDecide(cmd, args[0], f) },
	}
	// None is marked required: a missing part is SDLC-E0094, naming it, and not
	// cobra's usage error.
	cmd.Flags().StringVar(&f.question, flagQuestion, "", "what was decided, as a question, in one line")
	// An array, not a slice: a slice flag splits its value at commas.
	cmd.Flags().StringArrayVar(&f.options, flagOption, nil, "one option weighed; once for each, at least two")
	cmd.Flags().StringVar(&f.chosen, flagChosen, "", "the option chosen, word for word")
	cmd.Flags().StringVar(&f.reason, "reason", "", "why it was chosen, in one line")
	cmd.Flags().StringVar(&f.by, flagBy, "", "the agent whose recommendation or ruling was applied, such as researcher or architect-arbiter")
	return cmd
}

func runDecide(cmd *cobra.Command, id string, f decideFlags) error {
	choice, err := f.read()
	if err != nil {
		return err
	}
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	if choice.By, err = decidedBy(s, f.by); err != nil {
		return err
	}
	record, err := decisionRecord(cmd, s, id)
	if err != nil {
		return err
	}
	choice.Gate = model.AfterLastGate
	if g, ok := record.NextGate(); ok {
		choice.Gate = g
	}
	now := s.Now()
	record.Events = append(record.Events, model.Event{
		At: model.Timestamp(now), Type: model.EventDecision, Message: model.DecisionMessage(choice), Choice: &choice,
	})
	number := 0
	for _, e := range record.Events {
		if e.Type == model.EventDecision {
			number++
		}
	}
	// DECISIONS.md first, then the record, so that a record that cannot be
	// saved takes the file back with it: a refusal records nothing anywhere.
	path, undo, err := s.WriteDecisions(id, renderDecisions(id, record))
	if err != nil {
		return err
	}
	if err := s.SaveRecord(record); err != nil {
		if undoErr := undo(); undoErr != nil {
			return undoErr
		}
		return err
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), decidePayload{OK: true, Story: id, Gate: string(choice.Gate), By: choice.By, Number: number, Path: path})
	}
	// Nothing a model wrote is on the line: the gate and --by are names the
	// loop knows, and the path is the store's.
	fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  D-%d by %s recorded in %s\n", id, choice.Gate, number, choice.By, path)
	return nil
}

// decisionRecord is the record of the story a decision goes on, once the
// story is in the backlog, has a record, is held by this checkout, and its
// commit is not yet acknowledged.
func decisionRecord(cmd *cobra.Command, s *store.Store, id string) (*model.Record, error) {
	if _, _, err := s.Story(id); err != nil {
		return nil, err
	}
	if !s.RecordStored(id) {
		return nil, sdlcerr.New(sdlcerr.NoActiveIteration,
			id+" has no record, so there is nothing to record a decision on",
			"a decision belongs to a story the loop has started").
			WithFix(`a person runs "sdlc start ` + id + `", or /sdlc:next, to start it`)
	}
	if err := refuseIfHeldElsewhere(s, id, "a decision is about the work in that checkout, not this one"); err != nil {
		return nil, err
	}
	record, err := s.Record(id)
	if err != nil {
		return nil, err
	}
	if acknowledgedCommit(cmd.Context(), s, record) {
		return nil, sdlcerr.New(sdlcerr.DecisionRefused,
			id+"'s commit is already acknowledged, so a decision recorded on it now would be neither counted by sdlc status nor listed by sdlc log",
			"a person's sdlc ack marks a committed story's decisions read").
			WithFix("record it on the story under way, which sdlc status names, and name " + id + " in --question")
	}
	return record, nil
}

// read is the decision the flags give, or a refusal naming the part that is
// wrong, before the store is opened: a refusal records nothing. Each part is
// trimmed and then held to one line of bounded length, blank first, so that a
// question of white space is refused as blank and not as hidden text.
func (f decideFlags) read() (model.Choice, error) {
	c := model.Choice{
		Question: strings.TrimSpace(f.question), Chosen: strings.TrimSpace(f.chosen), Reason: strings.TrimSpace(f.reason),
	}
	parts := [][2]string{{"--" + flagQuestion, c.Question}, {"--" + flagChosen, c.Chosen}, {"--reason", c.Reason}}
	for _, p := range parts {
		if p[1] == "" {
			return c, decisionRefused(p[0]+" is missing or blank", "give "+p[0]+` "<...>" on one line`)
		}
	}
	for _, o := range f.options {
		if o = strings.TrimSpace(o); o != "" {
			c.Options = append(c.Options, o)
		}
	}
	switch {
	case len(c.Options) < 2:
		return c, decisionRefused(fmt.Sprintf("a decision weighs at least two options, and this gives %d", len(c.Options)),
			`give --option "<an option>" once for each option weighed, at least twice, none of them blank`)
	case len(c.Options) > model.MaxDecisionOptions:
		return c, decisionRefused(fmt.Sprintf("a decision weighs at most %d options, and this gives %d", model.MaxDecisionOptions, len(c.Options)),
			"give --option for the options that were weighed seriously, at most "+strconv.Itoa(model.MaxDecisionOptions))
	}
	for i, o := range c.Options {
		if slices.Contains(c.Options[:i], o) {
			return c, decisionRefused("--option "+quote(o)+" is given twice", "give each --option once, and no two the same")
		}
	}
	for _, o := range c.Options {
		parts = append(parts, [2]string{"--" + flagOption, o})
	}
	for _, p := range parts {
		switch {
		case len(p[1]) > model.MaxDecisionBytes:
			return c, decisionRefused(p[0]+" is longer than "+strconv.Itoa(model.MaxDecisionBytes)+" bytes",
				"say "+p[0]+" in a line or two: every hook call reads the record that holds it")
		case strings.ContainsFunc(p[1], hiddenText):
			return c, decisionRefused(p[0]+" holds a line break, a control character or an invisible one",
				"write "+p[0]+" on one line, in plain text: it is shown on lines of sdlc's own")
		}
	}
	if !slices.Contains(c.Options, c.Chosen) {
		return c, decisionRefused("--chosen "+quote(c.Chosen)+" is not one of the options",
			"give --chosen as one of the --option values, word for word")
	}
	return c, nil
}

// decisionRefused is a refusal of `sdlc decide` whose fix names only the part
// it refuses: a fix that named every part would say nothing of which.
func decisionRefused(what, fix string) error {
	return sdlcerr.New(sdlcerr.DecisionRefused, what, "nothing was recorded").WithFix(fix + ", then run the command again")
}

// decidedBy is --by as a name the loop knows: an agent of the plugin, an
// instance within its role's seats, or such a reviewer's arbiter. A person is
// never one, so that no decision reads as a person's answer (T-3 of the
// threats). It names whose recommendation or ruling was applied, not the
// session that ran the command, and no gate reads it.
func decidedBy(s *store.Store, given string) (string, error) {
	by := strings.ToLower(strings.TrimSpace(given))
	team := s.Config().Team
	if team.Staffable(by) {
		return by, nil
	}
	if reviewer, ok := model.ArbiterOf(by); ok && team.Staffable(reviewer) {
		return by, nil
	}
	what := "--by is missing"
	if by != "" {
		what = "--by " + quote(given) + " is no agent the loop knows"
	}
	return "", sdlcerr.New(sdlcerr.DecisionRefused, what, "nothing was recorded").
		WithFix("--by names the agent whose recommendation or ruling was applied: an agent of the plugin such as researcher, " +
			"an instance within team.agents.<agent>.instances such as security-2, or <role>-arbiter such as architect-arbiter; " +
			"a person is never one")
}

// acknowledgedCommit reports whether a person's ack already covers the commit
// of a story whose commit gate has passed, as `sdlc log` reads it: a decision
// recorded on it now would be counted and listed nowhere (security F-7 of the
// design review). An ack git cannot read, or a range that cannot be placed, is
// no cover: the decision is taken, and status counts it unread, which is the
// safe direction.
func acknowledgedCommit(ctx context.Context, s *store.Store, record *model.Record) bool {
	if !record.Pass(model.GateCommit) {
		return false
	}
	since, source, err := acknowledged(ctx, s)
	if err != nil || source == sinceNone {
		return false
	}
	head, err := gitx.Head(ctx, s.Root())
	if err != nil {
		return false
	}
	entry := logEntry{Story: record.Story}
	read, err := placeAndCheck(ctx, s.Root(), head, since, &entry, record)
	return err == nil && read
}

// decisionGate is the gate a decision names, or what DECISIONS.md and the log
// say where an older sdlc's save left none; one spelling, so the two agree.
func decisionGate(gate string) string { return cmp.Or(gate, "unknown gate") }

// renderDecisions is a story's DECISIONS.md, written whole from its record's
// decision events, so that the record is its one source. Every value follows a
// fixed prefix on a line of its own and is printed on one line, so that no
// text a model gave opens a heading or a line that reads as the tool's (T-5 of
// the threats).
func renderDecisions(id string, record *model.Record) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Decisions — %s\n\n", id)
	b.WriteString("Written by `sdlc decide`, in the order the decisions were made. The story's gate record is " +
		"its source, and this file is written again from it with each decision.\n")
	n := 0
	for _, e := range record.Events {
		if e.Type != model.EventDecision {
			continue
		}
		n++
		c := e.Decision()
		gate := decisionGate(string(c.Gate))
		if e.Choice == nil {
			fmt.Fprintf(&b, "\n## D-%d at %s, %s\n\n- Recorded as: %s\n", n, oneLine(gate), oneLine(e.At), oneLine(c.Question))
			continue
		}
		fmt.Fprintf(&b, "\n## D-%d at %s, by %s, %s\n\n- Question: %s\n- Options:\n", n, oneLine(gate), oneLine(c.By), oneLine(e.At), oneLine(c.Question))
		for _, o := range c.Options {
			fmt.Fprintf(&b, "  - %s\n", oneLine(o))
		}
		fmt.Fprintf(&b, "- Chosen: %s\n- Reason: %s\n", oneLine(c.Chosen), oneLine(c.Reason))
	}
	return []byte(b.String())
}
