# Adding sdlc to an existing project

This guide puts the loop on a repository that already has code, tests and a
build of its own. You will run `sdlc init`, check what it guessed with
`sdlc doctor`, fill in the contract the reviewers hold the work to, and learn
what each file under `.sdlc/` is for, so that you can decide what belongs in
version control.

It assumes the `sdlc` binary is on your `PATH` and the plugin is installed in
Claude Code. [Installation](../installation.md) covers both.

## Run `sdlc init`

From anywhere inside the repository:

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
  3. /sdlc:next               in Claude Code, to work the first story
```

`init` walks up to the nearest `.git` and writes everything relative to that
directory, so it does not matter which subdirectory you run it from.

### What it detects

`init` looks for a marker file in the repository root. It tries the stacks in
this order and takes the first match:

| Stack | Marker | Commands it fills in |
| --- | --- | --- |
| Go | `go.mod` | `smoke`, `build`, `test`, `lint` (golangci-lint), `fmt`, `fmt_check`, `fmt_file`, `coverage` |
| Rust | `Cargo.toml` | `smoke`, `build`, `test`, `lint` (clippy), `fmt`, `fmt_check` |
| Node | `package.json` | `build`, `test`, `lint` for each script that exists; `fmt` and `fmt_check` from `format` / `format:check` scripts or a `prettier` dependency; `fmt_file` from a `prettier` dependency; `smoke` is `build` |
| Python | `pyproject.toml`, `setup.py` or `requirements.txt` | `test` (pytest), `lint`, `fmt`, `fmt_check`, `fmt_file` (ruff), `coverage` (pytest-cov) |

For Node, a command is only wired to a script your `package.json` already has.
A command pointing at a missing script would fail every gate on a project that
is otherwise fine. A `package.json` that does not parse still counts as Node,
with no commands filled in.

Each stack also sets `paths.tests` and `paths.src`.
[Tuning commands for your stack](tuning-commands-for-your-stack.md) explains
what they do and how to change them.

The order matters in a mixed repository. A Go service with a `package.json`
for its front-end tooling is set up as Go, and a Python project with one is set
up as Node. `init` says when it saw more than one:

```console
Guessed the commands from a Go project. This repository also has Node in it,
so check .sdlc/config.json before you rely on it.
```

With no marker at all, every command is empty, `paths.tests` gets `tests/`,
`test/`, `testdata/` and `fixtures/` plus the built-in file patterns, and
`paths.src` is `src/`.

### What it writes, and what it leaves alone

| File | First run | `sdlc init --force` |
| --- | --- | --- |
| `.sdlc/config.json` | written | rewritten from a fresh detection and the defaults, keeping `backlog.path` and `git.trunk_branch` |
| `.sdlc/templates/story.schema.json` | written | rewritten |
| `user_stories.json` | written with one example story, unless it exists | kept |
| `CLAUDE.md` | created, or the `## SDLC Contract` section appended | section appended only if missing |

