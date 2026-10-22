---
name: remap
description: Make a landed story's CODEMAP.md change that could not be made on trunk, or put it over trunk's map with --replace, or discard it with --drop. You type it; the model cannot.
disable-model-invocation: true
argument-hint: <story-id> [--replace | --drop]
---

# `/sdlc:remap` did not run

If you are reading this, `/sdlc:remap` did not run: the sdlc hook records the person's decision
before this text would reach you, and it did not. Tell the person that, and that it usually means
the sdlc the hook runs is older than this plugin or is not there; typing `/sdlc:doctor`, or running
`sdlc doctor` in a terminal, says whether one is found, and asking you to run `sdlc version` says
which release it is. It can also mean that it waited for another story's landing and ran out of
time; then it is typed again once that landing has finished. Do not run `sdlc remap` yourself, and
do not change the map another way. Do nothing else.
