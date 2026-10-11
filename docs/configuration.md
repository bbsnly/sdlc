# Configuration

- [Introduction](#introduction)
- [What `init` writes for a Go project](#what-init-writes-for-a-go-project)
- [`commands`](#commands)
  - [Trusting the commands](#trusting-the-commands)
  - [Formatting each file](#formatting-each-file)
  - [Installing what a worktree needs](#installing-what-a-worktree-needs)
  - [The coverage profile](#the-coverage-profile)
  - [Which configuration a checkout reads](#which-configuration-a-checkout-reads)
  - [At landing](#at-landing)
- [`thresholds`](#thresholds)
- [`paths.tests`](#pathstests)
- [`git`](#git)
  - [How worktrees work](#how-worktrees-work)
  - [What start writes into the repository](#what-start-writes-into-the-repository)
  - [Turning worktrees off](#turning-worktrees-off)
- [`backlog.path`](#backlogpath)
- [`paths.src`](#pathssrc)
- [`spec.paths`](#specpaths)
- [`version`](#version)
- [`loop`](#loop)
- [`reviews.gate7_advisory`](#reviewsgate7_advisory)
- [`freeze.allow_new_test_files`](#freezeallow_new_test_files)
- [`human_gates`](#human_gates)
- [`budget`](#budget)
- [`parallel`](#parallel)
- [`staffing`](#staffing)
  - [Models and efforts](#models-and-efforts)
  - [Setting a floor with `lowest`](#setting-a-floor-with-lowest)
  - [Bringing a reviewer back with `bring_back`](#bringing-a-reviewer-back-with-bring_back)
  - [What a staffing cannot do](#what-a-staffing-cannot-do)
- [The other half of the contract](#the-other-half-of-the-contract)

## Introduction

Everything the loop knows about your project lives in one file,
`.sdlc/config.json`. You do not have to write it: `sdlc init` creates it with
values that already match the stack it found, so the loop works as soon as
`init` has run.

In most projects the only settings you will change are `commands`, the
programs that build, test and check your code, and, if your tests live somewhere
unusual, `paths.tests`. If `commands` is all you need, the
guide on [tuning the commands for your stack](guides/tuning-commands-for-your-stack.md)
is the place to go. The rest of this page describes each setting in turn, from
the ones you are most likely to touch to the ones you may never need.

A field you leave out keeps its documented default. The file is loaded over the
defaults rather than replacing them, so an old configuration keeps working when
a new field arrives.

A key `sdlc` does not know changes nothing, because the setting it was meant to
be keeps its default. `sdlc doctor` names such a key as a problem, so run it
after editing the file. A key that starts with `_` is a comment and is not
reported, and `commands` takes any name.

## What `init` writes for a Go project

Let's take a look at the file `sdlc init` writes for a Go project. It leaves
out the notes `init` writes under `_` keys, and `mutation`, which `init` leaves
empty for Go:

```json
{
  "version": 1,
  "backlog": { "path": "user_stories.json" },
  "git": { "trunk_branch": "main", "remote": false },
  "commands": {
    "smoke": "test -z \"$(go list ./...)\" || go vet ./...",
    "build": "go build ./...",
    "test": "go test ./... -count=1",
    "lint": "golangci-lint run",
    "fmt": "gofmt -w .",
    "fmt_check": "test -z \"$(gofmt -l .)\"",
    "fmt_file": "case \"$FILE\" in *.go) gofmt -w \"$FILE\" ;; esac",
    "coverage": "go test ./... -count=1 -coverprofile=.sdlc/state/cover.out >/dev/null && go tool cover -func=.sdlc/state/cover.out | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'"
  },
  "thresholds": { "coverage_min": 80, "mutation_min": 70 },
  "paths": {
    "tests": { "dirs": ["testdata/"], "file_globs": ["*_test.go"] },
    "src": ["cmd/", "internal/", "pkg/"]
  },
  "spec": { "paths": ["docs/", "spec/", "README.md"] },
  "loop": { "max_review_rounds": 2, "max_rework_rounds": 3, "max_stop_blocks": 3 },
  "reviews": { "gate7_advisory": false },
  "freeze": { "allow_new_test_files": false },
  "human_gates": { "pre_commit_pause_tiers": ["high"], "dor_advocate_check": false },
  "budget": { "per_story_usd": 60, "alert_fractions": [0.5, 0.8, 1.0] }
}
```

The file `init` writes has `\u0026\u0026` where the example shows `&&`, and
`\u003e` where it shows `>`. Both spellings are the same JSON string.

The sections below follow the order of the settings you are most likely to
change, starting with `commands`.

## `commands`

These are how to build, test and check this project. The loop runs them rather
than guessing at a toolchain, and `sdlc doctor` checks that the programs they
name are installed. They are the settings most projects adjust, usually to match
a build system `init` could not guess.

| Key | When it runs |
| --- | --- |
| `build` | the implementer, when the frozen tests pass; the verifier, at Gate 6 |
| `test` | the test author at Gate 3, the implementer after each step, the verifier at Gate 6 |
| `lint` | the implementer, when the frozen tests pass; the verifier, at Gate 6 |
| `fmt` | the implementer, when the frozen tests pass |
| `fmt_check` | the verifier |
| `fmt_file` | the hook, on each file a tool writes while a story is being worked on |
| `coverage` | the verifier, compared against `thresholds.coverage_min` |
| `mutation` | the verifier, compared against `thresholds.mutation_min` |
| `smoke` | `sdlc start`, before each new story: in the story's new worktree, or on trunk with `git.worktrees` off. Keep it fast: it runs before every story |

Any key you add is available to the agents; these are the ones the loop looks
for by name. A key you leave out is simply not run.

### Trusting the commands

> [!WARNING]
> These commands run with your permissions, and `sdlc` runs them itself:
> `smoke` is covered by the prompt for `sdlc start`, and `fmt_file` gets no
> prompt at all.

They are read from `.sdlc/config.json` in the repository, and a clone can bring
`.sdlc/state/active` with it, so in a repository you did not write, read its
`commands` before you work in it, as you would its `.claude/settings.json`.

### Formatting each file

`fmt_file` is run by `sh` from the repository root, with `$FILE` set to the file
that was written. It runs for every file, so a formatter that understands only
some should say which. The commands `sdlc init` writes do:

```json
"fmt_file": "case \"$FILE\" in *.go) gofmt -w \"$FILE\" ;; esac"
```

A formatter that fails is reported to the session with what it printed, and the
file stays as it was written. Nothing is refused: the write has already
happened.

### Installing what a worktree needs

With [`git.worktrees`](#git) on, `smoke` runs in the story's new worktree, and
so does every command a later gate runs, so what the smoke check installs is
what they use. A new worktree has only what trunk commits: no `node_modules`, no
virtual environment and no generated files. A project whose build needs them
installs them in `smoke`, as a Node project does with:

```json
"smoke": "npm ci && npm run build --silent"
```

Commit that to trunk before the next story starts: the worktree reads trunk's
committed `.sdlc/config.json`, so an edit to any command that is not committed
does not reach a new story.

Tools that look upward from where they run find the main checkout's copies from
inside `.sdlc/worktrees/<ID>`: Go's `go.work`, Node's `node_modules`, and
configuration that cascades, such as ESLint's `.eslintrc` and `.editorconfig`. A
`go.work` at the main checkout's root takes over the worktree's build: set
`GOWORK=off` in the commands, or commit a workspace that works from there.
Installing in `smoke` gives the worktree a `node_modules` of its own.

### The coverage profile

One thing worth knowing about the `coverage` command `init` guesses for Go: it
writes a coverage profile to `.sdlc/state/cover.out`. That is a build artefact
rather than loop state, and Gate 8 commits the whole story with `git add -A`, so if
you would rather it did not land in your history, either send the profile
somewhere you already ignore or add the one line:

```gitignore
.sdlc/state/cover.out
```

Nothing decides that for you. `sdlc init` writes no `.gitignore`, because what
a project commits is the project's decision.

### Which configuration a checkout reads

Each checkout reads its own `.sdlc/config.json`. A linked worktree, a story's
own or one made with `git worktree add`, runs the commands and uses the trunk
that its own copy names, while its backlog and story records are the main
checkout's
([more than one checkout](the-loop.md#more-than-one-checkout)). A second clone
is another repository altogether, with a backlog and records of its own, and
nothing stops two clones working the same story. Landing a story is the
exception, described next.

### At landing

A story worked in its worktree lands when `sdlc stop` runs there after the retro
([the loop](the-loop.md#done-when-it-lands)). Where the story was rebased
onto a trunk that had moved, or its change is not the one the reviewers saw, the
landing runs `build`, `test`, `lint`, `fmt_check`, `coverage` and `mutation`,
those that are set, in that order, in the story's worktree, on the commit that
would land. It reads them, and `thresholds`, from `.sdlc/config.json` as trunk
commits it at the tip the landing would move from, never from a checkout's copy
on disk: an uncommitted edit that would make a failing command pass changes
nothing. Trunk is the main checkout's `git.trunk_branch`.

#### What counts as passing

A command passes when it exits 0. `coverage` and `mutation` must also print a
number on standard output, and the last one printed has to reach
`thresholds.coverage_min` or `thresholds.mutation_min`; with the threshold at 0,
no number is needed. The number is looked for in the last 64 KB of standard
output, so a command that prints more than that after its figure, with no digit
in it, is refused as printing none. What a command prints on standard error is
shown when it fails, and is never read for the number.

#### What a command may not change

After each command, HEAD, `story/<ID>` and every tracked file must be as they
were: a command that rewrites a tracked file, as a formatter that writes does,
refuses the landing ([SDLC-E0055](troubleshooting.md#sdlc-e0055)). Each command
runs in a process group of its own, a job object on Windows, and a stop that is
interrupted kills the whole group before it lets the story go. On Windows a stop
killed outright takes the job with it too, and anything a command leaves running
when it ends is ended with the job; a process the command starts in the moment
before it joins its job is not in it.

#### Time, privileges and several stories

The landing sets no time limit of its own. The runbook runs `sdlc stop` with the
Bash tool's longest timeout, 600 seconds, and where the commands need longer, a
person runs that stop in their own terminal.

> [!WARNING]
> That stop runs the story's code, and whatever git settings the repository
> holds, with the person's own privileges and credentials: read the story before
> you run it.

With several stories worked at once, trunk moves each time one lands, so each of
the others is refused with [SDLC-E0054](troubleshooting.md#sdlc-e0054), rebased,
and has its commands run before it lands. With n stories open, the last to land
can go round n - 1 times, which is one more reason to keep the commands fast.

## `thresholds`

These are the numbers the verifier holds a change to. The defaults `init`
writes are usually a fine place to start.

| Key | What it means |
| --- | --- |
| `coverage_min` | the coverage the verifier holds the change to, when `commands.coverage` is configured |
| `mutation_min` | the mutation score the verifier holds the change to, where a project measures one |

A threshold of `0` is not enforced, and a negative one is refused.

## `paths.tests`

This is the one setting worth reading carefully. It decides what the freeze
covers, and therefore what the implementer cannot touch. `sdlc init` fills it in
for the stack it finds, and you only need to change it if your project keeps
tests, or things that tests depend on, somewhere unusual.

| Key | What it means |
| --- | --- |
| `dirs` | a directory and everything beneath it is tests. A bare name, such as `testdata` or `__snapshots__`, is a directory of that name wherever it is, because that is what projects mean by it; one with a slash inside it, like `src/fixtures`, is that directory and no other. A slash at either end does not count: `tests/` is the bare name `tests` |
| `file_globs` | a pattern matched against the whole path *and* against the file's own name, so `*_test.go` finds `internal/store/x_test.go`. `**` stands for any number of directories, so `src/**/*.spec.ts` finds a spec at any depth under `src/`, and `e2e/**` is everything under `e2e/` |

**Anything that decides whether a test passes belongs here, not only the test
files.** A frozen test that reads a golden file is frozen only if the golden
file is too; otherwise the fixture is the way round the freeze. `sdlc init`
covers the usual ones for the stack it finds: `testdata/` for Go, `tests/` and
`fixtures/` for Rust, `conftest.py` and `fixtures/` for Python, `__snapshots__/`,
`__mocks__/` and `*.snap` for Node. If your project keeps them somewhere else,
add it here.

> [!WARNING]
> A Rust unit test inside a source file, under `#[cfg(test)]`, is in no test
> file, so the freeze cannot hold it. Put the acceptance tests under `tests/`.

The test author is refused when it writes outside what this describes, and the
refusal says so.

## `git`

These settings tell the loop which branch is trunk and whether to give each
story a worktree of its own. The defaults suit most repositories.

| Key | What it means |
| --- | --- |
| `trunk_branch` | the branch a new story starts from. With `worktrees` on, each story's branch is cut from it; with `worktrees` off, `sdlc start` begins new work only there, and the story is committed straight to it. `sdlc init` writes the branch `origin`'s `HEAD` names, else `main` or `master` if the repository has one, else the branch it is on; `main` when none of those answers |
| `remote` | whether this repository has an `origin` to keep up with. With it on, `sdlc start` fetches trunk from `origin` and will not begin new work while trunk is behind it. Off by default, and the loop never pushes either way |
| `worktrees` | whether `sdlc start` gives each new story a worktree and a branch of its own: `true` or `false`. Left out, it is on |

### How worktrees work

With `worktrees` on, `sdlc start` makes a new story's worktree at
`.sdlc/worktrees/<ID>` in the main checkout, on a new branch `story/<ID>` cut
from the tip of the local trunk branch, and the story is worked and committed
there. The main checkout keeps its branch, its index and its files, apart from
the loop's own bookkeeping, and need not be on trunk
([more than one checkout](the-loop.md#more-than-one-checkout)).

A story worked in its worktree lands on the trunk the main checkout's
`trunk_branch` names, by fast-forward
([done when it lands](the-loop.md#done-when-it-lands)).

The worktree has only what trunk commits, and it reads trunk's committed
`.sdlc/config.json`, not the copy in your checkout: a change to `commands`,
`thresholds` or any other setting reaches a new story only once it is committed
to trunk. `worktrees` itself is read from the checkout `sdlc start` runs in.

### What start writes into the repository

The first start writes `.sdlc/worktrees/.gitignore`, holding `*`, so that
`git status` in the main checkout does not list the worktrees and `git add -A`
does not take them in; your own `.gitignore` and `.git/info/exclude` are left
alone. That file is never rewritten: one without a `*` line, or with a line
starting `!`, is refused ([SDLC-E0053](troubleshooting.md#sdlc-e0053)). It needs
git 2.17 or later, and `sdlc doctor` says when yours is older.

In each new worktree start also writes `.sdlc/state/.gitignore`, holding `*`,
where there is none, so that what the loop writes there is never the story's to
commit. Ignoring a file does not untrack it: where trunk tracks files under
`.sdlc/state`, no story worked in a worktree can land until trunk stops tracking
them, and `sdlc doctor` says so before one starts.

### Turning worktrees off

`false` is how `sdlc start` worked before: a new story begins in the checkout
it runs in, which has to be on trunk with nothing uncommitted, as
[SDLC-E0038](troubleshooting.md#sdlc-e0038) to
[SDLC-E0041](troubleshooting.md#sdlc-e0041) say.

## `backlog.path`

This is where your stories live, relative to the repository root. The default is
`user_stories.json`, and most projects keep it.

It has to be inside the repository, because the backlog is committed with the
work and protected while a story runs. An absolute path, or one that climbs out
with `..`, is refused.

## `paths.src`

This is where production code lives. The researcher looks there first at Gate 2,
the implementer puts new code there, and the code reviewer reads it with
`paths.tests` to tell a change's code apart from its tests.

Nothing refuses a write outside it. A build file, a migration or a
configuration change can be part of a story, and a rule that refused them would
be worked around rather than followed.

## `spec.paths`

This is where the project's own specifications live. The analysis gate reads
these before it writes anything.

## `version`

This is the configuration format's version, `1`. Leave it alone: it is how a
future release recognises a file written by this one, and how this one refuses a
file written by a newer release rather than misread it. A version of `0` or less
is refused too; leaving the key out reads as `1`
([SDLC-E0045](troubleshooting.md#sdlc-e0045)).

## `loop`

These limits decide when the loop stops trying and hands the story to a person.
The defaults of 2, 3 and 3 are rarely worth changing.

| Key | What it means |
| --- | --- |
| `max_review_rounds` | how many times one reviewer may block a gate before `sdlc review add` hands the story to a person. A block that cannot stop the gate is not counted |
| `max_rework_rounds` | how many times one gate may be recorded as failed before `sdlc gate` hands the story to a person |
| `max_stop_blocks` | how many stops in a row, with nothing recorded on the story, the `Stop` hook sends back before it hands the story to a person. See [stopping mid-story](enforcement.md#stopping-mid-story) |

A limit of `0` turns that limit off, for all three, and a negative one is
refused.

## `reviews.gate7_advisory`

This makes the code reviewer advisory rather than blocking. It is off by
default, and turning it on is a real loosening: Gate 7 is the last thing between
a change and trunk.

Advisory is not skipped. `code_review` still waits for the code reviewer's
report on the change as it stands (a missing or stale review holds the gate as
it always does), but a `block` from it no longer stops the gate.
`sdlc review list` shows it as advisory. The verifier stays blocking, and so
does security on a story Gate 2 called security-sensitive.

## `freeze.allow_new_test_files`

This lets the test author add a test file after the freeze has been taken. It is
off by default.

A new file is not frozen by being written. Until `sdlc freeze` is run again,
which adds it to the freeze, `tests_frozen`, `plan`, `implementation`,
`verification` and `commit` refuse with
[SDLC-E0043](troubleshooting.md#sdlc-e0043); with the setting off, the same
gates refuse and `sdlc freeze` does not add it.

> [!WARNING]
> This is a real loosening, because a test written after the implementation can
> be written to pass. It does **not** let the implementer write one. That rule
> is not a setting.

## `human_gates`

These settings decide where the loop waits for a person.

| Key | What it means |
| --- | --- |
| `pre_commit_pause_tiers` | the story risk tiers that wait for a person's approval before they are committed. Default `["high"]`; `[]` turns the pause off |
| `dor_advocate_check` | have the human advocate read the story at Gate 1 as well. `sdlc gate dor pass` then waits for its review, which is advisory there too. Off by default |

A story's tier is its `risk_tier` in the backlog, and a story without one is
`low`. One in a paused tier reaches Gate 8 and stops: the loop hands it over
with `sdlc escalate pre_commit_approval`, and both `git commit` and
`sdlc gate commit pass` refuse until a person has run
[`sdlc approve`](commands.md#sdlc-approve) in their own terminal. An approval is
bound to the work as it stands, so a change made after it needs approving again.

## `budget`

This is what one story is expected to cost, and when to say so as it gets close.

| Key | What it means |
| --- | --- |
| `per_story_usd` | what one story is expected to cost. `0` turns the budget off and still keeps the total; a negative one is refused |
| `alert_fractions` | the points along the way to say so, as fractions of the budget |

Spend is recorded with [`sdlc cost add`](commands.md#sdlc-cost-add), and shown by `sdlc cost`
and `sdlc status`. An alert goes to standard error once, on the entry that
crosses it, and the entry that uses up the budget says so whichever fractions
are set. Nothing blocks: a story stopped halfway costs more than the overspend.

## `parallel`

This setting matters only if you run several stories at once with
`sdlc parallel --unattended`.

| Key | What it means |
| --- | --- |
| `allow` | narrows the tools [`sdlc parallel --unattended`](commands.md#sdlc-parallel-id) gives each session. Absent: the whole built-in list. A list: only the built-in entries it names. `[]`: none |

The setting can only narrow. The built-in list is stated on the
[enforcement page](enforcement.md#unattended-sessions), and an entry that is not
on it refuses the run ([SDLC-E0066](troubleshooting.md#sdlc-e0066)): this file
is writable by a session on the project, so it never widens what a session may
do. Without `--unattended` the setting is not read.

To let the sessions use a command the list does not have, add an allow rule to
your Claude Code settings, not to this file:
[adding a command](enforcement.md#adding-a-command) says where and what it
hands over.

## `staffing`

Staffing lets a project manager agent say, story by story, which agents the
story needs and what model and effort each is asked for. It is off unless the
project turns it on: with the section absent, or `enabled` false, every agent
takes part in every story, each runs as its definition says, and nothing else in
the section is read.

| Key | What it means |
| --- | --- |
| `enabled` | turns the staffing on. Default `false` |
| `models` | the model names a staffing may ask for, **lowest first** |
| `efforts` | the effort names a staffing may ask for, **lowest first** |
| `lowest` | for an agent, the lowest `model` and `effort` it may be asked for |
| `bring_back` | for a reviewer of the code, the paths a change to which brings it back |

Let's take a look at an example that turns the staffing on:

```json
"staffing": {
  "enabled": true,
  "models": ["haiku", "sonnet", "opus"],
  "efforts": ["low", "medium", "high"],
  "lowest": {
    "verifier": { "model": "opus", "effort": "high" },
    "security": { "model": "sonnet" }
  },
  "bring_back": {
    "security": ["internal/hook/", "install.sh"],
    "perf": ["internal/hot/"]
  }
}
```

> [!WARNING]
> Turning it on is a real loosening. A gate then waits for fewer reviewers than
> it does today, and this file is writable by a session on the project between
> stories.

### Models and efforts

**The first name in each list is the lowest.** sdlc ranks nothing by its name: a
`lowest` entry, and "from the lowest up" in a refusal, are positions in the
project's own lists. A list written highest first allows everything, and nothing
can tell it from one that means it, so
[`sdlc staffing show`](commands.md#sdlc-staffing-show) prints both lists in
order. The names are whatever the project's Claude Code takes for a delegation.
sdlc checks what is asked for against these lists and against nothing else, so a
name mistyped here is asked for as it is typed.

The model and effort are what is asked for. The environment can override both,
and sdlc does not observe what a delegation is given.

### Setting a floor with `lowest`

A key of `lowest` is one of the twelve agents: `researcher`, `sdet`,
`implementer`, `verifier`, `bookkeeper`, `architect`, `security`, `red-team`,
`perf`, `human-advocate`, `code-reviewer`, `project-manager`. An entry may give
a model, an effort or both. An agent with no entry may be asked for from the
first name in each list.

### Bringing a reviewer back with `bring_back`

A key of `bring_back` is a reviewer of the code the staffing may leave out:
`security`, `perf` or `human-advocate`. Each path is a file or a directory
relative to the repository root, with `/` between its parts and no pattern; it
matches by path segment, without regard to case. `sdlc staffing show` prints the
paths as they were read, which is where a path typed wrongly shows: it loads,
and matches nothing.

### What a staffing cannot do

What a staffing cannot do is not a setting: the agents that do a gate's work,
the verifier and the code reviewer are never left out, and neither is security
on a story the analysis recorded as security-sensitive.

Two things it can do are worth knowing before it is on. The architect can be
left out, and nothing brings it back at the design review, where there is no
change to read. And perf can be left out, with `bring_back` all that returns it,
so a project with a performance budget lists its hot paths there.

With the staffing on, a section sdlc cannot use does not load
([SDLC-E0045](troubleshooting.md#sdlc-e0045)): an empty list, a name listed
twice, a `lowest` its list does not have, a key that is not an agent, or a path
that is not a plain relative one.

## The other half of the contract

`.sdlc/config.json` is what the hooks look for: in a project without it they do
nothing at all. The other half is a `## SDLC Contract` section in `CLAUDE.md`,
on a line of its own. The hooks do not read it, but the agents do, and
`sdlc doctor` reports a project that has no such section.

The contract is prose, not settings, and it is what the design reviewer and the
code reviewer hold the work to: architecture rules, phase constraints, test
conventions, allowed dependencies, and the glossary that keeps everyone using
the project's own words. `sdlc init` appends a template with every field marked
`TBD`; filling it in is the highest-value thing you can do before your first
story.

Once a few stories have finished, `/sdlc:consolidate` reads their retros and the
log, and proposes changes to both halves from what repeats. It applies only the
ones you confirm; see
[Consolidating what the retros say](guides/consolidating-what-the-retros-say.md).
