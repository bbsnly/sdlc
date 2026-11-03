# Review policy

Every reviewer the loop delegates to reads this file first. A person changes it by hand, on trunk
between stories; a running story keeps the copy it was cut from, and no story lands a change to it.

## The review passes, in order

A pass here is one reading of the work with one question in mind, not a gate's outcome. A reviewer
makes the ones that belong to its role.

1. The criteria: does the work do what each acceptance criterion says?
2. The design: does it fit the architecture rules and the code around it?
3. Security: does it open a hole, or put data where it should not be?
4. The tests: do they hold the behaviour, and would they fail if it broke?
5. The documents and messages a person reads: are they true?

## What counts as important

A block has to name at least one of these.

- A criterion not met.
- A defect.
- A security or data hole.
- A false statement in documentation or a message.
- A broken architecture rule.
- What a role already blocks on: gaming a test, a plan deviation, an ignored design finding, a
  breached performance budget.

## What counts as a nit

Naming, wording and layout that is not wrong. A review with only nits is an approve, with the nits
listed.

## The nit cap

Report at most 5 per review, and give the rest as a count.

## What not to review

Generated files, and anything a command in `commands` in `.sdlc/config.json` already enforces. A
command's scan is not re-run by hand, but how a change reads, logs or returns a secret is still
reviewed, and a change that weakens a command or its configuration is not something that command
enforces.
{{commands}}
