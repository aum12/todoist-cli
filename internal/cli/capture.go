// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source live

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aum12/todoist-cli/internal/cliutil"
	"github.com/aum12/todoist-cli/internal/store"
)

type captureResult struct {
	Task              json.RawMessage   `json:"task"`
	TaskID            string            `json:"task_id"`
	Reminders         []json.RawMessage `json:"reminders,omitempty"`
	LocationReminder  json.RawMessage   `json:"location_reminder,omitempty"`
	PartialFailures   []string          `json:"partial_failures,omitempty"`
	ResolvedProjectID string            `json:"resolved_project_id,omitempty"`
}

func newNovelCaptureCmd(flags *rootFlags) *cobra.Command {
	var (
		flagInto             string
		flagLabel            []string
		flagLabels           string
		flagDue              string
		flagDeadline         string
		flagReminder         []string
		flagReminderOffset   []string
		flagReminderUrgent   bool
		flagDescription      string
		flagPriority         string
		flagSection          string
		flagParent           string
		flagLocation         string
		flagLocationLat      string
		flagLocationLong     string
		flagLocationTrigger  string
		flagLocationRadius   int
		flagTz               string
		flagStdin            bool
	)

	cmd := &cobra.Command{
		Use:   "capture [content]",
		Short: "Voice/agent/routine-driven task entry with composed date+reminders+location.",
		Long: `Capture a Todoist task with optional date, deadline, multiple reminders, and a location
reminder, in one atomic call. Resolves --into project name from the local store. Labels
are passed by name (Todoist accepts label names directly). Use --stdin to batch-capture
one task per line with the same destination flags applied to every line.

The command POSTs to /api/v1/tasks first, then to /api/v1/reminders for each reminder
flag, then to /api/v1/location_reminders if --location-* is set. The agent-shaped JSON
response includes the new task id, each created reminder, and a partial_failures list
if any sub-call failed.

Use this command for voice/agent/routine-driven task entry where destination + date +
reminder + location are known by name. Do NOT use it for arbitrary task creation that
needs every spec field; use 'tasks create-task-api-v1-tasks' for that. For Inbox to
project triage of existing tasks use 'triage'.`,
		Example: strings.Trim(`
  # Voice capture from Apple Watch
  todoist-aum capture "buy carrots" --into Groceries --label walmart --agent

  # Stop-at-hardware-store with date, wrap-up reminder, location
  todoist-aum capture "stop by hardware store" --due "today 3:30pm" --reminder-offset 20m --location "Hardware Store" --label errand --label evening --agent

  # Recipe to grocery list (one ingredient per line on stdin)
  echo -e "carrots\ncelery\nonion" | todoist-aum capture --stdin --into Groceries --label walmart`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "false", "pp:typed-exit-codes": "0,2,4,5,6", "pp:no-error-path-probe": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			contents, err := captureCollectContents(args, flagStdin)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(err)
			}
			if len(contents) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("no task content provided (pass content as a positional arg or use --stdin)"))
			}

			priorityAPI, err := humanPriorityToAPI(flagPriority)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(err)
			}

			// Merge --label (repeatable) and --labels (comma-list)
			labels := append([]string{}, flagLabel...)
			if flagLabels != "" {
				for _, l := range strings.Split(flagLabels, ",") {
					if t := strings.TrimSpace(l); t != "" {
						labels = append(labels, t)
					}
				}
			}

			// Resolve --into project name if provided
			var projectID string
			var sectionID string
			if flagInto != "" {
				db, err := openLocalStore(flags)
				if err != nil {
					return err
				}
				pid, lerr := resolveProjectIDByName(cmd.Context(), db, flagInto)
				if lerr != nil {
					db.Close()
					return notFoundErr(lerr)
				}
				projectID = pid

				if flagSection != "" {
					sid, lerr := resolveSectionIDByName(cmd.Context(), db, projectID, flagSection)
					if lerr != nil {
						db.Close()
						return notFoundErr(lerr)
					}
					sectionID = sid
				}
				db.Close()
			}

			c, err := flags.newClient()
			if err != nil {
				return err
			}

			// Parse all reminder-offset values up-front so we fail fast
			offsets := make([]int, 0, len(flagReminderOffset))
			for _, ro := range flagReminderOffset {
				d, derr := cliutil.ParseDurationLoose(ro)
				if derr != nil {
					_ = cmd.Usage()
					return usageErr(fmt.Errorf("invalid --reminder-offset %q: %w", ro, derr))
				}
				offsets = append(offsets, int(d.Minutes()))
			}

			results := make([]captureResult, 0, len(contents))
			for _, content := range contents {
				body := map[string]any{"content": content}
				if flagDescription != "" {
					body["description"] = flagDescription
				}
				if projectID != "" {
					body["project_id"] = projectID
				}
				if sectionID != "" {
					body["section_id"] = sectionID
				}
				if flagParent != "" {
					body["parent_id"] = flagParent
				}
				if len(labels) > 0 {
					body["labels"] = labels
				}
				if priorityAPI > 0 {
					body["priority"] = priorityAPI
				}
				if flagDue != "" {
					body["due_string"] = flagDue
					if flagTz != "" {
						body["due_lang"] = flagTz
					}
				}
				if flagDeadline != "" {
					body["deadline_date"] = flagDeadline
				}

				data, status, perr := c.Post(cmd.Context(), "/api/v1/tasks", body)
				if perr != nil {
					return classifyAPIError(perr, flags)
				}
				if status < 200 || status >= 300 {
					return apiErr(fmt.Errorf("POST /api/v1/tasks returned status %d", status))
				}
				created := parsedCreatedTask(data)
				if created.ID == "" {
					return apiErr(fmt.Errorf("POST /api/v1/tasks succeeded but response had no id"))
				}

				result := captureResult{
					Task:              data,
					TaskID:            created.ID,
					ResolvedProjectID: projectID,
				}

				// Post relative-offset reminders
				for _, mins := range offsets {
					reminderBody := map[string]any{
						"task_id":       created.ID,
						"reminder_type": "relative",
						"minute_offset": mins,
					}
					if flagReminderUrgent {
						reminderBody["is_urgent"] = true
					}
					rdata, rstatus, rerr := c.Post(cmd.Context(), "/api/v1/reminders", reminderBody)
					if rerr != nil || rstatus < 200 || rstatus >= 300 {
						result.PartialFailures = append(result.PartialFailures,
							fmt.Sprintf("reminder offset=%dm failed: %v (status=%d)", mins, rerr, rstatus))
						continue
					}
					result.Reminders = append(result.Reminders, rdata)
				}

				// Post absolute-datetime reminders
				for _, iso := range flagReminder {
					ts, perr := parseFlexibleDatetime(iso, flagTz)
					if perr != nil {
						result.PartialFailures = append(result.PartialFailures,
							fmt.Sprintf("reminder %q invalid datetime: %v", iso, perr))
						continue
					}
					reminderBody := map[string]any{
						"task_id":       created.ID,
						"reminder_type": "absolute",
						"due": map[string]any{
							"date":     ts.Format("2006-01-02"),
							"datetime": ts.Format(time.RFC3339),
						},
					}
					if flagReminderUrgent {
						reminderBody["is_urgent"] = true
					}
					rdata, rstatus, rerr := c.Post(cmd.Context(), "/api/v1/reminders", reminderBody)
					if rerr != nil || rstatus < 200 || rstatus >= 300 {
						result.PartialFailures = append(result.PartialFailures,
							fmt.Sprintf("reminder %q failed: %v (status=%d)", iso, rerr, rstatus))
						continue
					}
					result.Reminders = append(result.Reminders, rdata)
				}

				// Post location reminder
				if flagLocation != "" || flagLocationLat != "" || flagLocationLong != "" {
					locBody := map[string]any{"task_id": created.ID}
					if flagLocation != "" {
						locBody["name"] = flagLocation
					}
					if flagLocationLat != "" {
						locBody["loc_lat"] = flagLocationLat
					}
					if flagLocationLong != "" {
						locBody["loc_long"] = flagLocationLong
					}
					if flagLocationTrigger != "" {
						locBody["loc_trigger"] = flagLocationTrigger
					} else {
						locBody["loc_trigger"] = "on_enter"
					}
					if flagLocationRadius > 0 {
						locBody["radius"] = flagLocationRadius
					}
					if flagLocationLat == "" || flagLocationLong == "" {
						fmt.Fprintf(cmd.ErrOrStderr(),
							"warning: location reminder created without coordinates; geofence triggering requires --location-lat and --location-long\n")
					}
					ldata, lstatus, lerr := c.Post(cmd.Context(), "/api/v1/location_reminders", locBody)
					if lerr != nil || lstatus < 200 || lstatus >= 300 {
						result.PartialFailures = append(result.PartialFailures,
							fmt.Sprintf("location reminder failed: %v (status=%d)", lerr, lstatus))
					} else {
						result.LocationReminder = ldata
					}
				}

				results = append(results, result)
			}

			// Output
			if flags.asJSON || flagStdin {
				return flags.printJSON(cmd, results)
			}
			for _, r := range results {
				fmt.Fprintf(cmd.OutOrStdout(), "created task %s\n", r.TaskID)
				if len(r.Reminders) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "  reminders: %d\n", len(r.Reminders))
				}
				if r.LocationReminder != nil {
					fmt.Fprintln(cmd.OutOrStdout(), "  location reminder: yes")
				}
				if len(r.PartialFailures) > 0 {
					for _, pf := range r.PartialFailures {
						fmt.Fprintf(cmd.ErrOrStderr(), "  partial failure: %s\n", pf)
					}
				}
			}
			for _, r := range results {
				if len(r.PartialFailures) > 0 && !flags.allowPartialFailure {
					return partialFailureErr(fmt.Errorf("%d capture(s) had partial failures; see stderr", len(results)))
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagInto, "into", "", "Project name (resolved against local store; case-insensitive prefix match); empty = Inbox")
	cmd.Flags().StringSliceVar(&flagLabel, "label", nil, "Label name; repeat for multiple labels (e.g. --label walmart --label errand)")
	cmd.Flags().StringVar(&flagLabels, "labels", "", "Comma-separated label list alias (--labels a,b,c equivalent to repeated --label)")
	cmd.Flags().StringVar(&flagDue, "due", "", "Todoist NL due string (e.g. \"today 3:30pm\", \"every monday\")")
	cmd.Flags().StringVar(&flagDeadline, "deadline", "", "Firm deadline date YYYY-MM-DD (Todoist deadlines are date-only)")
	cmd.Flags().StringSliceVar(&flagReminder, "reminder", nil, "Absolute reminder datetime (ISO 8601 or local-time string); repeatable")
	cmd.Flags().StringSliceVar(&flagReminderOffset, "reminder-offset", nil, "Reminder offset relative to --due (e.g. 20m, 1h, 30m); repeatable; positive = before due")
	cmd.Flags().BoolVar(&flagReminderUrgent, "reminder-urgent", false, "Mark created reminders as urgent (notification priority)")
	cmd.Flags().StringVar(&flagDescription, "description", "", "Task description (markdown body separate from title)")
	cmd.Flags().StringVar(&flagPriority, "priority", "", "Priority p1 (highest) through p4 (lowest); translated to Todoist's inverted API integer")
	cmd.Flags().StringVar(&flagSection, "section", "", "Section name within --into project (resolved from local store)")
	cmd.Flags().StringVar(&flagParent, "parent", "", "Parent task id for creating a subtask")
	cmd.Flags().StringVar(&flagLocation, "location", "", "Friendly location name (use with --location-lat/--location-long for geofence)")
	cmd.Flags().StringVar(&flagLocationLat, "location-lat", "", "Location latitude (required for geofence triggering)")
	cmd.Flags().StringVar(&flagLocationLong, "location-long", "", "Location longitude (required for geofence triggering)")
	cmd.Flags().StringVar(&flagLocationTrigger, "location-trigger", "", "Geofence trigger: on_enter (default) or on_leave")
	cmd.Flags().IntVar(&flagLocationRadius, "location-radius", 0, "Geofence radius in meters (0 = Todoist default)")
	cmd.Flags().StringVar(&flagTz, "tz", "", "IANA timezone for date parsing (e.g. America/New_York); defaults to user timezone")
	cmd.Flags().BoolVar(&flagStdin, "stdin", false, "Read one task content per line from stdin; same destination flags apply to each")
	return cmd
}

func captureCollectContents(args []string, fromStdin bool) ([]string, error) {
	if fromStdin {
		var out []string
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			out = append(out, line)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return out, nil
	}
	if len(args) == 0 {
		return nil, nil
	}
	return []string{strings.Join(args, " ")}, nil
}

func openLocalStore(flags *rootFlags) (*store.Store, error) {
	_ = flags
	path := defaultDBPath("todoist-aum")
	return store.OpenReadOnly(path)
}

func parseFlexibleDatetime(s, tz string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty datetime")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return applyTZ(t, tz), nil
	}
	if t, err := time.Parse("2006-01-02 15:04", s); err == nil {
		return applyTZ(t, tz), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return applyTZ(t, tz), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized datetime format %q (try RFC3339)", s)
}

func applyTZ(t time.Time, tz string) time.Time {
	if tz == "" {
		return t
	}
	if loc, err := time.LoadLocation(tz); err == nil {
		return t.In(loc)
	}
	return t
}
