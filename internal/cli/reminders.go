
package cli

import (
	"github.com/spf13/cobra"
)

func newRemindersCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "reminders",
		Short:       "_Availability of reminders is dependent on the current user plan._",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newRemindersCreateCmd(flags))
	cmd.AddCommand(newRemindersDeleteCmd(flags))
	cmd.AddCommand(newRemindersGetCmd(flags))
	cmd.AddCommand(newRemindersGetReminderidCmd(flags))
	cmd.AddCommand(newRemindersUpdateCmd(flags))
	return cmd
}
