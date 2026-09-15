package shellpolicy

import "testing"

// An option's value is not what a copy is given. Read as an operand, the value
// of an option written after the destination was taken for the destination:
// `rsync -a /tmp/x/ internal/calc/add_test.go --info progress2` wrote a frozen
// test as a copy to a directory called progress2.
func TestAnOptionsValueIsNotWhereACopyGoes(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"rsync -a /tmp/x/ internal/calc/add_test.go --info progress2",
		"rsync -a /tmp/x/ internal/calc/add_test.go --debug ALL",
		"rsync -a --delete /tmp/x/ internal/calc/add_test.go --max-delete 10",
		"rsync -a /tmp/x/ internal/calc/add_test.go --modify-window 1",
		"rsync -a /tmp/x/ internal/calc/add_test.go --stop-after 5",
		"scp host:evil internal/calc/add_test.go -X nsessions=1",
		"scp host:evil internal/calc/add_test.go -D /usr/libexec/sftp-server",
	} {
		refused(t, command, s, "frozen-test-through-the-tool")
	}
}
