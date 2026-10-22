---
name: init
description: Set this repository up for the loop, as `sdlc init` does in a terminal, and show what it did.
argument-hint: [--force]
---

# Set this repository up for the loop

Run `sdlc init` with the Bash tool, from the session's directory, and show the person exactly what
it printed, including any error and its SDLC-E code. Do nothing else.

When a person types `/sdlc:init`, the sdlc hook runs it before this text is read and shows them what
it printed. So if a person typed it and you are reading this, tell them too that the hook did not
run it, which usually means the sdlc it runs is older than this plugin.
