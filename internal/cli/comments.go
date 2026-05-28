
package cli

import (
	"github.com/spf13/cobra"
)

func newCommentsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "comments",
		Short:       "Get, create, update, and delete comments",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newCommentsCreateCmd(flags))
	cmd.AddCommand(newCommentsDeleteCmd(flags))
	cmd.AddCommand(newCommentsGetCmd(flags))
	cmd.AddCommand(newCommentsGetCommentidCmd(flags))
	cmd.AddCommand(newCommentsUpdateCmd(flags))
	return cmd
}
