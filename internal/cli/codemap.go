package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	cmap "github.com/bbsnly/sdlc/internal/codemap"
	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// A story's change to the root CODEMAP.md is not made in the story's branch.
// Two stories that each changed the file there met at the same lines, and the
// second to land stopped on a conflict only a person could resolve
// (SDLC-E0075), every time. The bookkeeper stores whole named sections
// instead; `sdlc stop` applies them to trunk's map as the landing left it, in
// a commit of that one file, once the story's own commits are on trunk.
//
// The sections are text a model wrote and no reviewer read. So they are parsed
// again every time they are read, the commit is made from trunk's objects and
// checked on the tree that was built, and a step that cannot be made leaves
// the landing standing and the change kept for a person's `sdlc remap`.

// eventCodemap is the type of a record's events about the map.
const eventCodemap = "codemap"

func newCodemapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codemap",
		Short: "Store the story's sections of CODEMAP.md for the stop to put on trunk",
		Long: "A story worked in a worktree of its own does not edit the root CODEMAP.md.\n" +
			"Its bookkeeper stores the sections it adds or replaces, and `sdlc stop` puts\n" +
			"them on trunk in a commit of their own once the story has landed.",
		Example: "  sdlc codemap write <<'MD'\n  <!-- sdlc:add -->\n  ## internal/refund\n\n  Keeps refunds.\n  MD",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newCodemapWriteCmd())
	return cmd
}

type codemapWritePayload struct {
	OK       bool   `json:"ok"`
	Story    string `json:"story"`
	Sections int    `json:"sections"`
}

func newCodemapWriteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "write",
		Short: "Store the sections the story adds to CODEMAP.md, or replaces in it",
		Long: "write reads a set of sections from standard input and keeps it for the story\n" +
			"being worked on, until `sdlc stop` has landed the story and put the sections on\n" +
			"trunk. Each section is a line that is exactly `" + cmap.AddLine + "` or\n" +
			"`" + cmap.ReplaceLine + "`, then its heading as `## ` and the text, then the lines\n" +
			"under it. A section is whole: a replaced one takes the place of the section by\n" +
			"that heading, and an added one goes at the end of the map.\n\n" +
			"Writing again replaces what was stored. Nothing in the working tree changes.",
		Example: "  sdlc codemap write <<'MD'\n  <!-- sdlc:replace -->\n  ## internal/billing\n\n  Keeps the accounts and invoices.\n  MD",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return runCodemapWrite(cmd) },
	}
}

func runCodemapWrite(cmd *cobra.Command) error {
	// Read before the lock is taken, as a gate's document is, and no further
	// than one byte past what a set may be.
	content, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), cmap.MaxBytes+1))
	if err != nil {
		return sdlcerr.New(sdlcerr.ArtifactUnreadable,
			"the sections could not be read from standard input",
			"reading stopped part way through").WithCause(err)
	}
	sections, err := cmap.Parse(content)
	if err != nil {
		return sdlcerr.New(sdlcerr.MapSectionsRefused,
			"the sections for "+codemap+" were not stored", err.Error()).WithCause(err)
	}

	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	id, err := storyToMap(s)
	if err != nil {
		return err
	}
	if err := s.WriteMapSections(id, content); err != nil {
		return err
	}
	if err := appendEvent(s, id, eventCodemap, "stored for the stop to put on "+trunkName(s)+": "+headingsOf(sections)); err != nil {
		return err
	}
	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), codemapWritePayload{OK: true, Story: id, Sections: len(sections)})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Stored %d %s for %s: %s. `sdlc stop` puts %s into %s on %s once the story has landed.\n",
		len(sections), plural(len(sections), "section", "sections"), id, headingsOf(sections),
		plural(len(sections), "it", "them"), codemap, trunkName(s))
	return nil
}

// storyToMap is the story sections are stored for: the one being worked on
// here, in a worktree of its own. Any other story's map is edited in place,
// since no stop would ever put stored sections on trunk for it.
func storyToMap(s *store.Store) (string, error) {
	id, err := activeStory(s, "the sections belong to the story being worked on")
	if err != nil {
		return "", err
	}
	if err := refuseIfHeldElsewhere(s, id, "its sections of "+codemap+" are stored from that checkout, not this one"); err != nil {
		return "", err
	}
	if !inItsWorktree(s, id) {
		return "", sdlcerr.New(sdlcerr.MapSectionsRefused,
			"the sections for "+codemap+" were not stored",
			quote(id)+" is not worked in a worktree of its own, so no stop lands it and nothing would put stored sections on "+trunkName(s)).
			WithFix("edit " + codemap + " in place, as part of the retro: it is committed with the story's own work")
	}
	return id, nil
}

