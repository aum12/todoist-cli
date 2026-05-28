
package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/aum12/todoist-cli/internal/cli"
	"github.com/aum12/todoist-cli/internal/client"
	"github.com/aum12/todoist-cli/internal/cliutil"
	"github.com/aum12/todoist-cli/internal/config"
	"github.com/aum12/todoist-cli/internal/mcp/cobratree"
	"github.com/aum12/todoist-cli/internal/store"
)

// RegisterTools registers all API operations as MCP tools.
func RegisterTools(s *server.MCPServer) {
	// Code-orchestration mode — the full surface is covered by two tools
	// (<api>_search + <api>_execute). Endpoint-mirror tools are suppressed.
	RegisterCodeOrchestrationTools(s)
	// Search tool — faster than iterating list endpoints for finding specific items
	s.AddTool(
		mcplib.NewTool("search",
			mcplib.WithDescription("Full-text search across all synced data. Faster than paginating list endpoints. Requires sync first."),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Search query (supports FTS5 syntax: AND, OR, NOT, quotes for phrases)")),
			mcplib.WithNumber("limit", mcplib.Description("Max results (default 25)")),
			mcplib.WithReadOnlyHintAnnotation(true),
			mcplib.WithDestructiveHintAnnotation(false),
		),
		handleSearch,
	)
	// SQL tool — ad-hoc analysis on synced data without API calls
	s.AddTool(
		mcplib.NewTool("sql",
			mcplib.WithDescription("Run read-only SQL against local database. Use for ad-hoc analysis, aggregations, and joins across synced resources. Requires sync first."),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("SQL query (SELECT or WITH...SELECT). Tables match resource names.")),
			mcplib.WithReadOnlyHintAnnotation(true),
			mcplib.WithDestructiveHintAnnotation(false),
		),
		handleSQL,
	)

	// Context tool — front-loaded domain knowledge for agents.
	// Call this first to understand the API taxonomy, query patterns, and capabilities.
	s.AddTool(
		mcplib.NewTool("context",
			mcplib.WithDescription("Get API domain context: resource taxonomy, auth requirements, query tips, and unique capabilities. Call this first."),
			mcplib.WithReadOnlyHintAnnotation(true),
			mcplib.WithDestructiveHintAnnotation(false),
		),
		handleContext,
	)

	// Runtime Cobra-tree mirror — exposes every user-facing command that is
	// not already covered by a typed endpoint or framework MCP tool.
	cobratree.RegisterAll(s, cli.RootCmd(), cobratree.SiblingCLIPath)
}

type mcpParamBinding struct {
	PublicName         string
	WireName           string
	Location           string
	Format             string
	RequestContentType string
}

func mcpMultipartFieldValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if data, err := json.Marshal(v); err == nil {
		return string(data)
	}
	return fmt.Sprintf("%v", v)
}

