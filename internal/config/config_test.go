package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// repo makes a directory that looks like a Git repository, and returns the path
// with symlinks resolved so that comparisons hold on macOS, where the temporary
// directory is reached through /var.
func repo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_WORK_TREE", "")
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(File)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func codeOf(t *testing.T, err error) sdlcerr.Code {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("error is not an *sdlcerr.Error: %v", err)
	}
	return e.Code
}

func TestFindRootFromTheRootItself(t *testing.T) {
	root := repo(t)
	got, err := FindRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
}

func TestFindRootFromDeepInside(t *testing.T) {
	root := repo(t)
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(deep)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
}

// A worktree and a submodule both carry .git as a file rather than a directory.
func TestFindRootWhenGitIsAFile(t *testing.T) {
	t.Setenv("GIT_WORK_TREE", "")
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
}

func TestFindRootOutsideAnyRepository(t *testing.T) {
	t.Setenv("GIT_WORK_TREE", "")
	dir := t.TempDir()
	_, err := FindRoot(dir)
	if err == nil {
		t.Fatal("FindRoot succeeded outside a repository")
	}
	if got := codeOf(t, err); got != sdlcerr.NotAGitRepo {
		t.Errorf("code = %s, want %s", got, sdlcerr.NotAGitRepo)
	}
	if !strings.Contains(err.Error(), "not a Git repository") {
		t.Errorf("error does not say what happened: %v", err)
	}
}

// Git does not climb into a directory GIT_CEILING_DIRECTORIES names, and a
// repository above one is not the repository it works in. FindRoot went on up
// and found it: from a temp directory inside a repository, "outside any
// repository" was that repository.
func TestFindRootStopsWhereGitCeilingDirectoriesSays(t *testing.T) {
	root := repo(t)
	inside := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Join(root, "a"))
	var e *sdlcerr.Error
	if _, err := FindRoot(inside); err == nil {
		t.Error("FindRoot climbed into a ceiling directory to find the repository above it")
	} else if !errors.As(err, &e) || !strings.Contains(e.Why, "GIT_CEILING_DIRECTORIES") {
		t.Errorf("the error does not say where the search stopped: %+v", err)
	}

	// The directory the search starts in is looked in even when it is a
	// ceiling, as git does, and so is everything below the ceiling.
	t.Setenv("GIT_CEILING_DIRECTORIES", string(filepath.ListSeparator)+root)
	if got, err := FindRoot(root); err != nil || got != root {
		t.Errorf("from the ceiling itself: FindRoot = %q, %v; want %q", got, err, root)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	if got, err := FindRoot(inside); err != nil || got != root {
		t.Errorf("with the ceiling above the repository: FindRoot = %q, %v; want %q", got, err, root)
	}

	// A ceiling named through a link is the directory the link leads to, as it
	// is to git. The temp directory is named that way on macOS, under /var.
	link := filepath.Join(t.TempDir(), "ceiling")
	if err := os.Symlink(filepath.Join(root, "a"), link); err != nil {
		t.Skipf("this system cannot make a symlink: %v", err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", link)
	if _, err := FindRoot(inside); err == nil {
		t.Error("FindRoot climbed into a ceiling directory named through a symlink")
	}
}

func TestFindRootHonoursGitWorkTree(t *testing.T) {
	elsewhere := t.TempDir()
	t.Setenv("GIT_WORK_TREE", elsewhere)
	got, err := FindRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != elsewhere {
		t.Errorf("FindRoot = %q, want the GIT_WORK_TREE %q", got, elsewhere)
	}
}

func TestLoadWithoutAConfig(t *testing.T) {
	root := repo(t)
	_, err := Load(root)
	if err == nil {
		t.Fatal("Load succeeded with no config file")
	}
	if got := codeOf(t, err); got != sdlcerr.NotInitialised {
		t.Errorf("code = %s, want %s", got, sdlcerr.NotInitialised)
	}
}

// A hand-edited file that names two settings must not silently zero the rest.
func TestLoadKeepsDefaultsForAbsentFields(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"thresholds":{"diff_size_cap":40}}`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Thresholds.DiffSizeCap != 40 {
		t.Errorf("DiffSizeCap = %d, want the file's 40", cfg.Thresholds.DiffSizeCap)
	}
	if cfg.Thresholds.CoverageMin != Default().Thresholds.CoverageMin {
		t.Errorf("CoverageMin = %v, want the default %v",
			cfg.Thresholds.CoverageMin, Default().Thresholds.CoverageMin)
	}
	if cfg.Backlog.Path != Default().Backlog.Path {
		t.Errorf("Backlog.Path = %q, want the default %q", cfg.Backlog.Path, Default().Backlog.Path)
	}
	if len(cfg.Paths.Tests.FileGlobs) == 0 {
		t.Error("Paths.Tests.FileGlobs was emptied by a file that did not mention it")
	}
}

func TestLoadOverridesWhatTheFileNames(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"backlog":{"path":"stories/backlog.json"},"git":{"trunk_branch":"trunk"}}`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backlog.Path != "stories/backlog.json" {
		t.Errorf("Backlog.Path = %q", cfg.Backlog.Path)
	}
	if cfg.Git.TrunkBranch != "trunk" {
		t.Errorf("TrunkBranch = %q", cfg.Git.TrunkBranch)
	}
}

func TestLoadPointsAtTheLineThatBrokeTheJSON(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, "{\n  \"version\": 1,\n  \"backlog\": { \"path\": }\n}\n")

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load accepted broken JSON")
	}
	if got := codeOf(t, err); got != sdlcerr.ConfigUnreadable {
		t.Errorf("code = %s, want %s", got, sdlcerr.ConfigUnreadable)
	}
	var e *sdlcerr.Error
	errors.As(err, &e)
	if !strings.Contains(e.Why, "line 3") {
		t.Errorf("why = %q, want it to name line 3", e.Why)
	}
}

