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
- `sdlc cost` keeps what a story spent beside everything else it did.
  `sdlc cost add --usd` records an amount, `sdlc status` reports the running
  total, and crossing one of `budget.alert_fractions` says so once, on
  standard error. Nothing blocks: a story stopped between gates costs more
  than the overspend. This is what `budget.per_story_usd` was for -- it was
  written into every project's configuration and read by nothing.
- The freeze holds against shell commands. Every rule protecting it applied to
  the file-writing tools only, so `Write` to a frozen test was refused and
  `echo cheat > x_test.go` was not. Reading one is still never refused.
- The freeze covers what a test depends on, not only the test file. A golden
  file, a jest snapshot, a mock and the `conftest.py` that decides what a
  pytest fixture returns each change whether a test passes without the test
  being touched, and each was outside the freeze. `sdlc init` now names the
  usual ones per stack, and a bare directory name in `paths.tests.dirs`
  matches such a directory at any depth rather than only at the root.
- The commit gate asks the verifier and the code reviewer about the tree as
  it is now. Their approvals were checked when their own gates were recorded
  and never again, so code added after the review and then committed reached
  trunk unreviewed.
- Enforcement holds from anywhere in the repository. A session started in a
  subdirectory reports that directory, and the hook looked for the project's
  configuration only there -- so `cd backend && claude` turned every rule off
  without saying so.
- Path rules match the way the filesystem does. macOS and Windows are
  case-insensitive, and `.SDLC/state/active`, `.sdlc/Config.json` and
  `claude.md` were writable while the identically-named files were refused.
  The test freeze held only the exact spelling, so a capital letter took it
  off the file it was protecting.
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
- Every agent can write only what its gate produces, reviewers included. The
  eight reviewing roles had no write scope at all, so an architect could edit
  production code in the middle of a design review and a code reviewer could
  fix what it was about to approve. Each keeps its own story directory, and
  the verdict still goes through `sdlc review add`.
- Picking a story up again changes nothing. `sdlc start` on a story already
  under way stamped a fresh timestamp into the backlog, which is a tracked
  file and therefore part of the tree the verifier and the Gate 7 reviewers
  are stamped against — so resuming in a new session, which is how the loop
  is meant to be used, sent five reviewers back to re-review work that had
  not changed.

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
- `npx skills add bbsnly/sdlc` installs the runbook on its own, as a plain
  Agent Skill, for any agent that reads them. It carries no binary, no agents
  and no hook, so the skill checks for the tool and for the agents before it
  does anything and says what to install if either is missing.

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
