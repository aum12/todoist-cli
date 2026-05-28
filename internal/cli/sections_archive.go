
package cli

import (
	"github.com/spf13/cobra"
)

func newSectionsArchiveCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "archive",
		Short:       "Run archive operations for sections",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newSectionsArchiveSectionCmd(flags))
	return cmd
}
