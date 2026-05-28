// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source live

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type filterBatchPlanEntry struct {
	TaskID  string         `json:"task_id"`
	Content string         `json:"content"`
	Action  string         `json:"action"`
	Before  map[string]any `json:"before,omitempty"`
	After   map[string]any `json:"after,omitempty"`
}

type filterBatchEnvelope struct {
	Plan     []filterBatchPlanEntry `json:"plan"`
	Applied  int                    `json:"applied"`
	Failed   int                    `json:"failed"`
	Failures []string               `json:"failures,omitempty"`
}

func newNovelFilterBatchCmd(flags *rootFlags) *cobra.Command {
	var (
		flagFilter string
		flagAction string
		flagTarget string
		flagLabels string
		flagApply  string
	)

	cmd := &cobra.Command{
		Use:   "filter-batch",
		Short: "Plan or apply a bulk action over Todoist-filter-matched tasks.",
		Long: `Bulk action over the result of a Todoist filter (e.g. "today | overdue"). In
default plan mode lists what would happen; use --apply <plan-file> to execute.

Actions:
  complete  — close tasks via /tasks/{id}/close
  move      — re-parent via /tasks/{id}/move ({project_id|section_id})
  relabel   — replace labels via /tasks/{id}
  delete    — DELETE /tasks/{id}`,
		Example: strings.Trim(`
  todoist-aum filter-batch --filter "today" --action complete --json > plan.json
  todoist-aum filter-batch --apply plan.json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "false", "pp:typed-exit-codes": "0,2,5,6"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagApply == "" && cmd.Flags().NFlag() == 0 && len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}

			if flagApply != "" {
				return filterBatchApply(cmd, flags, flagApply)
			}
			if flagFilter == "" || flagAction == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--filter and --action are required when not using --apply"))
			}
			switch flagAction {
			case "complete", "move", "relabel", "delete":
				// ok
			default:
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("invalid --action %q (expected complete, move, relabel, delete)", flagAction))
			}
			if flagAction == "move" && flagTarget == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--target is required for --action move"))
			}
			if flagAction == "relabel" && flagLabels == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("--labels is required for --action relabel"))
			}

			return filterBatchPlan(cmd, flags, flagFilter, flagAction, flagTarget, flagLabels)
		},
	}
	cmd.Flags().StringVar(&flagFilter, "filter", "", "Todoist filter query (e.g. \"today | overdue\")")
	cmd.Flags().StringVar(&flagAction, "action", "", "Action: complete, move, relabel, delete")
	cmd.Flags().StringVar(&flagTarget, "target", "", "Move target (project or section name, resolved from local store)")
	cmd.Flags().StringVar(&flagLabels, "labels", "", "Comma-separated label names (for --action relabel)")
	cmd.Flags().StringVar(&flagApply, "apply", "", "Apply a previously-emitted plan JSON/JSONL file")
	return cmd
}

func filterBatchPlan(cmd *cobra.Command, flags *rootFlags, filter, action, target, labels string) error {
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	data, err := c.Get(cmd.Context(), "/api/v1/tasks/filter", map[string]string{"query": filter})
	if err != nil {
		return classifyAPIError(err, flags)
	}

	tasks := extractFilterTasks(data)

	// Resolve --target (project or section) for move action against the local store.
	var moveProjectID, moveSectionID string
	if action == "move" {
		db, derr := openLocalStore(flags)
		if derr == nil {
			// Try project first.
			if pid, perr := resolveProjectIDByName(cmd.Context(), db, target); perr == nil && pid != "" {
				moveProjectID = pid
			}
			db.Close()
		}
		if moveProjectID == "" && moveSectionID == "" {
			// Fall back: treat --target as a literal id.
			moveProjectID = target
		}
	}

	var labelList []string
	if action == "relabel" {
		for _, s := range strings.Split(labels, ",") {
			if t := strings.TrimSpace(s); t != "" {
				labelList = append(labelList, t)
			}
		}
	}

	env := filterBatchEnvelope{Plan: []filterBatchPlanEntry{}}
	for _, t := range tasks {
		id, _ := t["id"].(string)
		if id == "" {
			continue
		}
		content, _ := t["content"].(string)
		entry := filterBatchPlanEntry{TaskID: id, Content: content, Action: action, Before: t}
		switch action {
		case "complete":
			entry.After = map[string]any{"checked": true}
		case "move":
			a := map[string]any{}
			if moveProjectID != "" {
				a["project_id"] = moveProjectID
			}
			if moveSectionID != "" {
				a["section_id"] = moveSectionID
			}
			entry.After = a
		case "relabel":
			entry.After = map[string]any{"labels": labelList}
		case "delete":
			entry.After = map[string]any{"deleted": true}
		}
		env.Plan = append(env.Plan, entry)
	}

	if flags.asJSON {
		return flags.printJSON(cmd, env)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "filter=%q action=%s matched=%d\n", filter, action, len(env.Plan))
	for _, p := range env.Plan {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  -> %s\n", p.Content, p.Action)
	}
	return nil
}

func filterBatchApply(cmd *cobra.Command, flags *rootFlags, planPath string) error {
	entries, err := readPlanEntries(planPath)
	if err != nil {
		return err
	}
	c, err := flags.newClient()
	if err != nil {
		return err
	}
	env := filterBatchEnvelope{}
	for _, raw := range entries {
		var e filterBatchPlanEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			env.Failed++
			env.Failures = append(env.Failures, fmt.Sprintf("parse: %v", err))
			continue
		}
		if e.TaskID == "" {
			env.Failed++
			env.Failures = append(env.Failures, "empty task_id")
			continue
		}
		var status int
		var apiErrLocal error
		switch e.Action {
		case "complete":
			_, status, apiErrLocal = c.Post(cmd.Context(), "/api/v1/tasks/"+e.TaskID+"/close", map[string]any{})
		case "move":
			body := map[string]any{}
			if v, ok := e.After["project_id"]; ok {
				body["project_id"] = v
			}
			if v, ok := e.After["section_id"]; ok {
				body["section_id"] = v
			}
			_, status, apiErrLocal = c.Post(cmd.Context(), "/api/v1/tasks/"+e.TaskID+"/move", body)
		case "relabel":
			body := map[string]any{"labels": e.After["labels"]}
			_, status, apiErrLocal = c.Post(cmd.Context(), "/api/v1/tasks/"+e.TaskID, body)
		case "delete":
			_, status, apiErrLocal = c.Delete(cmd.Context(), "/api/v1/tasks/"+e.TaskID)
		default:
			env.Failed++
			env.Failures = append(env.Failures, fmt.Sprintf("task %s: unknown action %q", e.TaskID, e.Action))
			continue
		}
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
		return partialFailureErr(fmt.Errorf("%d filter-batch apply(s) failed", env.Failed))
	}
	return nil
}

// extractFilterTasks unwraps the various shapes /api/v1/tasks/filter can return
// (bare array, {results: [...]}, {tasks: [...]}, etc.) into a list of task
// objects as map[string]any.
func extractFilterTasks(data json.RawMessage) []map[string]any {
	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	for _, k := range []string{"results", "data", "items", "tasks"} {
		if raw, ok := obj[k]; ok {
			var inner []map[string]any
			if err := json.Unmarshal(raw, &inner); err == nil {
				return inner
			}
		}
	}
	return nil
}

// readPlanEntries reads a plan file, accepting either an envelope ({"plan":[...]})
// or one JSONL entry per line.
func readPlanEntries(path string) ([]json.RawMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, usageErr(fmt.Errorf("opening plan file: %w", err))
	}
	defer f.Close()
	// Try envelope first.
	var env struct {
		Plan []json.RawMessage `json:"plan"`
	}
	dec := json.NewDecoder(f)
	if err := dec.Decode(&env); err == nil && env.Plan != nil {
		return env.Plan, nil
	}
	// Reset and try JSONL.
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	var out []json.RawMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		out = append(out, json.RawMessage(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading plan file: %w", err)
	}
	return out, nil
}
