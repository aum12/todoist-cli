
package cli

import (
	"github.com/spf13/cobra"
)

func newProjectsArchiveCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "archive",
		Short:       "Run archive operations for projects",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newProjectsArchiveProjectCmd(flags))
	return cmd
}
