package cli

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

type costPayload struct {
	OK        bool     `json:"ok"`
	Story     string   `json:"story"`
	AddedUSD  float64  `json:"added_usd,omitempty"`
	SpentUSD  float64  `json:"spent_usd"`
	BudgetUSD float64  `json:"budget_usd,omitempty"`
	Fraction  float64  `json:"fraction,omitempty"`
	Alerts    []string `json:"alerts,omitempty"`
}

func newCostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "What the story has cost so far",
		Long: "cost keeps the story's spend beside everything else the story did.\n\n" +
			"A loop that runs on its own spends money on its own, and a number that\n" +
			"lives only in a terminal somebody has closed is not an answer to \"what\n" +
			"did this cost\". `budget.per_story_usd` in .sdlc/config.json says what\n" +
			"one story is expected to cost, and `alert_fractions` says where along\n" +
			"the way to say so.\n\n" +
			"Nothing here blocks. A budget that stops a story halfway leaves the work\n" +
			"stranded between gates, which is worse than the overspend it prevents.",
		Example: "  sdlc cost\n  sdlc cost add --usd 1.42\n" +
			`  sdlc cost add --usd "$(claude -p ... --output-format json | jq .total_cost_usd)"`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, p, err := openStore()
			if err != nil {
				return err
			}
			id, err := activeStory(s, "cost is kept per story")
			if err != nil {
				return err
			}
			record, err := s.Record(id)
			if err != nil {
				return err
			}
			return reportCost(cmd, p.Config.Budget, id, 0, record.SpentUSD(), nil)
		},
	}
	cmd.AddCommand(newCostAddCmd())
	return cmd
}

func newCostAddCmd() *cobra.Command {
	var usd string
	var note string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Record an amount spent on this story",
		Long: "add puts one amount on the story's record, in US dollars.\n\n" +
			"Where the number comes from is the runner's business. A headless\n" +
			"iteration reports its own: `claude -p --output-format json` ends with\n" +
			"total_cost_usd. An interactive session has /cost.",
		Example: `  sdlc cost add --usd 1.42 --note "gate 4, five reviewers"`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			amount, err := parseUSD(usd)
			if err != nil {
				return err
			}
			s, p, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()
			id, err := activeStory(s, "cost is recorded against the story being worked on")
			if err != nil {
				return err
			}
			record, err := s.Record(id)
			if err != nil {
				return err
			}

			before := record.SpentUSD()
			after := record.AddSpend(amount, note, s.Now())
			budget := p.Config.Budget
			alerts := crossed(budget, before, after)
			for _, a := range alerts {
				record.Append("budget", a, s.Now())
			}
			if err := s.SaveRecord(record); err != nil {
				return err
			}
			return reportCost(cmd, budget, id, amount, after, alerts)
		},
	}
	cmd.Flags().StringVar(&usd, "usd", "", "the amount spent, in US dollars")
	cmd.Flags().StringVar(&note, "note", "", "what it was spent on")
	_ = cmd.MarkFlagRequired("usd")
	return cmd
}

// parseUSD is deliberately strict. An amount that arrives as an empty string
// from a shell substitution that produced nothing would otherwise be recorded
// as a free story, which is the one wrong answer nobody would question.
func parseUSD(raw string) (float64, error) {
	amount, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "$")), 64)
	switch {
	case err != nil:
		return 0, sdlcerr.New(sdlcerr.BadArgument,
			quote(raw)+" is not an amount",
			"--usd takes a number of US dollars, like 1.42").WithCause(err)
	case math.IsNaN(amount) || math.IsInf(amount, 0):
		return 0, sdlcerr.New(sdlcerr.BadArgument,
			quote(raw)+" is not an amount",
			"--usd takes a number of US dollars, like 1.42")
	case amount < 0:
		return 0, sdlcerr.New(sdlcerr.BadArgument,
			"an amount spent cannot be negative",
			"if a number was recorded wrongly, the record keeps both: add the "+
				"correction as its own entry with a note saying so")
	}
	return amount, nil
}

// crossed names every alert fraction this amount took the story past, in
// order. Only the ones crossed now: an alert that fired on the last entry is
// not news on this one.
func crossed(b config.Budget, before, after float64) []string {
	if b.PerStoryUSD <= 0 {
		return nil
	}
	fractions := append([]float64(nil), b.AlertFractions...)
	sort.Float64s(fractions)

	var out []string
	for _, f := range fractions {
		at := f * b.PerStoryUSD
		if before >= at || after < at {
			continue
		}
		out = append(out, fmt.Sprintf("%s of the %s budget for this story is spent (%s)",
			percent(f), money(b.PerStoryUSD), money(after)))
	}
	return out
}

func reportCost(cmd *cobra.Command, b config.Budget, id string,
	added, spent float64, alerts []string,
) error {
	var fraction float64
	if b.PerStoryUSD > 0 {
		fraction = spent / b.PerStoryUSD
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), costPayload{
			OK: true, Story: id, AddedUSD: added, SpentUSD: spent,
			BudgetUSD: b.PerStoryUSD, Fraction: fraction, Alerts: alerts,
		})
	}

	w := cmd.OutOrStdout()
	if b.PerStoryUSD > 0 {
		fmt.Fprintf(w, "%s  %s spent of %s (%s)\n", id, money(spent), money(b.PerStoryUSD), percent(fraction))
	} else {
		fmt.Fprintf(w, "%s  %s spent; no budget is set for this project\n", id, money(spent))
	}
	// The alert goes to stderr so that a runner reading stdout for the number
	// still sees it, and a person watching the terminal cannot miss it.
	for _, a := range alerts {
		fmt.Fprintf(cmd.ErrOrStderr(), "sdlc: %s\n", a)
	}
	if len(alerts) > 0 && fraction >= 1 {
		fmt.Fprintln(cmd.ErrOrStderr(),
			"      nothing is blocked -- stopping a story between gates costs more than "+
				"the overspend. `sdlc stop` ends the iteration if that is the call.")
	}
	return nil
}

func money(usd float64) string { return "$" + strconv.FormatFloat(usd, 'f', 2, 64) }

// percent reads like a percentage rather than a float: "41.7%", and "50%"
// where the fraction came straight out of the configuration.
func percent(f float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(f*100, 'f', 1, 64), ".0") + "%"
}
