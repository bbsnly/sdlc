package version

import "testing"

func TestGetAlwaysReportsSomethingTrue(t *testing.T) {
	if got := Get().Version; got == "" {
		t.Error("a build must always be able to name itself")
	}
}

func TestStringRendersCommitAndDirtiness(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Info
		want string
	}{
		{"no commit", Info{Version: "1.2.3"}, "1.2.3"},
		{"short commit", Info{Version: "1.2.3", Commit: "abc123"}, "1.2.3 (abc123)"},
		{"long commit truncates", Info{Version: "1.2.3", Commit: "0123456789abcdef0123"}, "1.2.3 (0123456789ab)"},
		{"dirty is visible", Info{Version: "1.2.3", Commit: "abc123", Dirty: true}, "1.2.3 (abc123, dirty)"},
	} {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestGetIsStable(t *testing.T) {
	first, second := Get(), Get()
	if first != second {
		t.Errorf("Get must be stable across calls: %+v then %+v", first, second)
	}
}
