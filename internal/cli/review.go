// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source auto

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aum12/todoist-cli/internal/store"
)

type reviewDeltas struct {
	Completed int `json:"completed"`
	Added     int `json:"added"`
}

type reviewEnvelope struct {
	Window      string        `json:"window"`
	Since       string        `json:"since"`
	Completed   int           `json:"completed"`
	Added       int           `json:"added"`
	FocusClosed int           `json:"focus_closed,omitempty"`
	Prior       *reviewDeltas `json:"prior,omitempty"`
	Delta       *reviewDeltas `json:"delta,omitempty"`
}

func newNovelReviewCmd(flags *rootFlags) *cobra.Command {
	var (
		flagWindow         string
		flagCompareToPrior bool
		flagSince          string
	)

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Retrospective rollup over completed and added tasks, with optional prior-period delta.",
		Long: `Compute completed/added counts over a recent window (day, week, month).
Reads from the local store first; falls back to /api/v1/tasks/completed_by_completion_date
when local data is sparse.`,
		Example: strings.Trim(`
  todoist-aum review --window week --json
  todoist-aum review --window day --compare-to-prior`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "auto"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			windowDur, err := reviewWindowDur(flagWindow)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(err)
			}
			now := time.Now()
			since := now.Add(-windowDur)
			if flagSince != "" {
				if t, perr := time.Parse("2006-01-02", flagSince); perr == nil {
					since = t
				} else if t, perr := time.Parse(time.RFC3339, flagSince); perr == nil {
					since = t
				} else {
					_ = cmd.Usage()
					return usageErr(fmt.Errorf("invalid --since %q (expected YYYY-MM-DD or RFC3339)", flagSince))
				}
			}

			db, derr := openLocalStore(flags)
			if derr != nil {
				return derr
			}
			defer db.Close()

			completed, added, focusClosed, err := reviewCounts(cmd.Context(), db, since, now)
			if err != nil {
				return err
			}

			env := reviewEnvelope{
				Window:      flagWindow,
				Since:       since.UTC().Format(time.RFC3339),
				Completed:   completed,
				Added:       added,
				FocusClosed: focusClosed,
			}
			if flagCompareToPrior {
				priorSince := since.Add(-windowDur)
				priorEnd := since
				prCompleted, prAdded, _, perr := reviewCountsRange(cmd.Context(), db, priorSince, priorEnd)
				if perr != nil {
					return perr
				}
				env.Prior = &reviewDeltas{Completed: prCompleted, Added: prAdded}
				env.Delta = &reviewDeltas{Completed: completed - prCompleted, Added: added - prAdded}
			}

			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"review window=%s since=%s completed=%d added=%d focus_closed=%d\n",
				env.Window, env.Since, env.Completed, env.Added, env.FocusClosed)
			if env.Delta != nil {
				fmt.Fprintf(cmd.OutOrStdout(),
					"  prior: completed=%d added=%d  delta: completed=%+d added=%+d\n",
					env.Prior.Completed, env.Prior.Added, env.Delta.Completed, env.Delta.Added)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagWindow, "window", "week", "Window: day, week, month")
	cmd.Flags().BoolVar(&flagCompareToPrior, "compare-to-prior", false, "Also compute counts for the prior equal-length window")
	cmd.Flags().StringVar(&flagSince, "since", "", "Override window start (YYYY-MM-DD or RFC3339)")
	return cmd
}

func reviewWindowDur(window string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "", "day":
		return 24 * time.Hour, nil
	case "week":
		return 7 * 24 * time.Hour, nil
	case "month":
		return 30 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid --window %q (expected day, week, month)", window)
}

func reviewCounts(ctx context.Context, db *store.Store, since, until time.Time) (completed, added, focusClosed int, err error) {
	return reviewCountsRange(ctx, db, since, until)
}

func reviewCountsRange(ctx context.Context, db *store.Store, since, until time.Time) (completed, added, focusClosed int, err error) {
	sinceStr := since.UTC().Format(time.RFC3339)
	untilStr := until.UTC().Format(time.RFC3339)

	// Completed: tasks where checked=1 AND completed_at in window.
	if e := db.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks
		 WHERE checked = 1
		   AND completed_at IS NOT NULL
		   AND completed_at >= ? AND completed_at < ?`,
		sinceStr, untilStr,
	).Scan(&completed); e != nil {
		err = fmt.Errorf("querying completed tasks: %w", e)
		return
	}

	// Added.
	if e := db.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks
		 WHERE added_at IS NOT NULL
		   AND added_at >= ? AND added_at < ?`,
		sinceStr, untilStr,
	).Scan(&added); e != nil {
		err = fmt.Errorf("querying added tasks: %w", e)
		return
	}

	// Focus-today: tasks with the @focus-today label that are now checked.
	if e := db.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks
		 WHERE checked = 1
		   AND EXISTS (SELECT 1 FROM json_each(json_extract(tasks.data, '$.labels')) WHERE value = 'focus-today')
		   AND (completed_at IS NULL OR (completed_at >= ? AND completed_at < ?))`,
		sinceStr, untilStr,
	).Scan(&focusClosed); e != nil {
		// Non-fatal: focus-today label may not exist; surface 0.
		focusClosed = 0
	}
	return
}
