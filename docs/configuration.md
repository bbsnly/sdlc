# Configuration

Everything the loop knows about your project lives in `.sdlc/config.json`.
`sdlc init` writes it with values that already match the stack it found, so in
most projects you will only ever change `commands` and `paths`.

A field you leave out keeps its documented default. The file is loaded over the
defaults rather than replacing them, so an old configuration keeps working when
a new field arrives.

## What `init` writes for a Go project

Leaving out the notes it writes under `_` keys, and `mutation`, which it leaves
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
  "thresholds": { "diff_size_cap": 500, "coverage_min": 80, "mutation_min": 70 },
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

## `version`

The configuration format's version. Leave it alone; it is how a future release
recognises a file written by this one.

## `backlog.path`

Where your stories live, relative to the repository root. Default
`user_stories.json`.

## `git`

| Key | What it means |
| --- | --- |
| `trunk_branch` | the branch a story starts from, is committed to, and is diffed against. `sdlc start` begins new work only there. Default `main` |
| `remote` | whether this repository has an `origin` to keep up with. With it on, `sdlc start` fetches trunk from `origin` and will not begin new work while trunk is behind it. Off by default, and the loop never pushes either way |

## `commands`

How to build, test and check this project. The loop runs these rather than
guessing at a toolchain, and `sdlc doctor` checks that the programs they name
are installed.

| Key | When it runs |
| --- | --- |
| `build` | the verifier, at Gate 6 |
| `test` | the test author at Gate 3, the implementer after each step, the verifier at Gate 6 |
| `lint` | the verifier |
| `fmt` | the implementer, when the frozen tests pass |
| `fmt_check` | the verifier |
| `fmt_file` | the hook, on each file a tool writes while a story is being worked on |
| `coverage` | the verifier, compared against `thresholds.coverage_min` |
| `mutation` | the verifier, compared against `thresholds.mutation_min` |
| `smoke` | `sdlc start`, on trunk, before each new story. Keep it fast: it runs before every story |

Any key you add is available to the agents; these are the ones the loop looks
for by name. A key you leave out is simply not run.

`fmt_file` is run by `sh` from the repository root, with `$FILE` set to the file
that was written. It runs for every file, so a formatter that understands only
some should say which — the commands `sdlc init` writes do:

```json
"fmt_file": "case \"$FILE\" in *.go) gofmt -w \"$FILE\" ;; esac"
```

A formatter that fails is reported to the session with what it printed, and the
file stays as it was written. Nothing is refused: the write has already
happened.

One thing worth knowing about the guessed `coverage` command: it writes a
coverage profile to `.sdlc/state/cover.out`. That is a build artefact rather
than loop state, and Gate 8 commits the whole story with `git add -A`, so if
you would rather it did not land in your history, either send the profile
somewhere you already ignore or add the one line:

```gitignore
.sdlc/state/cover.out
```

Nothing decides that for you — `sdlc init` writes no `.gitignore`, because what
a project commits is the project's decision.

## `thresholds`

| Key | What it means |
| --- | --- |
| `diff_size_cap` | the most lines one story's change may add and remove, counted by `git diff --numstat` against the last commit — tests included, `.sdlc/` and the backlog not. The analysis gate proposes a split rather than planning past it, and `implementation`, `verification` and `code_review` refuse a pass over it: [SDLC-E0042](troubleshooting.md#sdlc-e0042) |
| `coverage_min` | the coverage the verifier holds the change to, when `commands.coverage` is configured |
| `mutation_min` | the mutation score the verifier holds the change to, where a project measures one |

A threshold of `0` is not enforced.

## `paths.tests`

**This is the important one.** It decides what the freeze covers, and therefore
what the implementer cannot touch.

