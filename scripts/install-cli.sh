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

# Make sure GOPATH/bin (where `go install` drops the binary) is on PATH for
# interactive shells. Idempotent: only append the line once.
gobin="$(go env GOBIN)"
[ -n "$gobin" ] || gobin="$(go env GOPATH)/bin"
if ! grep -qsF "$gobin" "${HOME}/.bashrc" 2>/dev/null; then
  printf 'export PATH="$PATH:%s"\n' "$gobin" >> "${HOME}/.bashrc"
fi

echo "todoist-aum installed to ${gobin}"
exit 0
