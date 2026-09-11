## SDLC Contract

<!-- Read by the /sdlc:next runbook and every loop agent. Machine values (commands, thresholds,
     paths) are in .sdlc/config.json and are authoritative; this section holds the rules a script
     cannot encode. Keep it under ~150 lines: every line here is loaded into every session. -->

### Phase constraints

<!-- What is in and out of scope right now. Example for a pure-domain phase: -->

- Current phase: TBD (e.g. "pure business logic — no storage, no network, no external dependencies").
- Allowed dependencies: TBD (e.g. "standard library only; test framework X").
- Out of scope until a later phase: TBD (e.g. "persistence adapters, HTTP handlers, observability").

### Architecture rules (the design reviewer blocks on these)

<!-- State them as checkable sentences, in the project's own terms. Examples: -->

- Dependency direction: TBD (e.g. "domain → nothing; application → domain; adapters → application, domain").
- Boundaries: TBD (e.g. "bounded contexts under internal/<context>/ never import each other's internals; cross-context calls go through published ports").
- Aggregates/invariants: TBD (e.g. "state changes only through aggregate methods; no setters").
- Errors: TBD (e.g. "domain errors are typed values; no panics across boundaries").
- Do-not-touch areas: TBD (paths that need a human before any change).

### Test conventions (the SDET follows these; the verifier checks them)

- Framework and layout: TBD (e.g. "table-driven tests next to the code, *_test.go; property tests with rapid").
- Naming: every acceptance criterion's id (`AC-n`) appears in the name of the test that
  covers it, or in a subtest name where the language will not take a hyphen.
- Determinism: no wall-clock sleeps, no network, fixed seeds.

### Code conventions

- TBD (formatter is authoritative for style; this is for things a formatter cannot do: naming, package layout, comments).

### Domain glossary (ubiquitous language)

<!-- One line per term. Reviewers flag names that do not match. Alternatively point to CODEMAP.md#glossary. -->

- TBD term — definition

### Performance budgets (optional; sdlc-perf blocks only when one is breached)

- none

### Security and data rules (sdlc-security uses these)

- Data classes present: TBD (e.g. "PII: none in this phase").
- Anything that touches: TBD list (auth, money, secrets…) is `risk_tier: high`.

### Gate overrides (optional; defaults live in .sdlc/config.json)

- Human pre-commit pause for tiers: high
- Review rounds max: 2 · Rework rounds max: 3 · Diff cap: 500 changed lines
- Trunk-only: every commit goes to `main`; no branches; `pr_mode: off`

### Story source

- `user_stories.json` (schema: `.sdlc/templates/story.schema.json`)
