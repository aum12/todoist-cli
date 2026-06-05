#!/usr/bin/env bash
#
# install-cli.sh — build and install the todoist-aum CLI at session start.
#
# Wired up as a SessionStart hook in .claude/settings.json so it runs at the
# start of every Claude Code session. It only does work in cloud sessions,
# where the container is ephemeral and the CLI would otherwise be missing.
#
# Why a hook (and not just the environment setup script): the setup script is
# cached/snapshotted and does NOT re-run when you push new CLI code, so its
# binary can go stale. This hook rebuilds from the freshly-cloned repo every
# session, so the installed binary always matches HEAD. The environment setup
# script's job is to warm the Go module + build caches (which ARE snapshotted),
# turning this rebuild into a fast incremental compile instead of a cold one.
set -euo pipefail

# Cloud-only: local sessions manage their own toolchain. The cloud runtime sets
# CLAUDE_CODE_REMOTE=true; bail out everywhere else so this is a no-op locally.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

# Build from the repo root so the binary tracks the current checkout.
cd "${CLAUDE_PROJECT_DIR:-$(git rev-parse --show-toplevel)}"
go install ./cmd/todoist-aum

# Symlink into /usr/local/bin, which is on PATH for every shell type —
# interactive and non-interactive alike — so the CLI is callable everywhere
# (your terminal, hooks, the agent's shell) without relying on ~/.bashrc
# being sourced. -f makes it idempotent and repoints at the fresh build.
gobin="$(go env GOBIN)"
[ -n "$gobin" ] || gobin="$(go env GOPATH)/bin"
ln -sf "${gobin}/todoist-aum" /usr/local/bin/todoist-aum

echo "todoist-aum installed to ${gobin} and linked at /usr/local/bin/todoist-aum"

# Refresh the local store in the background so session start never waits.
# Two phases, sequential, detached via setsid:
#   1. everything except comments and activities — the fast set (~15s on a
#      large account), written page-by-page so it is queryable almost
#      immediately. activities is the premium audit log that no local command
#      reads (review/productivity read the tasks table; reschedule-history is
#      live), so it is skipped entirely.
#   2. comments only — the slow per-parent fan-out (one request per task and
#      project). Runs after phase 1 so it never delays the data you query at
#      session start. Parents are already populated, so --resources comments
#      syncs just the dependent.
# Gated on the token (sync no-ops without auth). Output goes to a log file, not
# the hook's stdout, so the session is interactive right away.
if [ -n "${TODOIST_API_TOKEN:-}" ]; then
  synclog="${TMPDIR:-/tmp}/todoist-aum-sync.log"
  setsid bash -c '
    /usr/local/bin/todoist-aum sync --exclude comments,activities
    /usr/local/bin/todoist-aum sync --resources comments
  ' >"$synclog" 2>&1 </dev/null &
  echo "todoist-aum sync started in background (fast set first, then comments) → $synclog"
fi

exit 0
