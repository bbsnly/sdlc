package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// Where an entry's range came from. A range read from commit messages is a
// guess the record could not make for itself, and says so.
const (
	rangeFromRecord  = "record"
	rangeFromMessage = "message"
	rangeUnknown     = "unknown"
)

// Where the log starts.
const (
	sinceAcknowledged = "acknowledged"
	sinceFlag         = "flag"
	sinceAll          = "all"
	sinceNone         = "none"
)

type logPayload struct {
	OK bool `json:"ok"`
	// Since is the commit the log starts after. A story whose commits end in
	// its history was already read. Absent when the log shows every story.
	Since       string     `json:"since,omitempty"`
	SinceSource string     `json:"since_source"`
	Entries     []logEntry `json:"entries"`
}

// logEntry is one committed story, as somebody reading trunk after it needs it:
// where its commits are, and what it took to get them there.
type logEntry struct {
	Story string `json:"story"`
	Title string `json:"title"`
	// NotInBacklog is set when the story has gone from the backlog, so that an
	// empty title is not mistaken for a story with no title.
	NotInBacklog bool   `json:"not_in_backlog,omitempty"`
	CommittedAt  string `json:"committed_at"`
	CommitBase   string `json:"commit_base,omitempty"`
	Commit       string `json:"commit,omitempty"`
	RangeSource  string `json:"range_source"`
	// OffBranch is an end commit HEAD's history does not hold, and Missing one
	// this repository does not have at all: rewritten away, or never fetched.
	// Neither can be placed against the commit the log starts after, so both
	// are always listed.
	OffBranch   bool            `json:"off_branch,omitempty"`
	Missing     bool            `json:"missing,omitempty"`
	Rounds      map[string]int  `json:"rounds,omitempty"`
	Blocks      []logBlock      `json:"blocks,omitempty"`
	Reopened    []logEvent      `json:"reopened,omitempty"`
	Unfrozen    []logEvent      `json:"unfrozen,omitempty"`
	Escalations []logEscalation `json:"escalations,omitempty"`
	SpentUSD    float64         `json:"spent_usd"`
}

type logBlock struct {
	Gate  string `json:"gate"`
	Role  string `json:"role"`
	Round int    `json:"round"`
	Note  string `json:"note,omitempty"`
}

type logEvent struct {
	At      string `json:"at"`
	Message string `json:"message"`
}

type logEscalation struct {
	At       string `json:"at"`
	Type     string `json:"type"`
	Message  string `json:"message"`
	Resolved bool   `json:"resolved"`
}

func newLogCmd() *cobra.Command {
	var since string
	var all bool
	cmd := &cobra.Command{
		Use:   "log",
		Short: "List the stories committed to trunk, and what each one went through",
		Long: "log lists every story whose commit gate has passed, oldest first: the\n" +
			"commits it landed in, how many rounds each gate's reviews took, what\n" +
			"blocked, and what is worth a second look -- a freeze lifted, gates\n" +
			"reopened, a question handed to a person, what it cost.\n\n" +
			"It starts after the last commit a person acknowledged, when there is one.\n" +
			"--since starts after another commit, and --all lists every story.\n\n" +
			"A range can hold commits somebody else made in the same window, so it is\n" +
			"a list to read, not a list to revert. log changes nothing.",
		Example: "  sdlc log\n  sdlc log --since HEAD~20\n  sdlc log --all --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sinceGiven := cmd.Flags().Changed("since")
			if all && sinceGiven {
				return usage(cmd, "--since and --all cannot be used together: --all already lists every story")
			}
			s, _, err := openStore()
			if err != nil {
				return err
			}
			backlog, err := s.Backlog()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			head, err := gitx.Head(ctx, s.Root())
			if err != nil {
				return err
			}

			payload := logPayload{OK: true, Entries: []logEntry{}}
			switch {
			case all:
				payload.SinceSource = sinceAll
			case sinceGiven:
				payload.SinceSource = sinceFlag
				if payload.Since, err = gitx.Resolve(ctx, s.Root(), since); err != nil {
					if !gitx.NamesNoCommit(err) {
						return err
					}
					return sdlcerr.New(sdlcerr.BadArgument,
						"--since "+quote(since)+" names no commit in this repository",
						"the log starts after the commit --since names, and git cannot read that as one").
						WithFix(`give a commit git knows: a hash, a tag, or a name like HEAD~20`).
						WithCause(err)
				}
			default:
				if payload.Since, payload.SinceSource, err = acknowledged(ctx, s); err != nil {
					return err
				}
			}

			ids, err := s.StoriesOnDisk()
			if err != nil {
				return err
			}
			for _, id := range ids {
				record, err := s.Record(id)
				if err != nil {
					return err
				}
				if !record.Pass(model.GateCommit) {
					continue
				}
				entry := describeCommitted(record, backlog)
				if err := placeRange(ctx, s.Root(), head, &entry, record); err != nil {
					return err
				}
				read, err := alreadyRead(ctx, s.Root(), payload.Since, entry)
				if err != nil {
					return err
				}
				if !read {
					payload.Entries = append(payload.Entries, entry)
				}
			}
			// The stories are listed in name order, so two committed in the
			// same second read the same way every time.
			sort.SliceStable(payload.Entries, func(i, j int) bool {
				return payload.Entries[i].CommittedAt < payload.Entries[j].CommittedAt
			})

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), payload)
			}
			printLog(cmd.OutOrStdout(), payload)
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "",
		"list the stories committed after this commit, instead of after the last one acknowledged")
	cmd.Flags().BoolVar(&all, "all", false, "list every committed story, whatever has been acknowledged")
	return cmd
}

