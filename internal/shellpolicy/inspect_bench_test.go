package shellpolicy

import "testing"

// BenchmarkInspect is Inspect on commands of the kind a session runs all day,
// each in the session the corpus asks it in. The hook reads every Bash and
// PowerShell call through it, so a change that makes these slower is felt on
// every call; internal/tools/hooktime has the long commands, slow by
// construction, that the hook-time leg holds to a budget. `./task bench`
// runs both, and CONTRIBUTING.md says how to compare two trees.
func BenchmarkInspect(b *testing.B) {
	for _, c := range []struct {
		name, who string
		ps        bool
		command   string
		refused   bool
	}{
		{name: "go-test", who: "implementer", command: "go test ./... -count=1 2>&1 | tail -n 40"},
		{name: "commit", command: `git add -A && git commit -m "feat(cli): say what the stop waits for"`, refused: true},
		{name: "commit-ready", who: "passed", command: `git add -A && git commit -m "feat(cli): say what the stop waits for"`},
		{name: "sed-in-place", who: "implementer", command: `sed -i 's/old/new/g' internal/cli/stop.go && gofmt -l internal/cli`},
		{name: "heredoc", who: "implementer", command: "cat > internal/cli/notes.go <<'EOF'\npackage cli\n\n// notes\nEOF\ngo build ./..."},
		{name: "frozen-test-write", who: "frozen", command: "echo // > " + aFrozenTest, refused: true},
		{name: "find-xargs", who: "implementer", command: `find . -name '*.go' -not -path './vendor/*' | xargs grep -n "TODO" | head`},
		{name: "loop", who: "implementer", command: `for f in internal/*/; do (cd "$f" && go vet . ) || exit 1; done`},
		{name: "powershell", ps: true, command: `Get-ChildItem -Recurse -Filter *.go | Select-String -Pattern 'TODO' | Select-Object -First 20`},
	} {
		s := corpusState(c.who, c.ps)
		if _, refused := Inspect(c.command, s); refused != c.refused {
			b.Fatalf("%s: Inspect refused it: %v, want %v; a time of another answer is not a time of the same work", c.name, refused, c.refused)
		}
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				Inspect(c.command, s)
			}
		})
	}
}
