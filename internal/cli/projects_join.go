
package cli

import (
	"github.com/spf13/cobra"
)

func newProjectsJoinCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "join",
		Short:       "Create join for projects",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newProjectsJoinJoinCmd(flags))
	return cmd
}
