---
name: assign
description: Choose the model and effort an agent works at on a story, in place of the project manager's choice. You type it; the model cannot.
disable-model-invocation: true
argument-hint: <agent> --model <model> [--effort <effort>] --reason <why>
---

# `/sdlc:assign` did not run

If you are reading this, `/sdlc:assign` did not run, and nothing was recorded. The sdlc hook makes
the person's decision before this text would reach you, and only for a command the person typed at
their own prompt; you are reading this because a subagent started it, or because the hook could not
act (the sdlc it runs is older than this plugin, or is not there). Tell the person that, and that
what they type is `/sdlc:assign` with its arguments at their own prompt, or `!sdlc staffing assign`
with the same arguments at the prompt in Claude Desktop, or `sdlc staffing assign` in a terminal
outside Claude Code. If they say they typed it themselves, `/sdlc:doctor`, or `sdlc doctor` in a
terminal, says whether the sdlc the hook runs is found and which release it is. Do not run
`sdlc staffing assign` yourself, and do not change the staffing another way. Do nothing else.
