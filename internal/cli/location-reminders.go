
package cli

import (
	"github.com/spf13/cobra"
)

func newLocationRemindersCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "location-reminders",
		Short:       "_Availability of location reminders is dependent on the current user plan._",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newLocationRemindersCreateCmd(flags))
	cmd.AddCommand(newLocationRemindersDeleteCmd(flags))
	cmd.AddCommand(newLocationRemindersGetCmd(flags))
	cmd.AddCommand(newLocationRemindersGetLocationremindersCmd(flags))
	cmd.AddCommand(newLocationRemindersUpdateCmd(flags))
	return cmd
}
