# sdlc documentation

`sdlc` takes one user story at a time through nine gates in Claude Code, and a
hook holds the agent to each of them.

If you are new, start with Installation and Getting started. That is enough to
run your first story. Everything below them is here for when you need it,
ordered from what you will meet first to what you may never need.

## Getting started

- [Installation](installation.md): install the command and the plugin
- [Getting started](getting-started.md): your first story, from nothing to a
  story going through the loop, in about ten minutes
- [Claude Desktop](claude-desktop.md): run the loop from Claude Desktop, with the
  plugin from its plugin browser and slash commands in place of a terminal
- [Adding sdlc to an existing project](guides/adding-to-an-existing-project.md):
  set up a repository that already has code, tests and a `CLAUDE.md`
- [Upgrading](installation.md#updating): move to a newer release

## The basics

The jobs you will do in your first week.

- [Writing stories](guides/writing-stories.md): acceptance criteria that the
  first gate accepts and that can be turned into failing tests
- [Tuning commands for your stack](guides/tuning-commands-for-your-stack.md):
  make the build, test and lint commands match what your project runs
- [Human approval gates](guides/human-approval-gates.md): the points where the
  loop stops and waits for you to decide
- [Stopping and resuming a story](guides/stopping-and-resuming-a-story.md):
  put a story down and pick it up again without losing its record

## When the loop says no

What to do when a gate does not pass. Each of these starts from what you see
on your screen.

- [Why was I refused?](guides/why-was-i-refused.md): read a hook refusal, find
  its rule, and check when enforcement is off
- [When a reviewer blocks](guides/when-a-reviewer-blocks.md): act on a
  blocking review and get the gate to pass
- [When a frozen test is wrong](guides/when-a-frozen-test-is-wrong.md): change
  a frozen acceptance test, with the reason on the record
- [Troubleshooting](troubleshooting.md): every error code, and the warnings
  the hook prints

## Digging deeper

Once stories are landing, these help you run the loop well over time.

- [Tracking what a story costs](guides/tracking-what-a-story-costs.md): record
  spend, set a budget, and read the alerts
- [Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md):
  read the stories committed since you last looked, and follow them up
- [Consolidating what the retros say](guides/consolidating-what-the-retros-say.md):
  turn what repeats across stories into changes to your contract and settings

## How it works

Read these when you want to understand the loop, and not only use it.

- [The loop](the-loop.md): what each gate is for, and what it produces
- [Agents](agents.md): who does the work at each gate, and what none of them
  may do
- [Enforcement](enforcement.md#what-this-is-and-what-it-is-not): what the hook
  is, what it is not, and when it applies

## Reference

Complete lists, for looking things up.

- [Commands](commands.md): every command and flag
- [Configuration](configuration.md): every setting in `.sdlc/config.json`
- Enforcement rules, on [writing files](enforcement.md#rules-on-writing-files)
  and on [shell commands](enforcement.md#rules-on-shell-commands): every rule
  the hook and the command enforce, with what to do instead
- [Troubleshooting](troubleshooting.md): every error code, and the warnings
  the hook prints
