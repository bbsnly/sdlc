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
  verification gate on tests that changed. The implementer writes no test
  file at all, before the freeze or after it, through the file tools or
  through the shell. A test file the freeze does not hold stops
  `tests_frozen`, `plan`, `implementation`, `verification` and `commit`; with
  `freeze.allow_new_test_files` on, the test author can add one and
  `sdlc freeze` adds it to the freeze. `sdlc unfreeze --reason` is the way
  out: it is on the record, and it sends the story back to Gate 3. The freeze
  belongs to the iteration: it
  survives `sdlc stop` and a new session, and is lifted when the story
  finishes, so the next story freezes its own tests.
- A question the loop should not answer for itself goes to a person.
  `sdlc escalate` puts it on the story's record and ends the iteration, and
  `sdlc start` refuses the story until somebody answers with `sdlc approve`,
  or sends the work back with `--reject`. The hook refuses `sdlc approve` from
  a tool call, because an agent that could run it would be approving its own
  work. A story in a risk tier `human_gates.pre_commit_pause_tiers` names —
  `high`, by default — cannot be committed until a person has approved the
  work as it stands; a change made after the approval needs approving again.
- `human_gates.dor_advocate_check` has the human advocate read the story at
  Gate 1, and `sdlc gate dor pass` waits for its review. It is advisory there
  as everywhere, and it goes stale if the story changes after it.
- A session cannot quietly end its turn mid-story. A `Stop` hook sends the stop
  back with what to do instead, and after `loop.max_stop_blocks` stops in a row
  with nothing recorded, hands the story to a person. A count in
  `.sdlc/state/stop-blocks.json` below zero starts again rather than putting
  the hand-over off indefinitely.
- `commands.fmt_file` runs on each file a tool writes while a story is being
  worked on, and a formatter that fails is reported to the session. The
  commands `sdlc init` writes for it name the files their formatter
  understands.
- A new story starts only from a healthy trunk. `sdlc start` refuses another
  branch, uncommitted work that belongs to no story, a trunk behind `origin`
  when `git.remote` is on, and a trunk that fails `commands.smoke`. A story
  picked up again is not held to it.
- A change bigger than `thresholds.diff_size_cap` cannot pass implementation,
  verification or code review. Lines are counted the way `git diff --numstat`
  counts them, and the loop's own files and the backlog are not counted.
- A block from perf stops the gate, as its role always said it would when a
  change breaks a stated performance budget. It still never has to approve,
  and `sdlc review list` shows it as `on budget`.
- A gate that keeps failing and a reviewer that keeps blocking go to a person.
  After `loop.max_rework_rounds` failures of one gate, or
  `loop.max_review_rounds` blocks from one reviewer, the command that recorded
  the last one hands the story over, counting again once the person answers.
- `sdlc cost` keeps what a story spent beside everything else it did.
  `sdlc cost add --usd` records an amount, `--story` names the story once a
  headless session is over, `sdlc status` reports the running total, and
  crossing one of `budget.alert_fractions` says so once, on standard error, as
  does using up the budget. Nothing blocks: a story stopped between gates
  costs more than the overspend. This is what `budget.per_story_usd` was for -- it was
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
- A lock is broken open only once the process holding it has stopped, not
  merely because it is old, so a slow command keeps it. The stop guard takes
  it too, and decides from what is recorded once it has it. A command killed
  part-way through a write no longer leaves a temporary file that the commit
  gate refuses as uncommitted work: the next command clears it.
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
- The retro agent writes the retro and the codebase map, and nothing else. It
  has file-writing tools and had no write scope, so it could edit production
  code while writing up what the story did. The seven reviewing roles are held
  to their story directory as well, as a backstop: they are given no
  file-writing tools, and a shell command is outside these rules, which
  docs/enforcement.md now says instead of leaving it implied.
- A story's record, reviews and documents go through the tool for every story
  in the backlog, not only the one being worked on. While a story was open,
  any agent — the implementer included — could rewrite a finished story's gate
  record, its plan and its code review, and nothing refused it. The history
  the loop keeps is only evidence if a later story cannot edit it.
