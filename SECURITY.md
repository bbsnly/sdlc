# Security policy

## Reporting a vulnerability

Please report security issues through GitHub's private vulnerability reporting:
[open a report](https://github.com/bbsnly/sdlc/security/advisories/new). That
keeps the details private until there is a fix to ship.

Please do not open a public issue for a vulnerability.

You should get an acknowledgement within a week. This is a single-maintainer
project, so please read that as a good-faith commitment rather than an SLA.

## Which versions get fixes

The latest release, and only that. This is a single-maintainer project before
1.0: there are no maintenance branches, and a fix ships as the next release
rather than as a patch to an older one.

| Version | Supported |
| --- | --- |
| the latest release | yes |
| anything older | no — upgrade |
| `main` | yes, and that is where a fix lands first |

## What is in scope

`sdlc` installs a binary, registers hooks with Claude Code, and decides whether
a command a model proposed is allowed to run. The parts where a defect is a
security defect rather than a bug:

- **The shell policy.** A command that should have been denied and was allowed.
  Spellings that evade classification are exactly the class of report that is
  most useful here.
- **The installer.** Anything that lets a downloaded artifact be replaced,
  or that places a binary without verifying its checksum against the pins
  shipped in the package.
- **Privilege of the model caller.** Anything that lets a model-driven session
  perform an action reserved for a human in a terminal.
- **What the hook writes down about commits.** Each commit the hook allows, it
  writes down the HEAD the commit is made on, in the repository's git directory
  at `<git-dir>/sdlc/commits/<story>/`, and the commit gate works out the
  story's range of commits from those. A range is what the hook saw the session
  land, not proof of it: what is written down is a claim, the commit gate
  believes it, and it is protected from the session only by the same
  classification that protects `.git/hooks`. A way for a session to write or
  remove one, or to make the gate record a commit the session did not land
  without one of the limits below, is in scope.
- **Which checkout holds a story.** In a repository with linked worktrees, the
  checkout that starts a story holds it, and a claim at
  `<git-dir>/sdlc/claims/<ID>.json`, in the git directory every checkout
  shares, names that checkout's path. Git never shows, commits or pushes it,
  and the path in it is only compared, printed and used to find that
  checkout's session; it is not written to any record. A session held in one
  checkout is held to the same rules in every other checkout of the repository.
  A way for a held session to write another checkout's backlog, records or
  claims, to commit there, or to leave the hook's reach by adding worktrees, is
  in scope; the limits `docs/enforcement.md` lists under "A repository with more
  than one checkout" are known.
- **The hook's deadline.** Claude Code kills a hook that outlasts the 30 seconds
  it gives a tool call, and a killed hook lets the call through with no rule
  applied. So the hook answers first: a command the hook cannot finish reading
  in time, or a change to a file it cannot check in time, is refused 25 seconds
  after the hook starts. A way to get a call the hook has learnt it checks past
  that deadline without a refusal is in scope. So is a way to make the hook
  refuse for time in a session that holds no story, other than for a command
  in a project that takes part.

What the commit gate checks is that its range holds exactly the commits it was
told about, except in the case the last limit below describes, never that it
holds every commit the story made. The known limits, so that a report can say
which one it goes past:

- A commit the hook did not watch is missing from the range and leaves no sign,
  except in the last limit below; and one made straight on a HEAD the hook wrote
  down joins it with no sign either, when it is the last commit before the gate.
- Some commands are written down though they commit nothing, because nothing in
  their words tells them from a commit: one git refuses, `git merge` of an
  ancestor, `git rebase` onto a branch it is already up to date with, `git am`
  on an empty mailbox. So is a command the person denies at Claude Code's
  permission prompt, or another hook refuses, because the hook is asked before
  either. A person's commit made on that HEAD is then read as the story's.
- The flags are read from the words as written, not as the shell expands them.
  `git commit -m x $'--dry-run'` and `git commit -m x --dry-\run` are both
  `--dry-run` to the shell, and `$DRY`, `$(echo --dry-run)` or a variable set
  earlier in the line can be anything; each is written down though it commits
  nothing.
- Nothing is written down for a commit whose repository the words do not
  identify as this one: a directory reached through a variable, a glob, `~-`,
  `cd -`, `popd`, a script run by `source` or `eval`, or CDPATH; a `-C` or
  `--git-dir` the shell expands; any assignment to `GIT_DIR`; or a repository
  nested inside the project. The commit is allowed or refused exactly as
  before, and the story loses that observation, so its range can come out
  shorter or as no range. That is the direction chosen: the refusal assumes
  this project when unsure, and the recorder taking the same guess wrote down
  this project's HEAD for a commit made elsewhere.
- An inline alias's expansion (`git -c alias.x='commit --dry-run' x`) is split on
  spaces rather than read as a shell would, so quotes inside it are kept and a
  flag in quotes is not seen. The command is then written down though it commits
  nothing.
- History rewritten under the loop — a rebase, a force-push, a squash — can put a
  person's commit in a story's range and leave the story's own out. Dropping
  what was written down on a rewrite — recording no range once a HEAD it names
  is no longer reachable — was considered and not taken: it catches only
  `git rebase` spelled that way, misses
  `git pull --rebase`, and turns every `git commit --amend` story into one with
  no range.
- Anyone who can write into the git directory can plant a HEAD, and what that
  buys is an unrelated commit already on trunk read as the story's.
- In a tier that waits for a person, a story in its own worktree is held at the
  stop to the commit its range ends at: a commit amended or added after the
  commit gate is refused until a person approves it. A story with no range is
  not held at all, which a commit the hook did not write down can bring about
  on the first pass; a range that stops short of HEAD is refused at the stop
  unless only commits that change the root `CODEMAP.md` alone follow it, and any
  number of those pass, with any author and message. The first message and
  author a session gives a commit are read by nobody.
- A story in its own worktree that is put onto a later trunk commit than the one
  it was cut from, by a person's rebase, by the one rebase the hook allows a
  session or by the stop's own replay, has its range start at that trunk commit,
  and the commits from there up to the first one the hook watched being made are
  the story's with nothing naming them: the rebase made them again, on commits
  that did not exist before. A person's commit made right on that trunk commit,
  or on the rebased story before the session's next commit, is read as the
  story's, and a story whose range was "no range recorded" because a person's
  commit sat among its own can read as a clean range once it has been put onto a
  later trunk commit. The change the reviewers saw and the warning of the loop's
  own files do not depend on it: they cover everything from that trunk commit to
  HEAD, and where trunk has been pointed at one of the story's own commits, that
  commit and those before it are outside both.

The deadline has a known limit of its own. The hook refuses only a call it has
learnt it checks, so a hook that Claude Code kills before it has learnt whether
the call is one it checks still lets the call through and says nothing. Before
it knows, the hook looks for the project from every path a command names, and
that search is not bounded; nor is a call that never finishes arriving.
`docs/enforcement.md` describes the limit under `not-checked-in-time`. A report
that gets the same result with a shorter command, or after the hook has learnt
it checks the call, is a new finding and not this known limit.

`SDLC_HOOK_DEADLINE_MS` can only shorten the deadline. It is read from the
environment the session was started in, which nothing run inside the session
changes, and it is not among the settings a command is refused for naming: the
most it can do is have a call refused sooner.

## What is out of scope

- The behaviour of Claude Code itself. Report those to Anthropic.
- A model writing bad code that this tool then dutifully runs through its
  gates. The loop is a process, not a sandbox.
