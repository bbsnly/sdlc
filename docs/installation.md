# Installation

Two things get installed: the `sdlc` binary, which owns the loop's state and
enforces its rules, and the Claude Code plugin, which is what your session
talks to. The loop needs both, and in a project that uses sdlc the plugin will
tell you if the binary is missing rather than quietly enforcing nothing. The
plugin's agents work without the binary; see
[the plugin without the binary](#the-plugin-without-the-binary).

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

Same download and destination. It installs the release the package was
published with, and checks it against the checksums the package carries, which
nobody can change after publishing. `--version` or `SDLC_VERSION` installs
another release. What ends up on your
`PATH` is the native binary, not a Node wrapper around it: sdlc runs as a hook
on every matching tool call, and a Node process start costs more than
everything else in the loop put together.

There is no postinstall script, so `npm install --ignore-scripts` changes
nothing here — the download happens when you ask for it.

Unlike the Windows install script, it does not change `PATH`: when the
directory is not on it, it prints the lines to add it. The plugin's hook finds
the binary in the default directory either way.

### By hand, from a release

Every [release](https://github.com/bbsnly/sdlc/releases) carries a prebuilt
binary for each platform, so nothing needs compiling. Pick the archive for your
machine:

| Machine | Archive |
| --- | --- |
| macOS, Apple silicon | `sdlc_<version>_darwin_arm64.tar.gz` |
| macOS, Intel | `sdlc_<version>_darwin_amd64.tar.gz` |
| Linux, x86-64 | `sdlc_<version>_linux_amd64.tar.gz` |
| Linux, ARM64 | `sdlc_<version>_linux_arm64.tar.gz` |
| Windows, x64 | `sdlc_<version>_windows_amd64.zip` |
| Windows, ARM64 | `sdlc_<version>_windows_arm64.zip` |

Each holds the `sdlc` binary (`sdlc.exe` on Windows) with the licence, README
and changelog beside it. Download it with `checksums.txt`, check the one against
the other, and put the binary somewhere on your `PATH`.

On macOS or Linux:

```console
$ version=0.1.0 asset=sdlc_0.1.0_darwin_arm64.tar.gz
$ curl -fsSLO "https://github.com/bbsnly/sdlc/releases/download/v${version}/${asset}"
$ curl -fsSLO "https://github.com/bbsnly/sdlc/releases/download/v${version}/checksums.txt"
$ grep " ${asset}\$" checksums.txt | shasum -a 256 -c -
$ tar -xzf "${asset}" sdlc
$ mkdir -p ~/.local/bin && mv sdlc ~/.local/bin/
```

`shasum -c` prints `OK` for a good download; anything else, delete it and do not
run it. `sha256sum -c -` does the same where `shasum` is missing. A binary
downloaded through a browser on macOS is quarantined and refused on its first
run; `xattr -d com.apple.quarantine sdlc` releases it, and `curl` does not
quarantine what it downloads.

On Windows, in PowerShell:

```powershell
$version = '0.1.0'; $asset = "sdlc_${version}_windows_amd64.zip"
$base = "https://github.com/bbsnly/sdlc/releases/download/v$version"
Invoke-WebRequest "$base/$asset" -OutFile $asset
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt
$want = ((Get-Content checksums.txt) -match " $([regex]::Escape($asset))$" -split '\s+')[0]
(Get-FileHash $asset -Algorithm SHA256).Hash.ToLower() -eq $want
Expand-Archive $asset -DestinationPath sdlc
```

The check prints `True` for a good download. Move `sdlc\sdlc.exe` into a
directory on your `PATH` — `%LOCALAPPDATA%\Programs\sdlc\bin` is where the
install script puts it — and open a new terminal.

To go further than a checksum and confirm which workflow built the archive, see
[Verifying a download yourself](#verifying-a-download-yourself).

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
gives you the `/sdlc:next` skill, two skills you run yourself —
`/sdlc:trunk-review` to look back at what landed and `/sdlc:consolidate` to turn
the retros into changes to your contract and configuration — eleven agents, and
the `PreToolUse`, `PostToolUse` and `Stop` hooks that do the enforcing.

Worth knowing: the marketplace serves the repository's default branch, not a
tag. `/plugin update sdlc@sdlc` therefore gives you the plugin as it is on
`main`, which can be ahead of the binary release you have installed — the
version in the plugin's manifest names the release it was cut for, not the
commit you received. The two halves are kept compatible on purpose, and the
binary is the one that enforces.

The hook looks for the binary on `PATH` and then where the installers put it,
so a desktop app such as Claude Desktop finds a binary in the default directory
even though it keeps the `PATH` it was started with. Installed anywhere else,
quit the app fully and reopen it after changing `PATH`, or set `SDLC_BIN` to the
full path of the binary.

### The plugin without the binary

If you only want the agents, install the plugin and stop there. Without the
binary:

- The agents run. Ask for the code reviewer, the architect or the researcher
  and you get their findings in the session. What they would have stored with
  `sdlc artifact write` or `sdlc review add` is not stored, because that is the
  binary's job.
- `/sdlc:next` says the binary is missing, offers to install it, and does
  nothing else until you say yes. It does not work the gates by hand.
- The hook enforces nothing. Outside a project with `.sdlc/config.json` it says
  nothing either, so sessions in every other repository carry on as if the
  plugin were not there. Inside such a project it says, on every tool call,
  that the binary was not found.
- A binary left where the installers put it (`~/.local/bin`, or
  `%LOCALAPPDATA%\Programs\sdlc\bin` on Windows) still counts: the hook runs it
  even when that directory is not on `PATH`.

### The skill on its own, with `npx skills add`

The runbook is also an [Agent Skill](https://agentskills.io), so the `skills`
CLI can install it into any agent that reads them:

```console
$ npx skills add bbsnly/sdlc
```

There are three skills, `next`, `trunk-review` and `consolidate`, so it asks
which to install. `--skill next` names one, and `--yes` installs all three
without asking. Each goes in `.claude/skills/<name>/` and the equivalent
directory for every other agent it knows, is recorded in `skills-lock.json`, and
gives you `/next`, `/trunk-review` or `/consolidate` in a session.

Know what that route brings and what it does not. It brings the runbook: the
order of the gates, what each one produces, what to do when one fails. It does
**not** bring the binary, the eleven agents, or the hook — so nothing is
recorded, nothing is frozen, and nothing is refused. `next` checks for the tool
and for the agents before it does anything and stops if either is missing,
because a loop with no enforcement behind it is a checklist, and a checklist you
mark off about your own work is worth nothing. `trunk-review` and `consolidate`
check for the tool only. With no hook, nothing refuses `sdlc ack` from the
assistant either: only `trunk-review`'s own instruction not to run it stands
between them.

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
`PATH`. Delete it even once it is off `PATH`: the hook looks there without
`PATH`, and runs a binary it finds. In Claude Code,
`/plugin uninstall sdlc@sdlc`.

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
