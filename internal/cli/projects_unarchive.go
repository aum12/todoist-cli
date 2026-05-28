
package cli

import (
	"github.com/spf13/cobra"
)

func newProjectsUnarchiveCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "unarchive",
		Short:       "Run unarchive operations for projects",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newProjectsUnarchiveProjectCmd(flags))
	return cmd
}
