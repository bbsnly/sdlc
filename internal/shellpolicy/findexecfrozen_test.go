package shellpolicy

import "testing"

// What find runs a writer on, it picks by name as much as what it deletes. Read
// by name only for -delete, `find internal -name add_test.go -exec rm {} \;`
// removed a frozen test that no word of the command spelled out.
func TestFindRunningAWriterOnAFrozenTestByName(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		`find internal -name add_test.go -exec rm {} \;`,
		`find . -name '*_test.go' -exec rm -f {} +`,
		`find internal -name add_test.go -execdir sed -i s/a/b/ {} \;`,
	} {
		refused(t, command, s, "frozen-test-through-the-tool")
	}
	for _, command := range []string{
		`find internal -name add_test.go -exec cat {} \;`,
		`find internal -name other.go -exec rm {} \;`,
		`find internal -name add_test.go -fprint /tmp/list`,
	} {
		allowed(t, command, s)
	}
}
