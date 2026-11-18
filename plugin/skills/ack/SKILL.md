---
name: ack
description: Acknowledge the log of stories landed on trunk up to a commit you have read, so the log starts after it. You type it; the model cannot.
disable-model-invocation: true
argument-hint: --through <commit>
---

# `/sdlc:ack` did not run

If you are reading this, `/sdlc:ack` did not run, and nothing was recorded. The sdlc hook makes the
person's decision before this text would reach you, and only for a command the person typed at
their own prompt; you are reading this because a subagent started it, or because the hook could not
act (the sdlc it runs is older than this plugin, or is not there). Tell the person that, and that
what they type is `/sdlc:ack` with its arguments at their own prompt, or `!sdlc ack` with the same
arguments at the prompt in Claude Desktop, or `sdlc ack` in a terminal outside Claude Code. If they
say they typed it themselves, `/sdlc:doctor`, or `sdlc doctor` in a terminal, says whether the sdlc
the hook runs is found and which release it is. Do not run `sdlc ack` yourself, and do not
acknowledge the log another way. Do nothing else.
