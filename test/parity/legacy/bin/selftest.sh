#!/usr/bin/env bash
# selftest.sh — exercises the gate CLI and every hook against a throwaway repo. Run after installing:
#   SDLC_KIT=~/.claude ~/.claude/sdlc/bin/selftest.sh     (or SDLC_KIT=<kit>/global before copying)
set -euo pipefail
KIT="${SDLC_KIT:-$HOME/.claude}"          # override with SDLC_KIT=/path/to/kit/global before installing
export SDLC_GATE="$KIT/sdlc/bin/gate"
H="$KIT/sdlc/hooks"
export PATH="$KIT/sdlc/bin:$PATH"
command -v jq >/dev/null || { echo "jq is required"; exit 2; }
W=$(mktemp -d /tmp/sdlc-smoke-XXXX); cd "$W"
export CLAUDE_PROJECT_DIR="$W"
git init -q -b main . && git config user.email t@t && git config user.name t
mkdir -p src tests
echo 'echo hello' > src/app.sh
cat > tests/run.sh <<'EOF'
#!/usr/bin/env bash
out=$(bash src/app.sh); [ "$out" = "hello" ] && echo "AC-1 ok" || { echo "AC-1 FAIL"; exit 1; }
EOF
echo "# proj" > README.md
git add -A && git commit -qm "init"

pass=0; fail=0
check() { # check <name> <expected-substring> <actual>
  if [[ "$3" == *"$2"* ]]; then pass=$((pass+1)); echo "  ok   $1"; else fail=$((fail+1)); echo "  FAIL $1 :: expected '$2' in: $3"; fi
}
hook() { # hook <script> <json>  -> stdout+exit
  local out rc; set +e; out="$(printf '%s' "$2" | bash "$H/$1" 2>&1)"; rc=$?; set -e; echo "$out"; echo "rc=$rc"; }

echo "== no config"
check "gate without config dies" "run /sdlc-init" "$(gate summary 2>&1 || true)"

echo "== scaffold"
mkdir -p .sdlc/state .sdlc/stories .sdlc/templates
cp "$KIT/sdlc/templates/"* .sdlc/templates/
cp .sdlc/templates/user_stories.json user_stories.json
cp .sdlc/templates/claude-progress.json .sdlc/claude-progress.json
cp .sdlc/templates/lessons.md .sdlc/lessons.md
jq '.commands.smoke="bash tests/run.sh" | .commands.test="bash tests/run.sh" | .commands.build="bash -n src/app.sh" | .commands.lint="" | .commands.coverage="echo cov 85.5" | .commands.mutation="" | .paths.tests={dirs:["tests/"],file_globs:[]} | .git.remote=false' \
  .sdlc/templates/sdlc.config.json > .sdlc/config.json
printf '.sdlc/state/\n.sdlc/claude-progress.json\n.sdlc/stories/*/logs/\n.sdlc/stories/*/heldout/\n' > .gitignore
git add -A && git commit -qm "chore(sdlc): initialise loop"
check "summary ok" '"ok":true' "$(gate summary)"
check "trunk health green" '"ok":true' "$(gate trunk-health)"

echo "== gate 1"
N="$(gate story next)"; check "story next US-001" '"id":"US-001"' "$N"
check "loop start" '"active":true' "$(gate loop start US-001)"
check "status in_progress" '"status":"in_progress"' "$(gate story status US-001 in_progress)"
check "record dor" '"dor":"pass"' "$(gate record US-001 dor pass "3 ACs")"
check "flag" '"security_sensitive":false' "$(gate flag US-001 security_sensitive false)"
gate record US-001 analysis pass >/dev/null

echo "== gate 3 freeze"
cat > tests/ac2_test.sh <<'EOF'
#!/usr/bin/env bash
out=$(bash src/app.sh --shout 2>/dev/null); [ "$out" = "HELLO" ] && echo "AC-2 ok" || { echo "AC-2 FAIL"; exit 1; }
EOF
F="$(gate freeze US-001)"; check "freeze 2 files" '"files_frozen":2' "$F"
check "freeze twice refused" "already frozen" "$(gate freeze US-001 2>&1 || true)"
check "lock-check ok" '"ok":true' "$(gate lock-check)"

