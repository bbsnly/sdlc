# Installation

Two things get installed: the `sdlc` binary, which owns the loop's state and
enforces its rules, and the Claude Code plugin, which is what your session
talks to. Both are needed, and the plugin will tell you if the binary is
missing rather than quietly enforcing nothing.

## 1. The binary

### macOS and Linux

```console
$ curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh
```

It downloads the release built for your machine, checks it against the
release's `checksums.txt`, and installs a single static binary into
`~/.local/bin`. It touches nothing else and asks for no privileges.

```console
$ curl -fsSL .../install.sh | sh -s -- --version 0.1.0 --dir ~/.local/bin
```

`--dir` can point anywhere you can write. Somewhere like `/usr/local/bin` needs
privileges the script does not ask for, so run it under `sudo` yourself if that
is where you want it — the script will otherwise tell you it could not write
there.

`SDLC_VERSION` and `SDLC_INSTALL_DIR` do the same thing, which is easier to
read in a provisioning script.

### Windows

```powershell
irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex
```

Installs into `%LOCALAPPDATA%\Programs\sdlc\bin` and adds that directory to
your user `PATH`. Open a new terminal afterwards for the `PATH` change to
apply. Set `$env:SDLC_NO_PATH = '1'` first if you would rather manage `PATH`
yourself, which is what you want inside a container image.

### With npm

```console
$ npx @bbsnly/sdlc install
```

Same download, same verification, same destination. What ends up on your
`PATH` is the native binary, not a Node wrapper around it: sdlc runs as a hook
on every matching tool call, and a Node process start costs more than
everything else in the loop put together.

There is no postinstall script, so `npm install --ignore-scripts` changes
nothing here — the download happens when you ask for it.

### With Go

```console
$ go install github.com/bbsnly/sdlc/cmd/sdlc@latest
```

Requires Go 1.26 or newer, and gives you a binary built from source on your
own machine. `sdlc version` reports the module version rather than a release
tag, which is the honest answer for a build nobody published.

### From source

```console
$ git clone https://github.com/bbsnly/sdlc && cd sdlc
$ ./task build
$ export PATH="$PWD/dist:$PATH"
```

`./task check` runs everything CI runs, if you want to know the clone is good
before you trust it.

## 2. The plugin

In Claude Code:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

The repository is its own marketplace, so there is nothing else to add. That
gives you the `/sdlc:next` skill, eleven agents, and the `PreToolUse` hook that
does the enforcing.

Worth knowing: the marketplace serves the repository's default branch, not a
tag. `/plugin update sdlc@sdlc` therefore gives you the plugin as it is on
`main`, which can be ahead of the binary release you have installed — the
version in the plugin's manifest names the release it was cut for, not the
commit you received. The two halves are kept compatible on purpose, and the
binary is the one that enforces; if they ever disagree, `sdlc doctor` is what
tells you.

### The skill on its own, with `npx skills add`

The runbook is also an [Agent Skill](https://agentskills.io), so the `skills`
CLI can install it into any agent that reads them:

```console
$ npx skills add bbsnly/sdlc
```

That puts `SKILL.md` in `.claude/skills/next/` (and the equivalent directory for
every other agent it knows), records it in `skills-lock.json`, and gives you
`/next` in a session.

Know what that route brings and what it does not. It brings the runbook: the
order of the gates, what each one produces, what to do when one fails. It does
**not** bring the binary, the eleven agents, or the hook — so nothing is
recorded, nothing is frozen, and nothing is refused. The skill checks for the
tool and for the agents before it does anything and stops if either is missing,
because a loop with no enforcement behind it is a checklist, and a checklist you
mark off about your own work is worth nothing.

Use it to read the loop, to run it against a different agent, or to pin the
runbook in a repository that installs the rest some other way. To actually use
the loop in Claude Code, install the plugin above.

## Check it worked

From inside a project:

```console
$ sdlc doctor
```

`doctor` checks git, the configuration, the backlog, the contract section, the
commands your configuration names, and — the one that catches a half install —
whether the hook can find the binary at all. Every problem it reports comes
with the command that fixes it.

## Verifying a download yourself

Every release publishes `checksums.txt`, and every archive is attested, along
with `checksums.txt` itself and the two install scripts: GitHub records which
workflow, in which repository, at which commit, produced them. Attesting the
checksum file is what makes it worth checking against — a list anyone could
replace would prove nothing about the archives it lists.

The `Source code (zip)` and `Source code (tar.gz)` entries GitHub adds to every
release are not ours and are neither attested nor listed in `checksums.txt`.

```console
$ gh attestation verify sdlc_0.1.0_darwin_arm64.tar.gz --repo bbsnly/sdlc
```

The same works on an install script, which is worth doing if you would rather
read one before running it:

```console
$ curl -fsSLO https://raw.githubusercontent.com/bbsnly/sdlc/v0.1.0/install.sh
$ gh attestation verify install.sh --repo bbsnly/sdlc
```

Fetch it at the tag, not at `main`: the tag is the content that was attested,
and `main` moves.

Each archive also ships an SBOM (`.sbom.json`) listing everything inside it.

The install scripts verify the checksum for you and refuse to install anything
that does not match. The npm package goes one step further: it carries the
checksums that were written when the release was built, so verification does
not depend on fetching a checksum from the same place as the download.

## Updating

Run the same installer again. Every route writes the new binary under a name
`PATH` cannot resolve and then renames it, so the only visible change is
atomic: a half-written binary on your `PATH` would be worse than an old one.

On Windows, close anything running `sdlc.exe` first — a running executable
cannot be replaced, and the installer will say so rather than leave a damaged
one behind.

To update the plugin, `/plugin update sdlc@sdlc` in Claude Code.

## Uninstalling

```console
$ rm ~/.local/bin/sdlc
```

On Windows, delete `%LOCALAPPDATA%\Programs\sdlc\bin` and remove it from your
`PATH`. In Claude Code, `/plugin uninstall sdlc@sdlc`.

A project's own `.sdlc/` directory is yours; nothing removes it for you.

## What it needs

| | |
| --- | --- |
| Claude Code | 2.1.265 or newer |
| git | in the project you run the loop on |
| an OS | macOS, Linux, or Windows 10 or newer |

Nothing else. The binary is static and has no runtime dependencies; Go and
Node are needed only if you install through them.

## Where to go next

[Getting started](getting-started.md) takes a project from `sdlc init` to a
story going through the loop.
