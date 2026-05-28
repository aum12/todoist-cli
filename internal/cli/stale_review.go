// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aum12/todoist-cli/internal/cliutil"
	"github.com/aum12/todoist-cli/internal/store"
)

type staleEntry struct {
	TaskID           string `json:"task_id"`
	Content          string `json:"content"`
	AgeDays          int    `json:"age_days"`
	InactiveDays     int    `json:"inactive_days"`
	SuggestedAction  string `json:"suggested_action"`
	Rationale        string `json:"rationale"`
}

type staleEnvelope struct {
	Plan           []staleEntry   `json:"plan"`
	Count          int            `json:"count"`
	ActionsSummary map[string]int `json:"actions_summary"`
	Applied        int            `json:"applied,omitempty"`
	Failed         int            `json:"failed,omitempty"`
	Failures       []string       `json:"failures,omitempty"`
}

func newNovelStaleReviewCmd(flags *rootFlags) *cobra.Command {
	var (
		flagAge      string
		flagInactive string
		flagLimit    int
		flagApply    string
	)

	cmd := &cobra.Command{
		Use:   "stale-review",
		Short: "Surface stale tasks and suggest mechanical actions; optionally apply them.",
		Long: `Find open tasks older than --age that haven't moved in --inactive, annotate with
a suggested action (close-as-obsolete, move-to-someday, break-down,
reschedule:+7d, manual-review), and emit a plan. --apply executes the suggested
actions via the API.`,
		Example: strings.Trim(`
  todoist-aum stale-review --age 30d --inactive 14d --json > stale-plan.json
  todoist-aum stale-review --apply stale-plan.json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:typed-exit-codes": "0,2,5,6"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if flagApply != "" {
				return staleReviewApply(cmd, flags, flagApply)
			}
			ageDur, err := cliutil.ParseDurationLoose(flagAge)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid --age: %w", err))
			}
			inactiveDur, err := cliutil.ParseDurationLoose(flagInactive)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid --inactive: %w", err))
			}
			return staleReviewPlan(cmd, flags, ageDur, inactiveDur, flagLimit)
		},
	}
	cmd.Flags().StringVar(&flagAge, "age", "30d", "Minimum task age")
	cmd.Flags().StringVar(&flagInactive, "inactive", "14d", "Minimum inactivity duration since updated_at")
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "Maximum number of plan entries")
	cmd.Flags().StringVar(&flagApply, "apply", "", "Apply a previously-emitted plan file")
	return cmd
}

func staleReviewPlan(cmd *cobra.Command, flags *rootFlags, ageDur, inactiveDur time.Duration, limit int) error {
	db, err := openLocalStore(flags)
	if err != nil {
		return err
	}
	defer db.Close()

	now := time.Now()
	ageCutoff := now.Add(-ageDur)
	inactiveCutoff := now.Add(-inactiveDur)

	// Pull all open tasks; filter in Go because updated_at may be NULL.
	rows, err := scanOpenTasksWhere(cmd.Context(), db, "")
	if err != nil {
		return err
	}

	env := staleEnvelope{
		Plan:           []staleEntry{},
		ActionsSummary: map[string]int{},
	}

	for _, r := range rows {
		addedT := parseStoredISO(r.AddedAt)
		if addedT.IsZero() || addedT.After(ageCutoff) {
			continue
		}
		updatedT := parseStoredISO(r.UpdatedAt)
		// NULL or empty updated_at counts as ancient.
		if !updatedT.IsZero() && updatedT.After(inactiveCutoff) {
			continue
		}

		action, rationale := classifyStale(cmd.Context(), db, r, now)
		entry := staleEntry{
			TaskID:          r.ID,
			Content:         r.Content,
			AgeDays:         int(now.Sub(addedT) / (24 * time.Hour)),
			SuggestedAction: action,
			Rationale:       rationale,
		}
		if !updatedT.IsZero() {
			entry.InactiveDays = int(now.Sub(updatedT) / (24 * time.Hour))
		} else {
			entry.InactiveDays = entry.AgeDays
		}
		env.Plan = append(env.Plan, entry)
		env.ActionsSummary[action]++
		if limit > 0 && len(env.Plan) >= limit {
			break
		}
	}
	env.Count = len(env.Plan)

	if flags.asJSON {
		return flags.printJSON(cmd, env)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "stale-review: %d tasks\n", env.Count)
	for _, e := range env.Plan {
		fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s  age=%dd  inactive=%dd  (%s)\n",
			e.SuggestedAction, e.Content, e.AgeDays, e.InactiveDays, e.Rationale)
	}
	return nil
}

func classifyStale(ctx context.Context, db *store.Store, r taskRow, now time.Time) (string, string) {
	due := parseDueObjectTime(r.Due)
	hasDue := !due.IsZero()
	// Look for unfinished subtasks (other open tasks with parent_id = r.ID).
	hasOpenSubtasks := false
	var cnt int
	if err := db.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE parent_id = ? AND (checked IS NULL OR checked = 0) AND (is_deleted IS NULL OR is_deleted = 0)`,
		r.ID,
	).Scan(&cnt); err == nil && cnt > 0 {
		hasOpenSubtasks = true
	}
	// Has recent comments?
	recentComment := false
	if rows, err := db.DB().QueryContext(ctx,
		`SELECT posted_at FROM comments WHERE json_extract(data, '$.item_id') = ? ORDER BY posted_at DESC LIMIT 1`,
		r.ID,
	); err == nil {
		if rows.Next() {
			var pa sql.NullString
			if err := rows.Scan(&pa); err == nil && pa.Valid {
				if t, perr := time.Parse(time.RFC3339, pa.String); perr == nil {
					if now.Sub(t) <= 7*24*time.Hour {
						recentComment = true
					}
				}
			}
		}
		rows.Close()
	}

	switch {
	case !hasDue && (r.Priority == 1 || r.Priority == 2):
		return "close-as-obsolete", "no due date and low priority"
	case hasDue && due.Before(now.Add(-30*24*time.Hour)):
		return "move-to-someday", "overdue by more than 30 days"
	case hasOpenSubtasks:
		return "break-down", "has unfinished subtasks"
	case recentComment:
		return "reschedule:+7d", "recent comment activity"
	default:
		return "manual-review", "doesn't match any heuristic"
	}
}

func staleReviewApply(cmd *cobra.Command, flags *rootFlags, planPath string) error {
	rawEntries, err := readPlanEntries(planPath)
	if err != nil {
		return err
	}
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	env := staleEnvelope{ActionsSummary: map[string]int{}}
	for _, raw := range rawEntries {
		var e staleEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			env.Failed++
			env.Failures = append(env.Failures, fmt.Sprintf("parse: %v", err))
			continue
		}
		if e.TaskID == "" || e.SuggestedAction == "" {
			continue
		}
		var status int
		var apiErrLocal error
		switch {
		case e.SuggestedAction == "close-as-obsolete":
			_, status, apiErrLocal = c.Post(cmd.Context(), "/api/v1/tasks/"+e.TaskID+"/close", map[string]any{})
		case e.SuggestedAction == "move-to-someday":
			// Heuristic: emit a label-update request marking it 'someday';
			// the API has no Someday concept, so we relabel.
			body := map[string]any{"labels": []string{"someday"}}
			_, status, apiErrLocal = c.Post(cmd.Context(), "/api/v1/tasks/"+e.TaskID, body)
		case strings.HasPrefix(e.SuggestedAction, "reschedule:"):
			shift := strings.TrimPrefix(e.SuggestedAction, "reschedule:")
			d, derr := cliutil.ParseDurationLoose(shift)
			if derr != nil {
				env.Failed++
				env.Failures = append(env.Failures, fmt.Sprintf("task %s: invalid shift %q: %v", e.TaskID, shift, derr))
				continue
			}
			newDate := time.Now().Add(d).Format("2006-01-02")
			body := map[string]any{"due_date": newDate}
			_, status, apiErrLocal = c.Post(cmd.Context(), "/api/v1/tasks/"+e.TaskID, body)
		default:
			// manual-review and break-down: no automated action.
			env.ActionsSummary[e.SuggestedAction]++
			continue
		}
		if apiErrLocal != nil || status < 200 || status >= 300 {
			env.Failed++
			env.Failures = append(env.Failures, fmt.Sprintf("task %s: status=%d err=%v", e.TaskID, status, apiErrLocal))
			continue
		}
		env.Applied++
		env.ActionsSummary[e.SuggestedAction]++
	}
	if flags.asJSON {
		_ = flags.printJSON(cmd, env)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "applied=%d failed=%d\n", env.Applied, env.Failed)
	}
	if env.Failed > 0 && !flags.allowPartialFailure {
		return partialFailureErr(fmt.Errorf("%d stale-review apply(s) failed", env.Failed))
	}
	return nil
}
