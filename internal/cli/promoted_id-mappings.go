
package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newIdMappingsPromotedCmd(flags *rootFlags) *cobra.Command {
	var flagObjName string

	cmd := &cobra.Command{
		Use:         "id-mappings <obj_ids>",
		Short:       "Translates IDs from v1 to v2 or vice versa.",
		Long:        "Translates IDs from v1 to v2 or vice versa.",
		Example:     "  todoist-aum id-mappings example-value --obj-name sections",
		Annotations: map[string]string{"pp:endpoint": "id-mappings.id_mappings", "pp:method": "GET", "pp:path": "/api/v1/id_mappings/{obj_name}/{obj_ids}", "mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("obj-name") {
				allowedObjName := []string{"sections", "tasks", "comments", "reminders", "location_reminders", "projects"}
				validObjName := false
				for _, v := range allowedObjName {
					if flagObjName == v {
						validObjName = true
						break
					}
				}
				if !validObjName {
					return fmt.Errorf("invalid value %q for --%s: must be one of %v", flagObjName, "obj-name", allowedObjName)
				}
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}

			path := "/api/v1/id_mappings/{obj_name}/{obj_ids}"
			if len(args) < 1 {
				// JSON envelope: {error, usage}. Written first; the
				// usageErr return preserves exit code 2 across modes.
				if flags.asJSON {
					if printErr := printJSONFiltered(cmd.OutOrStdout(), map[string]any{
						"error": "obj_ids is required",
						"usage": fmt.Sprintf("%s <%s>", cmd.CommandPath(), "obj_ids"),
					}, flags); printErr != nil {
						return printErr
					}
				}
				return usageErr(fmt.Errorf("obj_ids is required\nUsage: %s <%s>", cmd.CommandPath(), "obj_ids"))
			}
			path = replacePathParam(path, "obj_ids", args[0])
			path = replacePathParam(path, "obj_name", fmt.Sprintf("%v", flagObjName))
			params := map[string]string{}
			data, prov, err := resolveReadWithStrategy(cmd.Context(), c, flags, "auto", "id-mappings", false, path, params, nil, cmd.ErrOrStderr())
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
	cmd.Flags().StringVar(&flagObjName, "obj-name", "sections", "Obj name (one of: sections, tasks, comments, reminders, location_reminders, projects)")

	// Wire sibling endpoints and sub-resources as subcommands

	return cmd
}