// headingsOf names a set's sections for a message, a commit or the record:
// each quoted, since a heading is text a model wrote, and said to be added or
// replaced as the set has it.
func headingsOf(sections []cmap.Section) string {
	names := make([]string, len(sections))
	for i, s := range sections {
		names[i] = "adds " + quote(s.Heading)
		if s.Replace {
			names[i] = "replaces " + quote(s.Heading)
		}
	}
	return strings.Join(names, ", ")
}

// ---------------------------------------------------------------- the map step

// mapStep puts one landed story's kept sections on trunk. The stop runs it
// after the landing and a person's `sdlc remap` runs it later, both inside the
// landing lock, so that it reads trunk where the last landing left it.
type mapStep struct {
	s     *store.Store
	root  string // any checkout of the repository, for git
	trunk string
	id    string
	// by is the command that runs it, as the commit and the record say it.
	by string
	// replace is a person's --replace: the kept sections go over trunk's.
	replace bool
	// kept is keptAt's words, worked out when the step is made: the stop's
	// warning is printed after the checkout the store was opened in is gone.
	kept string
}

// Why a map step did not put the change on trunk, which decides what a person
// is told to do about it.
const (
	// mapStale: trunk's map changed under a section. The two flags answer it.
	mapStale = iota
	// mapDamaged: the kept sections are no longer a set that can be applied.
	mapDamaged
	// mapBlocked: something else is in the way, and the kept change is good.
	mapBlocked
	// mapThere: trunk's map already holds every kept section exactly, so no
	// commit can be made and only dropping the kept copy ends it.
	mapThere
	// mapUntold: a section on trunk cannot be told where it ends, and no form
	// of the repair puts text over it: a person edits the map.
	mapUntold
)

type mapFailure struct {
	kind int
	err  error
}

// forRecord is why the step failed, as the story's record keeps it. The
// record is a file a project commits, and the first form of this put the
// cause's own sentence there: for an uncommitted file in the checkout that
// has trunk checked out, that sentence names a directory on a person's
// machine in full. So nothing from an error that git, the disk or the store
// made is recorded, whatever it says. What internal/codemap said of the set is
// recorded: that package reads no file and runs nothing, and its sentences
// hold headings and lines of the set, quoted.
func (f *mapFailure) forRecord(id string) string {
	switch f.kind {
	case mapStale, mapDamaged, mapThere, mapUntold:
		// Each of these is made only from an error of internal/codemap's.
		return f.err.Error()
	}
	return "something other than the sections was in the way; what the stop printed on standard error said what, and " +
		`a person's "sdlc remap ` + id + `" says it again`
}

// firstError keeps the first error of several calls, so that reads which have
// nothing to decide between them share one refusal.
type firstError struct{ err error }

func (f *firstError) keep(err error) {
	if f.err == nil {
		f.err = err
	}
}

func (f *firstError) text(v string, err error) string {
	f.keep(err)
	return v
}

func (f *firstError) bytes(v []byte, err error) []byte {
	f.keep(err)
	return v
}

// run makes the step. It returns the map's commit and what the sections were;
// nothing at all where the story has no sections kept; and why not, where the
// change did not reach trunk. Nothing of a step that fails reaches trunk, and
// the kept sections are removed only once the record names the commit.
func (m mapStep) run(ctx context.Context) (commit, what string, failed *mapFailure) {
	content, stored, err := m.s.MapSections(m.id)
	if err != nil {
		return "", "", &mapFailure{mapBlocked, err}
	}
	if !stored {
		return "", "", nil
	}
	// Read as untrusted every time: the file is only as safe from a session
	// as the git directory is.
	sections, err := cmap.Parse(content)
	if err != nil {
		return "", "", &mapFailure{mapDamaged, err}
	}
	what = headingsOf(sections)

	var first firstError
	record, err := m.s.Record(m.id)
	first.keep(err)
	tip := first.text(gitx.BranchTip(ctx, m.root, m.trunk))
	now := first.bytes(mapAt(ctx, m.root, tip))
	if first.err != nil {
		return "", what, &mapFailure{mapBlocked, first.err}
	}
	seen := first.bytes(m.seen(ctx, record, sections))
	if first.err != nil {
		return "", what, &mapFailure{mapBlocked, first.err}
	}
	updated, err := cmap.Apply(now, seen, sections, m.replace)
	switch {
	case errors.Is(err, cmap.ErrAlreadyThere):
		return "", what, &mapFailure{mapThere, err}
	case errors.Is(err, cmap.ErrCannotTell):
		return "", what, &mapFailure{mapUntold, err}
	case errors.Is(err, cmap.ErrDoesNotApply):
		return "", what, &mapFailure{mapStale, err}
	case err != nil:
		return "", what, &mapFailure{mapDamaged, err}
	}

	commit = first.text(gitx.CommitFile(ctx, m.root, tip, codemap, updated, m.message(what)))
	if first.err == nil {
		first.keep(gitx.FastForward(ctx, m.root, m.trunk, tip, commit))
	}
	if first.err != nil {
		return "", what, &mapFailure{mapBlocked, first.err}
	}
	record.CodemapCommit = commit
	record.Append(eventCodemap, m.trunk+" moved from "+tip[:12]+" to "+commit[:12]+" by "+m.by+": "+what, m.s.Now())
	if err := m.s.SaveRecord(record); err != nil {
		return "", what, &mapFailure{mapBlocked, sdlcerr.New(sdlcerr.StateUnwritable,
			codemap+" is on "+m.trunk+" at "+commit[:12]+", and the record of "+m.id+" could not be written to name it",
			"the commit stands; the sections are still kept, so that this is said again until a person has dealt with it").WithCause(err)}
	}
	if err := m.s.DropMapSections(m.id); err != nil {
		// The change is on trunk and the record names it: what is left is a
		// file nothing reads again. It is said, not failed on.
		slog.Warn("the kept map sections could not be removed after they reached trunk", "story", m.id, "err", err)
	}
	return commit, what, nil
}

