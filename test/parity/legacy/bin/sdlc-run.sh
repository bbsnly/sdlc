#!/usr/bin/env bash
# sdlc-run.sh — run the SDLC loop unattended: one fresh Claude Code session per story.
#
#   sdlc-run.sh [-n MAX_ITERATIONS] [-b MAX_TOTAL_USD] [-s STORY_ID] [--dry-run]
#
# Each iteration: `claude -p "/sdlc-loop [ID]"` in a fresh context, JSON output captured, cost written
# to the ledger, then the state is inspected. The loop stops when: the backlog is exhausted, an
# escalation is pending (human needed), trunk is red twice in a row, the budget is hit, or MAX
# iterations ran. It never resumes past an escalation on its own.
#
# RUN THIS INSIDE A SANDBOX (Claude Code devcontainer, or the OS sandbox with network limited to
# your git remote and api.anthropic.com). Anthropic's containment guidance: cap the blast radius at
# the environment layer first; permission prompts are not a boundary for unattended runs.
set -euo pipefail

MAX_ITER=5; MAX_USD=""; STORY=""; DRY=0
while [ $# -gt 0 ]; do
  case "$1" in
    -n) MAX_ITER="$2"; shift 2 ;;
    -b) MAX_USD="$2"; shift 2 ;;
    -s) STORY="$2"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

GATE="${SDLC_GATE:-$HOME/.claude/sdlc/bin/gate}"
# Permission strategy for unattended runs (pick ONE, via SDLC_PERMISSION_FLAGS):
#   "--permission-mode auto"            classifier-gated approvals (inner layer; still needs a sandbox)
#   "--dangerously-skip-permissions"    only inside a devcontainer/VM you control
PERM="${SDLC_PERMISSION_FLAGS:---permission-mode auto}"
MODEL="${SDLC_MODEL:-fable}"
LOGDIR=".sdlc/state/runs"; mkdir -p "$LOGDIR"

command -v claude >/dev/null || { echo "claude CLI not on PATH" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq not on PATH" >&2; exit 2; }
[ -f .sdlc/config.json ] || { echo "no .sdlc/config.json — run /sdlc-init first" >&2; exit 2; }

total_usd=0; red_streak=0
for i in $(seq 1 "$MAX_ITER"); do
  S="$("$GATE" summary)"
  if [ "$(jq -r '.escalation != null' <<<"$S")" = "true" ]; then
    echo "[$i] escalation pending: $(jq -r '.escalation.type + " — " + .escalation.message' <<<"$S")"; echo "     resolve with: /sdlc-approve $(jq -r .escalation.story <<<"$S")"; break
  fi
  if [ -z "$(jq -r '.current_story // ""' <<<"$S")" ]; then
    NEXT="$("$GATE" story next ${STORY:+"$STORY"})"
    if [ "$(jq -r .ok <<<"$NEXT")" != "true" ]; then echo "[$i] $(jq -r .error <<<"$NEXT")"; break; fi
  fi
  TS="$(date -u +%Y%m%dT%H%M%SZ)"; OUT="$LOGDIR/$TS.json"
  PROMPT="/sdlc-loop${STORY:+ $STORY}"
  echo "[$i] $(date -u +%H:%M:%SZ) starting: claude -p \"$PROMPT\" --model $MODEL $PERM"
  [ "$DRY" = 1 ] && { echo "     (dry run) would write $OUT"; break; }
  set +e
  claude -p "$PROMPT" --model "$MODEL" $PERM --output-format json > "$OUT" 2> "$LOGDIR/$TS.stderr"
  rc=$?
  set -e
  cost="$(jq -r '.total_cost_usd // 0' "$OUT" 2>/dev/null || echo 0)"
  turns="$(jq -r '.num_turns // 0' "$OUT" 2>/dev/null || echo 0)"
  total_usd="$(awk -v a="$total_usd" -v b="$cost" 'BEGIN{printf "%.4f", a+b}')"
  S2="$("$GATE" summary)"; cur="$(jq -r '.current_story // ""' <<<"$S2")"
  # attribute cost to the story that was active (current or the last one closed in this run)
  sid="$cur"; [ -n "$sid" ] || sid="$(jq -r '.stories | to_entries | map(select(.value.finished != null)) | sort_by(.value.finished) | last | .key // ""' .sdlc/claude-progress.json)"
  if [ -n "$sid" ]; then
    prev="$(jq -r --arg id "$sid" '.stories[$id].metrics.cost_usd // 0' .sdlc/claude-progress.json)"
    "$GATE" metric "$sid" cost_usd "$(awk -v a="$prev" -v b="$cost" 'BEGIN{printf "%.4f", a+b}')" >/dev/null
    "$GATE" metric "$sid" sessions "$(( $(jq -r --arg id "$sid" '.stories[$id].metrics.sessions // 0' .sdlc/claude-progress.json) + 1 ))" >/dev/null
    "$GATE" metric "$sid" turns_last_session "$turns" >/dev/null
    budget="$(jq -r '.budget.per_story_usd // 0' .sdlc/config.json)"
    spent="$(jq -r --arg id "$sid" '.stories[$id].metrics.cost_usd // 0' .sdlc/claude-progress.json)"
    if [ "$budget" != "0" ]; then
      for f in $(jq -r '.budget.alert_fractions // [0.5,0.8,1.0] | .[]' .sdlc/config.json); do
        awk -v s="$spent" -v b="$budget" -v f="$f" 'BEGIN{exit !(s >= b*f)}' && echo "     budget: $sid at $(awk -v s="$spent" -v b="$budget" 'BEGIN{printf "%d", s/b*100}')% of \$$budget"
      done
      if awk -v s="$spent" -v b="$budget" 'BEGIN{exit !(s >= b)}' && [ -n "$cur" ]; then
        "$GATE" escalate "$sid" budget "Per-story budget \$$budget reached (spent \$$spent). Raise budget.per_story_usd or split the story." >/dev/null; echo "     budget exhausted for $sid — escalated"; break
      fi
    fi
  fi
  echo "     rc=$rc cost=\$$cost turns=$turns total=\$$total_usd story=${sid:-none} status=$(jq -r --arg id "$sid" '.stories[$id].status // "?"' .sdlc/claude-progress.json)"
  jq -r '.result // ""' "$OUT" 2>/dev/null | tail -12 | sed 's/^/     | /'
  if [ -n "$MAX_USD" ] && awk -v t="$total_usd" -v m="$MAX_USD" 'BEGIN{exit !(t >= m)}'; then echo "     run budget \$$MAX_USD reached"; break; fi
  TH="$("$GATE" trunk-health)"
  if [ "$(jq -r .ok <<<"$TH")" != "true" ] && [ "$(jq -r .smoke <<<"$TH")" = "FAIL" ]; then
    red_streak=$((red_streak+1)); echo "     trunk RED ($red_streak)"; [ "$red_streak" -ge 2 ] && { echo "     red twice — stopping for a human"; break; }
  else red_streak=0; fi
  [ -n "$STORY" ] && [ "$(jq -r --arg id "$STORY" '.stories[$id].status // ""' .sdlc/claude-progress.json)" = "done" ] && break
done
echo "done. total cost this run: \$$total_usd  (logs: $LOGDIR)"
