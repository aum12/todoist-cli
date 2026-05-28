// Hand-authored helpers shared by Todoist novel commands. Not generated.

package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aum12/todoist-cli/internal/store"
)

// humanPriorityToAPI converts a human "p1".."p4" priority into Todoist's
// inverted API integer (API 4 = UI p1 / highest; API 1 = UI p4 / lowest).
// Returns 0 when the input is empty, the parsed value when valid, or an error.
func humanPriorityToAPI(p string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "":
		return 0, nil
	case "p1", "1":
		return 4, nil
	case "p2", "2":
		return 3, nil
	case "p3", "3":
		return 2, nil
	case "p4", "4":
		return 1, nil
	default:
		return 0, fmt.Errorf("invalid priority %q (expected p1, p2, p3, or p4)", p)
	}
}

// apiPriorityToHuman converts the inverted Todoist API integer back to "p1".."p4".
func apiPriorityToHuman(p int) string {
	switch p {
	case 4:
		return "p1"
	case 3:
		return "p2"
	case 2:
		return "p3"
	case 1:
		return "p4"
	}
	return ""
}

// resolveProjectIDByName looks up a project by name (case-insensitive prefix
// match) in the local store. Returns the project id or an error. Use `""` to
// mean "Inbox" (the user's default inbox project).
func resolveProjectIDByName(ctx context.Context, db *store.Store, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	rows, err := db.DB().QueryContext(ctx,
		`SELECT id, json_extract(data, '$.name'), json_extract(data, '$.is_inbox_project')
		 FROM resources
		 WHERE resource_type = 'projects'
		   AND COALESCE(json_extract(data, '$.is_deleted'), 0) = 0`)
	if err != nil {
		return "", fmt.Errorf("querying projects: %w", err)
	}
	defer rows.Close()
	var exactID, prefixID, inboxID string
	wanted := strings.ToLower(name)
	for rows.Next() {
		var id string
		var pname sql.NullString
		var isInbox sql.NullInt64
		if err := rows.Scan(&id, &pname, &isInbox); err != nil {
			continue
		}
		if isInbox.Valid && isInbox.Int64 == 1 {
			inboxID = id
		}
		if !pname.Valid {
			continue
		}
		lc := strings.ToLower(pname.String)
		if lc == wanted {
			exactID = id
		} else if strings.HasPrefix(lc, wanted) && prefixID == "" {
			prefixID = id
		}
	}
	if exactID != "" {
		return exactID, nil
	}
	if prefixID != "" {
		return prefixID, nil
	}
	if strings.EqualFold(name, "inbox") && inboxID != "" {
		return inboxID, nil
	}
	return "", fmt.Errorf("project %q not found in local store (run `sync --resources projects --full` first?)", name)
}

// resolveSectionIDByName looks up a section by name within a given project.
// Returns the section id or an error. Empty name returns ("", nil).
func resolveSectionIDByName(ctx context.Context, db *store.Store, projectID, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	rows, err := db.DB().QueryContext(ctx,
		`SELECT id, json_extract(data, '$.name')
		 FROM sections WHERE project_id = ? AND (is_deleted IS NULL OR is_deleted = 0)`, projectID)
	if err != nil {
		return "", fmt.Errorf("querying sections: %w", err)
	}
	defer rows.Close()
	wanted := strings.ToLower(name)
	for rows.Next() {
		var id string
		var sname sql.NullString
		if err := rows.Scan(&id, &sname); err != nil {
			continue
		}
		if !sname.Valid {
			continue
		}
		if strings.ToLower(sname.String) == wanted {
			return id, nil
		}
	}
	return "", fmt.Errorf("section %q not found in project %s", name, projectID)
}

// taskRow is the projected shape used by agenda/near/focus/stale-review for
// local-store queries.
type taskRow struct {
	ID          string         `json:"id"`
	Content     string         `json:"content"`
	Description string         `json:"description,omitempty"`
	Priority    int            `json:"priority"`
	PriorityHuman string       `json:"priority_human"`
	ProjectID   string         `json:"project_id,omitempty"`
	ProjectName string         `json:"project_name,omitempty"`
	SectionID   string         `json:"section_id,omitempty"`
	ParentID    string         `json:"parent_id,omitempty"`
	Labels      []string       `json:"labels,omitempty"`
	Due         map[string]any `json:"due,omitempty"`
	Deadline    map[string]any `json:"deadline,omitempty"`
	AddedAt     string         `json:"added_at,omitempty"`
	UpdatedAt   string         `json:"updated_at,omitempty"`
	CompletedAt string         `json:"completed_at,omitempty"`
	URL         string         `json:"url,omitempty"`
}

