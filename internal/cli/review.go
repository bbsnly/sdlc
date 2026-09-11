package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

type reviewPayload struct {
	OK      bool   `json:"ok"`
	Story   string `json:"story"`
	Gate    string `json:"gate"`
	Role    string `json:"role"`
	Verdict string `json:"verdict"`
	Round   int    `json:"round"`
	Path    string `json:"path"`
	Subject string `json:"subject,omitempty"`
}

type reviewListPayload struct {
	OK      bool         `json:"ok"`
	Story   string       `json:"story"`
	Reviews []reviewInfo `json:"reviews"`
}

// reviewInfo is one expected review and where it has got to. Stale is the field
// that matters: an approval of a plan that has since changed is not an
// approval, and nothing else in the output would say so.
type reviewInfo struct {
	Gate     string `json:"gate"`
	Role     string `json:"role"`
	Blocking bool   `json:"blocking"`
	Verdict  string `json:"verdict,omitempty"`
	Round    int    `json:"round,omitempty"`
	Stale    bool   `json:"stale,omitempty"`
}

func newReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Record what a reviewer concluded, and about what",
		Long: "A gate that is reviewed does not pass because an agent said it should. It\n" +
			"passes because every reviewer it expects has reported, the blocking ones\n" +
			"approved, and what they approved is still what is there.\n\n" +
			"That last part is why reviews go through this command: it stamps each one\n" +
			"with the thing that was reviewed, so a later change makes the approval\n" +
			"stale instead of silently standing.",
		Example: "  sdlc review list\n  sdlc review add design_review architect approve < review.md",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newReviewAddCmd(), newReviewListCmd())
	return cmd
}

func newReviewAddCmd() *cobra.Command {
	var storyID, file, note string
	cmd := &cobra.Command{
		Use:   "add GATE ROLE VERDICT",
		Short: "Record one reviewer's conclusion",
		Long: "add stores a reviewer's document under the story and records the verdict\n" +
			"against what was in front of them.\n\n" +
			"Verdicts: " + model.VerdictNames() + ". A blocking reviewer's approve is what\n" +
			"lets the gate pass; a note is a finding worth recording that does not stand\n" +
			"in the way.\n\n" +
			"The document comes from standard input, or from --file.",
		Example: "  sdlc review add design_review architect block < review.md\n" +
			"  sdlc review add code_review perf note --file /tmp/perf.md",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			gate := model.Gate(args[0])
			if !gate.Valid() {
				return sdlcerr.New(sdlcerr.UnknownGate,
					"there is no gate called "+quote(args[0]),
					"this version knows "+model.GateNames())
			}
			reviewer, ok := model.FindReviewer(gate, args[1])
			if !ok {
				return sdlcerr.New(sdlcerr.UnknownReviewer,
					string(gate)+" does not expect a review from "+quote(args[1]),
					"it expects "+model.ReviewerNames(gate))
			}
			verdict := model.Verdict(strings.ToLower(strings.TrimSpace(args[2])))
			if !verdict.Valid() {
				return sdlcerr.New(sdlcerr.UnknownVerdict,
					quote(args[2])+" is not a verdict",
					"a reviewer approves, blocks, or leaves a note")
			}

			s, _, unlock, err := openStoreForWriting(cmd)
			if err != nil {
				return err
			}
			defer unlock()
			id := storyID
			if id == "" {
				if id, err = activeStory(s, "a review belongs to the story being worked on"); err != nil {
					return err
				}
			}
			content, err := readDocument(cmd, file)
			if err != nil {
				return err
			}
			record, err := s.Record(id)
			if err != nil {
				return err
			}
			subject, err := gateSubject(cmd.Context(), s, id, gate)
			if err != nil {
				return err
			}

			round := record.Round(gate, reviewer.Role) + 1
			path, err := s.WriteReview(id, gate, reviewer.Role, round, content)
			if err != nil {
				return err
			}
			rev := record.AddReview(model.Review{
				Gate: gate, Role: reviewer.Role, Verdict: verdict,
				Subject: subject, File: path, Note: note,
			}, s.Now())
			record.Append("review", string(gate)+" "+reviewer.Role+" "+string(verdict), s.Now())
			if err := s.SaveRecord(record); err != nil {
				return err
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), reviewPayload{
					OK: true, Story: id, Gate: string(gate), Role: reviewer.Role,
					Verdict: string(verdict), Round: rev.Round, Path: path, Subject: subject,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %s (round %d)\n  %s\n",
				id, gate, reviewer.Role, verdict, rev.Round, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the review from this path instead of standard input")
	cmd.Flags().StringVar(&note, "note", "", "the headline finding, in one line")
	cmd.Flags().StringVar(&storyID, "story", "", "record against this story instead of the one being worked on")
	return cmd
}

func newReviewListCmd() *cobra.Command {
	var storyID, gateName string
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "Show which reviews each gate expects and where they have got to",
		Example: "  sdlc review list\n  sdlc review list --gate code_review",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var only model.Gate
			if gateName != "" {
				only = model.Gate(gateName)
				if !only.Valid() {
					return sdlcerr.New(sdlcerr.UnknownGate,
						"there is no gate called "+quote(gateName),
						"this version knows "+model.GateNames())
				}
			}

			s, _, err := openStore()
			if err != nil {
				return err
			}
			id := storyID
			if id == "" {
				if id, err = activeStory(s, "reviews belong to the story being worked on"); err != nil {
					return err
				}
			}
			record, err := s.Record(id)
			if err != nil {
				return err
			}

			infos := make([]reviewInfo, 0, len(model.Reviewers))
			for _, r := range model.Reviewers {
				if only != "" && r.Gate != only {
					continue
				}
				subject, err := gateSubject(cmd.Context(), s, id, r.Gate)
				if err != nil {
					return err
				}
				info := reviewInfo{
					Gate: string(r.Gate), Role: r.Role,
					Blocking: r.Blocks(record.SecuritySensitive()),
				}
				if latest, ok := record.LatestReview(r.Gate, r.Role); ok {
					info.Verdict, info.Round = string(latest.Verdict), latest.Round
					info.Stale = latest.Subject != subject
				}
				infos = append(infos, info)
			}

			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), reviewListPayload{OK: true, Story: id, Reviews: infos})
			}
			w := cmd.OutOrStdout()
			for _, i := range infos {
				fmt.Fprintf(w, "  %-15s %-15s %-9s %s\n", i.Gate, i.Role,
					map[bool]string{true: "blocking", false: "advisory"}[i.Blocking],
					describeReviewState(i))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&gateName, "gate", "", "only this gate's reviews")
	cmd.Flags().StringVar(&storyID, "story", "", "for this story instead of the one being worked on")
	return cmd
}

