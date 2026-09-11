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

## A note on the Claude Code version

`.claude-code-version` is the version this is developed and tested against, and
it is the one number: the README and the installation page are checked against
it, so a bump lands everywhere or it fails.

Claude Code updates itself, so your local copy will run ahead of it. That is
not a problem and nothing fails because of it — the file says what we build
against, not what you must run. Bump it on trunk when the newer version is the
one worth testing against, and the test will tell you which pages to bring
with it.

## How changes are made

- **Trunk-based.** Small changes straight to `main` where they are safe;
  short-lived branches where they are not. Long-running branches are not used.
- **Trunk is always green.** `./task check` passes before anything is pushed.
- **Conventional commits.** `feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
  `chore:`, with a body that says *why* rather than restating *what*.

## Cutting a release

The version lives in `plugin/.claude-plugin/plugin.json`, and every other file
that repeats it is checked against that one on every commit. `./task
release-check` is the leg that does it, so a half-finished bump fails
immediately rather than during a release.

1. Bump the version in `plugin/.claude-plugin/plugin.json` and `npm/package.json`.
2. Write the section for it in `CHANGELOG.md`. The release notes are that
   section, verbatim — they are written by a person and reviewed like anything
   else, not generated from commit subjects.
3. `./task check`, then `./task release-snapshot` to see exactly what the
   release will publish, in `dist/`.
4. Commit, then tag and push:

   ```console
   $ git tag v0.1.0 && git push origin main v0.1.0
   ```

The tag starts `.github/workflows/release.yml`, which runs the full check
matrix on the tagged commit before it builds anything, then publishes the
archives, their checksums, an SBOM and a build provenance attestation, and
finally `@bbsnly/sdlc` to npm. A pre-release tag (`v0.2.0-rc.1`) is marked as a
pre-release on GitHub and published to npm under `next` rather than `latest`.

Two things the workflow cannot do for itself:

- **`NPM_TOKEN`** must exist in the repository's Actions secrets, or the
  release stops at its first step rather than publishing half of itself.
- **Immutable releases** are enabled by hand in Settings → General. There is no
  API for it, and it is what stops a published tag being quietly replaced.

## What is not in this repository

Design records, planning documents and the specification of the legacy shell kit
this project replaces are kept outside version control on purpose. What is here
is the product and what a person using it needs to understand it. If you want to
know why something is shaped the way it is and the code does not tell you, open
an issue and ask — the answer belongs in the documentation, which is where it
will go.
