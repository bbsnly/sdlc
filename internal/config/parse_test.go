package config

// US-029.3 (PLAN.md step 5, THREATS T5): the landing reads the commands and
// thresholds trunk commits, from git's objects, not from a file on disk. So
// the configuration's bytes are read by Parse, which is Load without the
// file: for the same bytes, the same configuration, and the same refusal.
//
// Parse(raw) returns what Load does; cfgOf reads it whether that is a Config
// or a pointer to one.

import (
	"errors"
	"reflect"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func cfgOf(v any) any {
	if r := reflect.ValueOf(v); r.Kind() == reflect.Pointer && !r.IsNil() {
		return r.Elem().Interface()
	}
	return v
}

func TestParseReadsTheBytesExactlyAsLoadReadsTheFile(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"the defaults", `{"version": 1}`},
		{"commands and thresholds", `{"version": 1, "commands": {"test": "go test ./...", "coverage": "echo 90"},
			"thresholds": {"coverage_min": 80, "mutation_min": 0}, "git": {"trunk_branch": "develop"}}`},
		{"a byte order mark", "\xef\xbb\xbf" + `{"version": 1, "commands": {"test": "exit 0"}}`},
		{"not JSON", `{"version": 1,`},
		{"a setting of the wrong type", `{"version": 1, "thresholds": {"coverage_min": "eighty"}}`},
		{"a setting it cannot use", `{"version": 1, "backlog": {"path": "../elsewhere.json"}}`},
		{"a newer version", `{"version": 99}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, tc.body)
			loaded, loadErr := Load(root)
			parsed, parseErr := Parse([]byte(tc.body))

			if (loadErr == nil) != (parseErr == nil) {
				t.Fatalf("Load said %v and Parse said %v", loadErr, parseErr)
			}
			if loadErr != nil {
				var l, p *sdlcerr.Error
				if !errors.As(loadErr, &l) || !errors.As(parseErr, &p) {
					t.Fatalf("Load said %v and Parse said %v; want both an sdlc error", loadErr, parseErr)
				}
				if l.Code != p.Code || l.What != p.What || l.Why != p.Why {
					t.Errorf("Parse refused with %s %q / %q, and Load with %s %q / %q", p.Code, p.What, p.Why, l.Code, l.What, l.Why)
				}
				return
			}
			if !reflect.DeepEqual(cfgOf(parsed), loaded) {
				t.Errorf("Parse read\n%+v\nand Load read\n%+v", cfgOf(parsed), loaded)
			}
		})
	}
}
