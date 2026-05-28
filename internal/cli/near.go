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

type nearMatchedVia struct {
	Labels    int `json:"labels"`
	Projects  int `json:"projects"`
	Locations int `json:"locations"`
}

type nearEnvelope struct {
	Context    string         `json:"context"`
	Items      []taskRow      `json:"items"`
	Count      int            `json:"count"`
	MatchedVia nearMatchedVia `json:"matched_via"`
}

func newNovelNearCmd(flags *rootFlags) *cobra.Command {
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "near [context]",
		Short: "Surface open tasks tagged with a label, project, or location context.",
		Long: `Geofence-friendly task surfacing. Given a context name (e.g. "walmart"),
returns open tasks whose labels contain the context, whose project name matches
case-insensitive prefix, or whose location reminder name contains it. Sorted by
priority desc then age desc (older first).`,
		Example: strings.Trim(`
  todoist-aum near walmart
  todoist-aum near home --limit 10 --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local", "pp:no-error-path-probe": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			if len(args) == 0 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("context is required (positional arg, e.g. `near walmart`)"))
			}
			ctxName := strings.TrimSpace(strings.Join(args, " "))
			if ctxName == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("context cannot be empty"))
			}
			lcCtx := strings.ToLower(ctxName)

			db, err := openLocalStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()

			// 1. Label match via JSON exists.
			labelMatchSQL := `EXISTS (SELECT 1 FROM json_each(json_extract(tasks.data, '$.labels')) WHERE LOWER(value) = ?)`
			labelRows, err := scanOpenTasksWhere(cmd.Context(), db, labelMatchSQL, lcCtx)
			if err != nil {
				return err
			}

			// 2. Project name prefix match.
			projRows, err := scanOpenTasksWhere(cmd.Context(), db, `LOWER(json_extract(projects.data, '$.name')) LIKE ?`, lcCtx+"%")
			if err != nil {
				return err
			}

			// 3. Location reminder name match.
			locRows, err := scanOpenTasksWhere(cmd.Context(), db,
				`tasks.id IN (SELECT item_id FROM location_reminders WHERE LOWER(name) LIKE ? AND (is_deleted IS NULL OR is_deleted = 0))`,
				"%"+lcCtx+"%")
			if err != nil {
				return err
			}

			env := nearEnvelope{
				Context: ctxName,
				Items:   []taskRow{},
				MatchedVia: nearMatchedVia{
					Labels:    len(labelRows),
					Projects:  len(projRows),
					Locations: len(locRows),
				},
			}

			// Deduplicate by task id.
			seen := map[string]bool{}
			merge := func(rows []taskRow) {
				for _, r := range rows {
					if seen[r.ID] {
						continue
					}
					seen[r.ID] = true
					env.Items = append(env.Items, r)
				}
			}
			merge(labelRows)
			merge(projRows)
			merge(locRows)

			// Sort: priority desc, then age desc (older AddedAt first).
			sort.SliceStable(env.Items, func(i, j int) bool {
				if env.Items[i].Priority != env.Items[j].Priority {
					return env.Items[i].Priority > env.Items[j].Priority
				}
				ai := parseStoredISO(env.Items[i].AddedAt)
				aj := parseStoredISO(env.Items[j].AddedAt)
				if ai.IsZero() && aj.IsZero() {
					return false
				}
				if ai.IsZero() {
					return false
				}
				if aj.IsZero() {
					return true
				}
				return ai.Before(aj)
			})

			if flagLimit > 0 && len(env.Items) > flagLimit {
				env.Items = env.Items[:flagLimit]
			}
			env.Count = len(env.Items)

			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			if env.Count == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "no tasks match context %q\n", ctxName)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "context=%q matches=%d (labels=%d projects=%d locations=%d)\n",
				ctxName, env.Count, env.MatchedVia.Labels, env.MatchedVia.Projects, env.MatchedVia.Locations)
			for _, it := range env.Items {
				fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s  project=%s  id=%s\n",
					it.PriorityHuman, it.Content, it.ProjectName, it.ID)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&flagLimit, "limit", 20, "Maximum number of items to return")
	return cmd
}

// parseStoredISO attempts to parse a sqlite-stored ISO/RFC3339 timestamp.
// Returns zero time on failure.
func parseStoredISO(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
