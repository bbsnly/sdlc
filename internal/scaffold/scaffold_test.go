package scaffold

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func touch(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// ------------------------------------------------------------------ detection

func TestDetectGo(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "go.mod", "module example.com/x\n")

	s, others := Detect(root)
	if s.Name != "Go" {
		t.Fatalf("stack = %q", s.Name)
	}
	if s.Commands["test"] != "go test ./... -count=1" {
		t.Errorf("test command = %q", s.Commands["test"])
	}
	if len(others) != 0 {
		t.Errorf("also seen = %v", others)
	}
}

// A Go service with a package.json for its front-end tooling is a Go project,
// but the user should be told the other one is there.
func TestDetectPrefersGoAndSaysWhatElseItSaw(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "go.mod", "module example.com/x\n")
	touch(t, root, "package.json", `{"scripts":{"build":"vite build"}}`)

	s, others := Detect(root)
	if s.Name != "Go" {
		t.Errorf("stack = %q, want Go", s.Name)
	}
	if !slices.Contains(others, "Node") {
		t.Errorf("also seen = %v, want it to mention Node", others)
	}
}

// Wiring a command to a script that does not exist would fail every gate on a
// project that is otherwise fine.
func TestDetectNodeOnlyWiresScriptsThatExist(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "package.json", `{"scripts":{"test":"vitest run"},"devDependencies":{"prettier":"^3"}}`)

	s, _ := Detect(root)
	if s.Name != "Node" {
		t.Fatalf("stack = %q", s.Name)
	}
	if s.Commands["test"] == "" {
		t.Error("test script exists but no test command was wired")
	}
	if s.Commands["build"] != "" {
		t.Errorf("build = %q, but package.json has no build script", s.Commands["build"])
	}
	if s.Commands["lint"] != "" {
		t.Errorf("lint = %q, but package.json has no lint script", s.Commands["lint"])
	}
	if !strings.Contains(s.Commands["fmt_check"], "prettier") {
		t.Errorf("fmt_check = %q, want prettier: it is in devDependencies", s.Commands["fmt_check"])
	}
}

// A package.json that does not parse is still a Node project.
func TestDetectNodeSurvivesABrokenPackageJSON(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "package.json", `{"scripts":`)

	s, _ := Detect(root)
	if s.Name != "Node" {
		t.Errorf("stack = %q, want Node", s.Name)
	}
	if len(s.Commands) != 0 {
		t.Errorf("commands = %v, want none: nothing could be read", s.Commands)
	}
}

func TestDetectPythonAndRust(t *testing.T) {
	for file, want := range map[string]string{
		"pyproject.toml":   "Python",
		"setup.py":         "Python",
		"requirements.txt": "Python",
		"Cargo.toml":       "Rust",
	} {
		root := t.TempDir()
		touch(t, root, file, "")
		if s, _ := Detect(root); s.Name != want {
			t.Errorf("%s detected as %q, want %q", file, s.Name, want)
		}
	}
}

func TestDetectUnknownProject(t *testing.T) {
	s, others := Detect(t.TempDir())
	if s.Name != "" {
		t.Errorf("stack = %q, want none", s.Name)
	}
	if len(others) != 0 {
		t.Errorf("also seen = %v", others)
	}
	if len(s.Commands) != 0 {
		t.Errorf("commands = %v, want none to guess at", s.Commands)
	}
}

func TestStackNamesListsWhatIsTried(t *testing.T) {
	got := StackNames()
	for _, want := range []string{"Go", "Rust", "Node", "Python"} {
		if !slices.Contains(got, want) {
			t.Errorf("StackNames = %v, missing %s", got, want)
		}
	}
}

// ------------------------------------------------------------------ rendering

// The template and the Go type are two descriptions of the same file. This is
// what catches them drifting apart.
func TestRenderedConfigMatchesTheDefaultsForEveryStack(t *testing.T) {
	for _, stack := range []Stack{unknownStack(), goStack(t), nodeStack(t)} {
		raw, err := renderConfig(stack)
		if err != nil {
			t.Fatalf("%s: %v", stack.Name, err)
		}
		if !json.Valid(raw) {
			t.Fatalf("%s: rendered config is not valid JSON:\n%s", stack.Name, raw)
		}

		cfg := config.Default()
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("%s: %v", stack.Name, err)
		}

		want := config.Default()
		if cfg.Version != want.Version {
			t.Errorf("%s: version = %d, want %d", stack.Name, cfg.Version, want.Version)
		}
		if cfg.Backlog.Path != want.Backlog.Path {
			t.Errorf("%s: backlog.path = %q", stack.Name, cfg.Backlog.Path)
		}
		if cfg.Thresholds != want.Thresholds {
			t.Errorf("%s: thresholds = %+v, want %+v", stack.Name, cfg.Thresholds, want.Thresholds)
		}
		if cfg.Loop != want.Loop {
			t.Errorf("%s: loop = %+v, want %+v", stack.Name, cfg.Loop, want.Loop)
		}
		if cfg.Budget.PerStoryUSD != want.Budget.PerStoryUSD {
			t.Errorf("%s: budget = %+v", stack.Name, cfg.Budget)
		}
		for name, command := range stack.Commands {
			if cfg.Commands[name] != command {
				t.Errorf("%s: commands.%s = %q, want %q", stack.Name, name, cfg.Commands[name], command)
			}
		}
	}
}

