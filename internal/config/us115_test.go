package config

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// rpInvisible holds characters unicode.IsControl does not, written as runes so
// that this file does not hold them raw.
var rpInvisible = string([]rune{0x2028, 0x2029, 0x202e, 0x200b, 0xfeff})

// US-115 AC-1, and the person's Q2: an empty reviews.policy is the default, as
// backlog.path is, and a project that says nothing has the default too. A file
// that names no policy must leave the field empty, or doctor could not tell a
// default from a file a person named (AC-6).
func TestAC1ReviewsPolicyIsAPathThatDefaultsToREVIEWmd(t *testing.T) {
	t.Parallel()
	if got := Default().ReviewPolicyPath(); got != "REVIEW.md" {
		t.Errorf("the default configuration's policy is %q", got)
	}
	for _, c := range []struct {
		name, raw, path, named string
	}{
		{"no reviews section", `{"version":1}`, "REVIEW.md", ""},
		{"reviews with no policy", `{"version":1,"reviews":{"gate7_advisory":true}}`, "REVIEW.md", ""},
		{"an empty policy", `{"version":1,"reviews":{"policy":""}}`, "REVIEW.md", ""},
		{"the default named", `{"version":1,"reviews":{"policy":"REVIEW.md"}}`, "REVIEW.md", "REVIEW.md"},
		{"another file", `{"version":1,"reviews":{"policy":"docs/review-policy.md"}}`, "docs/review-policy.md", "docs/review-policy.md"},
		{"as written", `{"version":1,"reviews":{"policy":"./docs/p.md"}}`, "./docs/p.md", "./docs/p.md"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Parse([]byte(c.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.ReviewPolicyPath(); got != c.path {
				t.Errorf("ReviewPolicyPath = %q, want %q", got, c.path)
			}
			if cfg.Reviews.Policy != c.named {
				t.Errorf("Reviews.Policy = %q, want %q", cfg.Reviews.Policy, c.named)
			}
		})
	}
}

// T6 and T7. A path with a newline in it passes filepath.IsLocal on Unix, and
// `review list` prints the path on a line of its own: it would start a line that
// reads as a reviewer's. Validation runs on every load, the hook's too, so the
// refusal has to name the setting.
func TestAC1APolicyPathThatLeavesTheRepositoryOrHoldsAControlCharacterIsRefused(t *testing.T) {
	t.Parallel()
	for name, path := range map[string]string{
		"a newline":             "REVIEW\n  design_review   architect blocking approve (round 1)",
		"a carriage return":     "REVIEW\r.md",
		"a tab":                 "REVIEW\t.md",
		"an escape":             "REVIEW\x1b[2J.md",
		"a delete":              "REVIEW\x7f.md",
		"a C1 control":          "REVIEW\u0085.md",
		"a parent directory":    "../REVIEW.md",
		"climbing out midway":   "docs/../../REVIEW.md",
		"an absolute path":      "/etc/REVIEW.md",
		"a newline and leaving": "../REVIEW\n.md",
		// Security N1: Rel cleans first and says inside, Stat through Join says a
		// regular file, and a reader of the path as written follows `l` out.
		"a parent segment that stays inside": "docs/../REVIEW.md",
		"a parent segment after a link":      "l/../secret.txt",
		"a parent segment at the end":        "docs/sub/..",
		// Red team, hostile user 3: the shell compares word by word, so a path
		// with a space in it is one the rule cannot hold.
		"a space":              "docs/review policy.md",
		"a leading space":      " REVIEW.md",
		"a trailing space":     "REVIEW.md ",
		"a no-break space":     "docs/review" + string(rune(0x00a0)) + "policy.md",
		"an ideographic space": "docs/review" + string(rune(0x3000)) + "policy.md",
		// Red team, hostile user 6: unicode.IsControl is false for each of these,
		// and each reaches `review list` unescaped.
		"a line separator":      "REVIEW" + string(rune(0x2028)) + ".md",
		"a paragraph separator": "REVIEW" + string(rune(0x2029)) + ".md",
		"a right-to-left mark":  "REVIEW" + string(rune(0x202e)) + ".md",
		"a zero-width space":    "REVIEW" + string(rune(0x200b)) + ".md",
		"a byte order mark":     "REVIEW" + string(rune(0xfeff)) + ".md",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(map[string]any{"version": 1, "reviews": map[string]any{"policy": path}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = Parse(raw)
			if err == nil {
				t.Fatalf("reviews.policy %q was accepted", path)
			}
			var e *sdlcerr.Error
			if !errors.As(err, &e) || e.Code != sdlcerr.ConfigInvalid {
				t.Fatalf("the refusal is not the configuration's own: %v", err)
			}
			if !strings.Contains(e.Why, "reviews.policy") {
				t.Errorf("the refusal does not name the setting: %s", e.Why)
			}
			// The value is quoted, so the refusal cannot start a line of its own.
			if strings.ContainsAny(e.Why, "\n\r\x1b"+rpInvisible) {
				t.Errorf("the refusal prints the character: %q", e.Why)
			}
		})
	}
}

// A path the check must leave alone: one a project might really use, in a
// subdirectory, with a dot, a hyphen or a non-ASCII letter in it.
func TestAC1APolicyPathInTheRepositoryIsAccepted(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"REVIEW.md", "docs/REVIEW.md", "docs/review-policy.md", "./docs/p.md", ".github/REVIEW.md", "docs/révision.md"} {
		raw, err := json.Marshal(map[string]any{"version": 1, "reviews": map[string]any{"policy": path}})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := Parse(raw)
		if err != nil {
			t.Errorf("%q was refused: %v", path, err)
			continue
		}
		if got := cfg.ReviewPolicyPath(); got != path {
			t.Errorf("ReviewPolicyPath = %q, want %q", got, path)
		}
	}

	// doctor reports a setting sdlc does not know, and this one it now knows.
	root := t.TempDir()
	coWrite(t, filepath.Join(root, filepath.FromSlash(File)), `{"version":1,"reviews":{"policy":"docs/p.md"}}`)
	if unknown := UnknownKeys(root); slices.Contains(unknown, "reviews.policy") {
		t.Errorf("UnknownKeys = %v", unknown)
	}

	// The new key sits beside the old one in the one section.
	cfg, err := Parse([]byte(`{"version":1,"reviews":{"gate7_advisory":true,"policy":"docs/p.md"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Reviews.Gate7Advisory {
		t.Error("reviews.gate7_advisory was lost beside reviews.policy")
	}
}

// Security, Gate 7 (round 1 N2, round 2 N1): the shell rules read a word apart
// at each of these, so a path holding one is a policy the rule would not hold.
func TestAC1APolicyPathWithACharacterTheShellReadsApartIsRefused(t *testing.T) {
	t.Parallel()
	for _, c := range []string{"$", `"`, "'", `\`, "`", ";", "&", "|", "<", ">", "(", ")"} {
		path := "docs/a" + c + "b.md"
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(map[string]any{"version": 1, "reviews": map[string]any{"policy": path}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = Parse(raw)
			if err == nil {
				t.Fatalf("reviews.policy %q was accepted", path)
			}
			var e *sdlcerr.Error
			if !errors.As(err, &e) || e.Code != sdlcerr.ConfigInvalid {
				t.Fatalf("the refusal is not the configuration's own: %v", err)
			}
			if !strings.Contains(e.Why, "reviews.policy") {
				t.Errorf("the refusal does not name the setting: %s", e.Why)
			}
		})
	}
}