// makeAPIHandler creates a generic MCP tool handler for an API endpoint.
func makeAPIHandler(method, pathTemplate string, readOnly bool, binaryResponse bool, headerOverrides map[string]string, bindings []mcpParamBinding, positionalParams []string) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		c, err := newMCPClient()
		if err != nil {
			return mcplib.NewToolResultError(err.Error()), nil
		}

		// mcp-go v0.47+ made CallToolParams.Arguments an `any` to support
		// non-map payloads; GetArguments() returns the map[string]any shape
		// we rely on here (or an empty map when the payload is something else).
		args := req.GetArguments()

		// positionalParams mixes real URL path params with CLI positional
		// args that map to query params (e.g. `search <query>` -> ?query=);
		// the placeholder check below disambiguates them at runtime.
		path := pathTemplate
		knownArgs := make(map[string]bool, len(bindings))
		pathParams := make(map[string]bool, len(positionalParams))
		params := make(map[string]string)
		bodyArgs := make(map[string]any)
		var headers map[string]string
		if len(headerOverrides) > 0 {
			headers = make(map[string]string, len(headerOverrides)+1)
			for k, v := range headerOverrides {
				headers[k] = v
			}
		}
		if binaryResponse {
			if headers == nil {
				headers = map[string]string{}
			}
			headers[client.BinaryResponseHeader] = "true"
		}
		multipartFields := make(map[string]string)
		multipartFileFields := make(map[string]string)
		multipart := false
		for _, binding := range bindings {
			knownArgs[binding.PublicName] = true
			if strings.EqualFold(binding.RequestContentType, "multipart/form-data") {
				multipart = true
			}
			v, ok := args[binding.PublicName]
			if !ok {
				continue
			}
			switch binding.Location {
			case "path":
				placeholder := "{" + binding.WireName + "}"
				pathParams[binding.PublicName] = true
				path = strings.Replace(path, placeholder, fmt.Sprintf("%v", v), 1)
			case "body":
				bodyArgs[binding.WireName] = v
				if multipart {
					if strings.EqualFold(binding.Format, "binary") {
						multipartFileFields[binding.WireName] = fmt.Sprintf("%v", v)
					} else {
						multipartFields[binding.WireName] = mcpMultipartFieldValue(v)
					}
				}
			default:
				params[binding.WireName] = fmt.Sprintf("%v", v)
			}
		}
		for _, p := range positionalParams {
			placeholder := "{" + p + "}"
			if !strings.Contains(pathTemplate, placeholder) {
				continue
			}
			pathParams[p] = true
			if v, ok := args[p]; ok {
				path = strings.Replace(path, placeholder, fmt.Sprintf("%v", v), 1)
			}
		}

		for k, v := range args {
			if pathParams[k] || knownArgs[k] {
				continue
			}
			switch method {
			case "POST", "PUT", "PATCH":
				bodyArgs[k] = v
				if multipart {
					multipartFields[k] = mcpMultipartFieldValue(v)
				}
			default:
				params[k] = fmt.Sprintf("%v", v)
			}
		}

		var data json.RawMessage
		switch method {
		case "GET":
			if len(headers) > 0 {
				data, err = c.GetWithHeaders(ctx, path, params, headers)
				break
			}
			data, err = c.Get(ctx, path, params)
		case "POST":
			if multipart {
				if len(headers) > 0 {
					data, _, err = c.PostMultipartWithParamsAndHeaders(ctx, path, params, multipartFields, multipartFileFields, headers)
					break
				}
				data, _, err = c.PostMultipartWithParams(ctx, path, params, multipartFields, multipartFileFields)
				break
			}
			if len(headers) > 0 {
				if readOnly {
					data, _, err = c.PostQueryWithParamsAndHeaders(ctx, path, params, bodyArgs, headers)
				} else {
					data, _, err = c.PostWithParamsAndHeaders(ctx, path, params, bodyArgs, headers)
				}
				break
			}
			if readOnly {
				data, _, err = c.PostQueryWithParams(ctx, path, params, bodyArgs)
			} else {
				data, _, err = c.PostWithParams(ctx, path, params, bodyArgs)
			}
		case "PUT":
			if multipart {
				if len(headers) > 0 {
					data, _, err = c.PutMultipartWithParamsAndHeaders(ctx, path, params, multipartFields, multipartFileFields, headers)
					break
				}
				data, _, err = c.PutMultipartWithParams(ctx, path, params, multipartFields, multipartFileFields)
				break
			}
			if len(headers) > 0 {
				data, _, err = c.PutWithParamsAndHeaders(ctx, path, params, bodyArgs, headers)
				break
			}
			data, _, err = c.PutWithParams(ctx, path, params, bodyArgs)
		case "PATCH":
			if multipart {
				if len(headers) > 0 {
					data, _, err = c.PatchMultipartWithParamsAndHeaders(ctx, path, params, multipartFields, multipartFileFields, headers)
					break
				}
				data, _, err = c.PatchMultipartWithParams(ctx, path, params, multipartFields, multipartFileFields)
				break
			}
			if len(headers) > 0 {
				data, _, err = c.PatchWithParamsAndHeaders(ctx, path, params, bodyArgs, headers)
				break
			}
			data, _, err = c.PatchWithParams(ctx, path, params, bodyArgs)
		case "DELETE":
			if len(headers) > 0 {
				data, _, err = c.DeleteWithParamsAndHeaders(ctx, path, params, headers)
				break
			}
			data, _, err = c.DeleteWithParams(ctx, path, params)
		default:
			return mcplib.NewToolResultError("unsupported method: " + method), nil
		}

		if err != nil {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "HTTP 409"):
				return mcplib.NewToolResultText("already exists (no-op)"), nil
			case strings.Contains(msg, "HTTP 400") && cliutil.LooksLikeAuthError(msg):
				return mcplib.NewToolResultError("authentication error: " + cliutil.SanitizeErrorBody(msg) +
					"\nhint: the API rejected the request — this usually means auth is missing or invalid." +
					"\n      Set your API key: export TODOIST_API_TOKEN=<your-key>" +
					"\n      Run 'todoist-aum doctor' to check auth status."), nil
			case strings.Contains(msg, "HTTP 401"):
				return mcplib.NewToolResultError("authentication failed: " + cliutil.SanitizeErrorBody(msg) +
					"\nhint: check your token." +
					"\n      Set it with: export TODOIST_API_TOKEN=<your-key>" +
					"\n      Run 'todoist-aum doctor' to check auth status."), nil
			case strings.Contains(msg, "HTTP 403"):
				return mcplib.NewToolResultError("permission denied: " + cliutil.SanitizeErrorBody(msg) +
					"\nhint: your credentials are valid but lack access to this resource." +
					"\n      Set it with: export TODOIST_API_TOKEN=<your-key>" +
					"\n      Run 'todoist-aum doctor' to check auth status."), nil
			case strings.Contains(msg, "HTTP 404"):
				if method == "DELETE" {
					return mcplib.NewToolResultText("already deleted (no-op)"), nil
				}
				return mcplib.NewToolResultError("not found: " + msg), nil
			case strings.Contains(msg, "HTTP 429"):
				return mcplib.NewToolResultError("rate limited: " + msg), nil
			default:
				return mcplib.NewToolResultError(msg), nil
			}
		}

		// For GET responses, wrap bare arrays with count metadata
		if method == "GET" {
			trimmed := strings.TrimSpace(string(data))
			if len(trimmed) > 0 && trimmed[0] == '[' {
				var items []json.RawMessage
				if json.Unmarshal(data, &items) == nil {
					wrapped := map[string]any{
						"count": len(items),
						"items": items,
					}
					out, _ := json.Marshal(wrapped)
					return mcplib.NewToolResultText(string(out)), nil
				}
			}
		}
		if binaryResponse {
			out, _ := json.Marshal(map[string]any{
				"content_encoding": "base64",
				"data_base64":      base64.StdEncoding.EncodeToString(data),
				"byte_count":       len(data),
			})
			return mcplib.NewToolResultText(string(out)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}
}

