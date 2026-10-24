# Adding sdlc to an existing project

- [Introduction](#introduction)
- [Run `sdlc init`](#run-sdlc-init)
  - [What it writes, and what it leaves alone](#what-it-writes-and-what-it-leaves-alone)
  - [What it detects](#what-it-detects)
  - [Mixed repositories and projects with no marker](#mixed-repositories-and-projects-with-no-marker)
  - [Running it again](#running-it-again)
- [Commit the setup](#commit-the-setup)
- [Check it with `sdlc doctor`](#check-it-with-sdlc-doctor)
  - [What `doctor` checks](#what-doctor-checks)
- [Fill in the contract](#fill-in-the-contract)
- [Deciding what to commit](#deciding-what-to-commit)
- [What lives under `.sdlc/`](#what-lives-under-sdlc)
  - [The files, one by one](#the-files-one-by-one)
  - [Files outside `.sdlc/`](#files-outside-sdlc)
- [Where to go next](#where-to-go-next)

## Introduction

When a repository already has code, tests and a `CLAUDE.md` of its own, it is
fair to wonder what a new tool will touch. This guide answers that, and takes
you through putting the loop on such a repository.

The short version is reassuring. `sdlc init` adds a few files and appends one
section to your `CLAUDE.md`. It does not change your code, your tests, your
build or your `.gitignore`, and it does not overwrite a `CLAUDE.md`, an
`AGENTS.md` or a backlog you already have. Along the way you will run `sdlc init`, commit what it
wrote, check what it guessed with `sdlc doctor`, fill in the contract the
reviewers hold the work to, and decide what else belongs in version control.

This guide assumes the `sdlc` binary is on your `PATH` and the plugin is
installed in Claude Code. [Installation](../installation.md) covers both.

## Run `sdlc init`

From anywhere inside the repository, run `sdlc init`:

```console
$ sdlc init
Set up sdlc in /home/<user>/src/billing

  created  .sdlc/config.json
  created  .sdlc/templates/story.schema.json
  created  user_stories.json
  created  CLAUDE.md (appended to)

Guessed the commands from a Go project.

Next:
  1. sdlc doctor              check the guesses
  2. edit user_stories.json   replace the example with a story of your own
  3. /sdlc:next               type it in Claude Code to work the first story
```

`init` walks up to the nearest `.git` and writes everything relative to that
directory, so it does not matter which subdirectory you run it from. The
output lists each file it wrote as `created` and each it left alone as `kept`,
and there is nothing else.

### What it writes, and what it leaves alone

Here is what happens to each file the first time you run it, and what changes
if you later run `sdlc init --force`:

| File | First run | `sdlc init --force` |
| --- | --- | --- |
| `.sdlc/config.json` | written | rewritten from a fresh detection and the defaults, keeping `backlog.path` and `git.trunk_branch` |
| `.sdlc/templates/story.schema.json` | written | rewritten |
| `user_stories.json` | written with one example story, unless it exists | kept |
| `CLAUDE.md` | created, or the `## SDLC Contract` section appended | section appended only if missing |
| `AGENTS.md` | the section appended in place of `CLAUDE.md`, where there is no `CLAUDE.md` or it links to `AGENTS.md` | section appended only if missing |

`init` writes no `.gitignore`. What a project commits and ignores is the
project's decision, and [Deciding what to commit](#deciding-what-to-commit)
gives you the facts to make it.

`init` and `doctor` look for the contract as a `## SDLC Contract` heading on a
line of its own. A `### SDLC Contract` heading, or a sentence that mentions the
words, does not count, and `init` appends a section of its own.

### What it detects

To fill in the commands for you, `init` looks for a marker file in the
repository root. It tries the stacks in this order and takes the first match:

| Stack | Marker | Commands it fills in |
| --- | --- | --- |
| Go | `go.mod` | `smoke`, `build`, `test`, `lint` (golangci-lint), `fmt`, `fmt_check`, `fmt_file`, `coverage` |
| Rust | `Cargo.toml` | `smoke`, `build`, `test`, `lint` (clippy), `fmt`, `fmt_check` |
| Node | `package.json` | `build`, `test`, `lint` for each script that exists; `fmt` and `fmt_check` from `format` / `format:check` scripts or a `prettier` dependency; `fmt_file` from a `prettier` dependency when there is no `format` script; `smoke` is `build` |
| Python | `pyproject.toml`, `setup.py` or `requirements.txt` | `test` (pytest), `lint`, `fmt`, `fmt_check`, `fmt_file` (ruff), `coverage` (pytest-cov) |

Each stack also sets `paths.tests` and `paths.src`.
[Tuning commands for your stack](tuning-commands-for-your-stack.md) explains
what they do and how to change them.

For Node, a command is only wired to a script your `package.json` already has.
A command pointing at a missing script would fail every gate on a project that
is otherwise fine. A `package.json` that does not parse still counts as Node,
with no commands filled in.

### Mixed repositories and projects with no marker

The order in the table matters in a mixed repository. A Go service with a
`package.json` for its front-end tooling is set up as Go, and a Python project
with one is set up as Node. `init` says when it saw more than one:

```console
Guessed the commands from a Go project. This repository also has Node in it,
so check .sdlc/config.json before you rely on it.
```

With no marker at all, every command is empty, `paths.tests` gets `tests/`,
`test/`, `testdata/` and `fixtures/` plus the built-in file patterns, and
`paths.src` is `src/`. Don't worry if that is your situation: you can fill the
commands in by hand, and `sdlc doctor` will tell you if one cannot be found.

### Running it again

Run `init` a second time without `--force` and it stops with
[SDLC-E0003](../troubleshooting.md#sdlc-e0003) rather than lose your settings.

If you want a fresh start, `--force` replaces every other setting with its
default, but keeps where your stories are and what your trunk branch is
called: those describe the repository rather than tune the loop. The backlog it
keeps, or writes if it is missing, is the one at the `backlog.path` you had.

### A project that keeps AGENTS.md

Claude Code reads `AGENTS.md` only where there is no `CLAUDE.md`, from version
2.1.277 on. So where the repository has an `AGENTS.md` and no `CLAUDE.md`,
`sdlc init` appends the section to `AGENTS.md` and creates no `CLAUDE.md`,
which would hide your `AGENTS.md` from Claude Code:

```console
  created  AGENTS.md (appended to)
```

Where your `CLAUDE.md` is a link to `AGENTS.md`, `init` appends to `AGENTS.md`
and keeps the link, and says `AGENTS.md (appended to; CLAUDE.md links to it)`.
Where `AGENTS.md` is itself a link, to a file shared between projects, `init`
writes a file of the project's own in its place and leaves the shared one
alone: `AGENTS.md (appended to a copy of the file it linked to, which is
unchanged)`. `init` writes nothing outside the project. Where either file
already carries the section, it changes neither.

`sdlc doctor` then says Claude Code reads the section there:

```console
  ok       project contract     AGENTS.md carries the "## SDLC Contract" section and there is no CLAUDE.md, so Claude Code 2.1.277 or later reads it
```

Whether Claude Code reads `AGENTS.md` at all is its Project instructions
setting, which you change with `/config` in Claude Code. Its default,
`claude-md-or-agents-md`, reads it; `claude-md` and `managed-only` do not. In
this layout `doctor` adds a `project instructions` check, which reads the
setting from your user settings only, never writes it, and says that managed
settings, which an organisation sets, were not checked.

A project that has both files keeps the section in one of them. With it in
`AGENTS.md`, `CLAUDE.md` needs a line `@AGENTS.md`, outside any code block,
for Claude Code to read it, and `doctor` reports it until it has one:

```console
  problem  project contract     AGENTS.md carries the "## SDLC Contract" section, but Claude Code reads CLAUDE.md in its place, and CLAUDE.md does not import it
                                fix: add a line @AGENTS.md to CLAUDE.md, outside any code block
```

With the line in place:

```console
  ok       project contract     AGENTS.md carries the "## SDLC Contract" section, and CLAUDE.md imports it with an @AGENTS.md line
```

A `CLAUDE.local.md` at the root is read in place of `AGENTS.md` too, so it
needs the same `@AGENTS.md` line, or can be a link to `AGENTS.md`. A `CLAUDE.md`
that imports `AGENTS.md` is enough even beside a `CLAUDE.local.md` without the
line, because Claude Code reaches `AGENTS.md` through the import, and `doctor`
reports that layout as ok.

A link in place of a file needs Developer Mode on Windows, or an administrator,
and git checks a link out as a plain file where it cannot make one. That is why
the `@AGENTS.md` line, which works everywhere, is the advice for Windows.

## Commit the setup

The `Next:` list that `init` prints starts with `sdlc doctor`. Commit first,
because each story's worktree is cut from trunk's commits and so holds only what
trunk has. Until trunk carries `.sdlc/config.json`, `doctor` reports a `story
worktrees` problem and `sdlc start` is refused with
[SDLC-E0053](../troubleshooting.md#sdlc-e0053):

```console
$ git add .sdlc user_stories.json CLAUDE.md
$ git commit -m "Set up the sdlc loop"
```

You can commit the contract again once you have filled it in. [Deciding what to
commit](#deciding-what-to-commit) covers the rest of the decision.

## Check it with `sdlc doctor`

`init` makes guesses, so the next step is to check them. Run `sdlc doctor` once
the setup is committed:

```console
$ sdlc doctor
sdlc doctor

  ok       git repository       /home/<user>/src/billing
  ok       git command          found on PATH
  ok       configuration        .sdlc/config.json  (Go project)
  ok       backlog              user_stories.json  (1 story, one runnable)
  ok       loop state           the iteration, every gate record, the test freeze and the commit acknowledged all read
  ok       project contract     CLAUDE.md carries the "## SDLC Contract" section
  ok       commands             build, coverage, fmt, fmt_check, fmt_file, lint, smoke, test
  problem  commands on PATH     golangci-lint is not installed, so lint would fail
                                fix: install golangci-lint, or clear commands.lint in .sdlc/config.json
  ok       sdlc on PATH         /usr/local/bin/sdlc

1 problem. The fix is above it.
```

Each problem comes with the fix beneath it. Here the guess was right but the
tool is missing, so you either install it or clear that command. `doctor`
exits non-zero when anything is a problem.

### What `doctor` checks

`doctor` does not run your commands, because a test suite can take minutes.
It takes the first word of each part of a command and looks it up on your
`PATH`, so `test -z "$(gofmt -l .)"` is checked for `gofmt`. A `;` or `|`
inside quotes does not start a new part, so `awk '{a; b}' file` is checked for
`awk` alone. To skip a step you have no tool for, set that command to `""`.

It also asks the hook's classifier whether each command would be refused, and
names the command by its key and the rule, without printing the command. A quiet
`doctor` is not a promise that the hook will allow every command: it judges each
one as the main conversation would run it from the project root, with no story
under way, so it does not judge the agent's role, the freeze, the other
checkouts or whether the story's gates have passed, and it does not judge
`fmt_file`, which the hook runs itself.

The last check asks what the hooks will run. With `SDLC_BIN` set, that is the
program it names, and `doctor` reports a problem when it names nothing
runnable, because the hooks then pass over it and run whatever `sdlc` they
find instead.

If the repository cannot be found, `doctor` marks every check after that as
skipped rather than reporting the same cause several times. If the
configuration cannot be read, it skips the rest too, apart from the loop state
check, which does not need it.

> [!NOTE]
> The contract check only looks for the heading. A section still full of `TBD`
> passes it, which is why the next step is yours.

## Fill in the contract

The `## SDLC Contract` section in `CLAUDE.md` is prose that the runbook and
every agent read. `.sdlc/config.json` holds the values a script can use, and
the contract holds the rules a script cannot check. Each part has a reader:

| Section | Read by |
| --- | --- |
| Phase constraints | the researcher, the test author, the implementer and the architect |
| Architecture rules | the implementer when it plans, and the architect, who blocks on them |
| Test conventions | the test author |
| Domain glossary | the researcher and the human advocate, who flag names that do not match |
| Performance budgets | the performance reviewer, which blocks only when one is breached |
| Security and data rules | the security reviewer, as the template says |

You do not need to fill in every part today. Write rules as sentences someone
could check against a diff, in the project's own words:

```markdown
### Architecture rules (the design reviewer blocks on these)

- Dependency direction: `internal/billing` imports nothing under `internal/http`.
- Errors: billing returns typed errors such as `ErrNonPositiveTotal`; no panics.
- Do-not-touch areas: `internal/ledger/` needs a person before any change.
```

The template asks you to keep the section under about 150 lines, because it is
loaded into every session.

> [!NOTE]
> Edit the contract between stories. While a story is running, the hook refuses
> any attempt by the assistant to write `CLAUDE.md`
> ([`write-protected-path`](../enforcement.md#write-protected-path)).

## Deciding what to commit

What to ignore is your call, and `init` leaves it to you. Committing the setup
is not optional, as [Commit the setup](#commit-the-setup) says. These are the
facts the rest of the decision rests on:

- `sdlc gate commit pass` refuses while `git status --porcelain` shows
  anything, untracked files included. When it runs, every file under `.sdlc/`
  must be either committed or ignored.
- The runbook commits a story with `git add -A`, so anything not ignored goes
  into the same commit as the code. With `git.worktrees` on, that commit is made
  in the story's worktree, on `story/<ID>`: the backlog and the story's records,
  kept in the main checkout, are not in it, and wait there for you to commit.
- Review approvals are stamped with a hash of the tree that leaves out `.sdlc/`
  and the `status` and `updated` fields of the backlog's stories. Committing
  loop files does not make an approval stale.
- Some files change after the commit. Recording the commit and retro gates
  updates `gate-record.json`, the retro adds `RETRO.md`, and `sdlc stop` on a
  finished story removes `state/active` and `state/tests.lock`. Passing the
  retro marks the story `done` in the backlog, except for a story in its own
  worktree, which is marked `done` when `sdlc stop` lands it.
- `state/session` names a Claude Code session, and `sdlc start` rewrites it
  whenever the story is picked up in another one. A clone that brings it along
  holds none of the cloner's sessions to the story until one of them runs
  `/sdlc:next`.
- The freeze only considers files git lists: tracked, or untracked and not
  ignored.

## What lives under `.sdlc/`

You will not need to edit most of what is under `.sdlc/`, but it helps to know
what each file is for, particularly when you decide what to commit. This is the
layout:

```text
.sdlc/
  config.json                   written by init; read by every command and the hook
  templates/story.schema.json   written by init; not read by sdlc itself
  state/
    active                      sdlc start writes it; sdlc stop and a hand-over remove it
    session                     sdlc start writes it in Claude Code; removed with active
    tests.lock                  sdlc freeze writes it
    stop-blocks.json            the Stop hook's count of stops it sent back, and when it began waiting for a subagent
  stories/<ID>/
    gate-record.json            sdlc start creates it; every gate, event and review
    ANALYSIS.md ... RETRO.md    sdlc artifact write
    reviews/                    sdlc review add
  worktrees/
    .gitignore                  sdlc start writes it, holding *
    <ID>/                       the story's own worktree, on the branch story/<ID>
```

With [`git.worktrees`](../configuration.md#git) on, the default, `sdlc start`
writes `state/` in the story's worktree, `.sdlc/worktrees/<ID>`, rather than in
the main checkout: every checkout has a `state/` of its own. `config.json` there
is trunk's committed copy. The backlog and `stories/` stay in the main
checkout, where every checkout reads and writes them.

### The files, one by one

- **`config.json`** makes a project take part. Without it the hook allows
  everything, and `sdlc status`, `sdlc start` and the other loop commands stop
  with [SDLC-E0002](../troubleshooting.md#sdlc-e0002).
- **`templates/story.schema.json`** describes a story for editors and for you.
  The backlog is not validated against it.
- **`state/active`** names the story under way. Its presence is what turns
  enforcement on. Handing a story to a person with `sdlc escalate` ends the
  iteration and removes it.
- **`state/session`** names the Claude Code session working the story. Only
  that session and its agents are held to the story, and the `sdlc` commands
  that change it refuse to run in any other Claude Code session; every other
  session in the repository is otherwise left alone. `sdlc start` in another
  session moves the story there, and outside Claude Code it records none.
- **`state/tests.lock`** is the freeze: every test file's sha256. `sdlc
  unfreeze` removes it, and so does `sdlc stop` on a finished story.
- **`state/stop-blocks.json`** counts the stops the `Stop` hook sent back with
  nothing recorded, up to `loop.max_stop_blocks`. Once a stop has been let
  through because a subagent was still running, `waiting_since` in it is when
  that [wait began](../enforcement.md#stopping-mid-story), in UTC.
- **`stories/<ID>/`** is the story's history: the gate record, the documents
  each gate stored and every review round.

### Files outside `.sdlc/`

Outside `.sdlc/`, the loop also relies on the backlog, whose `status` and
`updated` fields it rewrites, `CLAUDE.md`, and `CODEMAP.md`, which the analysis
and retro agents add to.

The lock that stops two `sdlc` commands writing at once lives in your user
cache directory, not in the repository.

## Where to go next

Now that the loop is set up, you are ready for your first story.

- [Writing stories the loop can run](writing-stories.md)
- [Tuning commands for your stack](tuning-commands-for-your-stack.md)
- [Configuration](../configuration.md): every setting
- [The loop](../the-loop.md#where-everything-lives): what each gate produces
- [Enforcement](../enforcement.md): what the hook refuses while a story runs