- Picking a story up again changes nothing. `sdlc start` on a story already
  under way stamped a fresh timestamp into the backlog, which is a tracked
  file and therefore part of the tree the verifier and the Gate 7 reviewers
  are stamped against — so resuming in a new session, which is how the loop
  is meant to be used, sent five reviewers back to re-review work that had
  not changed.
- Naming a story to `sdlc start` puts it ahead of the priority order and
  nothing more. A story that is dropped, blocked or marked done, or that
  waits on a dependency that is not done, is refused as it would have been
  passed over, and a story whose status is not one the schema lists is
  refused when the backlog is read, rather than silently never picked. So is
  a backlog with two stories of one id, whatever their case: every lookup
  found the first, so starting the second moved the first. A story id no
  longer ends in a dot, which Windows drops, so `A-1.` was `A-1`'s directory
  and starting it overwrote that story's record.

### The command

- `sdlc init`, `status`, `story list`, `start`, `stop`, `gate`, `artifact`,
  `review`, `freeze`, `unfreeze`, `escalate`, `approve`, `cost`, `doctor` and
  `version`. Every one of them takes `--json`, so a skill can read what a
  person reads.
- `sdlc doctor` checks the repository, the configuration, the backlog, the
  contract section, git, whether each configured command's program is
  installed, and whether the hooks can find the binary at all. Every problem
  it reports carries the command that fixes it.
- Error codes `SDLC-E0001` through `SDLC-E0046`, each with a heading in
  [the troubleshooting page](https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md).
  Every error says what happened, why, and what to do about it.

### The plugin

- `/sdlc:next` works the current story through its gates, one at a time, until
  it is committed or needs a person, and picks up where a session left off.
- Eleven agents, one per role: researcher, sdet, implementer, architect,
  security, red-team, perf, human-advocate, verifier, code-reviewer and
  bookkeeper.
- A `PreToolUse` hook that enforces the rules rather than asking for them. It
  covers `Write`, `Edit`, `MultiEdit`, `NotebookEdit`, and the tools that run a
  command — `Bash`, `PowerShell` and `Monitor`: write scopes
  per role, protected configuration and loop state, the test freeze, the
  commit gate, and the shell routes around all of those. Every refusal names
  the rule it applied and the sanctioned way to do the same thing.
- The repository is its own marketplace, so installing the plugin is two
  lines in Claude Code.
- `npx skills add bbsnly/sdlc` installs the runbook on its own, as a plain
  Agent Skill, for any agent that reads them. It carries no binary, no agents
  and no hook, so the skill checks for the tool and for the agents before it
  does anything and says what to install if either is missing.
- A protected path spelled with a letter that folds to ASCII at a different
  length is still the protected path. `.ſdlc/state/active` (a long s) is
  `.sdlc/state/active` to a case-insensitive filesystem, and the rules compared
  it by byte offset, landed mid-character, and let the write through.
- A Windows junction to a protected directory is that directory. Go has not
  followed junctions since 1.23, and making one needs no privilege, so a
  junction to `.sdlc` or `.git` reached the freeze, the active story and the
  git hooks under a name no rule knew. Paths are now resolved the way Windows
  resolves them, and `mklink` has its arguments checked like `ln`. In Git Bash
  `cmd //c`, the way `cmd /c` has to be written there, is read as `cmd /c`: the
  doubled slash was taken for the command, and nothing behind it was checked.
- A path in a shell command written with a doubled separator, or a `.` between
  two, is the path it names. `rm .sdlc//state//active`, `rm .sdlc/./state/active`
  and Git Bash's `del .sdlc\\state\\active` reached the loop's own files on every
  platform, because each was compared as it was spelled.
- `~+`, `~-` and `~2` in a shell command are directories the shell knows, not
  the user's home, and `~name` is that user's home, which can be yours.
  `rm ~+/.sdlc/state/active` was read as a file outside the project, and so was
  a project under `~you/`.
