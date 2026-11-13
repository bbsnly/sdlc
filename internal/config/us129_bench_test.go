package config

// US-129 AC-10: Load of a configuration as a project keeps one, with every
// section the scaffold writes and a team section: this repository's own, as
// it stood at the story's base. Load keeps nothing between calls, so each
// iteration reads and decodes the file afresh.

import (
	"os"
	"path/filepath"
	"testing"
)

const us129BenchConfig = `{
  "_doc": [
    "This is the machine-readable half of your project's contract with the loop.",
    "The other half is the '## SDLC Contract' section of your CLAUDE.md: rules a",
    "script cannot check, written for the assistant to read.",
    "Nothing here starts the loop: a person does, by typing /sdlc:next in Claude Code.",
    "",
    "Commands run from the repository root. Leave one empty to skip that step.",
    "The coverage and mutation commands must print their percentage as the last",
    "number on stdout.",
    "",
    "Run 'sdlc doctor' after changing anything here.",
    "Reference: https://github.com/bbsnly/sdlc/blob/main/docs/configuration.md"
  ],
  "version": 1,
  "backlog": { "path": "user_stories.json" },
  "git": { "trunk_branch": "main", "remote": true },
  "commands": {
    "smoke":     "test -z \"$(go list ./...)\" || go vet ./...",
    "build":     "go build ./...",
    "test":      "go test -race -timeout 20m ./... -count=1",
    "npm_test":  "./task npm-test",
    "lint":      "./task lint",
    "fmt":       "./task fmt",
    "fmt_check": "./task fmt-check",
    "fmt_file":  "case \"$FILE\" in *.go) go fmt \"$FILE\" >/dev/null ;; esac",
    "coverage":  "go test -timeout 20m ./... -count=1 -coverprofile=cover.out >/dev/null && go tool cover -func=cover.out | tail -1 | grep -Eo '[0-9]+\\.[0-9]+'",
    "mutation":  "",
    "freeze_check": "./task lint && go test -count=1 -run '^$' ./... >/dev/null"
  },
  "thresholds": { "coverage_min": 95, "mutation_min": 0 },
  "paths": {
    "tests": { "dirs": ["testdata/", "test/"], "file_globs": ["*_test.go", "*.test.js"] },
    "src": ["cmd/", "internal/", "plugin/", "npm/", "install.sh", "install.ps1"]
  },
  "spec": { "paths": ["docs/", "README.md"] },
  "loop": { "max_review_rounds": 2, "max_rework_rounds": 3, "max_stop_blocks": 3 },
  "freeze": { "allow_new_test_files": false },
  "human_gates": { "pre_commit_pause_tiers": ["high"], "dor_advocate_check": false },
  "budget": { "per_story_usd": 0, "alert_fractions": [0.5, 0.8, 1.0] },
  "team": {
    "models": ["haiku", "sonnet", "opus", "fable"],
    "efforts": ["low", "medium", "high", "xhigh"],
    "agents": {
      "security": {
        "bring_back": [
          "internal/hook/", "internal/shellpolicy/", "internal/policy/", "internal/pathrules/",
          "internal/testset/", "internal/store/",
          "internal/cli/approve.go", "internal/cli/ack.go", "internal/cli/freeze.go",
          "internal/cli/release.go", "internal/cli/staffing.go",
          "install.sh", "install.ps1", "npm/", "plugin/hooks/",
          ".github/workflows/", ".goreleaser.yml"
        ]
      },
      "perf": { "bring_back": ["internal/hook/", "internal/shellpolicy/", "internal/pathrules/", "cmd/sdlc/"] },
      "human-advocate": {
        "bring_back": [
          "docs/", "README.md", "internal/sdlcerr/", "plugin/skills/", "plugin/agents/",
          "internal/scaffold/templates/", "REVIEW.md"
        ]
      }
    }
  }
}
`

func BenchmarkConfigLoad(b *testing.B) {
	root := b.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sdlc"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sdlc", "config.json"), []byte(us129BenchConfig), 0o644); err != nil {
		b.Fatal(err)
	}
	if _, err := Load(root); err != nil {
		b.Fatalf("the configuration does not load: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Load(root); err != nil {
			b.Fatal(err)
		}
	}
}
