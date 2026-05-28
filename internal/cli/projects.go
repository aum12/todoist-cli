
package cli

import (
	"github.com/spf13/cobra"
)

func newProjectsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "projects",
		Short:       "Get, search, create, update, and delete projects",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newProjectsCreateCmd(flags))
	cmd.AddCommand(newProjectsDeleteCmd(flags))
	cmd.AddCommand(newProjectsGetCmd(flags))
	cmd.AddCommand(newProjectsGetArchivedCmd(flags))
	cmd.AddCommand(newProjectsGetProjectidCmd(flags))
	cmd.AddCommand(newProjectsPermissionsCmd(flags))
	cmd.AddCommand(newProjectsSearchCmd(flags))
	cmd.AddCommand(newProjectsUpdateCmd(flags))
	cmd.AddCommand(newProjectsArchiveCmd(flags))
	cmd.AddCommand(newProjectsCollaboratorsCmd(flags))
	cmd.AddCommand(newProjectsJoinCmd(flags))
	cmd.AddCommand(newProjectsUnarchiveCmd(flags))
	return cmd
}
