# sdlc

`sdlc` is a Go CLI and a Claude Code plugin that take one user story at a time through nine
recorded gates. This repository runs the loop on itself, so the released `sdlc` on PATH enforces
the rules while you change the source of the next one.

`./task check` is what CI runs and the only bar that counts. The commands in `.sdlc/config.json`
are the Go subset of it; a change to docs, the npm package, a workflow or an install script also
has to pass the leg of `./task check` that covers it (`markdownlint`, `linkcheck`, `npm-test`,
`actionlint`, `shellcheck`, `powershell`, `release-check`).

## SDLC Contract

<!-- Rules for a story a person works by typing /sdlc:next; nothing here starts the loop or its
     agents. Machine values (commands, thresholds, paths) are in .sdlc/config.json and are
     authoritative; this section holds the rules a script cannot encode. Keep it under ~150
     lines: every line here is loaded into every session. -->

### Phase constraints

- Current phase: released and in use (0.x). What a command prints, its exit code, every
  `SDLC-E` code, the hook's JSON on stdout and the on-disk formats under `.sdlc/` are API: change
  one only when the story says so, and mark the commit breaking (`fix(cli)!:`). `CHANGELOG.md`
  is written when a release is cut, not per story.
- Allowed dependencies: the Go standard library plus what `go.mod` already requires. A new module
  needs a person's decision recorded on the story; small and hand-rolled is preferred, and the
  licence has to pass `./task licenses`. The npm package has no runtime dependencies and keeps none.
- Out of scope unless a story names it: the binary making network calls of its own (only the
  installers and a `git` subprocess touch the network), a stack for `sdlc init` beyond Go, Rust,
  Node and Python, and anything that pushes.

### Architecture rules (the design reviewer blocks on these)

- Dependency direction: `cmd/sdlc` → `cli`, `hook` → `store`, `scaffold`, `policy`,
  `shellpolicy`, `testset` → `config`, `model` → `pathrules`, `sdlcerr`, and the leaves (`fsx`,
  `gitx`, `shellx`, `logging`, `version`). `parallel` sits beside `store`: only `cli` imports it,
  and it imports `shellpolicy`, `model`, `sdlcerr` and `shellx`. Never upward, and `cli` and
  `hook` never import each other. Nothing enforces this but review.
- `cmd/sdlc/main.go` stays a dispatcher. Behaviour goes in a package under `internal/`.
- Stdout is a protocol surface: the hook's stdout is JSON that Claude Code parses. Diagnostics go
  through `internal/logging` to stderr or a file, never stdout.
- The hook fails open on its own bugs: a panic still answers `{"continue":true}`. A refusal is a
  decision the code made, never a side effect of a crash.
- Errors: a package whose errors reach a person builds them with `sdlcerr.New` and `WithCause`
  (the linter refuses `errors.New` and `fmt.Errorf` there). A new code goes in
  `internal/sdlcerr/catalogue.go` with a fix, gets a heading in `docs/troubleshooting.md`, and is
  never reused or renumbered; a dead one is marked retired, not deleted. Tests hold all of it.
- Every refusal says what to do next, in words a person can act on without reading the source.
- Windows, macOS and Linux are all first class: `filepath` for the disk and slash-separated paths
  inside the loop, platform files as `_windows.go` beside `_other.go`, and a command the tool runs
  goes through `internal/shellx`.
- Package `init()` is on the latency path of every tool call in a user's session; keep work out of it.
- Do-not-touch without a person's say on the story: `.github/workflows/`, `.goreleaser.yml`,
  the version in `plugin/.claude-plugin/plugin.json`, and the checksum pins the installers verify.

### Test conventions (the SDET follows these; the verifier checks them)

- Framework and layout: the standard `testing` package only, no assertion library. Tests sit
  beside the code; `test/e2e` drives the built binary through the plugin launcher in a real git
  repository, and `test/install` runs the installers against a local `httptest` mirror.
- A package whose tests run git or read `SDLC_`, `CLAUDE_` or `GIT_` variables starts with
  `func TestMain(m *testing.M) { os.Exit(testenv.Run(m)) }`. Without it the tests pass or fail by
  the developer's own git configuration.
- Names are sentences that state the behaviour: `TestEveryCodeHasATroubleshootingHeading`.
- Naming: every acceptance criterion's id (`AC-n`) appears in the name of the test that
  covers it, or in a subtest name where the language will not take a hyphen (`AC1`, `AC_1`).
- Determinism: no wall-clock sleeps, no network, fixed seeds, and nothing outside `t.TempDir()`.
- A test that cannot run on one platform skips with the reason; it is not left out by a build tag
  unless the code under test is.
- A story that changes a command, a gate or a hook decision adds or extends a sandbox test in
  `test/e2e`; `CONTRIBUTING.md` says how to build a sandbox by hand.

### Code conventions

- A comment says why, and where there was one, names the failure that led to the rule. It does
  not restate the code. Match the density of the file you are in.
- Prose everywhere (comments, docs, messages, errors) is plain British English: behaviour,
  artefact, recognise.
- Text a user reads changes together with the docs that quote it; `docs/` reproduces command
  output, and `linkcheck` holds the links.
- Conventional Commits, checked by CI on the subject line. Commits are small and go to trunk.
- Nothing from `plan/`, and no path from anyone's machine, goes into a tracked file;
  `./task no-internal-docs` refuses it.

### Domain glossary (ubiquitous language)

`docs/the-loop.md` owns the full definitions. The words that get misused:

- gate — one of the nine numbered stages; a gate record key is not a gate (there are eleven keys,
  because Gates 4 and 6 each record the work and its review).
- freeze — the content hashes in `.sdlc/state/tests.lock`; frozen tests are what the implementer
  cannot touch. Only a person unfreezes.
- trunk — the branch named by `git.trunk_branch`; never "main" or "master" in code or messages.
- escalate — hand the story to a person and end the iteration; the story is then `awaiting_human`.
- blocking / advisory — whether a reviewer's `block` stops the gate; an advisory review is still
  required, never skipped.
- contract — this section; the configuration is its other half, not part of it.
- a person — whoever is at the keyboard. `approve`, `unfreeze` and `ack` are theirs alone; a
  model-driven session that can reach one is a security bug.

### Performance budgets (optional; sdlc:perf blocks only when one is breached)

- Package `init()` across the binary: the budget `./task init-time` enforces (`INIT_BUDGET_MS`
  in `Taskfile.yml`).

### Security and data rules (sdlc:security uses these)

- Data classes present: no personal data and no secrets. The only identifiers stored are the Claude
  Code session id in `.sdlc/state/session` and, in the claim on a story, the path of the checkout
  that holds it.
- `risk_tier: high` for anything that touches: `internal/hook`, `internal/shellpolicy`,
  `internal/policy`, `internal/pathrules`, the freeze (`internal/testset`, `sdlc freeze`,
  `unfreeze`), what separates a person from a model (`approve`, `ack`, the session check),
  story-id and path validation in `internal/store`, `install.sh`, `install.ps1`, `npm/`,
  `plugin/hooks`, and release or workflow files. `SECURITY.md` says why each is in scope.
- A shell spelling that gets past classification is the most valuable bug here: a change to
  `internal/shellpolicy` comes with the evasions it was tried against.

### Story source

- The backlog named by `backlog.path` in `.sdlc/config.json` (schema: `.sdlc/templates/story.schema.json`).