func newMCPClient() (*client.Client, error) {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "todoist-aum", "config.toml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	c := client.New(cfg, 60*time.Second, 0)
	// Agents calling through MCP need fresh data every call. The on-disk
	// response cache survives across MCP server invocations, so a
	// DELETE/PATCH followed by a GET would otherwise return the
	// pre-mutation snapshot for up to the cache TTL. The interactive CLI
	// constructs its own client and is unaffected.
	c.NoCache = true
	return c, nil
}

func dbPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "todoist-aum", "data.db")
}

// Note: MCP tools use their own dbPath() because they are in a separate package (main, not cli).
// The CLI's defaultDBPath() in the cli package uses the same canonical path.

func handleSearch(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return mcplib.NewToolResultError("query is required"), nil
	}

	limit := 25
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	db, err := store.OpenReadOnly(dbPath())
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("opening database: %v", err)), nil
	}
	defer db.Close()

	results, err := db.Search(query, limit)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	return mcplib.NewToolResultText(string(data)), nil
}

// validateReadOnlyQuery gates the MCP sql tool. The agent contract advertised
// to the host is ReadOnlyHintAnnotation(true); a false annotation on a
// mutating tool lets MCP hosts auto-approve writes and is treated as a real
// bug per the project's agent-native security model.
//
// The gate is an allowlist (SELECT or WITH only) applied AFTER stripping the
// leading whitespace, line comments, block comments, and semicolons that
// SQLite itself ignores before parsing. A naive HasPrefix check on a
// keyword blocklist is bypassable by prefixing the dangerous statement with
// "/* x */" or "-- x\n" — TrimSpace strips outer whitespace but does not
// understand SQL comment syntax. Combined with the empirical fact that
// modernc.org/sqlite's mode=ro does NOT block VACUUM INTO (writes a snapshot
// to a new file) or ATTACH DATABASE (opens a separate writable handle),
// such a bypass produces silent exfiltration to an attacker-chosen path.
//
// SELECT and WITH are the only allowed leading keywords. WITH supports
// SELECT-form CTEs; CTE-wrapped writes ("WITH x AS (...) INSERT ...") are
// caught by OpenReadOnly's mode=ro one layer down. PRAGMA, ATTACH, VACUUM,
// and every other DDL/DML keyword fail at this gate before reaching SQLite.
func validateReadOnlyQuery(query string) error {
	upper := strings.ToUpper(stripLeadingSQLNoise(query))
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return fmt.Errorf("only SELECT queries are allowed")
	}
	return nil
}

