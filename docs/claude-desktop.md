# Running the loop from Claude Desktop

- [Introduction](#introduction)
- [Install the plugin from the plugin browser](#install-the-plugin-from-the-plugin-browser)
  - [When the plugin browser cannot add a marketplace](#when-the-plugin-browser-cannot-add-a-marketplace)
- [Set up a project and run a story](#set-up-a-project-and-run-a-story)
- [Answering the loop](#answering-the-loop)
  - [Answer from the story's worktree](#answer-from-the-storys-worktree)
  - [The commands that are yours](#the-commands-that-are-yours)
- [Set variables in the local environment editor](#set-variables-in-the-local-environment-editor)
- [Using a terminal of your own](#using-a-terminal-of-your-own)
- [Why Cowork is not supported](#why-cowork-is-not-supported)
- [What is not protected](#what-is-not-protected)
- [Next steps](#next-steps)

## Introduction

The Code tab of Claude Desktop runs Claude Code sessions, with their plugins
and their hooks, so the loop works there as it does in a terminal. What
differs is that you may have no terminal at all.

This page covers what changes because of that: how to add the plugin, how to
set up a project and answer the loop without leaving the app, and where to set
the few variables `sdlc` reads. Everything else is as
[Getting started](getting-started.md) describes it, and this page says where
to pick that page up.

## Install the plugin from the plugin browser

In the Code tab, typing `/plugin` does not add a plugin. Open Claude Desktop's
plugin browser instead, add the marketplace `bbsnly/sdlc`, and install the
`sdlc` plugin from it. The repository is its own marketplace, so there is
nothing else to add.

You don't need to install the `sdlc` command. The first time a session starts,
the plugin downloads the release of `sdlc` that matches its own version, checks
it against checksums it carries, and keeps it for the hook and for your agent
to run. [Installation](installation.md#the-plugin-fetches-its-own-binary) says
more about it.

If the download fails, a session that starts in a project set up for the loop
tells you why, and what to do about it.
[Troubleshooting](troubleshooting.md#the-sdlc-binary-was-not-found-so-nothing-is-being-enforced)
lists each reason and its fix.

### When the plugin browser cannot add a marketplace

If the plugin browser offers no way to add a marketplace, your project can
declare it. Put this in the project's `.claude/settings.json`, and Claude Code
offers to install the plugin when a session opens the project:

```json
{
  "extraKnownMarketplaces": {
    "sdlc": { "source": { "source": "github", "repo": "bbsnly/sdlc" } }
  },
  "enabledPlugins": { "sdlc@sdlc": true }
}
```

## Set up a project and run a story

With the plugin installed, open a session in the project you want to work on
and type:

```text
/sdlc:init
```

Your agent runs `sdlc init` and shows you what it printed.
[Set up your project](getting-started.md#set-up-your-project) explains what
`init` wrote, and takes you through the steps after it: filling in the
contract, committing the setup and writing a story. Where that page runs
`sdlc doctor`, type `/sdlc:doctor`. Ask your agent to run the page's other
commands, such as `git commit` and `sdlc story list`.

Then start the loop, as you would anywhere:

```text
/sdlc:next
```

## Answering the loop

Some of the loop's commands are decisions, and decisions are yours: approving
work, lifting the test freeze, and a few more. The hook refuses them from the
model.

You type such a command as a slash command, such as `/sdlc:approve`, in Claude
Desktop as in Claude Code. The hook runs it before anything reaches the model,
as a terminal would, and shows you what it printed:

```text
/sdlc:approve US-001
```

Let's say the loop has handed the story `US-001` to you with a question. That
line approves the work, and shows:

```text
US-001  approved (spec_unclear)

A person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.
```

Type `/sdlc:next`, and the story carries on from the gate it stopped at. To
send the work back instead, give the reason:
`/sdlc:approve US-001 --reject "the migration has no way back"`.

### When a slash command does not run

The hook runs a command you typed only where it can tell that Claude Code
started it for a session you are at. It reads that from four variables in its
own environment, and that reading is known to work in Claude Code 2.1.321 in
Claude Desktop. A later release that keeps those variables is expected to work.
An earlier one was not tried, and may give its hooks another label: the command
then does not run, and the hook says to update.

Where the hook cannot tell, the command does not run, and the hook says which
variable it could not read and what to do about it.
[Troubleshooting](troubleshooting.md#a-slash-command-did-not-run) lists each.
In Claude Desktop, type the command on a line that starts with `!` instead,
which runs it directly and not through the model, and needs none of those
variables:

```text
!sdlc approve US-001
```

In Claude Code run from a terminal, it is the other way round: the slash command
is the way, and a `!` line [refuses itself](troubleshooting.md#sdlc-e0079) and
changes nothing. A slash command typed in a session that another session's tool
call started does not run either, and the hook says which variable told it so.

### Answer from the story's worktree

A command acts on the directory the session is in, and a story under way is
answered from its own worktree, `.sdlc/worktrees/<ID>`. You may open a session
in that directory, or, for a `!` line, change to it on the same line:

```text
!cd .sdlc/worktrees/US-001 && sdlc approve US-001
```

Typed from your main checkout, the approval
[is refused](troubleshooting.md#sdlc-e0050), and the refusal names the checkout
that holds the story.

### The commands that are yours

There are six of them. Each has a slash command, and in Claude Desktop each has
a `!` line for when the slash command does not run:

| To | Slash command | `!` line in Claude Desktop |
| --- | --- | --- |
| approve work, or send it back | `/sdlc:approve` | `!sdlc approve` |
| lift the test freeze | `/sdlc:unfreeze` | `!sdlc unfreeze` |
| mark the log of landed stories as read | `/sdlc:ack` | `!sdlc ack` |
| put a landed story's change to `CODEMAP.md` on trunk, or drop it | `/sdlc:remap` | `!sdlc remap` |
| let a story go from the checkout that holds it | `/sdlc:release` | `!sdlc release` |
| choose an agent's model | `/sdlc:assign` | `!sdlc staffing assign` |

The arguments are the command's own, typed as on a command line, and
[Commands](commands.md) describes them. Quotes group words, and nothing inside
them is run or expanded.

The model cannot start any of the six: Claude Code does not offer them to it,
and the hook runs only a command a person typed. `/sdlc:init` and
`/sdlc:doctor` decide nothing, so the model may run those too.

> [!NOTE]
> The slash commands need `sdlc` 0.5.0 or newer, which the plugin's own
> download always is. If a command you typed answers that it did not run,
> `/sdlc:doctor` says whether an `sdlc` is found at all, and asking your agent
> to run `sdlc version` shows which release it is.

## Set variables in the local environment editor

A desktop app does not read your shell's profile. The variables `sdlc` reads
are set in Claude Desktop's local environment editor instead, and a session
started after the change sees them. These are the three you may want:

| Set in the local environment editor | To | What it does |
| --- | --- | --- |
| `SDLC_BIN` | the full path of an `sdlc` you installed yourself | The hook runs that one, and the plugin's own download is turned off |
| `SDLC_DEBUG_FILE` | the full path of a file | The hook appends a line to it about each call. Look there when a session said nothing and you expected a refusal, or when a command you typed did not run |
| `SDLC_ENFORCE` | `0`, and exactly that | Turns the hook's enforcement off for the sessions Claude Desktop starts |

With `SDLC_ENFORCE` set to `0`, every refusal of the hook's stands down. Your
slash commands still run, and your six commands stay yours all the same: run by
the model, each still [refuses itself](troubleshooting.md#sdlc-e0079).
[Turning it off](enforcement.md#turning-it-off) says more.

Leave `AI_AGENT`, `CLAUDE_CODE_EXECPATH` and `GIT_EDITOR=true` out of the
editor. The hook reads all three to tell a command you typed from a model's,
and with any of them set there, a slash command answers that it did not run and
names the variable.

## Using a terminal of your own

You never need a terminal, but you may prefer one. A terminal of your own is a
window you opened yourself, such as Terminal on macOS or PowerShell from the
Start menu. A terminal that Claude Code or Claude Desktop opens is not one,
and nor is a `tmux` session that Claude Code started: your six commands
[refuse themselves](troubleshooting.md#sdlc-e0079) there, as they do for the
model.

Your own terminal has no `sdlc` until you install one, because the plugin's
copy is for Claude Code alone:

```console
$ npx @bbsnly/sdlc install
```

After that, `sdlc approve` and the rest work there as
[Commands](commands.md) describes. [Installation](installation.md) has the
other ways to install.

## Why Cowork is not supported

Cowork cannot run the loop. It refuses a plugin that has a `bin/` directory,
and this plugin keeps the program that finds `sdlc` in one. Cowork also turns
off the shell lines that skills use, and it cannot reach a git repository on
your machine, which every gate works in.

## What is not protected

`sdlc` is a discipline tool, not a sandbox. Three gaps are worth knowing when
you run it from a desktop app:

- **A project that is not set up for the loop is held to nothing.** A session
  there can write Claude Code's own configuration under `~/.claude`, this
  plugin included, and every project on the machine then runs what it wrote. A
  session in a project that is set up is refused that write.
- **Your agent's shell may find a different `sdlc` from the hook's.** Where an
  installer put one on `PATH`, your agent runs that one, which may be another
  release. `SDLC_DEBUG_FILE` records which `sdlc` the hook ran, and
  [updating the command and the plugin together](installation.md#updating)
  keeps the two alike.
- **Your commands refuse themselves only where Claude Code's environment
  reaches them.** A program that starts one with a clean environment is not
  caught. Nor is a program that types a slash command into your prompt. A
  session a model starts in a terminal of its own, with `osascript` or `socat`
  for instance, may look to the hook like one you are at: the rules refuse the
  starts they read, and Enforcement lists the ones they do not.

[Enforcement](enforcement.md#commands-the-rules-do-not-read) lists each of
these, with how you would notice it.

## Next steps

With the plugin in place and a project set up, the rest of the manual applies
as it is written:

- [Write a story](getting-started.md#write-a-story), and run it
- [Human approval gates](guides/human-approval-gates.md): every place the loop
  can stop for you, and how to answer each
- [Why was I refused?](guides/why-was-i-refused.md): how to read a refusal from
  the hook
