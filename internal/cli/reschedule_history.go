// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source live

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aum12/todoist-cli/internal/cliutil"
)

type rescheduleMove struct {
	On        string `json:"on"`
	From      string `json:"from"`
	To        string `json:"to"`
	DeltaDays int    `json:"delta_days"`
}

type rescheduleHistoryTask struct {
	TaskID    string           `json:"task_id"`
	Content   string           `json:"content,omitempty"`
	Moves     []rescheduleMove `json:"moves"`
	MoveCount int              `json:"move_count"`
}

type rescheduleHistoryEnvelope struct {
	Since   string                  `json:"since"`
	Tasks   []rescheduleHistoryTask `json:"tasks"`
	Message string                  `json:"message,omitempty"`
}

func newNovelRescheduleHistoryCmd(flags *rootFlags) *cobra.Command {
	var (
		flagTaskID string
		flagFilter string
		flagSince  string
		flagLimit  int
	)

	cmd := &cobra.Command{
		Use:   "reschedule-history",
		Short: "Walk Todoist activity events to show every due-date move per task.",
		Long: `Reads /api/v1/activities (premium-gated) for item_updated events, extracts
last_due_date transitions, and groups by task. Surfaces a clean message when
the endpoint is unavailable.`,
		Example: strings.Trim(`
  todoist-aum reschedule-history --task-id 1234 --since 30d
  todoist-aum reschedule-history --filter "p1" --since 7d --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if flagTaskID == "" && flagFilter == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("one of --task-id or --filter is required"))
			}
			sinceDur, err := cliutil.ParseDurationLoose(flagSince)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid --since %q: %w", flagSince, err))
			}
			since := time.Now().Add(-sinceDur)

			c, err := flags.newClient()
			if err != nil {
				return err
			}

			params := map[string]string{
				"object_type": "item",
				"event_type":  "updated",
			}
			if flagTaskID != "" {
				params["object_id"] = flagTaskID
			}

			data, err := c.Get(cmd.Context(), "/api/v1/activities", params)
			env := rescheduleHistoryEnvelope{
				Since: flagSince,
				Tasks: []rescheduleHistoryTask{},
			}
			if err != nil {
				msg := err.Error()
				if strings.Contains(msg, "HTTP 403") || strings.Contains(msg, "HTTP 402") {
					env.Message = "activity log requires Todoist premium / business tier"
					if flags.asJSON {
						return flags.printJSON(cmd, env)
					}
					fmt.Fprintln(cmd.OutOrStdout(), env.Message)
					return nil
				}
				return classifyAPIError(err, flags)
			}

			byTask := map[string]*rescheduleHistoryTask{}
			events := extractActivityEvents(data)
			for _, ev := range events {
				eventDate, _ := ev["event_date"].(string)
				if eventDate != "" {
					if t, err := time.Parse(time.RFC3339, eventDate); err == nil && t.Before(since) {
						continue
					}
				}
				objID, _ := ev["object_id"].(string)
				if objID == "" {
					if n, ok := ev["object_id"].(float64); ok {
						objID = fmt.Sprintf("%.0f", n)
					}
				}
				if objID == "" {
					continue
				}
				extra, _ := ev["extra_data"].(map[string]any)
				if extra == nil {
					continue
				}
				lastDue, _ := extra["last_due_date"].(string)
				newDue, _ := extra["due_date"].(string)
				if lastDue == "" || newDue == "" {
					continue
				}
				move := rescheduleMove{On: eventDate, From: lastDue, To: newDue}
				if td, terr := parseFlexibleDate(lastDue); terr == nil {
					if tn, terr2 := parseFlexibleDate(newDue); terr2 == nil {
						move.DeltaDays = int(tn.Sub(td) / (24 * time.Hour))
					}
				}
				w, ok := byTask[objID]
				if !ok {
					content, _ := ev["object_content"].(string)
					w = &rescheduleHistoryTask{TaskID: objID, Content: content}
					byTask[objID] = w
				}
				w.Moves = append(w.Moves, move)
				w.MoveCount++
			}

			for _, t := range byTask {
				env.Tasks = append(env.Tasks, *t)
				if flagLimit > 0 && len(env.Tasks) >= flagLimit {
					break
				}
			}

			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			if len(env.Tasks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no reschedule events in window")
				return nil
			}
			for _, t := range env.Tasks {
				fmt.Fprintf(cmd.OutOrStdout(), "task %s (%d moves)\n", t.TaskID, t.MoveCount)
				for _, m := range t.Moves {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s -> %s  (%+d days)\n", m.On, m.From, m.To, m.DeltaDays)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagTaskID, "task-id", "", "Restrict to a single task id")
	cmd.Flags().StringVar(&flagFilter, "filter", "", "Todoist filter query (alternative to --task-id)")
	cmd.Flags().StringVar(&flagSince, "since", "30d", "Window (e.g. 7d, 30d)")
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "Maximum number of tasks to return")
	return cmd
}

// extractActivityEvents unwraps the various shapes /api/v1/activities can
// return into a flat list of event objects.
func extractActivityEvents(data json.RawMessage) []map[string]any {
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	for _, k := range []string{"events", "results", "data", "items"} {
		if raw, ok := obj[k]; ok {
			var inner []map[string]any
			if err := json.Unmarshal(raw, &inner); err == nil {
				return inner
			}
		}
	}
	return nil
}

func parseFlexibleDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("cannot parse %q", s)
}