- The hook's warnings reach the session. They went to standard error, which
  Claude Code sends to its debug log when a hook allows the call, so "nothing
  is being enforced" was said to nobody. They now arrive as a system message,
  and so does the launcher's "the sdlc binary was not found".
- A test freeze that cannot be read no longer counts as no freeze. Corrupting
  `.sdlc/state/tests.lock` made every frozen test editable, quietly; until it
  reads again, every test file is treated as frozen, whether the write comes
  from a file tool or a shell command. The same goes for a `tests.lock` that
  was deleted, or rewritten to name another story or none, while the story's
  record still holds the freeze; `sdlc doctor` names it.
- A gate record that is missing or will not parse no longer opens the commit
  gate. Deleting `.sdlc/stories/<id>/gate-record.json` was the way to commit
  past every gate, without a word. The refusal names the file, `sdlc doctor`
  reads it, and `sdlc stop` still ends an iteration whose record is broken, so
  there is always a way out.
- A story id with `..` in it is refused by `sdlc start` as well as by the hook.
  The hook refused `A..1` and enforced nothing for that iteration, while the
  command had accepted it.
- A crash in the hook tells the session. It exited non-zero with a
  `stopReason`, and Claude Code reads neither in that case.
- `name::$DATA`, NTFS's other spelling of a file, is that file to every rule.
- A freeze belongs to the story it was taken for. With a second story started,
  `sdlc freeze` said "already frozen" and pointed at `sdlc unfreeze`, which
  then lifted the first story's freeze and logged it on the second story's
  record. `unfreeze` now refuses another story's freeze, `freeze` names that
  story instead, and marking a story `dropped` releases its freeze.
- Lifting the freeze is a person's decision in fact. The runbook asked agents
  not to run `sdlc unfreeze`, and nothing stopped one that did. The hook now
  refuses it from any tool call, however `sdlc` is reached, and every refusal
  that used to suggest it says the person running the session lifts the
  freeze from their own terminal.
- Recording a gate again reopens every gate after it. After `plan fail` and a
  new `plan pass`, the gates past the plan stayed passed, so the loop went on
  to code review and never asked for a design review of the new plan.
- The commit gate refuses a commit of work changed since it was reviewed. The
  hook asked only whether the gates had passed, so code edited after the code
  review was committed, and `sdlc gate commit pass` found the stale reviews only
  afterwards. The tree is measured only for `git commit`.
- `CLAUDE.md`, `.claude` and `.git` are protected from shell commands, as they
  already were from the file tools. `echo {} > .claude/settings.local.json` went
  through during an iteration, and a settings file is where hooks are turned
  off. A test now holds the shell rules to every path the file rules protect.
- The backlog cannot be edited during an iteration, through the file tools or
  the shell, wherever `backlog.path` puts it. The commit gate reads a story's
  `risk_tier` there, and an assistant that lowered it took away the person who
  approves the commit.
- `.sdlc/config.json` is checked when it is read. A `version` newer than this
  release reads, a `backlog.path` outside the repository, and a negative limit,
  threshold or budget are refused together with `SDLC-E0045`; each was accepted
  and quietly did something else. A setting of the wrong type is named, rather
  than valid JSON being called invalid, and a file saved with a byte order mark
  reads.
- `sdlc doctor` names a setting it does not know, such as a misspelled one,
  which was read as no setting at all while doctor called the configuration
  fine. For a configuration that does not load, it names the settings at fault
  and the fix for them, rather than suggesting `sdlc init`. So it does for a
  backlog that is there and does not read: `sdlc init` is the fix only for one
  that is missing.
- A backlog saved with a byte order mark, as Windows PowerShell 5 saves one,
  reads, like the configuration. It was refused as invalid JSON. Moving a story
  keeps the mark.
- The plugin's hooks run from a plugin directory with a space in its path, as
  one under a home directory whose name has a space in it does. The unquoted
  command split in two and failed, which Claude Code treats as a hook with
  nothing to say, so every rule was off without a word.
- A command after a lone `&` meets the shell rules. `true & git commit` and
  PowerShell's `& git commit` were read as commands called `true` and `&`, so
  the commit gate and every rule on loop state let them through.
