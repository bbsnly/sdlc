# @bbsnly/sdlc

Installs [sdlc](https://github.com/bbsnly/sdlc): a story-driven delivery loop
for Claude Code, with gates a model cannot talk its way past.

```sh
npx @bbsnly/sdlc install
```

That downloads the release for your machine, checks it against the release's
checksums, and puts the `sdlc` binary in `~/.local/bin` (on Windows,
`%LOCALAPPDATA%\Programs\sdlc\bin`).

Then, in Claude Code:

```text
/plugin marketplace add bbsnly/sdlc
/plugin install sdlc@sdlc
```

## What this package is, and is not

It is an installer. What ends up on your PATH is the native `sdlc` binary, not
a Node wrapper around it: sdlc runs as a Claude Code hook on every matching
tool call, where a Node process start would cost more than everything else in
the loop put together.

For the same reason there is no postinstall script. Installing a native binary
is something you ask for, not something that happens while you were installing
something else — and `--ignore-scripts` changes nothing here.

## Options

```sh
npx @bbsnly/sdlc install --version 0.1.0   # a version other than the latest
npx @bbsnly/sdlc install --dir /usr/local/bin
```

`SDLC_VERSION` and `SDLC_INSTALL_DIR` do the same, which is easier in a script.

## Other ways to install

```sh
curl -fsSL https://raw.githubusercontent.com/bbsnly/sdlc/main/install.sh | sh
go install github.com/bbsnly/sdlc/cmd/sdlc@latest
```

## Licence

MIT. See [the repository](https://github.com/bbsnly/sdlc) for the full text,
the documentation, and the source.