// scanOpenTasksWhere runs a SELECT over `tasks` with the given extra WHERE
// fragment (parameterized) and returns hydrated rows with project_name joined.
// Caller passes the `extraWhere` (e.g. `tasks.due IS NOT NULL AND ...`) plus
// args; this helper handles the JOIN and the JSON decode.
func scanOpenTasksWhere(ctx context.Context, db *store.Store, extraWhere string, args ...any) ([]taskRow, error) {
	q := `
		SELECT tasks.id, tasks.content, COALESCE(tasks.description, ''),
		       COALESCE(tasks.priority, 1),
		       COALESCE(tasks.project_id, ''),
		       COALESCE(json_extract(projects.data, '$.name'), '') AS project_name,
		       COALESCE(tasks.section_id, ''),
		       COALESCE(tasks.parent_id, ''),
		       tasks.due, tasks.deadline,
		       COALESCE(tasks.added_at, ''),
		       COALESCE(tasks.updated_at, ''),
		       COALESCE(tasks.completed_at, ''),
		       tasks.data
		FROM tasks
		LEFT JOIN resources AS projects
		  ON projects.id = tasks.project_id AND projects.resource_type = 'projects'
		WHERE (tasks.is_deleted IS NULL OR tasks.is_deleted = 0)
		  AND (tasks.checked IS NULL OR tasks.checked = 0)`
	if strings.TrimSpace(extraWhere) != "" {
		q += " AND " + extraWhere
	}
	rows, err := db.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying tasks: %w", err)
	}
	defer rows.Close()
	var out []taskRow
	for rows.Next() {
		var (
			tr        taskRow
			projectN  sql.NullString
			dueRaw    sql.NullString
			deadRaw   sql.NullString
			dataRaw   sql.NullString
		)
		if err := rows.Scan(&tr.ID, &tr.Content, &tr.Description, &tr.Priority,
			&tr.ProjectID, &projectN, &tr.SectionID, &tr.ParentID,
			&dueRaw, &deadRaw, &tr.AddedAt, &tr.UpdatedAt, &tr.CompletedAt, &dataRaw); err != nil {
			continue
		}
		tr.PriorityHuman = apiPriorityToHuman(tr.Priority)
		if projectN.Valid {
			tr.ProjectName = projectN.String
		}
		if dueRaw.Valid && dueRaw.String != "" && dueRaw.String != "null" {
			_ = json.Unmarshal([]byte(dueRaw.String), &tr.Due)
		}
		if deadRaw.Valid && deadRaw.String != "" && deadRaw.String != "null" {
			_ = json.Unmarshal([]byte(deadRaw.String), &tr.Deadline)
		}
		if dataRaw.Valid && dataRaw.String != "" {
			var data map[string]any
			if err := json.Unmarshal([]byte(dataRaw.String), &data); err == nil {
				if labs, ok := data["labels"].([]any); ok {
					for _, l := range labs {
						if s, ok := l.(string); ok {
							tr.Labels = append(tr.Labels, s)
						}
					}
				}
				if u, ok := data["url"].(string); ok {
					tr.URL = u
				}
			}
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}

// parseDueObjectTime extracts a time.Time from a Todoist due struct map.
// Prefers datetime over date. Returns zero time if not parseable.
func parseDueObjectTime(due map[string]any) time.Time {
	if due == nil {
		return time.Time{}
	}
	if dt, ok := due["datetime"].(string); ok && dt != "" {
		if t, err := time.Parse(time.RFC3339, dt); err == nil {
			return t
		}
	}
	if d, ok := due["date"].(string); ok && d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			return t
		}
	}
	return time.Time{}
}

// requireSync returns an error suitable for novel commands that need a local
// store; the message tells the user how to recover.
func requireSync(resources string) error {
	return fmt.Errorf("local store missing %s data; run `todoist-aum sync --resources %s --full` first", resources, resources)
}

// captureCreatedTask is the shape returned by POST /api/v1/tasks; we only
// need the id and due fields for downstream reminder/location wiring.
type captureCreatedTask struct {
	ID  string         `json:"id"`
	Due map[string]any `json:"due"`
}

// parsedCreatedTask decodes the POST /tasks response body to extract just the
// id and due. Returns zero value on parse failure.
func parsedCreatedTask(raw json.RawMessage) captureCreatedTask {
	var t captureCreatedTask
	_ = json.Unmarshal(raw, &t)
	return t
}
