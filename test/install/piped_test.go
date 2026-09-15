package install

import (
	"strings"
	"testing"
)

// `irm ... | iex` is how the documentation says to run install.ps1, and
// Invoke-Expression runs a script in the caller's own scope. The installer's
// $ErrorActionPreference = 'Stop' stayed set in the user's session afterwards,
// along with its variables, and nothing ran the piped route to notice.
func TestThePipedPowerShellInstallerLeavesTheSessionAsItWas(t *testing.T) {
	skipUnlessWindows(t)
	dir := t.TempDir()
	// Single quotes only: a double quote in a -Command argument is at the mercy
	// of how the command line is split on Windows.
	script := "Get-Content -Raw install.ps1 | Invoke-Expression; " +
		"'after: {0} {1} [{2}] [{3}]' -f $ErrorActionPreference, $ProgressPreference, $tmp, $key"
	out, err := run(t, serve(t, release(t, false)), dir,
		"powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	if err != nil {
		t.Fatalf("install.ps1 through Invoke-Expression failed: %v\n%s", err, out)
	}
	installed(t, dir)
	if !strings.Contains(out, "after: Continue Continue [] []") {
		t.Errorf("the installer changed the session it was piped into:\n%s", out)
	}
}
