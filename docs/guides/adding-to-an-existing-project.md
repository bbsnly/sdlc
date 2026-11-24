# Adding sdlc to an existing project

- [Introduction](#introduction)
- [Run `sdlc init`](#run-sdlc-init)
  - [What it writes, and what it leaves alone](#what-it-writes-and-what-it-leaves-alone)
  - [Check what it guessed](#check-what-it-guessed)
- [Commit the setup](#commit-the-setup)
- [Check it with `sdlc doctor`](#check-it-with-sdlc-doctor)
- [Fill in the contract](#fill-in-the-contract)
- [What to commit from here on](#what-to-commit-from-here-on)
- [A project that keeps AGENTS.md](#a-project-that-keeps-agentsmd)
  - [With a CLAUDE.md as well](#with-a-claudemd-as-well)
  - [Where one file is a link to the other](#where-one-file-is-a-link-to-the-other)
- [Starting again from the defaults](#starting-again-from-the-defaults)
- [Where to go next](#where-to-go-next)

## Introduction

When a repository already has code, tests, a build and a `CLAUDE.md` of its
own, it is fair to ask what a new tool will touch before you let it in. This
guide answers that, and takes you through adding the loop to such a
repository.

The short answer is reassuring. `sdlc init` adds a handful of files and appends
one section to your `CLAUDE.md`. It does not change your code, your tests, your
build or your `.gitignore`, and it never overwrites a file you wrote.

On this page you will run `init`, commit what it wrote, check its guesses with
`sdlc doctor`, and fill in the contract that the reviewers hold the work to.
You need the `sdlc` command and the plugin installed first:
[Installation](../installation.md) covers both.

## Run `sdlc init`

From anywhere inside the repository, run `sdlc init`, or type `/sdlc:init` in
Claude Code:

```console
$ sdlc init
Set up sdlc in /home/<user>/src/billing

  created  .sdlc/config.json
  created  .sdlc/templates/story.schema.json
  created  user_stories.json
  created  CLAUDE.md (appended to)
  created  REVIEW.md

Guessed the commands from a Go project.

Next:
  1. sdlc doctor              check the guesses
  2. edit user_stories.json   replace the example with a story of your own
  3. /sdlc:next               type it in Claude Code to work the first story
```

Every file `init` wrote is listed as `created`, and a file it found and left
alone is listed as `kept`. Here `CLAUDE.md` was already there, so `init` says
it appended to it.

### What it writes, and what it leaves alone

| File | What `init` does |
| --- | --- |
| `.sdlc/config.json` | writes the loop's settings, with the commands it guessed for your project |
| `.sdlc/templates/story.schema.json` | writes the description of a story, for you and your editor |
| `user_stories.json` | writes a backlog with one example story; a backlog you already have is kept |
| `CLAUDE.md` | appends a `## SDLC Contract` section and leaves the rest as it was; creates the file where there is none |
| `REVIEW.md` | writes the [review policy](../configuration.md#reviewspolicy) every reviewer reads first; one you already have is kept |
| `.sdlc/.gitignore` | keeps each story's cost measurements out of git; it is not in the list `init` prints |

That is everything. Your source, your tests, your build files, your CI and
your own `.gitignore` are left exactly as they were.

> [!NOTE]
> Does your project keep its instructions in `AGENTS.md`? Then the contract
> section goes there, and no `CLAUDE.md` is created.
> [A project that keeps AGENTS.md](#a-project-that-keeps-agentsmd) says what
> changes for you.

### Check what it guessed

To fill in the commands that build, test and lint your project, `init` looks
for a marker file at the root of the repository. It tries four stacks in this
order and takes the first that matches:

| Stack | Marker | Commands it fills in |
| --- | --- | --- |
| Go | `go.mod` | build, test, lint with golangci-lint, format and coverage |
| Rust | `Cargo.toml` | build, test, lint with clippy, and format |
| Node | `package.json` | build, test and lint, each only where your `package.json` has that script; format from your `format` scripts, or from prettier where it is a dependency |
| Python | `pyproject.toml`, `setup.py` or `requirements.txt` | test with pytest, lint and format with ruff, and coverage with pytest-cov |

The line beginning `Guessed the commands from` tells you which one it took. In
a repository with more than one stack, the order decides. A Go service with a
`package.json` for its front-end tooling is set up as Go, and `init` says so:

```text
Guessed the commands from a Go project. This repository also has Node in it,
so check .sdlc/config.json before you rely on it.
```

Take that advice. A Python project with a `package.json` beside it is set up
as Node, which is rarely what you want.

If `init` finds no marker at all, it says so and leaves every command empty:

```text
No stack was recognised, so the commands in .sdlc/config.json are empty.
sdlc looks for Go, Rust, Node and Python.
```

Don't worry if that is your project. Open `.sdlc/config.json` and fill in at
least `test` and `lint` with what you already run.
[Tuning commands for your stack](tuning-commands-for-your-stack.md) shows how,
and how to tell the loop where your tests and your source live.

## Commit the setup

Next, commit what `init` wrote:

```console
$ git add .sdlc user_stories.json CLAUDE.md REVIEW.md
$ git commit -m "Set up the sdlc loop"
```

> [!WARNING]
> Don't skip this commit. Each story is worked in a worktree of its own, a
> second checkout that git cuts from the commits on trunk, the branch your
> finished work lands on. What you have not committed is not there, so `sdlc`
> [refuses to start a story](../troubleshooting.md#sdlc-e0053) until you have.

You will commit `CLAUDE.md` again once you have filled in the contract. That
is fine: the setup is what has to be on trunk before the first story.

## Check it with `sdlc doctor`

`init` makes guesses, so check them. Run `sdlc doctor`, or type `/sdlc:doctor`
in Claude Code:

```console
$ sdlc doctor
sdlc doctor

  ok       git repository       /home/<user>/src/billing
  ok       git command          found on PATH
  ok       configuration        .sdlc/config.json  (Go project)
                                paths.conflict_markers_intended (this checkout's copy; "sdlc stop" reads trunk's): none
  ok       backlog              user_stories.json  (1 story, one runnable)
  ok       loop state           the iteration, every gate record, the test freeze and the commit acknowledged all read
  ok       review policy        REVIEW.md
  ok       project contract     CLAUDE.md carries the "## SDLC Contract" section
  ok       commands             build, coverage, fmt, fmt_check, fmt_file, lint, smoke, test, test_files
  problem  commands on PATH     golangci-lint is not installed, so lint would fail
                                fix: install golangci-lint, or clear commands.lint in .sdlc/config.json
  ok       freeze check         commands.freeze_check is not set, so sdlc freeze runs no check
  ok       sdlc on PATH         /usr/local/bin/sdlc

1 problem. The fix is above it.
```

Read down the left-hand column. Each `problem` has its fix directly beneath
it. Here the guess was right but the tool is missing, so you either install
golangci-lint or set `commands.lint` to `""` to go without a lint step. When
nothing is wrong, the last line reads `Everything checks out.`

`doctor` is quick because it does not run your commands. It only checks that
the program each one starts with is installed, so a test command that is
spelled correctly and fails is something you will find on the first story, not
here. Run your `test` and `lint` commands once by hand if you have any doubt.

> [!NOTE]
> The `project contract` line only looks for the section's heading. A contract
> still full of `TBD` passes it, which is why the next step is yours.

## Fill in the contract

Open `CLAUDE.md` and find the `## SDLC Contract` section that `init` appended.
It is where you tell the agents what good looks like in this project. Your
settings in `.sdlc/config.json` hold what a program can check, such as a test
command. The contract holds the rules only a reader can check, and each part
of it has a reader:

| Part | Who works from it |
| --- | --- |
| Phase constraints | the researcher, the test author, the implementer and the architect |
| Architecture rules | the implementer, when it plans, and the architect, who blocks a plan that breaks one |
| Test conventions | the test author |
| Code conventions | the code reviewer |
| Domain glossary | the researcher and the human advocate, who flag a name that does not match |
| Performance budgets | the performance reviewer, which blocks only when one is breached |
| Security and data rules | the security reviewer |

Most of the fields start as `TBD`. In an existing project you already know the
answers: they are the things you say in code review. Start with the
architecture rules, the test conventions and the glossary, and write each rule
as a sentence somebody could check against a diff:

```markdown
### Architecture rules (the design reviewer blocks on these)

- Dependency direction: `internal/billing` imports nothing under `internal/http`.
- Errors: billing returns typed errors such as `ErrNonPositiveTotal`; no panics.
- Do-not-touch areas: `internal/ledger/` needs a person before any change.
```

Keep the whole section under about 150 lines. It is loaded into every session,
so every line in it costs a little on every story.

When you are done, commit it. A story's worktree has only what trunk has
committed, so a rule you have not committed is a rule the next story never
sees.

> [!NOTE]
> Edit the contract between stories. While a story is running, the hook
> [refuses an agent](../enforcement.md#write-protected-path) that tries to
> write `CLAUDE.md`, so the rules a story is reviewed against cannot be changed
> by the story.

## What to commit from here on

Committing the setup was the one step you could not skip. After that, there is
only one more habit to pick up. When a story lands, two things are left in
your checkout for you to commit:

- the backlog, where the story is now `done`
- the story's directory, `.sdlc/stories/<ID>/`, which holds its record and the
  documents its gates produced: the analysis, the plan, every review and the
  retro

Commit them between stories, when nothing is running. They are plain text, and
they are the history of how each change was made.

Everything else under `.sdlc/` looks after itself:

| Path | In git? |
| --- | --- |
| `.sdlc/config.json`, `.sdlc/templates/`, `.sdlc/.gitignore` | yes: you committed them with the setup |
| `.sdlc/stories/<ID>/` | yes, once the story has landed |
| `.sdlc/stories/<ID>/trace.jsonl` | no: what the story cost, as measured on your machine, ignored by `.sdlc/.gitignore` |
| `.sdlc/worktrees/` | no: the stories' own checkouts, which ignore themselves |

You don't need to add anything to your own `.gitignore`. To commit the cost
measurements as well, empty `.sdlc/.gitignore`. Don't delete it, because
`sdlc` writes it again when the next story starts.

[The loop](../the-loop.md#where-everything-lives) shows every file under
`.sdlc/` and which gate writes it.

> [!NOTE]
> All of this assumes each story has a worktree of its own, which is the
> default. If you [turn worktrees off](../configuration.md#turning-worktrees-off),
> stories are worked and committed in your own checkout, and it has to be on
> trunk with nothing uncommitted before one can start.

## A project that keeps AGENTS.md

Claude Code reads `AGENTS.md` only where there is no `CLAUDE.md`, from version
2.1.277 on. So where your repository has an `AGENTS.md` and no `CLAUDE.md`,
`sdlc init` appends the contract to `AGENTS.md` and creates no `CLAUDE.md`,
which would hide your `AGENTS.md` from Claude Code. Its list then has this
line in place of the one for `CLAUDE.md`:

```text
  created  AGENTS.md (appended to)
```

Everything else on this page works the same way. Wherever it says
`CLAUDE.md`, read `AGENTS.md`, including in the `git add` line when you commit
the setup. `sdlc doctor` confirms that Claude Code will read the section
there:

```text
  ok       project contract     AGENTS.md carries the "## SDLC Contract" section and there is no CLAUDE.md, so Claude Code 2.1.277 or later reads it
```

Whether Claude Code reads `AGENTS.md` at all is its Project instructions
setting, which you change with `/config` in Claude Code. The default reads it.
In this layout `doctor` adds a `project instructions` line that checks the
setting in your own Claude Code settings, and reports a problem if it has been
changed to one that skips `AGENTS.md`. It cannot see settings your organisation
manages, and says so.

### With a CLAUDE.md as well

A project that has both files keeps the contract in one of them. Where it is
in `AGENTS.md`, Claude Code reads `CLAUDE.md` in its place, so `CLAUDE.md` has
to pull `AGENTS.md` in. Until it does, `doctor` reports it:

```text
  problem  project contract     AGENTS.md carries the "## SDLC Contract" section, but Claude Code reads CLAUDE.md in its place, and CLAUDE.md does not import it
                                fix: add a line @AGENTS.md to CLAUDE.md, outside any code block
```

Add the line `@AGENTS.md` to `CLAUDE.md`, on a line of its own and outside any
code block, and the check passes:

```text
  ok       project contract     AGENTS.md carries the "## SDLC Contract" section, and CLAUDE.md imports it with an @AGENTS.md line
```

A `CLAUDE.local.md` at the root is read in place of `AGENTS.md` too. It is
covered once `CLAUDE.md` has the `@AGENTS.md` line. Where there is no
`CLAUDE.md`, give `CLAUDE.local.md` the line.

### Where one file is a link to the other

Some projects make `CLAUDE.md` a link to `AGENTS.md`, so that every tool reads
the same file. `sdlc init` appends to `AGENTS.md` and keeps the link, and its
list says `AGENTS.md (appended to; CLAUDE.md links to it)`.

Where `AGENTS.md` is itself a link, to a file shared between projects, `init`
writes nothing outside your project. It puts a file of the project's own in
place of the link, with the contract appended, and leaves the shared file
unchanged.

On Windows, a link in place of a file needs Developer Mode or an
administrator, and git checks a link out as a plain file where it cannot make
one. So on Windows, prefer the `@AGENTS.md` line, which works everywhere.

## Starting again from the defaults

Run `init` a second time and it
[stops rather than lose your settings](../troubleshooting.md#sdlc-e0003). To
start again on purpose, pass `--force`:

```console
$ sdlc init --force
```

`--force` detects your stack afresh and puts every setting back to its
default. It keeps the things that describe your repository: where your backlog
is, what your trunk branch is called, and a review policy you have moved. It
never touches your stories, your `REVIEW.md` or a contract section that is
already there. Run it between stories: it is
[refused](../troubleshooting.md#sdlc-e0012) while one is being worked on.

## Where to go next

Now that the loop is set up, you are ready for your first story.

- [Writing stories](writing-stories.md): acceptance criteria the loop can turn
  into tests
- [Tuning commands for your stack](tuning-commands-for-your-stack.md): make the
  commands and paths match what your project runs
- [Getting started](../getting-started.md#run-your-first-story): what you will
  see when the first story runs
- [Configuration](../configuration.md): every setting in `.sdlc/config.json`
- [Enforcement](../enforcement.md): what the hook holds an agent to while a
  story runs
