
// Package mcp — code-orchestration thin surface.
//
// Two tools cover the entire API: <api>_search to discover endpoints, and
// <api>_execute to invoke one. This collapses a large API (50+ endpoints)
// to ~1K tokens of tool definitions while preserving full coverage — the
// agent writes the composition logic in its own sandbox.
//
// Pattern source: Anthropic 2026-04-22 "Building agents that reach
// production systems with MCP" — Cloudflare's MCP server covers ~2,500
// endpoints in roughly 1K tokens via the same search+execute shape.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"sort"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterCodeOrchestrationTools registers the two agent-facing tools that
// cover the whole API surface. Called from RegisterTools in place of the
// per-endpoint registrations when MCP.Orchestration is "code".
func RegisterCodeOrchestrationTools(s *server.MCPServer) {
	s.AddTool(
		mcplib.NewTool("todoist_search",
			mcplib.WithDescription("Search the todoist API for endpoints matching a natural-language query. Returns a ranked list of {endpoint_id, method, path, summary} entries. Call this first to find the endpoint to execute."),
			mcplib.WithString("query", mcplib.Required(), mcplib.Description("Natural-language description of what you want to do.")),
			mcplib.WithNumber("limit", mcplib.Description("Max endpoints to return (default 10).")),
		),
		handleCodeOrchSearch,
	)

	s.AddTool(
		mcplib.NewTool("todoist_execute",
			mcplib.WithDescription("Execute one todoist API endpoint by its endpoint_id (from todoist_search). Params are passed as a JSON object; path placeholders and query strings are resolved automatically."),
			mcplib.WithString("endpoint_id", mcplib.Required(), mcplib.Description("Endpoint identifier returned by todoist_search (e.g., \"users.list\").")),
			mcplib.WithObject("params", mcplib.Description("Parameters for the endpoint. Path placeholders match by name; remaining entries become query string on GET/DELETE or JSON body on POST/PUT/PATCH.")),
		),
		handleCodeOrchExecute,
	)
}

// codeOrchEndpoint captures the small slice of endpoint metadata the
// search+execute pair needs at runtime. `keywords` is a precomputed
// lowercase stream of description + path tokens used for naive ranking;
// anything more sophisticated belongs on the agent side.
type codeOrchEndpoint struct {
	ID         string
	Method     string
	Path       string
	Tier       string
	Summary    string
	Positional []string
	// TemplateParams carries public-to-wire bindings for promoted global path
	// placeholders. These resolve through Config.TemplateVars just like the
	// per-endpoint MCP tool inputs do.
	TemplateParams []codeOrchParamBinding
	// QueryParams carries public-to-wire bindings for spec-declared in:query
	// parameters. Write methods (POST/PUT/PATCH) route these to the query
	// string instead of dumping them into the JSON body. Derived from the
	// same mcpParamBindings location data the per-endpoint tools use.
	QueryParams []codeOrchParamBinding
	// HeaderOverrides carries per-endpoint request headers (e.g. an
	// Accept override for binary-only response endpoints). Without
	// threading these through, the code-orchestration execute path
	// sends the client's default Accept and binary endpoints 406.
	HeaderOverrides map[string]string
	// BodyIsArray marks endpoints whose request body schema root is a
	// bare top-level JSON array. The execute path then sends a top-level
	// array (the agent supplies it as params["body"]) instead of the
	// params object; a strict-mapping API rejects an object at the body
	// root with HTTP 422 "Invalid json".
	BodyIsArray bool
	keywords    []string
}

type codeOrchParamBinding struct {
	PublicName string
	WireName   string
}

