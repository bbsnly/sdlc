#!/usr/bin/env bash
# PreToolUse (Edit|Write|MultiEdit|NotebookEdit): who may write where, during an active loop.
# This table IS the loop's separation of duties. It is enforced here, not by instruction.
. "$(dirname "$0")/_lib.sh"
[ -n "$ACTIVE" ] || exit 0
FILE="$(j '.tool_input.file_path // .tool_input.notebook_path // ""')"; [ -n "$FILE" ] || exit 0
REL="$(relpath "$FILE")"
case "$REL" in
  .git/*|.claude/*|CLAUDE.md|.sdlc/config.json|.sdlc/state/*|.sdlc/claude-progress.json)
    deny "$REL is protected during an active SDLC loop: configuration is human-owned and state changes go through the gate CLI." ;;
esac
if is_frozen "$REL"; then
  deny "$REL is a FROZEN acceptance test (tests.lock). Implementation must satisfy the tests as written. If the test itself is wrong, stop and report it; only the orchestrator may run: gate unfreeze <ID> <reason> (which resets verification and review)."
fi
SNAP="$(snapshot_dir)"
case "$AGENT" in
  sdlc-sdet)
    if is_test_path "$REL" || [[ "$REL" == "$SD"/* ]]; then exit 0; fi
    deny "sdlc-sdet may only write test files and $SD/. Production code is the implementer's job." ;;
  sdlc-implementer)
    if is_test_path "$REL"; then deny "sdlc-implementer may not write test files: the SDET owns tests and they are frozen after Gate 3. Report a wrong test instead of editing it."; fi
    case "$REL" in
      "$SD"/implementation-notes.md) exit 0 ;;
      .sdlc/*) deny "sdlc-implementer may not write under .sdlc/ except $SD/implementation-notes.md" ;;
    esac
    exit 0 ;;
  sdlc-verifier)
    [ -n "$SNAP" ] && [[ "$FILE" == "$SNAP"/* ]] && exit 0
    case "$REL" in "$SD"/verification*.json|"$SD"/heldout/*) exit 0;; esac
    deny "sdlc-verifier writes only inside its snapshot worktree and $SD/verification*.json, never the main workspace." ;;
  sdlc-researcher)
    case "$REL" in "$SD"/*|CODEMAP.md) exit 0;; esac
    deny "sdlc-researcher writes only analysis artifacts under $SD/ and CODEMAP.md." ;;
  sdlc-bookkeeper)
    case "$REL" in .sdlc/*|CODEMAP.md) exit 0;; esac
    deny "sdlc-bookkeeper writes only under .sdlc/ (retro, lessons, papercuts) and CODEMAP.md." ;;
  "")
    case "$REL" in .sdlc/*|CODEMAP.md) exit 0;; esac
    deny "The orchestrator does not edit code or tests directly. Delegate: tests to sdlc-sdet, implementation to sdlc-implementer. To take over manually run: gate loop end (or start Claude with SDLC_ENFORCE=0)." ;;
  *)
    if is_reviewer "$AGENT"; then
      is_abs_outside "$FILE" && deny "$AGENT may not write outside the project."
      case "$REL" in "$SD"/reviews/"$AGENT"*) exit 0;; esac
      deny "$AGENT is read-only except its own review file: $SD/reviews/$AGENT-round<N>.json"
    fi
    exit 0 ;;
esac
