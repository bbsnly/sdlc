// Package config finds the repository the loop is running in and reads the
// contract it works to.
//
// Two files make a project participate: .sdlc/config.json, which this package
// reads, and a "## SDLC Contract" section in the project's CLAUDE.md, which the
// assistant reads. Everything else about the loop is the same in every project,
// which is what lets one tool serve a Go service and a Rails app without knowing
// anything about either.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Dir is the directory, relative to the repository root, that holds every file
// the loop owns.
const Dir = ".sdlc"

// File is the configuration file, relative to the repository root.
const File = Dir + "/config.json"

// Config is the machine-readable half of a project's contract. The human half
// lives in CLAUDE.md; nothing here describes a language or a framework, only
// the commands this project runs and the numbers it holds itself to.
type Config struct {
	Version    int               `json:"version"`
	Backlog    Backlog           `json:"backlog"`
	Git        Git               `json:"git"`
	Commands   map[string]string `json:"commands"`
	Thresholds Thresholds        `json:"thresholds"`
	Paths      Paths             `json:"paths"`
	Spec       Spec              `json:"spec"`
	Loop       Loop              `json:"loop"`
	Reviews    Reviews           `json:"reviews"`
	Freeze     Freeze            `json:"freeze"`
	HumanGates HumanGates        `json:"human_gates"`
	Budget     Budget            `json:"budget"`
}

// Backlog names the file the stories live in. It is deliberately a plain JSON
// file in the repository: a backlog that is not in version control cannot be
// reviewed, and cannot be recovered.
type Backlog struct {
	Path string `json:"path"`
}

// Git describes the branching the project uses. The loop is trunk-only by
// design, so this records which branch trunk is rather than offering a choice.
type Git struct {
	TrunkBranch string `json:"trunk_branch"`
	Remote      bool   `json:"remote"`
}

// Thresholds are the numbers a gate compares against. A zero means the gate
// that reads it is not enforced.
type Thresholds struct {
	DiffSizeCap int     `json:"diff_size_cap"`
	CoverageMin float64 `json:"coverage_min"`
	MutationMin float64 `json:"mutation_min"`
}

// Paths tells the loop which files are tests and which are source, so that it
// can freeze the first and measure the second without guessing.
type Paths struct {
	Tests TestPaths `json:"tests"`
	Src   []string  `json:"src"`
}

// TestPaths matches a test file by the directory it is in or by its name.
type TestPaths struct {
	Dirs      []string `json:"dirs"`
	FileGlobs []string `json:"file_globs"`
}

// Spec is where the project's own documentation lives, for the analysis gate to
// read before it writes anything.
type Spec struct {
	Paths []string `json:"paths"`
}

// Loop bounds the iteration so that a story cannot spin forever.
type Loop struct {
	MaxReviewRounds int `json:"max_review_rounds"`
	MaxReworkRounds int `json:"max_rework_rounds"`
	MaxStopBlocks   int `json:"max_stop_blocks"`
}

// Reviews configures whether the code-review gate can block.
type Reviews struct {
	Gate7Advisory bool `json:"gate7_advisory"`
}

// Freeze configures test freezing. Allowing new test files after the freeze is
// a real loosening: it lets an implementer add a test that passes.
type Freeze struct {
	AllowNewTestFiles bool `json:"allow_new_test_files"`
}

// HumanGates names the points where a person, not the assistant, decides.
type HumanGates struct {
	PreCommitPauseTiers []string `json:"pre_commit_pause_tiers"`
	DoRAdvocateCheck    bool     `json:"dor_advocate_check"`
}

// Budget caps what one story may cost and when to say so.
type Budget struct {
	PerStoryUSD    float64   `json:"per_story_usd"`
	AlertFractions []float64 `json:"alert_fractions"`
}

