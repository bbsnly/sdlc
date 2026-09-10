#!/usr/bin/env bash
# PreToolUse (Bash): dangerous-command guard + role limits on shell side effects.
. "$(dirname "$0")/_lib.sh"
CMD="$(j '.tool_input.command // ""')"; [ -n "$CMD" ] || exit 0
# ---- always, in any loop-enabled project ------------------------------------
case "$CMD" in
  *"git push"*--force*|*"git push"*" -f "*|*"git push -f"*) deny "force-push is never allowed: trunk history is immutable." ;;
  *"git reset --hard"*|*"git filter-branch"*|*"git rebase -i"*|*"git rebase --interactive"*) deny "history rewrite / hard reset blocked. Recover with git revert or Claude Code checkpoints." ;;
  *"git commit"*--amend*) deny "--amend rewrites history; make a new commit." ;;
  *"--no-verify"*) deny "--no-verify bypasses hooks; not allowed." ;;
  *"git checkout -- ."*|*"git checkout ."*|*"git restore ."*|*"git restore --staged ."*|*"git clean -f"*) deny "whole-tree discard blocked (would erase in-progress work). Restore individual files if needed." ;;
  *"core.hooksPath"*|*".git/hooks"*) deny "git hook configuration is protected." ;;
  *"SDLC_ENFORCE"*|*"disableAllHooks"*|*".claude/settings"*|*"~/.claude/"*|*"$HOME/.claude/"*) deny "hook/enforcement configuration cannot be changed from inside a session." ;;
  *"curl "*"| sh"*|*"curl "*"| bash"*|*"wget "*"| sh"*|*"wget "*"| bash"*) deny "piping downloads into a shell is blocked." ;;
  *"rm -rf /"*|*"rm -rf ~"*|*"rm -rf .."*|*"rm -rf .git"*|*"rm -rf .sdlc"*|*"rm -fr /"*) deny "destructive rm blocked." ;;
esac
[ -n "$ACTIVE" ] || exit 0
# ---- during an active loop ---------------------------------------------------
if [ -n "$AGENT" ]; then
  case "$CMD" in
    *"git commit"*) deny "Only the orchestrator commits (Gate 8). Subagents leave the tree uncommitted." ;;
    *"git push"*|*"git stash"*|*"git worktree remove"*|*"git worktree prune"*) deny "git push/stash/worktree changes belong to the orchestrator." ;;
    *"gate loop"*|*"gate escalate"*|*"gate approve"*|*"gate freeze"*|*"gate unfreeze"*|*"gate story status"*|*"gate record "*|*"gate metric"*|*"gate snapshot"*)
      deny "State-changing gate commands are reserved for the orchestrator. Subagents may use: gate run, gate lock-check, gate diff-size, gate tree-hash, gate record-get, gate story get." ;;
  esac
fi
if [ "$AGENT" = "sdlc-implementer" ]; then
  case "$CMD" in *heldout*|*"snapshot-"*) deny "sdlc-implementer may not touch the verifier's held-out tests or snapshot.";; esac
  if [ -f .sdlc/state/tests.lock ]; then
    while IFS= read -r f; do
      case "$CMD" in *"$f"*)
        case "$CMD" in *"sed -i"*|*">"*|*"tee "*|*"mv "*|*"rm "*|*"cp "*|*"truncate"*|*"git checkout"*|*"git restore"*|*"patch "*|*"chmod"*)
          deny "Shell write to frozen test file $f blocked (tests.lock). Report a wrong test; do not modify it." ;;
        esac ;;
      esac
    done < <(jq -r '.files | keys[]' .sdlc/state/tests.lock)
  fi
  case "$CMD" in *"tests.lock"*) deny "tests.lock is protected.";; esac
fi
if is_reviewer "$AGENT"; then
  deny "$AGENT has no shell: review from the repository files and $SD/diff.patch. If a command must be run, request it in your findings."
fi
exit 0
