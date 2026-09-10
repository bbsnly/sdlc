package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "doctor",
		Short:   "Check this machine and say how to fix what is wrong",
		Example: "  sdlc doctor",
		Args:    cobra.NoArgs,
		Long: "doctor inspects the installation and reports what is wrong and how to fix it.\n\n" +
			"Every check that can fail prints the command that fixes it. A check that\n" +
			"only reports a problem leaves the reader to guess, which is how a tool\n" +
			"teaches people to ignore it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(),
				"doctor has no checks yet: the things it would inspect do not exist.\n"+
					"It reports that plainly rather than printing a row of green ticks\n"+
					"for work that has not been done.")
			return err
		},
	}
}
