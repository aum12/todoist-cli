
package cli

import (
	"github.com/spf13/cobra"
)

func newAccessTokensCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "access-tokens",
		Short:       "Create and delete access tokens",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newAccessTokensMigratePersonalTokenCmd(flags))
	cmd.AddCommand(newAccessTokensRevokeApiCmd(flags))
	return cmd
}
