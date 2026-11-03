# Review policy

A person changes this by hand, on trunk between stories. A running story keeps the copy it was cut
from, and no story lands a change to it.

## The review passes, in order

A pass here is one reading of the work with one question in mind, not a gate's outcome.

1. The criteria.
2. The design.
3. Security.
4. The tests.
5. The documents and messages a person reads.

## What counts as important

- A criterion not met, a defect, a security or data hole, a false statement in documentation or a
  message, a broken architecture rule.
- What a role already blocks on: gaming a test, a plan deviation, an ignored design finding, a
  breached performance budget.
- British English matters only where it makes text false or unreadable.
- Conventional Commits matter only where a subject is false or unreadable.
- The comment rules matter only where a comment is false or unreadable.

## What counts as a nit

Naming, wording and layout that is not wrong. A nit an approving reviewer lists passes the gate
unaddressed: this policy is the reason on the record that the contract's scout rule asks for, and
Gate 7's go to the retro.

## The nit cap

Report at most 5 per review, and give the rest as a count.

## What not to review

Generated files, and every leg of `./task check`: markdownlint, linkcheck, golangci-lint, gofmt,
shellcheck, actionlint, powershell, the hygiene mode of no-internal-docs, secrets, release-check,
npm-test. A leg's scan is not re-run by hand, but how a change reads, logs or returns a secret is
still reviewed, and a change that weakens a command or its configuration (`Taskfile.yml`,
`.golangci.yml`, `//nolint`) is not something that command enforces.
