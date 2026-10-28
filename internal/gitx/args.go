package gitx

// Arguments to git that more than one call spells. A constant here is the
// argument itself, byte for byte: renaming one is a change to what git is run
// with.
const (
	headsPrefix = "refs/heads/"
	argHead     = "HEAD"

	argRevParse         = "rev-parse"
	argUpdateRef        = "update-ref"
	argUpdateIndex      = "update-index"
	argIndexInfo        = "--index-info"
	argQuiet            = "--quiet"
	argVerify           = "--verify"
	argNoRenames        = "--no-renames"
	argFullIndex        = "--full-index"
	argIgnoreSubmodNone = "--ignore-submodules=none"
)

// Wording that more than one message shares, so that the messages stay alike.
const (
	msgTheCheckout       = "the checkout "
	msgWhichHas          = ", which has "
	msgCouldNotBeRead    = " could not be read"
	msgGitSaid           = "git said: "
	msgAPath             = "a path"
	msgCannotNameBranch  = " cannot name a branch"
	msgBranchNotAnOption = "no branch is empty or starts with a dash, and git would read one that does as an option"
	msgAlreadyExists     = " already exists"
	msgTheBranch         = "the branch "
)
