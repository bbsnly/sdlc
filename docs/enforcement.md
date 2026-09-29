# Enforcement

The loop is a set of rules, and rules that are only asked for are advice. This
page lists every rule that is actually enforced, what it refuses, and what to do
instead.

## What this is, and what it is not

Enforcement uses Claude Code's own surfaces and nothing else: a `PreToolUse`
hook, a `Stop` hook, the plugin's agents, and the `sdlc` command. A
`PostToolUse` hook runs your [`commands.fmt_file`](configuration.md#commands),
and refuses nothing. No git hooks are installed,
and `sdlc` never writes to `.git/hooks`.

**This is a discipline tool, not a sandbox.** It constrains an assistant that is
trying to do the right thing and takes the shortest path to it. It does not
defend against one that is trying to escape, and it would be dishonest to imply
otherwise. The rules below are narrow on purpose, and every refusal names the
sanctioned route — a refusal that only says "no" makes an assistant retry, work
around the hook, or give up, all worse than the thing being prevented.

## When any of it applies

Nothing is enforced unless **all** of these are true:

1. The project has `.sdlc/config.json`, found by looking up from where the session and the
   tool call are, as far as the repository root and not into a directory
   `GIT_CEILING_DIRECTORIES` names.
2. A story is being worked on — `.sdlc/state/active` names one.
3. The call comes from the Claude Code session working the story, or from one
   of its agents. `sdlc start` records the session it runs in, in
   `.sdlc/state/session`, so the session that starts or resumes a story is the
   one held to it, and every other session in the repository is left alone.
   Run outside Claude Code, `sdlc start` records no session: a new story holds
   no session until one picks it up with `/sdlc:next`, and a story resumed that
   way keeps the session it had. `/sdlc:next` stops rather than work a story
   when `sdlc start` could not record the session it runs in. A call that names
   no session, or a `.sdlc/state/session` that is empty, unreadable or names
   something that cannot be a session id, holds no session either.
4. `SDLC_ENFORCE` is not `0` in the environment the session started with.

In every other session the plugin is silent: it never sends a session back at
the end of its turn, runs no formatter, writes nothing and prints nothing, with
the binary installed or not. It refuses nothing there but a person's decisions,
below. `sdlc status` and `sdlc doctor` say when a story is under way
and no session is working it.

Left alone means every rule. Another session can edit code and tests and
commit, and the loop commits a story with `git add -A`, so work it has left
uncommitted goes into the story's commit. A commit it makes moves HEAD
under the story, and from then on every gate before the story's own commit is
refused ([SDLC-E0046](troubleshooting.md#sdlc-e0046)), so the story goes to a
person.

Another session's `sdlc` commands do not write into the story.
`sdlc artifact write`, `review add`, `gate`, `escalate`, `cost add`, `freeze`,
`unfreeze`, `approve` and `stop` refuse to run, and change nothing, with
[SDLC-E0048](troubleshooting.md#sdlc-e0048), when `CLAUDE_CODE_SESSION_ID`
names a Claude Code session other than the one `.sdlc/state/session` records.
This keeps one session's `sdlc` commands out of a story another is working. It
is not a barrier against an assistant set on getting round it: `sdlc start` is
not refused, because it is how `/sdlc:next` takes a story over; a command with
no `CLAUDE_CODE_SESSION_ID` reads as a terminal, and a person; and another
session can still edit `.sdlc/` without `sdlc`. Nothing is refused while no
session is recorded, and `sdlc ack` changes no story.

Outside those, the hook allows everything and says nothing, with one exception:
a person's decisions, refused from a tool call in any session, in any project
with `.sdlc/config.json` the call reaches (the session's, the working
directory's, or one a path in the command names).

- `sdlc ack` is refused there always. The log of what landed on trunk is always
  there for a person to read.
- `sdlc release` is refused there always. It takes a story from the checkout
  holding it ([`release-is-a-human-decision`](#release-is-a-human-decision)).
- `sdlc approve` and `sdlc unfreeze` are refused while a project the call
  reaches, or the one the command moves to with `cd`, has a story a person could
  decide on: one under way, whichever session holds it or none, or one handed to
  a person with `sdlc escalate` that nobody has answered. With no story under
  way and none waiting, they are not. After a move the hook cannot follow, such
  as `cd "$OLDPWD"`, `cd ../$name` or `popd`, they are refused as if a story
  waited where it led.
- A command nested too deep to read
  ([`command-too-deep-to-read`](#command-too-deep-to-read)) is refused whatever
  it runs, since any of these could be inside it.

A command is read to its end, so `sdlc approve A-1; sdlc ack --through HEAD` is
refused for the `sdlc ack` in it. `sdlc escalate` ends the iteration, so the
answer to it always comes while no session is held, and no session's assistant
approves another's story. A session that is not using sdlc never meets the
refusals of `sdlc ack`, `sdlc approve` and `sdlc unfreeze`: each is refused only
when an assistant runs that command itself. A command nested too deep to read
is refused in any of those projects, whatever else it does.

The hook also **fails
open**: an unreadable payload, a missing configuration, a path it cannot
resolve — an error ends in "carry on", and in the session working a story, with
a message saying what is off. A call that cannot be read names no session, so
it goes through with nothing said. A hook that blocks a session because of its
own bug is worse than the mistake it was trying to prevent.

The exceptions are the files a rule exists to protect. A test freeze or a gate
record that cannot be read is not treated as absent, because then breaking one
would be the way round it: tests stay frozen and the commit waits until the file
reads. `sdlc doctor` names it under "loop state".

## A repository with more than one checkout

Every checkout of a repository, the main checkout and each linked worktree made
with `git worktree add`, works one backlog and one record of each story, kept in
the main checkout. Its `.sdlc/state` is its own. A session is held in the
checkout it started its story in, and the rules follow it into the others.

**Finding the session.** A call is judged as the session's wherever it runs.
When no checkout the call reaches holds the session, the hook asks the
repository: first every checkout a claim names, whatever their number, and then
the first 64 linked worktrees, for an iteration that was running when this
release was installed and has no claim yet. The first `sdlc` command that writes
there turns that into a claim, and `sdlc doctor` says when a repository has more
than 64 worktree entries, since a session past the 64th is not found until then.
Adding worktrees does not move a checkout a claim names out of reach, and the
claims are refused to a held session wherever it names them.

**The other checkouts.** From any checkout, a session held in one is refused,
in every other checkout of the repository, what it is refused in its own:
writing the backlog, `.sdlc/` (the story records, the configuration and that
checkout's own `.sdlc/state`), `.git/` (the claims included), `.claude/` and
`CLAUDE.md`, by an absolute path, a link, `..` through a link, `~`, `cd`,
`find`, `xargs` or code handed to an interpreter; discarding what that checkout
holds with `git -C … reset --hard` and its like; taking the checkout away, or a
directory that holds it
([`loop-state-through-the-tool`](#loop-state-through-the-tool)); and committing
there, however git is pointed at it
([`commit-in-another-checkout`](#commit-in-another-checkout)). Every
`git worktree` action but `list` is refused in any checkout
([`worktree-through-the-tool`](#worktree-through-the-tool)), and so is changing
`core.worktree` or `core.bare`
([`worktree-setting-through-the-tool`](#worktree-setting-through-the-tool)).
Reading anything, and writing with the shell files no rule protects, are not
refused. The file tools are refused everything outside the checkout the session
is held in, as they always were, and every other checkout nested inside it,
such as a story's worktree under `.sdlc/worktrees/` seen from the main checkout
([`write-outside-repository`](#write-outside-repository)).

Which checkout a path is in is worked out from where it lands on disk, links
followed, by walking up to the nearest directory whose `.git` belongs to this
repository. A `.git` that leads elsewhere is walked past rather than stopped
at, so a repository made with `git init` inside a checkout cannot hide that
checkout's files. The price is that an unrelated repository inside a checkout,
such as vendored code or a submodule never initialised, is under the `.git`
name rule like any other path in that checkout, and a commit made in it is
refused as a commit in that checkout. A worktree nested inside the project, as
a story's worktree under `.sdlc/worktrees/` is inside the main checkout, counts
as another checkout for what it holds, to the shell and to the file tools. A
linked worktree moved by hand to somewhere outside the project is still a
checkout of this repository, even where git's own record of it names its old
place.

**Failing closed.** When the main checkout cannot be found — a bare repository,
`core.bare` set, a rewritten `commondir` — the hook does not read the
worktree's own copy of the records. The commit is refused, every test counts as
frozen, no story counts as finished, and a person's decision counts as waiting.
Where not even git's directory can be read, every path outside the checkout that
might be the main checkout's evidence is refused by name, and the refusal says
to run `sdlc doctor`.

**Not held.** These are known and not closed. Each says what could happen, and
how you would notice.

- **Trunk, or another story's branch, moved from a story's worktree.** A
  session working a story in its worktree can move trunk without committing on
  it: `git update-ref refs/heads/<trunk> HEAD`, or
  `git checkout --ignore-other-worktrees <trunk>` and then a commit. Trunk then
  holds work no gate passed on trunk. In the main checkout, `git status` shows
  changes nobody made there, the difference reversed, because the branch moved
  and its files did not; `git reflog <trunk>` shows when trunk moved. The same
  commands can move or delete another story's branch, `story/<ID>`, as
  `git update-ref -d refs/heads/story/<ID>` does; that story's next gate
  reports it ([SDLC-E0046](troubleshooting.md#sdlc-e0046)).
- **A commit in the next call.** Each call is read on its own. A call that
  stages another checkout's work into the session's index, with
  `git --work-tree=<main> add` or `GIT_INDEX_FILE`, followed by a plain
  `git commit` in a later call, puts that work in the story's commit. The commit
  gate then does not pass: the reviews no longer match the tree
  ([SDLC-E0029](troubleshooting.md#sdlc-e0029)), or the tree no longer matches
  the commit ([SDLC-E0032](troubleshooting.md#sdlc-e0032)), and
  `git show --stat HEAD` in the story's worktree lists files the story never
  touched.
- **What the persistent shell keeps.** The Bash tool keeps one shell from call
  to call, and the hook sees only the environment the session started with. A
  variable exported in an earlier call, such as `export GIT_DIR=<main>/.git`,
  or set by a file run with `source` or `.`, is not seen, so a later
  `git commit` can land on the branch another checkout has checked out, trunk
  included. `git log` there shows a commit no gate passed, and `git reflog`
  when it came. A variable kept that way can also change which story a
  person's decision acts on, and the rules judge the decision only by what
  the command itself names. The story's `gate-record.json` lists every
  approval and unfreeze with when it was made.
- **A program the rules do not read through.** The rules read past the
  wrappers [the shell rules](#rules-on-shell-commands) list, and no others. A
  command that any other program runs, or that code handed to an interpreter
  runs as a command of its own, is not read for what it runs. A commit, the
  removal of a worktree or a checkout, and a person's decisions all get past
  the rules that way. `git log` and `git reflog` show a commit no gate passed,
  `git worktree list` a worktree gone or prunable, and the story's
  `gate-record.json` every approval and unfreeze with when it was made.
- **A command line for another kind of shell.** A string handed to a shell of
  another kind than the tool's, such as `pwsh -Command` or `powershell -c` in
  a `Bash` command, or `bash -c` or `sh -c` in a `PowerShell` one, is read
  with the rules of the tool the command came from, not as that shell reads
  it. So PowerShell's ways of setting a variable are not read in a string
  handed to PowerShell from `Bash`, nor `${NAME:=…}` and `${NAME=…}` in one
  handed to a POSIX shell from `PowerShell`, and a `git commit` after them can
  land in another checkout as a plain commit, which only
  [`commit-gate`](#commit-gate) holds. Where that shell's quoting and escapes
  make a word into something other than the tool's would, the word is read as
  the tool's would make it, by every rule that turns on which program a
  command runs. `git log` and `git reflog` in the other checkout show a commit
  no gate passed, and when it came; `git worktree list` shows a worktree gone,
  or marks it prunable; and the story's `gate-record.json` lists every
  approval and unfreeze with when it was made.
- **`parallel`'s own command lines.** GNU `parallel` runs its command once
  for each argument after `:::`, with the argument put after it, and runs
  each argument as a command line of its own where no command comes before
  `:::`. The rules read the command, `:::` and its arguments as one command
  line instead, so an argument run as a command line of its own is not read
  for what it runs. A commit, the removal of a worktree or a checkout, and a
  write to the loop's own record can each get past that way. `git log` and
  `git reflog` show a commit no gate passed, `git worktree list` a worktree
  gone or prunable, and `sdlc doctor` a loop record it can no longer read.
- **Other ways of setting git's place.** `GIT_DIR`, `GIT_WORK_TREE` and
  `GIT_INDEX_FILE` are read only where they are set in the ways
  [`commit-in-another-checkout`](#commit-in-another-checkout) lists. Set
  another way, as a `for` or `select` loop's variable, through a name
  reference (`declare -n`), with bash's `+=`, `mapfile`, `getopts` or
  arithmetic, with PowerShell's `Copy-Item` or `Rename-Item` on `Env:`, with
  a parameter PowerShell binds in a way the rules do not follow, or with a
  name held in a variable, as in `Set-Item "Env:$n"`, a later `git commit`
  can land in another checkout as a plain commit, which only
  [`commit-gate`](#commit-gate) holds. `git log` and `git reflog` in the other
  checkout show a commit no gate passed, and when it came.
- **Your own git configuration.** Aliases and includes in `~/.gitconfig`, the
  system configuration, a file `GIT_CONFIG_GLOBAL` or `GIT_CONFIG_SYSTEM` named
  when the session started, and anything those include, are not read. A bare
  alias of yours, such as `git st`, is allowed; so would be one whose value
  removes a worktree or makes a commit. `git config --show-origin
  --get-regexp '^alias\.'` lists every alias git would run, and where it came
  from.
- **A path given as a default.** `${X:-…}`, `${X:=…}` and `${X=…}` are read
  without the default, so `rm -rf "${D:-..}"` or `rm -rf "${D:=..}"`, run from
  a story's worktree, is not taken for removing the directory that holds the
  checkout.
- **Code that works out its own path.** A program handed to an interpreter is
  read for the paths it names outright, not for a path relative to where it
  runs or one it works out, such as its own current directory. So
  `node -e 'require("fs").rmSync("..", {recursive: true})'` and
  `python3 -c 'import os, shutil; shutil.rmtree(os.getcwd())'` are not
  refused. For this and the entry above, you would notice the checkout gone:
  `git worktree list` marks it prunable, and every `sdlc` command on its story
  says the checkout holding it no longer exists
  ([SDLC-E0051](troubleshooting.md#sdlc-e0051)).
- **PowerShell spellings of git.** `& (Get-Command git) worktree prune`,
  dot-sourcing a variable, as in `. $G worktree prune`, and a git command whose
  output is assigned, as in `$x = git worktree prune`, are not read as git, so
  no git rule refuses them. A commit made this way is refused at the next gate
  instead ([SDLC-E0046](troubleshooting.md#sdlc-e0046)); anything else shows
  only in what it did, such as a worktree `git worktree list` no longer lists,
  or lists as prunable.
- **Words or a script from standard input, or from a file.** The rules read a
  command as it is written. What reaches a program on its standard input, or
  in a file it is told to read, is not read for what it runs:
  `echo prune | xargs git worktree`, `xargs -a args.txt git worktree`,
  `echo 'git worktree prune' | sh`, `echo commit -m x | xargs git -C <main>`,
  and a script run from a file, such as `sh tidy.sh`. Such a command can remove
  or prune a worktree, or commit in another checkout, and the same holds for
  every rule that turns on which program a command runs. `git worktree list`
  shows a worktree gone, or marks it prunable; `git log` and `git reflog` in
  the other checkout show a commit no gate passed, and when it came; and the
  story's `gate-record.json` lists every approval and unfreeze with when it was
  made.
- **Deleting through the Finder.** AppleScript run with `osascript`, telling
  the Finder to delete a checkout or move it to the Trash, is not read. The
  checkout is then in the Trash, and can be put back from there. Until it is,
  `git worktree list` marks it prunable, and every `sdlc` command on its story
  says the checkout holding it no longer exists
  ([SDLC-E0051](troubleshooting.md#sdlc-e0051)).
- **The shell command a `!` alias runs.** An alias whose value starts with `!`
  runs a shell command. The rules on git and on commits read that command, so
  `git -c alias.x='!git worktree prune' x` is refused, but no other rule
  reads it: a file it writes is not refused, nor is anything else only the
  other rules would refuse. `git status` shows a file it changed, and the
  story's `gate-record.json` lists every approval and unfreeze with when it
  was made, so one nobody remembers making shows there.
- **Ordinary files in another checkout.** The shell can write a file no rule
  protects in another checkout: a story worktree's source from the main
  checkout, or the main checkout's from a story's worktree. Another story's
  frozen tests are among them. `git status` in that checkout shows the change,
  and a frozen test changed that way is reported by that story's next gate as
  a broken freeze ([SDLC-E0025](troubleshooting.md#sdlc-e0025)).
- **`GIT_WORK_TREE` around `sdlc` itself.** An `sdlc` command run with
  `GIT_WORK_TREE` set treats that directory as the checkout, `sdlc start`
  included. The paths `sdlc start` prints say where it made and recorded the
  story.
- **A finished story between its retro and `sdlc stop`.** Its claim goes when
  the story is done, and a checkout's `active` holds only a story in progress or
  waiting for a person, so from a call that reaches only other checkouts the
  session working it is not found, and is held there to nothing until
  `sdlc stop` ends the iteration. In its own checkout it is held as before.
  Nothing says so; the runbook runs `sdlc stop` straight after the retro, which
  closes it.
- **Claims written by a session that holds nothing.** Such a session can write
  or remove a claim or another checkout's `.sdlc/state/active`, and so make two
  checkouts name one story. Each is then refused it
  ([SDLC-E0050](troubleshooting.md#sdlc-e0050), which lists them) until a person
  runs `sdlc stop` in all but one. What the refusal says about each checkout is
  read from files anybody can write: it helps a person choose, and proves
  nothing.

## Rules on writing files

These apply to `Write`, `Edit`, `MultiEdit` and `NotebookEdit`. Reading,
searching and running things are nobody's business here.

### `write-outside-repository`

Refuses a write that resolves outside the repository, symlinks followed.

It also refuses a write into another checkout of this repository nested inside
the one the session is held in: a story's worktree under `.sdlc/worktrees/`,
written from a session held in the main checkout, by its path or through a
link. A file there is that checkout's work, not this story's, and its freeze and
loop state are that checkout's too. The refusal names the checkout the file is
in.

*Instead:* work inside the repository; if you need a file elsewhere, ask. A
file in another checkout of this repository belongs to that checkout's story:
work in the checkout this session holds, and leave that one to the session
holding its story.

### `write-protected-path`

Refuses any write to `.git`, `.claude`, `CLAUDE.md`, `.sdlc/config.json`,
`.sdlc/state` or `.sdlc/claude-progress.json`.

Configuration is the human's, and loop state is the tool's. An assistant editing
either can make the record say something that did not happen.

*Instead:* change configuration by hand outside a running iteration; change loop
state through the `sdlc` command.

`CLAUDE.md` is in that list for the same reason the test freeze exists. A rule
the agent can edit is a rule that stopped applying to it, and a contract rewritten
to get past a gate takes every gate after it with it. If a rule is genuinely
wrong, the loop's answer is to stop and say so, not to edit it and carry on.

### `backlog-is-not-edited`

Refuses any write to the backlog file, wherever
[`backlog.path`](configuration.md#backlogpath) puts it. A backlog linked in from
elsewhere is protected under the name the configuration gives it.

The backlog holds the acceptance criteria the tests are held to and each story's
`risk_tier`, which decides whether a person approves the commit. An assistant
that lowered a story to `low` mid-iteration would take that pause away. The
`sdlc` command still records a story's status there as the loop moves it.

*Instead:* if a story's criteria or risk tier are wrong, say which and stop.
Change the backlog by hand outside a running iteration.

### `gate-record-is-written-by-the-tool`

Refuses any in-place edit of `gate-record.json`, in any story's directory and
not only the one being worked on. A finished story's record is the evidence
that it finished, and the same is true of its reviews and its documents below.

*Instead:* `sdlc gate <name> pass|fail --note "..."`.

### `review-is-written-by-the-tool`

Refuses any write under a story's `reviews/` directory.

*Instead:* `sdlc review add <gate> <role> <verdict>`, which stamps the review
with what was in front of the reviewer.

### `gate-artifact-is-written-by-the-tool`

Refuses any in-place write of `ANALYSIS.md`, `THREATS.md`, `TEST-PLAN.md`,
`PLAN.md`, `VERIFICATION.md` or `RETRO.md`.

This one refuses **everyone**, including the agent whose gate it is. Two reasons,
and they point the same way. A gate's document is loop state, and loop state
goes through the tool. And Claude Code refuses a subagent's file write when the
filename reads like a report, so "only the researcher may write `ANALYSIS.md`"
is a rule nobody could keep.

*Instead:* `sdlc artifact write <name>`.

### `frozen-test-is-not-edited`

Refuses any edit of a file in the freeze, from any role.

*Instead:* change the code until the test passes. If the test itself is wrong,
say which acceptance criterion it got wrong and stop. The person running the
session lifts the freeze with `sdlc unfreeze --reason "..."`, which puts the
change on the record.

### `no-new-test-after-the-freeze`

Refuses a new test file once the tests are frozen, unless
`freeze.allow_new_test_files` is on. A shell command that would create one — a
redirect, `touch`, `cp` — is refused by the same rule, and so is naming one in
a command that runs `sed`, `awk`, `perl` or another interpreter other than
plainly as the file it is given to read. Removing a test file the freeze does
not hold is not: that is what `SDLC-E0043` asks for. "New" is what the freeze
does not hold, not what was written recently, so a test file that has been
there for years reports here too. A frozen test can be reported here rather
than as a frozen test, depending on how it was named; what to do is the same
either way.

*Instead:* say which case is missing and stop. The frozen files are frozen too,
so the case cannot go into one of them either: the person running the session
can unfreeze and freeze again so the new test is covered. With the setting on,
run `sdlc freeze` after adding the file, or `tests_frozen`, `plan`,
`implementation`, `verification` and `commit` refuse with `SDLC-E0043`.

### `implementer-does-not-write-tests`

Refuses the implementer writing any test file, frozen or not, before the freeze
or after it, through the file tools or through the shell, and, where `sed`,
`awk`, `perl` or another interpreter runs in the command, naming one other than
plainly as the file it is given to read. Reading one is not:
`sed -n 1,5p internal/x_test.go` names it plainly as the file `sed` reads.
`cat internal/x_test.go | sed -n 1,5p` names it somewhere else, and is refused.
This is not a setting: `freeze.allow_new_test_files` lets the test author add
one, never the implementer. After the freeze, the rules above answer first: a
frozen file is refused as `frozen-test-is-not-edited` and a new one as
`no-new-test-after-the-freeze`, so this is the rule named only before the
freeze, or once the setting lets a new file past.

*Instead:* make the existing tests pass; if they are wrong, say so rather than
changing them.

### `sdet-writes-tests-only`

Refuses the test author writing anything that is not a test, its own story
directory, or `CODEMAP.md`.

*Instead:* write the tests. If this project keeps fixtures somewhere the loop
does not recognise, say which directory and stop: the person running the session
adds it to `paths.tests`, outside the iteration.

### `researcher-writes-analysis-only`

Refuses the analysis agent writing outside its own story directory and
`CODEMAP.md`.

### `reviewer-reviews`

Refuses any of the seven reviewing agents — architect, security, red-team, perf,
human-advocate, verifier and code-reviewer — writing outside the story's own
directory. The retro agent has a rule of its own, below.

None of the seven is given a file-writing tool, so this is a backstop, not the
thing keeping them out: it is what still holds if an agent's tool list grows.
Like every rule on this page, it governs the file-writing tools. A reviewer's
shell commands meet the shell rules instead, which protect loop state, the
freeze and the commit, and not your source.

A reviewer that changes the work is reviewing its own, and the independence that
made the review worth having is gone. The story directory stays open so a
reviewer can work something out on paper; the verdict itself still goes through
`sdlc review add`, which the rule above holds it to.

*Instead:* record the verdict with `sdlc review add <gate> <role> approve|block`
and say what is wrong rather than fixing it.

### `bookkeeper-writes-the-retro-and-the-map`

Refuses the retro agent writing anything but its own story directory and
`CODEMAP.md`.

The retro records what happened. An agent that can change the code while writing
up the lessons can also write up lessons it has just made untrue.

*Instead:* `sdlc artifact write retro`; `CODEMAP.md` is the only other thing this
gate produces.

### `orchestrator-delegates`

Refuses the main conversation writing code or tests during an iteration, and
any subagent that is not one of the loop's own — Claude Code's general-purpose
agent, or one of yours.

The gates exist so that each is done by an agent that cannot see the others'
reasoning. The conversation doing the work itself is the failure the loop exists
to prevent, and it is reached without breaking a single stated rule unless
something refuses it. Handing the work to a subagent the loop does not know is
the same failure one step removed.

*Instead:* delegate to the agent whose gate it is. If none of them fits, say what
the work is and stop: ending the iteration to do the work outside the loop is
the person's decision.

## Rules on shell commands

Every rule above governs the file-writing tools, and two of them —
`no-new-test-after-the-freeze` and `implementer-does-not-write-tests` — hold
against a shell command too. The rest would leave
`cat > .sdlc/stories/A-1/ANALYSIS.md` walking past them. The rules below close
that, and nothing else about your shell is touched: your tests, your build and
your tooling run exactly as before.

They apply to every tool that runs a command: `Bash`, `PowerShell` and
`Monitor`. They read past what only wraps a command — `( … )`, `env` with the
values its `-u`, `-C` and `-P` take, `sudo`, `nice`, `nohup`, `setsid`,
`ionice`, `chrt`, `unbuffer`, `taskset`, `chronic`, `ts`, `firejail`,
`systemd-run`, `unshare`, `nsenter`, `busybox` and `toybox`, `sh -c`, `cmd /c`
and `xargs`; `env -S`, `flock` and `script` with the command line each is
given; `watch` and `parallel`, which hand their words to a shell, their words
read as one command line, with `parallel`'s `:::` and the arguments after it
among them ([not held](#a-repository-with-more-than-one-checkout)) — as well
as a command sent to the background with `&` or run with PowerShell's `&`. A
program that runs another and is not on this list is read as itself, not for
what it runs ([not held](#a-repository-with-more-than-one-checkout)). They know PowerShell's
cmdlets and their aliases, a parameter given as `-Path:file`, and a backtick
used as an escape. They read a relative path from where the command runs:
after a `cd` earlier in the same command, where an earlier call left the
session, or where `env -C`, `sudo -D`, `systemd-run --working-directory` or
`unshare -w` runs it, as a `cd` in front of it would. And they read a path as
the file it is on disk, so a link to `.sdlc`, or a Windows short name such as
`SDLC~1`, is the file it reaches. The body of a here-document is text, not
commands, so a plan or a review that mentions `git commit` is not refused for
it — unless the text goes to a shell or an interpreter, which runs it.
PowerShell's here-string, `@'` to `'@` or `@"` to `"@`, is a document too, but
only in a `PowerShell` command and only when it is closed; the lines after one
that is never closed are read as commands. In a
`Bash` or `Monitor` command `@'` is an `@` and an ordinary quote, and the
quotes are read as quotes: what is inside them is text, and what is outside
them is commands.

Which program a command runs is read with its quoting, so a commit message, a
`--note` or a `--message` that mentions `sdlc approve` or `git commit` is text,
not a command. Quotes and escapes are read as the tool's shell reads them:
`$'…'` with its escapes decoded, a backslash before an ordinary character
dropped, so `g\it com\mit` and `git $'commit'` are `git commit`, and an escaped
quote, as in `'it'\''s'`, a quote inside the word rather than the start of a
string. A string handed to `bash -c`, `cmd /c`, PowerShell's `-Command` or
`eval`, and a command inside `$(…)` or backticks, is read for the commands it
runs, with the rules of the tool the command came from: a string for a shell
of another kind, such as `pwsh -Command` in a `Bash` command or `bash -c` in a
`PowerShell` one, is read as the tool's own shell would read it, not as that
shell does ([not held](#a-repository-with-more-than-one-checkout)). A commit
in another repository, after a `cd` or with `git -C` or `--git-dir`, is not
held to the story's gates. A commit with `GIT_DIR` set is, and so is one in a
repository inside the project, such as a submodule; where `GIT_DIR` names
another checkout of this repository, it is refused as a commit there
([`commit-in-another-checkout`](#commit-in-another-checkout)).

This is pattern matching, not a shell. It is deliberately narrow.

### `loop-state-through-the-tool`

Refuses a command that would write or delete the loop's own files: `.sdlc/state`,
`.sdlc/config.json`, any story's `gate-record.json`, any `reviews/` directory,
or any gate document — by redirection; through `rm`, `mv`, `cp`, `tee`, `dd`,
`sed -i`, `find -delete`, `git rm`, `git checkout` and their like; or from
inside a program handed to an interpreter.

A scratch file inside a story's directory is not loop state. Reading the loop's
files with `cat`, `grep`, `head` or the file tools is never refused; `sed`,
`awk`, `perl` and interpreters are refused on them whatever they are asked to
do, because each can write through its own program.

It also refuses taking away a checkout of this repository, or a directory that
holds one: the session's own, the main checkout, or another story's worktree,
each with the story and the loop state it holds. That is removing or moving it
with `rm`, `rmdir`, `mv` and their PowerShell and `cmd` twins, moving it to the
trash with `trash`, `trash-put` or `gio trash`, or away with `gio move` or
`gio rename`, the destination of `rsync` with `--delete`, what `find` could
remove with `-delete` or `-exec rm`, a remover at the end of a pipe such as
`xargs rm`, and a remover in code handed to an interpreter. The checkout can
be named any way the shell has for it: by its path, `.`, `$PWD`, `~+` or
`$(pwd)`, and `$OLDPWD` or `~-` for where the shell was before its last `cd`;
in PowerShell, `(Get-Location)`; `..` and the directories above; and a glob or
a brace list that could match it, such as `rm -rf ../*` from a story's
worktree. A glob is refused for what it could
match, which is a guess, so the refusal names the checkout it could reach.
`rsync --delete` into a checkout, or a directory holding one, is refused
because it removes there whatever the source lacks. `rm -rf ./build/*`,
`rm -rf node_modules`, `find . -name '*.pyc' -delete` and, from a story's
worktree, `find .. -empty -delete`, which removes nothing that holds a
checkout, take away no checkout and are not refused. Three that take away none
are refused all the same: `rm -rf build/.*`, because `.*` is read as matching
`..`, as the refusal says; PowerShell's
`[IO.Directory]::Delete((Join-Path $PWD 'build'), $true)`, because `$PWD` names
the checkout; and a `find` that deletes all it finds, with no name to look
for, from `.` at a checkout's root or from `..` just below it, as
`find . -type d -empty -delete` there does, whatever else it tests, because it
is read as taking that checkout away with the loop's record in it. The refusal
names the checkout: at your own checkout's root it gives the loop-state reason,
and where the checkout is another it gives the take-away reason, and either way
the way round is to name the directory itself, as in `rm -rf build` or
`find build -type d -empty -delete`.

*Instead:* `sdlc artifact write`, `sdlc review add`, `sdlc gate`. The freeze is
lifted by the person running the session. For `.sdlc/config.json` there is no
command: the person running the session changes it by hand, outside a running
iteration. Remove only what the story made, by naming it inside the checkout,
such as `rm -rf build`; a checkout is removed by a person, in their own
terminal.

### `protected-path-through-the-tool`

Refuses a command that would write or delete `CLAUDE.md`, anything under
`.claude`, anything under `.git`, or the backlog — the paths
`write-protected-path` and `backlog-is-not-edited` protect from the file tools
that are not the loop's own state. It matches the way the filesystem does, so
`claude.md` is `CLAUDE.md` on macOS and Windows.

A settings file under `.claude` is where hooks are configured, `CLAUDE.md` is
the contract every gate reads, and the backlog is the story itself, so a shell
command that rewrote any of them would take the rules with it. Reading them with
`cat`, `grep`, `head` or the file tools is never refused, and neither is
`.gitignore` or `.github/`. As with loop state, `sed`, `awk`, `perl` and
interpreters are refused on them, though not for naming a whole directory, as
`python3 -m pytest --ignore .sdlc` does. A path outside the project, such as
your own `~/.claude`, is not the project's and is not protected.

*Instead:* change them by hand outside a running iteration. If what one says is
wrong, stop and say what.

### `frozen-test-through-the-tool`

Refuses a command that would write, rewrite or delete a frozen acceptance test:
by redirection; through `rm`, `mv`, `cp`, `tee`, `sed -i`, `find -delete`,
`git checkout` and their like; or from inside a program handed to `sed`, `awk`,
`perl` or another interpreter, on the command line or on standard input. A
command given its program somewhere other than its first argument — with `-f`,
`--file`, or gawk's `--source`/`-e` and `--exec`/`-E` — leaves nothing to say
which of its files is data, so every file it names is treated as one the
program could write.

A patch applied with `git apply` or `patch` names its files inside the patch,
where this cannot see them. `sdlc gate` still refuses a frozen test that
changed, with `SDLC-E0025`.

Reading one with `cat`, `grep`, `head` or the file tools is never refused where
`sed`, `awk`, `gawk`, `perl` or another interpreter is not in the command too,
and reading the tests is how the implementer knows what to implement. Those are
narrower, because each writes through a program of its own that this cannot
read. A frozen test named plainly as the file one of them is given to read —
`sed -n 1,5p internal/x_test.go` — is allowed, unless that command was given its
program somewhere other than its first argument, when every file it names is
treated as one the program could write. Otherwise the name is looked for as a
word of the command, not as the program would work it out, anywhere on a
command line one of these tools runs on, and refused there: inside the program
text, in quotes, or in another part of a pipeline or list. A `;`, `&&`, `||` or
`|` inside the program counts as another part of the list, so
`sed -n '1,5p;10,20p' internal/x_test.go` is refused too. The flags above are
looked for in any word starting with `-`, wherever it stands and program text
included, so `awk '$1 == "-f" {print $2}' internal/x_test.go` is refused as
well. Before the freeze this rule does nothing at all.

*Instead:* leave it alone. If it genuinely has to change, say which test and
why; the person running the session lifts the freeze with
`sdlc unfreeze --reason "..."`.

### `unfreeze-is-a-human-decision`

Refuses `sdlc unfreeze` run from a tool call, however `sdlc` is reached — on
`PATH`, by path, through `npx` or `go run`. In a session the story is not held
in, it is refused while a story is under way or waiting for a person.

Lifting the freeze is sometimes right, and it is also exactly the move an agent
would make to reach green. So it is a person's decision: they run it in their
own terminal, where the hook is not asked. A note that only mentions the word,
such as `--note "no need to unfreeze"`, is not refused.

*Instead:* say which frozen test is wrong and which acceptance criterion it gets
wrong, and stop there.

### `approval-is-a-human-decision`

Refuses `sdlc approve` run from a tool call, however `sdlc` is reached. In a
session the story is not held in, it is refused while a story is under way or
waiting for a person, and not in a project with neither.

An approval is a person's answer to a question the loop stopped to ask them:
`sdlc escalate` hands the story over and ends the iteration. An agent that could
give the answer would be approving its own work. A reviewer's `approve` verdict,
given with `sdlc review add`, is a different command and is not refused.

*Instead:* `sdlc escalate <type> --message "..."`, and stop. The person reads the
work and runs `sdlc approve` in their own terminal.

### `acknowledgement-is-a-human-decision`

Refuses `sdlc ack` run from a tool call, however `sdlc` is reached, in any
session, in any project with `.sdlc/config.json` the call reaches (the
session's, the working directory's, or one a path in the command names),
whether or not a story is being worked on.

`sdlc log` starts after the commit acknowledged, so acknowledging one takes every
story before it off the list a person reads. An agent that could run it would be
deciding which stories nobody needs to look at. `sdlc log` itself is not refused.

The log of what landed on trunk is always there for a person to read, so there
is always a decision here to keep, between stories and in a session that never
started one as much as during a story.

*Instead:* say what `sdlc log` lists, and stop. The person reads it and runs
`sdlc ack --through <commit>` in their own terminal.

### `release-is-a-human-decision`

Refuses `sdlc release` run from a tool call, however `sdlc` is reached, in any
session, in any project with `.sdlc/config.json` the call reaches, whether or
not a story is being worked on.

Releasing a story takes it from the checkout of the repository that holds it,
after which any checkout can start it. An agent that could run it could take a
story another checkout is working, or let go of the one it is held to and with
it every rule holding it.

*Instead:* say which checkout holds the story and stop. A person runs
`sdlc release <ID> --reason "..."` in their own terminal if it is to move.

### `stop-is-a-human-decision`

Refuses `sdlc stop` run from a tool call while the story still has a gate to
pass, however `sdlc` is reached.

Every rule on this page holds only while a story is being worked on, so ending
the iteration part-way is the way round all of them at once: a commit no gate
passed goes through straight after. Ending a story whose gates have all passed
is the runbook's last step, and is not refused.

*Instead:* work the next gate, or hand the story to a person with
`sdlc escalate <type> --message "..."` and stop. The person ends the iteration
with `sdlc stop` in their own terminal.

### `review-is-recorded-by-its-reviewer`

Refuses `sdlc review add <gate> <role> <verdict>` run by anyone but the agent
it names: the main conversation, a subagent outside the loop, another reviewer,
or the implementer recording an approval of its own work.

Every reviewer records its own verdict, and the gate passes on those verdicts.
A review recorded by someone else is a review nobody did. A person recording one
in their own terminal is not asked.

*Instead:* delegate to the reviewer, which reads the work and records its own
verdict.

### `commit-gate`

Refuses `git commit` until every gate before the commit gate has passed. The
refusal names the first one that has not. The git commands that make commits of
their own are refused the same way: `merge`, `cherry-pick`, `revert`, `am`,
`rebase` and `commit-tree`, apart from `--abort` or `--quit` on its own, and a
`--no-commit`, `merge --squash` or `cherry-pick -n` that leaves the change in the
working tree for `git commit`. So is an alias to one of them, set with
`git -c`, `git config`, `--config-env` or the `GIT_CONFIG_*` variables, in the
command or in the environment the session started with; an alias in your own
git configuration file is not read, and one the hook cannot read at all is
refused ([`git-command-cannot-be-read`](#git-command-cannot-be-read)). A commit
made in a way the hook cannot read is
refused at the next gate instead, which will not pass once HEAD has moved since
the story started ([`SDLC-E0046`](troubleshooting.md#sdlc-e0046)).

Once they have, it also refuses a commit of work that has changed since the
verifier and the code reviewers approved it. Their reviews are stamped with the
working tree as it was, outside `.sdlc/` and without the `status` and `updated`
fields `sdlc` writes into the backlog, and a change anywhere else makes them
stale; the refusal names whose. Measuring the tree takes a moment, so it is done
only for `git commit`.

A commit this rule lets through is also written down: the HEAD it is made on,
in the repository's git directory at `<git-dir>/sdlc/commits/<story>/`, so that
the commit gate can tell the story's commits from anybody else's. Only a command
that commits now is: not one that sets an alias, rehearses with `--dry-run` or
leaves the change uncommitted. Nor is one whose repository the command's words
do not identify as this one, such as after `cd "$HOME/other"`, with
`git -C "$DIR"` or with `GIT_DIR` set, or in a repository nested inside the
project: this rule holds such a commit back in case it is this repository's,
and writing it down on the same guess would record this repository's HEAD for
a commit made elsewhere. It is written only for the session working the
story, never refuses anything itself, and is never committed or pushed. The
commit gate believes it, so it is a claim the hook saw a commit coming rather
than proof; [Reviewing what landed on trunk](guides/reviewing-what-landed-on-trunk.md#ranges-to-read-with-care)
says what that leaves open.

A story whose risk tier is in `human_gates.pre_commit_pause_tiers` is also
refused until a person has approved that same tree with `sdlc approve`. A story
missing from the backlog is refused too, because its tier cannot be known.

A gate record that is missing or will not parse counts as no gate passed, and
the refusal names the file. `sdlc start` always writes one, so either means
something damaged it. `sdlc stop`, run by a person, still ends the iteration
when the record cannot be read.

*Instead:* finish the gates — `sdlc status` shows where the story stands.
Committing without them is for the person running the session to decide:
`sdlc stop`, which turns this rule off along with the iteration, is refused from
a tool call until they have passed
([`stop-is-a-human-decision`](#stop-is-a-human-decision)).

### `commit-in-another-checkout`

Refuses a commit, or a command that makes one as `commit-gate` counts them, in
another checkout of the repository: with `git -C`, `cd`, `env -C` or
`sudo -D` into the main checkout or another linked worktree, with `--git-dir`
naming the main checkout's `.git` or another worktree's git directory, or with
`--work-tree` naming another checkout. It refuses whatever the story's gates
say.

It reads the same places however git is given them in these ways, and only
these, each read with the rules of the tool the command came from, so a string
handed to a shell of another kind is not read for that shell's own ways
([not held](#a-repository-with-more-than-one-checkout)). `GIT_DIR` names the
repository, `GIT_WORK_TREE` the work tree and `GIT_INDEX_FILE` the index, set
anywhere earlier in the command:

- as an assignment, in front of the command or on its own, read as handed on
  to git whether or not it is exported; or with `export`, `declare`,
  `typeset`, `local`, `readonly` or `env`;
- with `printf -v NAME`, with or without `--` after the name;
- with `read` from a here-string (`<<<`); what `read` takes from anywhere else
  is not in the command, so cannot be placed;
- in a `Bash` or `Monitor` command, with `${NAME:=…}` or `${NAME=…}`, read as
  setting it whether or not it was set before, and inside single quotes too;
- in a `PowerShell` command, with `$env:NAME =` or `${env:NAME} =`, with `+=`
  for either, with `[Environment]::SetEnvironmentVariable`, or with
  `Set-Item`, `New-Item`, `Set-Content` or `Add-Content`, or their aliases
  `si`, `ni`, `sc` and `ac`, on a path that starts with `Env:`. Their
  parameters are read in any order, each named after a `-` in full, or cut
  short to a part that names it alone, with its value after a space, a `:` or
  both; a switch the cmdlet takes, by that name, given bare or with its value
  joined to the `:`; one path, quoted or not, given with `-Path`, with
  `-LiteralPath` or its aliases `-PSPath` and `-LP`, or first; `New-Item`'s
  `-Name` under `Env:` itself; and the value given with `-Value`, or next.
  `Add-Content` and `+=` are read as setting what they add. A value that comes
  down the pipeline is not in the command, so cannot be placed.

`core.worktree` names the work tree, given with `git -c`, `--config-env` or the
`GIT_CONFIG_*` variables. The same variables in the environment the session
started with count too. An index in another checkout's git directory, such as
the main checkout's `.git/index`, is that checkout's. A place that cannot be
placed, given through a variable the shell has yet to expand or in one of the
ways above, is refused as though it were another checkout. So is a commit
later in the same command than a `git add`, `rm`, `mv`, `restore`, `reset` or
other index write run with another checkout's work tree or index.

The story's gates cover only the checkout it is held in: what they reviewed is
not what would be committed in another, and that the story may commit says
nothing about it. `--git-dir` naming the session's own worktree's git
directory is its own checkout, and meets `commit-gate` as a plain `git commit`
does, and so does `GIT_DIR`, `GIT_WORK_TREE` or `GIT_INDEX_FILE` naming its own.

*Instead:* commit from the checkout the story is held in.

### `worktree-through-the-tool`

Refuses `git worktree` for every action but `list`: `add`, `remove`, `move`,
`prune`, `lock`, `unlock`, `repair`, and an action the hook cannot read,
however git is reached, an alias included, in any checkout. `git worktree list`,
`git worktree -h`, `git help worktree` and `man git-worktree` are not refused.

`git worktree` makes, moves or takes away a checkout of this repository, and
each story is held in the checkout git lists for it: its claim names that
checkout's path. Moving or removing a worktree ends the hold on the story in it,
pruning forgets one, and adding one can put a checkout where a story's worktree
is about to go.

*Instead:* `sdlc start` makes a story's worktree; a person adds, removes, prunes
or repairs one in their own terminal. To read another branch, use
`git show <branch>:<path>` or `git diff <branch>`.

### `worktree-setting-through-the-tool`

Refuses `git config` changing `core.worktree` or `core.bare`, in any scope,
whether it sets, adds, replaces or unsets them, and removing or renaming the
`core` section. Reading them is not refused.

Changing either changes which directory a checkout of this repository works in,
and it can move the main checkout's work tree out from under it, and with it
every story that checkout holds.

*Instead:* a person changes `core.worktree` and `core.bare` in their own
terminal. If git says this is not a work tree, stop and tell them.

### `git-command-cannot-be-read`

Refuses a git command whose subcommand, or an alias it defines or runs, the
hook cannot read, and one whose configuration comes from somewhere the hook
cannot read:

- a subcommand the shell has yet to expand, such as `git $CMD prune` or
  `git "$(echo worktree)" prune`; a brace list such as `git {worktree,} prune`
  is expanded first, and each word it makes is read, and one that makes more
  words than the rules read is refused;
- a program the shell has yet to expand is read as git, so `$G worktree prune`
  and `"$(command -v git)" commit` are judged as git commands; where the word
  after it has yet to be expanded too, as in `$X $Y`, that word would be git's
  subcommand, and cannot be read. That refuses commands that never run git:
  `$EDITOR "$FILE"`, `$CC $CFLAGS -o x x.c`, and
  `for f in ./t/*; do "$f" "$ARG"; done`, which runs each script in `t`, are
  refused, and the refusal says to name the program itself;
- aliases nested deeper than the rules read, or an alias that leads back to
  itself;
- `!` aliases whose shell command lines, counted each time one runs, come to
  more than the rules read in one command, 64 KB: an alias that runs another
  many times, a few levels deep, is refused rather than read for longer than the
  hook is given, and the refusal says to run what the aliases would run as
  commands of their own, a few at a time;
- an alias whose value cannot be read, wherever it is set: `-c alias.x=…`,
  `git config alias.x …`, `--config-env=alias.x=VAR` with no assignment to
  `VAR` in the command, or `GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_<n>` and
  `GIT_CONFIG_VALUE_<n>`, or `GIT_CONFIG_PARAMETERS`, whose value the shell has
  yet to expand or that will not parse;
- an include, `include.path` or `includeIf.<condition>.path`, given in the
  command, or `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM` or `GIT_CONFIG` set in
  it to a file other than `/dev/null`; `git config` writing an include is
  refused too.

The rules for git read what git would run. An alias set in the command is read
as its value, so `git -c alias.x='worktree remove --force' x .` is judged as
`git worktree remove`, an alias defined with `git config` is judged by what it
would run, and one whose value starts with `!` by the shell command it runs. A
git in that shell command runs with the settings of the git command around it,
which git hands on, so in
`git -c alias.a='!git b' -c alias.b='worktree prune' a` the `git b` is
`git worktree prune`.
What the hook cannot read, it cannot judge, and every other git rule would let
it pass unread. An alias in your own git configuration, such as `git st`, is
not read and not refused ([not held](#a-repository-with-more-than-one-checkout)).

*Instead:* spell git's subcommand and any alias out in the command itself, with
no include or configuration file. Where the program is not git, name it, such
as `python3`, rather than a variable. Where aliases hand the shell more than the
rules read, run what they would run as commands of their own, a few at a time.

### `enforcement-stays-on`

Refuses a command that sets `SDLC_ENFORCE`, `SDLC_BIN`, `CLAUDE_PROJECT_DIR` or
`CLAUDE_CODE_SESSION_ID`.

The switch that turns enforcement off belongs to the person who started the
session. A switch an assistant can reach is not a control.
`CLAUDE_CODE_SESSION_ID` is one: `sdlc start` records the session it finds
there as the one the story holds, so setting it hands the story to a session
that does not exist.

*Instead:* if a rule is wrong, say which one and why.

### `story-stays-in-its-session`

Refuses running `claude`, or running `@anthropic-ai/claude-code` through a
package runner such as `npx`, `npm exec` or `pnpm dlx`, from the session
working the story. Installing the package is not refused.

A Claude Code session started from a shell is a session of its own, and the
story holds only the session working it, so none of these rules would hold
what the new one did. A script file that runs `claude`, or sets
`CLAUDE_CODE_SESSION_ID`, is not read, like any other script file.

*Instead:* hand the work to this session's agents. A person who wants another
session opens one in their own terminal.

### `command-too-deep-to-read`

Refuses a command that nests `$( )` or backticks more than 32 deep.

Every level is read for the commands it runs, and a line nested thousands deep
took longer to read than Claude Code gives the hook, which then lets the command
run unchecked. Nothing a person writes nests that deep. It applies to any
command: it is refused in any session, in any project with `.sdlc/config.json`
the call reaches (the session's, the working directory's, or one a path in the
command names), whether or not a story is being worked on, because `sdlc ack`
or an approval could be buried in it.

*Instead:* write it as separate commands.

## Stopping mid-story

A session that ends its turn while a story is being worked on — without handing
the story to a person or ending the iteration — leaves the story where nobody
is looking. Finishing a gate is not a place to stop: the next one is waiting.
The `Stop` hook sends that stop back, with what to do instead: work the next
gate or `sdlc escalate`, or, once every gate has passed, `sdlc stop`.

A stop it has already sent back goes through, so a session is never held in a
loop. The count is kept per story and starts again whenever anything is recorded
on it. Once [`loop.max_stop_blocks`](configuration.md#loop) stops in a row have
been sent back with nothing recorded — three, by default — the next one hands
the story to a person with a `loop_stalled` escalation and ends the iteration.
`sdlc status` lists it as waiting, and `sdlc approve` lets it start again.

In an interactive session this is felt: a reply that ends mid-story is sent back.
`"max_stop_blocks": 0` turns the guard off, and so does `SDLC_ENFORCE=0`.

## Rules the command enforces

The hook is only half of it. `sdlc gate <name> pass` refuses when the gate did
not actually happen:

| Refused | Code |
| --- | --- |
| an earlier gate has not passed | `SDLC-E0031` |
| definition of ready, for a story with no acceptance criteria | `SDLC-E0044` |
| the gate's documents are not on disk | `SDLC-E0020` |
| a reviewer has not reported, or reviewed something that has changed since | `SDLC-E0029` |
| a blocking reviewer blocked, or perf found a stated performance budget broken | `SDLC-E0030` |
| the tests are not frozen, or the freeze belongs to another story | `SDLC-E0023` |
| a frozen test changed or vanished | `SDLC-E0025` |
| a test file the freeze does not hold, at `tests_frozen`, `plan`, `implementation`, `verification` and `commit` | `SDLC-E0043` |
| the commit gate, with work still uncommitted | `SDLC-E0032` |
| the commit gate, with a note naming a commit its range does not hold | `SDLC-E0049` |
| any gate before the commit, once HEAD has moved since `sdlc start` | `SDLC-E0046` |

A `fail` is always recordable. A gate can fail precisely because its work could
not be done, and refusing to record that would leave the loop with nowhere to
put the truth.

`sdlc start` holds new work to the same standard before it begins. A story
picked up again is not held to it, because its own work is in the tree:

| Refused | Code |
| --- | --- |
| HEAD is not on `git.trunk_branch`, with `git.worktrees` false | `SDLC-E0038` |
| uncommitted work that belongs to no story, with `git.worktrees` false | `SDLC-E0039` |
| trunk is behind `origin`, with `git.remote` on | `SDLC-E0040` |
| trunk fails `commands.smoke`, in the story's new worktree with `git.worktrees` on | `SDLC-E0041` |
| the story's worktree or branch cannot be made, with `git.worktrees` on | `SDLC-E0053` |

## Turning it off

`SDLC_ENFORCE=0` in the environment you start the session from disables every
hook rule. It cannot be set from inside a session.

Use it when you are debugging the loop itself. If you find yourself reaching for
it during ordinary work, the rule that is in your way is probably wrong — please
open an issue and say which one.

## Seeing what happened

A hook that allows a call says nothing unless something is wrong, and its stderr
goes to Claude Code's debug log, which makes "why did nothing happen" the
hardest question to answer from outside. This turns the hook's decisions into a
log you can read:

```console
$ export SDLC_DEBUG_FILE=/tmp/sdlc-hook.log
```

`SDLC_DEBUG=1` without a file writes the same log to stderr. `SDLC_TRACE=1` adds
how long each call to `sdlc` took, which is how to tell whether a slow session is the hook.
