
package cli

import (
	"github.com/spf13/cobra"
)

func newTasksCloseCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "close",
		Short:       "Run close operations for tasks",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newTasksCloseTaskCmd(flags))
	return cmd
}
