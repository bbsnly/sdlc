# Why was I refused?

While a story is being worked on, a `PreToolUse` hook looks at every file write
and every shell command before it runs. This page shows how to read a refusal,
how to find the rule behind it, and what to check when the hook refuses nothing
that you expected it to refuse.

## Read the refusal

A refusal is a JSON reply that Claude Code hands to the assistant. Here is the
implementer trying to write a test file:

```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"the agent that implements a story does not write its tests -- that is the separation the loop is made of. Instead: make the existing tests pass; if they are wrong, say so rather than changing them [implementer-does-not-write-tests]"}}
```

The reason always has the same three parts:

1. **What the rule is**, in one clause: the agent that implements a story does
   not write its tests.
2. **`Instead:`**, the sanctioned way to get the same thing done.
3. **The rule id** in square brackets at the end.

Read the `Instead:` part before you work around anything. It is usually one
command. For example, the main conversation running `git commit` too early
gets:

```text
this story has not been through the gates that come before committing: dor has
not passed. Instead: finish the gates -- `sdlc status` shows where this story
stands; committing without them is for the person running the session to decide
[commit-gate]
```

The rules run in order and the first refusal wins, so you only ever see one
rule per call.

## Look the rule up

Every rule id is a heading on the [enforcement](../enforcement.md) page, so the
id in brackets takes you straight to it:
[`implementer-does-not-write-tests`](../enforcement.md#implementer-does-not-write-tests),
[`commit-gate`](../enforcement.md#commit-gate), and so on.
Each section says what the rule refuses, why, and what to do instead.

The rules come in two groups. The
[rules on writing files](../enforcement.md#rules-on-writing-files) govern
`Write`, `Edit`, `MultiEdit` and `NotebookEdit`, and most of them depend on
which agent is asking. The
[rules on shell commands](../enforcement.md#rules-on-shell-commands) apply to
every agent alike, with two exceptions. A shell command that writes a test file
is refused from the implementer, under the same
`implementer-does-not-write-tests` id as the file tools use. And
`sdlc review add` is refused from anyone but the reviewer it names.

If a rule is wrong for your project, say which one and why. Do not route around
it.

## When nothing was refused

The hook enforces nothing unless all of these are true: the project has
`.sdlc/config.json`, `.sdlc/state/active` names a story, and `SDLC_ENFORCE` is
not `0`. See [when any of it applies](../enforcement.md#when-any-of-it-applies).

When the hook cannot do its job, it still lets the call through. This is
failing open, and it does not happen silently. The session shows a system
message that starts with `sdlc:`. For example:

```json
{"continue":true,"systemMessage":"sdlc: .sdlc/state/active could not be read, so nothing is being enforced in this session. Run `sdlc doctor` to see why."}
```

Take these messages seriously. Each one means a rule you are relying on is off
or running in a reduced form. Every message and what it switches off is listed
under [warnings the hook prints](../troubleshooting.md#warnings-the-hook-prints).

## Run `sdlc doctor`

Every warning sends you to `sdlc doctor`, and it is the fastest way to find out
which file is at fault:

```console
$ sdlc doctor
sdlc doctor

  ok       git repository       /work/invoices
  ok       git command          found on PATH
  ok       configuration        .sdlc/config.json  (Go project)
  ok       backlog              user_stories.json  (1 story, one runnable)
  problem  loop state           the test freeze could not be read (SDLC-E0005): invalid character 'o' in literal null (expecting 'u')
                                fix: every test is treated as frozen until .sdlc/state/tests.lock reads: restore it if you keep a copy, or lift it on the record with "sdlc unfreeze --reason ..." in your own terminal and run "sdlc freeze", which freezes the tests as they are now
  ok       project contract     CLAUDE.md carries the "## SDLC Contract" section
  ok       commands             build, coverage, fmt, fmt_check, fmt_file, lint, smoke, test
  ok       sdlc on PATH         /usr/local/bin/sdlc

1 problem. The fix is above it.
```

It changes nothing, and it exits non-zero when something is wrong. It checks
from the outside in. When the configuration will not read, the checks that
depend on it are reported as `skipped`, not passed; the loop state is still
checked, because the hook reads those files without the configuration. Fix the
problem and run it again.

The last line is the question the hook's launcher asks. When `SDLC_BIN` is set
but names nothing runnable, the launcher passes over it without a word and runs
whatever else it finds, so the binary enforcing your session is not the one you
meant. `sdlc doctor` is what says so.

## See every decision

A hook that allows a call says nothing about it, which makes "why did nothing
happen" hard to answer. Start the session with `SDLC_DEBUG_FILE` set, and every
decision is written to a file you can read:

```console
$ SDLC_DEBUG_FILE=/tmp/sdlc-hook.log claude
```

Each file write adds lines like these:

```text
time=2026-09-14T08:58:05.911+02:00 level=DEBUG msg="hook considering" tool=Write agent=sdlc:implementer role=implementer path=/work/invoices/src/a.go rel=src/a.go outside=false story=AUTH-3 project=/work/invoices
time=2026-09-14T08:58:05.911+02:00 level=DEBUG msg="hook decision" event=PreToolUse considered=true allowed=true rule=""
```

`role` is the agent name with its `sdlc:` or `sdlc-` prefix removed, which is
what the rules match on. An empty `agent` is the main conversation, and a
subagent that is not one of the loop's own is held to the same rules. `considered`
is `false` when the hook never got as far as the rules. `rule` names the rule
that refused. A shell command logs `hook considering a command` instead, with
`commit_ready` and the reason a commit would be refused.

Setting `SDLC_DEBUG_FILE` is enough on its own. `SDLC_DEBUG=1` without a file
writes the same log to the hook's standard error, which ends up in Claude Code's
debug log instead of in your session. `SDLC_TRACE=1` adds a `stage` line with
how long each call took, which is where to look when the hook is slow.

## Turn enforcement off

When you are debugging the loop itself, start the session with `SDLC_ENFORCE`
set to `0`:

```console
$ SDLC_ENFORCE=0 claude
```

It must be exactly `0`; `false` does not count. With it set, the hook allows
every call and prints no message about it, and the `Stop` hook and the
formatter hook stand down too, so remember that you set it.

Only the person who starts the session can do this. The hook reads its
environment from the Claude Code process that launched it. A shell command
inside the session runs in a process of its own and cannot change that
environment. A command that tries is refused all the same:

```text
SDLC_ENFORCE decides whether the loop is enforced at all, and it is set by the
person who started the session, not from inside it. Instead: if a rule is wrong,
say which one and why, and stop
[enforcement-stays-on]
```

The same rule covers `SDLC_BIN` and `CLAUDE_PROJECT_DIR`, which decide which
binary runs and which project it checks. See
[turning it off](../enforcement.md#turning-it-off).

## "The sdlc binary was not found"

The plugin runs a small launcher, and the launcher hands every call to the
`sdlc` binary. It looks for the binary at `SDLC_BIN`, then in the plugin's own
`bin` directory, then on `PATH`. When none of them has it, every call goes
through, and in a project that uses sdlc — one with `.sdlc/config.json` — the
session shows:

```text
sdlc: the sdlc binary was not found, so nothing is being enforced. why: the
plugin is installed, but the binary it drives is not on PATH and is not in the
bin directory of the plugin. fix: run sdlc doctor in your terminal; if that also
fails, reinstall with npx @bbsnly/sdlc install
```

Every other session says nothing: the plugin is installed for all of them, and
a project that does not use sdlc has nothing to enforce. That includes a
session opened in a directory above a project that uses sdlc — without the
binary, the launcher looks only from the session's own directory and where
it runs, so start the session inside the project to see the message.

Nothing is refused until the binary is found. Install it by any route in
[installation](../installation.md#1-the-binary), or run `/sdlc:next`, which
offers to install it.

## Where to go next

- [Enforcement](../enforcement.md): every rule, and why it exists
- [Troubleshooting](../troubleshooting.md): error codes, and the hook's
  warnings
- [When a frozen test is wrong](when-a-frozen-test-is-wrong.md): the route
  behind `frozen-test-is-not-edited`
- [Stopping and resuming a story](stopping-and-resuming-a-story.md): what
  `sdlc stop` hands back to you
