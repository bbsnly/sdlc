---
name: doctor
description: Check that sdlc, the plugin and this project are set up and working, as `sdlc doctor` does in a terminal, and say what to put right.
---

# Check the set-up

Run `sdlc doctor` with the Bash tool, from the session's directory, and show the person exactly what
it printed. Do nothing else.

When a person types `/sdlc:doctor`, the sdlc hook runs it before this text is read and shows them
what it printed. So if a person typed it and you are reading this, tell them too that the hook did
not run it, which usually means the sdlc it runs is older than this plugin, and that the Bash tool
may find another sdlc than the one the hook runs.
