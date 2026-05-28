
package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// whichEntry is one row of the curated capability index. The index is
// seeded at generation time from the same NovelFeature list that drives
// the SKILL.md feature section, so the command a `which` query returns
// is guaranteed to exist and to match what the skill advertises.
type whichEntry struct {
	Command      string `json:"command"`
	Description  string `json:"description"`
	Group        string `json:"group,omitempty"`
	WhyItMatters string `json:"why_it_matters,omitempty"`
}

// whichIndex is the curated list of capabilities this CLI advertises as
// its hero features. Endpoint-level commands are discoverable via
// `--help`; `which` exists to resolve a natural-language capability
// query to one of the commands the skill says matter most.
var whichIndex = []whichEntry{
	{Command: "capture", Description: "Voice/agent/routine-driven task entry. Accepts content, --into <project-name>, repeatable --label <name>, --due/--deadline (ISO or Todoist NL date), repeatable --reminder <iso> or --reminder-offset <duration relative to due>, --description, --priority p1..p4, --location <address>, --tz <iana-zone>, and --stdin for batch.", Group: "Composed workflows", WhyItMatters: "Apple Watch voice + Anthropic Routine + Claude agent calls this once per dictated task. The CLI handles Date vs Deadline vs Reminder distinction, timezone, multi-label, and location-reminder linkage so the agent does not have to issue four separate API calls."},
	{Command: "review", Description: "Retrospective rollup with prior-period delta. `--window day` covers end-of-day check-in; `--window week` covers Sunday review; `--window month` covers month rollup.", Group: "Composed workflows", WhyItMatters: "ADHD nightly check-in and Sunday review use the same shape — agents call `review --window day` at 9pm and `review --window week` on Sunday with no code change."},
	{Command: "agenda", Description: "Today's tasks plus the overdue tail plus a windowed lookahead with priority and project context — composed from the local store in one pass.", Group: "Composed workflows", WhyItMatters: "When an agent is asked 'plan my day', this returns a complete, priority-ordered, context-decorated payload in one MCP call instead of 8-15 atomic tool calls."},
	{Command: "triage", Description: "For each Inbox item, propose the most likely (project, label, section) by matching historical co-occurrence in the local store, then apply the reviewed plan atomically.", Group: "Local state that compounds", WhyItMatters: "Replaces a 20-30 minute Monday morning click-by-click ritual with a reviewable plan an agent can edit before commit."},
	{Command: "reschedule-cascade", Description: "Postpone every task matching a Todoist filter by a relative shift, previewing per-task new due strings, recurring-next-instance shifts, and deadline-violation warnings before any write.", Group: "Safe bulk mutation", WhyItMatters: "Agents asked to 'postpone everything I missed this week' get a safe, reviewable plan with full collision detection before any change is committed upstream."},
	{Command: "filter-batch", Description: "Bulk complete, move, or relabel every task matching a Todoist filter, with a before/after diff plan that requires explicit apply.", Group: "Safe bulk mutation", WhyItMatters: "Lets agents safely propose bulk operations to the user, preview the diff, and apply only after explicit confirmation."},
	{Command: "focus", Description: "`focus set --top N --reason <why>` picks the highest-priority/deadline-tightest tasks and labels them @focus-today; `focus show` returns just those; `focus clear` removes the label at end-of-day.", Group: "Composed workflows", WhyItMatters: "Lets an ADHD-aware agent commit the user to a small daily plan and report adherence at end-of-day — replaces the 'pick 3 things' note-taking loop that lives outside Todoist today."},
	{Command: "near", Description: "Takes a label or context name (walmart, home, office), returns open tasks tagged with that label OR in any matching project, ranked by priority and age — agent-shaped envelope for geofence-triggered routines.", Group: "Composed workflows", WhyItMatters: "Apple Shortcuts geofence at Walmart fires → Routine calls `near walmart` → the agent reminds the user of relevant tasks, no manual filter typing required."},
	{Command: "productivity-trend", Description: "Rollup over local completed_tasks with group-by dimensions (label, hour-of-day) the Todoist stats endpoint does not expose.", Group: "Local state that compounds", WhyItMatters: "Agents asked 'what am I spending my time on' or 'when do I actually finish things' get rollups they cannot obtain via any single endpoint."},
	{Command: "workload", Description: "Cross-project rollup of open tasks per collaborator inside a workspace, by priority, overdue-age bucket, and project.", Group: "Local state that compounds", WhyItMatters: "Team leads get a Monday standup-prep report (overloaded collaborators, slipping work) that the web UI cannot produce."},
	{Command: "reschedule-history", Description: "Walk cached activity events for a task (or filter) and show every time the due date moved, by how much, on which day.", Group: "Local state that compounds", WhyItMatters: "Agents asked 'why does this keep slipping' get an evidence trail without scraping the activity log every time."},
	{Command: "stale-review", Description: "Surface open tasks that are old and inactive; annotate each with a mechanical suggested action (close-as-obsolete / move-to-someday / break-down / reschedule:+7d / manual-review) from heuristics over priority, subtasks, recent comments, and overdue depth. Emit a plan file that filter-batch consumes.", Group: "Local state that compounds", WhyItMatters: "Agents can periodically surface ignored tasks, walk the user through a per-task decision, and commit the reviewed plan via filter-batch --apply — turning Todoist debt cleanup into a guided workflow instead of a daunting backlog scroll."},
}

