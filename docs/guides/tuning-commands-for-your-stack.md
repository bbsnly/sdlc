# Tuning commands for your stack

- [Introduction](#introduction)
- [Make the commands match your project](#make-the-commands-match-your-project)
  - [Check them with `sdlc doctor`](#check-them-with-sdlc-doctor)
- [`commands`](#commands)
  - [Rules that follow from who runs them](#rules-that-follow-from-who-runs-them)
  - [What `doctor` checks](#what-doctor-checks)
- [`thresholds`](#thresholds)
- [`paths.tests`](#pathstests)
  - [`dirs`](#dirs)
  - [`file_globs`](#file_globs)
  - [Only files git knows](#only-files-git-knows)
  - [Omitted is not empty](#omitted-is-not-empty)
- [`paths.src`](#pathssrc)
- [A Go project](#a-go-project)
- [A Node/TypeScript project](#a-nodetypescript-project)
- [A Python project](#a-python-project)
- [Where to go next](#where-to-go-next)

## Introduction

`sdlc init` guesses your commands and test paths from a single marker file.
For many projects the guess is close, but it cannot know that you run your
tests through `make`, or that your fixtures live in a directory of their own.

This guide shows the one change most projects make: editing the `commands` in
`.sdlc/config.json` so they match what you really run, and checking them with
`sdlc doctor`. After that it explains what `commands`, `thresholds`,
`paths.tests` and `paths.src` do inside the loop, and how test paths are
matched. It ends with configurations for a Go, a Node/TypeScript and a Python
project.

## Make the commands match your project

Open `.sdlc/config.json` and look at the `commands` object. Each value is a
shell command that the agents (and in two cases `sdlc` itself) run from the
root of the checkout the story is worked in. A key you leave out is not run.
(`paths.tests` is different: see [Omitted is not empty](#omitted-is-not-empty).)

Let's take a look at an example. Suppose your project builds, tests and lints
through a `Makefile`, and `init` guessed something else. You would change the
three commands to the ones you type yourself:

```json
"commands": {
  "build": "make build",
  "test": "make test",
  "lint": "make lint"
}
```

Now the agents build, test and lint the way you do. Save the file and run
`sdlc doctor`, which is described next.

Commit the change to trunk before you start the next story. A story's worktree
reads trunk's committed `.sdlc/config.json`, not the copy in your checkout, so
an edit that is not committed does not reach it.

> [!NOTE]
> You edit this file by hand. While a story is running, the hook refuses any
> attempt by the assistant to write it.

### Check them with `sdlc doctor`

Run `sdlc doctor` after every edit to the configuration:

```console
$ sdlc doctor
```

It looks for the programs your commands name, so a typo such as `mak test`, or a
tool that is not installed, shows up before a story trips over it. [What
`doctor` checks](#what-doctor-checks) says how it reads a command.

Don't worry about getting every key right the first time. The commands you
change are the ones the agents run, so you will see quickly if one is wrong.
The sections below explain what each key is for, and then the paths that tell
`sdlc` which files are tests.

## `commands`

Commands run from the root of the checkout the story is worked in, in a shell:
with [`git.worktrees`](../configuration.md#git) on, the story's own worktree.
Most are run by the agents; two are run by `sdlc` itself.

| Key | Who runs it |
| --- | --- |
| `test` | the test author at Gate 3 (every new test must fail), the implementer after each plan step, the verifier at Gate 6 |
| `build`, `lint`, format | the implementer, once the frozen tests pass |
| `build`, `lint`, `fmt_check`, `coverage`, `mutation` | the verifier at Gate 6, which blocks on any that fails |
| `smoke` | `sdlc start`, before each new story: in the story's new worktree, or on trunk with `git.worktrees` off; a failure refuses the start with [SDLC-E0041](../troubleshooting.md#sdlc-e0041) |
| `fmt_file` | the hook, on each file a tool writes while a story is being worked on, with `$FILE` set to that file |

Any key you add stays available for the agents to read, but nothing runs it by
name. An empty string is not run.

### Rules that follow from who runs them

A few rules follow from the table above:

- **`test` runs the whole suite.** The verifier runs all of it, because a
  change that fixes its own tests and breaks three others has not passed.
- **It has to exit.** A test script that starts a watcher never returns.
- **`smoke` runs before every new story**, so keep it fast. A story picked up
  again is not held to it.
- **`smoke` runs in a tree with nothing installed.** With
  [`git.worktrees`](../configuration.md#git) on, the default, it runs in the
  story's new worktree, which has only what trunk commits, and every later
  command runs there too. A build that needs installed dependencies installs
  them in `smoke`, and the change is committed to trunk, because the worktree
  reads trunk's committed `.sdlc/config.json`.
- **`fmt_file` runs for every file written**, whatever its type. A formatter
  that understands only some files should say which, as the commands `init`
  writes do with `case "$FILE" in ...`.
- **`coverage` and `mutation` print their percentage as the last number on
  standard output**, as the comment at the top of the file says.
  `thresholds` is compared against that number.

### What `doctor` checks

`doctor` checks that the first word of each part of a command is on your
`PATH`. It splits the command at `&&`, `||`, `|`, `;` and command
substitutions, looks past a `case` pattern to the program it runs, and skips
shell builtins such as `test`. It does not run anything.

## `thresholds`

The verifier compares the output of `commands.coverage` and `commands.mutation`
against two numbers, and a missed one is a reason for it to block:

| Key | Default | Used by |
| --- | --- | --- |
| `coverage_min` | `80` | the verifier, against the output of `commands.coverage` |
| `mutation_min` | `70` | the verifier, against the output of `commands.mutation` |

A threshold of `0` is not enforced. No stack
fills in `mutation`, so `mutation_min` has nothing to be compared with until you
add the command.

## `paths.tests`

This decides what the freeze covers, which files the implementer may never
write, and which files the test author may write outside its own story
directory and `CODEMAP.md`. Include anything that decides whether a test
passes: snapshots, golden files, fixtures, `conftest.py`. If you leave one out,
it becomes a way round the freeze.

It has two lists, `dirs` and `file_globs`. A file is a test if either one
matches it.

### `dirs`

Most projects need only a directory name here, such as `tests/`. Leading and
trailing slashes are trimmed first. What is left decides how the entry matches:

| Entry | Matches |
| --- | --- |
| `tests/` or `tests` | a directory named `tests` at any depth: `tests/a.py` and `internal/pkg/tests/b.txt` |
| `src/fixtures` | that one directory and everything under it |
| `/src/fixtures/` | the same as `src/fixtures` |
| `./fixtures` | nothing: do not start an entry with `./` |

A trailing slash does not root an entry. Only a slash in the middle does.

### `file_globs`

Use this for test files that sit beside the code they test. Each pattern is
tried against the whole path and against the file's name. `*` does not cross a
`/`, and a `**` segment stands for any number of directories, including none:

| Pattern | Matches |
| --- | --- |
| `*.test.ts` | `z.test.ts`, `src/a/y.test.ts`, `src/a/b/x.test.ts` |
| `src/*/*.test.ts` | only `src/a/y.test.ts`, exactly one level down |
| `src/**/*.test.ts` | `src/z.test.ts`, `src/a/y.test.ts` and `src/a/b/x.test.ts`, but nothing outside `src/` |
| `e2e/**` | everything under `e2e/` |
| `*_test.go` | `internal/Foo_Test.go` too: matching ignores case |

To match a pattern anywhere in the tree, use the file name alone.

### Only files git knows

`sdlc freeze` asks git for the file list: tracked files, and untracked files
that are not ignored. It then keeps the ones `paths.tests` matches. A fixture
your `.gitignore` excludes is never frozen. `sdlc freeze` prints every file it
froze, so read that list the first time.

The same list is checked again as the story moves. A file that matches
`paths.tests` and that the freeze does not hold refuses `tests_frozen`, `plan`,
`implementation`, `verification` and `commit` with
[SDLC-E0043](../troubleshooting.md#sdlc-e0043). That includes a file that
became a test because you widened `paths.tests` mid-story. With
`freeze.allow_new_test_files` on, running `sdlc freeze` again adds such files.

### Omitted is not empty

If you leave `paths.tests` out of the configuration, or leave out one of its
lists, `sdlc` fills in a default rather than treating the list as empty. If
`file_globs` is missing, it falls back to `*_test.go`, `*.test.ts`,
`*.spec.ts`, `test_*.py`, `*_test.py`, `*Test.java` and `*_spec.rb`. If `dirs`
is missing, it falls back to `tests/` and `test/`. Write `[]` to mean none.
With both empty, `sdlc freeze` refuses with
[SDLC-E0024](../troubleshooting.md#sdlc-e0024).

## `paths.src`

Where production code lives. The researcher looks there first at Gate 2, the
implementer puts new code there, and the code reviewer reads it with
`paths.tests` to tell a change's code apart from its tests. The hook does not
read it: it tells tests from everything else with `paths.tests` alone, and
nothing refuses a write outside `paths.src`.

## A Go project

The generated commands usually need no change. This project adds the race
detector to `test` and keeps end-to-end tests in `test/e2e`:

```json
"commands": {
  "smoke": "test -z \"$(go list ./...)\" || go vet ./...",
  "build": "go build ./...",
  "test": "go test -race ./... -count=1",
  "lint": "golangci-lint run",
  "fmt": "gofmt -w .",
  "fmt_check": "test -z \"$(gofmt -l .)\"",
  "fmt_file": "case \"$FILE\" in *.go) gofmt -w \"$FILE\" ;; esac",
  "coverage": "go test ./... -count=1 -coverprofile=.sdlc/state/cover.out >/dev/null && go tool cover -func=.sdlc/state/cover.out | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'"
},
"paths": {
  "tests": { "dirs": ["testdata/", "test/e2e"], "file_globs": ["*_test.go"] },
  "src": ["cmd/", "internal/", "pkg/"]
}
```

`init` writes `&&` as `\u0026\u0026` and `>` as `\u003e`. Both
spellings are the same JSON string. `testdata/` covers every package's golden
files, and `test/e2e` covers that one directory. The coverage command writes
its profile into the working tree, which the commit gate sees; see
[`commands`](../configuration.md#commands).

## A Node/TypeScript project

Take a `package.json` with `"build": "tsc"`, `"test": "vitest run"`,
`"lint": "eslint ."` and `prettier` as a dev dependency. `init` wires `build`,
`test`, `lint`, `fmt`, `fmt_check`, `fmt_file` and `smoke`. It never guesses
coverage for Node. This project keeps component tests beside the code as
`*.test.tsx`:

```json
"commands": {
  "smoke": "npm ci && npm run build --silent",
  "build": "npm run build --silent",
  "test": "npm test --silent",
  "lint": "npm run lint --silent",
  "fmt": "npx prettier --write .",
  "fmt_check": "npx prettier --check .",
  "fmt_file": "npx prettier --write --ignore-unknown \"$FILE\"",
  "coverage": "npx vitest run --coverage.enabled --coverage.reporter=json-summary >/dev/null && node -p \"require('./coverage/coverage-summary.json').total.lines.pct\""
},
"paths": {
  "tests": {
    "dirs": ["tests/", "test/", "__tests__/", "__snapshots__/", "__mocks__/", "__fixtures__/"],
    "file_globs": ["*.test.ts", "*.test.tsx", "*.spec.ts", "*.spec.tsx", "*.snap"]
  },
  "src": ["src/", "lib/"]
}
```

`smoke` installs from the lockfile before it builds. `init` writes the build
alone, which assumes `node_modules` is there, and a story's new worktree has
none of its own: the build fails, or Node finds the main checkout's
`node_modules` from inside `.sdlc/worktrees/` and the story builds against what
you installed rather than what trunk locks.

`*.test.tsx` matches a component test at any depth, inside `src/` or not.
`src/**/*.test.tsx` would hold only the ones under `src/`. `__snapshots__/` is a
bare name, so it covers the snapshot directory beside every test file.

## A Python project

`init` gives Python no `build` or `smoke`. A Python project that also has a
`package.json` is set up as Node, so check the stack `init` reported. This
project uses a `src/` layout and runs tools through the virtualenv's
interpreter:

```json
"commands": {
  "smoke": "python -m compileall -q src",
  "test": "python -m pytest -q",
  "lint": "ruff check .",
  "fmt": "ruff format .",
  "fmt_check": "ruff format --check .",
  "fmt_file": "case \"$FILE\" in *.py) ruff format \"$FILE\" ;; esac",
  "coverage": "python -m pytest -q --cov=src --cov-report=term | grep -E '^TOTAL' | grep -Eo '[0-9]+%' | tr -d %"
},
"paths": {
  "tests": {
    "dirs": ["tests/", "fixtures/", "testdata/"],
    "file_globs": ["test_*.py", "*_test.py", "conftest.py"]
  },
  "src": ["src/"]
}
```

`conftest.py` is a pattern, so it matches at the root and in every test
package. Fixtures defined there can change a test's outcome without touching
the test file.

## Where to go next

- [Configuration](../configuration.md): every setting and its default
- [`paths.tests`](../configuration.md#pathstests) in the reference
- [`sdlc freeze`](../commands.md#sdlc-freeze) and
  [`sdlc doctor`](../commands.md#sdlc-doctor)
- [Enforcement](../enforcement.md#sdet-writes-tests-only): what the test
  author is refused
- [Adding sdlc to an existing project](adding-to-an-existing-project.md)
