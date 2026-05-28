
package cli

import (
	"github.com/spf13/cobra"
)

func newProjectsCollaboratorsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "collaborators",
		Short:       "Get collaborators for projects",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newProjectsCollaboratorsGetProjectCmd(flags))
	return cmd
}
