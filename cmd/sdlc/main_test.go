package main

import (
	"strings"
	"testing"
)

func TestVersionVerbPrintsOnlyTheVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		var sb strings.Builder
		if err := run([]string{arg}, &sb); err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if got := strings.TrimSpace(sb.String()); got != version {
			t.Errorf("%s printed %q, want %q", arg, got, version)
		}
	}
}

func TestBareInvocationSaysItIsPreRelease(t *testing.T) {
	var sb strings.Builder
	if err := run(nil, &sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "pre-release") {
		t.Errorf("bare invocation should say what state this is in, got %q", sb.String())
	}
}
