#!/usr/bin/env bash
# Shared helpers for SDLC hooks. Sourced by each hook, not executed directly.
#
# Contract: a hook reads the event JSON from stdin, does nothing unless the
# project has .sdlc/config.json, and only *blocks* while a loop iteration is
# active (.sdlc/state/active exists). SDLC_ENFORCE=0 in the shell environment
# disables enforcement for a session started by a human; it cannot be set from
# inside a Claude session (bash-policy.sh blocks that).
INPUT="$(cat)"
j() { jq -r "$1" <<<"$INPUT" 2>/dev/null; }
PROJECT="${CLAUDE_PROJECT_DIR:-$(j '.cwd // ""')}"
[ -n "$PROJECT" ] && cd "$PROJECT" 2>/dev/null || exit 0
[ "${SDLC_ENFORCE:-1}" = "0" ] && exit 0
[ -f .sdlc/config.json ] || exit 0
GATE="${SDLC_GATE:-$HOME/.claude/sdlc/bin/gate}"
ACTIVE=""; [ -f .sdlc/state/active ] && ACTIVE="$(cat .sdlc/state/active)"
AGENT="$(j '.agent_type // ""')"
EVENT="$(j '.hook_event_name // ""')"
SD=".sdlc/stories/$ACTIVE"

relpath() { local p="$1"; case "$p" in "$PROJECT"/*) p="${p#"$PROJECT"/}";; esac; p="${p#./}"; printf '%s' "$p"; }
is_abs_outside() { case "$1" in /*) case "$1" in "$PROJECT"/*) return 1;; *) return 0;; esac;; *) return 1;; esac; }
deny() {
  jq -nc --arg r "$1" --arg e "$EVENT" \
    '{hookSpecificOutput:{hookEventName:$e,permissionDecision:"deny",permissionDecisionReason:$r}}'
  exit 0
}
_test_pattern() {
  local pattern="" d g
  while IFS= read -r d; do [ -n "$d" ] && pattern="$pattern|${d%/}/*"; done < <(jq -r '.paths.tests.dirs // [] | .[]' .sdlc/config.json)
  while IFS= read -r g; do [ -n "$g" ] && pattern="$pattern|$g|*/$g"; done < <(jq -r '.paths.tests.file_globs // [] | .[]' .sdlc/config.json)
  printf '%s' "${pattern#|}"
}
is_test_path() { local pat; pat="$(_test_pattern)"; [ -n "$pat" ] || return 1; eval "case \"\$1\" in $pat) return 0;; esac"; return 1; }
is_frozen() { [ -f .sdlc/state/tests.lock ] && jq -e --arg f "$1" '.files[$f]' .sdlc/state/tests.lock >/dev/null 2>&1; }
snapshot_dir() { [ -f ".sdlc/state/snapshot-$ACTIVE.json" ] && jq -r .worktree ".sdlc/state/snapshot-$ACTIVE.json" || true; }
is_reviewer() { case "$1" in sdlc-architect|sdlc-security|sdlc-red-team|sdlc-code-reviewer|sdlc-perf|sdlc-human-advocate) return 0;; *) return 1;; esac; }