| Key | What it means |
| --- | --- |
| `dirs` | a directory and everything beneath it is tests. A bare name — `testdata`, `__snapshots__` — is a directory of that name wherever it is, because that is what projects mean by it; one with a slash inside it, like `src/fixtures`, is that directory and no other. A slash at either end does not count: `tests/` is the bare name `tests` |
| `file_globs` | a pattern matched against the whole path *and* against the file's own name, so `*_test.go` finds `internal/store/x_test.go` |

**Anything that decides whether a test passes belongs here, not only the test
files.** A frozen test that reads a golden file is frozen only if the golden
file is too; otherwise the fixture is the way round the freeze. `sdlc init`
covers the usual ones for the stack it finds — `testdata/` for Go, `tests/`
and `fixtures/` for Rust, `conftest.py` and `fixtures/` for Python,
`__snapshots__/`, `__mocks__/` and `*.snap` for Node — and if your project
keeps them somewhere else, add it here. A Rust unit test inside a source file,
under `#[cfg(test)]`, is in no test file, so the freeze cannot hold it: put the
acceptance tests under `tests/`.

The test author is refused when it writes outside what this describes, and the
refusal says so.

## `paths.src`

Where production code lives. The researcher looks there first at Gate 2, the
implementer puts new code there, and the code reviewer reads it with
`paths.tests` to tell a change's code apart from its tests.

Nothing refuses a write outside it. A build file, a migration or a
configuration change can be part of a story, and a rule that refused them would
be worked around rather than followed.

## `spec.paths`

Where this project's own specifications live. The analysis gate reads these
before it writes anything.

## `loop`

| Key | What it means |
| --- | --- |
| `max_review_rounds` | how many times one reviewer may block a gate before `sdlc review add` hands the story to a person. A block that cannot stop the gate is not counted |
| `max_rework_rounds` | how many times one gate may be recorded as failed before `sdlc gate` hands the story to a person |
| `max_stop_blocks` | how many stops in a row, with nothing recorded on the story, the `Stop` hook sends back before it hands the story to a person. `0` turns it off. See [stopping mid-story](enforcement.md#stopping-mid-story) |

## `reviews.gate7_advisory`

Makes the code reviewer advisory rather than blocking. Off by default, and
turning it on is a real loosening: Gate 7 is the last thing between a change and
trunk.

Advisory is not skipped. `code_review` still waits for the code reviewer's
report on the change as it stands — a missing or stale review holds the gate as
it always does — but a `block` from it no longer stops the gate.
`sdlc review list` shows it as advisory. The verifier stays blocking, and so
does security on a story Gate 2 called security-sensitive.

## `freeze.allow_new_test_files`

Lets the test author add a test file after the freeze has been taken. Off by
default.

A new file is not frozen by being written. Until `sdlc freeze` is run again,
which adds it to the freeze, every gate from `tests_frozen` to `commit` refuses
with [SDLC-E0043](troubleshooting.md#sdlc-e0043); with the setting off, the
same gates refuse and `sdlc freeze` does not add it.

It is a real loosening — a test written after the implementation can be written
to pass — and it does **not** let the implementer write one. That rule is not a
setting.

## `human_gates`

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

| Key | What it means |
| --- | --- |
| `per_story_usd` | what one story is expected to cost. `0` turns the budget off and still keeps the total |
| `alert_fractions` | the points along the way to say so, as fractions of the budget |

Spend is recorded with [`sdlc cost add`](commands.md), and shown by `sdlc cost` and
`sdlc status`. An alert goes to standard error once, on the entry that crosses it. Nothing
blocks: a story stopped halfway costs more than the overspend.

## The other half of the contract

A project takes part in the loop when it has **both** `.sdlc/config.json` and a
`## SDLC Contract` section in its `CLAUDE.md`. The hooks do nothing at all in a
project without them.

The contract is prose, not settings, and it is what the design reviewer and the
code reviewer hold the work to: architecture rules, phase constraints, test
conventions, allowed dependencies, and the glossary that keeps everyone using
the project's own words. `sdlc init` appends a template with every field marked
`TBD`; filling it in is the highest-value thing you can do before your first
story.