echo "== hooks: write policy"
mk() { jq -nc --arg a "$1" --arg t "$2" --arg f "$3" '{hook_event_name:"PreToolUse",cwd:$ENV.CLAUDE_PROJECT_DIR,tool_name:$t,tool_input:{file_path:$f}} + (if $a=="" then {} else {agent_type:$a} end)'; }
check "implementer edit frozen test -> deny"   '"permissionDecision":"deny"' "$(hook write-policy.sh "$(mk sdlc-implementer Edit "$W/tests/ac2_test.sh")")"
check "implementer edit src -> allow"          'rc=0' "$(hook write-policy.sh "$(mk sdlc-implementer Edit "$W/src/app.sh")")"
check "implementer write .sdlc/state -> deny"  'protected' "$(hook write-policy.sh "$(mk sdlc-implementer Write "$W/.sdlc/state/x")")"
check "sdet edit src -> deny"                  'sdlc-sdet may only write' "$(hook write-policy.sh "$(mk sdlc-sdet Edit "$W/src/app.sh")")"
check "sdet new test file -> allow"            'rc=0' "$(hook write-policy.sh "$(mk sdlc-sdet Write "$W/tests/ac3_test.sh")")"
check "orchestrator edit src -> deny"          'does not edit code' "$(hook write-policy.sh "$(mk "" Edit "$W/src/app.sh")")"
check "orchestrator write PLAN -> allow"       'rc=0' "$(hook write-policy.sh "$(mk "" Write "$W/.sdlc/stories/US-001/PLAN.md")")"
check "orchestrator write backlog -> allow"    'rc=0' "$(hook write-policy.sh "$(mk "" Edit "$W/user_stories.json")")"
check "architect writes own review -> allow"   'rc=0' "$(hook write-policy.sh "$(mk sdlc-architect Write "$W/.sdlc/stories/US-001/reviews/sdlc-architect-round1.json")")"
check "architect writes src -> deny"           'read-only' "$(hook write-policy.sh "$(mk sdlc-architect Edit "$W/src/app.sh")")"
check "anyone edits CLAUDE.md -> deny"         'protected' "$(hook write-policy.sh "$(mk sdlc-implementer Edit "$W/CLAUDE.md")")"

echo "== hooks: read policy"
mkr() { jq -nc --arg a "$1" --arg f "$2" '{hook_event_name:"PreToolUse",cwd:$ENV.CLAUDE_PROJECT_DIR,tool_name:"Read",tool_input:{file_path:$f}} + (if $a=="" then {} else {agent_type:$a} end)'; }
check "implementer reads heldout -> deny" 'held-out' "$(hook read-policy.sh "$(mkr sdlc-implementer "$W/.sdlc/stories/US-001/heldout/x.sh")")"
check "anyone reads .env -> deny" 'credentials' "$(hook read-policy.sh "$(mkr sdlc-researcher "$W/.env")")"
check "implementer reads src -> allow" 'rc=0' "$(hook read-policy.sh "$(mkr sdlc-implementer "$W/src/app.sh")")"

echo "== hooks: bash policy"
mkb() { jq -nc --arg a "$1" --arg c "$2" '{hook_event_name:"PreToolUse",cwd:$ENV.CLAUDE_PROJECT_DIR,tool_name:"Bash",tool_input:{command:$c}} + (if $a=="" then {} else {agent_type:$a} end)'; }
check "force push -> deny"                'force-push' "$(hook bash-policy.sh "$(mkb "" "git push --force origin main")")"
check "subagent git commit -> deny"       'Only the orchestrator' "$(hook bash-policy.sh "$(mkb sdlc-implementer "git commit -m x")")"
check "implementer sed frozen test -> deny" 'frozen test' "$(hook bash-policy.sh "$(mkb sdlc-implementer "sed -i s/a/b/ tests/ac2_test.sh")")"
check "implementer runs tests -> allow"   'rc=0' "$(hook bash-policy.sh "$(mkb sdlc-implementer "gate run US-001 test")")"
check "reviewer bash -> deny"             'no shell' "$(hook bash-policy.sh "$(mkb sdlc-code-reviewer "ls")")"
check "SDLC_ENFORCE tamper -> deny"       'cannot be changed' "$(hook bash-policy.sh "$(mkb "" "export SDLC_ENFORCE=0")")"

echo "== gate 4-5"
gate record US-001 plan pass >/dev/null; gate record US-001 design_review pass "round 1" >/dev/null
cat > src/app.sh <<'EOF'
if [ "${1:-}" = "--shout" ]; then echo HELLO; else echo hello; fi
EOF
D="$(gate diff US-001)"; check "diff has code + new test only" '"files":["src/app.sh","tests/ac2_test.sh"]' "$D"
check "diff excludes .sdlc and backlog" '"changed_lines":4' "$D"
gate record US-001 implementation pass >/dev/null

