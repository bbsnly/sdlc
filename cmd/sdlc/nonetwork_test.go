package main

// US-108 AC-9: the launcher fetches the binary, and the binary makes no network
// call of its own (the contract's phase constraints). A binary that cannot
// speak HTTP or TLS cannot be the one fetching anything, which is what holds
// the line the launcher draws: verification happens before the binary runs.

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAC9TheBinaryLinksNoHTTPOrTLS(t *testing.T) {
	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	for pkg := range strings.FieldsSeq(string(out)) {
		if pkg == "net/http" || strings.HasPrefix(pkg, "net/http/") || pkg == "crypto/tls" {
			t.Errorf("cmd/sdlc links %s, so the binary could make a network call of its own", pkg)
		}
	}
}
