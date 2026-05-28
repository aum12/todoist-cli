
package cli

import (
	"github.com/spf13/cobra"
)

func newTasksCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "tasks",
		Short:       "Get, create, update, and delete tasks",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newTasksCompletedByCompletionDateCmd(flags))
	cmd.AddCommand(newTasksCompletedByDueDateCmd(flags))
	cmd.AddCommand(newTasksCreateCmd(flags))
	cmd.AddCommand(newTasksDeleteCmd(flags))
	cmd.AddCommand(newTasksGetCmd(flags))
	cmd.AddCommand(newTasksGetByFilterCmd(flags))
	cmd.AddCommand(newTasksGetProductivityStatsCmd(flags))
	cmd.AddCommand(newTasksGetTaskidCmd(flags))
	cmd.AddCommand(newTasksQuickAddCmd(flags))
	cmd.AddCommand(newTasksUpdateCmd(flags))
	cmd.AddCommand(newTasksCloseCmd(flags))
	cmd.AddCommand(newTasksMoveCmd(flags))
	cmd.AddCommand(newTasksReopenCmd(flags))
	return cmd
}
