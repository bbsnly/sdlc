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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Dir is the directory, relative to the repository root, that holds every file
// the loop owns.
const Dir = ".sdlc"

// File is the configuration file, relative to the repository root.
const File = Dir + "/config.json"

// FormatVersion is the version of the configuration format this release reads
// and writes.
const FormatVersion = 1

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
// a real loosening: it lets the test author add a test written after the code,
// which `sdlc freeze` then adds to the freeze. The implementer never writes one.
type Freeze struct {
	AllowNewTestFiles bool `json:"allow_new_test_files"`
}

// HumanGates names the points where a person, not the assistant, decides.
type HumanGates struct {
	PreCommitPauseTiers []string `json:"pre_commit_pause_tiers"`
	DoRAdvocateCheck    bool     `json:"dor_advocate_check"`
}

// DefaultRiskTier is the risk tier of a story that does not name one.
const DefaultRiskTier = "low"

// PausesBeforeCommit reports whether a story of this risk tier waits for a
// person's approval before it is committed. Tiers are words a person typed into
// two different files, so they compare without regard to case.
func (h HumanGates) PausesBeforeCommit(tier string) bool {
	tier = strings.TrimSpace(tier)
	if tier == "" {
		tier = DefaultRiskTier
	}
	for _, t := range h.PreCommitPauseTiers {
		if strings.EqualFold(strings.TrimSpace(t), tier) {
			return true
		}
	}
	return false
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
		Version:    FormatVersion,
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
	ceilings := ceilingEntries(os.Getenv)
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
		if i := slices.IndexFunc(ceilings, func(c ceiling) bool { return c.dir == parent }); i >= 0 {
			// The fix names the entry as it is written in the variable, which
			// is where it has to be taken out, not the directory it resolves to.
			return "", sdlcerr.New(sdlcerr.NotAGitRepo,
				"this is not a Git repository",
				"sdlc looked for .git in "+start+" and above it, as far as "+parent+
					", which GIT_CEILING_DIRECTORIES says not to look in").
				WithFix("take " + ceilings[i].written + " out of GIT_CEILING_DIRECTORIES, or unset it: this " +
					"directory may be inside a repository that the ceiling hides, and \"git init\" would start " +
					"another in it")
		}
		dir = parent
	}
}

// CeilingDirectories is GIT_CEILING_DIRECTORIES as git reads it: absolute
// paths, separated as PATH is, that the search for a repository does not climb
// into. An empty entry says the entries after it are not symlinks. Git stopped
// there while this went on up, and a repository git would not use was the one
// sdlc worked in.
func CeilingDirectories(getenv func(string) string) []string {
	entries := ceilingEntries(getenv)
	out := make([]string, 0, len(entries))
	for _, c := range entries {
		out = append(out, c.dir)
	}
	return out
}

// ceiling is one entry of GIT_CEILING_DIRECTORIES: as it is written, and the
// directory git takes it to name.
type ceiling struct{ written, dir string }