// Default is the configuration a project gets before it changes anything. It is
// also the base every load starts from, so a field left out of a hand-edited
// file keeps its documented value rather than becoming zero.
func Default() Config {
	return Config{
		Version:    1,
		Backlog:    Backlog{Path: "user_stories.json"},
		Git:        Git{TrunkBranch: "main"},
		Commands:   map[string]string{},
		Thresholds: Thresholds{DiffSizeCap: 500, CoverageMin: 80, MutationMin: 70},
		Paths: Paths{
			Tests: TestPaths{
				Dirs: []string{"tests/", "test/"},
				FileGlobs: []string{
					"*_test.go", "*.test.ts", "*.spec.ts",
					"test_*.py", "*_test.py", "*Test.java", "*_spec.rb",
				},
			},
			Src: []string{"src/", "internal/", "pkg/", "cmd/", "lib/"},
		},
		Spec:       Spec{Paths: []string{"docs/", "spec/", "README.md"}},
		Loop:       Loop{MaxReviewRounds: 2, MaxReworkRounds: 3, MaxStopBlocks: 3},
		Freeze:     Freeze{AllowNewTestFiles: false},
		HumanGates: HumanGates{PreCommitPauseTiers: []string{"high"}},
		Budget:     Budget{PerStoryUSD: 60, AlertFractions: []float64{0.5, 0.8, 1.0}},
	}
}

// BacklogPath is the backlog file as an absolute path under root.
func (c Config) BacklogPath(root string) string {
	p := c.Backlog.Path
	if p == "" {
		p = Default().Backlog.Path
	}
	return filepath.Join(root, filepath.FromSlash(p))
}

// FindRoot returns the root of the repository containing start.
//
// It walks up looking for .git rather than asking git, because the hooks call
// this on every tool use and a subprocess there costs more than the whole rest
// of the check. A worktree and a submodule both have .git as a file, so both
// are found the same way.
func FindRoot(start string) (string, error) {
	if wt := os.Getenv("GIT_WORK_TREE"); wt != "" {
		if abs, err := filepath.Abs(wt); err == nil {
			return abs, nil
		}
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", sdlcerr.New(sdlcerr.NotAGitRepo,
			"this is not a Git repository",
			"the current directory could not be resolved").WithCause(err)
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", sdlcerr.New(sdlcerr.NotAGitRepo,
				"this is not a Git repository",
				"sdlc looked for .git in "+start+" and in every directory above it")
		}
		dir = parent
	}
}

// Load reads root/.sdlc/config.json over the defaults.
func Load(root string) (Config, error) {
	path := filepath.Join(root, filepath.FromSlash(File))
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Config{}, sdlcerr.New(sdlcerr.NotInitialised,
			"this project has not been set up for the loop",
			"there is no "+File+" in "+root)
	case err != nil:
		return Config{}, sdlcerr.New(sdlcerr.ConfigUnreadable,
			File+" could not be read",
			"opening it failed").WithCause(err)
	}
	cfg := Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, sdlcerr.New(sdlcerr.ConfigUnreadable,
			File+" is not valid JSON",
			describeJSONError(raw, err)).WithCause(err)
	}
	return cfg, nil
}

// Project is a repository that takes part in the loop, already resolved.
type Project struct {
	Root   string
	Config Config
}

// Open finds the repository containing start and reads its configuration.
func Open(start string) (*Project, error) {
	root, err := FindRoot(start)
	if err != nil {
		return nil, err
	}
	cfg, err := Load(root)
	if err != nil {
		return nil, err
	}
	return &Project{Root: root, Config: cfg}, nil
}

// Path joins a repository-relative slash path onto the project root.
func (p *Project) Path(rel ...string) string {
	parts := append([]string{p.Root}, rel...)
	return filepath.Join(parts...)
}

// describeJSONError turns a byte offset into a line and column, because "invalid
// character '}' looking for beginning of object key string" is only useful once
// you know where to look.
func describeJSONError(raw []byte, err error) string {
	var offset int64 = -1
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		offset = syn.Offset
	case errors.As(err, &typ):
		offset = typ.Offset
	}
	if offset < 0 || offset > int64(len(raw)) {
		return err.Error()
	}
	before := string(raw[:offset])
	line := strings.Count(before, "\n") + 1
	col := offset - int64(strings.LastIndex(before, "\n"))
	return "the JSON stops making sense at line " + strconv.Itoa(line) +
		", column " + strconv.FormatInt(col, 10)
}
