package sdlcerr

import "fmt"

// The catalogue. Every failure a user can read is registered here, once.
//
// Codes are API. Once published, a code's meaning does not change, and a code is
// never reused for a second condition. To retire one, delete its entry here and
// mark its section in docs/troubleshooting.md with a leading **Retired.** — that
// page is the permanent registry, so a reader who finds the code in an old log
// still lands on what it meant. A test asserts the three halves of that rule:
// every live code has a heading, every heading is live or marked retired, and no
// live code is marked retired.
var (
	NotAGitRepo = register("SDLC-E0001",
		`run "git init" first, or change to a directory inside your repository`)

	NotInitialised = register("SDLC-E0002",
		`run "sdlc init" in the root of your repository`)

	AlreadyInitialised = register("SDLC-E0003",
		`edit .sdlc/config.json directly, or run "sdlc init --force" to overwrite it`)

	ConfigUnreadable = register("SDLC-E0004",
		`fix the JSON in .sdlc/config.json, or delete it and run "sdlc init" again`)

	StateUnreadable = register("SDLC-E0005",
		`run "sdlc doctor", which names every file it cannot read; restore a committed one from git, or fix its permissions — and if sdlc left it that way, open an issue at https://github.com/bbsnly/sdlc/issues`)

	StateUnwritable = register("SDLC-E0006",
		`check that you can write to .sdlc/ and that the disk is not full`)

	BacklogMissing = register("SDLC-E0007",
		`run "sdlc init" to create the backlog, or point backlog.path in .sdlc/config.json at the file you use`)

	BacklogUnreadable = register("SDLC-E0008",
		`fix the JSON in your backlog file — every story needs an "id", a "title" and a "status" from the schema`)

	StoryNotFound = register("SDLC-E0009",
		`run "sdlc story list" to see the ids you can use`)

	NoRunnableStory = register("SDLC-E0010",
		`add the next story, or unblock one — "sdlc story list" shows each story's status and what any blocked one is waiting on`)

	NoActiveIteration = register("SDLC-E0011",
		`run "sdlc start" to begin an iteration on the next story`)

	IterationAlreadyActive = register("SDLC-E0012",
		`finish the current story, or run "sdlc stop" to end the iteration without recording a result`)

	StoryAlreadyFinished = register("SDLC-E0033",
		`record a gate as failed to reopen it — "sdlc gate code_review fail --story ID --note ..." says on the record why the work came back`)

	BadArgument = register("SDLC-E0034",
		`check the flag's value — "sdlc <command> --help" says what it takes`)

	NothingToApprove = register("SDLC-E0035",
		`only a story handed to a person with "sdlc escalate" waits for a decision — "sdlc status" shows which stories are waiting`)

	AwaitingPerson = register("SDLC-E0036",
		`a person decides first, in their own terminal: "sdlc approve <ID>", or "sdlc approve <ID> --reject ..." to send the work back`)

	ApprovalRequired = register("SDLC-E0037",
		`hand it to a person with "sdlc escalate pre_commit_approval --message ..." and stop; once they have run "sdlc approve", commit the work they approved`)

	NotOnTrunk = register("SDLC-E0038",
		`switch to the branch named by git.trunk_branch in .sdlc/config.json, then start again`)

	UncommittedWork = register("SDLC-E0039",
		`commit it, stash it, or discard it, then start again — the loop's own files under .sdlc/ and the backlog do not count`)

	TrunkBehind = register("SDLC-E0040",
		`bring trunk up to date — "git pull --rebase" — then start again`)

	TrunkBroken = register("SDLC-E0041",
		`fix trunk, or revert what broke it, then start again — commands.smoke in .sdlc/config.json is the check that failed`)

	DiffTooLarge = register("SDLC-E0042",
		`record the gate as failed and hand the story to a person to split — "sdlc escalate story_too_large --message ..." — rather than cutting the change down to fit`)

	UnfrozenTests = register("SDLC-E0043",
		`remove the test files the freeze does not hold, or have a person run "sdlc unfreeze --reason ..." in their own terminal and freeze again, so they are frozen with the rest`)

	NoAcceptanceCriteria = register("SDLC-E0044",
		`write the story's acceptance_criteria in the backlog, each an observable behaviour — or record "sdlc gate dor fail" and hand the question to a person with "sdlc escalate spec_unclear --message ..."`)

	UnknownGate = register("SDLC-E0013",
		`run "sdlc gate --help" to see the gate names this version knows`)

	UnknownGateStatus = register("SDLC-E0014",
		`use one of: pass, fail, pending`)

	UnknownArtifact = register("SDLC-E0016",
		`run "sdlc artifact list" to see what this version can store`)

	EmptyArtifact = register("SDLC-E0017",
		`pipe the document in, or pass --file with a path that has something in it`)

	ArtifactUnreadable = register("SDLC-E0018",
		`check the path given to --file, or pipe the document in instead`)

	AlreadyFrozen = register("SDLC-E0022",
		`if the tests really have to change, run "sdlc unfreeze --reason ..." in your own terminal first`)

	NotFrozen = register("SDLC-E0023",
		`run "sdlc freeze" once the acceptance tests are written and failing`)

	NoTestsFound = register("SDLC-E0024",
		`write the acceptance tests first, or widen paths.tests in .sdlc/config.json so it finds them`)

	FreezeBroken = register("SDLC-E0025",
		`restore the frozen tests, or have a person run "sdlc unfreeze --reason ..." in their own terminal and freeze again, so the change is on the record`)

	ReasonRequired = register("SDLC-E0026",
		`pass --reason with the one line that explains why the tests have to change`)

	UnknownReviewer = register("SDLC-E0027",
		`run "sdlc review list" to see which reviews this gate expects`)

	UnknownVerdict = register("SDLC-E0028",
		`use one of: approve, block, note`)

	ReviewsMissing = register("SDLC-E0029",
		`delegate to the reviewers this gate expects -- "sdlc review list" shows who is outstanding`)

	ReviewBlocks = register("SDLC-E0030",
		`fix what the reviewer found, then have the same reviewer look again`)

	GateOutOfOrder = register("SDLC-E0031",
		`record the earlier gate first -- "sdlc status" shows where this story stands`)

	TreeNotCommitted = register("SDLC-E0032",
		`commit the work, or stash what does not belong to this story by path with "git stash push -- <path>", then record the gate`)

	RepositoryUnreadable = register("SDLC-E0021",
		`check that git is installed and that this directory is a repository git can read`)

	GateDocumentsMissing = register("SDLC-E0020",
		`store the gate's documents with "sdlc artifact write", then record the gate`)

	ArtifactTooLarge = register("SDLC-E0019",
		`store the document itself and link to the bulk from it — a gate's document is read by a person`)

	UnsafeStoryID = register("SDLC-E0015",
		`rename the story so its id is letters, digits, dots, dashes and underscores -- for example AUTH-3`)

	ConfigInvalid = register("SDLC-E0045",
		`change the settings the message names in .sdlc/config.json — docs/configuration.md says what each one takes; for a newer version, upgrade sdlc`)

	TrunkMoved = register("SDLC-E0046",
		`record the gate as failed, hand the story to a person with "sdlc escalate", and stop; a person keeps the commits or reverts them, and answers with "sdlc approve", after which "sdlc start" picks the story up from where trunk is then`)

	AcknowledgedUnknown = register("SDLC-E0047",
		`run "sdlc ack --through <commit>" in your own terminal to acknowledge a commit git has, or remove .sdlc/state/acknowledged to list every story; meanwhile "sdlc log --since <commit>" starts after a commit you name, and "sdlc log --all" lists every story`)

	SessionElsewhere = register("SDLC-E0048",
		`a person takes the story over by typing /sdlc:next in this Claude Code session, or runs the command in their own terminal, outside Claude Code; an assistant stops and tells the person that the story is being worked on in another session and only they can move it here, and does not run "sdlc start"`)
)

// entry is one row of the catalogue.
type entry struct {
	code Code
	fix  string
}

// catalogue holds every registered code in registration order.
var catalogue []entry

// byID guards against two conditions sharing a code, at init time rather than
// at test time, so a duplicate cannot reach a build that skipped the tests.
var byID = map[string]int{}

func register(id, fix string) Code {
	if _, dup := byID[id]; dup {
		panic(fmt.Sprintf("sdlcerr: %s is registered twice — codes are API and are never reused", id))
	}
	if fix == "" {
		panic(fmt.Sprintf("sdlcerr: %s has no fix — an error without a fix is not finished", id))
	}
	byID[id] = len(catalogue)
	catalogue = append(catalogue, entry{code: Code{id: id}, fix: fix})
	return Code{id: id}
}

func fixFor(c Code) string {
	i, ok := byID[c.id]
	if !ok {
		// Unreachable: Code cannot be constructed outside this package.
		return `this is a bug — please open an issue at https://github.com/bbsnly/sdlc/issues`
	}
	return catalogue[i].fix
}
