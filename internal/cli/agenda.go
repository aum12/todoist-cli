// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source local

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type agendaEnvelope struct {
	Window         string    `json:"window"`
	AsOf           string    `json:"as_of"`
	Items          []taskRow `json:"items"`
	OverdueCount   int       `json:"overdue_count"`
	DueTodayCount  int       `json:"due_today_count"`
	UpcomingCount  int       `json:"upcoming_count"`
}

func newNovelAgendaCmd(flags *rootFlags) *cobra.Command {
	var (
		flagWindow         string
		flagIncludeOverdue bool
		flagLimit          int
	)

	cmd := &cobra.Command{
		Use:   "agenda",
		Short: "Today's tasks plus overdue tail plus a windowed lookahead from the local store.",
		Long: `Single-call daily plan composed from the local SQLite store: today's tasks, the
overdue tail (open), and a configurable lookahead window. Sorted by priority desc,
then due asc. No live API calls — run 'sync --resources tasks,projects --full'
first if the store is stale.`,
		Example: strings.Trim(`
  todoist-aum agenda --window today
  todoist-aum agenda --window 3d --include-overdue
  todoist-aum agenda --window 7d --limit 50 --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				// Bare invocation is still a valid daily plan; fall through.
			}
			if dryRunOK(flags) {
				return nil
			}

			windowEnd, err := agendaWindowEnd(flagWindow)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(err)
			}

			db, err := openLocalStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()

			rows, err := scanOpenTasksWhere(cmd.Context(), db, "")
			if err != nil {
				return err
			}

			now := time.Now()
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			tomorrow := today.Add(24 * time.Hour)

			env := agendaEnvelope{
				Window: flagWindow,
				AsOf:   now.UTC().Format(time.RFC3339),
				Items:  []taskRow{},
			}

			for _, r := range rows {
				dueT := parseDueObjectTime(r.Due)
				if dueT.IsZero() {
					// No due date; only include if window includes "all" — agenda focuses on dated work.
					continue
				}
				inPast := dueT.Before(today)
				inWindow := !dueT.Before(today) && dueT.Before(windowEnd)
				if inPast {
					if !flagIncludeOverdue {
						continue
					}
					env.OverdueCount++
				} else if inWindow {
					if !dueT.Before(today) && dueT.Before(tomorrow) {
						env.DueTodayCount++
					} else {
						env.UpcomingCount++
					}
				} else {
					continue
				}
				env.Items = append(env.Items, r)
			}

			// Sort: priority desc (API priority high=4), then due asc.
			sort.SliceStable(env.Items, func(i, j int) bool {
				if env.Items[i].Priority != env.Items[j].Priority {
					return env.Items[i].Priority > env.Items[j].Priority
				}
				return parseDueObjectTime(env.Items[i].Due).Before(parseDueObjectTime(env.Items[j].Due))
			})

			if flagLimit > 0 && len(env.Items) > flagLimit {
				env.Items = env.Items[:flagLimit]
			}

			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			if len(env.Items) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no agenda items")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"agenda window=%s overdue=%d due_today=%d upcoming=%d\n",
				env.Window, env.OverdueCount, env.DueTodayCount, env.UpcomingCount)
			for _, it := range env.Items {
				due := parseDueObjectTime(it.Due)
				dueStr := ""
				if !due.IsZero() {
					dueStr = due.Format("2006-01-02")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s  due=%s  project=%s  id=%s\n",
					it.PriorityHuman, it.Content, dueStr, it.ProjectName, it.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagWindow, "window", "today", "Lookahead window: today, tomorrow, 3d, 7d")
	cmd.Flags().BoolVar(&flagIncludeOverdue, "include-overdue", true, "Include overdue open tasks in the result")
	cmd.Flags().IntVar(&flagLimit, "limit", 100, "Maximum number of items to return")
	return cmd
}

// agendaWindowEnd resolves window keyword into the end-of-window timestamp
// (exclusive). Returns an error for unknown keywords.
func agendaWindowEnd(window string) (time.Time, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "", "today":
		return today.Add(24 * time.Hour), nil
	case "tomorrow":
		return today.Add(48 * time.Hour), nil
	case "3d":
		return today.Add(3 * 24 * time.Hour), nil
	case "7d":
		return today.Add(7 * 24 * time.Hour), nil
	}
	return time.Time{}, fmt.Errorf("invalid --window %q (expected today, tomorrow, 3d, 7d)", window)
}