// Windows PowerShell 5 saves a file with a byte order mark.
func TestLoadReadsAFileWithAByteOrderMark(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, "\ufeff{\"version\":1,\"git\":{\"trunk_branch\":\"trunk\"}}\r\n")
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Git.TrunkBranch != "trunk" {
		t.Errorf("TrunkBranch = %q, want the file's", cfg.Git.TrunkBranch)
	}
}

// A setting of the wrong type is valid JSON, so a JSON checker finds nothing
// wrong with it. The message has to name the setting.
func TestLoadNamesTheSettingOfTheWrongType(t *testing.T) {
	for body, want := range map[string][]string{
		"{\n  \"loop\": {\"max_stop_blocks\": \"3\"}\n}\n": {"loop.max_stop_blocks", "a whole number", "a string", "line 2"},
		`{"thresholds":{"diff_size_cap":1.5}}`:             {"thresholds.diff_size_cap", "a whole number", "1.5"},
		`{"paths":{"tests":{"dirs":"tests/"}}}`:            {"paths.tests.dirs", "a list", "a string"},
		`{"freeze":{"allow_new_test_files":"yes"}}`:        {"freeze.allow_new_test_files", "true or false"},
	} {
		root := repo(t)
		writeConfig(t, root, body)
		_, err := Load(root)
		if err == nil {
			t.Fatalf("Load accepted %s", body)
		}
		if got := codeOf(t, err); got != sdlcerr.ConfigUnreadable {
			t.Errorf("%s: code = %s, want %s", body, got, sdlcerr.ConfigUnreadable)
		}
		if strings.Contains(err.Error(), "not valid JSON") {
			t.Errorf("%s: valid JSON was called invalid: %v", body, err)
		}
		var e *sdlcerr.Error
		errors.As(err, &e)
		for _, w := range want {
			if !strings.Contains(e.Why, w) {
				t.Errorf("%s: why = %q, want it to say %q", body, e.Why, w)
			}
		}
	}
}

