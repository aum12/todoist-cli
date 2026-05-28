
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newTasksGetCmd(flags *rootFlags) *cobra.Command {
	var flagProjectId string
	var flagSectionId string
	var flagParentId string
	var flagLabel string
	var flagIds string
	var flagGoalId string
	var flagCursor string
	var flagLimit int
	var flagAll bool

	cmd := &cobra.Command{
		Use:         "get",
		Short:       "Get all active tasks for the user. All provided parameters are used to narrow down the list of tasks.",
		Example:     "  todoist-aum tasks get",
		Annotations: map[string]string{"pp:endpoint": "tasks.get", "pp:method": "GET", "pp:path": "/api/v1/tasks", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.newClient()
			if err != nil {
				return err
			}

			path := "/api/v1/tasks"
			data, prov, err := resolvePaginatedReadWithStrategy(cmd.Context(), c, flags, "auto", "tasks", path, map[string]string{
				"project_id": fmt.Sprintf("%v", flagProjectId),
				"section_id": fmt.Sprintf("%v", flagSectionId),
				"parent_id":  fmt.Sprintf("%v", flagParentId),
				"label":      fmt.Sprintf("%v", flagLabel),
				"ids":        fmt.Sprintf("%v", flagIds),
				"goal_id":    fmt.Sprintf("%v", flagGoalId),
				"cursor":     fmt.Sprintf("%v", flagCursor),
				"limit":      fmt.Sprintf("%v", flagLimit),
			}, nil, flagAll, "cursor", "cursor", "limit", "next_cursor", "", cmd.ErrOrStderr())
			if err != nil {
				return classifyAPIError(err, flags)
			}
			// Print provenance to stderr for human-facing output only.
			// Machine-format flags (--json, --csv, --compact, --quiet, --plain,
			// --select) and piped stdout suppress this line; the JSON envelope
			// already carries meta.source for those consumers.
			// SYNC: keep this gate aligned with command_promoted.go.tmpl.
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				var countItems []json.RawMessage
				_ = json.Unmarshal(data, &countItems)
				printProvenance(cmd, len(countItems), prov)
			}
			// For JSON output, wrap with provenance envelope before passing through flags.
			// --select wins over --compact when both are set; --compact only runs when
			// no explicit fields were requested. Explicit format flags (--csv, --quiet,
			// --plain) opt out of the auto-JSON path so piped consumers that asked for
			// a non-JSON format reach the standard pipeline below.
			if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !flags.csv && !flags.quiet && !flags.plain) {
				filtered := data
				if flags.selectFields != "" {
					filtered = filterFields(filtered, flags.selectFields)
				} else if flags.compact {
					filtered = compactFields(filtered)
				}
				wrapped, wrapErr := wrapWithProvenance(filtered, prov)
				if wrapErr != nil {
					return wrapErr
				}
				return printOutput(cmd.OutOrStdout(), wrapped, true)
			}
			// For all other output modes (table, csv, plain, quiet), use the standard pipeline
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				var items []map[string]any
				if json.Unmarshal(data, &items) == nil && len(items) > 0 {
					if err := printAutoTable(cmd.OutOrStdout(), items); err != nil {
						return err
					}
					if len(items) >= 25 {
						fmt.Fprintf(os.Stderr, "\nShowing %d results. To narrow: add --limit, --json --select, or filter flags.\n", len(items))
					}
					return nil
				}
			}
			return printOutputWithFlags(cmd.OutOrStdout(), data, flags)
		},
	}
	cmd.Flags().StringVar(&flagProjectId, "project-id", "", "Project id")
	cmd.Flags().StringVar(&flagSectionId, "section-id", "", "Section id")
	cmd.Flags().StringVar(&flagParentId, "parent-id", "", "Parent id")
	cmd.Flags().StringVar(&flagLabel, "label", "", "Label")
	cmd.Flags().StringVar(&flagIds, "ids", "", "Ids")
	cmd.Flags().StringVar(&flagGoalId, "goal-id", "", "Goal id")
	cmd.Flags().StringVar(&flagCursor, "cursor", "", "Cursor")
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "Limit")
	cmd.Flags().BoolVar(&flagAll, "all", false, "Fetch all pages")

	return cmd
}
