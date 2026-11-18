---
name: unfreeze
description: Lift the freeze on a story's acceptance tests, with your reason on the record, so they can be changed and frozen again. You type it; the model cannot.
disable-model-invocation: true
argument-hint: --reason <why the tests have to change>
---

# `/sdlc:unfreeze` did not run

If you are reading this, `/sdlc:unfreeze` did not run, and nothing was recorded. The sdlc hook makes
the person's decision before this text would reach you, and only for a command the person typed at
their own prompt; you are reading this because a subagent started it, or because the hook could not
act (the sdlc it runs is older than this plugin, or is not there). Tell the person that, and that
what they type is `/sdlc:unfreeze` with its arguments at their own prompt, or `!sdlc unfreeze` with
the same arguments at the prompt in Claude Desktop, or `sdlc unfreeze` in a terminal outside Claude
Code. If they say they typed it themselves, `/sdlc:doctor`, or `sdlc doctor` in a terminal, says
whether the sdlc the hook runs is found and which release it is. Do not run `sdlc unfreeze`
yourself, and do not lift the freeze another way. Do nothing else.
