// Hand-authored novel command. Replaces generator scaffold.

package cli

// pp:data-source local

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/aum12/todoist-cli/internal/store"
)

type triagePlanEntry struct {
	TaskID               string   `json:"task_id"`
	Content              string   `json:"content"`
	SuggestedProjectID   string   `json:"suggested_project_id"`
	SuggestedProjectName string   `json:"suggested_project_name"`
	SuggestedLabels      []string `json:"suggested_labels"`
	SuggestedSectionID   string   `json:"suggested_section_id"`
	Confidence           float64  `json:"confidence"`
}

type triageEnvelope struct {
	Plan       []triagePlanEntry `json:"plan"`
	Count      int               `json:"count"`
	InboxTotal int               `json:"inbox_total"`
}

type triageApplyEnvelope struct {
	Applied  int      `json:"applied"`
	Failed   int      `json:"failed"`
	Failures []string `json:"failures,omitempty"`
}

func newNovelTriageCmd(flags *rootFlags) *cobra.Command {
	var (
		flagApply string
		flagLimit int
	)

	cmd := &cobra.Command{
		Use:   "triage",
		Short: "Plan Inbox cleanup by matching content tokens to historical (project, label) pairs.",
		Long: `Token-overlap matcher for Inbox cleanup. In default plan mode emits a JSONL plan
proposing (project, labels, section) for each Inbox task with a confident historical
match. Apply with --apply <plan-file>.`,
		Example: strings.Trim(`
  # Generate plan
  todoist-aum triage --json > triage-plan.json

  # Apply previously emitted plan
  todoist-aum triage --apply triage-plan.json`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:typed-exit-codes": "0,2,5,6"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagApply == "" && len(args) == 0 && cmd.Flags().NFlag() == 0 {
				// bare invocation: still produce a plan
			}
			if dryRunOK(flags) {
				return nil
			}

			if flagApply != "" {
				return triageApply(cmd, flags, flagApply)
			}
			return triagePlan(cmd, flags, flagLimit)
		},
	}
	cmd.Flags().StringVar(&flagApply, "apply", "", "Apply a previously-emitted plan JSON file (writes via API)")
	cmd.Flags().IntVar(&flagLimit, "limit", 50, "Maximum number of plan entries to emit")
	return cmd
}

func triagePlan(cmd *cobra.Command, flags *rootFlags, limit int) error {
	db, err := openLocalStore(flags)
	if err != nil {
		return err
	}
	defer db.Close()

	// Find inbox project id.
	inboxID, err := triageFindInboxID(cmd.Context(), db)
	if err != nil {
		return err
	}
	if inboxID == "" {
		env := triageEnvelope{Plan: []triagePlanEntry{}}
		if flags.asJSON {
			return flags.printJSON(cmd, env)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "no inbox project found in local store")
		return nil
	}

	// All open tasks; we'll partition into inbox vs. historical (non-inbox).
	allOpen, err := scanOpenTasksWhere(cmd.Context(), db, "")
	if err != nil {
		return err
	}
	var inboxTasks, history []taskRow
	for _, t := range allOpen {
		if t.ProjectID == inboxID {
			inboxTasks = append(inboxTasks, t)
		} else {
			history = append(history, t)
		}
	}

	// Precompute token sets for historical tasks.
	type histTok struct {
		row    taskRow
		tokens map[string]struct{}
	}
	histToks := make([]histTok, 0, len(history))
	for _, h := range history {
		toks := tokenize(h.Content)
		if len(toks) == 0 {
			continue
		}
		m := make(map[string]struct{}, len(toks))
		for _, t := range toks {
			m[t] = struct{}{}
		}
		histToks = append(histToks, histTok{row: h, tokens: m})
	}

	env := triageEnvelope{
		Plan:       []triagePlanEntry{},
		InboxTotal: len(inboxTasks),
	}
	for _, in := range inboxTasks {
		inToks := tokenize(in.Content)
		if len(inToks) == 0 {
			continue
		}
		// Find top historical matches by overlap count (>=2).
		type scored struct {
			row     taskRow
			overlap int
		}
		var matches []scored
		for _, h := range histToks {
			overlap := 0
			for _, t := range inToks {
				if _, ok := h.tokens[t]; ok {
					overlap++
				}
			}
			if overlap >= 2 {
				matches = append(matches, scored{row: h.row, overlap: overlap})
			}
		}
		if len(matches) == 0 {
			continue
		}
		// Pick the most-common (project_id, sorted-labels) tuple, ranked by frequency then overlap.
		type key struct {
			projectID string
			labelsKey string
		}
		counts := map[key]int{}
		bestOverlap := map[key]int{}
		labelsByKey := map[key][]string{}
		nameByKey := map[key]string{}
		for _, m := range matches {
			labs := append([]string{}, m.row.Labels...)
			sort.Strings(labs)
			k := key{projectID: m.row.ProjectID, labelsKey: strings.Join(labs, ",")}
			counts[k]++
			if m.overlap > bestOverlap[k] {
				bestOverlap[k] = m.overlap
			}
			if _, ok := labelsByKey[k]; !ok {
				labelsByKey[k] = labs
				nameByKey[k] = m.row.ProjectName
			}
		}
		var bestKey key
		bestCount := 0
		for k, c := range counts {
			if c > bestCount || (c == bestCount && bestOverlap[k] > bestOverlap[bestKey]) {
				bestCount = c
				bestKey = k
			}
		}
		// Confidence: overlap fraction over inbox token count, capped at 1.
		confidence := float64(bestOverlap[bestKey]) / float64(len(inToks))
		if confidence > 1 {
			confidence = 1
		}

		entry := triagePlanEntry{
			TaskID:               in.ID,
			Content:              in.Content,
			SuggestedProjectID:   bestKey.projectID,
			SuggestedProjectName: nameByKey[bestKey],
			SuggestedLabels:      labelsByKey[bestKey],
			SuggestedSectionID:   "",
			Confidence:           confidence,
		}
		env.Plan = append(env.Plan, entry)
		if limit > 0 && len(env.Plan) >= limit {
			break
		}
	}
	env.Count = len(env.Plan)

	if flags.asJSON {
		return flags.printJSON(cmd, env)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "triage plan: %d / %d Inbox tasks\n", env.Count, env.InboxTotal)
	for _, p := range env.Plan {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  -> project=%s labels=%v (%.2f)\n",
			p.Content, p.SuggestedProjectName, p.SuggestedLabels, p.Confidence)
	}
	return nil
}

