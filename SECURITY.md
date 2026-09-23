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

What the commit gate checks is that its range holds exactly the commits it was
told about, never that it holds every commit the story made. The known limits,
so that a report can say which one it goes past:

- A commit the hook did not watch is missing from the range and leaves no sign;
  and one made straight on a HEAD the hook wrote down joins it with no sign
  either, when it is the last commit before the gate.
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

## What is out of scope

- The behaviour of Claude Code itself. Report those to Anthropic.
- A model writing bad code that this tool then dutifully runs through its
  gates. The loop is a process, not a sandbox.
