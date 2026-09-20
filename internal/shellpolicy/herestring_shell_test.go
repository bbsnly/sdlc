package shellpolicy

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellx"
)

// What a shell does with each row of the here-string tables is asked of the
// shell. The rows were written from a reading of two grammars, and the tests of
// the classifier pass whether or not that reading is right: a row that says
// PowerShell takes a line as text when PowerShell runs it is a hole the table
// would hold open. Only a stand-in is ever run. It prints 42, which its own
// text does not contain, so a body echoed as text cannot be taken for a run.

// sawTheShell compares what a shell printed with what the row says it does.
func (c hereStringCase) sawTheShell(t *testing.T, script, stdout string, failed error) {
	t.Helper()
	ran, echoed := strings.Contains(stdout, "42"), strings.Contains(stdout, "6*7")
	got := nothing
	switch {
	case ran:
		got = runsIt
	case echoed:
		got = asText
	case failed == nil:
		t.Fatalf("the shell finished %q without running or printing the line:\n%s", script, stdout)
	}
	if got != c.shell {
		t.Errorf("the shell read the line of %q as %d, and the row says %d (0 runs it, 1 text, 2 nothing runs); it printed:\n%s",
			script, got, c.shell, stdout)
	}
}

// Rows that differ only in the command under test are one row to a shell.
func rowsFor(ps bool) []hereStringCase {
	var rows []hereStringCase
	for _, c := range slices.Concat(hereStringEvasions, hereStringGuards, hereStringNet) {
		if c.ps == ps && c.run == "" {
			rows = append(rows, c)
		}
	}
	return rows
}

func TestAPOSIXShellReadsEachBashRowAsTheTableSays(t *testing.T) {
	for _, c := range rowsFor(false) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			script := c.text("echo $((6*7))")
			cmd, err := shellx.Command(t.Context(), t.TempDir(), script)
			if errors.Is(err, shellx.ErrNoShell) {
				t.Skip("neither sh nor bash is on PATH, so no shell was asked")
			}
			if err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			failed := cmd.Run()
			c.sawTheShell(t, script, stdout.String(), failed)
		})
	}
}

func TestPowerShellReadsEachPowerShellRowAsTheTableSays(t *testing.T) {
	var shell string
	for _, name := range []string{"pwsh", "powershell"} {
		if path, err := exec.LookPath(name); err == nil {
			shell = path
			break
		}
	}
	if shell == "" {
		// Nothing else asks PowerShell, and `go test` does not print a skip: on
		// CI a runner without it must fail, or the rows go unchecked in silence.
		if os.Getenv("CI") != "" {
			t.Fatal("neither pwsh nor powershell is on PATH on CI, so no PowerShell row was checked")
		}
		t.Skip("neither pwsh nor powershell is on PATH; CI asks PowerShell about these rows")
	}
	for _, c := range rowsFor(true) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			script := c.text("Write-Output (6*7)")
			file := filepath.Join(t.TempDir(), "row.ps1")
			if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), shell, "-NoProfile", "-NonInteractive", "-File", file)
			cmd.Dir = filepath.Dir(file)
			// Without these PowerShell calls home on start.
			cmd.Env = append(os.Environ(), "POWERSHELL_TELEMETRY_OPTOUT=1", "POWERSHELL_UPDATECHECK=Off")
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			failed := cmd.Run()
			c.sawTheShell(t, script, stdout.String(), failed)
		})
	}
}
