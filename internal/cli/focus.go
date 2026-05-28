// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source auto

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// focusLabel is the constant label name the focus loop manages.
const focusLabel = "focus-today"

type focusItem struct {
	ID            string         `json:"id"`
	Content       string         `json:"content"`
	Priority      int            `json:"priority"`
	PriorityHuman string         `json:"priority_human"`
	Due           map[string]any `json:"due,omitempty"`
	Deadline      map[string]any `json:"deadline,omitempty"`
	ProjectID     string         `json:"project_id,omitempty"`
	ProjectName   string         `json:"project_name,omitempty"`
	Labels        []string       `json:"labels,omitempty"`
}

type focusEnvelope struct {
	Action string      `json:"action"`
	Reason string      `json:"reason,omitempty"`
	Items  []focusItem `json:"items"`
	Count  int         `json:"count"`
	Notes  string      `json:"notes,omitempty"`
}

func newNovelFocusCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "focus",
		Short: "Structured daily-goal setter; manages the @focus-today label.",
		Long: `Set, show, or clear the day's top-N focus items by tagging them with the
@focus-today label. The agent calls 'focus set' in the morning to commit to a
small daily plan and 'focus show' (or 'review --window day') in the evening to
check adherence.

Subcommands:
  focus set --top N [--reason "..."]   Pick the highest-priority/deadline-tightest
                                       open tasks (limit N) and add @focus-today.
  focus show                           Return tasks tagged with @focus-today.
  focus clear                          Remove the @focus-today label everywhere.`,
		Annotations: map[string]string{"mcp:read-only": "true"},
	}
	cmd.AddCommand(newFocusSetCmd(flags))
	cmd.AddCommand(newFocusShowCmd(flags))
	cmd.AddCommand(newFocusClearCmd(flags))
	return cmd
}

func newFocusSetCmd(flags *rootFlags) *cobra.Command {
	var topN int
	var reason string
	cmd := &cobra.Command{
		Use:     "set",
		Short:   "Tag the top-N highest-priority open tasks with @focus-today.",
		Example: `  todoist-aum focus set --top 3 --reason "meeting prep"`,
		Annotations: map[string]string{"mcp:read-only": "false", "pp:typed-exit-codes": "0,2,5,6"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().NFlag() == 0 && len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if topN <= 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--top must be a positive integer"))
			}

			db, err := openLocalStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()

			tasks, err := scanOpenTasksWhere(cmd.Context(), db, "1=1")
			if err != nil {
				return apiErr(err)
			}
			ranked := rankFocusCandidates(tasks, topN)

			c, err := flags.newClient()
			if err != nil {
				return err
			}
			updated := []focusItem{}
			partial := []string{}
			for _, t := range ranked {
				newLabels := append([]string{}, t.Labels...)
				if !containsString(newLabels, focusLabel) {
					newLabels = append(newLabels, focusLabel)
				}
				body := map[string]any{"labels": newLabels}
				_, status, perr := c.Post(cmd.Context(), "/api/v1/tasks/"+t.ID, body)
				if perr != nil || status < 200 || status >= 300 {
					partial = append(partial, fmt.Sprintf("task %s: %v (status=%d)", t.ID, perr, status))
					continue
				}
				updated = append(updated, taskRowToFocusItem(t))
			}

			env := focusEnvelope{Action: "set", Reason: reason, Items: updated, Count: len(updated)}
			if len(partial) > 0 {
				env.Notes = fmt.Sprintf("%d of %d updates failed", len(partial), len(ranked))
			}
			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "set %d focus items\n", env.Count)
			if reason != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "reason: %s\n", reason)
			}
			for _, it := range env.Items {
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s\n", it.PriorityHuman, it.Content)
			}
			for _, pf := range partial {
				fmt.Fprintf(cmd.ErrOrStderr(), "  partial failure: %s\n", pf)
			}
			if len(partial) > 0 && !flags.allowPartialFailure {
				return partialFailureErr(fmt.Errorf("%d focus assignment(s) failed; see stderr", len(partial)))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&topN, "top", 0, "Number of focus items to set (required, positive integer)")
	cmd.Flags().StringVar(&reason, "reason", "", "Optional rationale recorded in the JSON output")
	return cmd
}

func newFocusShowCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "show",
		Short:       "Show tasks tagged with @focus-today.",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			db, err := openLocalStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()
			tasks, err := scanOpenTasksWhere(cmd.Context(), db,
				`EXISTS (SELECT 1 FROM json_each(json_extract(tasks.data, '$.labels')) WHERE value = ?)`,
				focusLabel)
			if err != nil {
				return apiErr(err)
			}
			items := make([]focusItem, 0, len(tasks))
			for _, t := range tasks {
				items = append(items, taskRowToFocusItem(t))
			}
			env := focusEnvelope{Action: "show", Items: items, Count: len(items)}
			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d focus items\n", env.Count)
			for _, it := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s\n", it.PriorityHuman, it.Content)
			}
			return nil
		},
	}
	return cmd
}

func newFocusClearCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "clear",
		Short:       "Remove the @focus-today label from every task.",
		Annotations: map[string]string{"mcp:read-only": "false", "pp:typed-exit-codes": "0,5,6"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			db, err := openLocalStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()
			tasks, err := scanOpenTasksWhere(cmd.Context(), db,
				`EXISTS (SELECT 1 FROM json_each(json_extract(tasks.data, '$.labels')) WHERE value = ?)`,
				focusLabel)
			if err != nil {
				return apiErr(err)
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			cleared := 0
			partial := []string{}
			for _, t := range tasks {
				newLabels := make([]string, 0, len(t.Labels))
				for _, l := range t.Labels {
					if l != focusLabel {
						newLabels = append(newLabels, l)
					}
				}
				body := map[string]any{"labels": newLabels}
				_, status, perr := c.Post(cmd.Context(), "/api/v1/tasks/"+t.ID, body)
				if perr != nil || status < 200 || status >= 300 {
					partial = append(partial, fmt.Sprintf("task %s: %v (status=%d)", t.ID, perr, status))
					continue
				}
				cleared++
			}
			env := focusEnvelope{Action: "clear", Count: cleared}
			if len(partial) > 0 {
				env.Notes = strings.Join(partial, "; ")
			}
			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "cleared @focus-today from %d task(s)\n", cleared)
			for _, pf := range partial {
				fmt.Fprintf(cmd.ErrOrStderr(), "  partial failure: %s\n", pf)
			}
			if len(partial) > 0 && !flags.allowPartialFailure {
				return partialFailureErr(fmt.Errorf("%d clear(s) failed; see stderr", len(partial)))
			}
			return nil
		},
	}
	return cmd
}

func rankFocusCandidates(tasks []taskRow, topN int) []taskRow {
	type scored struct {
		row    taskRow
		dueAt  time.Time
		hasDue bool
	}
	scoredAll := make([]scored, 0, len(tasks))
	for _, t := range tasks {
		due := parseDueObjectTime(t.Due)
		scoredAll = append(scoredAll, scored{row: t, dueAt: due, hasDue: !due.IsZero()})
	}
	sortStable(scoredAll, func(a, b scored) bool {
		if a.row.Priority != b.row.Priority {
			return a.row.Priority > b.row.Priority
		}
		if a.hasDue && b.hasDue {
			return a.dueAt.Before(b.dueAt)
		}
		if a.hasDue != b.hasDue {
			return a.hasDue
		}
		return a.row.AddedAt < b.row.AddedAt
	})
	if topN > len(scoredAll) {
		topN = len(scoredAll)
	}
	out := make([]taskRow, 0, topN)
	for i := 0; i < topN; i++ {
		out = append(out, scoredAll[i].row)
	}
	return out
}

func taskRowToFocusItem(t taskRow) focusItem {
	return focusItem{
		ID:            t.ID,
		Content:       t.Content,
		Priority:      t.Priority,
		PriorityHuman: t.PriorityHuman,
		Due:           t.Due,
		Deadline:      t.Deadline,
		ProjectID:     t.ProjectID,
		ProjectName:   t.ProjectName,
		Labels:        t.Labels,
	}
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func sortStable[T any](slice []T, less func(a, b T) bool) {
	for i := 1; i < len(slice); i++ {
		for j := i; j > 0 && less(slice[j], slice[j-1]); j-- {
			slice[j], slice[j-1] = slice[j-1], slice[j]
		}
	}
}