func ceilingEntries(getenv func(string) string) []ceiling {
	var out []ceiling
	resolve := true
	for _, entry := range filepath.SplitList(getenv("GIT_CEILING_DIRECTORIES")) {
		if entry == "" {
			resolve = false
			continue
		}
		dir := filepath.Clean(entry)
		if resolve {
			if resolved, err := filepath.EvalSymlinks(dir); err == nil {
				dir = resolved
			}
		}
		out = append(out, ceiling{written: entry, dir: dir})
	}
	return out
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
	// Windows PowerShell 5 saves a file with a byte order mark, and the file is
	// otherwise fine.
	raw = bytes.TrimPrefix(raw, []byte("\ufeff"))
	cfg := Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		// A setting of the wrong type is valid JSON, and a JSON checker finds
		// nothing wrong with it, so the message names the setting instead.
		var typ *json.UnmarshalTypeError
		if errors.As(err, &typ) && typ.Field != "" {
			return Config{}, sdlcerr.New(sdlcerr.ConfigUnreadable,
				File+" has a setting of the wrong type",
				typ.Field+" should be "+kindOf(typ.Type)+", not "+valueOf(typ.Value)+
					", at "+position(raw, typ.Offset)).WithCause(err)
		}
		return Config{}, sdlcerr.New(sdlcerr.ConfigUnreadable,
			File+" is not valid JSON",
			describeJSONError(raw, err)).WithCause(err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate refuses the settings the loop cannot honour, every one of them in
// one message. Each was accepted once and quietly did something else: a file
// from a newer release had what this one did not know dropped, a backlog
// outside the repository was neither committed with the work nor protected
// while a story ran, and a negative limit was a limit turned off.
func (c Config) validate() error {
	var problems []string
	switch {
	case c.Version > FormatVersion:
		problems = append(problems, fmt.Sprintf("version is %d, and this sdlc reads version %d: "+
			"a newer sdlc wrote the file, so upgrade sdlc", c.Version, FormatVersion))
	case c.Version < 1:
		problems = append(problems, fmt.Sprintf("version is %d, and the format is version %d",
			c.Version, FormatVersion))
	}
	if p := c.Backlog.Path; p != "" && !filepath.IsLocal(filepath.FromSlash(p)) {
		problems = append(problems, fmt.Sprintf("backlog.path is %q, which leaves the repository: "+
			"give it relative to the repository root", p))
	}
	for _, n := range []struct {
		name  string
		value float64
	}{
		{"thresholds.diff_size_cap", float64(c.Thresholds.DiffSizeCap)},
		{"thresholds.coverage_min", c.Thresholds.CoverageMin},
		{"thresholds.mutation_min", c.Thresholds.MutationMin},
		{"loop.max_review_rounds", float64(c.Loop.MaxReviewRounds)},
		{"loop.max_rework_rounds", float64(c.Loop.MaxReworkRounds)},
		{"loop.max_stop_blocks", float64(c.Loop.MaxStopBlocks)},
		{"budget.per_story_usd", c.Budget.PerStoryUSD},
	} {
		if n.value < 0 {
			problems = append(problems, fmt.Sprintf("%s is %v, and 0 is how it is turned off", n.name, n.value))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return sdlcerr.New(sdlcerr.ConfigInvalid,
		File+" has settings sdlc cannot use",
		strings.Join(problems, "; "))
}

// UnknownKeys names every setting in root/.sdlc/config.json that the
// configuration does not have, dotted as the documentation spells them. A
// misspelled setting is read as no setting at all and its default applies,
// with nothing said. A key starting with _ is a comment, and `commands` takes
// any name. A file that does not load names nothing: Load says why.
func UnknownKeys(root string) []string {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(File)))
	if err != nil {
		return nil
	}
	var doc any
	if json.Unmarshal(bytes.TrimPrefix(raw, []byte("\ufeff")), &doc) != nil {
		return nil
	}
	var unknown []string
	unknownIn(doc, reflect.TypeFor[Config](), "", &unknown)
	sort.Strings(unknown)
	return unknown
}

func unknownIn(v any, t reflect.Type, prefix string, unknown *[]string) {
	object, ok := v.(map[string]any)
	if !ok || t.Kind() != reflect.Struct {
		return
	}
	for key, value := range object {
		if strings.HasPrefix(key, "_") {
			continue
		}
		// encoding/json matches a field's name without regard to case, so a
		// key it reads is not one to report.
		field, found := t.FieldByNameFunc(func(name string) bool {
			f, _ := t.FieldByName(name)
			tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			return strings.EqualFold(tag, key)
		})
		if !found {
			*unknown = append(*unknown, prefix+key)
			continue
		}
		unknownIn(value, field.Type, prefix+key+".", unknown)
	}
}

// kindOf says what a setting takes, in the words of JSON rather than Go.
func kindOf(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "a whole number"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "true or false"
	case reflect.Slice, reflect.Array:
		return "a list"
	default:
		return "an object"
	}
}

// valueOf says what was found instead. encoding/json names a number that does
// not fit with the number itself, "number 1.5".
func valueOf(v string) string {
	kind, number, _ := strings.Cut(v, " ")
	switch kind {
	case "number":
		if number != "" {
			return number
		}
		return "a number"
	case "string":
		return "a string"
	case "bool":
		return "true or false"
	case "array":
		return "a list"
	case "object":
		return "an object"
	default:
		return v
	}
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
	return "the JSON stops making sense at " + position(raw, offset)
}

// position is a byte offset as a line and column.
func position(raw []byte, offset int64) string {
	offset = min(max(offset, 0), int64(len(raw)))
	before := string(raw[:offset])
	line := strings.Count(before, "\n") + 1
	col := offset - int64(strings.LastIndex(before, "\n"))
	return "line " + strconv.Itoa(line) + ", column " + strconv.FormatInt(col, 10)
}