func triageApply(cmd *cobra.Command, flags *rootFlags, planPath string) error {
	f, err := os.Open(planPath)
	if err != nil {
		return usageErr(fmt.Errorf("opening plan file: %w", err))
	}
	defer f.Close()

	// Accept either JSONL (one entry per line) or the envelope produced by plan mode.
	var entries []triagePlanEntry
	first, err := bufio.NewReader(f).Peek(1)
	if err == nil && len(first) > 0 && first[0] == '{' {
		// Re-open to read full body and try envelope first.
		f.Seek(0, 0)
		var env triageEnvelope
		dec := json.NewDecoder(f)
		if err := dec.Decode(&env); err == nil && env.Plan != nil {
			entries = env.Plan
		} else {
			// Try JSONL.
			f.Seek(0, 0)
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				var e triagePlanEntry
				if err := json.Unmarshal([]byte(line), &e); err != nil {
					return usageErr(fmt.Errorf("parsing plan line: %w", err))
				}
				entries = append(entries, e)
			}
		}
	}

	c, err := flags.newClient()
	if err != nil {
		return err
	}
	out := triageApplyEnvelope{}
	for _, e := range entries {
		if e.TaskID == "" {
			continue
		}
		body := map[string]any{}
		if e.SuggestedProjectID != "" {
			body["project_id"] = e.SuggestedProjectID
		}
		if e.SuggestedLabels != nil {
			body["labels"] = e.SuggestedLabels
		}
		if e.SuggestedSectionID != "" {
			body["section_id"] = e.SuggestedSectionID
		}
		_, status, err := c.Post(cmd.Context(), "/api/v1/tasks/"+e.TaskID, body)
		if err != nil || status < 200 || status >= 300 {
			out.Failed++
			out.Failures = append(out.Failures, fmt.Sprintf("task %s: status=%d err=%v", e.TaskID, status, err))
			continue
		}
		out.Applied++
	}
	if flags.asJSON {
		_ = flags.printJSON(cmd, out)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "applied=%d failed=%d\n", out.Applied, out.Failed)
		for _, f := range out.Failures {
			fmt.Fprintf(cmd.ErrOrStderr(), "  failure: %s\n", f)
		}
	}
	if out.Failed > 0 && !flags.allowPartialFailure {
		return partialFailureErr(fmt.Errorf("%d triage apply(s) failed", out.Failed))
	}
	return nil
}

func triageFindInboxID(ctx context.Context, db *store.Store) (string, error) {
	rows, err := db.DB().QueryContext(ctx,
		`SELECT id FROM resources
		 WHERE resource_type = 'projects' AND json_extract(data, '$.is_inbox_project') = 1
		 LIMIT 1`)
	if err != nil {
		return "", fmt.Errorf("looking up inbox project: %w", err)
	}
	defer rows.Close()
	var id string
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return "", err
	}
	return id, nil
}

// tokenize lowercases and splits on non-alphanumeric. Filters tokens shorter
// than 3 chars (noise) and a small stopword set.
func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			if b.Len() > 0 {
				tok := b.String()
				b.Reset()
				if len(tok) >= 3 && !triageStopwords[tok] {
					out = append(out, tok)
				}
			}
		}
	}
	if b.Len() > 0 {
		tok := b.String()
		if len(tok) >= 3 && !triageStopwords[tok] {
			out = append(out, tok)
		}
	}
	return out
}

var triageStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "from": true,
	"this": true, "that": true, "into": true, "have": true, "has": true,
	"to": true, "of": true, "on": true, "at": true, "in": true,
}
