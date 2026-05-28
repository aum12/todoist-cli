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

type rescheduleCascadeEntry struct {
	TaskID            string `json:"task_id"`
	Content           string `json:"content"`
	BeforeDue         string `json:"before_due"`
	AfterDue          string `json:"after_due"`
	DeadlineViolation bool   `json:"deadline_violation"`
}

type rescheduleCascadeEnvelope struct {
	Plan           []rescheduleCascadeEntry `json:"plan"`
	Shift          string                   `json:"shift"`
	ViolationCount int                      `json:"violation_count"`
	Applied        int                      `json:"applied"`
	Failed         int                      `json:"failed"`
	Failures       []string                 `json:"failures,omitempty"`
}

func newNovelRescheduleCascadeCmd(flags *rootFlags) *cobra.Command {
	var (
		flagFilter            string
		flagShift             string
		flagRespectDeadlines  bool
		flagApply             string
	)

	cmd := &cobra.Command{
		Use:   "reschedule-cascade",
		Short: "Preview and apply a relative date shift over filter-matched tasks.",
		Long: `Compute new due dates for every task matching a Todoist filter, shifted by
--shift (e.g. +2d, +1w, -3h). In default plan mode emits a JSONL plan and
deadline-violation count; use --apply <plan-file> to commit via the API.`,
		Example: strings.Trim(`
  todoist-aum reschedule-cascade --filter "overdue" --shift +1d --json > plan.json
  todoist-aum reschedule-cascade --apply plan.json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "false", "pp:typed-exit-codes": "0,2,5,6"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagApply == "" && cmd.Flags().NFlag() == 0 && len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}

			if flagApply != "" {
				return rescheduleCascadeApply(cmd, flags, flagApply)
			}
			if flagFilter == "" || flagShift == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--filter and --shift are required when not using --apply"))
			}
			shift, err := cliutil.ParseDurationLoose(flagShift)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid --shift %q: %w", flagShift, err))
			}
			return rescheduleCascadePlan(cmd, flags, flagFilter, flagShift, shift, flagRespectDeadlines)
		},
	}
	cmd.Flags().StringVar(&flagFilter, "filter", "", "Todoist filter query (e.g. \"overdue & p2\")")
	cmd.Flags().StringVar(&flagShift, "shift", "", "Relative shift: e.g. +2d, +1w, -3h, 90m")
	cmd.Flags().BoolVar(&flagRespectDeadlines, "respect-deadlines", true, "Mark entries whose shifted due-date exceeds the deadline")
	cmd.Flags().StringVar(&flagApply, "apply", "", "Apply a previously-emitted plan file")
	return cmd
}

func rescheduleCascadePlan(cmd *cobra.Command, flags *rootFlags, filter, shiftStr string, shift time.Duration, respectDeadlines bool) error {
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	data, err := c.Get(cmd.Context(), "/api/v1/tasks/filter", map[string]string{"query": filter})
	if err != nil {
		return classifyAPIError(err, flags)
	}
	tasks := extractFilterTasks(data)

	env := rescheduleCascadeEnvelope{
		Plan:  []rescheduleCascadeEntry{},
		Shift: shiftStr,
	}
	for _, t := range tasks {
		id, _ := t["id"].(string)
		if id == "" {
			continue
		}
		content, _ := t["content"].(string)
		due, _ := t["due"].(map[string]any)
		before, after, _ := shiftDue(due, shift)
		entry := rescheduleCascadeEntry{
			TaskID:    id,
			Content:   content,
			BeforeDue: before,
			AfterDue:  after,
		}
		if respectDeadlines {
			if deadline, ok := t["deadline"].(map[string]any); ok {
				if dd, ok := deadline["date"].(string); ok && dd != "" && after > dd {
					entry.DeadlineViolation = true
					env.ViolationCount++
				}
			}
		}
		env.Plan = append(env.Plan, entry)
	}
	if flags.asJSON {
		return flags.printJSON(cmd, env)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "filter=%q shift=%s tasks=%d violations=%d\n",
		filter, shiftStr, len(env.Plan), env.ViolationCount)
	for _, p := range env.Plan {
		violation := ""
		if p.DeadlineViolation {
			violation = " [DEADLINE VIOLATION]"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s -> %s%s\n", p.Content, p.BeforeDue, p.AfterDue, violation)
	}
	return nil
}

func rescheduleCascadeApply(cmd *cobra.Command, flags *rootFlags, planPath string) error {
	rawEntries, err := readPlanEntries(planPath)
	if err != nil {
		return err
	}
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	env := rescheduleCascadeEnvelope{}
	for _, raw := range rawEntries {
		var e rescheduleCascadeEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			env.Failed++
			env.Failures = append(env.Failures, fmt.Sprintf("parse: %v", err))
			continue
		}
		if e.TaskID == "" || e.AfterDue == "" {
			continue
		}
		body := map[string]any{}
		// If it looks like a datetime (has 'T'), use due_datetime; otherwise due_date.
		if strings.Contains(e.AfterDue, "T") {
			body["due_datetime"] = e.AfterDue
		} else {
			body["due_date"] = e.AfterDue
		}
		_, status, apiErrLocal := c.Post(cmd.Context(), "/api/v1/tasks/"+e.TaskID, body)
		if apiErrLocal != nil || status < 200 || status >= 300 {
			env.Failed++
			env.Failures = append(env.Failures, fmt.Sprintf("task %s: status=%d err=%v", e.TaskID, status, apiErrLocal))
			continue
		}
		env.Applied++
	}
	if flags.asJSON {
		_ = flags.printJSON(cmd, env)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "applied=%d failed=%d\n", env.Applied, env.Failed)
		for _, f := range env.Failures {
			fmt.Fprintf(cmd.ErrOrStderr(), "  failure: %s\n", f)
		}
	}
	if env.Failed > 0 && !flags.allowPartialFailure {
		return partialFailureErr(fmt.Errorf("%d reschedule-cascade apply(s) failed", env.Failed))
	}
	return nil
}

// shiftDue returns (beforeString, afterString, hasDatetime) for a Todoist due
// object shifted by shift. When the due has a datetime field, both are emitted
// in RFC3339; when only a date, both are emitted in 2006-01-02.
func shiftDue(due map[string]any, shift time.Duration) (string, string, bool) {
	if due == nil {
		return "", "", false
	}
	if dt, ok := due["datetime"].(string); ok && dt != "" {
		if t, err := time.Parse(time.RFC3339, dt); err == nil {
			return dt, t.Add(shift).UTC().Format(time.RFC3339), true
		}
	}
	if d, ok := due["date"].(string); ok && d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			// Round shift to whole days for date-only.
			days := int(shift / (24 * time.Hour))
			if shift%(24*time.Hour) != 0 {
				if shift > 0 {
					days++
				} else {
					days--
				}
			}
			return d, t.AddDate(0, 0, days).Format("2006-01-02"), false
		}
	}
	return "", "", false
}