// stripLeadingSQLNoise removes leading whitespace, SQL line comments
// (-- to end of line), block comments (/* ... */), and statement
// separators (;) from query. SQLite skips these before parsing the first
// keyword, so a security gate that does not strip them mismatches what the
// driver actually executes.
func stripLeadingSQLNoise(query string) string {
	for {
		query = strings.TrimLeft(query, " \t\r\n;")
		switch {
		case strings.HasPrefix(query, "--"):
			if idx := strings.IndexByte(query, '\n'); idx >= 0 {
				query = query[idx+1:]
				continue
			}
			return ""
		case strings.HasPrefix(query, "/*"):
			if idx := strings.Index(query[2:], "*/"); idx >= 0 {
				query = query[2+idx+2:]
				continue
			}
			return ""
		default:
			return query
		}
	}
}

func handleSQL(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return mcplib.NewToolResultError("query is required"), nil
	}

	if err := validateReadOnlyQuery(query); err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}

	db, err := store.OpenReadOnly(dbPath())
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("opening database: %v", err)), nil
	}
	defer db.Close()

	rows, err := db.Query(query)
	if err != nil {
		return mcplib.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		rows.Scan(ptrs...)
		row := make(map[string]any)
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	data, _ := json.MarshalIndent(results, "", "  ")
	return mcplib.NewToolResultText(string(data)), nil
}

