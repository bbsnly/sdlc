---
name: approve
description: Answer the question a story is waiting on you for, approving the work or sending it back with --reject. You type it; the model cannot.
disable-model-invocation: true
argument-hint: <story-id> [--reject <why>]
---

# `/sdlc:approve` did not run

If you are reading this, `/sdlc:approve` did not run: the sdlc hook records the person's decision
before this text would reach you, and it did not. Tell the person that, and that it usually means
the sdlc the hook runs is older than this plugin or is not there; typing `/sdlc:doctor`, or running
`sdlc doctor` in a terminal, says whether one is found, and asking you to run `sdlc version` says
which release it is. Do not run `sdlc approve` yourself, and do not make the decision another way.
Do nothing else.
