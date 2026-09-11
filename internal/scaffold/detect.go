package scaffold

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Stack is a guess at what kind of project this is, and the commands that go
// with it. The guess is written into the configuration and then checked by
// sdlc doctor, rather than being asked about up front: a question a tool can
// answer for itself is a question it should not ask.
type Stack struct {
	Name      string
	Commands  map[string]string
	TestDirs  []string
	TestGlobs []string
	SrcDirs   []string
}

// Detect works out what a repository is built with. It returns the stack it
// chose and the names of any others it also saw, so that a mixed repository is
// told about the guess rather than left to discover it.
func Detect(root string) (Stack, []string) {
	var matched []Stack
	for _, d := range detectors {
		if s, ok := d.detect(root); ok {
			matched = append(matched, s)
		}
	}
	if len(matched) == 0 {
		return unknownStack(), nil
	}
	var others []string
	for _, s := range matched[1:] {
		others = append(others, s.Name)
	}
	return matched[0], others
}

// detector recognises one kind of project.
type detector struct {
	name   string
	detect func(root string) (Stack, bool)
}

// detectors run in order, and the first match wins. The order is the one that
// is least often wrong in a mixed repository: a Go service with a package.json
// for its front-end tooling is a Go project.
var detectors = []detector{
	{"Go", detectGo},
	{"Rust", detectRust},
	{"Node", detectNode},
	{"Python", detectPython},
}

func exists(root, name string) bool {
	_, err := os.Stat(filepath.Join(root, name))
	return err == nil
}

func detectGo(root string) (Stack, bool) {
	if !exists(root, "go.mod") {
		return Stack{}, false
	}
	return Stack{
		Name: "Go",
		Commands: map[string]string{
			"smoke":     "go build ./... && go vet ./...",
			"build":     "go build ./...",
			"test":      "go test ./... -count=1",
			"lint":      "golangci-lint run",
			"fmt":       "gofmt -w .",
			"fmt_check": `test -z "$(gofmt -l .)"`,
			"fmt_file":  `gofmt -w "$FILE"`,
			"coverage": "go test ./... -count=1 -coverprofile=.sdlc/state/cover.out >/dev/null && " +
				`go tool cover -func=.sdlc/state/cover.out | tail -1 | grep -Eo '[0-9]+\.[0-9]+'`,
		},
		// testdata as well as the test files: a frozen test that reads a
		// golden file is only frozen if the golden file is too.
		TestDirs:  []string{"testdata/"},
		TestGlobs: []string{"*_test.go"},
		SrcDirs:   []string{"cmd/", "internal/", "pkg/"},
	}, true
}

func detectRust(root string) (Stack, bool) {
	if !exists(root, "Cargo.toml") {
		return Stack{}, false
	}
	return Stack{
		Name: "Rust",
		Commands: map[string]string{
			"smoke":     "cargo check --all-targets",
			"build":     "cargo build",
			"test":      "cargo test",
			"lint":      "cargo clippy --all-targets -- -D warnings",
			"fmt":       "cargo fmt",
			"fmt_check": "cargo fmt --check",
		},
		TestDirs:  []string{"tests/", "testdata/", "fixtures/"},
		TestGlobs: []string{"*_test.rs"},
		SrcDirs:   []string{"src/"},
	}, true
}

// packageJSON is the part of a package.json this needs: what the project can
// already do. Wiring a command to a script that does not exist would make every
// gate fail on a project that is otherwise fine.
type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	DevDependencies map[string]string `json:"devDependencies"`
	Dependencies    map[string]string `json:"dependencies"`
}

func detectNode(root string) (Stack, bool) {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return Stack{}, false
	}
	var pkg packageJSON
	// A package.json that does not parse is still a Node project; it just does
	// not tell us anything, so the commands stay empty for the user to fill in.
	_ = json.Unmarshal(raw, &pkg)

	hasScript := func(name string) bool { _, ok := pkg.Scripts[name]; return ok }
	hasDep := func(name string) bool {
		_, dev := pkg.DevDependencies[name]
		_, prod := pkg.Dependencies[name]
		return dev || prod
	}

	cmds := map[string]string{}
	if hasScript("build") {
		cmds["build"] = "npm run build --silent"
	}
	if hasScript("test") {
		cmds["test"] = "npm test --silent"
	}
	if hasScript("lint") {
		cmds["lint"] = "npm run lint --silent"
	}
	switch {
	case hasScript("format"):
		cmds["fmt"] = "npm run format --silent"
	case hasDep("prettier"):
		cmds["fmt"] = "npx prettier --write ."
		cmds["fmt_file"] = `npx prettier --write "$FILE"`
	}
	switch {
	case hasScript("format:check"):
		cmds["fmt_check"] = "npm run format:check --silent"
	case hasDep("prettier"):
		cmds["fmt_check"] = "npx prettier --check ."
	}
	if cmds["build"] != "" {
		cmds["smoke"] = cmds["build"]
	}

	return Stack{
		Name:     "Node",
		Commands: cmds,
		// Snapshots and mocks decide whether a test passes as much as the
		// test does, and `jest -u` rewrites a snapshot without being asked
		// twice.
		TestDirs:  []string{"tests/", "test/", "__tests__/", "__snapshots__/", "__mocks__/", "__fixtures__/"},
		TestGlobs: []string{"*.test.ts", "*.test.js", "*.spec.ts", "*.spec.js", "*.snap"},
		SrcDirs:   []string{"src/", "lib/"},
	}, true
}

func detectPython(root string) (Stack, bool) {
	if !exists(root, "pyproject.toml") && !exists(root, "setup.py") && !exists(root, "requirements.txt") {
		return Stack{}, false
	}
	return Stack{
		Name: "Python",
		Commands: map[string]string{
			"test":      "pytest -q",
			"lint":      "ruff check .",
			"fmt":       "ruff format .",
			"fmt_check": "ruff format --check .",
			"fmt_file":  `ruff format "$FILE"`,
			"coverage": "pytest -q --cov --cov-report=term | grep -E '^TOTAL' | " +
				"grep -Eo '[0-9]+%' | tr -d %",
		},
		// conftest.py is where pytest fixtures live, so a test's outcome can
		// be changed there without touching a test file.
		TestDirs:  []string{"tests/", "test/", "fixtures/", "testdata/"},
		TestGlobs: []string{"test_*.py", "*_test.py", "conftest.py"},
		SrcDirs:   []string{"src/"},
	}, true
}

func unknownStack() Stack {
	return Stack{
		Name:      "",
		Commands:  map[string]string{},
		TestDirs:  []string{"tests/", "test/", "testdata/", "fixtures/"},
		TestGlobs: []string{},
		SrcDirs:   []string{"src/"},
	}
}

// StackNames lists every stack this version can recognise, in the order they
// are tried, for a message that has to say what was looked for.
func StackNames() []string {
	names := make([]string, 0, len(detectors))
	for _, d := range detectors {
		names = append(names, d.name)
	}
	return names
}
