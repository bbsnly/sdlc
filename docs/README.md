# sdlc documentation

`sdlc` takes one user story at a time through nine gates in Claude Code, and a
hook holds your agent to each of them.

New here? [Getting started](getting-started.md) takes you from nothing to your
first story in about ten minutes. Every other page is here for when you need
it, in roughly the order you will.

## Getting started

- [Installation](installation.md): install the command and the plugin
- [Getting started](getting-started.md): set up a project and run your first
  story
- [Running the loop from Claude Desktop](claude-desktop.md): do it all with no
  terminal
- [Adding sdlc to an existing project](guides/adding-to-an-existing-project.md):
  bring the loop to a repository with code and conventions of its own
- [Updating](installation.md#updating): move the command and the plugin to a
  newer release

## The basics

- [Writing stories](guides/writing-stories.md): acceptance criteria the loop
  can turn into tests
- [Tuning commands for your stack](guides/tuning-commands-for-your-stack.md):
  match the build, test and lint commands to your project
- [Human approval gates](guides/human-approval-gates.md): answer the loop when
  it stops for you
- [Stopping and resuming a story](guides/stopping-and-resuming-a-story.md):
  put a story down and pick it up again

## When the loop says no

- [Why was I refused?](guides/why-was-i-refused.md): read a refusal from the
  hook, and find the way through
- [When a reviewer blocks](guides/when-a-reviewer-blocks.md): act on a blocking
  review
- [When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md): change
  a frozen test, with the reason on the record
- [Troubleshooting](troubleshooting.md): look up an error code

## Digging deeper

- [Tracking what a story costs](guides/tracking-what-a-story-costs.md): record
  spend, set a budget and read the alerts
- [Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md):
  read the stories that landed since you last looked
- [Consolidating what the retros say](guides/consolidating-what-the-retros-say.md):
  turn what repeats across stories into changes to your contract

## How it works

- [The loop](the-loop.md): what each gate is for, and what it produces
- [Agents](the-agents.md): who does the work at each gate
- [Enforcement](enforcement.md): what the hook holds your agent to, rule by
  rule, and what it does not

## Reference

- [Commands](commands.md): every command and flag
- [Configuration](configuration.md): every setting in `.sdlc/config.json`
- [Troubleshooting](troubleshooting.md): every error code, with its cause and
  its fix

## Contributing

- [Writing the documentation](style.md): the style guide these pages follow