Run `init` a second time without `--force` and it stops with
[SDLC-E0003](../troubleshooting.md#sdlc-e0003) rather than lose your settings.
`--force` replaces every other setting with its default, but keeps where your
stories are and what your trunk branch is called: those describe the
repository rather than tune the loop. The backlog it keeps, or writes if it is
missing, is the one at the `backlog.path` you had.

`init` and `doctor` look for the contract as a `## SDLC Contract` heading on a
line of its own. A `### SDLC Contract` heading, or a sentence that mentions the
words, does not count, and `init` appends a section of its own.

`init` writes no `.gitignore`.

## Check it with `sdlc doctor`

```console
$ sdlc doctor
sdlc doctor

  ok       git repository       /home/<user>/src/billing
  ok       git command          found on PATH
  ok       configuration        .sdlc/config.json  (Go project)
  ok       backlog              user_stories.json  (1 story, one runnable)
  ok       loop state           the iteration, every gate record and the test freeze all read
  ok       project contract     CLAUDE.md carries the "## SDLC Contract" section
  ok       commands             build, coverage, fmt, fmt_check, fmt_file, lint, smoke, test
  problem  commands on PATH     golangci-lint is not installed, so lint would fail
                                fix: install golangci-lint, or clear commands.lint in .sdlc/config.json
  ok       sdlc on PATH         /usr/local/bin/sdlc

1 problem. The fix is above it.
```

`doctor` does not run your commands, because a test suite can take minutes.
It takes the first word of each part of a command and looks it up on your
`PATH`, so `test -z "$(gofmt -l .)"` is checked for `gofmt`. To skip a step
you have no tool for, set that command to `""`.

The last check asks what the hooks will run. With `SDLC_BIN` set, that is the
program it names, and `doctor` reports a problem when it names nothing
runnable, because the hooks then pass over it and run whatever `sdlc` they
find instead.

It exits non-zero when anything is a problem. If the repository cannot be
found, it marks every check after that as skipped rather than reporting the
same cause several times. If the configuration cannot be read, it skips the
rest too, apart from the loop state check, which does not need it.

The contract check only looks for the heading. A section still full of `TBD`
passes it, which is why the next step is yours.

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

Write rules as sentences someone could check against a diff, in the project's
own words:

```markdown
### Architecture rules (the design reviewer blocks on these)

- Dependency direction: `internal/billing` imports nothing under `internal/http`.
- Errors: billing returns typed errors such as `ErrNonPositiveTotal`; no panics.
- Do-not-touch areas: `internal/ledger/` needs a person before any change.
```

The template asks you to keep the section under about 150 lines, because it is
loaded into every session.

Edit the contract between stories. While a story is running, the hook refuses
any attempt by the assistant to write `CLAUDE.md`
([`write-protected-path`](../enforcement.md#write-protected-path)).

## What lives under `.sdlc/`

```text
.sdlc/
  config.json                   written by init; read by every command and the hook
  templates/story.schema.json   written by init; not read by sdlc itself
  state/
    active                      sdlc start writes it; sdlc stop and a hand-over remove it
    tests.lock                  sdlc freeze writes it
    stop-blocks.json            the Stop hook's count of stops it sent back
    cover.out                   only if the Go coverage command has run
  stories/<ID>/
    gate-record.json            sdlc start creates it; every gate, event and review
    ANALYSIS.md ... RETRO.md    sdlc artifact write
    reviews/                    sdlc review add
```

- **`config.json`** makes a project take part. Without it the hook allows
  everything, and `sdlc status`, `sdlc start` and the other loop commands stop
  with [SDLC-E0002](../troubleshooting.md#sdlc-e0002).
- **`templates/story.schema.json`** describes a story for editors and for you.
  The backlog is not validated against it.
- **`state/active`** names the story under way. Its presence is what turns
  enforcement on. Handing a story to a person with `sdlc escalate` ends the
  iteration and removes it.
- **`state/tests.lock`** is the freeze: every test file's sha256. `sdlc
  unfreeze` removes it, and so does `sdlc stop` on a finished story.
- **`state/stop-blocks.json`** counts the stops the `Stop` hook sent back with
  nothing recorded, up to `loop.max_stop_blocks`.
- **`stories/<ID>/`** is the story's history: the gate record, the documents
  each gate stored and every review round.

Outside `.sdlc/`, the loop also relies on the backlog, whose `status` and
`updated` fields it rewrites, `CLAUDE.md`, and `CODEMAP.md`, which the analysis
and retro agents add to.

The lock that stops two `sdlc` commands writing at once lives in your user
cache directory, not in the repository.

## Deciding what to commit

That is your call. These are the facts it rests on:

- `sdlc gate commit pass` refuses while `git status --porcelain` shows
  anything, untracked files included. When it runs, every file under `.sdlc/`
  must be either committed or ignored.
- The runbook commits a story with `git add -A`, so anything not ignored goes
  into the same commit as the code.
- Review approvals are stamped with a hash of the tree that leaves out `.sdlc/`
  and the `status` and `updated` fields of the backlog's stories. Committing
  loop files does not make an approval stale.
- Some files change after the commit. Recording the commit and retro gates
  updates `gate-record.json`, the retro adds `RETRO.md`, passing the retro marks
  the story `done` in the backlog, and `sdlc stop` on a finished story removes
  `state/active` and `state/tests.lock`.
- The freeze only considers files git lists: tracked, or untracked and not
  ignored.

## Where to go next

- [Writing stories the loop can run](writing-stories.md)
- [Tuning commands for your stack](tuning-commands-for-your-stack.md)
- [Configuration](../configuration.md): every setting
- [The loop](../the-loop.md#where-everything-lives): what each gate produces
- [Enforcement](../enforcement.md): what the hook refuses while a story runs
