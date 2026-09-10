#!/usr/bin/env bash
# PostToolUse (Edit|Write): optional per-file formatter from config commands.fmt_file
# (use $FILE inside the command). Non-blocking: failures are reported, not enforced.
. "$(dirname "$0")/_lib.sh"
[ -n "$ACTIVE" ] || exit 0
FMT="$(jq -r '.commands.fmt_file // empty' .sdlc/config.json)"; [ -n "$FMT" ] || exit 0
FILE="$(j '.tool_input.file_path // ""')"; [ -n "$FILE" ] || exit 0
REL="$(relpath "$FILE")"; case "$REL" in .sdlc/*|*.md|*.json|*.txt|*.lock) exit 0;; esac
FILE="$FILE" bash -lc "$FMT" >/dev/null 2>&1 || { echo "formatter failed on $REL (commands.fmt_file)" >&2; exit 2; }
exit 0
