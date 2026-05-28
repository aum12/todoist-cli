
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newActivitiesPromotedCmd(flags *rootFlags) *cobra.Command {
	var flagObjectType string
	var flagObjectId string
	var flagParentProjectId string
	var flagParentItemId string
	var flagIncludeParentObject bool
	var flagIncludeChildObjects bool
	var flagInitiatorId string
	var flagInitiatorIdNull string
	var flagEventType string
	var flagEnsureLastState bool
	var flagObjectEventTypes string
	var flagWorkspaceId string
	var flagAnnotateNotes bool
	var flagAnnotateParents bool
	var flagCursor string
	var flagLimit int
	var flagDateFrom string
	var flagDateTo string
	var flagAll bool

	cmd := &cobra.Command{
		Use:         "activities",
		Short:       "Get activity logs. Returns a paginated list of activity events for the user.",
		Long:        "Get activity logs. Returns a paginated list of activity events for the user.",
		Example:     "  todoist-aum activities",
		Annotations: map[string]string{"pp:endpoint": "activities.get-activity-logs", "pp:method": "GET", "pp:path": "/api/v1/activities", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.newClient()
			if err != nil {
				return err
			}

			path := "/api/v1/activities"
			data, prov, err := resolvePaginatedReadWithStrategy(cmd.Context(), c, flags, "auto", "activities", path, map[string]string{
				"object_type":           fmt.Sprintf("%v", flagObjectType),
				"object_id":             fmt.Sprintf("%v", flagObjectId),
				"parent_project_id":     fmt.Sprintf("%v", flagParentProjectId),
				"parent_item_id":        fmt.Sprintf("%v", flagParentItemId),
				"include_parent_object": fmt.Sprintf("%v", flagIncludeParentObject),
				"include_child_objects": fmt.Sprintf("%v", flagIncludeChildObjects),
				"initiator_id":          fmt.Sprintf("%v", flagInitiatorId),
				"initiator_id_null":     fmt.Sprintf("%v", flagInitiatorIdNull),
				"event_type":            fmt.Sprintf("%v", flagEventType),
				"ensure_last_state":     fmt.Sprintf("%v", flagEnsureLastState),
				"object_event_types":    fmt.Sprintf("%v", flagObjectEventTypes),
				"workspace_id":          fmt.Sprintf("%v", flagWorkspaceId),
				"annotate_notes":        fmt.Sprintf("%v", flagAnnotateNotes),
				"annotate_parents":      fmt.Sprintf("%v", flagAnnotateParents),
				"cursor":                fmt.Sprintf("%v", flagCursor),
				"limit":                 fmt.Sprintf("%v", flagLimit),
				"date_from":             fmt.Sprintf("%v", flagDateFrom),
				"date_to":               fmt.Sprintf("%v", flagDateTo),
			}, nil, flagAll, "cursor", "cursor", "limit", "next_cursor", "", cmd.ErrOrStderr())
			if err != nil {
				return classifyAPIError(err, flags)
			}
			// Print provenance to stderr for human-facing output only.
			// Machine-format flags (--json, --csv, --compact, --quiet, --plain,
			// --select) and piped stdout suppress this line; the JSON envelope
			// already carries meta.source for those consumers.
			// SYNC: keep this gate aligned with command_endpoint.go.tmpl.
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				var countItems []json.RawMessage
				if json.Unmarshal(data, &countItems) != nil {
					// Single object, not an array
					countItems = []json.RawMessage{data}
				}
				printProvenance(cmd, len(countItems), prov)
			}
			// For JSON output, wrap with provenance envelope. --select wins over
			// --compact when both are set; --compact only runs when no explicit
			// fields were requested. Explicit format flags (--csv, --quiet, --plain)
			// opt out of the auto-JSON path so piped consumers that asked for a
			// non-JSON format reach the standard pipeline below.
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
	cmd.Flags().StringVar(&flagObjectType, "object-type", "", "Object type")
	cmd.Flags().StringVar(&flagObjectId, "object-id", "", "Object id")
	cmd.Flags().StringVar(&flagParentProjectId, "parent-project-id", "", "Parent project id")
	cmd.Flags().StringVar(&flagParentItemId, "parent-item-id", "", "Parent item id")
	cmd.Flags().BoolVar(&flagIncludeParentObject, "include-parent-object", false, "Include parent object")
	cmd.Flags().BoolVar(&flagIncludeChildObjects, "include-child-objects", false, "Include child objects")
	cmd.Flags().StringVar(&flagInitiatorId, "initiator-id", "", "Initiator id")
	cmd.Flags().StringVar(&flagInitiatorIdNull, "initiator-id-null", "", "Initiator id null")
	cmd.Flags().StringVar(&flagEventType, "event-type", "", "Event type")
	cmd.Flags().BoolVar(&flagEnsureLastState, "ensure-last-state", false, "Ensure last state")
	cmd.Flags().StringVar(&flagObjectEventTypes, "object-event-types", "", "Object event types")
	cmd.Flags().StringVar(&flagWorkspaceId, "workspace-id", "", "Workspace id")
	cmd.Flags().BoolVar(&flagAnnotateNotes, "annotate-notes", false, "Annotate notes")
	cmd.Flags().BoolVar(&flagAnnotateParents, "annotate-parents", false, "Annotate parents")
	cmd.Flags().StringVar(&flagCursor, "cursor", "", "Cursor")
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "Limit")
	cmd.Flags().StringVar(&flagDateFrom, "date-from", "", "Date from")
	cmd.Flags().StringVar(&flagDateTo, "date-to", "", "Date to")
	cmd.Flags().BoolVar(&flagAll, "all", false, "Fetch all pages")

	// Wire sibling endpoints and sub-resources as subcommands

	return cmd
}
