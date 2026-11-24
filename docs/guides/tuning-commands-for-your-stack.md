# Tuning commands for your stack

- [Introduction](#introduction)
- [See what `init` guessed](#see-what-init-guessed)
- [Make the commands match your project](#make-the-commands-match-your-project)
  - [Check them with `sdlc doctor`](#check-them-with-sdlc-doctor)
  - [Commit the change](#commit-the-change)
- [What each command is for](#what-each-command-is-for)
  - [What a command has to do](#what-a-command-has-to-do)
- [Give a new worktree what it needs](#give-a-new-worktree-what-it-needs)
- [Measure coverage](#measure-coverage)
- [Tell sdlc which files are tests](#tell-sdlc-which-files-are-tests)
  - [Directories](#directories)
  - [File patterns](#file-patterns)
  - [Check what the freeze covers](#check-what-the-freeze-covers)
  - [Leaving a list out](#leaving-a-list-out)
- [Say where the source lives](#say-where-the-source-lives)
- [Where to go next](#where-to-go-next)

## Introduction

The loop does not guess how to build or test your project. It runs the
commands in `.sdlc/config.json`: the agents run them at their gates, and `sdlc`
runs a few itself. When a command is wrong, the gate that runs it fails, so
these are the settings most projects adjust first.

`sdlc init` fills them in from what it finds in your repository. For many
projects the guess is close. It cannot know that you run your tests through
`make`, or that your fixtures live in a directory of their own.

This guide shows how to make the commands match what you really run, and how
to tell `sdlc` which files are tests. There are examples for Go, Rust, Node
and Python, the four stacks `init` recognises.

## See what `init` guessed

Open `.sdlc/config.json` and find the `commands` object. Here is what `init`
writes for each stack. A command left as `""` is skipped.

| Command | Go | Rust | Node | Python |
| --- | --- | --- | --- | --- |
| `smoke` | `go vet ./...`, where there is a package | `cargo check --all-targets` | the same as `build` | none |
| `build` | `go build ./...` | `cargo build` | `npm run build --silent` | none |
| `test` | `go test ./... -count=1` | `cargo test` | `npm test --silent` | `pytest -q` |
| `test_files` | `go test` over the packages that hold the files | `cargo test --test` for each file under `tests/` | `npm test` with the files | `pytest` over the files |
| `lint` | `golangci-lint run` | `cargo clippy --all-targets -- -D warnings` | `npm run lint --silent` | `ruff check .` |
| `fmt` | `gofmt -w .` | `cargo fmt` | `npx prettier --write .` | `ruff format .` |
| `fmt_check` | `gofmt -l .`, which must list nothing | `cargo fmt --check` | `npx prettier --check .` | `ruff format --check .` |
| `fmt_file` | `gofmt -w` on a `.go` file | none | `npx prettier --write --ignore-unknown` | `ruff format` on a `.py` file |
| `coverage` | the figure from the cover profile `test` wrote | none | none | the figure from the data `pytest --cov` wrote |

For Node, `init` reads your `package.json`. It fills in `build`, `test`,
`test_files` and `lint` only where there is a script of that name. It formats with your
`format` and `format:check` scripts where you have them, and with Prettier
where that is a dependency. No stack gets `mutation` or `freeze_check`.

For Go and Python, `init` writes `test` and `coverage` as a pair that share one
run of the suite. When a gate runs them, `test` also leaves its coverage data
for `coverage` to read, so the suite runs once. Run by hand, each does its own
work. [Configuration](../configuration.md#the-run-directory) says how the pair
works, and what a pair for Rust or Node looks like.

> [!NOTE]
> A repository with two stacks in it is set up as one of them, and `init` says
> so: `Guessed the commands from a Node project. This repository also has
> Python in it,` is the first line of that message. Check which one it chose
> before you rely on the commands.

## Make the commands match your project

Each value is a shell command, run from the root of the checkout the story is
worked in. Let's say your project builds, tests and lints through a `Makefile`.
Change those three commands to the ones you type yourself:

```json
"commands": {
  "build": "make build",
  "test": "make test",
  "lint": "make lint"
}
```

Leave the other keys as they are. Now the agents build, test and lint the way
you do.

You may notice `\u0026\u0026` and `\u003e` in the commands `init` wrote. They
are `&&` and `>`, spelled the way `init` writes JSON. You may type either
spelling, and both mean the same.

> [!NOTE]
> This file is yours to edit by hand. While a story is being worked on, the
> hook [refuses](why-was-i-refused.md#from-the-hook) an agent's attempt to
> change it, so that a story is never held to commands it rewrote itself.

### Check them with `sdlc doctor`

Run `sdlc doctor` after every edit to the configuration:

```console
$ sdlc doctor
```

`doctor` looks up the program each command starts with, so a typo or a tool
that is not installed shows up before a story trips over it. With `mak test`
in place of `make test`, its report has these two lines:

```text
  problem  commands on PATH     mak is not installed, so test would fail
                                fix: install mak, or clear commands.test in .sdlc/config.json
```

It also tells you when a command does something the hook would refuse during
a story, such as committing. It never runs your commands, so a quiet `doctor`
means they can start, not that they pass. [`sdlc doctor`](../commands.md#sdlc-doctor)
says exactly what it checks.

### Commit the change

Commit the configuration to trunk before you start the next story:

```console
$ git add .sdlc/config.json
$ git commit -m "Run the loop's commands through make"
```

> [!WARNING]
> Don't leave the edit uncommitted. Each story is worked in a worktree of its
> own, cut from what trunk commits, so a story reads the committed
> `.sdlc/config.json` and never the copy you are editing. An uncommitted change
> reaches no story at all.

## What each command is for

You don't need every command. Fill in `test` and `lint` at least, and add the
others as your project has them.

| Command | When it runs |
| --- | --- |
| `test_files` | Runs only the test files a story adds or changes. The test author runs it at Gate 3, and the implementer after each step of the plan. `sdlc freeze` runs it over the new tests, which must all fail. |
| `test` | Runs the whole suite. `sdlc` runs it at Gate 5, once the frozen tests pass. `sdlc freeze` runs it where `test_files` is empty. |
| `build`, `lint`, `fmt_check` | `sdlc` runs them at Gate 5 with `test`. |
| `coverage`, `mutation` | `sdlc` runs them at Gate 5 with `test`, and holds the number each prints to its [threshold](#measure-coverage). |
| `fmt` | The implementer runs it once the frozen tests pass. |
| `smoke` | `sdlc start` runs it before each new story, in the story's new worktree. If it fails, the story does not start. |
| `fmt_file` | The hook runs it on each file an agent writes during a story, with `$FILE` set to that file. |
| `freeze_check` | `sdlc freeze` runs it before the tests are frozen. It is empty until you choose one: see [The freeze check](../configuration.md#the-freeze-check). |

At Gate 5, `sdlc` runs `build`, `test`, `lint`, `fmt_check`, `coverage` and
`mutation`, those that are set, in that order, and a command that fails, or a
number below its threshold, refuses the gate. The verifier at Gate 6 reads that
result and runs the commands itself only where the tree has changed since. The
landing reuses it too, and runs the commands again only where the tree it would
land is not the one Gate 5 measured, as when trunk has moved.
[At landing](../configuration.md#at-landing) says when, and what counts as passing.

A key of your own is not run by anything, but the agents can read it.

### What a command has to do

A few things follow from who runs each command:

- **`test` runs the whole suite, and exits.** Gate 5 wants all of it, because a
  change that fixes its own tests and breaks three others has not passed. A
  script that starts a watcher never returns.
- **`test_files` runs only the files it is given.** It reads their names, one
  to a line, from the file `SDLC_TEST_FILES` names. Where it is empty, the test
  author and the implementer run their own files with your test runner, and
  the freeze runs `test` instead.
- **`smoke` is fast.** It runs before every new story. A story you pick up
  again is not held to it.
- **`fmt_file` says which files it formats.** It runs for every file written,
  whatever its type. The commands `init` writes do this with
  `case "$FILE" in *.go) ... ;; esac`, and Prettier does it with
  `--ignore-unknown`.
- **Nothing is left behind in the project.** A file a command leaves in the
  working tree, and git does not ignore, is uncommitted work, and the commit
  gate [refuses to pass](../troubleshooting.md#sdlc-e0032) while there is any.

## Give a new worktree what it needs

A story's worktree has only what trunk commits. There is no `node_modules`
in it, no virtual environment and no generated code. `smoke` is the first
command to run there, and every later command runs there too, so `smoke` is
where you install what they need.

For Node, `init` writes a `smoke` that only builds, which assumes the
dependencies are already installed. Install them from the lockfile first:

```json
"smoke": "npm ci && npm run build --silent"
```

Without that, the build fails in the new worktree, or Node finds the
`node_modules` of your main checkout from inside `.sdlc/worktrees/` and the
story builds against what you happen to have installed.

For Python, `init` writes no `smoke` at all. If your tests need packages
installed, create the virtual environment in `smoke` and run the tools from
it:

```json
"smoke": "python -m venv .venv && .venv/bin/pip install --quiet -r requirements.txt",
"test": ".venv/bin/python -m pytest -q"
```

Make sure git ignores `.venv`, so that it is not taken for the story's work.

Go and Rust keep their dependencies outside the project, so the commands
`init` writes work in a new worktree as they are. One thing to watch for in
Go: a `go.work` file at the root of your main checkout takes over the build in
the worktrees beneath it. [Installing what a worktree
needs](../configuration.md#installing-what-a-worktree-needs) covers that and
other tools that look upwards.

When `smoke` fails, `sdlc start` says so, shows what the command printed and
removes the worktree again. Nothing is left half-started.

## Measure coverage

The verifier compares two numbers with the `thresholds` in the same file:

```json
"thresholds": { "coverage_min": 80, "mutation_min": 70 }
```

Those are the defaults. `coverage_min` is held against what `commands.coverage`
prints, and `mutation_min` against `commands.mutation`. A threshold of `0` is
not enforced, and a threshold with no command has nothing to be compared with.

The rule for both commands is the same: **print the percentage as the last
number on standard output.** How you get there is up to you. `init` writes a
coverage command for Go and for Python, and each pairs with the `test` it
writes. Here is the pair for Go:

```json
"test": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then go test ./... -count=1 -coverprofile=\"$SDLC_RUN_DIR/cover.out\"; else go test ./... -count=1; fi",
"coverage": "if [ -n \"${SDLC_RUN_DIR-}\" ]; then test -f \"$SDLC_RUN_DIR/cover.out\" && go tool cover -func=\"$SDLC_RUN_DIR/cover.out\" | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'; else p=$(mktemp) && trap 'rm -f \"$p\"' EXIT && go test ./... -count=1 -coverprofile=\"$p\" >/dev/null && go tool cover -func=\"$p\" | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'; fi"
```

When `sdlc` runs them at Gate 5, it sets `SDLC_RUN_DIR` to a new directory
outside your project, the same one for every command of that run. `test` leaves
its cover profile there, and `coverage` reads the figure from it, so the suite
runs once. Run by hand, with no `SDLC_RUN_DIR`, each does its own work, and
`coverage` writes its profile to a temporary file that it removes on the way
out. The Python pair does the same with pytest-cov's data file. A command of
your own should leave nothing behind in the project, so that the commit gate
has nothing to refuse.

`init` never guesses coverage for Node or Rust, and there the pair is yours to
write. [The run directory](../configuration.md#the-run-directory) gives one for
each: cargo-llvm-cov for Rust, and c8 for Node. Until you add a coverage
command, the verifier measures no coverage for the story.

A `coverage` command written before the pair, one that runs the suite itself,
still works. It runs the suite a second time.

## Tell sdlc which files are tests

`paths.tests` is the one setting on this page worth reading slowly. It decides
what the freeze covers, and so what the implementer may never touch. `init`
fills it in for your stack:

| Stack | `dirs` | `file_globs` |
| --- | --- | --- |
| Go | `testdata/` | `*_test.go` |
| Rust | `tests/`, `testdata/`, `fixtures/` | `*_test.rs` |
| Node | `tests/`, `test/`, `__tests__/`, `__snapshots__/`, `__mocks__/`, `__fixtures__/` | `*.test.ts`, `*.test.js`, `*.spec.ts`, `*.spec.js`, `*.snap` |
| Python | `tests/`, `test/`, `fixtures/`, `testdata/` | `test_*.py`, `*_test.py`, `conftest.py` |

A file is a test if either list matches it. You only need to change them when
your project keeps tests, or the things tests read, somewhere else.

The second half of that matters most. Anything that decides whether a test
passes belongs here: golden files, snapshots, fixtures, a `conftest.py`. A
frozen test that reads a fixture nobody froze can be made to pass by changing
the fixture, and the freeze would never notice.

Let's take a look at a Node project that keeps its component tests beside the
code as `*.test.tsx`, which `init` does not list:

```json
"paths": {
  "tests": {
    "dirs": ["tests/", "test/", "__tests__/", "__snapshots__/", "__mocks__/", "__fixtures__/"],
    "file_globs": ["*.test.ts", "*.test.tsx", "*.spec.ts", "*.spec.tsx", "*.snap"]
  },
  "src": ["src/", "lib/"]
}
```

> [!WARNING]
> In Rust, a unit test inside a source file, under `#[cfg(test)]`, is in no
> test file, so the freeze cannot hold it. Keep the acceptance tests under
> `tests/`.

### Directories

Most projects need only a directory's name in `dirs`. How you write the entry
decides how far it reaches:

| Entry | Matches |
| --- | --- |
| `tests/` or `tests` | a directory named `tests` at any depth: `tests/a.py`, and `internal/pkg/tests/b.txt` too |
| `src/fixtures` | that one directory, from the root, and everything under it |
| `/src/fixtures/` | the same as `src/fixtures` |
| `./fixtures` | nothing. Don't start an entry with `./` |

A slash at either end changes nothing. Only a slash in the middle ties an
entry to one place. That is why `testdata/` covers every Go package's golden
files, and `__snapshots__/` covers the snapshot directory beside every test.

### File patterns

Use `file_globs` for tests that sit beside the code they test. Each pattern is
tried against the whole path and against the file's own name. A `*` does not
cross a `/`, and `**` stands for any number of directories, including none:

| Pattern | Matches |
| --- | --- |
| `*.test.ts` | `z.test.ts`, `src/a/y.test.ts` and `src/a/b/x.test.ts` |
| `src/*/*.test.ts` | only `src/a/y.test.ts`, exactly one level down |
| `src/**/*.test.ts` | `src/z.test.ts`, `src/a/y.test.ts` and `src/a/b/x.test.ts`, and nothing outside `src/` |
| `e2e/**` | everything under `e2e/` |
| `conftest.py` | that file at the root and in every package |
| `*_test.go` | `internal/Foo_Test.go` as well: matching ignores case |

To match a kind of file anywhere in the tree, write the file name pattern on
its own.

### Check what the freeze covers

You don't have to trust the patterns. At Gate 3, `sdlc freeze` prints every
file it froze:

```text
AUTH-3  1 test file frozen
  internal/auth/session_test.go
```

Read that list the first time a story reaches the gate, and look for the
fixture you expected to see. One thing can surprise you here: the freeze only
sees the files git does not ignore. A fixture your `.gitignore` excludes is
never frozen.

Change `paths.tests` between stories, not during one. A file that becomes a
test halfway through a story is a test the freeze does not hold, and the
story's next gate [refuses to pass](../troubleshooting.md#sdlc-e0043) until
that is settled.

### Leaving a list out

A list you leave out is not an empty list. `sdlc` fills in a default for it:

- with no `dirs`: `tests/` and `test/`
- with no `file_globs`: `*_test.go`, `*.test.ts`, `*.spec.ts`, `test_*.py`,
  `*_test.py`, `*Test.java` and `*_spec.rb`

Write `[]` to mean none. With both lists empty there is nothing to freeze, and
`sdlc freeze` [says so](../troubleshooting.md#sdlc-e0024) and stops.

## Say where the source lives

`paths.src` lists where production code lives:

```json
"src": ["cmd/", "internal/", "pkg/"]
```

The researcher looks there first at Gate 2, the implementer puts new code
there, and the code reviewer uses it to tell a change's code from its tests.
It is a hint, and nothing refuses a write outside it. A build file or a
migration can be part of a story too.

## Where to go next

- [Configuration](../configuration.md): every setting and its default
- [`sdlc doctor`](../commands.md#sdlc-doctor) and
  [`sdlc freeze`](../commands.md#sdlc-freeze) in the command reference
- [When a frozen test is wrong](when-a-frozen-test-is-wrong.md): what the
  freeze means once a story is under way
- [Adding sdlc to an existing project](adding-to-an-existing-project.md): the
  rest of what `init` writes
