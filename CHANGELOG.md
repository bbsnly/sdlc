# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Pre-release. Nothing is installable yet; the repository is public from its first
commit so the work is visible as it happens.

### Added

- Repository scaffolding: MIT license, build tasks, CI on Linux, macOS and
  Windows, and the link and hygiene checks that gate every commit.
- Error codes `SDLC-E0001` through `SDLC-E0014`, each documented in
  [docs/troubleshooting.md](docs/troubleshooting.md). Codes are API: once
  published, a code's meaning does not change and it is never reused.

### Deferred

Recorded here so that "not in v1" is a decision with a place to live rather
than an omission:

- A rendered documentation site with search. Which route it takes is settled
  before the documentation is written.
- Harness support beyond Claude Code. The loop's design is deliberately
  tech-agnostic, but v1 ships one integration and does it properly.
