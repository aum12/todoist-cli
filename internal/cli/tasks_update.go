
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/aum12/todoist-cli/internal/cliutil"
)

func newTasksUpdateCmd(flags *rootFlags) *cobra.Command {
	var bodyAssigneeId string
	var bodyChildOrder int
	var bodyContent string
	var bodyDayOrder int
	var bodyDeadlineDate string
	var bodyDescription string
	var bodyDueDate string
	var bodyDueDatetime string
	var bodyDueLang string
	var bodyDueString string
	var bodyDuration string
	var bodyDurationUnit string
	var bodyIsCollapsed bool
	var bodyLabels string
	var bodyPriority int
	var stdinBody bool

	cmd := &cobra.Command{
		Use:         "update <task_id>",
		Short:       "Updates an existing task.",
		Example:     "  todoist-aum tasks update 550e8400-e29b-41d4-a716-446655440000",
		Annotations: map[string]string{"pp:endpoint": "tasks.update", "pp:method": "POST", "pp:path": "/api/v1/tasks/{task_id}"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if !stdinBody {
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}

			path := "/api/v1/tasks/{task_id}"
			path = replacePathParam(path, "task_id", args[0])
			params := map[string]string{}
			var body map[string]any
			if stdinBody {
				stdinData, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
				var jsonBody map[string]any
				if err := json.Unmarshal(stdinData, &jsonBody); err != nil {
					return fmt.Errorf("parsing stdin JSON: %w", err)
				}
				body = jsonBody
			} else {
				body = map[string]any{}
				if bodyAssigneeId != "" {
					body["assignee_id"] = bodyAssigneeId
				}
				if bodyChildOrder != 0 {
					body["child_order"] = bodyChildOrder
				}
				if bodyContent != "" {
					body["content"] = bodyContent
				}
				if bodyDayOrder != 0 {
					body["day_order"] = bodyDayOrder
				}
				if bodyDeadlineDate != "" {
					body["deadline_date"] = bodyDeadlineDate
				}
				if bodyDescription != "" {
					body["description"] = bodyDescription
				}
				if bodyDueDate != "" {
					body["due_date"] = bodyDueDate
				}
				if bodyDueDatetime != "" {
					body["due_datetime"] = bodyDueDatetime
				}
				if bodyDueLang != "" {
					body["due_lang"] = bodyDueLang
				}
				if bodyDueString != "" {
					body["due_string"] = bodyDueString
				}
				if bodyDuration != "" {
					body["duration"] = bodyDuration
				}
				if bodyDurationUnit != "" {
					body["duration_unit"] = bodyDurationUnit
				}
				if cmd.Flags().Changed("is-collapsed") {
					body["is_collapsed"] = bodyIsCollapsed
				}
				if bodyLabels != "" {
					body["labels"] = cliutil.SplitCSV(bodyLabels)
				}
				if bodyPriority != 0 {
					body["priority"] = bodyPriority
				}
			}
			data, statusCode, err := c.PostWithParams(cmd.Context(), path, params, body)
			if err != nil {
				return classifyAPIError(err, flags)
			}
			// Inspect the mutate response body for a partial-failure-shaped
			// field (e.g. Google Ads `partialFailureError`). Several Google
			// APIs return 200 OK with a partial-failure field when some
			// operations in the batch failed; ignoring it silently swallows
			// real failures. Detection runs before output-mode selection so
			// the exit code is consistent regardless of how stdout is
			// rendered. --dry-run short-circuits because no real request
			// was sent.
			var partialFailure *partialFailureReport
			if !flags.dryRun && statusCode >= 200 && statusCode < 300 {
				partialFailure = detectPartialFailure(data)
				if partialFailure != nil {
					fmt.Fprintf(os.Stderr, "warning: partial failure detected in %s response: %s\n", "tasks", partialFailure.Message)
					if len(partialFailure.ResourceNames) > 0 {
						fmt.Fprintf(os.Stderr, "         succeeded: %d operation(s)\n", len(partialFailure.ResourceNames))
					}
				}
			}
			if !flags.dryRun && statusCode >= 200 && statusCode < 300 && (partialFailure == nil || flags.allowPartialFailure) {
				writeMutationResponseToStore(cmd.Context(), "tasks", data, "")
			}
			if wantsHumanTable(cmd.OutOrStdout(), flags) {
				// Check if response contains an array (directly or wrapped in "data")
				var items []map[string]any
				if json.Unmarshal(data, &items) == nil && len(items) > 0 {
					if err := printAutoTable(cmd.OutOrStdout(), items); err != nil {
						fmt.Fprintf(os.Stderr, "warning: table rendering failed, falling back to JSON: %v\n", err)
					} else {
						if partialFailure != nil && !flags.allowPartialFailure {
							return partialFailureErr(fmt.Errorf("partial failure in %s response: %s", "tasks", partialFailure.Message))
						}
						return nil
					}
				} else {
					var wrapped struct {
						Data []map[string]any `json:"data"`
					}
					if json.Unmarshal(data, &wrapped) == nil && len(wrapped.Data) > 0 {
						if err := printAutoTable(cmd.OutOrStdout(), wrapped.Data); err != nil {
							fmt.Fprintf(os.Stderr, "warning: table rendering failed, falling back to JSON: %v\n", err)
						} else {
							if partialFailure != nil && !flags.allowPartialFailure {
								return partialFailureErr(fmt.Errorf("partial failure in %s response: %s", "tasks", partialFailure.Message))
							}
							return nil
						}
					}
				}
			}
			if flags.asJSON || (!isTerminal(cmd.OutOrStdout()) && !flags.csv && !flags.quiet && !flags.plain) {
				if flags.quiet {
					if partialFailure != nil && !flags.allowPartialFailure {
						return partialFailureErr(fmt.Errorf("partial failure in %s response: %s", "tasks", partialFailure.Message))
					}
					return nil
				}
				envelope := map[string]any{
					"action":   "post",
					"resource": "tasks",
					"path":     path,
					"status":   statusCode,
					"success":  statusCode >= 200 && statusCode < 300 && (partialFailure == nil || flags.allowPartialFailure),
				}
				if partialFailure != nil {
					envelope["partial_failure"] = partialFailure
				}
				if flags.dryRun {
					envelope["dry_run"] = true
					envelope["status"] = 0
					envelope["success"] = false
				}
				// Verify-mode synthetic envelope detection runs against RAW data
				// (before --compact/--select filtering) so the sentinel field is
				// guaranteed to be visible even if the operator passes a filter
				// flag that would otherwise strip it. Surfaces a top-level
				// verify_noop signal + flips success to false. Mirrors the dry_run
				// shape above.
				if len(data) > 0 {
					var rawParsed any
					if err := json.Unmarshal(data, &rawParsed); err == nil {
						if m, ok := rawParsed.(map[string]any); ok {
							if v, ok := m["__pp_verify_synthetic__"].(bool); ok && v {
								envelope["verify_noop"] = true
								envelope["success"] = false
							}
						}
					}
				}
				// Apply --compact and --select to the API response before wrapping.
				// --select wins when both are set: explicit field choice trumps the
				// generic high-gravity allow-list. Otherwise --compact still applies
				// when --agent is on but the user did not name fields.
				filtered := data
				if flags.selectFields != "" {
					filtered = filterFields(filtered, flags.selectFields)
				} else if flags.compact {
					filtered = compactFields(filtered)
				}
				if len(filtered) > 0 {
					var parsed any
					if err := json.Unmarshal(filtered, &parsed); err == nil {
						envelope["data"] = parsed
					}
				}
				envelopeJSON, err := json.Marshal(envelope)
				if err != nil {
					return err
				}
				if perr := printOutput(cmd.OutOrStdout(), json.RawMessage(envelopeJSON), true); perr != nil {
					return perr
				}
				if partialFailure != nil && !flags.allowPartialFailure {
					return partialFailureErr(fmt.Errorf("partial failure in %s response: %s", "tasks", partialFailure.Message))
				}
				return nil
			}
			// Fall-through for mutate paths that did not hit the table or
			// asJSON branches: --quiet, --csv, --plain, and default terminal
			// raw output. printOutputWithFlags renders the body, then the
			// typed partial-failure exit fires unless --allow-partial-failure
			// downgrades it. Without this guard a partial failure would exit
			// 0 for these output modes — the exact silent-swallow regression
			// the surrounding patch is preventing for asJSON / piped output.
			if perr := printOutputWithFlags(cmd.OutOrStdout(), data, flags); perr != nil {
				return perr
			}
			if partialFailure != nil && !flags.allowPartialFailure {
				return partialFailureErr(fmt.Errorf("partial failure in %s response: %s", "tasks", partialFailure.Message))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&bodyAssigneeId, "assignee-id", "", "ID of the user to assign the task to.")
	cmd.Flags().IntVar(&bodyChildOrder, "child-order", 0, "Updated position of the task in its current scope. Omit this field to keep it unchanged.")
	cmd.Flags().StringVar(&bodyContent, "content", "", "Updated task content. Omit this field to keep it unchanged.")
	cmd.Flags().IntVar(&bodyDayOrder, "day-order", 0, "Updated position of the task in Today and Upcoming views. Omit this field to keep it unchanged.")
	cmd.Flags().StringVar(&bodyDeadlineDate, "deadline-date", "", "Updated deadline date in YYYY-MM-DD format. Pass null to clear the value. Omit this field to keep it unchanged.")
	cmd.Flags().StringVar(&bodyDescription, "description", "", "Updated task description. Omit this field to keep it unchanged.")
	cmd.Flags().StringVar(&bodyDueDate, "due-date", "", "Updated due date in RFC 3339 format or similar. See the [Due dates](#tag/Due-dates) section for more details.")
	cmd.Flags().StringVar(&bodyDueDatetime, "due-datetime", "", "Updated due date and time. See the [Due dates](#tag/Due-dates) section for more details.")
	cmd.Flags().StringVar(&bodyDueLang, "due-lang", "", "Updated due date language code. See the [Due dates](#tag/Due-dates) section for more details.")
	cmd.Flags().StringVar(&bodyDueString, "due-string", "", "Updated human-readable representation of the due date. See the [Due dates](#tag/Due-dates) section for more details.")
	cmd.Flags().StringVar(&bodyDuration, "duration", "", "Updated task duration, in either minutes or days. Only used if `duration_unit` is also provided.")
	cmd.Flags().StringVar(&bodyDurationUnit, "duration-unit", "", "Unit of time for duration. Must be provided to update the task duration. Pass null to clear the value.")
	cmd.Flags().BoolVar(&bodyIsCollapsed, "is-collapsed", false, "Updated collapsed state of the task for the current user. Omit this field to keep it unchanged.")
	cmd.Flags().StringVar(&bodyLabels, "labels", "", "Updated list of label names. Omit this field to keep it unchanged.")
	cmd.Flags().IntVar(&bodyPriority, "priority", 0, "Updated task priority (1-4, where 1 is highest). Omit this field to keep it unchanged.")
	cmd.Flags().BoolVar(&stdinBody, "stdin", false, "Read request body as JSON from stdin")

	return cmd
}
