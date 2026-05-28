
package cli

import (
	"github.com/spf13/cobra"
)

func newTemplatesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "templates",
		Short:       "Templates allow exporting of a project's tasks to a file or URL",
		Hidden:      true,
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE:        parentNoSubcommandRunE(flags),
	}

	cmd.AddCommand(newTemplatesCreateCmd(flags))
	cmd.AddCommand(newTemplatesExportAsFileCmd(flags))
	cmd.AddCommand(newTemplatesExportAsUrlCmd(flags))
	cmd.AddCommand(newTemplatesImportIntoProjectFromFileCmd(flags))
	cmd.AddCommand(newTemplatesImportIntoProjectFromIdCmd(flags))
	return cmd
}
