#!/usr/bin/env bash
# PreToolUse (Read|Grep|Glob): secrets never enter any agent context; the
# implementer never sees the verifier's held-out tests or snapshot location.
. "$(dirname "$0")/_lib.sh"
P="$(j '.tool_input.file_path // .tool_input.path // ""')"; [ -n "$P" ] || exit 0
REL="$(relpath "$P")"; BASE="$(basename "$REL")"
case "$REL" in
  .env|.env.*|*.pem|*.key|*.p12|*.pfx|*/.aws/*|*/.ssh/*|.netrc|*/.netrc|*credentials*.json|*.keystore|.npmrc|*/.npmrc)
    deny "Reading $BASE is blocked: credentials and secrets never enter an agent's context." ;;
esac
case "$P" in "$HOME"/.aws/*|"$HOME"/.ssh/*|"$HOME"/.config/gh/*|"$HOME"/.netrc) deny "Reading $P is blocked: credential store.";; esac
[ -n "$ACTIVE" ] || exit 0
if [ "$AGENT" = "sdlc-implementer" ]; then
  case "$REL" in
    "$SD"/heldout/*|"$SD"/verification*|.sdlc/state/snapshot-*)
      deny "sdlc-implementer may not read the verifier's held-out tests or snapshot. Satisfy the spec and the frozen tests, not the checker." ;;
  esac
fi
exit 0
