
package cli

import (
	"github.com/spf13/cobra"
)

func newBackupsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "backups",
		Short:       "_Availability of backups functionality is dependent on the current user plan.",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newBackupsDownloadCmd(flags))
	cmd.AddCommand(newBackupsGetCmd(flags))
	return cmd
}
