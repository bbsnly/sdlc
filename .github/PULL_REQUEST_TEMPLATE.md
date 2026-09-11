## What this changes, and why

<!-- Why, rather than a restatement of the diff. What was wrong before? -->

## Checks

- [ ] `./task check` passes locally
- [ ] Anything a user can see is documented in `docs/`, and any new error code
      has a heading in `docs/troubleshooting.md`
- [ ] New behaviour has a test that fails without the change

<!--
The drift tests will tell you if the documentation is missing: every command,
flag, gate, rule, agent and configuration key is checked against the pages that
describe them. If one of those fails, it is not being pedantic — the page is
genuinely out of date.
-->
