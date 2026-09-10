# Contributing

Thanks for looking. This is a single-maintainer project in its pre-release
phase, so the most useful contributions right now are issues: a spelling of a
shell command that the policy classifies wrongly, a platform where something
does not work, or a piece of documentation that did not explain itself.

## Running the checks

A fresh clone needs **Go** and nothing else:

```console
$ ./task check
```

On Windows, `task.cmd check`.

`./task` is a bootstrap wrapper in the manner of `gradlew`: it runs the pinned
version of [Task](https://taskfile.dev) through `go run`. Every other tool this
repository uses is pinned in `Taskfile.yml` and run the same way, resolved
against its own dependency graph. The product's `go.mod` stays free of build
tooling, which is why `go install` of this module pulls nothing you did not ask
for.

Two things Go cannot fetch for you, because they are not Go programs:

- **shellcheck**, at the exact version in `Taskfile.yml`. Different versions
  disagree about what is a warning, so the Taskfile refuses to run rather than
  silently checking something else. `brew install shellcheck`, or see
  [installing shellcheck](https://github.com/koalaman/shellcheck#installing).
- **Node** 22 or newer, because markdownlint runs through `npx`.

`./task check` never skips a leg. A check that skips when its tool is missing
reports success for work it did not do, so every leg either runs or fails with
an install line.

## A note on the Claude Code version pin

`.claude-code-version` pins the version that CI installs and that some tests
assert against. Claude Code updates itself, so **your local copy will drift past
that pin, and that is not your fault.** When it does, the check fails and names
the flag that lets you continue:

```console
$ SDLC_ALLOW_CLI_DRIFT=1 ./task check
```

CI keeps the hard failure, because CI installs the pinned version and a mismatch
there is a real defect. Locally it is noise, and bumping the pin on trunk is the
honest fix when the newer version is the one we should be testing against.

## How changes are made

- **Trunk-based.** Small changes straight to `main` where they are safe;
  short-lived branches where they are not. Long-running branches are not used.
- **Trunk is always green.** `./task check` passes before anything is pushed.
- **Conventional commits.** `feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
  `chore:`, with a body that says *why* rather than restating *what*.

## What is not in this repository

Design records, planning documents and the specification of the legacy shell kit
this project replaces are kept outside version control on purpose. What is here
is the product and what a person using it needs to understand it. If you want to
know why something is shaped the way it is and the code does not tell you, open
an issue and ask — the answer belongs in the documentation, which is where it
will go.
