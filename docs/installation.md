# Installation

- [Introduction](#introduction)
  - [What it needs](#what-it-needs)
- [Install sdlc](#install-sdlc)
  - [Install the command](#install-the-command)
  - [Add the plugin](#add-the-plugin)
  - [Check it worked](#check-it-worked)
- [Other ways to install the command](#other-ways-to-install-the-command)
  - [macOS and Linux](#macos-and-linux)
  - [Windows](#windows)
  - [Choosing a version or a directory](#choosing-a-version-or-a-directory)
  - [With Go](#with-go)
  - [From source](#from-source)
  - [By hand, from a release](#by-hand-from-a-release)
- [Verifying a download yourself](#verifying-a-download-yourself)
- [How the plugin finds sdlc](#how-the-plugin-finds-sdlc)
  - [The plugin fetches its own binary](#the-plugin-fetches-its-own-binary)
  - [The plugin without the binary](#the-plugin-without-the-binary)
  - [The skills on their own](#the-skills-on-their-own)
- [Updating](#updating)
- [Uninstalling](#uninstalling)
- [Next steps](#next-steps)

## Introduction

`sdlc` comes in two parts. The `sdlc` command keeps the loop's records and
enforces its rules. The Claude Code plugin brings the `/sdlc:next` skill, the
agents that do the work, and the hooks that call the command. The loop needs
both.

Installing them takes about two minutes: one line in a terminal, and two in
Claude Code. This page takes you through that first. After it come the other
ways to install the command, how to verify a download yourself, and how to
update and uninstall.

If you would rather hand the job to your agent,
[Getting started](getting-started.md#letting-your-agent-do-the-setup) has a
prompt that does it.

### What it needs

| You need | Which |
| --- | --- |
| Claude Code | 2.1.309 or newer, in a terminal or in the Code tab of Claude Desktop |
| git | 2.17 or newer, in the project you run the loop on. On Windows this is [Git for Windows](https://git-scm.com/download/win); WSL is not required |
| An operating system | macOS on Apple silicon, Linux, or Windows 10 or newer |
| Node | 18 or newer, and only for the one-line `npx` install |

That is all. `sdlc` is a single file with nothing else to install beside it.

> [!NOTE]
> There is no prebuilt `sdlc` for an Intel Mac. The installers say so and
> stop, and the way in is to [build it with Go](#with-go).

## Install sdlc

There are two things to install, the command and the plugin, and one check to
make at the end.

### Install the command

First, install the `sdlc` command. One line does it on macOS, Linux and
Windows:

```console
$ npx @bbsnly/sdlc install
```

The installer downloads the native `sdlc` binary built for your machine, checks
it against the checksums the package carries, and puts it in `~/.local/bin`, or
in `%LOCALAPPDATA%\Programs\sdlc\bin` on Windows. Nothing of Node stays behind:
what you run afterwards is `sdlc` itself.

The installer does not change your `PATH`. If its directory is not on it, the
installer prints the lines to add. Add them, then open a new terminal.

On Windows, the lines are these. Run them in PowerShell, with your install
directory in the second (`%LOCALAPPDATA%\Programs\sdlc\bin` unless you chose
another):

```powershell
$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$key.SetValue('Path', "$env:LOCALAPPDATA\Programs\sdlc\bin;" + $key.GetValue('Path', '', 'DoNotExpandEnvironmentNames'), 'ExpandString')
[Environment]::SetEnvironmentVariable('SDLC_PATH_REFRESH', $null, 'User')
```

The plugin's hook finds the binary in the default directory without them. They
are for a terminal, so that `sdlc` runs by name there.

If you have no Node, the scripts for [macOS and Linux](#macos-and-linux) and
for [Windows](#windows) do the same job.

### Add the plugin

Next, add the plugin. Only a person can do this, so type these two commands
into Claude Code yourself:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

The repository is its own marketplace, so there is nothing else to add. Here
is what the plugin brings:

- `/sdlc:next`, the skill that works a story through the loop, and two more
  that you start yourself: `/sdlc:trunk-review` looks back at what landed, and
  `/sdlc:consolidate` turns what the retros say into changes to your contract
  and configuration
- thirteen agents, each with one job in the loop
- the hooks that hold those agents to the gates
- a slash command for each `sdlc` command that only you may run, such as
  `/sdlc:approve`, along with `/sdlc:init` and `/sdlc:doctor`

Adding the plugin changes nothing for the work you already do in Claude Code.
Its skills start only when you type them, and in a project you have not set up
for the loop, the hook holds a session to nothing.
[Enforcement](enforcement.md#when-any-of-it-applies) says exactly when it
applies.

> [!NOTE]
> In the Code tab of Claude Desktop, typing `/plugin` does not add the plugin.
> You add it from Claude Desktop's plugin browser, and you may skip installing
> the command.
> [Running the loop from Claude Desktop](claude-desktop.md#install-the-plugin-from-the-plugin-browser)
> shows how.

### Check it worked

Now make sure everything is in place. Open a new terminal and ask `sdlc` for
its version:

```console
$ sdlc version
```

If that prints a version, your terminal can find the command. If it cannot,
look back at what the installer printed about your `PATH`.

That is the installation done. The next step is to set up a project with
`sdlc init`, and [Getting started](getting-started.md#set-up-your-project)
takes you through it, all the way to your first story. Once a project is set
up, `sdlc doctor` checks the rest from inside it, and prints the fix beneath
anything it does not like.

## Other ways to install the command

`npx` is the route most people take. If you have no Node, or you want to
choose where the command goes, pin a version, or build it yourself, use one of
the routes below. Every one of them gives you the same `sdlc`.

### macOS and Linux

On macOS and Linux, a shell script does what the npm installer does:

```console
$ curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh
```

It downloads the latest release built for your machine, checks it against the
release's `checksums.txt`, and installs it into `~/.local/bin`. It touches
nothing else and asks for no privileges. If that directory is not on your
`PATH`, the script prints the line to add.

### Windows

On Windows, run this in PowerShell:

```powershell
irm https://raw.githubusercontent.com/bbsnly/sdlc/main/install.ps1 | iex
```

The script installs into `%LOCALAPPDATA%\Programs\sdlc\bin` and adds that
directory to your user `PATH`. Open a new terminal afterwards so that the
change applies.

If you would rather manage `PATH` yourself, as you would inside a container
image, set `$env:SDLC_NO_PATH = '1'` before you run it.

### Choosing a version or a directory

Each installer picks a release and a directory for you. To choose your own,
pass `--version` and `--dir` to the npm installer:

```console
$ npx @bbsnly/sdlc install --version 0.6.1 --dir ~/bin
```

The shell script takes the same two options, after `sh -s --`:

```console
$ curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh -s -- --version 0.6.1 --dir ~/bin
```

All three installers also read `SDLC_VERSION` and `SDLC_INSTALL_DIR` from the
environment, which is easier to read in a provisioning script, and is how you
choose with the Windows script.

The directory may be anywhere you can write. Somewhere like `/usr/local/bin`
needs privileges the script does not ask for, so run it under `sudo` yourself
if that is where you want `sdlc`.

### With Go

If you have Go 1.26 or newer, you may build `sdlc` from source on your own
machine:

```console
$ go install github.com/bbsnly/sdlc/cmd/sdlc@latest
```

Go puts the command in its own `bin` directory, so make sure that directory is
on your `PATH`. A build like this was never published as a release, so
`sdlc version` reports the module's version and not a release tag.

### From source

To build from a clone of the repository, you need Go and nothing else:

```console
$ git clone https://github.com/bbsnly/sdlc && cd sdlc
$ ./task build
$ export PATH="$PWD/dist:$PATH"
```

If you want to know the clone is good before you trust it, `./task check` runs
everything CI runs.

To use the plugin from the same clone, and not an installed one, start the
session with `claude --plugin-dir /path/to/sdlc/plugin`.

### By hand, from a release

Every [release](https://github.com/bbsnly/sdlc/releases) carries a prebuilt
`sdlc` for each platform, so you may download one yourself and check it before
it goes anywhere. First, pick the archive for your machine:

| Machine | Archive |
| --- | --- |
| macOS, Apple silicon | `sdlc_<version>_darwin_arm64.tar.gz` |
| Linux, x86-64 | `sdlc_<version>_linux_amd64.tar.gz` |
| Linux, ARM64 | `sdlc_<version>_linux_arm64.tar.gz` |
| Windows, x64 | `sdlc_<version>_windows_amd64.zip` |
| Windows, ARM64 | `sdlc_<version>_windows_arm64.zip` |

Each holds `sdlc` (`sdlc.exe` on Windows) with the licence, the README and the
changelog beside it. Download the archive together with the release's
`checksums.txt`, check one against the other, and put `sdlc` in a directory on
your `PATH`.

On macOS or Linux:

```console
$ version=0.6.1 asset=sdlc_0.6.1_darwin_arm64.tar.gz
$ curl --proto '=https' --tlsv1.2 -fsSLO "https://github.com/bbsnly/sdlc/releases/download/v${version}/${asset}"
$ curl --proto '=https' --tlsv1.2 -fsSLO "https://github.com/bbsnly/sdlc/releases/download/v${version}/checksums.txt"
$ grep " ${asset}\$" checksums.txt | shasum -a 256 -c -
$ tar -xzf "${asset}" sdlc
$ mkdir -p ~/.local/bin && mv sdlc ~/.local/bin/
```

`shasum -c` prints `OK` for a good download. If it prints anything else,
delete the archive and do not run what is in it. Where `shasum` is missing,
`sha256sum -c -` does the same.

On macOS, download with `curl`, as above, and not through a browser. macOS
quarantines a binary that a browser downloaded and refuses to run it the first
time. If that happens, `xattr -d com.apple.quarantine sdlc` releases it.

On Windows, in PowerShell:

```powershell
$version = '0.6.1'; $asset = "sdlc_${version}_windows_amd64.zip"
$base = "https://github.com/bbsnly/sdlc/releases/download/v$version"
Invoke-WebRequest "$base/$asset" -OutFile $asset
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt
$want = ((Get-Content checksums.txt) -match " $([regex]::Escape($asset))$" -split '\s+')[0]
(Get-FileHash $asset -Algorithm SHA256).Hash.ToLower() -eq $want
Expand-Archive $asset -DestinationPath sdlc
```

The check prints `True` for a good download. Move `sdlc\sdlc.exe` into a
directory on your `PATH` and open a new terminal. The install script uses
`%LOCALAPPDATA%\Programs\sdlc\bin`, and that is a good choice here too.

## Verifying a download yourself

The installers check every download for you, and refuse to install one that
does not match its checksum. The npm package goes a step further. It carries
the checksums that were written when the release was built, so the check does
not depend on a list fetched from the same place as the download.

You may go further than a checksum and confirm where an archive was built.
Every archive is attested, along with `checksums.txt` and the two install
scripts: GitHub records which workflow, in which repository, at which commit,
produced them. With the GitHub CLI, check an archive like this:

```console
$ gh attestation verify sdlc_0.6.1_darwin_arm64.tar.gz --repo bbsnly/sdlc
```

The same works on an install script, which is worth doing if you would rather
read one before you run it:

```console
$ curl --proto '=https' --tlsv1.2 -fsSLO https://raw.githubusercontent.com/bbsnly/sdlc/v0.6.1/install.sh
$ gh attestation verify install.sh --repo bbsnly/sdlc
```

Fetch the script at the release's tag, as above, and not at `main`. The tag is
the content that was attested, and `main` moves.

Two more things are worth knowing. Each archive ships with an SBOM, a
`.sbom.json` file that lists everything inside it. And the `Source code`
archives GitHub adds to every release are not built by this project, so they
are neither attested nor listed in `checksums.txt`.

## How the plugin finds sdlc

The plugin's hook calls `sdlc` on your agent's tool calls, so it has to find
one. You rarely need to think about how. This section is for when you do: you
run Claude Code with no terminal, you keep `sdlc` somewhere unusual, or a
session has told you that the binary was not found.

### The plugin fetches its own binary

The plugin does not depend on the `sdlc` you installed. The first time a
session starts after you add it, the plugin downloads the release of `sdlc`
that matches its own version and keeps it for the hook. This is why a plugin
added from Claude Desktop's plugin browser works with nothing else installed.

The download is checked against checksums the plugin itself carries, before
anything is unpacked. An archive that does not match is neither installed nor
run.

If the download fails, the hook falls back on the `sdlc` on your `PATH`, and
then on one in an installer's default directory, whether or not that directory
is on your `PATH`. Offline, the download can take up to about two minutes to
give up, and it is tried again each time a session starts until a binary is in
place.

A desktop app such as Claude Desktop keeps the `PATH` it was started with. If
you change your `PATH` for the hook's sake, quit the app fully and open it
again.

To have the hook run an `sdlc` of your choosing, set `SDLC_BIN` to its full
path in the environment Claude Code starts with. That also turns the download
off. In Claude Desktop, you set it in the
[local environment editor](claude-desktop.md#set-variables-in-the-local-environment-editor).

[Troubleshooting](troubleshooting.md#the-sdlc-binary-was-not-found-so-nothing-is-being-enforced)
lists what a session is told when the download fails, and what to do about
each reason.

### The plugin without the binary

If the hook finds no `sdlc` at all, nothing is enforced. The plugin does not
hide that from you, and it does not carry on by other means:

- `/sdlc:next` says the binary is missing, offers to install it, and does
  nothing else until you say yes. It does not work the gates by hand.
- The session working a story is told on every tool call that the binary was
  not found. A session that starts in a project set up for the loop is told
  once, when it starts.
- If you type one of the plugin's slash commands, you are told that it did
  not run.
- Every other session carries on as if the plugin were not there.

The thirteen agents are not a general review kit, with or without the binary.
Each is told to work only a gate of a story you started with `/sdlc:next`, and
to say so and stop when handed anything else, such as a code review. The model
follows that as an instruction, and nothing enforces it.

### The skills on their own

The loop's runbook is also an [Agent Skill](https://agentskills.io), so the
`skills` CLI can install it into any agent that reads them:

```console
$ npx skills add bbsnly/sdlc
```

Know what this route brings, and what it does not. It brings the runbook: the
order of the gates, what each one produces, and what to do when one fails. It
does **not** bring the `sdlc` command, the agents or the hook, so nothing is
recorded, nothing is frozen and nothing is refused. The `next` skill checks
for the command and for the agents before it does anything, and stops if
either is missing.

Use this route to read the loop, or to pin the runbook in a repository that
installs the rest some other way. To run the loop in Claude Code, add the
plugin.

## Updating

Update the command and the plugin together. An old one beside a new one may
not know what the other asks of it.

To update the command, run the installer you used again. With npm, ask for the
newest package by name:

```console
$ npx @bbsnly/sdlc@latest install
```

On Windows, close anything that is running `sdlc.exe` first. A running program
cannot be replaced, and the installer says so and leaves the old one in place.

To update the plugin, type this into Claude Code:

```text
/plugin update sdlc@sdlc
```

Then restart Claude Code. Until you do, it keeps running the old plugin's
hooks, and nothing says so. `sdlc version` in a new terminal confirms the
command's release.

The marketplace serves the plugin as it is on the repository's default branch,
so the plugin you receive may be a little ahead of the newest release of the
command. The two are kept compatible on purpose.

A release with breaking changes comes with a migration guide. It says what to
change, in what order, and how to check that it worked:

- [0.5.0 to 0.6.1](migrations/v0.5.0-to-v0.6.1.md)
- [0.6.1 to 0.7.1](migrations/v0.6.1-to-v0.7.1.md)

## Uninstalling

To remove the command on macOS or Linux, delete it:

```console
$ rm ~/.local/bin/sdlc
```

On Windows, delete `%LOCALAPPDATA%\Programs\sdlc\bin` and take it off your
`PATH`. Delete the directory even if it is already off your `PATH`: the hook
looks there regardless, and runs an `sdlc` it finds.

To remove the plugin, type this into Claude Code:

```text
/plugin uninstall sdlc@sdlc
```

A project's own `.sdlc/` directory is yours. Nothing removes it for you.

## Next steps

Now that `sdlc` is installed, you are ready to use it:

- [Getting started](getting-started.md#set-up-your-project) sets up a project
  and takes your first story through the loop
- [Adding sdlc to an existing project](guides/adding-to-an-existing-project.md)
  goes through what `sdlc init` touches in a repository that already has code,
  tests and conventions
- [Running the loop from Claude Desktop](claude-desktop.md) covers what
  differs where you have no terminal
