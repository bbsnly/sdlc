package shellpolicy

import "testing"

// An archiver or a copy to elsewhere writes only where it is told to: a
// snapshot of the project, with the careful exclude, was refused as a write to
// everything it read.
func TestACopyWritesOnlyWhereItGoes(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"tar --exclude=.git -cf - . | tar -xf - -C /tmp/snap",
		"tar --exclude .sdlc -czf /tmp/p.tgz .",
		"tar --exclude=x -cf /tmp/t.tar .sdlc",
		"tar -cf /tmp/t.tar internal/calc/add_test.go CLAUDE.md",
		"tar czf /tmp/t.tgz .sdlc",
		"tar -cf/tmp/x.tar .sdlc",
		"tar --create --file /tmp/t.tar .sdlc CLAUDE.md",
		"tar -xf /tmp/t.tar -C /tmp/out",
		"tar -xf /tmp/t.tar --exclude .sdlc -C /tmp/out",
		"tar -xf /tmp/t.tar --exclude-from .sdlc/state/active -C /tmp/out",
		"tar -x --files-from .sdlc/state/active -f /tmp/t.tar -C /tmp/out",
		"tar -x -X .sdlc/state/active -f /tmp/t.tar -C /tmp/out",
		"tar -x -T .sdlc/state/active -f /tmp/t.tar -C /tmp/out",
		"rsync -a --exclude .sdlc ./ /tmp/snap/",
		"rsync -a ./ /tmp/snap --exclude .sdlc",
		"rsync -a --exclude-from .sdlc/ignore . /tmp/snap",
		"rsync .sdlc/",
		"tar -xf",
		"scp CLAUDE.md host:/tmp/",
		"scp CLAUDE.md host:/tmp/ -i .sdlc/key",
	} {
		allowed(t, command, s)
	}

	for command, rule := range map[string]string{
		"tar -xf evil.tar -C .sdlc":                             "loop-state-through-the-tool",
		"tar -xf evil.tar --directory=.sdlc/state":              "loop-state-through-the-tool",
		"tar -xf evil.tar --directory .sdlc/state":              "loop-state-through-the-tool",
		"tar xf evil.tar -C .sdlc":                              "loop-state-through-the-tool",
		"tar xfC evil.tar .sdlc":                                "loop-state-through-the-tool",
		"tar -xC.sdlc -f evil.tar":                              "loop-state-through-the-tool",
		"tar --extract -f evil.tar .sdlc/state/active":          "loop-state-through-the-tool",
		"tar --get --file=evil.tar .sdlc/state/active":          "loop-state-through-the-tool",
		"tar -cf .sdlc/state/active src":                        "loop-state-through-the-tool",
		"tar -g .sdlc/state/active -cf /tmp/t.tar src":          "loop-state-through-the-tool",
		"tar --listed-incremental .sdlc/state/active -cf x src": "loop-state-through-the-tool",
		"tar --listed-incremental=.sdlc/state/active -cf x src": "loop-state-through-the-tool",
		"tar -cfCLAUDE.md src":                                  "protected-path-through-the-tool",
		"tar --create --file=CLAUDE.md src":                     "protected-path-through-the-tool",
		"tar --create --file CLAUDE.md src":                     "protected-path-through-the-tool",
		"tar -xf t.tar internal/calc/add_test.go":               "frozen-test-through-the-tool",
		"rsync -a /tmp/x/ .sdlc/":                               "loop-state-through-the-tool",
		"rsync -a /tmp/x .sdlc/ -v":                             "loop-state-through-the-tool",
		"rsync -a --exclude x /tmp/src .sdlc/state/active":      "loop-state-through-the-tool",
		"rsync --log-file=.sdlc/state/active a b":               "loop-state-through-the-tool",
		"rsync --log-file .sdlc/state/active a b":               "loop-state-through-the-tool",
		"rsync --write-batch=.sdlc/state/active a b":            "loop-state-through-the-tool",
		"rsync --only-write-batch=.sdlc/state/active a b":       "loop-state-through-the-tool",
		"rsync -a /tmp/add_test.go internal/calc/add_test.go":   "frozen-test-through-the-tool",
		"scp host:evil .sdlc/state/active":                      "loop-state-through-the-tool",
		"scp -P 22 host:evil CLAUDE.md":                         "protected-path-through-the-tool",
	} {
		refused(t, command, s, rule)
	}
}