// A command with a quote in it has to survive being written into JSON.
func TestRenderedConfigEscapesShellQuoting(t *testing.T) {
	raw, err := renderConfig(goStack(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("a Go project's fmt_check broke the JSON: %v\n%s", err, raw)
	}
	if got := cfg.Commands["fmt_check"]; got != `test -z "$(gofmt -l .)"` {
		t.Errorf("fmt_check = %q", got)
	}
}

func goStack(t *testing.T) Stack {
	t.Helper()
	root := t.TempDir()
	touch(t, root, "go.mod", "module example.com/x\n")
	s, _ := Detect(root)
	return s
}

func nodeStack(t *testing.T) Stack {
	t.Helper()
	root := t.TempDir()
	touch(t, root, "package.json", `{"scripts":{"build":"tsc","test":"vitest run","lint":"eslint ."}}`)
	s, _ := Detect(root)
	return s
}

// ------------------------------------------------------------------ init

func TestInitWritesEverythingAProjectNeeds(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "go.mod", "module example.com/x\n")

	res, err := Init(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stack != "Go" {
		t.Errorf("stack = %q", res.Stack)
	}
	for _, rel := range []string{config.File, schemaPath, "user_stories.json"} {
		if !slices.Contains(res.Created, rel) {
			t.Errorf("Created = %v, missing %s", res.Created, rel)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s was reported as created but is not there", rel)
		}
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("the config it just wrote does not load: %v", err)
	}
	if cfg.Commands["test"] == "" {
		t.Error("a Go project got no test command")
	}

	var backlog model.Backlog
	if err := json.Unmarshal([]byte(read(t, root, "user_stories.json")), &backlog); err != nil {
		t.Fatalf("the backlog it just wrote does not parse: %v", err)
	}
	if len(backlog.Stories) != 1 || backlog.Stories[0].Status != model.StatusReady {
		t.Errorf("backlog = %+v, want one ready example story", backlog.Stories)
	}
	if !strings.Contains(read(t, root, claudeMDPath), ContractHeading) {
		t.Error("CLAUDE.md has no contract section, so the project does not take part")
	}
}

// The loop's state belongs to the project, and what a project commits is the
// project's decision.
func TestInitWritesNoGitignoreOpinion(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".gitignore", config.Dir + "/.gitignore"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("init wrote %s; what to ignore is the user's decision", rel)
		}
	}
}

func TestInitRefusesToOverwriteAnExistingSetup(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}

	_, err := Init(root, false)
	if err == nil {
		t.Fatal("a second init overwrote the first")
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.AlreadyInitialised {
		t.Errorf("err = %v, want %s", err, sdlcerr.AlreadyInitialised)
	}
}

// --force replaces the settings, which are ours, and never the backlog, which
// is the user's.
func TestInitForceKeepsTheUsersOwnFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	touch(t, root, "user_stories.json", `{"stories":[{"id":"MINE-1","title":"Mine","status":"ready"}]}`)
	touch(t, root, config.File, `{"version":1,"thresholds":{"diff_size_cap":1}}`)

	res, err := Init(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Kept, "user_stories.json") {
		t.Errorf("Kept = %v, want the backlog", res.Kept)
	}
	if !strings.Contains(read(t, root, "user_stories.json"), "MINE-1") {
		t.Error("--force overwrote the user's stories")
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Thresholds.DiffSizeCap != config.Default().Thresholds.DiffSizeCap {
		t.Error("--force did not restore the default settings")
	}
}

func TestInitAppendsToAnExistingClaudeMD(t *testing.T) {
	root := t.TempDir()
	touch(t, root, claudeMDPath, "# My project\n\nSome existing instructions.\n")

	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	got := read(t, root, claudeMDPath)
	if !strings.Contains(got, "Some existing instructions.") {
		t.Error("init replaced the user's CLAUDE.md instead of appending to it")
	}
	if !strings.Contains(got, ContractHeading) {
		t.Error("the contract section was not appended")
	}
}

func TestInitAppendsANewlineWhenTheFileLacksOne(t *testing.T) {
	root := t.TempDir()
	touch(t, root, claudeMDPath, "# My project\n\nNo trailing newline.")

	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, root, claudeMDPath), "newline.## SDLC") {
		t.Error("the contract ran onto the end of the last line")
	}
}

func TestInitLeavesAClaudeMDThatAlreadyHasTheContract(t *testing.T) {
	root := t.TempDir()
	touch(t, root, claudeMDPath, "# Mine\n\n"+ContractHeading+"\n\n- I filled this in myself.\n")

	res, err := Init(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Kept, claudeMDPath) {
		t.Errorf("Kept = %v, want CLAUDE.md", res.Kept)
	}
	if !strings.Contains(read(t, root, claudeMDPath), "I filled this in myself.") {
		t.Error("a filled-in contract was overwritten")
	}
}