- On Windows, a path rooted without a drive, such as `\repo\CLAUDE.md`, is read
  as the file it names. It was joined onto the project as `repo/CLAUDE.md`, a
  file no rule protects, while the tool wrote the project's own `CLAUDE.md`. A
  path relative to a drive's working directory, such as `C:CLAUDE.md`, is
  refused as outside the repository, since only the writer knows where it is.
- The shell rules know more of PowerShell and cmd. A parameter given as
  `-Path:CLAUDE.md`, the aliases `sc`, `ac`, `clc`, `ren`, `rd` and
  `Tee-Object`, `Push-Location`, `cmd /c`, and a backtick used as an escape
  (``CLAUDE`.md``) each wrote or removed a protected file unrefused.
- The shell rules read a path as the file it is on disk. A link to `.sdlc`,
  `CLAUDE.md` or a frozen test, or a Windows short name such as `SDLC~1`, named
  the file in letters no rule matched, and `rm notes/state/active` went through.
- A wrapper no longer hides what it runs. After `timeout 60`, `sudo -u me`,
  `ssh localhost`, `caffeinate`, `direnv exec` or any `find -exec`, a `git commit`, an
  `sdlc approve` or the shell a document is handed to went unread, and sdlc is
  now found behind a wrapper the rules do not know. PowerShell's `<# ... #>`
  comment no longer hides the command after it.
- Two ways past the shell rules on Windows: Python's `py` launcher ran a script
  on loop state as no other interpreter could, and PowerShell set `GIT_DIR` for a
  commit made elsewhere as `Env:\GIT_DIR` or through
  `[Environment]::SetEnvironmentVariable('GIT_DIR', ...)`. Both are now read.
- On macOS and Windows, the project's path spelled in another case was read as
  somewhere else: `rm /OPT/PROJECT/.sdlc/state/active`, or `cd` to it and
  `git commit`, went past the rules on loop state and the commit gate. So did a
  path relative to a drive's working directory, such as `C:.sdlc\state\active`.
  Both are now read as the project's.
- A command nesting `$( )` thousands deep took the hook longer to read than
  Claude Code gives it, and Claude Code then runs the command unchecked: `git
  commit` on the line before went through. A command nested more than 32 deep is
  now refused as `command-too-deep-to-read`.
- Work committed before the review, in a way the hook cannot read, went on
  through the gates: an alias in your own git configuration, or a commit made in
  a clone and fetched. Every gate after it measured the change against a HEAD
  that already held that work, so the reviewers never saw it. `sdlc start` now
  records where HEAD is, and each gate before the commit refuses once it has
  moved (`SDLC-E0046`).
- Only `git commit` met the commit gate. Work committed on a branch in another
  worktree reached trunk with `git merge`, and `git cherry-pick`, `git revert`,
  `git am`, `git rebase`, `git commit-tree` and an alias such as `git -c
  alias.ci=commit ci` went past it too. They are now refused the same way, and
  so is `hub commit`.
- A commit to the project's own repository made from somewhere else went past
  the commit gate: `cd /tmp && git --git-dir=/path/to/project/.git commit`, or
  the same with `GIT_DIR` set. A commit with `GIT_DIR` set is now held to the
  story's gates, and one with `--git-dir` is held to them when that names the
  project.
- A path in your home directory was read as the project's own: `~` was not
  expanded, so `rm ~/.claude/settings.json` was refused as a write to the
  project's `.claude`, and an interpreter given a script under `~/.claude`
  was refused too. A path outside the project is no longer protected.
- `sed --in-pl` and `perl -lpi` rewrote a frozen test unrefused: GNU sed takes a
  shortened long option, and perl's `-l` in front hid the `-i`.
- `awk -i inplace` rewrote a frozen test unrefused, and called as `gawk` it
  rewrote the loop's record and `CLAUDE.md` too.
- A bare file name is not a frozen file of that name elsewhere. With a fixture
  `testdata/config.json` frozen, `cp config.example.json config.json` at the
  root was refused as a write to a frozen test, and so was `touch main.go` in
  another package. A name is still read that way where its directory cannot be
  known: after a `cd` to a variable, a glob, home or `-`, after `popd` or a `cd`
  in a script handed to a shell, and in `find`,
  `xargs`, `git -C` and a program's own code.
- A document handed over in a PowerShell here-string (`@' ... '@`) is read as
  text, as a here-document is. A plan or review whose table named `git commit`
  or `sdlc stop` was refused as that command, and a here-document went to
  `source` when its `--note` merely said "source". A body handed to a shell or
  an interpreter, or to `Invoke-Expression` on a later line, is still read for
  what it runs.
- Which program a shell command runs is read with its quoting. A commit
  message, a `--note` or a `--message` that mentioned `sdlc approve`, `sdlc stop`
  or `sdlc unfreeze` after a `;` or in backticks was refused as that command,
  including the runbook's own `sdlc escalate`; `git log --grep commit` and
  `git stash push -m "wip before commit"` met the commit gate; and
  `docker compose -p sdlc stop` was a stop. A string handed to `bash -c`,
  `cmd /c`, `-Command` or `eval`, and a command in `$(...)` or backticks, is
  still read for what it runs. A commit in another repository, reached by a
  `cd` or `git -C` the rules can follow out of the project, is not held to the
  story's gates.
- `sdlc stop` is refused from a tool call while the story still has a gate to
  pass (`stop-is-a-human-decision`). Every rule holds only while a story is
  being worked on, so ending it part-way was the way round all of them at
  once, and the Stop hook, the runbook and several refusals kept naming it as
  the way out. The runbook's `sdlc stop` after the last gate is not refused.
- `sdlc approve` and `sdlc unfreeze` are refused from a tool call with no story
  being worked on. `sdlc escalate` ends the iteration, and the hook allowed
  everything once it had, so an agent could hand a story over and approve it
  in the next command.
- Refusals no longer send an agent to `sdlc stop`. The main conversation told
  to delegate, and a command that tried to set `SDLC_ENFORCE`, were both
  pointed at ending the iteration, which turns every rule off.
- `sdlc init --force` is refused while a story is being worked on
  (SDLC-E0012). Run from an agent's shell, it put the default settings back
  under a running story, which no rule was looking at: a person's pause on
  medium-risk commits was gone mid-story.
- A flag before the subcommand no longer hides it from the shell rules.
  `sdlc --reason x unfreeze`, `sdlc --reject x approve` and
  `sdlc --note x review add ...` were read as running `x`, and went through.
- Refusals lead somewhere that works. SDLC-E0033 names the story in the
  `sdlc gate ... fail` it suggests: without `--story` that command is refused
  because no iteration is running, which led back to `sdlc start`. The commit
  gate no longer suggests `sdlc stop`, which turned it off, and was followed to
  commit work a person was waiting to approve. A shell write to
  `.sdlc/config.json` and the test author's fixtures route say a person changes
  the configuration, rather than naming commands or edits the agent is refused.
  A `.sdlc/state/active` that names no story says to run `sdlc stop`, not to
  rename a story that does not exist.
- `sdlc version --json` prints JSON. It printed the prose line whatever it was
  asked, although `--json` is documented for every command.
- The hook reads a long shell command in time. A command naming the same paths
  in segment after segment was checked, and looked up on disk, word by word
  again for every segment: a hundred `echo ... | xargs rm` took longer than the
  thirty seconds Claude Code gives the hook, which then lets the call through
  without a word. So did a write to a path tens of thousands of levels deep.
- A new test file cannot be added through the shell after the freeze. The file
  tools refused one; `echo > new_test.go` did not, because the shell was checked
  only against the files the freeze already held.
- `sdlc doctor` checks the loop's state files. Every hook warning about them
  said to run it, and it did not read either one.

### Documentation

- Guides for the jobs people come to the documentation with: adding the loop
  to an existing project, writing stories, tuning commands, stopping and
  resuming, a frozen test that is wrong, a reviewer that blocks, the points
  where a person decides, tracking cost, and reading a refusal. They sit
  beside the reference pages, indexed from docs/README.md.

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
