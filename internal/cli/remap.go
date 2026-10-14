package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	cmap "github.com/bbsnly/sdlc/internal/codemap"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// newRemapCmd is the repair of a landed story's change to CODEMAP.md, for a
// person. It commits text no reviewer read to trunk, and its flags choose
// between a story's section and what trunk holds, so the hook refuses it to
// every model-driven session, whatever follows the word
// (remap-is-a-human-decision). Nothing here asks who runs it: the hook is that
// line, as it is for approve.
func newRemapCmd() *cobra.Command {
	var replace, drop bool
	cmd := &cobra.Command{
		Use:   "remap STORY",
		Short: "Put a landed story's kept change to CODEMAP.md on trunk, or drop it",
		Long: "remap is for a person. A story's sections of CODEMAP.md are put on trunk by\n" +
			"the stop that lands it; where that could not be done the story stays landed, the\n" +
			"sections stay kept in the repository's git directory, and every sdlc command says\n" +
			"so until this one has been run.\n\n" +
			"With no flag it makes the change where every section still applies, in one commit\n" +
			"of CODEMAP.md alone, and refuses by name a section that trunk's map has changed\n" +
			"under. --replace puts the kept sections over whatever trunk holds under their\n" +
			"headings. --drop discards the kept change and commits nothing, for after you\n" +
			"have edited CODEMAP.md yourself.\n\n" +
			"The commit moves trunk, as any commit does: a story held in a checkout of trunk\n" +
			"then has to take it in before it lands.",
		Example: "  sdlc remap US-012\n  sdlc remap US-012 --replace\n  sdlc remap US-012 --drop",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if replace && drop {
				return usage(cmd, "--replace and --drop are two different answers, and only one can be given")
			}
			return runRemap(cmd, args[0], replace, drop)
		},
	}
	cmd.Flags().BoolVar(&replace, "replace", false,
		"put the kept sections over whatever trunk's map holds under their headings")
	cmd.Flags().BoolVar(&drop, "drop", false,
		"discard the kept change and commit nothing")
	return cmd
}

type remapPayload struct {
	OK            bool   `json:"ok"`
	Story         string `json:"story"`
	CodemapCommit string `json:"codemap_commit,omitempty"`
	Dropped       bool   `json:"dropped,omitempty"`
}

func runRemap(cmd *cobra.Command, id string, replace, drop bool) error {
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	// Called through the variable: the project's lock is let go while this
	// waits for a landing in front of it, and taken again.
	defer func() { unlock() }()
	if _, _, err := s.Story(id); err != nil {
		return err
	}
	form := "sdlc remap " + id
	switch {
	case replace:
		form += " --replace"
	case drop:
		form += " --drop"
	}
	// The repair is a second writer of trunk, so it takes the lock every
	// landing holds, and reads everything below only once it has it.
	turn, _, err := landingTurn(cmd, s, id, &unlock, turnWords{
		command:   "remap",
		meanwhile: "this command cut short while it waits can be run again at once",
		fix: `run "` + form + `" again once that landing has finished: nothing was changed, and the change to ` +
			codemap + ` is still kept`,
	})
	if turn != nil {
		defer turn.Release()
	}
	if err != nil {
		return err
	}

	trunk := trunkName(s)
	nothing := func(why string) *sdlcerr.Error {
		return sdlcerr.New(sdlcerr.NoMapChangeToRepair, "there is no change to "+codemap+" to repair for "+id, why)
	}
	story, _, err := s.Story(id)
	if err != nil {
		return err
	}
	if story.Status != model.StatusDone {
		return nothing(quote(id) + " has not landed: sections stored for it are put on " + trunk +
			" by the stop that lands it, and are left for that stop").
			WithFix("there is nothing to run: the stop that lands " + id + " puts its sections on " + trunk)
	}
	content, stored, err := s.MapSections(id)
	if err != nil {
		return err
	}
	if !stored {
		return nothing("no sections are kept for it: its change is on " + trunk + " already, was dropped, or was never stored")
	}

	if drop {
		return dropKept(cmd, s, id, form, content)
	}
	return putKept(cmd, s, id, form, replace)
}

// dropKept discards a landed story's kept change, with no commit. A set that
// no longer parses is dropped all the same: discarding it is the one thing a
// person can always do with it.
func dropKept(cmd *cobra.Command, s *store.Store, id, form string, content []byte) error {
	what := "what was kept was not a set of sections"
	if sections, err := cmap.Parse(content); err == nil {
		what = headingsOf(sections)
	}
	// The record first: a drop that then fails leaves the file, which the next
	// command says again, where the other order left a drop nothing recorded.
	if err := appendEvent(s, id, eventCodemap, "the kept sections were removed by a person with "+form+
		", which made no commit: "+what); err != nil {
		return err
	}
	if err := s.DropMapSections(id); err != nil {
		return err
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), remapPayload{OK: true, Story: id, Dropped: true})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Dropped the change to %s kept for %s (%s). Nothing was committed, and %s is as it was.\n",
		codemap, id, what, trunkName(s))
	return nil
}

// putKept makes the commit the stop could not, from the sections still kept.
func putKept(cmd *cobra.Command, s *store.Store, id, form string, replace bool) error {
	trunk := trunkName(s)
	nothing := func(why string) *sdlcerr.Error {
		return sdlcerr.New(sdlcerr.NoMapChangeToRepair, "there is no change to "+codemap+" to repair for "+id, why)
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	if record.CodemapCommit != "" {
		return nothing("its record names " + record.CodemapCommit[:12] + " as the commit that put its change on " + trunk +
			"; what is still kept " + keptAt(s, id) + " is left over from it").
			WithFix(`a person, in their own terminal, removes the leftover file with "sdlc remap ` + id +
				` --drop", which commits nothing; there is nothing else to do`)
	}
	step := mapStep{s: s, root: s.Root(), trunk: trunk, id: id, by: form, replace: replace, kept: keptAt(s, id)}
	commit, what, failed := step.run(cmd.Context())
	switch {
	case failed != nil && failed.kind == mapDamaged:
		return sdlcerr.New(sdlcerr.MapSectionsRefused,
			"the sections kept for "+id+" are not a set that can be put into "+codemap,
			causeOf(failed.err)+". They were changed after they were stored, and nothing was committed").
			WithFix("put right the file kept " + step.kept + ` and run "sdlc remap ` + id +
				`" again, or discard it with "sdlc remap ` + id + ` --drop"`)
	case failed != nil:
		return step.notOnTrunk(failed)
	case commit == "":
		// Somebody took the file away between the read above and the step's.
		return nothing("the sections kept for it were removed while this command ran")
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), remapPayload{OK: true, Story: id, CodemapCommit: commit})
	}
	fmt.Fprintln(cmd.OutOrStdout(), mappedLine(trunk, commit, what))
	return nil
}
