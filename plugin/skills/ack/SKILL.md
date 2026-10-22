---
name: ack
description: Acknowledge the log of stories landed on trunk up to a commit you have read, so the log starts after it. You type it; the model cannot.
disable-model-invocation: true
argument-hint: --through <commit>
---

# `/sdlc:ack` did not run

If you are reading this, `/sdlc:ack` did not run: the sdlc hook records the person's decision before
this text would reach you, and it did not. Tell the person that, and that it usually means the sdlc
the hook runs is older than this plugin or is not there; typing `/sdlc:doctor`, or running `sdlc
doctor` in a terminal, says whether one is found, and asking you to run `sdlc version` says which
release it is. Do not run `sdlc ack` yourself, and do not acknowledge the log another way. Do
nothing else.
