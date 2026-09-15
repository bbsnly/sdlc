package shellpolicy

import "testing"

// An option's value is not a URL. Read as one, `wget -r -np -A '*.md'
// https://docs.example.com/guide/` saved a file called `*.md`, which is
// CLAUDE.md among others, and was refused.
func TestAWgetOptionsValueIsNotAURL(t *testing.T) {
	for _, command := range []string{
		"wget -r -np -A '*.md' https://docs.example.com/guide/",
		"wget -r -np --accept '*.md' https://docs.example.com/guide/",
		"wget -r -R CLAUDE.md https://docs.example.com/",
		"wget --reject CLAUDE.md -r https://docs.example.com/",
		"wget -r -I /CLAUDE.md https://docs.example.com/",
	} {
		allowed(t, command, ready)
	}
	refused(t, "wget -A '*.md' example.com/CLAUDE.md", ready, "protected-path-through-the-tool")
}