// codeOrchEndpoints is the generator-populated registry covering every
// endpoint declared in the spec. Kept flat on purpose — the agent queries
// via <api>_search, so hierarchy shows up as dotted IDs, not nested maps.
var codeOrchEndpoints = []codeOrchEndpoint{
	{
		ID:             "access-tokens.migrate-personal-token",
		Method:         "POST",
		Path:           "/api/v1/access_tokens/migrate_personal_token",
		Summary:        "Tokens obtained via the old email/password authentication method can be migrated to the new OAuth access token.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("access-tokens", "migrate-personal-token", "Tokens obtained via the old email/password authentication method can be migrated to the new OAuth access token.", "/api/v1/access_tokens/migrate_personal_token"),
	},
	{
		ID:             "access-tokens.revoke-api",
		Method:         "DELETE",
		Path:           "/api/v1/access_tokens",
		Summary:        "Revoke the access tokens obtained via OAuth",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "client_id", WireName: "client_id"}, {PublicName: "client_secret", WireName: "client_secret"}, {PublicName: "access_token", WireName: "access_token"}},
		keywords:       codeOrchKeywords("access-tokens", "revoke-api", "Revoke the access tokens obtained via OAuth", "/api/v1/access_tokens"),
	},
	{
		ID:             "activities.get-activity-logs",
		Method:         "GET",
		Path:           "/api/v1/activities",
		Summary:        "Get activity logs. Returns a paginated list of activity events for the user.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "object_type", WireName: "object_type"}, {PublicName: "object_id", WireName: "object_id"}, {PublicName: "parent_project_id", WireName: "parent_project_id"}, {PublicName: "parent_item_id", WireName: "parent_item_id"}, {PublicName: "include_parent_object", WireName: "include_parent_object"}, {PublicName: "include_child_objects", WireName: "include_child_objects"}, {PublicName: "initiator_id", WireName: "initiator_id"}, {PublicName: "initiator_id_null", WireName: "initiator_id_null"}, {PublicName: "event_type", WireName: "event_type"}, {PublicName: "ensure_last_state", WireName: "ensure_last_state"}, {PublicName: "object_event_types", WireName: "object_event_types"}, {PublicName: "workspace_id", WireName: "workspace_id"}, {PublicName: "annotate_notes", WireName: "annotate_notes"}, {PublicName: "annotate_parents", WireName: "annotate_parents"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}, {PublicName: "date_from", WireName: "date_from"}, {PublicName: "date_to", WireName: "date_to"}},
		keywords:       codeOrchKeywords("activities", "get-activity-logs", "Get activity logs. Returns a paginated list of activity events for the user.", "/api/v1/activities"),
	},
	{
		ID:             "backups.download",
		Method:         "GET",
		Path:           "/api/v1/backups/download",
		Summary:        "Download a backup archive.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "file", WireName: "file"}},
		keywords:       codeOrchKeywords("backups", "download", "Download a backup archive.", "/api/v1/backups/download"),
	},
	{
		ID:             "backups.get",
		Method:         "GET",
		Path:           "/api/v1/backups",
		Summary:        "Todoist creates a backup archive of users' data on a daily basis.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "mfa_token", WireName: "mfa_token"}},
		keywords:       codeOrchKeywords("backups", "get", "Todoist creates a backup archive of users' data on a daily basis.", "/api/v1/backups"),
	},
	{
		ID:             "comments.create",
		Method:         "POST",
		Path:           "/api/v1/comments",
		Summary:        "Creates a new comment on a project or task and returns it.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("comments", "create", "Creates a new comment on a project or task and returns it.", "/api/v1/comments"),
	},
	{
		ID:             "comments.delete",
		Method:         "DELETE",
		Path:           "/api/v1/comments/{comment_id}",
		Summary:        "Delete a comment by ID",
		Positional:     []string{"comment_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("comments", "delete", "Delete a comment by ID", "/api/v1/comments/{comment_id}"),
	},
	{
		ID:             "comments.get",
		Method:         "GET",
		Path:           "/api/v1/comments",
		Summary:        "Get all comments for a given task or project. Exactly one of `task_id` or `project_id` arguments is required.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "project_id", WireName: "project_id"}, {PublicName: "task_id", WireName: "task_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}, {PublicName: "public_key", WireName: "public_key"}},
		keywords:       codeOrchKeywords("comments", "get", "Get all comments for a given task or project. Exactly one of `task_id` or `project_id` arguments is required.", "/api/v1/comments"),
	},
	{
		ID:             "comments.get-commentid",
		Method:         "GET",
		Path:           "/api/v1/comments/{comment_id}",
		Summary:        "Returns a single comment by ID",
		Positional:     []string{"comment_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("comments", "get-commentid", "Returns a single comment by ID", "/api/v1/comments/{comment_id}"),
	},
	{
		ID:             "comments.update",
		Method:         "POST",
		Path:           "/api/v1/comments/{comment_id}",
		Summary:        "Update a comment by ID and returns its content",
		Positional:     []string{"comment_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("comments", "update", "Update a comment by ID and returns its content", "/api/v1/comments/{comment_id}"),
	},
	{
		ID:             "emails.disable",
		Method:         "DELETE",
		Path:           "/api/v1/emails",
		Summary:        "Disable the current email to a Todoist object",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "obj_type", WireName: "obj_type"}, {PublicName: "obj_id", WireName: "obj_id"}},
		keywords:       codeOrchKeywords("emails", "disable", "Disable the current email to a Todoist object", "/api/v1/emails"),
	},
	{
		ID:             "emails.get-or-create",
		Method:         "PUT",
		Path:           "/api/v1/emails",
		Summary:        "Get or create an email to a Todoist object, currently only projects and tasks are supported.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("emails", "get-or-create", "Get or create an email to a Todoist object, currently only projects and tasks are supported.", "/api/v1/emails"),
	},
	{
		ID:             "folders.create",
		Method:         "POST",
		Path:           "/api/v1/folders",
		Summary:        "Create a new folder in the given workspace.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("folders", "create", "Create a new folder in the given workspace.", "/api/v1/folders"),
	},
	{
		ID:             "folders.delete",
		Method:         "DELETE",
		Path:           "/api/v1/folders/{folder_id}",
		Summary:        "Delete a folder. Projects in the folder will be moved out of it.",
		Positional:     []string{"folder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("folders", "delete", "Delete a folder. Projects in the folder will be moved out of it.", "/api/v1/folders/{folder_id}"),
	},
	{
		ID:             "folders.get",
		Method:         "GET",
		Path:           "/api/v1/folders",
		Summary:        "Get all folders for a workspace. This is a paginated endpoint.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "workspace_id", WireName: "workspace_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("folders", "get", "Get all folders for a workspace. This is a paginated endpoint.", "/api/v1/folders"),
	},
	{
		ID:             "folders.get-folderid",
		Method:         "GET",
		Path:           "/api/v1/folders/{folder_id}",
		Summary:        "Return the folder for the given folder ID.",
		Positional:     []string{"folder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("folders", "get-folderid", "Return the folder for the given folder ID.", "/api/v1/folders/{folder_id}"),
	},
	{
		ID:             "folders.update",
		Method:         "POST",
		Path:           "/api/v1/folders/{folder_id}",
		Summary:        "Update an existing folder.",
		Positional:     []string{"folder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("folders", "update", "Update an existing folder.", "/api/v1/folders/{folder_id}"),
	},
	{
		ID:             "id-mappings.id_mappings",
		Method:         "GET",
		Path:           "/api/v1/id_mappings/{obj_name}/{obj_ids}",
		Summary:        "Translates IDs from v1 to v2 or vice versa.",
		Positional:     []string{"obj_ids"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("id-mappings", "id_mappings", "Translates IDs from v1 to v2 or vice versa.", "/api/v1/id_mappings/{obj_name}/{obj_ids}"),
	},
	{
		ID:             "labels.create",
		Method:         "POST",
		Path:           "/api/v1/labels",
		Summary:        "Create a personal label. Premium limits apply to personal label creation.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("labels", "create", "Create a personal label. Premium limits apply to personal label creation.", "/api/v1/labels"),
	},
	{
		ID:             "labels.delete",
		Method:         "DELETE",
		Path:           "/api/v1/labels/{label_id}",
		Summary:        "Deletes a personal label. All instances of the label will be removed from tasks",
		Positional:     []string{"label_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("labels", "delete", "Deletes a personal label. All instances of the label will be removed from tasks", "/api/v1/labels/{label_id}"),
	},
	{
		ID:             "labels.get",
		Method:         "GET",
		Path:           "/api/v1/labels",
		Summary:        "Get all user labels. This is a paginated endpoint.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("labels", "get", "Get all user labels. This is a paginated endpoint.", "/api/v1/labels"),
	},
	{
		ID:             "labels.get-labelid",
		Method:         "GET",
		Path:           "/api/v1/labels/{label_id}",
		Summary:        "Return a personal label by ID. Returns `NOT_FOUND` when the label does not exist or the ID is invalid.",
		Positional:     []string{"label_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("labels", "get-labelid", "Return a personal label by ID. Returns `NOT_FOUND` when the label does not exist or the ID is invalid.", "/api/v1/labels/{label_id}"),
	},
	{
		ID:             "labels.search",
		Method:         "GET",
		Path:           "/api/v1/labels/search",
		Summary:        "Search user labels by name. This is a paginated endpoint.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "query", WireName: "query"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("labels", "search", "Search user labels by name. This is a paginated endpoint.", "/api/v1/labels/search"),
	},
	{
		ID:             "labels.shared",
		Method:         "GET",
		Path:           "/api/v1/labels/shared",
		Summary:        "Returns a set of unique strings containing [shared labels](https://www.todoist.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "omit_personal", WireName: "omit_personal"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("labels", "shared", "Returns a set of unique strings containing [shared labels](https://www.todoist.", "/api/v1/labels/shared"),
	},
	{
		ID:             "labels.shared-remove",
		Method:         "POST",
		Path:           "/api/v1/labels/shared/remove",
		Summary:        "Remove the given shared label from all active tasks",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("labels", "shared-remove", "Remove the given shared label from all active tasks", "/api/v1/labels/shared/remove"),
	},
	{
		ID:             "labels.shared-rename",
		Method:         "POST",
		Path:           "/api/v1/labels/shared/rename",
		Summary:        "Rename the given shared label from all active tasks",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("labels", "shared-rename", "Rename the given shared label from all active tasks", "/api/v1/labels/shared/rename"),
	},
	{
		ID:             "labels.update",
		Method:         "POST",
		Path:           "/api/v1/labels/{label_id}",
		Summary:        "Update a personal label. At least one mutable field must be provided.",
		Positional:     []string{"label_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("labels", "update", "Update a personal label. At least one mutable field must be provided.", "/api/v1/labels/{label_id}"),
	},
	{
		ID:             "location-reminders.create",
		Method:         "POST",
		Path:           "/api/v1/location_reminders",
		Summary:        "Create a new location reminder for a task.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("location-reminders", "create", "Create a new location reminder for a task.", "/api/v1/location_reminders"),
	},
	{
		ID:             "location-reminders.delete",
		Method:         "DELETE",
		Path:           "/api/v1/location_reminders/{reminder_id}",
		Summary:        "Delete a location reminder by ID.",
		Positional:     []string{"reminder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("location-reminders", "delete", "Delete a location reminder by ID.", "/api/v1/location_reminders/{reminder_id}"),
	},
	{
		ID:             "location-reminders.get",
		Method:         "GET",
		Path:           "/api/v1/location_reminders",
		Summary:        "Get all active location reminders. Optionally filter by `task_id` to return only location reminders for a specific task.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "task_id", WireName: "task_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("location-reminders", "get", "Get all active location reminders. Optionally filter by `task_id` to return only location reminders for a specific task.", "/api/v1/location_reminders"),
	},
	{
		ID:             "location-reminders.get-locationreminders",
		Method:         "GET",
		Path:           "/api/v1/location_reminders/{reminder_id}",
		Summary:        "Return a single location reminder by ID.",
		Positional:     []string{"reminder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("location-reminders", "get-locationreminders", "Return a single location reminder by ID.", "/api/v1/location_reminders/{reminder_id}"),
	},
	{
		ID:             "location-reminders.update",
		Method:         "POST",
		Path:           "/api/v1/location_reminders/{reminder_id}",
		Summary:        "Update an existing location reminder.",
		Positional:     []string{"reminder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("location-reminders", "update", "Update an existing location reminder.", "/api/v1/location_reminders/{reminder_id}"),
	},
	{
		ID:             "notification-setting.update",
		Method:         "PUT",
		Path:           "/api/v1/notification_setting",
		Summary:        "Update a notification delivery preference.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("notification-setting", "update", "Update a notification delivery preference.", "/api/v1/notification_setting"),
	},
	{
		ID:             "payments.cancel-plan-with-redirect-to-stripe",
		Method:         "POST",
		Path:           "/api/v1/payments/cancel_plan_with_redirect_to_stripe",
		Summary:        "Start a hosted cancellation flow and return the redirect URL.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("payments", "cancel-plan-with-redirect-to-stripe", "Start a hosted cancellation flow and return the redirect URL.", "/api/v1/payments/cancel_plan_with_redirect_to_stripe"),
	},
	{
		ID:             "payments.get-subscription-info",
		Method:         "POST",
		Path:           "/api/v1/payments/get_subscription_info",
		Summary:        "Return the current user's subscription state.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("payments", "get-subscription-info", "Return the current user's subscription state.", "/api/v1/payments/get_subscription_info"),
	},
	{
		ID:             "payments.reactivate-plan",
		Method:         "POST",
		Path:           "/api/v1/payments/reactivate_plan",
		Summary:        "Reactivate a previously canceled subscription.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("payments", "reactivate-plan", "Reactivate a previously canceled subscription.", "/api/v1/payments/reactivate_plan"),
	},
	{
		ID:             "projects.create",
		Method:         "POST",
		Path:           "/api/v1/projects",
		Summary:        "Creates a new project and returns it",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("projects", "create", "Creates a new project and returns it", "/api/v1/projects"),
	},
	{
		ID:             "projects.delete",
		Method:         "DELETE",
		Path:           "/api/v1/projects/{project_id}",
		Summary:        "Deletes a project and all of its sections and tasks.",
		Positional:     []string{"project_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("projects", "delete", "Deletes a project and all of its sections and tasks.", "/api/v1/projects/{project_id}"),
	},
	{
		ID:             "projects.get",
		Method:         "GET",
		Path:           "/api/v1/projects",
		Summary:        "Get all active user projects, optionally filtered by folder or workspace. This is a paginated endpoint.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "folder_id", WireName: "folder_id"}, {PublicName: "workspace_id", WireName: "workspace_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("projects", "get", "Get all active user projects, optionally filtered by folder or workspace. This is a paginated endpoint.", "/api/v1/projects"),
	},
	{
		ID:             "projects.get-archived",
		Method:         "GET",
		Path:           "/api/v1/projects/archived",
		Summary:        "Get the user's archived projects.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("projects", "get-archived", "Get the user's archived projects.", "/api/v1/projects/archived"),
	},
	{
		ID:             "projects.get-projectid",
		Method:         "GET",
		Path:           "/api/v1/projects/{project_id}",
		Summary:        "Returns a project object related to the given ID",
		Positional:     []string{"project_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("projects", "get-projectid", "Returns a project object related to the given ID", "/api/v1/projects/{project_id}"),
	},
	{
		ID:             "projects.permissions",
		Method:         "GET",
		Path:           "/api/v1/projects/permissions",
		Summary:        "Returns a list of all the available roles and the associated actions they can perform in a project.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("projects", "permissions", "Returns a list of all the available roles and the associated actions they can perform in a project.", "/api/v1/projects/permissions"),
	},
	{
		ID:             "projects.search",
		Method:         "GET",
		Path:           "/api/v1/projects/search",
		Summary:        "Search active user projects by name. This is a paginated endpoint.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "query", WireName: "query"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("projects", "search", "Search active user projects by name. This is a paginated endpoint.", "/api/v1/projects/search"),
	},
	{
		ID:             "projects.update",
		Method:         "POST",
		Path:           "/api/v1/projects/{project_id}",
		Summary:        "Updates a project and returns it.",
		Positional:     []string{"project_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("projects", "update", "Updates a project and returns it.", "/api/v1/projects/{project_id}"),
	},
	{
		ID:             "projects.archive.project",
		Method:         "POST",
		Path:           "/api/v1/projects/{project_id}/archive",
		Summary:        "Marks a project as archived.",
		Positional:     []string{"project_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("projects", "project", "Marks a project as archived.", "/api/v1/projects/{project_id}/archive"),
	},
	{
		ID:             "projects.collaborators.get-project",
		Method:         "GET",
		Path:           "/api/v1/projects/{project_id}/collaborators",
		Summary:        "Get all collaborators for a given project. This is a paginated endpoint.",
		Positional:     []string{"project_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}, {PublicName: "public_key", WireName: "public_key"}},
		keywords:       codeOrchKeywords("projects", "get-project", "Get all collaborators for a given project. This is a paginated endpoint.", "/api/v1/projects/{project_id}/collaborators"),
	},
	{
		ID:             "projects.join.join",
		Method:         "POST",
		Path:           "/api/v1/projects/{project_id}/join",
		Summary:        "_Only used for workspaces_ This endpoint is used to join a workspace project by a workspace_user and is only usable by",
		Positional:     []string{"project_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("projects", "join", "_Only used for workspaces_ This endpoint is used to join a workspace project by a workspace_user and is only usable by", "/api/v1/projects/{project_id}/join"),
	},
	{
		ID:             "projects.unarchive.project",
		Method:         "POST",
		Path:           "/api/v1/projects/{project_id}/unarchive",
		Summary:        "Marks a previously archived project as active again.",
		Positional:     []string{"project_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("projects", "project", "Marks a previously archived project as active again.", "/api/v1/projects/{project_id}/unarchive"),
	},
	{
		ID:             "reminders.create",
		Method:         "POST",
		Path:           "/api/v1/reminders",
		Summary:        "Create a new reminder for a task.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("reminders", "create", "Create a new reminder for a task.", "/api/v1/reminders"),
	},
	{
		ID:             "reminders.delete",
		Method:         "DELETE",
		Path:           "/api/v1/reminders/{reminder_id}",
		Summary:        "Delete a reminder by ID.",
		Positional:     []string{"reminder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("reminders", "delete", "Delete a reminder by ID.", "/api/v1/reminders/{reminder_id}"),
	},
	{
		ID:             "reminders.get",
		Method:         "GET",
		Path:           "/api/v1/reminders",
		Summary:        "Get all active reminders. Optionally filter by `task_id` to return only reminders for a specific task.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "task_id", WireName: "task_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("reminders", "get", "Get all active reminders. Optionally filter by `task_id` to return only reminders for a specific task.", "/api/v1/reminders"),
	},
	{
		ID:             "reminders.get-reminderid",
		Method:         "GET",
		Path:           "/api/v1/reminders/{reminder_id}",
		Summary:        "Return a single reminder by ID.",
		Positional:     []string{"reminder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("reminders", "get-reminderid", "Return a single reminder by ID.", "/api/v1/reminders/{reminder_id}"),
	},
	{
		ID:             "reminders.update",
		Method:         "POST",
		Path:           "/api/v1/reminders/{reminder_id}",
		Summary:        "Update an existing reminder.",
		Positional:     []string{"reminder_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("reminders", "update", "Update an existing reminder.", "/api/v1/reminders/{reminder_id}"),
	},
	{
		ID:             "revoke.token-rfc7009-compliant",
		Method:         "POST",
		Path:           "/api/v1/revoke",
		Summary:        "Revoke an access token according to RFC 7009 OAuth Token Revocation.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("revoke", "token-rfc7009-compliant", "Revoke an access token according to RFC 7009 OAuth Token Revocation.", "/api/v1/revoke"),
	},
	{
		ID:             "sections.create",
		Method:         "POST",
		Path:           "/api/v1/sections",
		Summary:        "Create a new section",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("sections", "create", "Create a new section", "/api/v1/sections"),
	},
	{
		ID:             "sections.delete",
		Method:         "DELETE",
		Path:           "/api/v1/sections/{section_id}",
		Summary:        "Delete the section and all of its tasks",
		Positional:     []string{"section_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("sections", "delete", "Delete the section and all of its tasks", "/api/v1/sections/{section_id}"),
	},
	{
		ID:             "sections.get",
		Method:         "GET",
		Path:           "/api/v1/sections",
		Summary:        "Get all active sections for the user, optionally filtered by project. This is a paginated endpoint.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "project_id", WireName: "project_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}, {PublicName: "public_key", WireName: "public_key"}},
		keywords:       codeOrchKeywords("sections", "get", "Get all active sections for the user, optionally filtered by project. This is a paginated endpoint.", "/api/v1/sections"),
	},
	{
		ID:             "sections.get-sectionid",
		Method:         "GET",
		Path:           "/api/v1/sections/{section_id}",
		Summary:        "Return the section for the given section ID",
		Positional:     []string{"section_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "public_key", WireName: "public_key"}},
		keywords:       codeOrchKeywords("sections", "get-sectionid", "Return the section for the given section ID", "/api/v1/sections/{section_id}"),
	},
	{
		ID:             "sections.search",
		Method:         "GET",
		Path:           "/api/v1/sections/search",
		Summary:        "Search active sections by name, optionally filtered by project. This is a paginated endpoint.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "query", WireName: "query"}, {PublicName: "project_id", WireName: "project_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("sections", "search", "Search active sections by name, optionally filtered by project. This is a paginated endpoint.", "/api/v1/sections/search"),
	},
	{
		ID:             "sections.update",
		Method:         "POST",
		Path:           "/api/v1/sections/{section_id}",
		Summary:        "Update a section. Passing `null` for an optional field keeps the existing value unchanged.",
		Positional:     []string{"section_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("sections", "update", "Update a section. Passing `null` for an optional field keeps the existing value unchanged.", "/api/v1/sections/{section_id}"),
	},
	{
		ID:             "sections.archive.section",
		Method:         "POST",
		Path:           "/api/v1/sections/{section_id}/archive",
		Summary:        "Marks a section as archived.",
		Positional:     []string{"section_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("sections", "section", "Marks a section as archived.", "/api/v1/sections/{section_id}/archive"),
	},
	{
		ID:             "sections.unarchive.section",
		Method:         "POST",
		Path:           "/api/v1/sections/{section_id}/unarchive",
		Summary:        "Marks a section as active again.",
		Positional:     []string{"section_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("sections", "section", "Marks a section as active again.", "/api/v1/sections/{section_id}/unarchive"),
	},
	{
		ID:             "tasks.completed-by-completion-date",
		Method:         "GET",
		Path:           "/api/v1/tasks/completed/by_completion_date",
		Summary:        "Retrieves a list of completed tasks strictly limited by the specified completion date range (up to 3 months).",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "since", WireName: "since"}, {PublicName: "until", WireName: "until"}, {PublicName: "workspace_id", WireName: "workspace_id"}, {PublicName: "project_id", WireName: "project_id"}, {PublicName: "section_id", WireName: "section_id"}, {PublicName: "parent_id", WireName: "parent_id"}, {PublicName: "filter_query", WireName: "filter_query"}, {PublicName: "filter_lang", WireName: "filter_lang"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}, {PublicName: "public_key", WireName: "public_key"}},
		keywords:       codeOrchKeywords("tasks", "completed-by-completion-date", "Retrieves a list of completed tasks strictly limited by the specified completion date range (up to 3 months).", "/api/v1/tasks/completed/by_completion_date"),
	},
	{
		ID:             "tasks.completed-by-due-date",
		Method:         "GET",
		Path:           "/api/v1/tasks/completed/by_due_date",
		Summary:        "Retrieves a list of completed items strictly limited by the specified due date range (up to 6 weeks).",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "since", WireName: "since"}, {PublicName: "until", WireName: "until"}, {PublicName: "workspace_id", WireName: "workspace_id"}, {PublicName: "project_id", WireName: "project_id"}, {PublicName: "section_id", WireName: "section_id"}, {PublicName: "parent_id", WireName: "parent_id"}, {PublicName: "filter_query", WireName: "filter_query"}, {PublicName: "filter_lang", WireName: "filter_lang"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("tasks", "completed-by-due-date", "Retrieves a list of completed items strictly limited by the specified due date range (up to 6 weeks).", "/api/v1/tasks/completed/by_due_date"),
	},
	{
		ID:             "tasks.create",
		Method:         "POST",
		Path:           "/api/v1/tasks",
		Summary:        "Create a new task.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("tasks", "create", "Create a new task.", "/api/v1/tasks"),
	},
	{
		ID:             "tasks.delete",
		Method:         "DELETE",
		Path:           "/api/v1/tasks/{task_id}",
		Summary:        "Delete a task and all of its subtasks.",
		Positional:     []string{"task_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("tasks", "delete", "Delete a task and all of its subtasks.", "/api/v1/tasks/{task_id}"),
	},
	{
		ID:             "tasks.get",
		Method:         "GET",
		Path:           "/api/v1/tasks",
		Summary:        "Get all active tasks for the user. All provided parameters are used to narrow down the list of tasks.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "project_id", WireName: "project_id"}, {PublicName: "section_id", WireName: "section_id"}, {PublicName: "parent_id", WireName: "parent_id"}, {PublicName: "label", WireName: "label"}, {PublicName: "ids", WireName: "ids"}, {PublicName: "goal_id", WireName: "goal_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("tasks", "get", "Get all active tasks for the user. All provided parameters are used to narrow down the list of tasks.", "/api/v1/tasks"),
	},
	{
		ID:             "tasks.get-by-filter",
		Method:         "GET",
		Path:           "/api/v1/tasks/filter",
		Summary:        "Get all tasks matching the filter. This is a paginated endpoint.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "query", WireName: "query"}, {PublicName: "lang", WireName: "lang"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("tasks", "get-by-filter", "Get all tasks matching the filter. This is a paginated endpoint.", "/api/v1/tasks/filter"),
	},
	{
		ID:             "tasks.get-productivity-stats",
		Method:         "GET",
		Path:           "/api/v1/tasks/completed/stats",
		Summary:        "Get comprehensive productivity statistics for the authenticated user.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("tasks", "get-productivity-stats", "Get comprehensive productivity statistics for the authenticated user.", "/api/v1/tasks/completed/stats"),
	},
	{
		ID:             "tasks.get-taskid",
		Method:         "GET",
		Path:           "/api/v1/tasks/{task_id}",
		Summary:        "Returns a single active (non-completed) task by ID",
		Positional:     []string{"task_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "public_key", WireName: "public_key"}},
		keywords:       codeOrchKeywords("tasks", "get-taskid", "Returns a single active (non-completed) task by ID", "/api/v1/tasks/{task_id}"),
	},
	{
		ID:             "tasks.quick-add",
		Method:         "POST",
		Path:           "/api/v1/tasks/quick",
		Summary:        "Add a new task using Quick Add with natural language processing.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("tasks", "quick-add", "Add a new task using Quick Add with natural language processing.", "/api/v1/tasks/quick"),
	},
	{
		ID:             "tasks.update",
		Method:         "POST",
		Path:           "/api/v1/tasks/{task_id}",
		Summary:        "Updates an existing task.",
		Positional:     []string{"task_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("tasks", "update", "Updates an existing task.", "/api/v1/tasks/{task_id}"),
	},
	{
		ID:             "tasks.close.task",
		Method:         "POST",
		Path:           "/api/v1/tasks/{task_id}/close",
		Summary:        "Closes a task.",
		Positional:     []string{"task_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("tasks", "task", "Closes a task.", "/api/v1/tasks/{task_id}/close"),
	},
	{
		ID:             "tasks.move.task",
		Method:         "POST",
		Path:           "/api/v1/tasks/{task_id}/move",
		Summary:        "Moves task to another project, section or parent.",
		Positional:     []string{"task_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("tasks", "task", "Moves task to another project, section or parent.", "/api/v1/tasks/{task_id}/move"),
	},
	{
		ID:             "tasks.reopen.task",
		Method:         "POST",
		Path:           "/api/v1/tasks/{task_id}/reopen",
		Summary:        "Reopens a task. Any ancestor tasks or sections will also be marked as uncomplete and restored from history.",
		Positional:     []string{"task_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("tasks", "task", "Reopens a task. Any ancestor tasks or sections will also be marked as uncomplete and restored from history.", "/api/v1/tasks/{task_id}/reopen"),
	},
	{
		ID:             "templates.create",
		Method:         "POST",
		Path:           "/api/v1/templates/create_project_from_file",
		Summary:        "A template can be imported in an existing project, or in a newly created one.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("templates", "create", "A template can be imported in an existing project, or in a newly created one.", "/api/v1/templates/create_project_from_file"),
	},
	{
		ID:             "templates.export-as-file",
		Method:         "GET",
		Path:           "/api/v1/templates/file",
		Summary:        "Get a template for a project as a CSV file",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "project_id", WireName: "project_id"}, {PublicName: "use_relative_dates", WireName: "use_relative_dates"}},
		keywords:       codeOrchKeywords("templates", "export-as-file", "Get a template for a project as a CSV file", "/api/v1/templates/file"),
	},
	{
		ID:             "templates.export-as-url",
		Method:         "GET",
		Path:           "/api/v1/templates/url",
		Summary:        "Get a template for a project as a shareable URL. The URL can then be passed to `https://todoist.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "project_id", WireName: "project_id"}, {PublicName: "use_relative_dates", WireName: "use_relative_dates"}},
		keywords:       codeOrchKeywords("templates", "export-as-url", "Get a template for a project as a shareable URL. The URL can then be passed to `https://todoist.", "/api/v1/templates/url"),
	},
	{
		ID:             "templates.import-into-project-from-file",
		Method:         "POST",
		Path:           "/api/v1/templates/import_into_project_from_file",
		Summary:        "A template can be imported in an existing project, or in a newly created one.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("templates", "import-into-project-from-file", "A template can be imported in an existing project, or in a newly created one.", "/api/v1/templates/import_into_project_from_file"),
	},
	{
		ID:             "templates.import-into-project-from-id",
		Method:         "POST",
		Path:           "/api/v1/templates/import_into_project_from_template_id",
		Summary:        "Import a saved template into an existing project. The target project must exist and must not be frozen.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("templates", "import-into-project-from-id", "Import a saved template into an existing project. The target project must exist and must not be frozen.", "/api/v1/templates/import_into_project_from_template_id"),
	},
	{
		ID:             "uploads.delete",
		Method:         "DELETE",
		Path:           "/api/v1/uploads",
		Summary:        "Delete an uploaded file. The file must belong to the authenticated user.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "file_url", WireName: "file_url"}},
		keywords:       codeOrchKeywords("uploads", "delete", "Delete an uploaded file. The file must belong to the authenticated user.", "/api/v1/uploads"),
	},
	{
		ID:             "uploads.file",
		Method:         "POST",
		Path:           "/api/v1/uploads",
		Summary:        "Upload a file to Todoist. This endpoint accepts file uploads via two methods: 1.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("uploads", "file", "Upload a file to Todoist. This endpoint accepts file uploads via two methods: 1.", "/api/v1/uploads"),
	},
	{
		ID:             "user.info",
		Method:         "GET",
		Path:           "/api/v1/user",
		Summary:        "Get information about the currently authenticated user.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("user", "info", "Get information about the currently authenticated user.", "/api/v1/user"),
	},
	{
		ID:             "workspaces.accept-invitation",
		Method:         "PUT",
		Path:           "/api/v1/workspaces/invitations/{invite_code}/accept",
		Summary:        "Accept a workspace invitation. Usable by authenticated users only.",
		Positional:     []string{"invite_code"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "accept-invitation", "Accept a workspace invitation. Usable by authenticated users only.", "/api/v1/workspaces/invitations/{invite_code}/accept"),
	},
	{
		ID:             "workspaces.all-invitations",
		Method:         "GET",
		Path:           "/api/v1/workspaces/invitations/all",
		Summary:        "Return a list containing details of all pending invitation to a workspace. This list is not paginated.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "workspace_id", WireName: "workspace_id"}},
		keywords:       codeOrchKeywords("workspaces", "all-invitations", "Return a list containing details of all pending invitation to a workspace. This list is not paginated.", "/api/v1/workspaces/invitations/all"),
	},
	{
		ID:             "workspaces.create",
		Method:         "POST",
		Path:           "/api/v1/workspaces",
		Summary:        "Creates a new workspace and returns it.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "create", "Creates a new workspace and returns it.", "/api/v1/workspaces"),
	},
	{
		ID:             "workspaces.delete",
		Method:         "DELETE",
		Path:           "/api/v1/workspaces/{workspace_id}",
		Summary:        "Deletes a workspace.",
		Positional:     []string{"workspace_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "delete", "Deletes a workspace.", "/api/v1/workspaces/{workspace_id}"),
	},
	{
		ID:             "workspaces.delete-invitation",
		Method:         "POST",
		Path:           "/api/v1/workspaces/invitations/delete",
		Summary:        "Deletes a workspace invitation. Only admins can delete invitations.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "delete-invitation", "Deletes a workspace invitation. Only admins can delete invitations.", "/api/v1/workspaces/invitations/delete"),
	},
	{
		ID:             "workspaces.get",
		Method:         "GET",
		Path:           "/api/v1/workspaces",
		Summary:        "Returns all workspaces where the user is a member.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "get", "Returns all workspaces where the user is a member.", "/api/v1/workspaces"),
	},
	{
		ID:             "workspaces.get-users",
		Method:         "GET",
		Path:           "/api/v1/workspaces/users",
		Summary:        "Returns all workspace_users for a given workspace if workspace_id is provided.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "workspace_id", WireName: "workspace_id"}, {PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("workspaces", "get-users", "Returns all workspace_users for a given workspace if workspace_id is provided.", "/api/v1/workspaces/users"),
	},
	{
		ID:             "workspaces.get-workspaceid",
		Method:         "GET",
		Path:           "/api/v1/workspaces/{workspace_id}",
		Summary:        "Returns a workspace by ID.",
		Positional:     []string{"workspace_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "get-workspaceid", "Returns a workspace by ID.", "/api/v1/workspaces/{workspace_id}"),
	},
	{
		ID:             "workspaces.invitations",
		Method:         "GET",
		Path:           "/api/v1/workspaces/invitations",
		Summary:        "Return a list of user emails who have a pending invitation to a workspace. The list is not paginated.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "workspace_id", WireName: "workspace_id"}},
		keywords:       codeOrchKeywords("workspaces", "invitations", "Return a list of user emails who have a pending invitation to a workspace. The list is not paginated.", "/api/v1/workspaces/invitations"),
	},
	{
		ID:             "workspaces.join",
		Method:         "POST",
		Path:           "/api/v1/workspaces/join",
		Summary:        "Join a workspace via link or via workspace ID, if the user can auto-join the workspace by domain.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "join", "Join a workspace via link or via workspace ID, if the user can auto-join the workspace by domain.", "/api/v1/workspaces/join"),
	},
	{
		ID:             "workspaces.plan-details",
		Method:         "GET",
		Path:           "/api/v1/workspaces/plan_details",
		Summary:        "Lists details of the workspace's current plan and usage",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "workspace_id", WireName: "workspace_id"}},
		keywords:       codeOrchKeywords("workspaces", "plan-details", "Lists details of the workspace's current plan and usage", "/api/v1/workspaces/plan_details"),
	},
	{
		ID:             "workspaces.reject-invitation",
		Method:         "PUT",
		Path:           "/api/v1/workspaces/invitations/{invite_code}/reject",
		Summary:        "Reject a workspace invitation. Usable by authenticated users only.",
		Positional:     []string{"invite_code"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "reject-invitation", "Reject a workspace invitation. Usable by authenticated users only.", "/api/v1/workspaces/invitations/{invite_code}/reject"),
	},
	{
		ID:             "workspaces.update",
		Method:         "POST",
		Path:           "/api/v1/workspaces/{workspace_id}",
		Summary:        "Updates an existing workspace and returns it.",
		Positional:     []string{"workspace_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "update", "Updates an existing workspace and returns it.", "/api/v1/workspaces/{workspace_id}"),
	},
	{
		ID:             "workspaces.update-logo",
		Method:         "POST",
		Path:           "/api/v1/workspaces/logo",
		Summary:        "Upload an image to be used as the workspace logo. Similar to a user’s avatar.",
		Positional:     []string{},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "update-logo", "Upload an image to be used as the workspace logo. Similar to a user’s avatar.", "/api/v1/workspaces/logo"),
	},
	{
		ID:             "workspaces.projects.active",
		Method:         "GET",
		Path:           "/api/v1/workspaces/{workspace_id}/projects/active",
		Summary:        "Returns all active workspace projects, including those visible but not joined by the user.",
		Positional:     []string{"workspace_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("workspaces", "active", "Returns all active workspace projects, including those visible but not joined by the user.", "/api/v1/workspaces/{workspace_id}/projects/active"),
	},
	{
		ID:             "workspaces.projects.archived",
		Method:         "GET",
		Path:           "/api/v1/workspaces/{workspace_id}/projects/archived",
		Summary:        "Return archived projects in a workspace. Workspace guests cannot list archived projects and receive a `FORBIDDEN` error.",
		Positional:     []string{"workspace_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{{PublicName: "cursor", WireName: "cursor"}, {PublicName: "limit", WireName: "limit"}},
		keywords:       codeOrchKeywords("workspaces", "archived", "Return archived projects in a workspace. Workspace guests cannot list archived projects and receive a `FORBIDDEN` error.", "/api/v1/workspaces/{workspace_id}/projects/archived"),
	},
	{
		ID:             "workspaces.users.invite-workspace",
		Method:         "POST",
		Path:           "/api/v1/workspaces/{workspace_id}/users/invite",
		Summary:        "Invites users to a workspace by email.",
		Positional:     []string{"workspace_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "invite-workspace", "Invites users to a workspace by email.", "/api/v1/workspaces/{workspace_id}/users/invite"),
	},
	{
		ID:             "workspaces.users.remove-workspace",
		Method:         "DELETE",
		Path:           "/api/v1/workspaces/{workspace_id}/users/{user_id}",
		Summary:        "Removes a user from a workspace.",
		Positional:     []string{"workspace_id", "user_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "remove-workspace", "Removes a user from a workspace.", "/api/v1/workspaces/{workspace_id}/users/{user_id}"),
	},
	{
		ID:             "workspaces.users.update-workspace",
		Method:         "POST",
		Path:           "/api/v1/workspaces/{workspace_id}/users/{user_id}",
		Summary:        "Updates a workspace user's role.",
		Positional:     []string{"workspace_id", "user_id"},
		TemplateParams: []codeOrchParamBinding{},
		QueryParams:    []codeOrchParamBinding{},
		keywords:       codeOrchKeywords("workspaces", "update-workspace", "Updates a workspace user's role.", "/api/v1/workspaces/{workspace_id}/users/{user_id}"),
	},
}

// codeOrchStopwords filters two-letter and short common-word substrings
// that pollute ranking via the substring-contains rule. Without them, a
// search for "list links" matches every endpoint whose description
// contains "is enrolled" because "is" is two chars and the matcher
// accepts kw.contains(t) || t.contains(kw).
var codeOrchStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true,
	"at": true, "be": true, "by": true, "for": true, "from": true,
	"has": true, "in": true, "is": true, "it": true, "its": true,
	"of": true, "on": true, "or": true, "that": true, "the": true,
	"this": true, "to": true, "was": true, "will": true, "with": true,
	"your": true, "you": true, "any": true, "all": true,
}

// codeOrchKeywords produces the lowercase token stream used for search
// ranking. Defined at package level so the registry initializer can call it
// inline above without pulling in a separate precompute step.
func codeOrchKeywords(resource, endpoint, summary, path string) []string {
	raw := strings.ToLower(resource + " " + endpoint + " " + summary + " " + path)
	raw = strings.Map(func(r rune) rune {
		switch r {
		case '_', '-', '/', '{', '}', '.', ',', ':', ';':
			return ' '
		}
		return r
	}, raw)
	out := make([]string, 0, 16)
	seen := map[string]bool{}
	for _, tok := range strings.Fields(raw) {
		if len(tok) < 3 || codeOrchStopwords[tok] || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

func handleCodeOrchSearch(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return mcplib.NewToolResultError("query is required"), nil
	}
	limit := 10
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	terms := codeOrchKeywords("", "", query, "")
	type scored struct {
		ep    *codeOrchEndpoint
		score int
	}
	results := make([]scored, 0, len(codeOrchEndpoints))
	for i := range codeOrchEndpoints {
		ep := &codeOrchEndpoints[i]
		score := 0
		for _, t := range terms {
			for _, kw := range ep.keywords {
				if kw == t {
					score += 2
				} else if strings.Contains(kw, t) || strings.Contains(t, kw) {
					score++
				}
			}
		}
		if score > 0 {
			results = append(results, scored{ep: ep, score: score})
		}
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].score > results[j].score })
	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]any{
			"endpoint_id": r.ep.ID,
			"method":      r.ep.Method,
			"path":        r.ep.Path,
			"summary":     r.ep.Summary,
			"score":       r.score,
		})
	}
	data, _ := json.Marshal(map[string]any{"count": len(out), "results": out})
	return mcplib.NewToolResultText(string(data)), nil
}

func handleCodeOrchExecute(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	args := req.GetArguments()
	id, ok := args["endpoint_id"].(string)
	if !ok || id == "" {
		return mcplib.NewToolResultError("endpoint_id is required (call todoist_search first)"), nil
	}

	var ep *codeOrchEndpoint
	for i := range codeOrchEndpoints {
		if codeOrchEndpoints[i].ID == id {
			ep = &codeOrchEndpoints[i]
			break
		}
	}
	if ep == nil {
		return mcplib.NewToolResultError(fmt.Sprintf("unknown endpoint_id %q — call todoist_search to discover valid ids", id)), nil
	}

	params, _ := args["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}

	c, err := newMCPClient()
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}

	path := ep.Path
	for _, p := range ep.Positional {
		if v, ok := params[p]; ok {
			path = strings.ReplaceAll(path, "{"+p+"}", fmt.Sprintf("%v", v))
			delete(params, p)
		}
	}

	// Route params to their runtime slots. GET/DELETE params are query
	// strings; write methods split spec-declared query params from the
	// remaining params used as the request body below.
	query := map[string]string{}
	if ep.Method == "GET" || ep.Method == "DELETE" {
		for k, v := range params {
			query[codeOrchWireQueryName(ep.QueryParams, k)] = fmt.Sprintf("%v", v)
		}
	} else {
		// Route spec-declared in:query params to the query string for write
		// methods too. Without this, a query param (e.g. sendToLedger on
		// PUT /ledger/voucher/{id}) wrongly lands in the JSON body and the
		// API silently ignores it or rejects the request. The remaining
		// params stay in the map for codeOrchWriteBody (the JSON body).
		if enc := codeOrchSplitQuery(ep.QueryParams, params); enc != "" {
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			path += sep + enc
		}
	}

	hdrs := ep.HeaderOverrides
	writeBody := func() any {
		if ep.BodyIsArray {
			return codeOrchArrayBody(params)
		}
		return codeOrchWriteBody(params)
	}
	var data json.RawMessage
	switch ep.Method {
	case "GET":
		if len(hdrs) > 0 {
			data, err = c.GetWithHeaders(ctx, path, query, hdrs)
		} else {
			data, err = c.Get(ctx, path, query)
		}
	case "DELETE":
		if len(hdrs) > 0 {
			data, _, err = c.DeleteWithParamsAndHeaders(ctx, path, query, hdrs)
		} else {
			data, _, err = c.DeleteWithParams(ctx, path, query)
		}
	case "POST":
		body := writeBody()
		if len(hdrs) > 0 {
			data, _, err = c.PostWithHeaders(ctx, path, body, hdrs)
		} else {
			data, _, err = c.Post(ctx, path, body)
		}
	case "PUT":
		body := writeBody()
		if len(hdrs) > 0 {
			data, _, err = c.PutWithHeaders(ctx, path, body, hdrs)
		} else {
			data, _, err = c.Put(ctx, path, body)
		}
	case "PATCH":
		body := writeBody()
		if len(hdrs) > 0 {
			data, _, err = c.PatchWithHeaders(ctx, path, body, hdrs)
		} else {
			data, _, err = c.Patch(ctx, path, body)
		}
	default:
		return mcplib.NewToolResultError(fmt.Sprintf("unsupported method %q", ep.Method)), nil
	}
	if err != nil {
		return mcplib.NewToolResultError(err.Error()), nil
	}
	return mcplib.NewToolResultText(string(data)), nil
}

// codeOrchWriteBody returns the value handed to the client layer as the
// request body for write methods (POST/PUT/PATCH). It MUST be the structured
// params map, never pre-marshaled bytes.
//
// client.do() marshals the body value exactly once. Handing it []byte makes
// json.Marshal([]byte) emit a base64-encoded JSON *string*, so the API
// receives "eyJ...==" where it expects the request object. Strict JSON APIs
// reject that as the wrong type at the body root. GET/DELETE carry no body,
// so this defect stays latent until the first write attempt.
func codeOrchWriteBody(params map[string]any) any {
	return params
}

// codeOrchArrayBody returns the request body for endpoints whose schema root
// is a bare top-level JSON array (ep.BodyIsArray). Such a body cannot be
// expressed as the params object, so the agent supplies the array under the
// conventional params key "body" (these endpoints have no flattened named
// params, so there is no collision risk). When present and array-shaped it is
// sent as the top-level array the API expects. Otherwise params is returned
// unchanged so a malformed call fails loudly at the API (HTTP 422) instead of
// silently sending the wrong shape or a partial write — financial mutations
// are never retried with body-variant guesses (broken-convenience guardrail).
func codeOrchArrayBody(params map[string]any) any {
	if v, ok := params["body"]; ok {
		if arr, ok := v.([]any); ok {
			return arr
		}
	}
	return params
}

// codeOrchSplitQuery removes spec-declared in:query params from params and
// returns them URL-encoded for appending to the request path. The remaining
// entries stay in the map for codeOrchWriteBody (the JSON body), so a write
// method's query parameters never get buried in the body. Mutates params by
// design (deletes the consumed query keys).
func codeOrchSplitQuery(queryParams []codeOrchParamBinding, params map[string]any) string {
	uv := neturl.Values{}
	for _, q := range queryParams {
		for _, key := range []string{q.PublicName, q.WireName} {
			if key == "" {
				continue
			}
			if v, ok := params[key]; ok {
				uv.Set(q.WireName, fmt.Sprintf("%v", v))
				delete(params, key)
				break
			}
		}
	}
	return uv.Encode()
}

func codeOrchWireQueryName(queryParams []codeOrchParamBinding, name string) string {
	for _, q := range queryParams {
		if q.PublicName == name || q.WireName == name {
			return q.WireName
		}
	}
	return name
}