func describeReviewState(i reviewInfo) string {
	switch {
	case i.Verdict == "":
		return "not reviewed"
	case i.Stale:
		return i.Verdict + " (round " + strconv.Itoa(i.Round) + ", stale -- what was reviewed has changed since)"
	default:
		return i.Verdict + " (round " + strconv.Itoa(i.Round) + ")"
	}
}

// gateSubject is what a reviewer of this gate is looking at, by content.
//
// The design gate reviews a document, so the document's hash is the subject.
// The gates that review work review the whole tree, because a change anywhere
// can invalidate what they concluded. A gate with no reviewers has no subject,
// and that is not an error.
func gateSubject(ctx context.Context, s *store.Store, id string, gate model.Gate) (string, error) {
	switch gate {
	case model.GateDesignReview:
		// No plan yet is a state, not a failure. It leaves the subject empty,
		// which makes every review of a plan that is not there stale by
		// definition -- exactly the right answer.
		plan, ok := model.FindArtifact("plan")
		if !ok || !s.ArtifactStored(id, plan) {
			return "", nil
		}
		return s.HashFile(store.ArtifactPath(id, plan))
	case model.GateVerifierReview, model.GateCodeReview:
		return gitx.TreeHash(ctx, s.Root())
	default:
		return "", nil
	}
}

// requireReviews holds a reviewed gate to what being reviewed means.
func requireReviews(ctx context.Context, s *store.Store, id string, gate model.Gate,
	record *model.Record,
) error {
	reviewers := model.ReviewersFor(gate)
	if len(reviewers) == 0 {
		return nil
	}
	subject, err := gateSubject(ctx, s, id, gate)
	if err != nil {
		return err
	}

	var outstanding, blocked []string
	for _, r := range reviewers {
		blocking := r.Blocks(record.SecuritySensitive())
		latest, ok := record.LatestReview(gate, r.Role)
		switch {
		case !ok:
			outstanding = append(outstanding, r.Role+" has not reviewed")
		case latest.Subject != subject:
			outstanding = append(outstanding, r.Role+" reviewed something that has changed since")
		case blocking && latest.Verdict == model.VerdictBlock:
			blocked = append(blocked, r.Role+" blocked"+describeNote(latest.Note))
		case blocking && latest.Verdict != model.VerdictApprove:
			outstanding = append(outstanding, r.Role+" has not approved")
		}
	}
	if len(blocked) > 0 {
		return sdlcerr.New(sdlcerr.ReviewBlocks,
			string(gate)+" is blocked by a reviewer",
			strings.Join(blocked, "; "))
	}
	if len(outstanding) > 0 {
		return sdlcerr.New(sdlcerr.ReviewsMissing,
			string(gate)+" cannot pass until its reviews are in",
			strings.Join(outstanding, "; "))
	}
	return nil
}

func describeNote(note string) string {
	if strings.TrimSpace(note) == "" {
		return ""
	}
	return ": " + note
}
