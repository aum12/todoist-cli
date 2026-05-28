
package cli

import (
	"github.com/spf13/cobra"
)

func newSectionsUnarchiveCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "unarchive",
		Short:       "Run unarchive operations for sections",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newSectionsUnarchiveSectionCmd(flags))
	return cmd
}
