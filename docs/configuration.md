# Configuration

- [Introduction](#introduction)
- [The configuration file](#the-configuration-file)
  - [Comments in the file](#comments-in-the-file)
- [Changing a setting](#changing-a-setting)
  - [When a change takes effect](#when-a-change-takes-effect)
  - [When a setting is wrong](#when-a-setting-is-wrong)
- [The settings you will change most](#the-settings-you-will-change-most)
  - [Match the commands to your project](#match-the-commands-to-your-project)
  - [Set the numbers a change is held to](#set-the-numbers-a-change-is-held-to)
  - [Choose where stories are worked](#choose-where-stories-are-worked)
  - [Choose where the loop waits for you](#choose-where-the-loop-waits-for-you)
  - [Shape the team](#shape-the-team)
  - [Set a budget](#set-a-budget)
- [Every setting at once](#every-setting-at-once)
- [`commands`](#commands)
  - [Trusting the commands](#trusting-the-commands)
  - [Installing what a worktree needs](#installing-what-a-worktree-needs)
  - [The narrow test command](#the-narrow-test-command)
  - [The run directory](#the-run-directory)
  - [Coverage and mutation](#coverage-and-mutation)
  - [The freeze check](#the-freeze-check)
  - [At landing](#at-landing)
- [`thresholds`](#thresholds)
- [`paths`](#paths)
  - [`paths.tests`](#pathstests)
  - [`paths.src`](#pathssrc)
  - [`paths.conflict_markers_intended`](#pathsconflict_markers_intended)
- [`git`](#git)
  - [Turning worktrees off](#turning-worktrees-off)
- [`human_gates`](#human_gates)
- [`team`](#team)
  - [An agent's entry](#an-agents-entry)
  - [Models, efforts and floors](#models-efforts-and-floors)
  - [Bringing a reviewer back](#bringing-a-reviewer-back)
  - [What the project manager cannot leave out](#what-the-project-manager-cannot-leave-out)
  - [Upgrading from the staffing section](#upgrading-from-the-staffing-section)
- [`budget`](#budget)
- [`loop`](#loop)
- [`freeze`](#freeze)
  - [`freeze.allow_new_test_files`](#freezeallow_new_test_files)
- [`reviews`](#reviews)
  - [`reviews.policy`](#reviewspolicy)
  - [`reviews.gate7_advisory`](#reviewsgate7_advisory)
- [`backlog`](#backlog)
  - [`backlog.path`](#backlogpath)
- [`spec`](#spec)
  - [`spec.paths`](#specpaths)
- [`consolidate`](#consolidate)
- [`version`](#version)
- [Settings read from the environment](#settings-read-from-the-environment)
  - [`SDLC_HOOK_DEADLINE_MS`](#sdlc_hook_deadline_ms)
- [The other half of the contract](#the-other-half-of-the-contract)
- [Where to go next](#where-to-go-next)

## Introduction

Everything `sdlc` knows about your project is in two places. The file
`.sdlc/config.json`, at the root of your repository, holds what a program can
check: the commands that build and test your code, the numbers a change is
held to, which files are tests, and where the loop waits for you. The
`## SDLC Contract` section of your `CLAUDE.md` holds the rules only a reader
can check.

You don't have to write either from nothing. `sdlc init` creates both, with
settings that already match the stack it found, so the loop works as soon as
`init` has run. Most projects then change a handful of settings and leave the
rest alone.

This page starts with the file and how to change it safely, and then walks
through the few settings you are most likely to change. After that it is a
reference: every setting, with its default, an example, and what changes when
you change it.

## The configuration file

Let's take a look at the file `sdlc init` writes for a Go project. This is all
of it, apart from the comment blocks and the two commands `init` leaves empty
for Go, `mutation` and `freeze_check`:

```json
{
  "version": 1,
  "backlog": { "path": "user_stories.json" },
  "git": { "trunk_branch": "main", "remote": false },
  "commands": {
    "smoke": "test -z \"$(go list ./...)\" || go vet ./...",
    "build": "go build ./...",
    "test": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then go test ./... -count=1 -coverprofile=\"$SDLC_RUN_DIR/cover.out\"; else go test ./... -count=1; fi",
    "test_files": "if [ -n \"${SDLC_TEST_FILES-}\" ]; then sed -e 's#^#./#' -e 's#/testdata/.*#/x#' -e 's#/[^/]*$##' \"$SDLC_TEST_FILES\" | tr '\\n' '\\0' | xargs -0 go test -count=1; else go test ./... -count=1; fi",
    "lint": "golangci-lint run",
    "fmt": "gofmt -w .",
    "fmt_check": "test -z \"$(gofmt -l .)\"",
    "fmt_file": "case \"$FILE\" in *.go) gofmt -w \"$FILE\" ;; esac",
    "coverage": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then test -f \"$SDLC_RUN_DIR/cover.out\" && go tool cover -func=\"$SDLC_RUN_DIR/cover.out\" | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'; else p=$(mktemp) && trap 'rm -f \"$p\"' EXIT && go test ./... -count=1 -coverprofile=\"$p\" >/dev/null && go tool cover -func=\"$p\" | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'; fi"
  },
  "thresholds": { "coverage_min": 80, "mutation_min": 70 },
  "paths": {
    "tests": { "dirs": ["testdata/"], "file_globs": ["*_test.go"] },
    "src": ["cmd/", "internal/", "pkg/"]
  },
  "spec": { "paths": ["docs/", "spec/", "README.md"] },
  "loop": { "max_review_rounds": 2, "max_rework_rounds": 3, "max_stop_blocks": 3 },
  "team": {
    "retries": 2,
    "models": ["haiku", "sonnet", "opus"],
    "efforts": ["low", "medium", "high", "xhigh", "max"],
    "agents": {
      "project-manager": { "enabled": true, "instances": 1 },
      "researcher":      { "enabled": true, "instances": 1 },
      "sdet":            { "enabled": true, "instances": 1 },
      "implementer":     { "enabled": true, "instances": 1 },
      "verifier":        { "enabled": true, "can_block": true, "instances": 1 },
      "bookkeeper":      { "enabled": true, "instances": 1 },
      "architect":       { "enabled": true, "can_block": true, "instances": 1 },
      "security":        { "enabled": true, "can_block": true, "instances": 1 },
      "red-team":        { "enabled": true, "can_block": false, "instances": 1 },
      "perf":            { "enabled": true, "can_block": true, "instances": 1 },
      "human-advocate":  { "enabled": true, "can_block": false, "instances": 1 },
      "code-reviewer":   { "enabled": true, "can_block": true, "instances": 1 },
      "test-reviewer":   { "enabled": true, "can_block": true, "instances": 1 }
    }
  },
  "freeze": { "allow_new_test_files": false },
  "human_gates": { "pre_commit_pause_tiers": ["high"], "pre_commit_pause_paths": [], "dor_advocate_check": false },
  "budget": { "per_story_usd": 60, "alert_fractions": [0.5, 0.8, 1.0] }
}
```

The `test`, `test_files` and `coverage` commands are long because each does
two jobs: one when the loop runs it, and one when you run it by hand.
[The narrow test command](#the-narrow-test-command) and
[The run directory](#the-run-directory) explain them.

A Rust, Node or Python project gets the same file with its own
[`commands`](#commands) and [`paths`](#paths), and everything else as it is
here. The file is plain JSON, so you may see `\u0026\u0026` where this page
shows `&&`, and `\u003e` for `>`. That is how `init` writes them, and both
spellings mean the same.

### Comments in the file

`sdlc init` writes a comment block above each setting. A block is a list of
lines under a key made of `_` and the setting's name: a title, what the
setting controls and what changing it costs, and the address of that setting's
entry on this page. This is the one above `thresholds`:

```json
  "_thresholds": [
    "The numbers a change is held to",
    "coverage_min and mutation_min are the percentages the verifier compares a change with.",
    "Lowering one lets weaker work through; 0 turns it off, and a negative number is refused.",
    "https://github.com/bbsnly/sdlc/blob/main/docs/configuration.md#thresholds"
  ],
  "thresholds": { "coverage_min": 80, "mutation_min": 70 },
```

A top-level key that starts with `_` is never read as a setting, so the blocks
change nothing, and you may add notes of your own in the same way. Keep them
at the top level: inside `commands` every key is read as a command, whatever
it is called.

## Changing a setting

The file is yours to edit by hand. Change it on trunk, in your main checkout,
between stories, and then check it and commit it:

```console
$ sdlc doctor
$ git add .sdlc/config.json
$ git commit -m "Raise the coverage threshold"
```

`sdlc doctor` reads the file the way the loop will. It names a key it does not
know, a value it cannot use and a command whose program is not installed, each
with its fix. It never runs your commands.

> [!WARNING]
> Commit the change to trunk before you start the next story. Each story is
> worked in a worktree cut from what trunk commits, so an edit you have not
> committed never reaches it. Don't edit the file inside a story's worktree
> either: no story lands a change under `.sdlc/`, so that story is
> [refused at landing](troubleshooting.md#sdlc-e0055).

A setting you leave out keeps its default, so a short file is a valid one, and
an old file keeps working when a new setting arrives. While a story is being
worked on, the hook refuses an agent's attempt to change the file, so a story
is never held to settings it rewrote itself.

To put every setting back to its default, there is `sdlc init --force`. It
keeps your backlog's path, your trunk branch and a review policy you have
moved, and nothing else: your commands and your team are written afresh.
[Starting again from the defaults](guides/adding-to-an-existing-project.md#starting-again-from-the-defaults)
says when that is worth it.

### When a change takes effect

Each checkout reads its own `.sdlc/config.json`. A story's worktree has the
file trunk had committed when the story started, and that is the one its gates
read. So a change reaches the stories you start after it is on trunk, and a
story already under way keeps the settings it started with.

Two things are read from somewhere else:

- **The landing** reads `commands` and `thresholds` as trunk commits them at
  that moment, never from a file on disk. [At landing](#at-landing) says what
  it runs.
- **`git.worktrees`** is read from the checkout you run `sdlc start` in, since
  it decides whether there will be a worktree at all.

The simplest course with a story under way is to let it finish. If it cannot
wait, commit the change on trunk and bring the story onto the new trunk, as
[a story in flight](#upgrading-from-the-staffing-section) is brought there.

A second clone of the repository is another project as far as `sdlc` can
tell, with a backlog and records of its own. Nothing stops two clones working
the same story.

### When a setting is wrong

A key `sdlc` does not know changes nothing, because the setting it was meant
to be keeps its default. No command stops for it, so run `sdlc doctor` after
every edit. With `thresholds` misspelt, the first line of its `configuration`
check reads:

```text
  problem  configuration        .sdlc/config.json has settings sdlc does not know, which change nothing: threshholds
```

A value `sdlc` cannot use is another matter. Every command refuses the file
until you correct it, and says which setting it is:

```text
sdlc: .sdlc/config.json has settings sdlc cannot use

  why  thresholds.coverage_min is -5, and 0 is how it is turned off
  fix  change the settings the message names in .sdlc/config.json — docs/configuration.md says what each one takes; for a newer version, upgrade sdlc

  SDLC-E0045  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0045
```

Fix a refused file before the next story. The hook cannot read it either, and
until it can, it works from the defaults: your own test paths and freeze
setting do not apply.

## The settings you will change most

Six groups of settings account for nearly every edit. Each is described
briefly here, with a link to its full entry further down.

### Match the commands to your project

`commands` are the programs that build, test and check your code. `init`
guesses them from your stack, and it cannot know that you run your tests
through `make`. Put in what you type yourself:

```json
"commands": {
  "build": "make build",
  "test": "make test",
  "lint": "make lint"
}
```

Leave the other keys as they are. A command set to `""` is skipped.
[`commands`](#commands) lists every key and who runs it, and
[Tuning commands for your stack](guides/tuning-commands-for-your-stack.md)
goes through the job for Go, Rust, Node and Python.

### Set the numbers a change is held to

`thresholds` are the percentages a change has to reach before its
implementation is recorded as done at Gate 5:

```json
"thresholds": { "coverage_min": 85, "mutation_min": 0 }
```

A threshold of `0` is not enforced. [`thresholds`](#thresholds) says what each
number is compared with.

### Choose where stories are worked

By default every story gets a worktree of its own, a second checkout under
`.sdlc/worktrees/`, and your own checkout is left alone. To have stories
worked in your checkout instead, one at a time, turn worktrees off:

```json
"git": { "trunk_branch": "main", "remote": false, "worktrees": false }
```

Read [Turning worktrees off](#turning-worktrees-off) first. It changes what
has to be committed, and when.

### Choose where the loop waits for you

`human_gates` says which stories wait for your approval before they are
committed. With the default, those are the stories you marked
`"risk_tier": "high"`. To see the medium ones as well, and any story that
touches your authentication code, whatever its tier:

```json
"human_gates": {
  "pre_commit_pause_tiers": ["medium", "high"],
  "pre_commit_pause_paths": ["src/auth/**"],
  "dor_advocate_check": false
}
```

[`human_gates`](#human_gates) has all three settings, and
[Human approval gates](guides/human-approval-gates.md) shows what the pause
looks like and how to answer it.

### Shape the team

`team` has a line for each of the thirteen agents. You may turn one off, decide
whether a reviewer's block stops a gate, or ask for two reviews where there
was one:

```json
"security": { "enabled": true, "can_block": true, "instances": 2 },
"red-team": { "enabled": true, "can_block": true, "instances": 1 },
```

Two of these settings loosen the loop, so read [`team`](#team) before you
commit a change to it.
[Change the team for every story](the-agents.md#change-the-team-for-every-story)
walks through an example.

### Set a budget

`budget` is what you expect one story to cost, and the points along the way at
which `sdlc` warns you:

```json
"budget": { "per_story_usd": 40, "alert_fractions": [0.5, 0.9] }
```

A budget warns and never stops a story. [`budget`](#budget) has the details,
and [Tracking what a story costs](guides/tracking-what-a-story-costs.md) shows
the warnings as they appear.

## Every setting at once

Here is the whole file, with every setting at its default and a comment
naming the values it can take. The `//` comments are for reading here. The
file itself is plain JSON, which takes none: a file with a `//` in it is
refused as [SDLC-E0004](troubleshooting.md#sdlc-e0004), so leave them out of
your copy. To annotate your own file, use a top-level key that starts with
`_`, as [`sdlc init` does](#comments-in-the-file).

`init` writes most of this for a new project. The example also shows the
settings `init` leaves out, each at the value its absence means, so that none
of them changes anything: `git.worktrees`, `paths.conflict_markers_intended`,
`reviews.policy`, `budget.gate_hours` and `consolidate`, and on `security` the
three keys any agent's entry may hold, `max_rounds`, `lowest` and
`bring_back`.

```jsonc
{
  "version": 1,                              // the file's format; leave it at 1
  "backlog": { "path": "user_stories.json" }, // relative to the repository root, and inside it
  "git": {
    "trunk_branch": "main",                  // the branch stories start from and land on
    "remote": false,                         // true or false; true: sdlc start fetches from origin and refuses while trunk is behind
    "worktrees": true                        // true or false; false: stories are worked in this checkout, one at a time
  },
  // each a command run from the repository root; "" skips the step that runs it
  "commands": {
    "smoke": "",
    "build": "",
    "test": "",
    "test_files": "",
    "lint": "",
    "fmt": "",
    "fmt_check": "",
    "fmt_file": "",
    "coverage": "",
    "mutation": "",
    "freeze_check": ""
  },
  "thresholds": { "coverage_min": 80, "mutation_min": 70 }, // percentages; 0 turns one off
  "paths": {
    "tests": {
      "dirs": ["tests/", "test/"],
      "file_globs": ["*_test.go", "*.test.ts", "*.spec.ts", "test_*.py", "*_test.py", "*Test.java", "*_spec.rb"]
    },
    "src": ["src/", "internal/", "pkg/", "cmd/", "lib/"],
    "conflict_markers_intended": []          // files that hold conflict markers on purpose
  },
  "spec": { "paths": ["docs/", "spec/", "README.md"] },
  // whole numbers, 0 or more; 0 turns that limit off
  "loop": { "max_review_rounds": 2, "max_rework_rounds": 3, "max_stop_blocks": 3 },
  "team": {
    "retries": 2,                            // 0 to 20: runs of an agent that may fail at a gate before the story is handed to you
    "models": ["haiku", "sonnet", "opus"],   // the models a staffing may ask for, lowest first
    "efforts": ["low", "medium", "high", "xhigh", "max"], // the efforts a staffing may ask for, lowest first
    "agents": {
      // enabled: true or false; false: the agent is not run, and a worker's gate, or the
      //   verifier's or the code reviewer's review, is yours to do by hand
      // instances: 1 to 16, how many instances of a reviewer review each gate it reviews
      // can_block and instances act on an agent that reviews; on the others they do nothing
      "project-manager": { "enabled": true, "instances": 1 },
      "researcher":      { "enabled": true, "instances": 1 },
      "sdet":            { "enabled": true, "instances": 1 },
      "implementer":     { "enabled": true, "instances": 1 },
      // can_block: true or false; false: its block is advisory and does not stop the gate
      "verifier":        { "enabled": true, "can_block": true, "instances": 1 },
      "bookkeeper":      { "enabled": true, "instances": 1 },
      "architect":       { "enabled": true, "can_block": true, "instances": 1 },
      "security": {
        "enabled": true,
        "can_block": true,
        "instances": 1,
        // max_rounds: 1 or more, the blocks that stop the gate before it goes to the
        //   agent's arbiter, and the arbiter's before the story is handed to you; left
        //   out, it is loop.max_review_rounds
        "max_rounds": 2,
        // lowest: the floor the project manager may not staff this agent below: a model
        //   from team.models (haiku, sonnet, opus) and an effort from team.efforts
        //   (low, medium, high, xhigh, max)
        "lowest": { "model": "haiku", "effort": "low" },
        // bring_back: paths relative to the project's root; a change touching one brings
        //   this agent back where the staffing left it out; only an agent that reviews the
        //   code takes it: security, perf, human-advocate and code-reviewer
        "bring_back": []
      },
      "red-team":        { "enabled": true, "can_block": false, "instances": 1 },
      "perf":            { "enabled": true, "can_block": true, "instances": 1 },
      "human-advocate":  { "enabled": true, "can_block": false, "instances": 1 },
      "code-reviewer":   { "enabled": true, "can_block": true, "instances": 1 },
      "test-reviewer":   { "enabled": true, "can_block": true, "instances": 1 }
    }
  },
  // the file every reviewer reads, relative to the repository root; leave it out to use
  // REVIEW.md, and name it only to move the file: sdlc doctor warns where the named file
  // is not there
  "reviews": { "policy": "REVIEW.md" },
  "freeze": { "allow_new_test_files": false }, // true or false; true: a test file may be added after the freeze
  "human_gates": {
    "pre_commit_pause_tiers": ["high"],      // any of the risk tiers low, medium and high; [] pauses no tier
    "pre_commit_pause_paths": [],            // globs from the repository root, such as "src/auth/**"; [] pauses on no path
    "dor_advocate_check": false              // true or false; true: the human advocate reads each story at Gate 1 too
  },
  "budget": {
    "per_story_usd": 60,                     // US dollars, 0 or more; 0 turns the budget off
    "alert_fractions": [0.5, 0.8, 1.0],      // shares of per_story_usd at which sdlc warns you
    // hours by gate key, such as {"plan": 1}; the keys are dor, analysis, tests_frozen,
    //   plan, design_review, implementation, verification, verifier_review, code_review,
    //   commit and retro; 0 or absent is off
    "gate_hours": {}
  },
  "consolidate": { "nudge_after": 3 }        // finished stories before the reminder, 0 or more; 0 turns it off
}
```

The rest of this page takes the settings one section at a time. Every entry
has the same parts: what the section is for, a table of its settings and
their defaults, an example, and what changes when you change it.

## `commands`

`commands` are how to build, test and check this project. The loop runs them
and never guesses at a toolchain. `sdlc` runs most of them itself, once, when
the implementation is recorded as done at Gate 5, and the agents run the rest
at their gates.

| Command | Who runs it, and when |
| --- | --- |
| `test` | `sdlc`, when the implementation gate is recorded as passed at Gate 5. This is the whole suite |
| `test_files` | the test author and the test reviewer at Gate 3, and the implementer after each step of the plan, on the story's own tests; `sdlc freeze`, on the new and changed tests: [The narrow test command](#the-narrow-test-command) |
| `build` | `sdlc`, at the implementation gate |
| `lint` | `sdlc`, at the implementation gate |
| `fmt` | the implementer once the frozen tests pass |
| `fmt_check` | `sdlc`, at the implementation gate |
| `coverage` | `sdlc`, at the implementation gate, which holds the number it prints to `thresholds.coverage_min` |
| `mutation` | `sdlc`, at the implementation gate, which holds the number it prints to `thresholds.mutation_min` |
| `smoke` | `sdlc start`, before each new story, in the story's new worktree. If it fails, the story does not start |
| `fmt_file` | the hook, on each file an agent writes during a story, with `$FILE` set to that file |
| `freeze_check` | `sdlc freeze`, before the tests are frozen. Empty until you choose one: [The freeze check](#the-freeze-check) |

At the implementation gate `sdlc` runs `build`, `test`, `lint`, `fmt_check`,
`coverage` and `mutation`, those that are set, in that order, and records what
passed against the tree it ran them on. While that result is current, nobody
runs them again: the verifier at Gate 6 works from it, and so does
[the landing](#at-landing). Where the tree has changed since, the verifier
runs them itself.
[`sdlc gate`](commands.md#sdlc-gate-gate-status) says what a failing command
looks like.

There is no default for a command. `init` fills them in from the stack it
finds, and a command it has no guess for is left as `""`:

| Command | Go | Rust | Node | Python |
| --- | --- | --- | --- | --- |
| `smoke` | `go vet ./...`, where the module has a package | `cargo check --all-targets` | the same as `build` | none |
| `build` | `go build ./...` | `cargo build` | `npm run build --silent` | none |
| `test` | `go test ./... -count=1` | `cargo test` | `npm test --silent` | `pytest -q` |
| `test_files` | `go test -count=1` on the packages of the files listed | `cargo test --test=<name>` for each file listed | `npm test --silent --` and the files listed | `pytest -q` on the files listed |
| `lint` | `golangci-lint run` | `cargo clippy --all-targets -- -D warnings` | `npm run lint --silent` | `ruff check .` |
| `fmt` | `gofmt -w .` | `cargo fmt` | `npx prettier --write .` | `ruff format .` |
| `fmt_check` | `test -z "$(gofmt -l .)"` | `cargo fmt --check` | `npx prettier --check .` | `ruff format --check .` |
| `fmt_file` | `gofmt -w "$FILE"`, for a `.go` file | none | `npx prettier --write --ignore-unknown "$FILE"` | `ruff format "$FILE"`, for a `.py` file |
| `coverage` | `go test` with a cover profile | none | none | `pytest --cov` |

The table gives each command in short. For Go and Python, `test` and
`coverage` are written as a pair that shares one run of the suite, which
[The run directory](#the-run-directory) shows in full, and
[The narrow test command](#the-narrow-test-command) shows each stack's
`test_files`.

No stack gets a `mutation` or a `freeze_check`. For Node, `init` reads your
`package.json`. It fills in `build`, `test` and `lint` only where there is a
script of that name. It formats with your `format` and `format:check` scripts
where you have them, and with Prettier where that is a dependency.

Let's say your Python project runs its tests from a virtual environment, and
has no build step:

```json
"commands": {
  "build": "",
  "test": ".venv/bin/python -m pytest -q"
}
```

**When you change it.** Each value is a shell command, run from the root of
the checkout the story is worked in. A command that is wrong fails the gate
that runs it: a failure at the implementation gate
[records the gate as failed](troubleshooting.md#sdlc-e0095), and the verifier
blocks when any command it runs fails. A command you set to `""`, or leave
out, is skipped. A key of your own is not run by anything, though the agents
can read it.

A few things follow from who runs each command:

- **`test` runs the whole suite, and exits.** A script that starts a watcher
  never returns.
- **`test_files` runs only the files it is given.** With it empty, the freeze
  runs the whole of `test` in its place, and the agents run a story's test
  files with your runner as best they can.
- **`smoke` is fast.** It runs before every new story.
- **`fmt_file` says which files it formats.** It runs for every file written,
  whatever its type. A formatter that fails is reported to the session, and
  the file stays as it was written.
- **Nothing is left behind in the project.** A file a command leaves in the
  working tree, and git does not ignore, is uncommitted work, and the commit
  gate [refuses to pass](troubleshooting.md#sdlc-e0032) while there is any.

### Trusting the commands

> [!WARNING]
> These commands run with your permissions, and `sdlc` runs most of them
> itself. `smoke` is covered by the prompt for `sdlc start`. The rest get no
> prompt at all: `fmt_file` on every file written, `freeze_check` and
> `test_files` whenever an agent runs `sdlc freeze` at Gate 3, and the others
> when it records the implementation at Gate 5.

The commands are read from `.sdlc/config.json` in the repository, and a clone
brings that file with it. In a repository you did not write, read its
`commands` before you work in it, as you would its `.claude/settings.json`.

### Installing what a worktree needs

A story's worktree has only what trunk commits. There is no `node_modules` in
it, no virtual environment and no generated code. `smoke` is the first command
to run there, and every later command runs there too, so `smoke` is where you
install what they need.

For Node, install from the lockfile before you build:

```json
"smoke": "npm ci && npm run build --silent"
```

For Python, create the virtual environment, and make sure git ignores it:

```json
"smoke": "python -m venv .venv && .venv/bin/pip install --quiet -r requirements.txt"
```

Go and Rust keep their dependencies outside the project, so the commands
`init` writes work in a new worktree as they are.

Watch for tools that look upwards from where they run. From inside
`.sdlc/worktrees/<ID>` they find the files of your main checkout: Go's
`go.work`, Node's `node_modules`, and configuration that cascades, such as
ESLint's `.eslintrc` and `.editorconfig`. A `go.work` at the root of your main
checkout takes over the worktree's build. Set `GOWORK=off` in the commands, or
commit a workspace that works from there. Installing in `smoke` gives the
worktree a `node_modules` of its own.

### The narrow test command

`commands.test_files` runs the test files a story adds or changes, and not
the whole suite. The test author and the test reviewer run it on the story's tests at
Gate 3, the implementer runs it after each step of the plan, and `sdlc freeze`
runs it over the new and changed test files before it takes the freeze. The
whole suite, `test`, is left to the implementation gate.

The command runs from the repository root. `SDLC_TEST_FILES` names a file
that lists the test files, one to a line, each relative to the root with `/`
between its parts, as git names them. `init` writes the command for every
stack it knows.

For Go, it tests the packages that hold the files listed. A file under
`testdata/` stands for the package above it:

```json
"test_files": "if [ -n \"${SDLC_TEST_FILES-}\" ]; then sed -e 's#^#./#' -e 's#/testdata/.*#/x#' -e 's#/[^/]*$##' \"$SDLC_TEST_FILES\" | tr '\\n' '\\0' | xargs -0 go test -count=1; else go test ./... -count=1; fi"
```

For Python, it hands pytest each file listed:

```json
"test_files": "if [ -n \"${SDLC_TEST_FILES-}\" ]; then sed 's#^#./#' \"$SDLC_TEST_FILES\" | tr '\\n' '\\0' | xargs -0 pytest -q; else pytest -q; fi"
```

For Rust, it passes `--test=<name>` for each `tests/<name>.rs`. Where a file
listed is anything else, it runs the whole of `cargo test`:

```json
"test_files": "if [ -n \"${SDLC_TEST_FILES-}\" ] && ! grep -qv '^tests/[^/]*\\.rs$' \"$SDLC_TEST_FILES\"; then sed 's#^tests/\\(.*\\)\\.rs$#--test=\\1#' \"$SDLC_TEST_FILES\" | tr '\\n' '\\0' | xargs -0 cargo test; else cargo test; fi"
```

For Node, it hands each file listed to your `test` script, where
`package.json` has one:

```json
"test_files": "if [ -n \"${SDLC_TEST_FILES-}\" ]; then sed 's#^#./#' \"$SDLC_TEST_FILES\" | tr '\\n' '\\0' | xargs -0 npm test --silent --; else npm test --silent; fi"
```

Each of them runs the whole suite when you run it by hand, with no
`SDLC_TEST_FILES` set. A command of your own should treat every name in the
list as data, as these do: a name can start with `-` or hold a space.

**When you change it.** With `test_files` empty, `sdlc freeze` runs the whole
of `test` in its place, and with neither set it runs nothing. A wrong command
is hard to see, because the freeze expects the run to fail: new tests fail
until the code is written, and a command that fails for a reason of its own
looks the same. So read the first story's
`.sdlc/stories/<ID>/freeze-run.log` after you change it.
[`sdlc freeze`](commands.md#sdlc-freeze) says what the freeze does with the
run. A test file whose name holds a line break or another control character
is refused before anything runs.

### The run directory

Whenever `sdlc` runs your commands itself, it hands that run one new
directory, in `SDLC_RUN_DIR`. That is so at the implementation gate, at the
landing and in the freeze's narrow run. The directory is empty when the first
command begins, it is the same for every command of the run, and it is outside
your project.

It is how `test` and `coverage` share one run of the suite: `test` leaves its
coverage data in the directory, and `coverage` reads the figure from it. Each
run's directory is new, so a figure another run left is never read. A
`coverage` that finds no data prints no figure, and the run is refused.

When you run a command by hand, there is no `SDLC_RUN_DIR`, and each command
works alone as it always did. A `coverage` command written before the run
directory, which runs the suite itself, still works. It runs the suite twice.

**Go.** `init` writes this pair:

```json
"test": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then go test ./... -count=1 -coverprofile=\"$SDLC_RUN_DIR/cover.out\"; else go test ./... -count=1; fi",
"coverage": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then test -f \"$SDLC_RUN_DIR/cover.out\" && go tool cover -func=\"$SDLC_RUN_DIR/cover.out\" | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'; else p=$(mktemp) && trap 'rm -f \"$p\"' EXIT && go test ./... -count=1 -coverprofile=\"$p\" >/dev/null && go tool cover -func=\"$p\" | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'; fi"
```

**Python.** `init` writes this pair. It needs pytest-cov, which `--cov` turns
on, and coverage.py, which comes with it:

```json
"test": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then COVERAGE_FILE=\"$SDLC_RUN_DIR/.coverage\" pytest -q --cov; else pytest -q; fi",
"coverage": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then COVERAGE_FILE=\"$SDLC_RUN_DIR/.coverage\" python3 -m coverage report | grep -E '^TOTAL' | grep -Eo '[0-9]+%' | tr -d %; else p=$(mktemp) && trap 'rm -f \"$p\"' EXIT && COVERAGE_FILE=\"$p\" pytest -q --cov --cov-report=term | grep -E '^TOTAL' | grep -Eo '[0-9]+%' | tr -d %; fi"
```

**Rust.** `init` writes `cargo test` and no `coverage`. With
[cargo-llvm-cov](https://github.com/taiki-e/cargo-llvm-cov) installed, a pair
that shares one run is:

```json
"test": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then cargo llvm-cov --lcov --output-path \"$SDLC_RUN_DIR/lcov.info\"; else cargo test; fi",
"coverage": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then test -f \"$SDLC_RUN_DIR/lcov.info\" && awk -F: '/^LF:/{t+=$2} /^LH:/{h+=$2} END{if (t) printf \"%.1f\\n\", 100*h/t}' \"$SDLC_RUN_DIR/lcov.info\"; else cargo llvm-cov --summary-only | grep -E '^TOTAL' | grep -Eo '[0-9]+\\.[0-9]+%' | tail -1 | tr -d %; fi"
```

**Node.** `init` writes `npm test --silent` and no `coverage`. With
[c8](https://github.com/bcoe/c8) as a development dependency, a pair that
shares one run is:

```json
"test": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then npx c8 --reporter=json-summary --report-dir \"$SDLC_RUN_DIR\" npm test --silent; else npm test --silent; fi",
"coverage": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then test -f \"$SDLC_RUN_DIR/coverage-summary.json\" && node -e 'console.log(require(process.argv[1]).total.lines.pct)' \"$SDLC_RUN_DIR/coverage-summary.json\"; fi"
```

**When you change it.** `sdlc` makes the directory, names it in
`SDLC_RUN_DIR` and removes it when the run ends, whether the run passed,
failed or was interrupted. It knows nothing of what a tool puts there, so the
file names above are yours to choose. The directory is made in your operating
system's temporary directory, with a name that starts `sdlc-run-`. A run that
is killed outright leaves it behind, and it is safe to delete.

What the commands print is kept for you in the story's directory:
`.sdlc/stories/<ID>/implementation-run.log` for the implementation gate, and
`freeze-run.log` beside it for the freeze. Git ignores both. Neither run has
a time limit of its own.

### Coverage and mutation

The rule for both commands is the same: print the percentage as the last
number on standard output. `sdlc` compares it with the
[threshold](#thresholds) at the implementation gate and at
[the landing](#at-landing), and so does the verifier where it runs the
command itself.

Leave nothing in the project on the way. In a run of the loop's, the
`coverage` commands `init` writes for Go and Python read their data from
[the run directory](#the-run-directory). Run by hand, the Go one puts its
profile in a temporary file outside the project and removes it when the shell
exits, and the Python one does the same with pytest-cov's data file. A
command of your own should do likewise, because a profile left in the working
tree is uncommitted work. Don't write it under `.sdlc/`: the
hook [refuses a command](enforcement.md#loop-state-through-the-tool) that
writes or deletes there, and `sdlc doctor` reports a configured command that
may.

`init` guesses no coverage command for Rust or Node, and no mutation command
for any stack.
[Measure coverage](guides/tuning-commands-for-your-stack.md#measure-coverage)
has examples.

### The freeze check

`freeze_check` is a command that `sdlc freeze` runs before it freezes
anything. It is there so that a test file CI would refuse, for its formatting
or for a secret in it, is corrected by the test author at Gate 3. Found later,
the same mistake costs an unfreeze, and that is yours to give. The key is
empty until you choose a command, and with it empty `sdlc freeze` runs no
check.

The check is handed the test files that are new or changed in the story, in
`SDLC_FREEZE_FILES`, one name to a line. It is run by `sh` from the repository
root, once for each freeze: the first, a freeze after `sdlc unfreeze`, and
each addition under
[`freeze.allow_new_test_files`](#freezeallow_new_test_files).

A check that exits 0 passes. Anything else
[refuses the freeze](troubleshooting.md#sdlc-e0085) and shows the last 40
lines the check printed, so the test author can correct the files and run
`sdlc freeze` again. A check that rewrites, deletes or creates a test file is
refused too, so use your formatter's checking mode, never the one that writes.

Here is a check for Go, saved as `scripts/freeze-check.sh`:

```sh
#!/bin/sh
# sdlc freeze runs this from the repository root, with the test files that are
# new or changed in $SDLC_FREEZE_FILES, one to a line. It writes nothing into
# the tree.
failed=0
tmp=$(mktemp -d) || exit 1
trap 'rm -rf "$tmp"' EXIT
printf '%s\n' "$SDLC_FREEZE_FILES" > "$tmp/files"

while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    *.go)
      # gofmt -l exits 0 when it lists a file, so its output is what counts.
      if ! out=$(gofmt -l "./$f" 2>&1) || [ -n "$out" ]; then
        printf 'gofmt: %s is not formatted, or does not parse:\n%s\n' "./$f" "$out"
        failed=1
      fi
      ;;
  esac
  case "$f" in
    testdata/* | */testdata/*) ;;
    *.go) dirname "./$f" >> "$tmp/dirs" ;;
  esac
  gitleaks dir --no-banner --no-color --redact --verbose "./$f" || failed=1
  found=$(grep -a -i -o -E '(/Users/|/home/|/c/Users/|[a-z]:\\Users\\)[A-Za-z0-9._-]+' "./$f" | grep -v -E '[/\\]dev$')
  if [ -n "$found" ]; then
    printf 'account paths in %s:\n%s\n' "./$f" "$found"
    failed=1
  fi
done < "$tmp/files"

# A package whose tests name code that is not written yet does not compile, and
# golangci-lint stops at it. Lint the packages that do, and say which were left out.
if [ -s "$tmp/dirs" ]; then
  sort -u "$tmp/dirs" > "$tmp/each"
  while IFS= read -r d; do
    if go vet "$d" > "$tmp/vet" 2>&1; then
      set -- "$@" "$d"
    elif go test -vet=off -c -o /dev/null "$d" > /dev/null 2>&1; then
      cat "$tmp/vet"
      failed=1
    else
      echo "not linted, does not compile yet: $d"
    fi
  done < "$tmp/each"
  if [ $# -gt 0 ]; then
    golangci-lint run "$@" || failed=1
  fi
fi
exit "$failed"
```

Name it in the configuration:

```json
"freeze_check": "sh scripts/freeze-check.sh"
```

The script checks the format of each listed file, scans it for secrets and
for paths that carry an account name, and lints the packages the files are
in. For another stack, keep the loop over the list and put your own
formatter's check and your own linter in place of the Go tools.

A few things are worth copying from it, whatever your stack:

- **Treat every name as data.** A name can start with `-` or hold `$(...)`,
  so quote it, put `./` in front of it and never evaluate it.
- **Read the list from a file.** An unquoted `$SDLC_FREEZE_FILES` splits a
  name with a space in it, and a `while read` at the end of a pipe loses
  `failed` in a subshell.
- **Test the output where the exit code lies.** `gofmt -l` exits 0 when it
  lists a file. `| tee` and `a; b` hide a failure in the same way.
- **Print no secret.** What the check prints goes into the session, which is
  why the script passes `--redact`. Take a token from the environment, never
  from the command's own text.

At Gate 3 the tests name code nobody has written yet, so a check over the
whole project, such as `go vet ./...`, fails on every story. That is also why
the script does not lint a package that does not compile yet, and says so in
its output: `gofmt` and `gitleaks` still read those files, and a lint finding
in them can come to light only after the code is written.

A check is honest only about the machine it ran on. It passes or fails in the
environment of whoever ran `sdlc freeze`, so it catches mistakes early and is
no substitute for CI. `sdlc doctor` says whether a check is set, and names a
program it cannot find.

### At landing

A story worked in its own worktree lands when `sdlc stop` runs there after the
retro. Where Gate 5 recorded a result for the very tree that would land, with
the commands and thresholds trunk has, and the story was not put onto a trunk
that had moved, the landing lands on that result and runs nothing again.

Otherwise it says why, and runs `build`, `test`, `lint`, `fmt_check`,
`coverage` and `mutation`, those that are set, in that order, on the commit
that would land. A story that was put onto a trunk that had moved lands this
way. So does one whose tree is not the one Gate 5 measured, as when its branch
carries a commit of `CODEMAP.md`.

It reads those commands, and `thresholds`, from the `.sdlc/config.json` trunk
has committed. An uncommitted edit that would make a failing command pass
changes nothing.

> [!NOTE]
> A file git tracks and `.gitignore` also matches makes the landing run the
> commands every time, because the tree Gate 5 measured leaves it out. Run
> `git ls-files -ci --exclude-standard` to list such files, and untrack each
> one or stop ignoring it.

A command passes when it exits 0. `coverage` and `mutation` must also print a
number on standard output, and the last one has to reach its threshold. With
the threshold at `0`, no number is needed.

A command may not change what it checks. If a tracked file differs after a
command has run, as it does after a formatter that writes,
[the landing is refused](troubleshooting.md#sdlc-e0055). That is what
`fmt_check` is for.

Keep these commands fast. A session's `sdlc stop` has ten minutes for the
whole landing, the wait for another story that is landing included. Where
your commands need longer, run `sdlc stop` in the story's worktree from a
terminal of your own.

> [!WARNING]
> A stop you run yourself runs the story's code with your own privileges and
> credentials. Read the story before you run it.

[Done when it lands](the-loop.md#done-when-it-lands) describes the rest of
what a landing checks.

## `thresholds`

`thresholds` are the numbers a change is held to: by `sdlc` when the
implementation is recorded at Gate 5 and again at the landing, and by the
verifier at Gate 6.

| Setting | Default | What it does |
| --- | --- | --- |
| `coverage_min` | `80` | the lowest percentage `commands.coverage` may print |
| `mutation_min` | `70` | the lowest percentage `commands.mutation` may print |

```json
"thresholds": { "coverage_min": 85.5, "mutation_min": 0 }
```

**When you change it.** Lowering a threshold lets weaker work through, and
`0` turns it off. A negative number is refused. A threshold whose command is
empty has nothing to be compared with, so with no `mutation` command the
default of `70` holds nothing. The landing reads both numbers from what trunk
commits, as it does the [commands](#at-landing).

## `paths`

`paths` tells the loop which files are tests and which are source, and where
the lines of an unresolved conflict are there on purpose.

| Setting | Default | What it does |
| --- | --- | --- |
| [`tests`](#pathstests) | by stack | which files are tests, and so what the freeze covers |
| [`src`](#pathssrc) | by stack | where production code lives |
| [`conflict_markers_intended`](#pathsconflict_markers_intended) | none | the files a story may add conflict markers to |

### `paths.tests`

`paths.tests` decides what the freeze covers, and so what the implementer may
never touch. A file is a test if `dirs` or `file_globs` matches it.

| Setting | What it does |
| --- | --- |
| `dirs` | directories whose every file is a test |
| `file_globs` | patterns for test files that sit beside the code |

`init` fills both in for your stack:

| Stack | `dirs` | `file_globs` |
| --- | --- | --- |
| Go | `testdata/` | `*_test.go` |
| Rust | `tests/`, `testdata/`, `fixtures/` | `*_test.rs` |
| Node | `tests/`, `test/`, `__tests__/`, `__snapshots__/`, `__mocks__/`, `__fixtures__/` | `*.test.ts`, `*.test.js`, `*.spec.ts`, `*.spec.js`, `*.snap` |
| Python | `tests/`, `test/`, `fixtures/`, `testdata/` | `test_*.py`, `*_test.py`, `conftest.py` |

A list you leave out of the file is not an empty list. `dirs` is then `tests/`
and `test/`, and `file_globs` is `*_test.go`, `*.test.ts`, `*.spec.ts`,
`test_*.py`, `*_test.py`, `*Test.java` and `*_spec.rb`. Write `[]` to mean
none.

Let's take a look at a Node project that keeps its component tests beside the
code as `*.test.tsx`, and its fixtures in one directory of its own:

```json
"paths": {
  "tests": {
    "dirs": ["tests/", "__snapshots__/", "__mocks__/", "src/fixtures"],
    "file_globs": ["*.test.ts", "*.test.tsx", "*.snap"]
  }
}
```

The two lists read their entries differently:

- **A bare name in `dirs`**, such as `testdata/` or `__snapshots__`, is a
  directory of that name at any depth. A slash at either end changes nothing.
- **A name with a slash inside it**, such as `src/fixtures`, is that one
  directory, from the root. An entry that starts with `./` matches nothing.
- **A pattern in `file_globs`** is tried against the whole path and against
  the file's own name, so `*_test.go` finds `internal/store/x_test.go`.
- **Wildcards.** `*` does not cross a `/`, and `**` stands for any number of
  directories. `src/**/*.spec.ts` finds a spec at any depth under `src/`, and
  `e2e/**` is everything under `e2e/`.
- **Case is ignored** in a pattern, so `*_test.go` matches `Foo_Test.go`.

**When you change it.** Anything that decides whether a test passes belongs
here, not only the test files: golden files, snapshots, fixtures, a
`conftest.py`. A frozen test that reads a fixture nobody froze can be made to
pass by changing the fixture. The test author is refused when it writes
outside what `paths.tests` describes, and the refusal says so.

Change it between stories. A file that becomes a test halfway through a story
is a test the freeze does not hold, and the story's next gate
[refuses to pass](troubleshooting.md#sdlc-e0043).
[Tell sdlc which files are tests](guides/tuning-commands-for-your-stack.md#tell-sdlc-which-files-are-tests)
has more examples, and shows how to check what a freeze covered.

> [!WARNING]
> A Rust unit test inside a source file, under `#[cfg(test)]`, is in no test
> file, so the freeze cannot hold it. Put the acceptance tests under `tests/`.

### `paths.src`

`paths.src` lists where production code lives. `init` writes `cmd/`,
`internal/` and `pkg/` for Go, `src/` for Rust and for Python, and `src/` and
`lib/` for Node. Left out of the file, it is `src/`, `internal/`, `pkg/`,
`cmd/` and `lib/`.

```json
"paths": { "src": ["app/", "lib/"] }
```

**When you change it.** The researcher looks there first at Gate 2, the
implementer puts new code there, and the code reviewer uses it to tell a
change's code from its tests. It is a hint, and nothing refuses a write
outside it: a build file or a migration can be part of a story too.

### `paths.conflict_markers_intended`

The commit gate and `sdlc stop` refuse a story whose commits
[add the markers of an unresolved conflict](troubleshooting.md#sdlc-e0080) to
a file. A document that shows what a conflict looks like, or a test fixture
that holds one, is meant to have them. This setting lists those files. It is
empty by default, and `init` does not write it, so no path is exempt until you
add one.

```json
"paths": {
  "conflict_markers_intended": ["docs/merging.md", "testdata/**", "**/*.patch"]
}
```

A pattern here is stricter than one in `paths.tests`, because an exemption
that matched more than it names would let a conflict in:

- It is matched against the whole path from the repository root, and exactly:
  `Docs/a.md` is not `docs/a.md`, and a bare name such as `testdata` is that
  one path, not a directory of that name anywhere.
- `**` is a segment of its own that stands for any number of directories, so
  `docs/**` is everything under `docs/`, and `**/*.patch` is a patch file at
  any depth.
- An entry that cannot mean what it says is
  [refused](troubleshooting.md#sdlc-e0045), and `sdlc doctor` names it: an
  empty one, one that starts or ends with a slash, or one with a backslash, a
  `..` or a `.` segment in it.

**When you change it.** The commit gate reads the file in the checkout it
runs in, and `sdlc stop` reads the one trunk has committed. A story cannot
carry a change to `.sdlc/`, so to exempt a path for a story that is waiting
to land, commit the setting on trunk and run `sdlc stop` in the story's
worktree again. `sdlc doctor` shows what this checkout's file lists, on a
line of its `configuration` check:

```text
                                paths.conflict_markers_intended (this checkout's copy; "sdlc stop" reads trunk's): "docs/merging.md", "testdata/**", "**/*.patch"
```

## `git`

`git` says which branch is trunk, whether there is a remote to keep up with,
and whether each story gets a worktree of its own.

| Setting | Default | What it does |
| --- | --- | --- |
| `trunk_branch` | `main` | the branch stories start from and land on. `init` writes the branch `origin`'s `HEAD` names, else `main` or `master` if the repository has one, else the branch you are on |
| `remote` | `false` | with `true`, `sdlc start` fetches trunk from `origin`, and will not begin new work while trunk is behind it. The loop never pushes, whatever this says |
| `worktrees` | `true` | whether `sdlc start` gives each new story a worktree and a branch of its own. `init` does not write it, and left out it is on |

```json
"git": { "trunk_branch": "trunk", "remote": true }
```

**When you change it.** With `worktrees` on, `sdlc start` makes a new story's
worktree at `.sdlc/worktrees/<ID>` in your main checkout, on a new branch
`story/<ID>` cut from the tip of your local trunk. The story is worked and
committed there, and lands on trunk by fast-forward when it is finished. Your
main checkout keeps its branch and its files, and need not be on trunk.
Worktrees need git 2.17 or newer, and `sdlc doctor` says when yours is older.
[More than one checkout](the-loop.md#more-than-one-checkout) describes working
several stories at once.

### Turning worktrees off

Set `worktrees` to `false` and stories are worked in the checkout you run
`sdlc start` in, one at a time, and committed straight to trunk:

```json
"git": { "trunk_branch": "main", "remote": false, "worktrees": false }
```

Several things change with it:

- **A story starts only from a clean trunk.** The checkout has to be on trunk
  ([SDLC-E0038](troubleshooting.md#sdlc-e0038)) with nothing uncommitted that
  belongs to no story ([SDLC-E0039](troubleshooting.md#sdlc-e0039)). The
  loop's own files under `.sdlc/` and the backlog do not count. `smoke` runs
  on trunk itself, and a failure
  [stops the start](troubleshooting.md#sdlc-e0041).
- **The story's commit takes everything.** The commit gate refuses while
  `git status --porcelain` shows anything at all. So every file under `.sdlc/`
  has to be committed or ignored, the story's own directory and the loop's
  state among them, and the story's commit carries whatever git does not
  ignore.
- **There is no landing.** A story is on trunk as soon as it is committed, so
  nothing runs your commands a second time.
- **An uncommitted edit to the configuration is read at once.** There is one
  checkout and one file. Commit the edit yourself all the same, or the next
  story's commit takes it along.

> [!NOTE]
> Add `/.sdlc/state/` to your `.gitignore` before the first story. It holds
> the loop's own state, and if trunk comes to track it, no story can land
> from a worktree after you turn them back on. `sdlc doctor` says so, and
> prints the commands that put it right.

## `human_gates`

`human_gates` decides where the loop waits for a person.

| Setting | Default | What it does |
| --- | --- | --- |
| `pre_commit_pause_tiers` | `["high"]` | the risk tiers whose stories wait for your approval before they are committed: any of `low`, `medium` and `high`. `[]` pauses no story for its tier, though a path can still pause it |
| `pre_commit_pause_paths` | `[]` | patterns for files, from the repository root. A story whose change touches a file one of them matches waits for your approval before it is committed, whatever its tier |
| `dor_advocate_check` | `false` | with `true`, the human advocate reads each story at Gate 1, before any work starts |

```json
"human_gates": { "pre_commit_pause_tiers": ["medium", "high"], "dor_advocate_check": true }
```

**When you change it.** A story's tier is its `risk_tier` in the backlog, and
a story without one is `low`. Tiers are compared without regard to case, so
`"High"` in this list still matches a `high` story. A story waits for you
where either list says so: its tier is listed, or its change touches a listed
path. Such a story reaches Gate 8 and stops. The loop hands it to you, and the
commit is refused, naming the tier or each file with the pattern it matched,
until you have run [`sdlc approve`](commands.md#sdlc-approve). Your approval
is for the work as it stands, so a change made after it needs approving again.
[The pause before the commit](guides/human-approval-gates.md#the-pause-before-the-commit)
shows the whole exchange.

**Paths.** A pattern in `pre_commit_pause_paths` is written from the
repository root with `/` between its parts, such as `"src/auth/**"`. `*` does
not cross a `/`, and `**` stands for any number of directories. A bare
directory, such as `"src/auth"`, covers everything under it. Case is ignored,
as macOS and Windows ignore it in a file's name. A pattern that cannot mean
what it says is [refused](troubleshooting.md#sdlc-e0045) when the file loads,
and the refusal names it: an empty one, one that starts or ends with a slash,
or one with a backslash, a `..` or a `.` segment in it.

What a story changes is measured from where its branch left trunk. Its
commits count, and so does everything not yet committed: what is staged, what
is changed, what is deleted, and files git does not track yet. A file that
moved counts under both its names, so moving a file out from under a pattern
still pauses the story.

These are `sdlc`'s own paths, as an example of the kind to list: the code
that decides who may approve, what is frozen and what the hook refuses. Your
project will have its own, such as its authentication, its payments or its
migrations:

```json
{
  "human_gates": {
    "pre_commit_pause_tiers": ["high"],
    "pre_commit_pause_paths": [
      "internal/cli/approve.go",
      "internal/cli/freeze.go",
      "internal/cli/freezecheck.go",
      "internal/cli/ack.go",
      "internal/cli/remap.go",
      "internal/cli/staffing.go",
      "internal/cli/inclaudecode.go",
      "internal/store/store.go",
      "internal/testset/**",
      "internal/hook/**",
      "internal/policy/**",
      "internal/pathrules/**",
      "internal/shellpolicy/**",
      "internal/config/commitpause.go",
      "internal/config/glob.go",
      "internal/store/commitpause.go",
      "internal/gitx/changedsince.go",
      "plugin/hooks/**",
      "plugin/bin/**",
      "plugin/skills/approve/**",
      "plugin/skills/unfreeze/**",
      "plugin/skills/ack/**",
      "plugin/skills/remap/**",
      "plugin/skills/assign/**"
    ]
  }
}
```

List the files that decide such things and little else. A file that nearly
every story touches would pause nearly every story.

**Keeping the tiers.** The two lists add up, so a project that wants every
high-tier story to pause as before keeps `pre_commit_pause_tiers` at
`["high"]`, the default, and adds paths beside it. A release of `sdlc` older
than 0.7.0 ignores `pre_commit_pause_paths`. Empty the tiers only once every
machine that works the project runs a release that reads the paths.

**A story cannot remove its own pause.** In a story's worktree, the lists in
your main checkout and the ones in the worktree's own copy of the file count
together, so a story that empties its copy still pauses. With
[worktrees off](#turning-worktrees-off), the file committed when the story
started counts as well as the one on disk, so narrowing either list takes a
commit before the story starts. The landing asks again of a story that was
put onto a trunk that had moved, with trunk's lists added. And a
configuration or a change that cannot be read is a pause too, never a commit
without one: the refusal says what did not read, your approval answers it,
and [`sdlc doctor`](commands.md#sdlc-doctor) names the file.

**Seeing it beforehand, and afterwards.**
[`sdlc status`](commands.md#sdlc-status) says before the commit gate whether
the commit will pause, and why. A story that passes the commit gate with no
pause is marked as such on its record, as a `landed_without_pause` event, and
[`sdlc log`](commands.md#sdlc-log) shows a `no pause` line for it until you
run `sdlc ack`. So every story committed without your sign-off is shown to
you as one, in every project, whether or not it lists any paths.

With `dor_advocate_check` on, Gate 1 waits for the human advocate's review of
the story. The review is advisory there, as it is everywhere unless you
[change that](#an-agents-entry), and it is listed with the others:

```console
$ sdlc review list --gate dor
Review policy: REVIEW.md
  dor             human-advocate  advisory  not reviewed
```

If the story changes after the advocate has read it, the review goes stale,
and the gate waits for a new one.

## `team`

`team` says which agents take part, which of them can block a gate, how many
instances of a reviewer there are, how often a run may fail, and what the
project manager may ask for. Every agent is on by default, the project manager
included: story by story, it says which agents the story needs and the model
and effort each is asked for, and records that as the story's staffing.
[Agents](the-agents.md) describes each of them.

| Setting | Default | What it does |
| --- | --- | --- |
| `retries` | `2` | how many times a run of one agent may fail at a gate, reported with [`sdlc team failure`](commands.md#sdlc-team-failure-agent), before the story is handed to you. From `0`, which hands it over at the first failure, to `20` |
| `models` | `["haiku", "sonnet", "opus"]` | the model names a staffing may ask for, **lowest first** |
| `efforts` | `["low", "medium", "high", "xhigh", "max"]` | the effort names a staffing may ask for, **lowest first** |
| `agents` | every agent on | one entry for each agent, under the name the plugin gives it. An agent with no entry keeps every default |

Let's take a look at a team that lets the red team stop a gate, asks for two
security reviews, turns the performance reviewer off and sets a floor under
what the verifier is asked for. Only the entries that differ from the defaults
are shown:

```json
"team": {
  "agents": {
    "verifier": { "lowest": { "model": "opus", "effort": "high" } },
    "security": { "instances": 2, "bring_back": ["internal/auth/"] },
    "red-team": { "can_block": true },
    "perf":     { "enabled": false }
  }
}
```

**When you change it.** A change reaches the stories started after it is on
trunk. A story under way reads the file its worktree started with, so turning
off a failing agent on trunk changes nothing in that story. The story's
record keeps no history of the team either: afterwards, only git's history of
`.sdlc/config.json` says what it was.

> [!WARNING]
> Two settings loosen the loop: `enabled` set to `false` takes an agent out of
> every story, and `can_block` set to `false` lets a gate pass over that
> reviewer's block. An agent can write this file between stories, when the
> hook is not holding it, so read a change to either before you commit it.

To see what you committed, run `sdlc doctor`. Under `configuration` it adds a
line for each agent you have disabled, each `can_block` that differs from its
default, and each reviewer with more than one instance. For the team above,
those lines are:

```text
                                team.agents.security.instances is 2: 2 instances of security review
                                team.agents.red-team.can_block is true, where the default is false
                                perf is disabled in team.agents.perf.enabled
```

It also says where a `can_block`, an `instances` or a `max_rounds` is set on
an agent it can do nothing for. A team at its defaults adds no line at all.

### An agent's entry

| Key | Default | What it does |
| --- | --- | --- |
| `enabled` | `true` | whether the agent runs. `false` takes it out of every story |
| `can_block` | `true` for the architect, security, perf, the verifier, the code reviewer and the test reviewer; `false` for red-team and the human advocate | for an agent that reviews, whether its block stops the gate |
| `max_rounds` | `loop.max_review_rounds` | how many blocks that stop the gate one reviewer may make before the gate goes to its [arbiter](the-loop.md#the-arbiter), and how many the arbiter may make before the story is handed to you. `1` or more |
| `instances` | `1` | how many instances of a reviewer review each gate it reviews, from `1` to `16` |
| `lowest` | none | the lowest `model` and `effort` the project manager may ask for this agent: [Models, efforts and floors](#models-efforts-and-floors) |
| `bring_back` | none | for a reviewer of the code, the paths that return it to a story it was left out of: [Bringing a reviewer back](#bringing-a-reviewer-back) |

Each agent has one entry, under one of these names: `project-manager`,
`researcher`, `sdet`, `implementer`, `verifier`, `bookkeeper`, `architect`,
`security`, `red-team`, `perf`, `human-advocate`, `code-reviewer` and
`test-reviewer`. An agent listed twice is refused, so write each agent's
settings once.

**`enabled`.** With an agent disabled, what happens depends on its job. Where
it does a gate's work, or is the verifier or the code reviewer, that work is
yours to do by hand, and the loop
[hands the story to you](the-agents.md#doing-an-agents-gate-by-hand) when it
reaches the gate. A disabled reviewer of the tests, the plan or the change is
not waited for: with the test reviewer disabled, `sdlc freeze` waits for no
review. With the project manager disabled no story is staffed, and every
enabled agent takes part in every story.

**`can_block`.** `false` makes a review advisory: still required, never
skipped, and its block does not stop the gate. On the test reviewer, `sdlc
freeze` then waits only until it has reviewed. Its findings are still closed
before the commit, like any other reviewer's. `true` gives a reviewer no more
than its role has: security still blocks only a story recorded as
security-sensitive, and perf only on a stated budget. On red-team or the
human advocate, `true` makes a block stop the gate.
[Which reviewers can stop a gate](the-agents.md#which-reviewers-can-stop-a-gate)
has the whole table.

**`max_rounds`.** It counts only blocks that stop the gate, so it does nothing
on an advisory agent, or on one that reviews nothing. Each instance of a
reviewer counts its own blocks, and so does its arbiter. A disabled agent has
no arbiter, so at its limit the story is handed to you.

**`instances`.** The second instance of `security` is `security-2`, the third
`security-3`. Each is recorded under its own name, and the gate waits for a
review from each. Be honest with yourself about what that buys: `sdlc` cannot
see that two instances were two runs, and one run can record a review under
both names. Nothing shows them to be independent, though
[`sdlc trace`](commands.md#sdlc-trace) shows how many runs there were.
Lowering `instances` drops the reviews of the instances it removes.

### Models, efforts and floors

The model and effort a staffing records are what is asked for. The environment
can override both, and `sdlc` does not observe what a delegation is given.

**The first name in each list is the lowest.** `sdlc` ranks nothing by its
name: a floor is a position in your own lists. A list written highest first
allows everything, and nothing can tell it from one that means it, so
[`sdlc staffing show`](commands.md#sdlc-staffing-show) prints both lists in
order. The names are whatever your Claude Code takes for a delegation. `sdlc`
checks what is asked for against these lists and against nothing else, so a
name mistyped here is asked for as it is typed.

`lowest` sets a floor for one agent. It names a model from `models`, an effort
from `efforts`, or both, and the project manager may not ask for less:

```json
"verifier": { "lowest": { "model": "opus", "effort": "high" } }
```

An agent with no `lowest` may be asked for from the first name in each list.
Every instance has its role's floor.

### Bringing a reviewer back

The project manager may leave a reviewer out of a story that does not need it.
`bring_back` lists the paths that return a reviewer of the code to the code
review all the same: `security`, `perf`, `human-advocate` or `code-reviewer`.

```json
"security": { "bring_back": ["internal/auth/", "install.sh"] }
```

Each path is a file or a directory relative to the repository root, with `/`
between its parts and no pattern. It is matched a segment at a time, without
regard to case, and it brings back every instance of its reviewer. A path
typed wrongly loads and matches nothing, and `sdlc staffing show` prints the
paths as they were read, which is where to look for one.

### What the project manager cannot leave out

Some agents are never left out while they are enabled, and that is not a
setting: the agents that do a gate's work, the verifier, the code reviewer,
and security on a story the analysis recorded as security-sensitive.

Three things the project manager can do are worth knowing. It can leave the
architect out, and nothing brings the architect back at the design review,
where there is no change to read. It can leave perf out, with `bring_back`
all that returns it, so a project with a performance budget lists its hot
paths there. And it can leave the test reviewer out of a story, with a
reason, and `sdlc freeze` then waits for no review. A staffing that does not
mention the test reviewer keeps it in.

### Upgrading from the staffing section

Releases up to 0.5.0 configured the agents in a `staffing` section and in
`reviews.gate7_advisory`. `team` replaced both. A file that still has either
is refused by every command that loads it, and the refusal,
[SDLC-E0087](troubleshooting.md#sdlc-e0087), names each old setting it found
and where that setting went. Move each one as this table says:

| Before | After |
| --- | --- |
| `staffing.enabled` set to `true` | nothing: the project manager is on by default |
| `staffing.enabled` set to `false` | `enabled` set to `false` in `team.agents.project-manager` |
| `staffing.models` | `team.models` |
| `staffing.efforts` | `team.efforts` |
| `staffing.lowest`, an entry for each agent | `lowest` in that agent's own entry, such as `team.agents.verifier.lowest` |
| `staffing.bring_back`, an entry for each agent | `bring_back` in that agent's own entry |
| `reviews.gate7_advisory` set to `true` | `team.agents.code-reviewer.can_block` set to `false` |
| `reviews.gate7_advisory` set to `false` | nothing: it is a leftover that is not refused, and `sdlc doctor` reports it |

Then delete the whole `staffing` section, because an empty `"staffing": {}`
is still refused, and delete the `gate7_advisory` line. Keep `reviews.policy`
where it is. A file that turned the project manager off, set a floor for the
verifier and made the code reviewer advisory now has this:

```json
"team": {
  "agents": {
    "project-manager": { "enabled": false },
    "verifier":        { "lowest": { "model": "sonnet", "effort": "high" } },
    "code-reviewer":   { "can_block": false }
  }
},
"reviews": { "policy": "REVIEW.md" }
```

How you make the edit depends on where you are. Start at case 3 if a command
has already refused the file, and at case 1 otherwise:

1. **Your file has no `staffing`, and `gate7_advisory` is false or absent.**
   There is nothing to edit. One thing does change on upgrading: the project
   manager, which the old default left off, now staffs your next story. To
   keep it off, add `"project-manager": { "enabled": false }` to
   `team.agents`.
2. **A story is in flight.** It is staffed by the project manager from its
   next gate. Turning the project manager off on trunk reaches only the
   stories you start from then on.
3. **Your file has `staffing`, or `gate7_advisory` set to true.** Once the new
   `sdlc` is installed, every command refuses until a person edits the file,
   `sdlc stop` among them, so no story can land until then.
   - **Before you update**, land what is in flight with the `sdlc` you have.
     Then edit the file on trunk, commit it, and update the command and the
     plugin, in one sitting.
   - **Already updated, with nothing in flight**, edit the file on trunk and
     commit it.
   - **Already updated, with a story in flight**, edit the file on trunk in
     your main checkout and commit it there. Leave the file in the story's
     worktree alone: an edit there only moves the refusal to the landing. In
     the worktree, run `git rebase --autostash <trunk>`. Every gate before
     the commit is now
     [refused because trunk moved](troubleshooting.md#sdlc-e0046), so ask the
     session to run
     `sdlc escalate trunk_moved --message "config moved on trunk"`, or run it
     yourself in the worktree. Then, in your own terminal, run
     `sdlc approve <ID>` and, from your main checkout, `sdlc start <ID>`,
     which picks the story up from where trunk is.

Don't reach for `sdlc init --force` to clear the refusal: it writes the whole
`team` section back at its defaults. To check that the upgrade took, run
`sdlc doctor`, which should find no problem. A team `sdlc` cannot use, such
as an agent that does not exist or an `instances` out of range, is refused as
[SDLC-E0088](troubleshooting.md#sdlc-e0088), and the message lists each
problem. Update the command and the plugin together, as
[Updating](installation.md#updating) says.

## `budget`

`budget` is what one story is expected to cost, and when to say so as it gets
close.

| Setting | Default | What it does |
| --- | --- | --- |
| `per_story_usd` | `60` | what one story is expected to cost, in US dollars. `0` turns the budget off, and the total is still kept |
| `alert_fractions` | `[0.5, 0.8, 1.0]` | the shares of the budget at which `sdlc` warns you |
| `gate_hours` | none | hours of the agents' time a gate may take before `sdlc` says so, by gate key. `init` does not write it |

```json
"budget": {
  "per_story_usd": 40,
  "alert_fractions": [0.5, 0.9],
  "gate_hours": { "plan": 1, "implementation": 2 }
}
```

**When you change it.** Spend is recorded with
[`sdlc cost add`](commands.md#sdlc-cost-add), and shown by `sdlc cost` and
`sdlc status`. A warning is printed once, by the entry that carries the total
past a fraction. The entry that uses up the whole budget always says so,
whatever the fractions are. A fraction of `0` never fires, and one above `1`,
such as `1.5`, warns once the story is that far over. A negative budget is
refused.

A budget never blocks: a story stopped halfway costs more than the overspend.
If a story should stop, that is your call, and
[Tracking what a story costs](guides/tracking-what-a-story-costs.md) shows the
warnings and what to do about them.

`gate_hours` gives a gate a number of hours, under the gate's key: `dor`,
`analysis`, `tests_frozen`, `plan`, `design_review`, `implementation`,
`verification`, `verifier_review`, `code_review`, `commit` or `retro`. It
counts the time the agents were at work on the gate, as the `agents` column of
[`sdlc trace`](commands.md#sdlc-trace) does, and not the time the story spent
waiting for you. Once a gate is past its number, `sdlc gate`,
`sdlc review add` and `sdlc status` each print one more line saying so, and
then do exactly what they would have done. The line comes back on every one of
those commands for the rest of the story. To silence it, set that gate's
number to `0` or take the entry out. The overrun is also noted once on the
story's record, and `sdlc trace` marks the gate.

`gate_hours` never blocks either, and for the same reason: stopping a story
halfway costs more than the overrun.

A key that is not a gate key, a negative number or a value that is not a
number is [refused](troubleshooting.md#sdlc-e0045), like any setting `sdlc`
cannot use.

There is no default to start from, so here are the median and the upper
quartile of the hours each gate took over the 39 stories that built `sdlc`
itself:

| Gate | Median | Upper quartile |
| --- | --- | --- |
| `analysis` | 0.27 | 0.39 |
| `tests_frozen` | 0.80 | 1.54 |
| `plan` | 0.53 | 0.94 |
| `design_review` | 0.49 | 0.96 |
| `implementation` | 0.65 | 1.32 |
| `verification` | 0.52 | 1.31 |
| `code_review` | 0.31 | 0.99 |

These were measured as time on the clock, which includes sessions left open
and idle. `gate_hours` leaves idle time out, so a number taken from this table
is a generous start. Yours will differ: `sdlc trace <ID>` shows the agents'
time each gate took in a story of your own, and `sdlc trace --all` puts your
stories side by side.

## `loop`

`loop` sets how long the loop keeps trying before it hands the story to you.

| Setting | Default | What it does |
| --- | --- | --- |
| `max_review_rounds` | `2` | how many times one reviewer may block a gate before the gate goes to its [arbiter](the-loop.md#the-arbiter), and how many times the arbiter may before the story is handed to you. A block that cannot stop the gate is not counted, and an agent's own [`max_rounds`](#an-agents-entry) takes its place |
| `max_rework_rounds` | `3` | how many times one gate may be recorded as failed before the story is handed to you |
| `max_stop_blocks` | `3` | how many times in a row the hook sends a session back to work when it [stops mid-story](enforcement.md#stopping-mid-story) with nothing recorded, before the story is handed to you |

```json
"loop": { "max_review_rounds": 3, "max_rework_rounds": 3, "max_stop_blocks": 3 }
```

**When you change it.** Each is a whole number. A higher limit lets the
agents try for longer before you hear of it, and costs what those extra
rounds cost. `0` turns that limit off, so the loop never stops to ask for
that reason, and a negative number is refused.
[When a reviewer blocks](guides/when-a-reviewer-blocks.md) shows what a
hand-over looks like.

## `freeze`

`freeze` changes what the test freeze allows. It has one setting.

### `freeze.allow_new_test_files`

| Setting | Default | What it does |
| --- | --- | --- |
| `allow_new_test_files` | `false` | with `true`, the test author may add a test file after the freeze has been taken |

```json
"freeze": { "allow_new_test_files": true }
```

**When you change it.** A new test file is not frozen by being written.
Either way the story's next gate
[refuses to pass](troubleshooting.md#sdlc-e0043) while there is a test file
the freeze does not hold. With the setting on, the test author runs
`sdlc freeze` again, which adds the file. With it off, `sdlc freeze` does not
add it, and the way through is
[yours](guides/when-a-frozen-test-is-wrong.md#adding-a-test-file-after-the-freeze).

Where the test reviewer is staffed, an added file is reviewed with the rest
before `sdlc freeze` takes it. Until the test reviewer has approved the test
files as they now are, [the freeze waits](commands.md#sdlc-freeze).

> [!WARNING]
> Turning this on is a real loosening, because a test written after the code
> can be written to pass. It does not let the implementer write one. That rule
> is not a setting.

## `reviews`

`reviews` says where the review policy is, the file every reviewer reads
first. How much a reviewer's word counts is set for each agent in
[`team`](#an-agents-entry), with `can_block`.

| Setting | Default | What it does |
| --- | --- | --- |
| `policy` | `REVIEW.md` | the review policy, relative to the repository root. `init` does not write the setting |

### `reviews.policy`

The review policy tells every reviewer which passes to make, what counts as
important, what is a nit, how many nits to report, and what not to review.
`init` writes one as `REVIEW.md`. Name `reviews.policy` only to keep the file
somewhere else:

```json
"reviews": { "policy": "docs/review-policy.md" }
```

**When you change it.** Give the path relative to the repository root. It is
[refused](troubleshooting.md#sdlc-e0045) where it leaves the repository, has
a `..` in it, ends in `/`, or holds white space, a quote, a backslash, a
backtick or any of `$`, `;`, `&`, `|`, `<`, `>`, `(` and `)`.

With no file at the path, the project has no review policy, and reviewers
review as they do without one. `sdlc doctor` does not warn about a missing
`REVIEW.md`, and does warn about a missing file you named. It warns too where
the path is a directory, or a link that leads out of the repository, which
counts as no policy.

No agent [writes the policy](enforcement.md#review-policy-is-not-edited)
while a story is being worked on, and no story lands a change to it. Commit a
change on trunk between stories. A story under way keeps the policy it
started with.

### `reviews.gate7_advisory`

This setting is gone. It moved to `team.agents.code-reviewer.can_block`, which
takes the opposite value: to make the code reviewer advisory, set
`"can_block": false` on it. A file with `"gate7_advisory": true` is refused,
and `"gate7_advisory": false` is a leftover that changes nothing and can be
deleted. [Upgrading from the staffing section](#upgrading-from-the-staffing-section)
shows both.

## `backlog`

`backlog` names the file your stories live in. It has one setting.

### `backlog.path`

| Setting | Default | What it does |
| --- | --- | --- |
| `path` | `user_stories.json` | the backlog file, relative to the repository root |

```json
"backlog": { "path": "docs/stories.json" }
```

**When you change it.** Move the file in the same commit, between stories.
The path has to be inside the repository, because the backlog is committed
with the work and protected while a story runs: an absolute path, or one that
climbs out with `..`, is refused.
[Writing stories](guides/writing-stories.md) says what goes in the file.

## `spec`

`spec` names where your project's own specifications live. It has one setting.

### `spec.paths`

| Setting | Default | What it does |
| --- | --- | --- |
| `paths` | `["docs/", "spec/", "README.md"]` | the files and directories the researcher reads at Gate 2, before it writes anything |

```json
"spec": { "paths": ["docs/", "adr/", "README.md"] }
```

**When you change it.** A folder of specifications that is not listed is not
given to the analysis, so add yours here if it has another name.

## `consolidate`

`consolidate` sets when the loop reminds you to run `/sdlc:consolidate`, the
skill that turns what several finished stories show into changes to your
contract and your settings.

| Setting | Default | What it does |
| --- | --- | --- |
| `nudge_after` | `3` | how many stories must have finished since you last ran `/sdlc:consolidate` before `sdlc status` and `sdlc stop` end with a line saying so. `0` turns the reminder off |

```json
"consolidate": { "nudge_after": 5 }
```

**When you change it.** `init` does not write this section, and left out,
`nudge_after` is 3. It is a whole number, and a negative one is refused. A
story counts when it finished on a later day than your last consolidation, so
a run of the skill ends the reminder at once.
[The reminder](guides/consolidating-what-the-retros-say.md#the-reminder) says
how the count is kept.

## `version`

`version` is the version of the file's format, which is `1`. Leave it alone.
It is how a later release recognises a file written by this one, and how this
one refuses a file written by a newer release without misreading it.

| Setting | Default | What it does |
| --- | --- | --- |
| `version` | `1` | says which format the file is in |

```json
"version": 1
```

**When you change it.** Don't. A number above `1`, or one of `0` or less, is
[refused](troubleshooting.md#sdlc-e0045). Where a newer `sdlc` wrote the
file, the fix is to upgrade.

## Settings read from the environment

A few settings are not in the file, because they belong to the person who
starts the session and not to the project. They are read from the environment
Claude Code was started in, and nothing run inside a session changes them.

| Variable | What it does |
| --- | --- |
| `SDLC_ENFORCE` | set to `0`, disables every rule of the hook: [Turning it off](enforcement.md#turning-it-off) |
| `SDLC_DEBUG`, `SDLC_DEBUG_FILE`, `SDLC_TRACE` | write a log of what the hook decided, and how long it took: [Seeing what happened](enforcement.md#seeing-what-happened) |
| `SDLC_HOOK_DEADLINE_MS` | shortens the hook's deadline, as described next |

### `SDLC_HOOK_DEADLINE_MS`

The hook gives itself 25 seconds to check a call, and
[refuses a call](enforcement.md#not-checked-in-time) it could not check in
that time. `SDLC_HOOK_DEADLINE_MS` can only shorten that deadline. It is a
whole number of milliseconds, above 0 and below 25000:

```console
$ export SDLC_HOOK_DEADLINE_MS=10000
```

Any other value leaves the 25 seconds in force, and nothing in the session
says the value was not taken. Nothing lengthens the deadline.

A shorter deadline turns no rule off. It refuses the calls that outlast it,
and which calls those are depends on how busy your machine is. It is there for
testing the refusal, and for a person who would sooner be refused than wait.

## The other half of the contract

`.sdlc/config.json` holds what a program can check. The other half of your
project's contract with the loop is prose: a `## SDLC Contract` section in
`CLAUDE.md`, or in `AGENTS.md` where your project keeps its instructions
there. The hook does not read it. The agents do, and each part of it has a
reader:

| Part | Who works from it |
| --- | --- |
| Phase constraints | the researcher, the test author, the implementer and the architect |
| Architecture rules | the implementer, when it plans, and the architect, who blocks a plan that breaks one |
| Test conventions | the test author |
| Code conventions | the code reviewer |
| Domain glossary | the researcher and the human advocate, who flag a name that does not match |
| Performance budgets | the performance reviewer, which blocks only when one is breached |
| Security and data rules | the security reviewer |

`sdlc init` appends the section as a template, with most fields marked `TBD`.
Write each rule as a sentence somebody could check against a diff:

```markdown
### Architecture rules (the design reviewer blocks on these)

- Dependency direction: `internal/billing` imports nothing under `internal/http`.
- Errors: billing returns typed errors such as `ErrNonPositiveTotal`; no panics.
```

You change the contract the way you change a setting: on trunk, between
stories, with a commit. While a story is running, the hook
[refuses an agent](enforcement.md#write-protected-path) that tries to write
the file, so the rules a story is reviewed against cannot be changed by the
story. Keep the section under about 150 lines, because it is loaded into
every session.

`sdlc doctor` reports a project that has no such section. A plain `sdlc init`
does not put it back in a project that is already set up.
`sdlc init --force` does, and puts your settings back to their defaults as it
goes, so restore `.sdlc/config.json` from git afterwards.

Once a few stories have finished, `/sdlc:consolidate` reads their retros and
proposes changes to both halves from what repeats. It applies only the ones
you confirm.
[Fill in the contract](guides/adding-to-an-existing-project.md#fill-in-the-contract)
has more on writing the section, and
[Consolidating what the retros say](guides/consolidating-what-the-retros-say.md)
on improving it.

## Where to go next

- [Tuning commands for your stack](guides/tuning-commands-for-your-stack.md):
  the commands and the test paths, with examples for each stack
- [Agents](the-agents.md): who the `team` section configures
- [Human approval gates](guides/human-approval-gates.md): where the loop waits
  for you, and how to answer
- [Commands](commands.md): every command and flag, `sdlc doctor` among them
- [Troubleshooting](troubleshooting.md): every `SDLC-E` code a refused file
  can carry
