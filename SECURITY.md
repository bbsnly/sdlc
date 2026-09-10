# Security policy

## Reporting a vulnerability

Please report security issues through GitHub's private vulnerability reporting:
[open a report](https://github.com/bbsnly/sdlc/security/advisories/new). That
keeps the details private until there is a fix to ship.

Please do not open a public issue for a vulnerability.

You should get an acknowledgement within a week. This is a single-maintainer
project, so please read that as a good-faith commitment rather than an SLA.

## What is in scope

`sdlc` installs a binary, registers hooks with Claude Code, and decides whether
a command a model proposed is allowed to run. The parts where a defect is a
security defect rather than a bug:

- **The shell policy.** A command that should have been denied and was allowed.
  Spellings that evade classification are exactly the class of report that is
  most useful here.
- **The installer.** Anything that lets a downloaded artifact be replaced,
  or that places a binary without verifying its checksum against the pins
  shipped in the package.
- **Privilege of the model caller.** Anything that lets a model-driven session
  perform an action reserved for a human in a terminal.

## What is out of scope

- The behaviour of Claude Code itself. Report those to Anthropic.
- A model writing bad code that this tool then dutifully runs through its
  gates. The loop is a process, not a sandbox.
