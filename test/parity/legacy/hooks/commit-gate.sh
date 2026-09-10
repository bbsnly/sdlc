#!/usr/bin/env bash
# PreToolUse (Bash, if Bash(git commit *)): under trunk-only, the commit IS the gate.
# Nothing lands on main unless every blocking gate passed on exactly this tree.
. "$(dirname "$0")/_lib.sh"
CMD="$(j '.tool_input.command // ""')"
# The settings "if: Bash(git commit *)" condition also fires on commands it cannot analyse
# (shell variables, brace groups, substitutions). Guard here so only real commits are gated.
case "$CMD" in *"git commit"*) ;; *) exit 0 ;; esac
[ -n "$ACTIVE" ] || exit 0                      # commits outside a loop iteration are untouched
[ -n "$AGENT" ] && deny "Only the orchestrator commits."
MSGFILE=""
if [[ "$CMD" =~ (-F|--file)[=[:space:]]+([^[:space:]]+) ]]; then MSGFILE="${BASH_REMATCH[2]}"; fi
if [ -z "$MSGFILE" ]; then
  case "$CMD" in *"$ACTIVE"*) ;; *) deny "Commit message must reference story $ACTIVE. Use: git commit -F $SD/commit-message.txt";; esac
fi
RES="$("$GATE" commit-check "$ACTIVE" ${MSGFILE:+"$MSGFILE"} 2>/dev/null || echo '{"ok":false,"problems":["gate commit-check failed to run"]}')"
if [ "$(jq -r .ok <<<"$RES")" != "true" ]; then
  deny "COMMIT BLOCKED for $ACTIVE: $(jq -r '.problems | join(" | ")' <<<"$RES")"
fi
exit 0
