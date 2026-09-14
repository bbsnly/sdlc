package shellpolicy

import "testing"

// The shell takes quotes and backslashes out of a word before the command sees
// it, and a quoted or escaped `(`, `)` or `;` is an argument, not the end of a
// command. Read as written, each of these reached the loop's own files.
func TestAQuoteOrAnEscapeDoesNotHideAPath(t *testing.T) {
	for _, command := range []string{
		"rm '.sdlc'/state/active",
		`rm ".sdlc"/config.json`,
		`rm .sd"lc"/state/active`,
		"rm $'.sdlc/state/active'",
		`rm .sd\lc/state/active`,
		"rm .sd\\\nlc/state/active",
		"rm .sdlc/sta\\\r\nte/active",
		`find . \( -name active \) -delete`,
		"find . '(' -name active ')' -delete",
		`find . \( -name x \) -exec rm .sdlc/state/active \;`,
		"find . '(' -name x ')' -exec rm .sdlc/state/active ';'",
		`find . -exec true {} \; -name active -delete`,
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	// Split there, what followed was a command called b.
	for _, sep := range []string{`\)`, `\;`, `\|`, `\&`, `')'`, `';'`, `'|'`, `'&'`, `")"`, `";"`, `"|"`, `"&"`} {
		refused(t, "rm a"+sep+"b .sdlc/state/active", ready, "loop-state-through-the-tool")
	}
	refused(t, "echo x > 'CLAUDE'.md", ready, "protected-path-through-the-tool")
	ps := ready
	ps.PowerShell = true
	refused(t, "Remove-Item '.sdlc'/state/active", ps, "loop-state-through-the-tool")
	// In PowerShell a backslash is a separator, and one at the end of a line
	// continues nothing.
	refused(t, "cd C:\\work\\\nRemove-Item .sdlc\\state\\active", ps, "loop-state-through-the-tool")

	for _, command := range []string{
		`echo "it's fine" > notes.txt`,
		`printf '%s\n' a > out.txt`,
		`echo \(x\) > build/out`,
		`find . -name '*.go' -exec gofmt -l {} \;`,
		`grep -r "a)b" src`,
	} {
		allowed(t, command, ready)
	}
}
