
package cli

import (
	"github.com/spf13/cobra"
)

func newLabelsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "labels",
		Short:       "Get, search, create, update, and delete labels",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newLabelsCreateCmd(flags))
	cmd.AddCommand(newLabelsDeleteCmd(flags))
	cmd.AddCommand(newLabelsGetCmd(flags))
	cmd.AddCommand(newLabelsGetLabelidCmd(flags))
	cmd.AddCommand(newLabelsSearchCmd(flags))
	cmd.AddCommand(newLabelsSharedCmd(flags))
	cmd.AddCommand(newLabelsSharedRemoveCmd(flags))
	cmd.AddCommand(newLabelsSharedRenameCmd(flags))
	cmd.AddCommand(newLabelsUpdateCmd(flags))
	return cmd
}
