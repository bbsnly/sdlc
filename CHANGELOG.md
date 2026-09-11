# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Error codes are part of the interface: once published, a code's meaning does
not change and it is never reused.

## [Unreleased]

Nothing yet.

## [0.1.0](https://github.com/bbsnly/sdlc/releases/tag/v0.1.0) - 2026-09-11

The first release: the whole loop, end to end, on macOS, Linux and Windows.

### The loop

- Nine gates, worked one story at a time: definition of ready, analysis,
  frozen tests, plan, design review, implementation, verification, code
  review, and the commit. A gate cannot be recorded before the gates in
  front of it, and every gate that produces a document refuses to pass
  without it.
- Acceptance tests are frozen by content before a line of implementation is
  written. `sdlc freeze` records a hash per test file; after that the
  implementer cannot edit a test, cannot add a test, and cannot pass the
  verification gate on tests that changed. `sdlc unfreeze --reason` is the
  way out, and it is on the record. The freeze belongs to the iteration: it
  survives `sdlc stop` and a new session, and is lifted when the story
  finishes, so the next story freezes its own tests.
- Commands that change loop state take the project's lock first, so that the
  reviewers a gate runs in parallel all land. Without it, five reviewers
  approving at once left one verdict in the record and the other four
  reported success and were discarded.
- Reviews are recorded against the thing they reviewed. A design review is
  stamped with the hash of the plan it read and a code review with the hash
  of the tree it read, so a review of an older version of the work shows as
  stale instead of counting.
- A gate's documents are written through `sdlc artifact write`, by the agent
  whose gate it is. Nobody edits them in place, including the conversation
  running the loop — which is what keeps each gate reviewing work it did not
  shape.
- Passing the last gate finishes the story: it becomes `done` and leaves the
  backlog, so the next `sdlc start` takes the next story instead of reopening
  it. That is read from the gate record rather than from the gate's name, so
  recording a gate as failed afterwards puts the story back to `in_progress`
  — which is the only way back into finished work, and says on the record why
  it came back.

### The command

- `sdlc init`, `status`, `story list`, `start`, `stop`, `gate`, `artifact`,
  `review`, `freeze`, `unfreeze`, `doctor` and `version`. Every one of them
  takes `--json`, so a skill can read what a person reads.
- `sdlc doctor` checks the repository, the configuration, the backlog, the
  contract section, git, whether each configured command's program is
  installed, and whether the hooks can find the binary at all. Every problem
  it reports carries the command that fixes it.
- Error codes `SDLC-E0001` through `SDLC-E0033`, each with a heading in
  [the troubleshooting page](https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md).
  Every error says what happened, why, and what to do about it.

### The plugin

- `/sdlc:next` works the current story to its next gate and stops there.
- Eleven agents, one per role: researcher, sdet, implementer, architect,
  security, red-team, perf, human-advocate, verifier, code-reviewer and
  bookkeeper.
- A `PreToolUse` hook that enforces the rules rather than asking for them. It
  covers `Write`, `Edit`, `MultiEdit`, `NotebookEdit` and `Bash`: write scopes
  per role, protected configuration and loop state, the test freeze, the
  commit gate, and the shell routes around all of those. Every refusal names
  the rule it applied and the sanctioned way to do the same thing.
- The repository is its own marketplace, so installing the plugin is two
  lines in Claude Code.

### Installing

- `install.sh` and `install.ps1` for macOS, Linux and Windows, and
  `npx @bbsnly/sdlc install` for people who would rather not pipe a script
  into a shell. All three verify the download against the release's checksums
  before anything reaches your PATH, and all three install the same native
  binary — no wrapper, because the binary runs on every matching tool call.
- Release archives carry an SBOM and build provenance attestation.

### Deferred

Recorded so that "not in the first release" is a decision with a place to
live rather than an omission:

- A rendered documentation site with search. The documentation is written and
  checked against the code; where it is published is a separate question.
- Harness support beyond Claude Code. The loop's design is deliberately
  tech-agnostic, but the first release ships one integration and does it
  properly.
