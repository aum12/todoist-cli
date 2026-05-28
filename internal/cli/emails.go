
package cli

import (
	"github.com/spf13/cobra"
)

func newEmailsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "emails",
		Short:       "Get and disable emails",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newEmailsDisableCmd(flags))
	cmd.AddCommand(newEmailsGetOrCreateCmd(flags))
	return cmd
}