echo "== gate 6"
V="$(gate verify US-001)"; check "verify ok" '"ok":true' "$V"; check "coverage parsed" '"coverage":"pass"' "$V"
check "mutation skipped" '"mutation":"skipped"' "$V"
S="$(gate snapshot US-001)"; check "snapshot ok" '"ok":true' "$S"
SNAP="$(jq -r .worktree <<<"$S")"; check "snapshot has new code" "shout" "$(cat "$SNAP/src/app.sh")"
check "snapshot has story dir" "gate-record" "$(ls "$SNAP/.sdlc/stories/US-001/")"
check "verifier writes in snapshot -> allow" 'rc=0' "$(hook write-policy.sh "$(mk sdlc-verifier Write "$SNAP/tests/ac1_heldout.sh")")"
check "verifier writes main workspace -> deny" 'snapshot' "$(hook write-policy.sh "$(mk sdlc-verifier Write "$W/src/app.sh")")"
check "snapshot clean" 'removed' "$(gate snapshot-clean US-001)"
[ -d "$SNAP" ] && echo "  FAIL snapshot dir still exists" || echo "  ok   snapshot dir removed"
check "verifier_review recorded w/ hash" '"verifier_review":"pass"' "$(gate record US-001 verifier_review pass "3/3")"
check "manual verification record refused" "gate verify" "$(gate record US-001 verification pass 2>&1 || true)"

echo "== gate 7-8"
gate record US-001 code_review pass "round 1" >/dev/null
sed 's/<ID>/US-001/g; s/<type>(<scope>): <summary in ≤ 72 chars>/feat(app): add --shout/' .sdlc/templates/commit-message.txt > .sdlc/stories/US-001/commit-message.txt
C="$(gate commit-check US-001 .sdlc/stories/US-001/commit-message.txt)"; check "commit-check ok" '"ok":true' "$C"
echo "# tamper" >> src/app.sh
C2="$(gate commit-check US-001 .sdlc/stories/US-001/commit-message.txt)"; check "commit-check detects code change" 'changed since gate verify' "$C2"
sed -i.bak '$d' src/app.sh && rm -f src/app.sh.bak   # -i.bak: portable across GNU and BSD sed
check "commit-check ok again" '"ok":true' "$(gate commit-check US-001 .sdlc/stories/US-001/commit-message.txt)"
# tamper a frozen test via shell, then check
echo "# x" >> tests/ac1_test.sh 2>/dev/null || true
cp tests/run.sh tests/run.sh.bak; echo "# tampered" >> tests/run.sh
check "commit-check detects test tamper" 'tampered' "$(gate commit-check US-001 .sdlc/stories/US-001/commit-message.txt)"
mv tests/run.sh.bak tests/run.sh; rm -f tests/ac1_test.sh
check "lock ok after restore" '"ok":true' "$(gate lock-check)"
check "commit hook allows" 'rc=0' "$(hook commit-gate.sh "$(mkb "" "git commit -F .sdlc/stories/US-001/commit-message.txt")")"
check "commit hook blocks bad msg" 'must reference story' "$(hook commit-gate.sh "$(mkb "" "git commit -m 'oops'")")"
# high-risk story requires approval
jq '(.stories[] | select(.id=="US-001") | .risk_tier) = "high"' user_stories.json > u.tmp && mv u.tmp user_stories.json
check "commit-check requires approval for high tier" 'approval required' "$(gate commit-check US-001 .sdlc/stories/US-001/commit-message.txt)"
E="$(gate escalate US-001 pre_commit_approval "please approve")"; check "escalate written" '"type":"pre_commit_approval"' "$E"
check "loop inactive after escalate" '"loop_active":false' "$(gate summary)"
check "stop guard silent when escalated" 'rc=0' "$(hook stop-guard.sh "$(jq -nc '{hook_event_name:"Stop",cwd:$ENV.CLAUDE_PROJECT_DIR,stop_hook_active:false}')")"
A="$(gate approve US-001)"; check "approved" '"decision":"approved"' "$A"
gate loop start US-001 >/dev/null
check "commit-check ok with approval" '"ok":true' "$(gate commit-check US-001 .sdlc/stories/US-001/commit-message.txt)"
git add -A && git commit -q -F .sdlc/stories/US-001/commit-message.txt
check "commit landed on main" 'feat(app): add --shout [US-001]' "$(git log -1 --format=%s)"
gate record US-001 commit pass "$(git rev-parse --short HEAD)" >/dev/null

