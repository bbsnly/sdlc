#!/usr/bin/env bash
# PreCompact: snapshot durable state before the context window is summarized. Never blocks.
. "$(dirname "$0")/_lib.sh"
[ -n "$ACTIVE" ] || exit 0
TS="$(date -u +%Y%m%dT%H%M%SZ)"; D=".sdlc/state/compact-$TS"; mkdir -p "$D"
cp .sdlc/claude-progress.json "$D/" 2>/dev/null || true
cp "$SD/gate-record.json" "$D/" 2>/dev/null || true
exit 0
