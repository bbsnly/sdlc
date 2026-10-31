package cli

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

type artifactDiffPayload struct {
	OK              bool   `json:"ok"`
	Story           string `json:"story"`
	Role            string `json:"role"`
	ReviewedSubject string `json:"reviewed_subject"`
	Subject         string `json:"subject"`
	Diff            string `json:"diff"`
}

func newArtifactDiffCmd() *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "diff plan",
		Short: "Show what changed in the plan since a reviewer last reviewed it",
		Long: "diff prints a unified diff from the plan the reviewer's latest design review\n" +
			"was stamped with to the current PLAN.md, for a reviewer asked to look again.\n" +
			"It prints nothing where nothing changed, and it records nothing: every reviewer\n" +
			"still records a verdict on the plan as it now stands.\n\n" +
			"Only the plan keeps revisions. Where the revision cannot be given, because it\n" +
			"was not kept or no longer matches its name, the command refuses with SDLC-E0084\n" +
			"and that reviewer reads the whole plan this round.",
		Example: "  sdlc artifact diff plan --since architect",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactDiff(cmd, args[0], since)
		},
	}
	// Not MarkFlagRequired: that refuses as a command line not understood
	// (SDLC-E0034), and says nothing of whom to name.
	cmd.Flags().StringVar(&since, "since", "",
		"required: the design reviewer whose latest review the diff starts from")
	return cmd
}

func runArtifactDiff(cmd *cobra.Command, name, role string) error {
	artifact, ok := model.FindArtifact(name)
	if !ok || artifact.Gate != model.GatePlan {
		return sdlcerr.New(sdlcerr.UnknownArtifact,
			"there is no diff of a document called "+quote(name),
			"only the plan keeps its revisions").
			WithFix(`name the plan: "sdlc artifact diff plan --since <role>"`)
	}
	if role == "" {
		return sdlcerr.New(sdlcerr.UnknownReviewer,
			"--since is required",
			"the diff starts from the plan that reviewer last reviewed; the design review expects "+
				model.ReviewerNames(model.GateDesignReview)).
			WithFix(`add --since <role>, for example "sdlc artifact diff plan --since architect"`)
	}
	if _, ok := model.FindReviewer(model.GateDesignReview, role); !ok {
		return sdlcerr.New(sdlcerr.UnknownReviewer,
			"--since has to name a design reviewer, and "+quote(role)+" is not one",
			"the diff starts from the plan that reviewer last reviewed; the design review expects "+
				model.ReviewerNames(model.GateDesignReview))
	}

	s, _, err := openStore(cmd)
	if err != nil {
		return err
	}
	id, err := activeStory(s, "a plan's revisions belong to the story being worked on")
	if err != nil {
		return err
	}
	record, err := s.Record(id)
	if err != nil {
		return err
	}
	latest, ok := record.LatestReview(model.GateDesignReview, role)
	if !ok {
		return planNotKept(role, "it has no design review on this story")
	}
	current, err := currentPlan(s, id, artifact)
	if err != nil {
		return err
	}

	// The comparison the gate makes for staleness, made first: if it holds, the
	// gate accepts this review as it stands, so "nothing changed" is true whether
	// or not a revision was kept. Any other answer has `review list` and this
	// command disagree about one review. A review it does not hold for always goes
	// through the check on the kept revision.
	subject, diff := store.ContentHash(current), ""
	if subject != latest.Subject {
		reviewed, err := s.PlanRevision(id, latest.Subject)
		if err != nil {
			return revisionRefused(role, err)
		}
		diff = "--- " + artifact.File + " " + latest.Subject + "\n" +
			"+++ " + artifact.File + " " + subject + "\n" +
			unifiedHunks(string(reviewed), string(current))
	}

	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), artifactDiffPayload{
			OK: true, Story: id, Role: role, ReviewedSubject: latest.Subject, Subject: subject, Diff: diff,
		})
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), diff)
	return err
}

// currentPlan is the new side of the diff, bounded and a regular file as the old
// side is: nothing upstream has checked what stands at PLAN.md. There is no whole
// plan for a reviewer to read here, so this is not SDLC-E0084.
func currentPlan(s *store.Store, id string, plan model.Artifact) ([]byte, error) {
	dir, err := s.StoryDir(id)
	if err != nil {
		return nil, err
	}
	content, why, cause := store.ReadBounded(filepath.Join(dir, plan.File))
	if why != "" {
		return nil, sdlcerr.New(sdlcerr.StateUnreadable, plan.File+" could not be read", why).
			WithFix(`have the implementer store the plan again with "sdlc artifact write plan"`).
			WithCause(cause)
	}
	return content, nil
}

// planNotKept is SDLC-E0084 with the reviewer it is about and the cause in words.
func planNotKept(role, why string) *sdlcerr.Error {
	return sdlcerr.New(sdlcerr.PlanRevisionNotKept,
		"there is no diff for "+role+", which reads the whole plan this round", why)
}

// revisionRefused names the reviewer in the store's refusal, which knows only the
// revision, keeping what the store said of the cause and what to do about it.
func revisionRefused(role string, err error) error {
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.PlanRevisionNotKept {
		return err
	}
	return planNotKept(role, e.Why).WithFix(e.Fix).WithCause(err)
}
