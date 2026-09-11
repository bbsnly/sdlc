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
$ curl -fsSL .../install.sh | sh -s -- --version 0.1.0 --dir /usr/local/bin
```

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

Every release publishes `checksums.txt`, and every archive is attested: GitHub
records which workflow, in which repository, at which commit, produced it.

```console
$ gh attestation verify sdlc_0.1.0_darwin_arm64.tar.gz --repo bbsnly/sdlc
```

Each archive also ships an SBOM (`.sbom.json`) listing everything inside it.

The install scripts verify the checksum for you and refuse to install anything
that does not match. The npm package goes one step further: it carries the
checksums that were written when the release was built, so verification does
not depend on fetching a checksum from the same place as the download.

## Updating

Run the same installer again. It overwrites the binary in place, atomically —
a half-written binary on your `PATH` would be worse than an old one.

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
| Claude Code | any recent version |
| git | in the project you run the loop on |
| an OS | macOS, Linux, or Windows 10 or newer |

Nothing else. The binary is static and has no runtime dependencies; Go and
Node are needed only if you install through them.

## Where to go next

[Getting started](getting-started.md) takes a project from `sdlc init` to a
story going through the loop.