// acknowledged is the commit the log starts after by default. A file that names
// no commit git has is refused rather than read as none: listing every story
// again would bury the new ones under everything already read.
func acknowledged(ctx context.Context, s *store.Store) (string, string, error) {
	named, ok, err := s.Acknowledged()
	if err != nil || !ok {
		return "", sinceNone, err
	}
	sha, err := gitx.Resolve(ctx, s.Root(), named)
	if err != nil {
		if !gitx.NamesNoCommit(err) {
			return "", "", err
		}
		return "", "", sdlcerr.New(sdlcerr.AcknowledgedUnknown,
			store.AcknowledgedFile+" names "+quote(named)+", which is no commit in this repository",
			"the log starts after the last commit a person acknowledged, and git cannot read that as one").
			WithCause(err)
	}
	return sha, sinceAcknowledged, nil
}

// describeCommitted is what the record says about a committed story, apart
// from where its commits are.
func describeCommitted(r *model.Record, b *model.Backlog) logEntry {
	e := logEntry{Story: r.Story, CommittedAt: r.Gates[model.GateCommit].At, SpentUSD: r.SpentUSD()}
	if story, ok := b.Find(r.Story); ok {
		e.Title = story.Title
	} else {
		e.NotInBacklog = true
	}
	for _, rev := range r.Reviews {
		if rev.Round > e.Rounds[string(rev.Gate)] {
			if e.Rounds == nil {
				e.Rounds = map[string]int{}
			}
			e.Rounds[string(rev.Gate)] = rev.Round
		}
		if rev.Verdict == model.VerdictBlock {
			e.Blocks = append(e.Blocks, logBlock{Gate: string(rev.Gate), Role: rev.Role, Round: rev.Round, Note: rev.Note})
		}
	}
	for _, ev := range r.Events {
		switch {
		case ev.Type == "gates_reopened":
			e.Reopened = append(e.Reopened, logEvent{At: ev.At, Message: ev.Message})
		// Lifting a finished story's freeze is the loop tidying up, not
		// somebody changing the tests.
		case ev.Type == "unfreeze" && ev.Message != finishedUnfreeze:
			e.Unfrozen = append(e.Unfrozen, logEvent{At: ev.At, Message: ev.Message})
		}
	}
	for _, esc := range r.Escalations {
		e.Escalations = append(e.Escalations, logEscalation{
			At: esc.At, Type: esc.Type, Message: esc.Message, Resolved: esc.Resolved,
		})
	}
	return e
}

// placeRange finds where a committed story's commits are. The record says,
// when an sdlc that kept it passed the commit gate. Otherwise the commits in
// HEAD's history whose messages name the story stand in: the newest ends the
// range, and the one before the oldest starts it. Nothing else is guessed: a
// range worked out from timestamps would look right and be wrong.
func placeRange(ctx context.Context, root, head string, e *logEntry, r *model.Record) error {
	if r.Commit == "" {
		e.RangeSource = rangeUnknown
		if head == gitx.NoCommits {
			return nil
		}
		named, err := gitx.CommitsNaming(ctx, root, r.Story)
		if err != nil || len(named) == 0 {
			return err
		}
		// Found in HEAD's history, so on this branch by construction.
		e.CommitBase, e.Commit, e.RangeSource = named[len(named)-1].Parent, named[0].Hash, rangeFromMessage
		return nil
	}

	// What the record names is text somebody could have edited, so the entry
	// carries the commit git reads it as, and only that reaches git again. Only
	// git saying there is no such commit makes it missing: git that stopped
	// running has said nothing about it.
	e.CommitBase, e.Commit, e.RangeSource = r.CommitBase, r.Commit, rangeFromRecord
	commit, unresolved := gitx.Resolve(ctx, root, r.Commit)
	if unresolved != nil {
		e.Missing = gitx.NamesNoCommit(unresolved)
		if !e.Missing {
			return unresolved
		}
		return nil
	}
	e.Commit = commit
	if r.CommitBase != "" {
		if base, err := gitx.Resolve(ctx, root, r.CommitBase); err == nil {
			e.CommitBase = base
		}
	}
	// A branch with no commits yet, such as a new orphan branch, holds none of
	// the commits the repository has.
	if head == gitx.NoCommits {
		e.OffBranch = true
		return nil
	}
	onBranch, err := gitx.IsAncestor(ctx, root, e.Commit, head)
	e.OffBranch = !onBranch
	return err
}

