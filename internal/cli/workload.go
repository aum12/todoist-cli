// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source local

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aum12/todoist-cli/internal/cliutil"
	"github.com/aum12/todoist-cli/internal/store"
)

type workloadCollab struct {
	UID         string         `json:"uid"`
	Name        string         `json:"name,omitempty"`
	OpenTotal   int            `json:"open_total"`
	ByPriority  map[string]int `json:"by_priority"`
	ByAge       map[string]int `json:"by_age"`
	ByProject   map[string]int `json:"by_project"`
}

type workloadEnvelope struct {
	WorkspaceID   string           `json:"workspace_id,omitempty"`
	Horizon       string           `json:"horizon"`
	Collaborators []workloadCollab `json:"collaborators"`
}

func newNovelWorkloadCmd(flags *rootFlags) *cobra.Command {
	var (
		flagWorkspace    string
		flagCollaborator string
		flagHorizon      string
		flagLimit        int
	)

	cmd := &cobra.Command{
		Use:   "workload",
		Short: "Per-collaborator open-task load report across workspace projects.",
		Long: `Cross-project rollup of open tasks per collaborator inside a workspace.
Buckets by priority, overdue-age (overdue / due-week / later), and per-project.`,
		Example: strings.Trim(`
  todoist-aum workload --workspace MyTeam --horizon 14d
  todoist-aum workload --collaborator alice --json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "local"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			horizonDur, err := cliutil.ParseDurationLoose(flagHorizon)
			if err != nil {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid --horizon %q: %w", flagHorizon, err))
			}

			db, err := openLocalStore(flags)
			if err != nil {
				return err
			}
			defer db.Close()

			// Optionally resolve --workspace by name or id.
			workspaceID := ""
			if flagWorkspace != "" {
				wid, _ := resolveWorkspaceIDByNameOrID(cmd.Context(), db, flagWorkspace)
				workspaceID = wid
			}

			// Collect tasks scoped to workspace projects when set.
			whereParts := []string{}
			args2 := []any{}
			if workspaceID != "" {
				whereParts = append(whereParts,
					`tasks.project_id IN (SELECT id FROM workspaces_projects WHERE workspaces_id = ?)`)
				args2 = append(args2, workspaceID)
			}
			extra := strings.Join(whereParts, " AND ")
			rows, err := scanOpenTasksWhere(cmd.Context(), db, extra, args2...)
			if err != nil {
				return err
			}

			// Resolve uid -> collaborator name (best-effort).
			nameByUID := map[string]string{}
			if crows, err := db.DB().QueryContext(cmd.Context(),
				`SELECT id, json_extract(data, '$.full_name') FROM collaborators`); err == nil {
				for crows.Next() {
					var id string
					var name sql.NullString
					if err := crows.Scan(&id, &name); err == nil && name.Valid {
						nameByUID[id] = name.String
					}
				}
				crows.Close()
			}

			now := time.Now()
			horizonEnd := now.Add(horizonDur)

			byUID := map[string]*workloadCollab{}
			for _, r := range rows {
				// taskRow doesn't carry responsible_uid; look it up lazily.
				uid := lookupResponsibleUID(cmd.Context(), db, r.ID)
				if uid == "" {
					continue
				}
				if flagCollaborator != "" {
					nm := strings.ToLower(nameByUID[uid])
					if !strings.Contains(nm, strings.ToLower(flagCollaborator)) && uid != flagCollaborator {
						continue
					}
				}
				w, ok := byUID[uid]
				if !ok {
					w = &workloadCollab{
						UID:        uid,
						Name:       nameByUID[uid],
						ByPriority: map[string]int{},
						ByAge:      map[string]int{},
						ByProject:  map[string]int{},
					}
					byUID[uid] = w
				}
				w.OpenTotal++
				w.ByPriority[r.PriorityHuman]++
				due := parseDueObjectTime(r.Due)
				switch {
				case !due.IsZero() && due.Before(now):
					w.ByAge["overdue"]++
				case !due.IsZero() && due.Before(now.Add(7*24*time.Hour)):
					w.ByAge["due_week"]++
				case !due.IsZero() && due.Before(horizonEnd):
					w.ByAge["later"]++
				default:
					w.ByAge["later"]++
				}
				key := r.ProjectName
				if key == "" {
					key = r.ProjectID
				}
				w.ByProject[key]++
			}

			env := workloadEnvelope{
				WorkspaceID:   workspaceID,
				Horizon:       flagHorizon,
				Collaborators: []workloadCollab{},
			}
			for _, w := range byUID {
				env.Collaborators = append(env.Collaborators, *w)
			}
			sort.Slice(env.Collaborators, func(i, j int) bool {
				return env.Collaborators[i].OpenTotal > env.Collaborators[j].OpenTotal
			})
			if flagLimit > 0 && len(env.Collaborators) > flagLimit {
				env.Collaborators = env.Collaborators[:flagLimit]
			}

			if flags.asJSON {
				return flags.printJSON(cmd, env)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "workload horizon=%s workspace_id=%s collaborators=%d\n",
				env.Horizon, env.WorkspaceID, len(env.Collaborators))
			for _, c := range env.Collaborators {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s (%s)  open=%d  by_priority=%v  by_age=%v\n",
					c.Name, c.UID, c.OpenTotal, c.ByPriority, c.ByAge)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagWorkspace, "workspace", "", "Workspace id or name")
	cmd.Flags().StringVar(&flagCollaborator, "collaborator", "", "Restrict to a collaborator (uid or name substring)")
	cmd.Flags().StringVar(&flagHorizon, "horizon", "14d", "Lookahead horizon (e.g. 7d, 14d, 30d)")
	cmd.Flags().IntVar(&flagLimit, "limit", 100, "Maximum number of collaborators to return")
	return cmd
}

// resolveWorkspaceIDByNameOrID returns a workspace id matching either the id
// itself or a name (case-insensitive). Returns ("", nil) if not found.
func resolveWorkspaceIDByNameOrID(ctx context.Context, db *store.Store, q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", nil
	}
	rows, err := db.DB().QueryContext(ctx,
		`SELECT id, json_extract(data, '$.name') FROM workspaces`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	lq := strings.ToLower(q)
	for rows.Next() {
		var id string
		var name sql.NullString
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		if id == q {
			return id, nil
		}
		if name.Valid && strings.EqualFold(name.String, q) {
			return id, nil
		}
		if name.Valid && strings.HasPrefix(strings.ToLower(name.String), lq) {
			return id, nil
		}
	}
	return "", nil
}

// lookupResponsibleUID pulls $.responsible_uid (falls back to $.assigned_by_uid)
// from tasks.data for the given task id. Returns "" when neither is set.
func lookupResponsibleUID(ctx context.Context, db *store.Store, id string) string {
	var raw sql.NullString
	if err := db.DB().QueryRowContext(ctx,
		`SELECT data FROM tasks WHERE id = ?`, id).Scan(&raw); err != nil {
		return ""
	}
	if !raw.Valid {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw.String), &m); err != nil {
		return ""
	}
	if v, ok := m["responsible_uid"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["assigned_by_uid"].(string); ok && v != "" {
		return v
	}
	return ""
}