// A setting the loop cannot honour is refused when the file is read. Each of
// these was accepted, and quietly did something other than what it said.
func TestLoadRefusesSettingsTheLoopCannotHonour(t *testing.T) {
	for body, want := range map[string]string{
		`{"version":2}`:                               "upgrade sdlc",
		`{"version":-1}`:                              "version is -1",
		`{"backlog":{"path":"../stories.json"}}`:      "backlog.path",
		`{"backlog":{"path":"/srv/stories.json"}}`:    "backlog.path",
		`{"backlog":{"path":"stories/../../x.json"}}`: "backlog.path",
		`{"thresholds":{"diff_size_cap":-5}}`:         "thresholds.diff_size_cap is -5",
		`{"thresholds":{"coverage_min":-1}}`:          "thresholds.coverage_min",
		`{"thresholds":{"mutation_min":-1}}`:          "thresholds.mutation_min",
		`{"loop":{"max_review_rounds":-1}}`:           "loop.max_review_rounds",
		`{"loop":{"max_rework_rounds":-1}}`:           "loop.max_rework_rounds",
		`{"loop":{"max_stop_blocks":-1}}`:             "loop.max_stop_blocks",
		`{"budget":{"per_story_usd":-60}}`:            "budget.per_story_usd",
	} {
		root := repo(t)
		writeConfig(t, root, body)
		_, err := Load(root)
		if err == nil {
			t.Errorf("Load accepted %s", body)
			continue
		}
		if got := codeOf(t, err); got != sdlcerr.ConfigInvalid {
			t.Errorf("%s: code = %s, want %s", body, got, sdlcerr.ConfigInvalid)
		}
		var e *sdlcerr.Error
		errors.As(err, &e)
		if !strings.Contains(e.Why, want) {
			t.Errorf("%s: why = %q, want it to say %q", body, e.Why, want)
		}
	}

	root := repo(t)
	writeConfig(t, root, `{"version":2,"loop":{"max_stop_blocks":-1}}`)
	var e *sdlcerr.Error
	if _, err := Load(root); !errors.As(err, &e) ||
		!strings.Contains(e.Why, "version") || !strings.Contains(e.Why, "max_stop_blocks") {
		t.Errorf("not every setting was named at once: %v", err)
	}

	for _, body := range []string{
		`{"version":1,"backlog":{"path":"./stories/backlog.json"}}`,
		`{"backlog":{"path":"stories/../user_stories.json"}}`,
		`{"loop":{"max_stop_blocks":0},"thresholds":{"diff_size_cap":0},"budget":{"per_story_usd":0}}`,
	} {
		root := repo(t)
		writeConfig(t, root, body)
		if _, err := Load(root); err != nil {
			t.Errorf("Load refused %s: %v", body, err)
		}
	}
}

// A misspelled setting was read as no setting at all, and its default applied
// with nothing said. Comments and command names are the user's to choose.
func TestUnknownKeysNamesEveryMisspelledSetting(t *testing.T) {
	root := repo(t)
	bom := string([]byte{0xEF, 0xBB, 0xBF})
	writeConfig(t, root, bom+`{"_doc":["a comment"],"Version":1,
		"humangates":{"pre_commit_pause_tiers":["high","medium"]},
		"loop":{"max_stop_block":0,"max_review_rounds":2},
		"commands":{"my_own_step":"make x"},
		"paths":{"tests":{"dir":["t/"],"file_globs":["*_t.go"]}}}`)
	if got, want := strings.Join(UnknownKeys(root), " "), "humangates loop.max_stop_block paths.tests.dir"; got != want {
		t.Errorf("UnknownKeys = %q, want %q", got, want)
	}

	writeConfig(t, root, `{not json`)
	if got := UnknownKeys(root); got != nil {
		t.Errorf("a file that does not load named %q", got)
	}
}

func TestBacklogPathFallsBackWhenTheFileEmptiesIt(t *testing.T) {
	cfg := Default()
	cfg.Backlog.Path = ""
	got := cfg.BacklogPath(filepath.FromSlash("/repo"))
	want := filepath.Join(filepath.FromSlash("/repo"), "user_stories.json")
	if got != want {
		t.Errorf("BacklogPath = %q, want %q", got, want)
	}
}

func TestOpenResolvesRootAndConfigTogether(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"git":{"trunk_branch":"main"}}`)
	deep := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := Open(deep)
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != root {
		t.Errorf("Root = %q, want %q", p.Root, root)
	}
	if got, want := p.Path(Dir, "state"), filepath.Join(root, Dir, "state"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

// A story names its tier in the backlog and the project names the paused tiers
// in its configuration, and a story that names none is low.
func TestATierPausesBeforeCommitOnlyWhenTheProjectSaysSo(t *testing.T) {
	gates := Default().HumanGates
	for _, c := range []struct {
		tier string
		want bool
	}{{"high", true}, {" High ", true}, {"low", false}, {"", false}, {"medium", false}} {
		if got := gates.PausesBeforeCommit(c.tier); got != c.want {
			t.Errorf("PausesBeforeCommit(%q) = %v, want %v", c.tier, got, c.want)
		}
	}
	if (HumanGates{PreCommitPauseTiers: []string{"low"}}).PausesBeforeCommit("") != true {
		t.Error("a story with no tier is not treated as low")
	}
	if (HumanGates{}).PausesBeforeCommit("high") {
		t.Error("a project that pauses no tier held a high-risk story")
	}
}
