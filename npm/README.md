# @bbsnly/sdlc

[sdlc](https://github.com/bbsnly/sdlc) is a delivery loop for
[Claude Code](https://claude.com/claude-code). It takes one user story at a
time through nine gates: failing tests are written from your acceptance
criteria and frozen before any code exists, the plan and the diff are each
reviewed, and the commit comes last. A hook holds your agent to every gate.

This package installs the `sdlc` command. It needs Node 18 or newer, and the
loop itself needs git 2.17 and Claude Code 2.1.309 or newer.

## Install

First, install the command. One line does it on macOS, Linux and Windows:

```console
$ npx @bbsnly/sdlc install
```

The installer downloads the native `sdlc` binary built for your machine,
checks it against the checksums this package carries, and puts it in
`~/.local/bin`, or in `%LOCALAPPDATA%\Programs\sdlc\bin` on Windows. If that
directory is not on your `PATH`, it prints the lines to add.

What you run afterwards is `sdlc` itself, with nothing of Node behind it. The
package has no postinstall script, so nothing is installed until you ask.

Next, add the plugin. Type these two commands into Claude Code:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

In the Code tab of Claude Desktop, you add it from the
[plugin browser](https://github.com/bbsnly/sdlc/blob/main/docs/claude-desktop.md#install-the-plugin-from-the-plugin-browser)
instead.

Then set up your project, from anywhere inside its repository:

```console
$ sdlc init
```

From here,
[Getting started](https://github.com/bbsnly/sdlc/blob/main/docs/getting-started.md)
takes you to your first story in about ten minutes.

## Choose a version or a directory

To install another release, or to put the command somewhere else, pass
`--version` and `--dir`:

```console
$ npx @bbsnly/sdlc install --version 0.6.1 --dir ~/bin
```

[Installation](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md)
covers every other way to install, with or without Node.

## Update

Update the command and the plugin together. First the command:

```console
$ npx @bbsnly/sdlc@latest install
```

Then the plugin, in Claude Code:

```text
/plugin update sdlc@sdlc
```

Restart Claude Code afterwards.
[Updating](https://github.com/bbsnly/sdlc/blob/main/docs/installation.md#updating)
lists the migration guide for each release with breaking changes.

## Licence

`sdlc` is open-source software under the
[MIT licence](https://github.com/bbsnly/sdlc/blob/main/LICENSE).