// mapAt is the root CODEMAP.md as a commit has it, and nil where it has none,
// or has something there that is not a plain file: a directory or a link is
// then met by the commit's own checks, which name it.
func mapAt(ctx context.Context, root, commit string) ([]byte, error) {
	if commit == "" {
		return nil, nil
	}
	mode, err := gitx.ModeAt(ctx, root, commit, codemap)
	if err != nil || (mode != "100644" && mode != "100755") {
		return nil, err
	}
	return gitx.FileAt(ctx, root, commit, codemap)
}

// seen is the map a replaced section on trunk is held to: what the bookkeeper
// read, section by section, from commits the record names and trunk keeps.
//
// The bookkeeper read the map at the story's last commit. Where the story
// landed as it was cut, that is the whole answer. Where it was put onto a
// trunk that had moved, the map at its last commit also holds what trunk
// gained meanwhile, which the bookkeeper may never have seen: so a section
// trunk changed between the cut and the commit the story was put onto is held
// to the cut's text, and a replacement of it is refused; any other section is
// held to the story's own last commit, which is where a section the story's
// own commits wrote is what its bookkeeper read.
func (m mapStep) seen(ctx context.Context, r *model.Record, sections []cmap.Section) ([]byte, error) {
	var first firstError
	landed := first.bytes(mapAt(ctx, m.root, r.Commit))
	var cut, base []byte
	if r.CommitBase != r.Cut {
		cut = first.bytes(mapAt(ctx, m.root, r.Cut))
		base = first.bytes(mapAt(ctx, m.root, r.CommitBase))
	}
	var out strings.Builder
	for _, s := range sections {
		from := landed
		atCut, inCut := cmap.SectionText(cut, s.Heading)
		atBase, inBase := cmap.SectionText(base, s.Heading)
		if inCut != inBase || atCut != atBase {
			from = cut
		}
		if text, ok := cmap.SectionText(from, s.Heading); ok {
			out.WriteString(text + "\n\n")
		}
	}
	return []byte(out.String()), first.err
}

// message is the map commit's message. The body is the only record on trunk
// of where the text came from and that nobody reviewed it.
func (m mapStep) message(what string) string {
	how := "applied where each still applied"
	if m.replace {
		how = "put over whatever " + m.trunk + " held under those headings"
	}
	return "docs: map " + m.id + "\n\n" +
		"Sections of " + codemap + " stored for " + m.id + " with `sdlc codemap write`, " + how + " by `" + m.by + "`:\n" +
		what + ".\n\n" +
		"No reviewer read this commit. sdlc made it from the stored text: it is not signed, and no hook ran for it.\n"
}

// causeOf is the sentence of a failure for another refusal's why: what it
// said and why, never its code or its fix, which were written for another
// command and another moment.
func causeOf(err error) string {
	if e, ok := errors.AsType[*sdlcerr.Error](err); ok {
		return strings.TrimSuffix(strings.TrimSpace(e.What+": "+e.Why), ":")
	}
	return err.Error()
}

// keptAt says where a story's sections are kept, for a message that goes to
// standard error and nowhere else: "at" and the path the store reads, since
// the git directory is not always `.git` in the main checkout. Where the
// checkout cannot be found it names no path. A stop asks before the story's
// worktree is removed: asked after, it printed a path with no git directory in
// it, which no file was at.
func keptAt(s *store.Store, id string) string {
	co, _ := config.FindCheckout(s.Root())
	if co.Common == "" {
		return "in the repository's git directory"
	}
	return "at " + sdlcerr.QuotePath(filepath.Join(co.Common, filepath.FromSlash(store.MapSectionsRel), id+".md"))
}

