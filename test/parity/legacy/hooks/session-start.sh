#!/usr/bin/env bash
# SessionStart (startup|resume|clear|compact): re-inject loop state as plain facts.
. "$(dirname "$0")/_lib.sh"
SRC="$(j '.source // ""')"
if [ "$SRC" = "compact" ] && [ -n "$ACTIVE" ]; then
  cur="$(jq -r '.metrics.compactions // 0' "$SD/gate-record.json" 2>/dev/null || echo 0)"
  "$GATE" metric "$ACTIVE" compactions "$((cur+1))" >/dev/null 2>&1 || true
fi
S="$("$GATE" summary 2>/dev/null || true)"; [ -n "$S" ] || exit 0
echo "SDLC loop state for this project, read from .sdlc/ at $(date -u +%Y-%m-%dT%H:%MZ):"
echo "$S" | jq -r '
  "- loop_active: \(.loop_active)",
  "- current_story: \(.current_story // "none")",
  "- gates: \((.record.gates // {}) | tostring)",
  "- escalation_pending: \(if .escalation then (.escalation.type + " - " + .escalation.message) else "none" end)",
  "- branch: \(.branch), dirty_files: \(.dirty_files), backlog: \(.backlog|tostring)",
  "- recent_lessons: \(.recent_lessons|join(" / "))"'
echo "Gate details are in .sdlc/stories/<ID>/gate-record.json. The loop runbook is the /sdlc-loop skill."
