#!/usr/bin/env bash
# Stop: the orchestrator may not end its turn mid-story without finishing the
# current gate, escalating to a human, or explicitly ending the iteration.
. "$(dirname "$0")/_lib.sh"
[ -n "$ACTIVE" ] || exit 0
[ "$(j '.stop_hook_active // false')" = "true" ] && exit 0
[ -f .sdlc/state/escalation.json ] && exit 0
N=0; [ -f .sdlc/state/stop-blocks ] && N="$(cat .sdlc/state/stop-blocks)"
MAX="$(jq -r '.loop.max_stop_blocks // 3' .sdlc/config.json)"
if [ "$N" -ge "$MAX" ]; then
  "$GATE" escalate "$ACTIVE" loop_stalled "Orchestrator tried to stop $N times mid-story without finishing a gate or escalating. Human: inspect $SD/gate-record.json, then resume with /sdlc-loop $ACTIVE." >/dev/null 2>&1 || true
  exit 0
fi
echo $((N+1)) > .sdlc/state/stop-blocks
GATES="$(jq -c '.gates | to_entries | map({(.key):.value.status}) | add // {}' "$SD/gate-record.json" 2>/dev/null || echo "{}")"
jq -nc --arg id "$ACTIVE" --arg g "$GATES" '{decision:"block",reason:("Story \($id) is still active (gates: \($g)). Finish the current gate, or run: gate escalate <ID> <type> <message> to hand off to a human, or run: gate loop end if this iteration is legitimately complete. Do not stop silently.")}'
