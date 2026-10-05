package config

import (
	"slices"
	"strings"
	"testing"
)

// US-058 AC-5: a person can narrow the allowlist an unattended `sdlc parallel`
// grants, in one place in configuration. The key is parallel.allow: absent
// means the built-in list as it is, a list means that much of it, and an empty
// list means none. Whether an entry may be named at all is the launcher's to
// say (internal/parallel's Narrow); this is only that the setting reads, and
// that absent and empty are not the same thing.

func TestAC5ANarrowingListIsReadFromParallelAllow(t *testing.T) {
	cfg, err := Parse([]byte(`{"version":1,"parallel":{"allow":["Read","Bash(go test:*)"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Parallel.Allow == nil || !slices.Equal(*cfg.Parallel.Allow, []string{"Read", "Bash(go test:*)"}) {
		t.Errorf("Parallel.Allow = %v", cfg.Parallel.Allow)
	}
}

func TestAC5AnEmptyNarrowingListIsNotTheSameAsNone(t *testing.T) {
	absent, err := Parse([]byte(`{"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if absent.Parallel.Allow != nil {
		t.Errorf("a configuration that does not name the setting has %v: absent means the whole built-in list", *absent.Parallel.Allow)
	}
	empty, err := Parse([]byte(`{"version":1,"parallel":{"allow":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Parallel.Allow == nil || len(*empty.Parallel.Allow) != 0 {
		t.Errorf("Parallel.Allow = %v, want present and empty: nothing is granted", empty.Parallel.Allow)
	}
	if Default().Parallel.Allow != nil {
		t.Error("the default narrows the allowlist")
	}
}

func TestAC5TheNarrowingListIsNotAMisspelledSettingButItsTyposAre(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"version":1,"parallel":{"allow":["Read"]}}`)
	if got := UnknownKeys(root); got != nil {
		t.Errorf("UnknownKeys named %q for parallel.allow", got)
	}
	writeConfig(t, root, `{"version":1,"parallel":{"alow":["Read"]},"paralel":{}}`)              //nolint:misspell // deliberate typo: the test checks an unknown top-level key is reported
	if got, want := strings.Join(UnknownKeys(root), " "), "paralel parallel.alow"; got != want { //nolint:misspell // deliberate typo: the test checks an unknown top-level key is reported
		t.Errorf("UnknownKeys = %q, want %q", got, want)
	}
}

func TestAC5AnAllowListOfTheWrongTypeIsRefusedNotIgnored(t *testing.T) {
	if _, err := Parse([]byte(`{"version":1,"parallel":{"allow":"Read"}}`)); err == nil {
		t.Error("a string where the list goes was accepted")
	}
}
