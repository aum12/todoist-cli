
package cli

import (
	"github.com/spf13/cobra"
)

func newTasksMoveCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "move",
		Short:       "Create move for tasks",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newTasksMoveTaskCmd(flags))
	return cmd
}