func handleContext(_ context.Context, _ mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	ctx := map[string]any{
		"api":         "todoist",
		"description": "Every Todoist feature, plus voice-friendly capture, ADHD-aware daily loops",
		"archetype":   "project-management",
		"tool_count":  102,
		// tool_surface tells agents which surface a capability lives on.
		"tool_surface": "MCP exposes typed endpoint tools plus a runtime mirror of user-facing CLI commands. Endpoint tools keep typed schemas; command-mirror tools shell out to the companion todoist-aum binary.",
		"auth": map[string]any{
			"type": "bearer_token",
			"env_vars": []map[string]any{
				{
					"name":        "TODOIST_API_TOKEN",
					"kind":        "per_call",
					"required":    false,
					"sensitive":   true,
					"description": "Set to your API credential.",
				},
				{
					"name":        "TODOIST_BEARER_AUTH",
					"kind":        "per_call",
					"required":    false,
					"sensitive":   true,
					"description": "Set to your API credential.",
				},
			},
		},
		"resources": []map[string]any{
			{
				"name":        "access-tokens",
				"description": "Manage access tokens",
				"endpoints":   []string{"migrate-personal-token", "revoke-api"},
				"searchable":  true,
			},
			{
				"name":        "activities",
				"description": "Manage activities",
				"endpoints":   []string{"get-activity-logs"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "backups",
				"description": "_Availability of backups functionality is dependent on the current user plan.",
				"endpoints":   []string{"download", "get"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "comments",
				"description": "Manage comments",
				"endpoints":   []string{"create", "delete", "get", "get-commentid", "update"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "emails",
				"description": "Manage emails",
				"endpoints":   []string{"disable", "get-or-create"},
				"searchable":  true,
			},
			{
				"name":        "folders",
				"description": "Manage folders",
				"endpoints":   []string{"create", "delete", "get", "get-folderid", "update"},
				"searchable":  true,
			},
			{
				"name":        "id-mappings",
				"description": "Manage id mappings",
				"endpoints":   []string{"id_mappings"},
				"searchable":  true,
			},
			{
				"name":        "labels",
				"description": "Manage labels",
				"endpoints":   []string{"create", "delete", "get", "get-labelid", "search", "shared", "shared-remove", "shared-rename", "update"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "location-reminders",
				"description": "_Availability of location reminders is dependent on the current user plan._",
				"endpoints":   []string{"create", "delete", "get", "get-locationreminders", "update"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "notification-setting",
				"description": "Manage notification setting",
				"endpoints":   []string{"update"},
				"searchable":  true,
			},
			{
				"name":        "payments",
				"description": "Manage payments",
				"endpoints":   []string{"cancel-plan-with-redirect-to-stripe", "get-subscription-info", "reactivate-plan"},
				"searchable":  true,
			},
			{
				"name":        "projects",
				"description": "Manage projects",
				"endpoints":   []string{"create", "delete", "get", "get-archived", "get-projectid", "permissions", "search", "update"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "reminders",
				"description": "_Availability of reminders is dependent on the current user plan._",
				"endpoints":   []string{"create", "delete", "get", "get-reminderid", "update"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "revoke",
				"description": "Manage revoke",
				"endpoints":   []string{"token-rfc7009-compliant"},
				"searchable":  true,
			},
			{
				"name":        "sections",
				"description": "Manage sections",
				"endpoints":   []string{"create", "delete", "get", "get-sectionid", "search", "update"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "tasks",
				"description": "Manage tasks",
				"endpoints":   []string{"completed-by-completion-date", "completed-by-due-date", "create", "delete", "get", "get-by-filter", "get-productivity-stats", "get-taskid", "quick-add", "update"},
				"syncable":    true,
				"searchable":  true,
			},
			{
				"name":        "templates",
				"description": "Templates allow exporting of a project's tasks to a file or URL",
				"endpoints":   []string{"create", "export-as-file", "export-as-url", "import-into-project-from-file", "import-into-project-from-id"},
				"searchable":  true,
			},
			{
				"name":        "uploads",
				"description": "Availability of uploads functionality and the maximum size for a file attachment are dependent on the current user plan.",
				"endpoints":   []string{"delete", "file"},
				"searchable":  true,
			},
			{
				"name":        "user",
				"description": "Manage user",
				"endpoints":   []string{"info"},
			},
			{
				"name":        "workspaces",
				"description": "Manage workspaces",
				"endpoints":   []string{"accept-invitation", "all-invitations", "create", "delete", "delete-invitation", "get", "get-users", "get-workspaceid", "invitations", "join", "plan-details", "reject-invitation", "update", "update-logo"},
				"syncable":    true,
				"searchable":  true,
			},
		},
		"query_tips": []string{
			"Pagination uses cursor-based paging. Pass cursor parameter for subsequent pages.",
			"Control page size with the limit parameter (default 100).",
			"Use since for incremental fetches (filter by modification time).",
			"Use the sql tool for ad-hoc analysis on synced data. Run sync first to populate the local database.",
			"Use the search tool for full-text search across all synced resources. Faster than iterating list endpoints.",
			"Prefer sql/search over repeated API calls when the data is already synced.",
		},
		// Command-mirror capabilities are exposed through MCP by shelling out
		// to the companion CLI binary.
		"command_mirror_capabilities": []map[string]string{
			{"name": "Composed NL capture with date, reminders, and location", "command": "capture", "description": "Voice/agent/routine-driven task entry.", "rationale": "One composed call atomically POSTs to /tasks then /reminders (one per reminder)", "via": "mcp-command-mirror"},
			{"name": "Unified daily/weekly/monthly review", "command": "review", "description": "Retrospective rollup with prior-period delta.", "rationale": "Joins local completed_tasks and activity tables with live productivity stats and computes deltas vs the prior period —", "via": "mcp-command-mirror"},
			{"name": "Unified daily agenda", "command": "agenda", "description": "Today's tasks plus the overdue tail plus a windowed lookahead with priority and project context — composed from the", "rationale": "Joins tasks, projects, and labels in SQLite to assemble a view no single Todoist endpoint exposes", "via": "mcp-command-mirror"},
			{"name": "Inbox triage with dry-run plan", "command": "triage", "description": "For each Inbox item, propose the most likely (project, label, section)", "rationale": "Token-overlap match against synced historical tasks is a local-SQL join no API endpoint offers", "via": "mcp-command-mirror"},
			{"name": "Reschedule cascade with preview", "command": "reschedule-cascade", "description": "Postpone every task matching a Todoist filter by a relative shift, previewing per-task new due strings", "rationale": "The Todoist web UI bulk-edits without preview; recurring rules and deadline-vs-due distinctions are easy to miss.", "via": "mcp-command-mirror"},
			{"name": "Filter-batch with preview-then-commit", "command": "filter-batch", "description": "Bulk complete, move, or relabel every task matching a Todoist filter", "rationale": "scholer/actionista pioneered the chain-of-actions pattern but made commit irreversible.", "via": "mcp-command-mirror"},
			{"name": "Daily focus loop (ADHD-friendly goal setter)", "command": "focus", "description": "`focus set --top N --reason <why>` picks the highest-priority/deadline-tightest tasks and labels them @focus-today", "rationale": "Cross-cuts tasks ⋈ labels ⋈ deadlines as a daily ritual that no atomic MCP tool exposes", "via": "mcp-command-mirror"},
			{"name": "Location/context-aware task surfacing", "command": "near", "description": "Takes a label or context name (walmart, home, office)", "rationale": "Composes label + project matching with priority/age ranking; the Todoist web UI cannot rank cross-label/project", "via": "mcp-command-mirror"},
			{"name": "Productivity trend rollups", "command": "productivity-trend", "description": "Rollup over local completed_tasks with group-by dimensions (label, hour-of-day)", "rationale": "completed_tasks ⋈ projects ⋈ labels join is purely local", "via": "mcp-command-mirror"},
			{"name": "Workspace workload by collaborator", "command": "workload", "description": "Cross-project rollup of open tasks per collaborator inside a workspace, by priority, overdue-age bucket, and project.", "rationale": "Joins local tasks ⋈ projects ⋈ collaborators ⋈ workspaces — the Todoist web UI shows one project at a time and exposes", "via": "mcp-command-mirror"},
			{"name": "Reschedule history timeline", "command": "reschedule-history", "description": "Walk cached activity events for a task (or filter) and show every time the due date moved, by how much, on which day.", "rationale": "Activity log is premium-gated and surfaced by zero competing tools", "via": "mcp-command-mirror"},
			{"name": "Stale-task review with suggested actions", "command": "stale-review", "description": "Surface open tasks that are old and inactive", "rationale": "Pure local SQL identifies stale tasks; the per-task action is a mechanical rule, not an LLM call.", "via": "mcp-command-mirror"},
		},
		"playbook": []map[string]string{
			{"topic": "Composed NL capture with date, reminders, and location", "insight": "One composed call atomically POSTs to /tasks then /reminders (one per reminder) then /location_reminders if location is set. No competitor weaves Date + Deadline + multi-Reminder + location + multi-label + timezone-aware datetime into one round trip."},
			{"topic": "Unified daily/weekly/monthly review", "insight": "Joins local completed_tasks and activity tables with live productivity stats and computes deltas vs the prior period — analysis the API stats endpoint does not produce."},
			{"topic": "Unified daily agenda", "insight": "Joins tasks, projects, and labels in SQLite to assemble a view no single Todoist endpoint exposes; every existing CLI ships separate `today`, `inbox`, and `overdue` commands instead."},
			{"topic": "Inbox triage with dry-run plan", "insight": "Token-overlap match against synced historical tasks is a local-SQL join no API endpoint offers; proposals are mechanical (modal of co-occurring tags), no LLM required."},
			{"topic": "Reschedule cascade with preview", "insight": "The Todoist web UI bulk-edits without preview; recurring rules and deadline-vs-due distinctions are easy to miss. Server filter + local due-string preview + batched commit is unique to a CLI with a local store."},
			{"topic": "Filter-batch with preview-then-commit", "insight": "scholer/actionista pioneered the chain-of-actions pattern but made commit irreversible. We make `--dry-run` the default and require explicit `--apply` for write."},
			{"topic": "Daily focus loop (ADHD-friendly goal setter)", "insight": "Cross-cuts tasks ⋈ labels ⋈ deadlines as a daily ritual that no atomic MCP tool exposes; designed to be paired with `review --window day` for end-of-day adherence tracking."},
			{"topic": "Location/context-aware task surfacing", "insight": "Composes label + project matching with priority/age ranking; the Todoist web UI cannot rank cross-label/project, and `tasks get-tasks-by-filter @walmart` returns unordered results."},
			{"topic": "Productivity trend rollups", "insight": "completed_tasks ⋈ projects ⋈ labels join is purely local; Todoist's API stats endpoint only exposes daily/weekly aggregate counts, not label-level or hour-of-day breakdowns."},
			{"topic": "Workspace workload by collaborator", "insight": "Joins local tasks ⋈ projects ⋈ collaborators ⋈ workspaces — the Todoist web UI shows one project at a time and exposes no cross-project collaborator view."},
			{"topic": "Reschedule history timeline", "insight": "Activity log is premium-gated and surfaced by zero competing tools; the timeline is a pure local query over cached events."},
			{"topic": "Stale-task review with suggested actions", "insight": "Pure local SQL identifies stale tasks; the per-task action is a mechanical rule, not an LLM call. No competitor surfaces stale tasks with an actionable plan."},
			{"topic": "Finding stale work", "insight": "Use the stale command or sql query to find items not updated recently. More reliable than scanning list results manually."},
			{"topic": "Load analysis", "insight": "When analyzing team workload, filter by assignee and status. Raw counts without status filtering are misleading."},
			{"topic": "Bulk operations", "insight": "For bulk status changes, prefer update endpoints over delete+create. Most PM APIs track history on updates."},
		},
	}
	data, _ := json.MarshalIndent(ctx, "", "  ")
	return mcplib.NewToolResultText(string(data)), nil
}

// RegisterNovelFeatureTools is kept as a compatibility no-op for older MCP
// mains. New generated mains call RegisterTools only; RegisterTools now
// includes the runtime Cobra-tree mirror.
func RegisterNovelFeatureTools(s *server.MCPServer) {
	_ = s
}