// whichMatch pairs an index entry with its ranking score for a query.
// Higher score means stronger match. The ranker is naive (exact token
// then substring then group tag) because 20-40 entries do not need
// semantic retrieval - a ranker upgrade is a future change that would
// not break this contract.
type whichMatch struct {
	Entry whichEntry `json:"entry"`
	Score int        `json:"score"`
}

// rankWhich returns up to `limit` best matches for `query` against the
// index, sorted by descending score. Score breakdown:
//
//	+3  exact token match on the command's leaf or full path
//	+2  substring match on the command (any part)
//	+2  substring match on the description
//	+1  group tag contains the query as a word
//
// Ties break on declaration order in the index. An empty query returns
// every entry at score 0 in declaration order - this is the "list all"
// behavior the skill documents for broad agent discovery.
func rankWhich(index []whichEntry, query string, limit int) []whichMatch {
	if limit <= 0 {
		limit = 3
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]whichMatch, 0, len(index))
		for _, e := range index {
			out = append(out, whichMatch{Entry: e, Score: 0})
		}
		return out
	}
	qTokens := strings.Fields(q)

	scored := make([]whichMatch, 0, len(index))
	for i, e := range index {
		score := whichScoreEntry(e, q, qTokens)
		scored = append(scored, whichMatch{Entry: e, Score: score})
		_ = i
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	// Drop zero-score matches when the query was non-empty; agents
	// branching on exit code rely on "no match" meaning no confidence.
	filtered := scored[:0]
	for _, m := range scored {
		if m.Score > 0 {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func whichScoreEntry(e whichEntry, query string, qTokens []string) int {
	score := 0
	cmd := strings.ToLower(e.Command)
	cmdTokens := strings.Fields(cmd)
	desc := strings.ToLower(e.Description)
	group := strings.ToLower(e.Group)

	// Exact token match on the command path (any token).
	for _, qt := range qTokens {
		for _, ct := range cmdTokens {
			if qt == ct {
				score += 3
				break
			}
		}
	}
	// Substring match on the full command (covers hyphenated leaves).
	if strings.Contains(cmd, query) {
		score += 2
	}
	// Substring match on the description.
	if strings.Contains(desc, query) {
		score += 2
	}
	// Group tag match.
	if group != "" {
		for _, qt := range qTokens {
			if strings.Contains(group, qt) {
				score += 1
				break
			}
		}
	}
	return score
}

func newWhichCmd(flags *rootFlags) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "which [query]",
		Short: "Find the command that implements a capability",
		Annotations: map[string]string{
			"pp:typed-exit-codes": "0,2",
		},
		Long: `which resolves a natural-language capability query (for example, "search messages" or "stale tickets") to the best matching command from this CLI's curated feature index.

Exit codes:
  0  at least one match found
  2  no confident match - the query did not score against any indexed capability; fall back to '--help' or 'search' if this CLI has one`,
		Example: `  todoist-aum which "stale tickets"
  todoist-aum which "bottleneck"
  todoist-aum which --limit 1 "send message"
  todoist-aum which                                # list the full capability index`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(whichIndex) == 0 {
				return usageErr(fmt.Errorf("this CLI has no curated capability index; run '--help' to see every command"))
			}
			query := strings.Join(args, " ")
			matches := rankWhich(whichIndex, query, limit)

			// Empty query returns the whole index at score 0 (listing mode).
			if strings.TrimSpace(query) == "" {
				return renderWhich(cmd, flags, rankWhichAll(whichIndex))
			}

			if len(matches) == 0 {
				// Under --json, return an empty matches envelope at exit 0
				// so agents can branch on `matches.length == 0` instead of
				// parsing a usage error message. Non-JSON keeps the typed
				// exit-2 path so terminal users see the help hint.
				if flags.asJSON {
					return printJSONFiltered(cmd.OutOrStdout(), map[string]any{
						"matches": []whichMatch{},
					}, flags)
				}
				return usageErr(fmt.Errorf("no match for %q; try '%s --help' for the full command list", query, cmd.Root().Name()))
			}
			return renderWhich(cmd, flags, matches)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 3, "Maximum number of matches to return")
	return cmd
}

// rankWhichAll is a narrow helper used by the "empty query lists the
// index" path. It returns every entry in declaration order at score 0
// so the render path treats them uniformly.
func rankWhichAll(index []whichEntry) []whichMatch {
	out := make([]whichMatch, 0, len(index))
	for _, e := range index {
		out = append(out, whichMatch{Entry: e, Score: 0})
	}
	return out
}

func renderWhich(cmd *cobra.Command, flags *rootFlags, matches []whichMatch) error {
	w := cmd.OutOrStdout()
	// Output shape follows the same rule as every other generated
	// command: JSON when the caller asked for it OR when stdout is not
	// a terminal; table when a human is looking.
	asJSON := flags.asJSON
	if !asJSON && !isTerminal(w) {
		asJSON = true
	}
	if asJSON {
		// JSON envelope: {matches: [...]}. The wrap is critical:
		// printJSONFiltered's --compact path uses compactListFields
		// (allowlist) for top-level arrays, which would strip
		// entry/score keys; routing through compactObjectFields
		// (blocklist) via an object envelope preserves them.
		if matches == nil {
			matches = []whichMatch{}
		}
		return printJSONFiltered(w, map[string]any{"matches": matches}, flags)
	}
	fmt.Fprintf(w, "%-24s  %-8s  %s\n", "COMMAND", "SCORE", "DESCRIPTION")
	for _, m := range matches {
		fmt.Fprintf(w, "%-24s  %-8d  %s\n", m.Entry.Command, m.Score, m.Entry.Description)
	}
	return nil
}
