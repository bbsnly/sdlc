---
name: release
description: Release a story from the checkout that holds it, with your reason, so that it can be picked up in another. You type it; the model cannot.
disable-model-invocation: true
argument-hint: <story-id> --reason <why>
---

# `/sdlc:release` did not run

If you are reading this, `/sdlc:release` did not run, and nothing was recorded. The sdlc hook makes
the person's decision before this text would reach you, and only for a command the person typed at
their own prompt; you are reading this because a subagent started it, or because the hook could not
act (the sdlc it runs is older than this plugin, or is not there). Tell the person that, and that
what they type is `/sdlc:release` with its arguments at their own prompt, or `!sdlc release` with
the same arguments at the prompt in Claude Desktop, or `sdlc release` in a terminal outside Claude
Code. If they say they typed it themselves, `/sdlc:doctor`, or `sdlc doctor` in a terminal, says
whether the sdlc the hook runs is found and which release it is. Do not run `sdlc release`
yourself, and do not move the story another way. Do nothing else.