// alreadyRead reports whether a story's commits end in the history of the
// commit the log starts after. One that cannot be placed never is.
func alreadyRead(ctx context.Context, root, since string, e logEntry) (bool, error) {
	if since == "" || e.RangeSource == rangeUnknown || e.Missing || e.OffBranch {
		return false, nil
	}
	return gitx.IsAncestor(ctx, root, e.Commit, since)
}

func printLog(w io.Writer, p logPayload) {
	if len(p.Entries) == 0 {
		switch p.SinceSource {
		case sinceAcknowledged:
			fmt.Fprintf(w, "No story has been committed since %s, the last commit acknowledged. "+
				"`sdlc log --all` lists every one.\n", shortCommit(p.Since))
		case sinceFlag:
			fmt.Fprintf(w, "No story has been committed since %s.\n", shortCommit(p.Since))
		default:
			fmt.Fprintln(w, "No story has been committed yet.")
		}
		return
	}
	switch p.SinceSource {
	case sinceAcknowledged:
		fmt.Fprintf(w, "Committed since %s, the last commit acknowledged:\n\n", shortCommit(p.Since))
	case sinceFlag:
		fmt.Fprintf(w, "Committed since %s:\n\n", shortCommit(p.Since))
	}

	for i, e := range p.Entries {
		if i > 0 {
			fmt.Fprintln(w)
		}
		title := oneLine(e.Title)
		if e.NotInBacklog {
			title = "(not in the backlog)"
		}
		fmt.Fprintf(w, "%s  %s  %s\n", describeRange(e), e.Story, title)
		switch {
		case e.RangeSource == rangeUnknown:
			fmt.Fprintf(w, "  range      unknown: the record names no commit, and no commit message names %s\n", e.Story)
		case e.Missing:
			fmt.Fprintln(w, "  range      not in this repository")
		case e.OffBranch:
			fmt.Fprintln(w, "  range      not on this branch")
		case e.RangeSource == rangeFromMessage:
			fmt.Fprintln(w, "  range      inferred from commit messages")
		}
		var rounds []string
		for _, g := range model.Gates {
			if n := e.Rounds[string(g)]; n > 0 {
				rounds = append(rounds, fmt.Sprintf("%s %d", g, n))
			}
		}
		if len(rounds) > 0 {
			fmt.Fprintf(w, "  rounds     %s\n", strings.Join(rounds, " · "))
		}
		if len(e.Blocks) > 0 {
			blocks := make([]string, 0, len(e.Blocks))
			for _, b := range e.Blocks {
				blocks = append(blocks, fmt.Sprintf("%s %s (round %d)", b.Gate, oneLine(b.Role), b.Round))
			}
			fmt.Fprintf(w, "  blocks     %d: %s\n", len(e.Blocks), strings.Join(blocks, ", "))
		}
		for _, ev := range e.Reopened {
			fmt.Fprintf(w, "  reopened   %s\n", oneLine(ev.Message))
		}
		for _, ev := range e.Unfrozen {
			fmt.Fprintf(w, "  unfrozen   %s\n", oneLine(ev.Message))
		}
		for _, esc := range e.Escalations {
			answer := "waiting"
			if esc.Resolved {
				answer = "answered"
			}
			fmt.Fprintf(w, "  escalated  %s: %s (%s)\n", oneLine(esc.Type), oneLine(esc.Message), answer)
		}
		if e.SpentUSD > 0 {
			fmt.Fprintf(w, "  spent      %s\n", money(e.SpentUSD))
		}
	}
}

func describeRange(e logEntry) string {
	switch {
	case e.Commit == "":
		return "(unknown range)"
	case e.CommitBase == "":
		return shortCommit(e.Commit)
	}
	return shortCommit(e.CommitBase) + ".." + shortCommit(e.Commit)
}

func shortCommit(sha string) string { return oneLine(sha[:min(len(sha), 12)]) }
