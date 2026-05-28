# todoist-cli (`todoist-aum`)

A Go CLI for the Todoist Unified API v1. Built for voice-driven capture, structured daily loops, and agent automation (Apple Watch + Anthropic Routines + Claude).

## Install

```bash
go install github.com/aum12/todoist-cli/cmd/todoist-aum@latest
```

Or build from source:

```bash
git clone https://github.com/aum12/todoist-cli
cd todoist-cli
make build              # writes binary to bin/todoist-aum
make install            # go install to $GOPATH/bin
```

Set your API token (from todoist.com → Settings → Integrations → Developer):

```bash
export TODOIST_API_TOKEN=<your-token>
```

Verify:

```bash
todoist-aum doctor
```

## Highlight commands

Every endpoint of the Todoist API v1 is exposed as a subcommand. Plus 12 hand-written compound commands designed for agent workflows:

- **`capture`** — Voice/routine-driven task entry with composed date + reminders + location (name-based project/label resolution from local store).
- **`agenda`** — Today's priority-ordered tasks plus the overdue tail plus a windowed lookahead, in one composed call.
- **`focus`** — Daily-goal setter with `set`/`show`/`clear` subcommands; manages the `@focus-today` label.
- **`near <context>`** — Open tasks tagged for a label/context or in a matching project, ranked.
- **`review`** — Day/week/month retrospective with prior-period delta.
- **`triage`** — Inbox-to-project plan via local historical co-occurrence; `--dry-run` then `--apply <plan>`.
- **`stale-review`** — Find ignored open tasks with mechanical action suggestions.
- **`reschedule-cascade`** — Bulk date-shift with deadline-collision preview.
- **`filter-batch`** — Bulk complete/move/relabel with mandatory `--dry-run`.
- **`productivity-trend`** — Local rollups grouped by day/week/month/project/label/hour.
- **`workload`** — Cross-project collaborator load report.
- **`reschedule-history`** — Per-task timeline of due-date moves from the activity log.

## Quick examples

```bash
# Voice capture from Apple Watch
todoist-aum capture "buy carrots" --into Groceries --label walmart --agent

# Composed capture: task + wrap-up reminder + location reminder
todoist-aum capture "stop by hardware store" \
  --due "today 3:30pm" --reminder-offset 20m \
  --location "Hardware Store" --label errand --label evening --agent

# Plan my day
todoist-aum agenda --window today --include-overdue --agent

# Morning ritual
todoist-aum focus set --top 3 --reason "meeting prep" --agent
todoist-aum review --window day --compare-to-prior --agent

# Bulk cleanup
todoist-aum stale-review --age 30d --inactive 14d --dry-run > plan.md
# (review plan.md, then)
todoist-aum filter-batch --apply plan.md
```

## Data layer

The CLI maintains a local SQLite mirror at `~/.local/share/todoist-aum/data.db`. Run `sync` first to populate it:

```bash
todoist-aum sync --resources tasks,projects,sections,labels --full
```

Offline-friendly commands (`agenda`, `near`, `focus`, `stale-review`, `productivity-trend`, `workload`, `triage`, `review`) read from this mirror. `--data-source live` forces an upstream call.

## MCP server

A separate binary, `todoist-aum-mcp`, runs an MCP server exposing every CLI command as an MCP tool (via cobra-tree mirror). Useful for Claude Desktop, Claude Code, or any MCP-aware agent:

```bash
go install github.com/aum12/todoist-cli/cmd/todoist-aum-mcp@latest
```

Then add to your MCP host config:

```json
{
  "mcpServers": {
    "todoist": {
      "command": "todoist-aum-mcp",
      "env": {"TODOIST_API_TOKEN": "<your-token>"}
    }
  }
}
```

## Layout

```
.
├── cmd/
│   ├── todoist-aum/              # main CLI entry point
│   └── todoist-aum-mcp/          # MCP server entry point
├── internal/
│   ├── cli/                      # ~150 Cobra command files (~70 endpoint + 12 novel + framework)
│   ├── client/                   # HTTP client + bearer auth + rate limiting
│   ├── config/                   # config loading (env vars, file fallback)
│   ├── store/                    # SQLite schema + queries
│   ├── mcp/                      # MCP server with cobra-tree mirror
│   ├── cliutil/                  # shared helpers (duration parsing, CSV, rate limiting, etc.)
│   ├── cache/                    # response cache
│   └── types/                    # shared types
├── go.mod                        # module github.com/aum12/todoist-cli
├── .goreleaser.yaml              # cross-platform release builder
├── Makefile
└── LICENSE                       # Apache-2.0
```

## Acknowledgments

If you found this CLI useful, the foundational work is the Printing Press itself — give the upstream project a ⭐ and consider applying its [installer](https://github.com/mvanhorn/printing-press-library) to generate your own API CLIs.

## License

Apache-2.0. See [LICENSE](./LICENSE) and [NOTICE](./NOTICE).
