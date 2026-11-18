---
name: remap
description: Make a landed story's CODEMAP.md change that could not be made on trunk, or put it over trunk's map with --replace, or discard it with --drop. You type it; the model cannot.
disable-model-invocation: true
argument-hint: <story-id> [--replace | --drop]
---

# `/sdlc:remap` did not run

If you are reading this, `/sdlc:remap` did not run, and nothing was recorded. The sdlc hook makes
the person's decision before this text would reach you, and only for a command the person typed at
their own prompt; you are reading this because a subagent started it, or because the hook could not
act (the sdlc it runs is older than this plugin, or is not there). Tell the person that, and that
what they type is `/sdlc:remap` with its arguments at their own prompt, or `!sdlc remap` with the
same arguments at the prompt in Claude Desktop, or `sdlc remap` in a terminal outside Claude Code.
If they say they typed it themselves, `/sdlc:doctor`, or `sdlc doctor` in a terminal, says whether
the sdlc the hook runs is found and which release it is. It can also mean that it waited for another
story's landing and ran out of time; then it is typed again once that landing has finished. Do not
run `sdlc remap` yourself, and do not change the map another way. Do nothing else.
