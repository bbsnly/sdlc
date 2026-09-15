# sdlc documentation

`sdlc` runs a story-driven delivery loop in Claude Code: nine gates per story,
enforced by a hook. Start with the first section. Come back to the guides when
you have a specific job to do.

## Getting started

- [Installation](installation.md): install the binary and the plugin, and
  verify a download
- [Your first story](getting-started.md): from nothing to a story going
  through the loop
- [Upgrading](installation.md#updating): move to a newer release

## Guides

- [Adding sdlc to an existing project](guides/adding-to-an-existing-project.md):
  set up a repository that already has code, tests and a `CLAUDE.md`
- [Writing stories](guides/writing-stories.md): acceptance criteria that the
  first gate accepts and that can be turned into failing tests
- [Tuning commands for your stack](guides/tuning-commands-for-your-stack.md):
  make the build, test and lint commands match what your project runs
- [Stopping and resuming a story](guides/stopping-and-resuming-a-story.md):
  put a story down and pick it up again without losing its record
- [When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md): change
  a frozen acceptance test, with the reason on the record
- [When a reviewer blocks](guides/when-a-reviewer-blocks.md): act on a
  blocking review and get the gate to pass
- [Human approval gates](guides/human-approval-gates.md): the points where the
  loop stops and waits for you to decide
- [Tracking what a story costs](guides/tracking-what-a-story-costs.md): record
  spend, set a budget, and read the alerts
- [Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md):
  read the stories committed since you last looked, and follow them up
- [Consolidating what the retros say](guides/consolidating-what-the-retros-say.md):
  turn what repeats across stories into changes to your contract and settings
- [Why was I refused?](guides/why-was-i-refused.md): read a hook refusal, find
  its rule, and check when enforcement is off

## Concepts

- [The loop](the-loop.md): what each gate is for, and what it produces
- [Agents](agents.md): who does the work at each gate, and what none of them
  may do
- [Enforcement](enforcement.md#what-this-is-and-what-it-is-not): what the hook
  is, what it is not, and when it applies

## Reference

- [Commands](commands.md): every command and flag
- [Configuration](configuration.md): every setting in `.sdlc/config.json`
- [Enforcement rules](enforcement.md#rules-on-writing-files): every rule the
  hook and the command enforce, with the route around each
- [Troubleshooting](troubleshooting.md): every error code, and the warnings
  the hook prints
