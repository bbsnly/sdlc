---
name: unfreeze
description: Lift the freeze on a story's acceptance tests, with your reason on the record, so they can be changed and frozen again. You type it; the model cannot.
disable-model-invocation: true
argument-hint: --reason <why the tests have to change>
---

# `/sdlc:unfreeze` did not run

If you are reading this, `/sdlc:unfreeze` did not run: the sdlc hook records the person's decision
before this text would reach you, and it did not. Tell the person that, and that it usually means
the sdlc the hook runs is older than this plugin or is not there; typing `/sdlc:doctor`, or running
`sdlc doctor` in a terminal, says whether one is found, and asking you to run `sdlc version` says
which release it is. Do not run `sdlc unfreeze` yourself, and do not lift the freeze another way. Do
nothing else.
