package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

type ackPayload struct {
	OK      bool   `json:"ok"`
	Through string `json:"through"`
	// Previous is the commit acknowledged before: in full when git has it, and as
	// the file named it when git does not. Absent when nothing was.
	Previous string `json:"previous,omitempty"`
	// Back is set when Through comes before Previous, so the log lists the
	// stories committed in between again.
	Back bool `json:"back"`
}

func newAckCmd() *cobra.Command {
	var through string
	cmd := &cobra.Command{
		Use:   "ack",
		Short: "Mark the log read up to a commit, so it starts after it",
		Long: "ack records the commit you have read `sdlc log` up to. The log starts after\n" +
			"it from then on, so what it lists is what you have not read yet.\n\n" +
			"--through is required, and names a commit on this branch: HEAD, or one in\n" +
			"its history. A commit before the one acknowledged is allowed, and the log\n" +
			"then lists the stories in between again.\n\n" +
			"Run it in your own terminal. The hook refuses it from a tool call, because an\n" +
			"agent that could run it would be deciding which stories nobody needs to look\n" +
			"at. It changes nothing but .sdlc/state/acknowledged.",
		Example: "  sdlc ack --through HEAD\n  sdlc ack --through 3f2a9c1e0b7d",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return runAck(cmd, through) },
	}
	cmd.Flags().StringVar(&through, "through", "", "the commit you have read the log up to. Required")
	return cmd
}

func runAck(cmd *cobra.Command, through string) error {
	if strings.TrimSpace(through) == "" {
		return usage(cmd, "ack needs --through: the commit you have read the log up to")
	}
	// The lock is for the commit acknowledged before: it is read and
	// replaced here, and two acks at once would each report the other's.
	s, _, unlock, err := openStoreForWriting(cmd)
	if err != nil {
		return err
	}
	defer unlock()
	ctx, root := cmd.Context(), s.Root()

	// HEAD first: git that does not run fails here, with its own reason,
	// rather than as a commit that is not there.
	head, err := gitx.Head(ctx, root)
	if err != nil {
		return err
	}
	sha, err := ackTarget(cmd, root, through, head)
	if err != nil {
		return err
	}

	payload := ackPayload{OK: true, Through: sha}
	named, _, err := s.Acknowledged()
	if err != nil {
		return err
	}
	if named != "" {
		if err := notePreviousAck(cmd, root, named, &payload); err != nil {
			return err
		}
	}
	if err := s.Acknowledge(sha); err != nil {
		return err
	}

	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), payload)
	}
	printAck(cmd.OutOrStdout(), payload)
	return nil
}

// ackTarget resolves --through to a commit that is on the branch at head.
func ackTarget(cmd *cobra.Command, root, through, head string) (string, error) {
	ctx := cmd.Context()
	sha, err := gitx.Resolve(ctx, root, through)
	if err != nil {
		if !gitx.NamesNoCommit(err) {
			return "", err
		}
		return "", sdlcerr.New(sdlcerr.BadArgument,
			"--through "+quote(through)+" names no commit in this repository",
			"the log starts after the commit acknowledged, and git cannot read that as one").
			WithFix(`give a commit git knows: a hash, a tag, or a name like HEAD`).
			WithCause(err)
	}
	// A branch with no commits yet, such as a new orphan branch, holds
	// none of the commits the repository has.
	onBranch := false
	if head != gitx.NoCommits {
		if onBranch, err = gitx.IsAncestor(ctx, root, sha, head); err != nil {
			return "", err
		}
	}
	if !onBranch {
		return "", sdlcerr.New(sdlcerr.BadArgument,
			"--through "+quote(through)+" is not on this branch",
			"the log lists the stories in HEAD's history, and a commit outside it says "+
				"nothing about which of those were read").
			WithFix(`acknowledge HEAD or a commit in its history; "git log" lists them`)
	}
	return sha, nil
}

// notePreviousAck fills in the commit acknowledged before, and whether the new
// one is behind it.
func notePreviousAck(cmd *cobra.Command, root, named string, payload *ackPayload) error {
	ctx := cmd.Context()
	payload.Previous = named
	// One git cannot read is what this is run to replace, and says so
	// as the file named it.
	previous, err := gitx.Resolve(ctx, root, named)
	switch {
	case err == nil:
		payload.Previous = previous
		if previous != payload.Through {
			if payload.Back, err = gitx.IsAncestor(ctx, root, payload.Through, previous); err != nil {
				return err
			}
		}
	case !gitx.NamesNoCommit(err):
		return err
	}
	return nil
}

func printAck(w io.Writer, p ackPayload) {
	fmt.Fprintf(w, "Acknowledged through %s. `sdlc log` starts after it.\n", shortCommit(p.Through))
	switch {
	case p.Previous == "":
	case p.Previous == p.Through:
		fmt.Fprintln(w, "It already was.")
	case p.Back:
		fmt.Fprintf(w, "That is before %s, the commit acknowledged until now, so the log lists "+
			"the stories committed in between again.\n", shortCommit(p.Previous))
	default:
		fmt.Fprintf(w, "It was %s until now.\n", shortCommit(p.Previous))
	}
}
