
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func newTasksQuickAddCmd(flags *rootFlags) *cobra.Command {
	var bodyAutoReminder bool
	var bodyMeta bool
	var bodyNote string
	var bodyReminder string
	var bodyText string
	var stdinBody bool

	cmd := &cobra.Command{
		Use:         "quick-add",
		Short:       "Add a new task using Quick Add with natural language processing.",
		Example:     "  todoist-aum tasks quick-add --text example-value",
		Annotations: map[string]string{"pp:endpoint": "tasks.quick-add", "pp:method": "POST", "pp:path": "/api/v1/tasks/quick"},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bare invocation of a command with required input prints help
			// instead of pflag's terse "required flag not set" error. Optional-
			// only read commands fall through so a bare call still executes.
			if cmd.Flags().NFlag() == 0 && len(args) == 0 && !flags.dryRun {
				return cmd.Help()
			}
			if !stdinBody {
				if !cmd.Flags().Changed("text") && !flags.dryRun {
					return fmt.Errorf("required flag \"%s\" not set", "text")
				}
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}

			path := "/api/v1/tasks/quick"
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
				if cmd.Flags().Changed("auto-reminder") {
					body["auto_reminder"] = bodyAutoReminder
				}
				if cmd.Flags().Changed("meta") {
					body["meta"] = bodyMeta
				}
				if bodyNote != "" {
					body["note"] = bodyNote
				}
				if bodyReminder != "" {
					body["reminder"] = bodyReminder
				}
				if bodyText != "" {
					body["text"] = bodyText
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
	cmd.Flags().BoolVar(&bodyAutoReminder, "auto-reminder", false, "When enabled, the default reminder will be added to the new item if it has a due date with time set.")
	cmd.Flags().BoolVar(&bodyMeta, "meta", false, "When enabled, the response includes a `meta` object containing the detailed parsing results.")
	cmd.Flags().StringVar(&bodyNote, "note", "", "A note to attach to the task. The note content is added as a comment on the created task.")
	cmd.Flags().StringVar(&bodyReminder, "reminder", "", "A reminder date in natural language format, similar to due date parsing.")
	cmd.Flags().StringVar(&bodyText, "text", "", "The text of the task to parse using Quick Add syntax. See the endpoint description for supported syntax elements.")
	cmd.Flags().BoolVar(&stdinBody, "stdin", false, "Read request body as JSON from stdin")

	return cmd
}
