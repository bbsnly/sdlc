package shellpolicy

import "testing"

// A directory taken away takes the frozen tests in it with it. Each of these
// removed or moved a frozen test while naming only the directory it was in.
func TestTakingADirectoryAwayTakesItsFrozenTests(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go", "internal/load/testdata/config.json", "internal/Parse/parse_test.go"}
	for _, command := range []string{
		"rm -rf internal/calc",
		"rm -rf internal/calc/",
		"rm -rf ./internal/calc",
		"rm -rf Internal/Calc",
		"rm -rf internal",
		"rm -rf /work/project/internal/calc",
		"rm -r internal/load/testdata",
		"rm -R internal/calc",
		"rm --recursive internal/calc",
		"cd internal && rm -rf calc",
		"cd internal && rm -rf .",
		"cd internal/calc && rm -rf ..",
		"rmdir internal/calc",
		"rd /s /q internal\\calc",
		"mv internal/calc /tmp/x",
		"mv internal/calc internal/load /tmp",
		"mv -t /tmp internal/calc",
		"mv -t/tmp internal/calc",
		"mv --target-directory /tmp internal/calc",
		"mv --target-directory=/tmp internal/calc",
		"git rm -r internal/calc",
		"git -C internal rm -r calc",
		"git -c alias.nuke=rm nuke -r internal/calc",
		"rm -rf internal/parse",
		"git checkout -- internal",
		"git restore internal/calc",
		"git mv internal/calc internal/sum",
		"find internal/calc -delete",
		"find internal -type f -delete",
		`del internal\calc`,
	} {
		refused(t, command, s, "frozen-test-through-the-tool")
	}
	ps := s
	ps.PowerShell = true
	for _, command := range []string{
		`Remove-Item -Recurse internal\calc`,
		"Remove-Item internal/load -Recurse -Force",
		"Move-Item -Destination C:/tmp internal/calc",
		"Move-Item -Destination:C:/tmp internal/calc",
		"Rename-Item internal/calc sum",
	} {
		refused(t, command, ps, "frozen-test-through-the-tool")
	}

	// Putting a file into the directory, or copying it somewhere, takes nothing
	// away; the implementer does both all day.
	for _, command := range []string{
		"cp new.go internal/calc/",
		"mv new.go internal/calc",
		"mv -t internal/calc new.go",
		"mv -tinternal/calc new.go",
		"mv --target-directory internal/calc new.go",
		"mv --target-directory=internal/calc new.go",
		"cp -r internal/calc /tmp/backup",
		"touch internal/calc/new.go",
		"rm internal/calc/new.go",
		"mv internal/calc/new.go /tmp",
		"rm -rf internal/other",
		"rm -rf build",
		"rm internal/calc",
		"rm -f internal/calc",
		"cd build && rm -rf .",
		"cd $OUT && rm -rf .",
		"git mv new.go internal/calc",
		"git -C build rm -r internal/calc",
		"find internal -name '*.tmp' -delete",
		"find internal -name '*.tmp' -print",
		"find internal/calc -type f",
		"rm --force internal/calc",
	} {
		allowed(t, command, s)
	}
	for _, command := range []string{
		"Move-Item new.go -Destination internal/calc",
		"Move-Item -Destination:internal/calc new.go",
	} {
		allowed(t, command, ps)
	}
}
