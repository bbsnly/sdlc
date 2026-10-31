package shellpolicy

// US-112 AC-17 and design review round 1 (security, B2): a hard link is a
// second name for the same file, and Lstat reads one as a regular file. The
// trace is appended in place, so a link planted at its name takes the hook's
// lines into whatever file it names, and a second name made for it lets that
// name be written as the trace. `ln`, `cp -l`, mklink and New-Item's HardLink
// are refused onto the gate record already; `link`, which takes exactly two
// names, and Windows' `fsutil hardlink create` were not. These rows are tried
// on the trace under its own rule and on the gate record under its rule.
//
// The Bash rows were run under bash and zsh with macOS's /bin/link: each made
// the second name, except the glob, which names a trace only once one exists
// (it is a row for the trace as the source). The cmd and PowerShell rows were
// not run, as no Windows was at hand.

import "testing"

// hardLinks is every row that makes a hard link onto, or of, the story file
// named name.
func hardLinks(name string) (bash, powerShell []string) {
	at := ".sdlc/stories/A-1/" + name
	return []string{
		// A link planted at the name.
		"link /tmp/victim " + at,
		"link -- /tmp/victim " + at,
		"/bin/link /tmp/victim " + at,
		"command link /tmp/victim " + at,
		"env link /tmp/victim " + at,
		`\link /tmp/victim ` + at,
		"'link' /tmp/victim " + at,
		`link /tmp/victim .sdlc/stories/A-1/"` + name + `"`,
		"cd .sdlc/stories/A-1 && link /tmp/victim " + name,
		"link /tmp/victim /home/dev/repo/" + at,
		// A second name made for it, written as the file afterwards.
		"link " + at + " /tmp/second",
		"link .sdlc/stories/*/" + name + " /tmp/second",
		// Windows' ways, from the Bash tool.
		`cmd /c mklink /H .sdlc\stories\A-1\` + name + ` C:\victim`,
		`fsutil hardlink create .sdlc\stories\A-1\` + name + ` C:\victim`,
		`cmd /c fsutil hardlink create .sdlc\stories\A-1\` + name + ` C:\victim`,
	}, []string{
		`cmd /c mklink /H .sdlc\stories\A-1\` + name + ` C:\victim`,
		`fsutil hardlink create .sdlc\stories\A-1\` + name + ` C:\victim`,
		`New-Item -ItemType HardLink -Path .sdlc\stories\A-1\` + name + ` -Target C:\victim`,
		`New-Item -ItemType HardLink -Path C:\second -Target .sdlc\stories\A-1\` + name,
	}
}

func TestAC17AHardLinkOntoOrOfTheTraceIsRefused(t *testing.T) {
	bash, powerShell := hardLinks("trace.jsonl")
	for _, command := range bash {
		refused(t, command, ready, traceRule)
	}
	for _, command := range powerShell {
		refused(t, command, State{CommitReady: true, PowerShell: true}, traceRule)
	}
}

// The scout rule: `link` and `fsutil hardlink` onto the gate record, which
// the store replaces by a rename, so a link there harms nothing today but is
// refused as `ln` is.
func TestAC17AHardLinkOntoOrOfTheGateRecordIsRefusedAsLnIs(t *testing.T) {
	bash, powerShell := hardLinks("gate-record.json")
	for _, command := range bash {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	for _, command := range powerShell {
		refused(t, command, State{CommitReady: true, PowerShell: true}, "loop-state-through-the-tool")
	}
}

// What `link` and fsutil make elsewhere, and reading the files, is not
// refused.
func TestAC17AHardLinkElsewhereIsLeftAlone(t *testing.T) {
	for _, command := range []string{
		"link /tmp/a /tmp/b",
		"link build/out.bin build/out-copy.bin",
		"fsutil hardlink list C:\\work\\a.txt",
		"cat .sdlc/stories/A-1/trace.jsonl",
		"ls -i .sdlc/stories/A-1/trace.jsonl",
	} {
		allowed(t, command, ready)
	}
}
