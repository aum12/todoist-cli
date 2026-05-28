
package cli

import (
	"github.com/spf13/cobra"
)

func newTasksReopenCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "reopen",
		Short:       "Create reopen for tasks",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newTasksReopenTaskCmd(flags))
	return cmd
}
