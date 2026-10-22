# Running the loop from Claude Desktop

Claude Desktop's Code tab runs Claude Code sessions, with their plugins and
hooks, so the loop works there as it does in a terminal. What is
different is that a person there may have no terminal. This page covers what
changes because of that: how the plugin gets its binary, the slash commands
that stand for the commands only a person runs, where to set the variables the
hook reads, and what still needs a terminal.

The slash commands need an `sdlc` released after 0.4.0; see
[Which release you need](#which-release-you-need).

## Install the plugin from the plugin browser

Open Claude Desktop's plugin browser, add the marketplace `bbsnly/sdlc`, and
install the `sdlc` plugin from it. The repository is its own marketplace, so
there is nothing else to add. If the plugin browser offers no way to add a
marketplace, the project can declare it in its `.claude/settings.json`, and
Claude Code offers to install the plugin when a session opens the project:

```json
{
  "extraKnownMarketplaces": {
    "sdlc": { "source": { "source": "github", "repo": "bbsnly/sdlc" } }
  },
  "enabledPlugins": { "sdlc@sdlc": true }
}
```

Nobody runs an installer this way, so the plugin fetches its own binary. The
first time a session starts, the plugin's launcher downloads the release that
matches the plugin's version into the plugin's `bin` directory, and checks it
against the checksums the plugin carries before it unpacks anything. Claude Code
puts that directory on the model's `PATH` too, so the model's Bash tool finds
the same `sdlc`. [Installation](installation.md#the-plugin-fetches-its-own-binary)
says more.

When the fetch fails, a session that starts in a project taking part in the
loop is told why and what to do, for example:

```text
sdlc: the sdlc binary was not found, so nothing is being enforced. why: could
not fetch sdlc 0.5.0 (sdlc_0.5.0_darwin_arm64.tar.gz) from the release. fix:
check that this machine can reach the release, and open a new session, which
tries again. Or, in Claude Desktop, open the local environment editor and set
SDLC_BIN to the full path of an sdlc you installed; in a terminal, install it
with npx @bbsnly/sdlc install. Then type /sdlc:doctor, or run sdlc doctor in a
terminal.
```

Each reason the fetch can stop for, and its fix, is listed under
[troubleshooting](troubleshooting.md#the-sdlc-binary-was-not-found-so-nothing-is-being-enforced).

## The commands that are yours

Some of the loop's commands are a person's decision, and the hook refuses them
from the model: approving, lifting the freeze, acknowledging the log, putting a
landed story's map change on trunk, moving a story, and choosing an agent's
model. In a terminal outside Claude Code you run them as `sdlc` commands. In
Claude Code you type them as slash commands:

| Type in Claude Code | Stands for |
| --- | --- |
| `/sdlc:approve` | `sdlc approve` |
| `/sdlc:unfreeze` | `sdlc unfreeze` |
| `/sdlc:ack` | `sdlc ack` |
| `/sdlc:remap` | `sdlc remap` |
| `/sdlc:release` | `sdlc release` |
| `/sdlc:assign` | `sdlc staffing assign` |
| `/sdlc:init` | `sdlc init` |
| `/sdlc:doctor` | `sdlc doctor` |

The arguments are the command's own, typed as on a command line:
`/sdlc:approve US-001 --reject "the migration has no way back"`. Quotes group
words, and nothing in them is run or expanded. The hook runs the command as
the terminal would, before anything reaches the model, and shows you what it
printed, refusals and their SDLC-E codes included. The model cannot start the
first six: Claude Code does not offer them to it, and the hook runs only a
command a person typed. `/sdlc:init` and `/sdlc:doctor` decide nothing, so the
model may run those too.

Inside Claude Code, the slash command is the way. A `!` line such as
`!sdlc approve US-001` runs with the environment variables Claude Code sets for
the model's Bash, so the command refuses itself with
[SDLC-E0079](troubleshooting.md#sdlc-e0079) and changes nothing; type
`/sdlc:approve US-001` instead. A slash command typed in a session that was
started from another session's tool call, such as a `claude` a model's Bash
started, does not run either: the hook finds `AI_AGENT` in its environment and
says that the command did not run.

After the loop handed `US-001` to a person with a question, typing
`/sdlc:approve US-001` shows:

```text
US-001  approved (spec_unclear)

A person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.
```

A command acts on the directory the session is in, as the same command typed
in a terminal there would. A story under way in its own worktree is answered
there, so open a session in `.sdlc/worktrees/<ID>` for it; typed from the main
checkout, `/sdlc:approve` is refused with
[SDLC-E0050](troubleshooting.md#sdlc-e0050), which names the checkout that
holds the story.

The hook gives a slash command 60 seconds. `/sdlc:remap` waits for another
story's landing to finish before it changes anything, and a landing can take
longer than that. Then nothing is shown for a minute, and the model tells you
the command did not run, which is true: the wait comes before any change. Type
it again once the landing has finished.

## Which release you need

The slash commands are run by an `sdlc` released after 0.4.0. An `sdlc` 0.4.0
does not know them: typing one with it runs nothing, and the model, reading
the command's text, tells you it did not run and why that usually is.
To see which release you have, ask the assistant to run `sdlc version`, or run
it in a terminal, and compare it with the plugin's version in the plugin
browser. `/sdlc:doctor` says whether an `sdlc` is found at all; it does not
print a version.

## Variables, in the local environment editor

A desktop app does not read your shell's profile, so the variables the hook
reads are set in Claude Desktop's local environment editor, and a session
started after the change sees them.

`SDLC_ENFORCE` set to `0` in the local environment editor turns the hook's
enforcement off for the sessions Desktop starts: every refusal of the hook's
stands down, and so do the `Stop` hook and the formatter. The slash commands
above still run, as they would in a terminal with enforcement off. A person's
command run in the model's Bash, or from a `!` line, still refuses itself
([SDLC-E0079](troubleshooting.md#sdlc-e0079), and SDLC-E0061 or SDLC-E0074 for
`sdlc parallel` and `sdlc staffing assign`). It must be exactly `0`.

`SDLC_DEBUG_FILE` set in the local environment editor to the full path of a file
makes the hook, and the plugin's launcher, append a line to it about each call.
That is where to look when a session says nothing and you expected a refusal,
or a slash command did not run.

`SDLC_BIN` set in the local environment editor to the full path of an `sdlc` you
installed yourself makes the hook run that one, and turns the plugin's own fetch
off.

## What still needs a terminal

`sdlc parallel`, which works more than one story at a time in their own
worktrees, has no slash command. Run it in a terminal outside Claude Code: a
terminal window you opened yourself, such as Terminal on macOS or PowerShell
from the Start menu. A terminal that Claude Code or Claude Desktop opens is not
one, and neither is a `tmux` session Claude Code started: where one has Claude
Code's environment variables, the command refuses itself there with
[SDLC-E0079](troubleshooting.md#sdlc-e0079).

A terminal of your own has no `sdlc` unless you installed one, with
`npx @bbsnly/sdlc install`: the plugin's copy is on Claude Code's `PATH` only.
Install it before you need `sdlc parallel`. The slash commands need nothing
installed.

## Why Cowork is not supported

Cowork refuses a plugin with a `bin/` directory, and that directory is where
this plugin's launcher lives, so the plugin cannot be installed there. Cowork
also turns off the shell lines skills use, and it cannot reach a git repository
on your machine, which every gate works in.

## What is not protected

- A session in a project that does not take part in the loop is held to
  nothing, so it can still write Claude Code's own configuration under
  `~/.claude`, this plugin's launcher, binary and checksums included. Every
  project on the machine then runs what it wrote. A session in a project that
  takes part is refused that write, and refused starting a second Claude Code
  session that could type a person's slash command.
- The model's Bash tool finds an `sdlc` an installer put on `PATH` ahead of the
  plugin's own, so it may run another release than the one the hook runs. The
  launcher's line in `SDLC_DEBUG_FILE` is the only record of which `sdlc` the
  hook ran.
- The PowerShell tool finds the plugin's `bin` directory only from Claude Code
  2.1.303, as its changelog says; before that, the model's PowerShell finds only
  an `sdlc` on `PATH`.
- A person's command refuses itself only where Claude Code's environment
  variables reach it. A program that starts it with a clean environment, such
  as `launchctl submit` or `ssh`, or one run after those variables were
  removed, is not caught. Nor is a program Claude Code starts from the
  project's own configuration, such as an MCP server or a hook, that runs the
  hook on a payload it wrote, nor a slash command another program types into
  your prompt, as `tmux send-keys` can. Each is listed, with how you would notice it, in
  [Commands the rules do not read](enforcement.md#commands-the-rules-do-not-read).

[Enforcement](enforcement.md#claude-config-is-the-persons) has the rules that
hold a session in a project that takes part, and what they do not catch.
