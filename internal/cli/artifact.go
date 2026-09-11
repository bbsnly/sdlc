package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// maxArtifact bounds what will be stored. These are documents a person reads,
// not data files, and a runaway agent should not be able to fill a repository.
const maxArtifact = 1 << 20

type artifactPayload struct {
	OK    bool   `json:"ok"`
	Story string `json:"story"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

type artifactListPayload struct {
	OK        bool           `json:"ok"`
	Artifacts []artifactInfo `json:"artifacts"`
}

type artifactInfo struct {
	Name string `json:"name"`
	File string `json:"file"`
	Gate string `json:"gate"`
	Role string `json:"role"`
}

func newArtifactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Store the documents a gate produces",
		Long: "A gate's documents -- the analysis, the threat assessment -- are loop state,\n" +
			"and loop state is written by sdlc rather than edited in place. Writing them\n" +
			"through this command is also the only way that works: Claude Code refuses a\n" +
			"subagent's file write when the name reads like a report.",
		Example: "  sdlc artifact list\n  sdlc artifact write analysis < notes.md",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newArtifactWriteCmd(), newArtifactListCmd())
	return cmd
}

func newArtifactListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List the documents this version can store",
		Example: "  sdlc artifact list",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			infos := make([]artifactInfo, 0, len(model.Artifacts))
			for _, a := range model.Artifacts {
				infos = append(infos, artifactInfo{
					Name: a.Name, File: a.File, Gate: string(a.Gate), Role: a.Role,
				})
			}
			if wantJSON(cmd) {
				return emitJSON(cmd.OutOrStdout(), artifactListPayload{OK: true, Artifacts: infos})
			}
			w := cmd.OutOrStdout()
			for _, a := range infos {
				fmt.Fprintf(w, "  %-10s %-14s gate %-10s written by the %s agent\n",
					a.Name, a.File, a.Gate, a.Role)
			}
			return nil
		},
	}
}

func newArtifactWriteCmd() *cobra.Command {
	var storyID, file string
	cmd := &cobra.Command{
		Use:   "write NAME",
		Short: "Store one of a gate's documents for the story being worked on",
		Long: "write stores a document under the story's directory, where the gates after\n" +
			"it will read it.\n\n" +
			"The content comes from standard input, or from --file. Writing it again\n" +
			"replaces it: a gate that ran twice has one record, not two.",
		Example: "  sdlc artifact write analysis < analysis.md\n" +
			"  sdlc artifact write threats --file /tmp/threats.md\n" +
			"  sdlc artifact write analysis <<'MD'\n  # Analysis\n  ...\n  MD",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			artifact, ok := model.FindArtifact(args[0])
			if !ok {
				return sdlcerr.New(sdlcerr.UnknownArtifact,
					"there is no document called "+quote(args[0]),
					"this version stores "+model.ArtifactNames())
			}

			s, _, err := openStore()
			if err != nil {
				return err
			}
			id := storyID
			if id == "" {
				if id, err = activeStory(s, "a gate's documents belong to the story being worked on"); err != nil {
					return err
				}
			}

			content, err := readDocument(cmd, file)
			if err != nil {
				return err
			}
			path, err := s.WriteArtifact(id, artifact, content)
			if err != nil {
				return err
			}
			return reportArtifact(cmd, s, id, artifact, path, len(content))
		},
	}
	cmd.Flags().StringVar(&file, "file", "",
		"read the document from this path instead of standard input")
	cmd.Flags().StringVar(&storyID, "story", "",
		"store against this story instead of the one being worked on")
	return cmd
}

// readDocument reads a gate's document from --file or standard input.
//
// The size limit is checked rather than applied. Storing the first megabyte of
// a longer document would leave every gate after this one reviewing something
// that stops mid-sentence, which is a worse failure than refusing outright.
func readDocument(cmd *cobra.Command, file string) ([]byte, error) {
	src, source := cmd.InOrStdin(), "standard input"
	if file != "" {
		source = file
		f, err := os.Open(file)
		if err != nil {
			return nil, sdlcerr.New(sdlcerr.ArtifactUnreadable,
				"the document could not be read from "+file,
				"opening it failed").WithCause(err)
		}
		defer f.Close()
		src = f
	}

	content, err := io.ReadAll(io.LimitReader(src, maxArtifact+1))
	if err != nil {
		return nil, sdlcerr.New(sdlcerr.ArtifactUnreadable,
			"the document could not be read from "+source,
			"reading stopped part way through").WithCause(err)
	}
	switch {
	case len(content) > maxArtifact:
		return nil, sdlcerr.New(sdlcerr.ArtifactTooLarge,
			"the document from "+source+" is larger than "+
				strconv.Itoa(maxArtifact>>20)+" MiB",
			"a gate's document is prose that a person reads and that the gates after "+
				"it review")
	case len(bytes.TrimSpace(content)) == 0:
		return nil, sdlcerr.New(sdlcerr.EmptyArtifact,
			"there was nothing to store",
			"the document read from "+source+" was empty")
	}
	if !bytes.HasSuffix(content, []byte("\n")) {
		content = append(content, '\n')
	}
	return content, nil
}

func reportArtifact(cmd *cobra.Command, s *store.Store, id string,
	a model.Artifact, path string, size int,
) error {
	if err := appendEvent(s, id, "artifact", a.Name+" stored at "+path); err != nil {
		return err
	}

	if wantJSON(cmd) {
		return emitJSON(cmd.OutOrStdout(), artifactPayload{
			OK: true, Story: id, Name: a.Name, Path: path, Bytes: size,
		})
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s (%d bytes)\n", id, a.Name, path, size)
	return err
}
