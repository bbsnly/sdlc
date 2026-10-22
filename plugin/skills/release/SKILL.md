---
name: release
description: Release a story from the checkout that holds it, with your reason, so that it can be picked up in another. You type it; the model cannot.
disable-model-invocation: true
argument-hint: <story-id> --reason <why>
---

# `/sdlc:release` did not run

If you are reading this, `/sdlc:release` did not run: the sdlc hook records the person's decision
before this text would reach you, and it did not. Tell the person that, and that it usually means
the sdlc the hook runs is older than this plugin or is not there; typing `/sdlc:doctor`, or running
`sdlc doctor` in a terminal, says whether one is found, and asking you to run `sdlc version` says
which release it is. Do not run `sdlc release` yourself, and do not move the story another way. Do
nothing else.
