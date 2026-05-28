
package cli

import (
	"github.com/spf13/cobra"
)

func newUploadsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "uploads",
		Short:       "Availability of uploads functionality and the maximum size for a file attachment are dependent on the current user plan.",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newUploadsDeleteCmd(flags))
	cmd.AddCommand(newUploadsFileCmd(flags))
	return cmd
}