// withdrawMap drops the sections kept for a story whose attempt is over: it
// is begun afresh, or a person released it. They were stored against a map
// and a cut that are no longer the story's, and the next stop would have put
// them on trunk, with nobody having chosen them for that landing, if the
// bookkeeper then stored nothing. The record says they went; the caller saves
// it.
func withdrawMap(s *store.Store, r *model.Record, id, why string) {
	if _, stored, _ := s.MapSections(id); !stored {
		return
	}
	if err := s.DropMapSections(id); err != nil {
		slog.Warn("the sections kept for the map from an earlier attempt could not be removed", "story", id, "err", err)
		return
	}
	r.Append(eventCodemap, "the sections stored for "+codemap+" were dropped, with no commit: "+why, s.Now())
}

// notOnTrunk is the refusal for a landed story whose change to the map did
// not reach trunk. The stop prints it as a warning and exits 0; the repair
// fails with it. The fix is the cause's: the two flags are offered only where
// they answer what went wrong.
func (m mapStep) notOnTrunk(f *mapFailure) *sdlcerr.Error {
	const (
		person    = "a person, in their own terminal, in the main checkout"
		assistant = "; an assistant says so and runs none of these"
	)
	plain := `"sdlc remap ` + m.id + `"`
	replace := `"sdlc remap ` + m.id + ` --replace"`
	drop := `"sdlc remap ` + m.id + ` --drop"`
	e := sdlcerr.New(sdlcerr.MapChangeNotOnTrunk,
		m.id+" has landed, and its change to "+codemap+" is not on "+m.trunk,
		causeOf(f.err)+". The landing stands, and the change is kept "+m.kept)
	switch f.kind {
	case mapUntold:
		return e.WithFix(person + ", edits " + codemap + " on " + m.trunk + " by hand, with what the kept sections say, commits it, and then runs " +
			drop + " to discard the kept copy; the --replace form is refused for the same reason, so it is not for this" + assistant)
	case mapThere:
		return e.WithFix(person + ", runs " + drop + ", which removes the kept copy and commits nothing: " + m.trunk +
			" holds the text already, so nothing is lost, and no other form of the command has anything to commit" + assistant)
	case mapStale:
		return e.WithFix(person + ", chooses: " + replace + " puts the kept sections over what " + m.trunk +
			" holds under those headings; " + drop + " discards the kept change, for after they have edited " +
			codemap + " themselves; and " + plain + " makes the change once " + m.trunk + "'s sections are again " +
			"what the bookkeeper read" + assistant)
	case mapDamaged:
		return e.WithFix(person + ", puts the file named above right and runs " + plain + ", or discards it with " +
			drop + assistant)
	}
	return e.WithFix(person + ", puts right what is named above and then runs " + plain +
		", which makes the change; nothing is lost meanwhile, and the --drop form would discard the change, so it is not for this" + assistant)
}

// ---------------------------------------------------------------- the notice

// mapsWaiting says, before a command runs, which landed stories have a change
// to CODEMAP.md that is not on trunk. A stop killed between the landing and
// the map's commit prints nothing, and what it leaves is exactly this: a done
// story, sections kept for it, and no commit named on its record. It is worked
// out from disk every time, so it is said until a person has dealt with it.
//
// It goes to standard error whatever the command prints, and a failure here
// says nothing: a notice must not break the command it comes before. The
// directory is listed first, and almost always holds nothing, so a command
// pays one directory read for it.
func mapsWaiting(env commandEnv, stderr io.Writer) {
	p, err := config.Open(env.dir)
	if err != nil {
		return
	}
	s := store.New(p).WithEnv(env.getenv)
	ids, err := s.StoriesWithMapSections()
	if err != nil {
		slog.Debug("the kept map sections could not be listed", "err", err)
	}
	if len(ids) == 0 {
		return
	}
	// Read once, and only where something is kept: the backlog is not small,
	// and this runs before every command.
	backlog, err := s.Backlog()
	if err != nil {
		return
	}
	for _, id := range ids {
		if story, ok := backlog.Find(id); !ok || story.Status != model.StatusDone {
			continue
		}
		if r, err := s.Record(id); err != nil || r.CodemapCommit != "" {
			continue
		}
		fmt.Fprintf(stderr, "sdlc: %s has landed, and its change to %s is not on %s: a person runs `sdlc remap %s` "+
			"in their own terminal (the change is kept %s; `sdlc help remap` says what the command does)\n",
			id, codemap, trunkName(s), id, keptAt(s, id))
	}
}

// noticeBefore reports whether the notice is said before this command. The
// repair is what it asks for, and help and the version read no repository.
func noticeBefore(root *cobra.Command, args []string) bool {
	cmd, _, err := root.Find(args)
	if err != nil || cmd == root {
		return false
	}
	switch cmd.Name() {
	case "remap", "help", versionCommand:
		return false
	}
	return true
}
