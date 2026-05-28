// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source local

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aum12/todoist-cli/internal/cliutil"
)

type productivityBucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type productivityTrendEnvelope struct {
	GroupBy string               `json:"group_by"`
	Since   string               `json:"since"`
	Buckets []productivityBucket `json:"buckets"`
	Total   int                  `json:"total"`
}

func newNovelProductivityTrendCmd(flags *rootFlags) *cobra.Command {
	var (
		flagGroupBy string
		flagSince   string
		flagLimit   int
	)

	cmd := &cobra.Command{
		Use:   "productivity-trend",
		Short: "Rollup completed tasks by day, week, month, project, label, or hour-of-day.",
		Long: `Local-store rollup over completed (checked) tasks within --since. Group by:
  day, week, month   — calendar bucket of completed_at
  project            — joined projects.name
  label              — explode the labels array
  hour-of-day        — 00..23 bucket of completed_at`,
		Example: strings.Trim(`
  todoist-aum productivity-trend --group-by label --since 30d
  todoist-aum productivity-trend --group-by hour-of-day --since 7d --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			switch flagGroupBy {
			case "day", "week", "month", "project", "label", "hour-of-day":
				// ok
			case "":
				flagGroupBy = "day"
			default:
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid --group-by %q", flagGroupBy))
			}
			windowDur, err := cliutil.ParseDurationLoose(flagSince)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid --since %q: %w", flagSince, err))
			}
			since := time.Now().Add(-windowDur)
			sinceStr := since.UTC().Format(time.RFC3339)

			db, err := openLocalStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()

			env := productivityTrendEnvelope{
				GroupBy: flagGroupBy,
				Since:   flagSince,
				Buckets: []productivityBucket{},
			}

			var q string
			args2 := []any{sinceStr}
			switch flagGroupBy {
			case "day":
				q = `SELECT substr(completed_at, 1, 10) AS bucket, COUNT(*) FROM tasks
				     WHERE checked = 1 AND completed_at IS NOT NULL AND completed_at >= ?
				     GROUP BY bucket ORDER BY bucket DESC`
			case "week":
				q = `SELECT strftime('%Y-W%W', completed_at) AS bucket, COUNT(*) FROM tasks
				     WHERE checked = 1 AND completed_at IS NOT NULL AND completed_at >= ?
				     GROUP BY bucket ORDER BY bucket DESC`
			case "month":
				q = `SELECT substr(completed_at, 1, 7) AS bucket, COUNT(*) FROM tasks
				     WHERE checked = 1 AND completed_at IS NOT NULL AND completed_at >= ?
				     GROUP BY bucket ORDER BY bucket DESC`
			case "project":
				q = `SELECT COALESCE(json_extract(projects.data, '$.name'), tasks.project_id, '') AS bucket, COUNT(*) FROM tasks
				     LEFT JOIN resources AS projects
				       ON projects.id = tasks.project_id AND projects.resource_type = 'projects'
				     WHERE tasks.checked = 1 AND tasks.completed_at IS NOT NULL AND tasks.completed_at >= ?
				     GROUP BY bucket ORDER BY COUNT(*) DESC`
			case "label":
				q = `SELECT value AS bucket, COUNT(*) FROM tasks,
				     json_each(json_extract(tasks.data, '$.labels'))
				     WHERE tasks.checked = 1 AND tasks.completed_at IS NOT NULL AND tasks.completed_at >= ?
				     GROUP BY bucket ORDER BY COUNT(*) DESC`
			case "hour-of-day":
				q = `SELECT strftime('%H', completed_at) AS bucket, COUNT(*) FROM tasks
				     WHERE checked = 1 AND completed_at IS NOT NULL AND completed_at >= ?
				     GROUP BY bucket ORDER BY bucket ASC`
			}

			rows, err := db.DB().QueryContext(cmd.Context(), q, args2...)
			if err != nil {
				return fmt.Errorf("querying productivity rollup: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var key sql.NullString
				var count int
				if err := rows.Scan(&key, &count); err != nil {
					continue
				}
				k := ""
				if key.Valid {
					k = key.String
				}
				env.Buckets = append(env.Buckets, productivityBucket{Key: k, Count: count})
				env.Total += count
				if flagLimit > 0 && len(env.Buckets) >= flagLimit {
					break
				}
			}
			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "productivity-trend group_by=%s since=%s total=%d\n",
				env.GroupBy, env.Since, env.Total)
			for _, b := range env.Buckets {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\t%d\n", b.Key, b.Count)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagGroupBy, "group-by", "day", "Group by: day, week, month, project, label, hour-of-day")
	cmd.Flags().StringVar(&flagSince, "since", "30d", "Window start (e.g. 7d, 30d, 12h)")
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "Maximum number of buckets to return")
	return cmd
}