echo "== consensus + retro"
cat > .sdlc/stories/US-001/reviews/sdlc-architect-round1.json <<'EOF'
{"persona":"sdlc-architect","round":1,"artifact":"PLAN.md","verdict":"request_changes","findings":[{"id":"ARCH-1","severity":"major","required":true,"title":"AC-2 has no step"}]}
EOF
cat > .sdlc/stories/US-001/reviews/sdlc-red-team-round1.json <<'EOF'
{"persona":"sdlc-red-team","round":1,"artifact":"PLAN.md","verdict":"request_changes","findings":[{"id":"RT-1","severity":"major","required":true,"title":"advisory only"}]}
EOF
cat > .sdlc/stories/US-001/reviews/sdlc-architect-round2.json <<'EOF'
{"persona":"sdlc-architect","round":2,"artifact":"PLAN.md","verdict":"approve","findings":[]}
EOF
cat > .sdlc/stories/US-001/reviews/sdlc-code-reviewer-gate7-round1.json <<'EOF'
{"persona":"sdlc-code-reviewer","round":1,"artifact":"diff.patch","verdict":"approve","findings":[]}
EOF
C1="$(gate consensus US-001 4 1)"
check "consensus blocks on architect" '"passes":false' "$C1"
check "consensus rereview = architect only" '"rereview":["sdlc-architect"]' "$C1"
check "consensus advisory major kept apart" '"advisory_majors":[{"reviewer":"sdlc-red-team"' "$C1"
check "consensus round 2 passes" '"passes":true' "$(gate consensus US-001 4 2)"
check "consensus gate 7 sees only gate7 files" '"persona":"sdlc-code-reviewer","verdict":"approve","blocking":true' "$(gate consensus US-001 7 1)"
check "consensus rejects other gates" 'must be 4' "$(gate consensus US-001 5 1 2>&1 || true)"
R="$(gate retro US-001)"; check "retro written" '"design_rounds":2' "$R"
check "retro papercut from unfreeze/escalation" '"kind":"pre_commit_approval"' "$(cat .sdlc/papercuts.jsonl)"
check "retro table has gate 7 row" '| 7 | 1 | sdlc-code-reviewer | approve |' "$(cat .sdlc/stories/US-001/retro.md)"
printf -- '- kept old name\n' > dev.tmp; awk -v ins="$(cat dev.tmp)" '/^## Deviations/{print; print ins; skip=1; next} skip&&/^- $/{skip=0; next} {print}' .sdlc/stories/US-001/retro.md > r.tmp && mv r.tmp .sdlc/stories/US-001/retro.md; rm -f dev.tmp
gate retro US-001 >/dev/null
check "retro preserves bookkeeper section" 'kept old name' "$(cat .sdlc/stories/US-001/retro.md)"
check "retro papercuts idempotent" '"papercuts_added":0' "$(gate retro US-001)"

echo "== gate 9"
echo "- module src/app.sh: greeting" >> CODEMAP.md 2>/dev/null || echo "- module src/app.sh" > CODEMAP.md
check "status done" '"status":"done"' "$(gate story status US-001 done)"
check "bookkeeping commit-check" '"kind":"bookkeeping"' "$(gate commit-check US-001)"
check "bookkeeping commit ok" '"ok":true' "$(gate commit-check US-001)"
gate record US-001 retro pass >/dev/null
check "loop end" '"active":false' "$(gate loop end)"
printf 'chore(sdlc): close US-001\n\nStory: US-001\n' > .sdlc/stories/US-001/commit-close.txt
git add -A && git commit -q -F .sdlc/stories/US-001/commit-close.txt
check "metric after done touches only ledger" '"ok":true' "$(gate metric US-001 cost_usd 1.25)"
check "trunk clean after close" '"ok":true' "$(gate trunk-health)"
check "backlog exhausted" 'no runnable story' "$(gate story next 2>&1 || true)"

echo "== stop guard escalation path"
gate story add '{"id":"US-002","title":"t","priority":1,"status":"ready","risk_tier":"low","depends_on":[],"acceptance_criteria":[{"id":"AC-1","text":"The system shall x"}]}' >/dev/null
gate loop start US-002 >/dev/null
SJ="$(jq -nc '{hook_event_name:"Stop",cwd:$ENV.CLAUDE_PROJECT_DIR,stop_hook_active:false}')"
check "stop blocked #1" '"decision":"block"' "$(hook stop-guard.sh "$SJ")"
hook stop-guard.sh "$SJ" >/dev/null; hook stop-guard.sh "$SJ" >/dev/null
check "stop #4 escalates loop_stalled" 'loop_stalled' "$(hook stop-guard.sh "$SJ"; gate summary)"
check "stop_hook_active respected" 'rc=0' "$(hook stop-guard.sh "$(jq -nc '{hook_event_name:"Stop",cwd:$ENV.CLAUDE_PROJECT_DIR,stop_hook_active:true}')")"

echo "== session start"
SS="$(hook session-start.sh "$(jq -nc '{hook_event_name:"SessionStart",source:"startup",cwd:$ENV.CLAUDE_PROJECT_DIR}')")"
check "session start prints state" 'escalation_pending: loop_stalled' "$SS"

echo "== unfreeze"
gate approve US-002 >/dev/null; gate loop start US-002 >/dev/null; gate freeze US-002 >/dev/null
check "unfreeze resets gates" 'reset to pending' "$(gate unfreeze US-002 "test AC-1 contradicts spec")"
check "refreeze with --force" '"files_frozen":' "$(gate freeze US-002 --force)"
gate loop end >/dev/null

echo; echo "RESULT: $pass passed, $fail failed  (workdir $W)"
[ "$fail" = 0 ]
