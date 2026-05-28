
package cli

import (
	"github.com/spf13/cobra"
)

func newSectionsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "sections",
		Short:       "Get, search, create, update, and delete sections",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newSectionsCreateCmd(flags))
	cmd.AddCommand(newSectionsDeleteCmd(flags))
	cmd.AddCommand(newSectionsGetCmd(flags))
	cmd.AddCommand(newSectionsGetSectionidCmd(flags))
	cmd.AddCommand(newSectionsSearchCmd(flags))
	cmd.AddCommand(newSectionsUpdateCmd(flags))
	cmd.AddCommand(newSectionsArchiveCmd(flags))
	cmd.AddCommand(newSectionsUnarchiveCmd(flags))
	return cmd
}
