package scaffold

// US-109: `sdlc init --force` on a file the commands refuse. The refusal's fix never names
// --force, but a person can run it, and it used to reset where the stories are and what trunk
// is on any file that would not load. A file refused for an old key or an unusable team was
// read, so it keeps them. This is config.ForRules' own table from its thinnest caller:
// the function is not there to call while every package here compiles ahead of the code.

import (
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
)

func TestAC13InitForceKeepsAFilesOwnPlacesWhereItWasReadAndNotWhereItWasNot(t *testing.T) {
	const own = `"backlog":{"path":"planning/stories.json"},"git":{"trunk_branch":"trunk"}`
	for _, tc := range []struct {
		name, file string
		kept       bool
	}{
		{"AC_12 a staffing section", `{"version":1,` + own + `,"staffing":{"enabled":true}}`, true},
		{"AC_12 a staffing of no shape", `{"version":1,` + own + `,"staffing":3}`, true},
		{"AC_12 gate7_advisory true", `{"version":1,` + own + `,"reviews":{"gate7_advisory":true}}`, true},
		{"AC_13 a team problem", `{"version":1,` + own + `,"team":{"retries":-1}}`, true},
		{"AC_13 a key that is not an agent", `{"version":1,` + own + `,"team":{"agents":{"designer":{}}}}`, true},
		{"AC_8 instances past the ceiling", `{"version":1,` + own + `,"team":{"agents":{"perf":{"instances":17}}}}`, true},
		{"AC_13 a file with nothing wrong", `{"version":1,` + own + `}`, true},
		{"AC_13 a version too high beside a team problem", `{"version":2,` + own + `,"team":{"retries":-1}}`, false},
		{"AC_13 a negative limit beside a team problem", `{"version":1,` + own + `,"team":{"retries":-1},"loop":{"max_review_rounds":-1}}`, false},
		{"AC_13 a wrong type inside the team", `{"version":1,` + own + `,"team":{"agents":{"security":{"enabled":"false"}}}}`, false},
		{"AC_13 the team of the wrong type", `{"version":1,` + own + `,"team":"yes"}`, false},
		{"AC_13 a file that is not JSON", `{not json`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if _, err := Init(root, false); err != nil {
				t.Fatal(err)
			}
			touch(t, root, "planning/stories.json", `{"stories":[{"id":"MINE-1","title":"Mine","status":"ready"}]}`)
			touch(t, root, config.File, tc.file)
			if _, err := Init(root, true); err != nil {
				t.Fatalf("--force on this file: %v", err)
			}
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatalf("--force left a file that does not load: %v", err)
			}
			wantBacklog, wantTrunk := config.Default().Backlog.Path, config.Default().Git.TrunkBranch
			if tc.kept {
				wantBacklog, wantTrunk = "planning/stories.json", "trunk"
			}
			if cfg.Backlog.Path != wantBacklog || cfg.Git.TrunkBranch != wantTrunk {
				t.Errorf("after --force: backlog %q, trunk %q, want %q, %q", cfg.Backlog.Path, cfg.Git.TrunkBranch, wantBacklog, wantTrunk)
			}
		})
	}
}
