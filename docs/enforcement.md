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
holds with `git -C … reset --hard` and its like; taking the checkout away; and
committing there ([`commit-in-another-checkout`](#commit-in-another-checkout)).
Reading anything, and writing files no rule protects, are not refused. The file
tools are refused everything outside the checkout the session is held in, as
they always were.

Which checkout a path is in is worked out from where it lands on disk, links
followed, by walking up to the nearest directory whose `.git` belongs to this
repository. A `.git` that leads elsewhere is walked past rather than stopped
at, so a repository made with `git init` inside a checkout cannot hide that
checkout's files. The price is that an unrelated repository inside a checkout,
such as vendored code or a submodule never initialised, is under the `.git`
name rule like any other path in that checkout, and a commit made in it is
refused as a commit in that checkout. A worktree nested inside the
project, as one under `.sdlc/` would be, counts as another checkout for what it
holds.

**Failing closed.** When the main checkout cannot be found — a bare repository,
`core.bare` set, a rewritten `commondir` — the hook does not read the
worktree's own copy of the records. The commit is refused, every test counts as
frozen, no story counts as finished, and a person's decision counts as waiting.
Where not even git's directory can be read, every path outside the checkout that
might be the main checkout's evidence is refused by name, and the refusal says
to run `sdlc doctor`.

**Not held.** These are known and not closed here:

- A session's own `git worktree remove`, `git worktree move` and
  `git worktree prune`, and `mv` of its own checkout. Each ends its own hold,
  because the claim names the old path. They are for the next story (US-029.2,
  decided 2026-09-24T18:44:45Z).
- A commit in a linked worktree the walk cannot find: one whose `.git` does not
  lead into this repository, or that was moved. A repository or work tree named
  through a variable the shell has yet to expand is judged as the project, as it
  always was; so is one set with `GIT_DIR` or `GIT_WORK_TREE` in the
  environment, by any spelling: `export`, an assignment in front of the
  command, or `env`. A `--work-tree` outside every checkout is not held.
- Another checkout's work tree reached through git's configuration rather than
  its flags. `git -c core.worktree=<main> commit` is judged as a commit in the
  session's own checkout, and meets the commit gate there, where
  `git --work-tree=<main> commit` is refused; so is `git --work-tree=<main> add`
  followed by a plain `git commit`. Once the session's own story is ready to
  commit, either lands the other checkout's changes on its branch.
- Removing a directory above another checkout, such as `rm -rf <main>/..` or a
  directory that holds every checkout. Taking a checkout away is refused only
  where the command names the checkout itself, as it is for the project's own
  root and the directories above it.
- The file tools and a worktree nested inside the main checkout, such as
  `<main>/wts/W`. To a session held in the main checkout, that worktree's
  `.sdlc/state` and `.sdlc/config.json` are paths inside its own checkout, and
  the file tools' rules match from the checkout's root, so they can be written
  with the file tools. The shell is refused the same writes.
- A finished story between its retro and `sdlc stop`. Its claim goes when the
  story is done, and a checkout's `active` holds only a story in progress or
  waiting for a person, so from a call that reaches only other checkouts the
  session working it is not found, and is held there to nothing until
  `sdlc stop` ends the iteration. In its own checkout it is held as before.
- A session that holds nothing can write or remove a claim or another
  checkout's `.sdlc/state/active`, and so make two checkouts name one story. Each
  is then refused it until a person runs `sdlc stop` in all but one. What the
  refusal says about each checkout is read from files anybody can write: it
  helps a person choose, and proves nothing.

## Rules on writing files

These apply to `Write`, `Edit`, `MultiEdit` and `NotebookEdit`. Reading,
searching and running things are nobody's business here.

### `write-outside-repository`

Refuses a write that resolves outside the repository, symlinks followed.

*Instead:* work inside the repository; if you need a file elsewhere, ask.

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
`cat > .sdlc/stories/A-1/ANALYSIS.md` walking past them. These nine close
that, and nothing else about your shell is touched: your tests, your build and
your tooling run exactly as before.

They apply to every tool that runs a command: `Bash`, `PowerShell` and
`Monitor`. They read past what only wraps a command — `( … )`, `env`, `sudo`,
`sh -c`, `cmd /c`, `xargs` and their like — as well as a command sent to the
background with `&` or run with PowerShell's `&`. They know PowerShell's
cmdlets and their aliases, a parameter given as `-Path:file`, and a backtick
used as an escape. They read a relative path from where the command runs:
after a `cd` earlier in the same command, or where an earlier call left the
session. And they read a path as the file it is on disk, so a link to `.sdlc`,
or a Windows short name such as `SDLC~1`, is the file it reaches. The body of a
here-document is text, not commands, so a plan or a review that mentions
`git commit` is not refused for it — unless the text goes to a shell or an
interpreter, which runs it. PowerShell's here-string, `@'` to `'@` or `@"` to
`"@`, is a document too, but only in a `PowerShell` command and only when it is
closed; the lines after one that is never closed are read as commands. In a
`Bash` or `Monitor` command `@'` is an `@` and an ordinary quote, and the
quotes are read as quotes: what is inside them is text, and what is outside
them is commands.

Which program a command runs is read with its quoting, so a commit message, a
`--note` or a `--message` that mentions `sdlc approve` or `git commit` is text,
not a command. A string handed to `bash -c`, `cmd /c`, PowerShell's `-Command`
or `eval`, and a command inside `$(…)` or backticks, is read for what it runs. A
commit in another repository, after a `cd` or with `git -C` or `--git-dir`, is
not held to the story's gates. A commit with `GIT_DIR` set is, and so is one in
a repository inside the project, such as a submodule.

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

*Instead:* `sdlc artifact write`, `sdlc review add`, `sdlc gate`. The freeze is
lifted by the person running the session. For `.sdlc/config.json` there is no
command: the person running the session changes it by hand, outside a running
iteration.

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
working tree for `git commit`. So is an
alias to one of them set with `git -c` or `git config`; an alias already in your
git configuration is not read. A commit made in a way the hook cannot read is
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
another checkout of the repository: with `git -C` or `cd` into the main checkout
or another linked worktree, with `--git-dir` naming the main checkout's `.git`
or another worktree's git directory, or with `--work-tree` naming another
checkout. It refuses whatever the story's gates say.

The gates were passed on the work in the checkout the story is held in. A commit
in another checkout records work nobody reviewed, and that the story may commit
says nothing about it. `--git-dir` naming the session's own worktree's git
directory is its own checkout, and meets `commit-gate` as a plain `git commit`
does.

*Instead:* commit from the checkout the story is held in.

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
| HEAD is not on `git.trunk_branch` | `SDLC-E0038` |
| uncommitted work that belongs to no story | `SDLC-E0039` |
| trunk is behind `origin`, with `git.remote` on | `SDLC-E0040` |
| trunk fails `commands.smoke` | `SDLC-E0041` |

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
