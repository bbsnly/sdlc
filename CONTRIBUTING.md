# Contributing

Thanks for looking. This is a single-maintainer project in its pre-release
phase, so the most useful contributions right now are issues: a spelling of a
shell command that the policy classifies wrongly, a platform where something
does not work, or a piece of documentation that did not explain itself.

## Running the checks

A fresh clone needs **Go** and nothing else:

```console
$ ./task check
```

On Windows, `task.cmd check`.

`./task` is a bootstrap wrapper in the manner of `gradlew`: it runs the pinned
version of [Task](https://taskfile.dev) through `go run`. Every other tool this
repository uses is pinned in `Taskfile.yml` and run the same way, resolved
against its own dependency graph. The product's `go.mod` stays free of build
tooling, which is why `go install` of this module pulls nothing you did not ask
for.

Two things Go cannot fetch for you, because they are not Go programs:

- **shellcheck**, at the exact version in `Taskfile.yml`. Different versions
  disagree about what is a warning, so the Taskfile refuses to run rather than
  silently checking something else. `brew install shellcheck`, or see
  [installing shellcheck](https://github.com/koalaman/shellcheck#installing).
- **Node** 22 or newer, because markdownlint runs on it. `./task markdownlint`
  installs it with `npm ci` from the lockfile in `.github/tools/markdownlint`.

`./task check` never skips a leg. A check that skips when its tool is missing
reports success for work it did not do, so every leg either runs or fails with
an install line.

Tests that drive the built binary are worth more than tests that agree with
their own parts, so two checks keep a gap in them visible. A test in `test/e2e`
fails, naming the command, when a command has no case that ran it to the result
the case expected; a new command needs such a case, or an entry with its reason
in the list of known gaps in `test/e2e/zcommands_test.go`. And a test fails when
a function calls `time.Sleep` that is not on the list in
`internal/testscan/sleeps_test.go`; each entry there needs a reason, and an
entry that matches nothing fails too. Only `time.Sleep` is found: other waits,
such as `time.After`, are not.

`./task test-report` prints the same picture from the sources, without running
anything. A `command` line is a command that no case in `test/e2e` runs, and a
`gate` line is a gate record key that none passes; neither is an instruction.
The lines `e2e`, `unit` and `other` give the number of tests in each package.
The exit is always 0, and the task is not part of `./task check`. It reads the
test files, so it can differ from the check above, which records real runs.

The tests of a package that starts with `testenv.Run` run with `HOME` and the
cache directory redirected into a throwaway directory, so that no test takes a
lock in your own cache directory; `GOCACHE`, `GOMODCACHE`, `GOPATH`, `GOENV`
and `npm_config_cache` stay as they were. npm's user configuration
(`~/.npmrc`) is not read while the tests run, so a registry, proxy or cache set
there has to be given as an `npm_config_` variable in your environment. A test
that leaves anything in that cache directory fails the package and is named,
and the message says how to find the test. On Windows the `Microsoft`
directory there is left out: the system's own programs keep it, not a test.

A leg that fails on a file it read also says whether git tracks that file,
so you can tell whether the same failure would happen on a fresh clone.

`./task secrets` also uses `mktemp` and `tar`, which ship with every
supported platform (coreutils on macOS and Linux; Git for Windows' bundled
tools, or Windows' own `tar.exe` since 1803, on Windows), and git 2.31 or
newer. `./task tools` checks for all three and says how to get them if one is
missing.

`./task tools` also runs `npm ci` for markdownlint and `gitleaks --version`,
so the tests that drive those legs find their tools already fetched. The first
run is slower for it and needs the network. `./task markdownlint` skips
gitignored files, as a fresh clone would not have them, and `./task secrets`
scans a snapshot of what git tracks or would accept with `git add`, not the
working tree itself.

`./task hook-time` holds the hook's classifier to a time. The hook gives itself
25 seconds and refuses a call it has not decided by then, so a classifier that
gets slower refuses, for time, long commands it used to answer. The leg times
`shellpolicy.Inspect` in process, with the test matchers and the path resolver
wired as the hook wires them for an implementer whose story has frozen tests,
on nine commands that are slow by construction. All but `links` are the
longest whole command of their shape that fits in 8,192 bytes, and the leg
prints them by these names:

- `perl`: `perl -` followed by 8,181 of the letter `a`, a space and `file`:
  8,192 bytes.
- `here-string`: 630 here-string blocks, each the opener `$s = @'`, the
  one-line body `x` and the closer `'@`, read as PowerShell: 8,190 bytes.
- `dot-glob`: `rm` of one path, 4,093 `a/` segments and then `.*`: 8,191
  bytes. The leg classifies it and never runs it.
- `single-dash-touch`, `single-dash-cp`, `single-dash-sed` and
  `single-dash-tee`: the tool, a space, one single-dash word of the letter `a`,
  a space and `file`: 8,192 bytes each, one shape for each tool so that a bound
  keyed on one tool's name does not pass for the others.
- `links`: `touch` and fifty words, each through a chain of 255 symbolic links
  of its own, which the leg makes in a temporary directory of its own and
  removes. Every word goes through more links than sdlc follows in one path,
  so the shape times the refusal, `SDLC-E0081`, of words past that bound until
  the command's count of links is spent, and not a command allowed at the edge
  of that count. Where no link can be made, as on Windows without Developer Mode
  or an administrator, the leg skips the `links` shape, prints a line that says
  so and why, and times and judges the others as usual.
- `cdpath`: 500 assignments `CDPATH=a`, then `true;` and 614 of `cd x;`: 8,190
  bytes, allowed. It times how `CDPATH` is read on a line of many assignments
  and many relative `cd`s.

The shapes are timed in turn, five times each, and a shape's fastest run counts.
A line for each shape gives that time, the time of its first run and its
budget. The first run is there because the hook is a new process for every
command and pays for the first; it is not compared with anything. The budgets
are `HOOK_PERL_BUDGET_MS`, `HOOK_HERESTRING_BUDGET_MS` and
`HOOK_DOTGLOB_BUDGET_MS`, beside `INIT_BUDGET_MS` in the vars of
`Taskfile.yml`. The single-dash, `links` and `cdpath` shapes take no budget of
their own: each is held to the `perl` budget.

The leg fails when a shape's fastest run is over its budget. It also fails,
however fast the shape was, when `Inspect` allows the `dot-glob` or `links`
shape or refuses any of the others. If that happened on every run, running the
leg again changes nothing, and the place to look is `internal/shellpolicy`, or
for the `links` shape the bound on links in `internal/pathrules`. If it
happened on only some of the runs, the answer depends on something besides the
command (state, time or ordering), and that is what to look for. If a story
meant to change the answer, the answer the leg expects changes with it, in
`internal/tools/hooktime`.

A time over budget is not always a slower classifier. A busy machine goes over
on a tree that has not changed, and not evenly: the `dot-glob` shape rises
first, then `here-string`, and `perl` least; under heavy load every shape
goes over. So when the leg fails on a time:

- On your own machine, run `./task hook-time` again when the machine is quiet,
  which means nothing else on it is building or running tests. When it passes,
  run `./task check`, which is what gates a commit.
- To tell a busy machine from a change of yours, run `./task hook-time` on
  trunk without the change, in a second checkout or after `git stash`. Slow
  there too means the machine; fast there means the change.
- On a CI runner, run the job again.
- If it fails twice on a tree that did not change, the runner is slower than
  the figure allows for, and a person raises the figure in `Taskfile.yml`. The
  same figure is in `hookBudgets` in `test/checklegs/hooktime_test.go` and
  changes in the same commit, whose message says why: it is kept twice so that
  a raise is never one line nobody notices. A raise is not for a failure that
  arrived with a change to `internal/shellpolicy` or `internal/hook`. That is
  the leg doing its work, and every raise widens what a real slowdown can hide
  behind.

The leg is the last in `./task check` and adds about ten seconds to it on a
quiet laptop, and on macOS several more to make and remove the links shape's
12,750 links.

### How long the tests take

go test kills a package that runs past its limit, `TEST_TIMEOUT` in the vars
of `Taskfile.yml`, and names the test that was running. Long before that,
`./task test` says a package is getting slow: after go test's own lines it
prints each package's time, slowest first, then the twenty slowest tests, and
a line for every package that took more than half the limit. Such a package
passed, but a loaded runner can take twice as long, so it is made faster
before it reaches the limit, not given a higher one. `./task check-linux`
prints the same, for Linux.

To find out why a package is slow:

```console
$ ./task profile PKG=./internal/cli
$ ./task profile PKG=./internal/cli RUN=TestLanding FLAGS=-race
```

It runs the package's tests once, prints its slowest tests, and then where its
CPU and its memory went, from `go tool pprof -top`. `RUN` narrows the tests as
`-run` does, and `FLAGS` passes go test anything else, such as `-race`, which
changes where the time goes and which `./task test` runs under, except for the
packages `NO_RACE_PKGS` names in `Taskfile.yml`. The profiles are kept in a
temporary directory the task names, for `go tool pprof -http=:`. A profile is
of the test process alone: a test that spends its time waiting for git or the
built binary shows in the slowest tests and not in the CPU.

The classifier the hook runs on every Bash and PowerShell call has benchmarks:
`BenchmarkInspect` in `internal/shellpolicy`, on commands of the kind a session
runs all day, and in `internal/tools/hooktime`, on the nine shapes the
`hook-time` leg times. To see what a change does to them, run the benchmarks
on trunk and on the change, and compare the two with benchstat, which the
Taskfile pins:

```console
$ ./task bench PKG="./internal/shellpolicy ./internal/tools/hooktime" > old.txt
$ ./task bench PKG="./internal/shellpolicy ./internal/tools/hooktime" > new.txt
$ ./task benchstat OLD=old.txt NEW=new.txt
```

Run the first in a second checkout of trunk, or before the change, on a quiet
machine. `COUNT` (10 by default) is how often each benchmark runs, which is what
benchstat needs to say whether a difference is more than noise, and `BENCH`
narrows them as `-bench` does. Neither `bench` nor `profile` is part of
`./task check`: a benchmark's time is the machine's as much as the code's.

### Linux and Windows before trunk

A test can pass on your machine and fail on a runner because it read your
machine: your git identity, your `HOME`, your shell. `./task check-linux` runs
the Go tests the way CI's Linux jobs do, in Docker, so a Mac finds those
failures before trunk does:

```console
$ ./task check-linux
$ ./task check-linux PLATFORMS="linux/arm64 linux/amd64"
```

It needs a running Docker engine. Each platform gets a fresh clone of `HEAD`,
with no git identity, a `HOME` of its own and no `SHELL`, run as your user and
not as root, in the `golang` image of the `toolchain` in `go.mod`. It clones
`HEAD`, so commit first; what is not committed is not tested. The build and
module caches are kept in `~/.cache/sdlc-check-linux`, so a second run takes
minutes, not tens of them. With no `PLATFORMS` it runs the Docker engine's own
architecture, `linux/arm64` on an Apple silicon Mac. The other one runs under
emulation, several times slower: there `internal/cli` reaches go test's
twenty-minute limit and the timing tests fail on time alone, so its result
says something only about the tests that finished and do not time anything.

Docker on macOS or Linux cannot run Windows. Work goes straight to trunk, so
Windows is first checked there, by CI's `windows-latest` and `windows-11-arm`
jobs; a red job is fixed on trunk, and a release is tagged only on a trunk
commit whose every job is green. To find a Windows failure before trunk, run
`task.cmd check` in a Windows 11 virtual machine. On an Apple silicon Mac that
is Windows 11 on Arm, the same as the `windows-11-arm` runner.

## Testing in a sandbox

A sandbox, here, is a throwaway directory to try a change to sdlc in: a new git
repository with sdlc initialised and one ready story, and the `sdlc` built from
your checkout. It is not the sandbox the manual means when it says that sdlc is
[a discipline tool, not a sandbox](docs/enforcement.md#what-this-is-and-what-it-is-not).
That sentence is about confinement, and this confines nothing. It is somewhere
to run the loop that is not a project you care about.

```console
$ ./task sandbox
```

On Windows, `task.cmd sandbox`. It prints where everything is, and nothing
else:

```text
sandbox     <sandbox>
repository  <sandbox>/repo
sdlc        <sandbox>/bin/sdlc
```

`<sandbox>` is a new directory named `sdlc-sandbox-` and a number, directly
under your temp directory, which only your user can read. A sandbox goes
outside the checkout: if your temp directory is the checkout or inside it,
`./task sandbox` refuses and makes nothing, and the way on is to point `TMPDIR`
(`TMP` on Windows) at a directory outside the checkout and run it again. In the
sandbox:

- `repo` is the repository: `git init`, then `sdlc init`, then one commit, so
  it holds the one example story `sdlc init` writes and nothing uncommitted.
  It has no stack and no commands configured, as any repository `sdlc init`
  finds empty.
- `bin/sdlc` (`sdlc.exe` on Windows) is built from the checkout you ran the
  task in, uncommitted changes included. It is not installed and is not on your
  `PATH`.
- `home` and `tmp` are a home and a temp directory for commands run in the
  sandbox. The sandbox's own build and its git used them. A command you run
  there uses yours unless you point it at these.

To remove one, give its directory back, after `--`:

```console
$ ./task sandbox-remove -- <sandbox>
```

That removes a directory `./task sandbox` made, still where it made it, and
refuses anything else. The task runs from the checkout, so it cannot see where
your shell is and will remove a sandbox you are standing in: `cd` out of the
sandbox before you remove it. If it stops part-way it says what it could not
delete, and the same command finishes the job once that is put right. The one
case it cannot finish is the last step: when everything inside is gone and only
the empty directory could not be deleted, it says so and names the directory,
which is then yours to delete, because nothing in it marks it as a sandbox any
more and the command refuses it. A sandbox you forget stays
until your system clears its temp directory. To find leftovers, look for
`sdlc-sandbox-*` in the directory `TMPDIR` names on macOS and Linux (`/tmp` on
Linux when it is unset; on macOS `echo $TMPDIR` prints a directory under
`/var/folders`), and in `%TEMP%` on Windows.

The build inside `./task sandbox` uses no network: it runs with `GOPROXY=off`,
so every module has to be in your module cache already, which it is after
`./task check` has run once. If the build fails, no sandbox is left behind.

The tests in `test/e2e` whose names begin `sandbox_` are the unattended route.
They run what `./task sandbox` runs, then work a whole story in the sandbox from its
start to trunk, with every tool call put to the hook first and what each
command prints compared with what is expected. The steps the loop keeps for a
person are taken by the test process itself, in the sandbox it made a moment
before; no task, flag or variable takes them, here or in a project. A Claude
Code session that is working a story in this repository should run those tests
and should not drive the loop by hand in a sandbox.

What the tests show about a sandbox keeping to itself is narrower than it
sounds. They close every route known to lead out of the directory and fail if
one opens: the lock and the scratch directories sdlc makes go under the
sandbox's `home` and `tmp`, the repository has no remote, and it is configured
with `protocol.allow=never`. They do not show that nothing is written anywhere
else, because nothing portable can watch for that. That git setting is the
repository's own and stops a fetch made by accident, not one somebody means to
make. Go's build cache and module cache are the stated exception: the build
writes to them, outside the sandbox.

## A note on the Claude Code version

`.claude-code-version` is the version this is developed against. Nothing in CI
installs Claude Code, so it is what we build for rather than a pin a job
enforces. It is also the one number: the README and the installation page are checked against
it, so a bump lands everywhere or it fails.

Claude Code updates itself, so your local copy will run ahead of it. That is
not a problem and nothing fails because of it — the file says what we build
against, not what you must run. Bump it on trunk when the newer version is the
one worth testing against, and the test will tell you which pages to bring
with it.

## How changes are made

- **Trunk-based.** Small changes straight to `main` where they are safe;
  short-lived branches where they are not. Long-running branches are not used.
- **Trunk is always green.** `./task check` passes before anything is pushed.
- **Conventional commits.** `feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
  `chore:`, `build:`, `ci:`, `perf:` or `revert:`, with an optional scope, and
  a body that says *why* rather than restating *what*. CI checks the subject of
  every commit a push or pull request adds.

## Cutting a release

The version lives in `plugin/.claude-plugin/plugin.json`, and every other file
that repeats it is checked against that one on every commit. `./task
release-check` is the leg that does it, so a half-finished bump fails
immediately rather than during a release.

1. Bump the version in `plugin/.claude-plugin/plugin.json` and `npm/package.json`.
2. Write the section for it in `CHANGELOG.md`. The release notes are that
   section, verbatim — they are written by a person and reviewed like anything
   else, not generated from commit subjects.
3. `./task check`, then `./task release-snapshot` to see exactly what the
   release will publish, in `dist/`.
4. Commit the bump on a branch named for the tag, cut from trunk, and push the
   branch and the tag together. Do not push trunk yet:

   ```console
   $ git switch -c release/v0.1.0 origin/main
   $ git commit -am "chore(release): v0.1.0"
   $ git tag v0.1.0
   $ git push origin release/v0.1.0 v0.1.0
   ```

   `release.yml` builds a tag whose commit is on trunk, or exactly one commit
   on top of trunk on `release/<tag>`, counting every commit a merge brings;
   any other it refuses before it builds.

5. When the release is published, pin its checksums in the plugin, which checks
   every binary it fetches against them. Check that the pins name the five
   archives of this release before you commit them:

   ```console
   $ gh release download v0.1.0 --repo bbsnly/sdlc --pattern checksums.txt --output plugin/checksums.txt --clobber
   $ grep -cE '_0\.1\.0_[a-z0-9_]+\.(tar\.gz|zip)$' plugin/checksums.txt
   5
   $ ./task check
   $ git commit -m "chore(plugin): pin the checksums of v0.1.0" -- plugin/checksums.txt
   ```

   `gh attestation verify <archive> --repo bbsnly/sdlc` on a downloaded archive
   checks the values came from this repository's build.

6. Put both commits on trunk together, then remove the branch:

   ```console
   $ git switch main && git merge --ff-only release/v0.1.0
   $ git push origin main
   $ git push origin --delete release/v0.1.0
   ```

   If trunk moved meanwhile, merge it into the branch (`git merge origin/main`)
   rather than rebasing. A rebase gives the release commit a new name, and the
   tag would point at a commit trunk never has. Cut a release while no story is
   under way, as before: a story started on trunk sees trunk move (SDLC-E0046).

   The order is the point. The marketplace serves the plugin from trunk, and
   Claude Code keeps the files of a plugin version until the version changes.
   Trunk pushed with the release commit and without its pins would serve that
   version with the previous release's pins to everyone who picked it up then,
   and its launcher would refuse to fetch, and say so, every session until the
   next release. The failure is closed, never an unchecked binary, but it lasts
   a release.

   `published.yml` ran when the release finished, against trunk's installers
   from before this push; where the release changed `install.sh` or
   `install.ps1`, run it again from the Actions tab.

The tag starts `.github/workflows/release.yml`, which runs the full check
matrix on the tagged commit before it builds anything, then publishes the
archives, their checksums, an SBOM and a build provenance attestation, and
finally `@bbsnly/sdlc` to npm. A pre-release tag (`v0.2.0-rc.1`) is marked as a
pre-release on GitHub and published to npm under `next` rather than `latest`.

The npm job does not publish on its own. It stages the package through npm's
trusted publishing, with no token stored anywhere, and a maintainer makes it
public with two-factor authentication: **Approve** under the package's Staged
Packages tab on npmjs.com, or `npm stage approve <stage-id>`. The run's summary
names the stage. Until it is approved, `npx @bbsnly/sdlc` still installs the
previous release.

The npm job runs after the release is open. If it fails before staging, the
GitHub release is already out and complete: fix the cause and use **Re-run
failed jobs** on the run, and only the `npm` job runs again.

`.github/workflows/published.yml` then installs what was published, on every
platform a release ships: `install.sh` piped from curl, `install.ps1` piped
into PowerShell 7 and Windows PowerShell 5.1, `npx @bbsnly/sdlc install`,
`go install`, and the archive downloaded and checked by hand. Every installed
binary has to report the published version. It runs after each release, every
day, and from the Actions tab — a red run means someone following the
documentation cannot install sdlc.

Things the workflow cannot do for itself:

- **The trusted publisher** is configured by hand on npmjs.com, under the
  package's settings: repository `bbsnly/sdlc`, workflow `release.yml`,
  allowed to stage a publish.
- **Immutable releases** are enabled by hand in Settings → General. There is no
  API for it, and it is what stops a published tag being quietly replaced.

## What is not in this repository

Design records, planning documents and the specification of the legacy shell kit
this project replaces are kept outside version control on purpose. What is here
is the product and what a person using it needs to understand it. If you want to
know why something is shaped the way it is and the code does not tell you, open
an issue and ask — the answer belongs in the documentation, which is where it
will go.
