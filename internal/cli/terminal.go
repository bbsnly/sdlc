package cli

import (
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// controls writes what sdlc prints with its control characters spelled out.
// The titles, messages and reasons a command shows come from files a clone
// brings along, and an escape sequence among them is acted on by the terminal:
// it can retitle the window, move the cursor over lines already printed, or
// hide what follows. Newlines and tabs are sdlc's own layout and pass as they
// are, as does a carriage return that ends a line. Every other control is written as \u and its code, which reads as what
// it is and, inside a JSON string, decodes back to the same character.
type controls struct {
	w io.Writer
	// pending is the start of a character the next write completes. fmt writes
	// whole strings, so it stays empty unless something was cut mid-character,
	// but a control cut in two is one again once the terminal joins it.
	pending []byte
}

func (c *controls) Write(p []byte) (int, error) {
	out, rest := spell(append(c.pending, p...), false)
	c.pending = append([]byte(nil), rest...)
	if _, err := c.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Flush writes out a character that was never completed.
func (c *controls) Flush() error {
	out, _ := spell(c.pending, true)
	c.pending = nil
	if len(out) == 0 {
		return nil
	}
	_, err := c.w.Write(out)
	return err
}

// spell returns b with its controls spelled out, and, unless final, the start
// of a character at the end that is still to be completed.
func spell(b []byte, final bool) (out, rest []byte) {
	out = make([]byte, 0, len(b))
	for len(b) > 0 {
		if !final && !utf8.FullRune(b) {
			return out, b
		}
		r, size := utf8.DecodeRune(b)
		switch {
		case r == '\r' && len(b) == 1 && !final:
			// It may be the first half of a line ending.
			return out, b
		case r == '\r' && len(b) > 1 && b[1] == '\n':
			// A line ending as a program on Windows prints it, in output sdlc
			// ran and shows. It moves the cursor nowhere a newline does not.
			out = append(out, '\r')
		case r == utf8.RuneError && size == 1 && b[0] >= 0x80 && b[0] <= 0x9f:
			// Not UTF-8, but a terminal that reads bytes takes it as a control.
			out = fmt.Appendf(out, `\x%02x`, b[0])
		case r != '\n' && r != '\t' && unicode.IsControl(r):
			out = fmt.Appendf(out, `\u%04x`, r)
		default:
			out = append(out, b[:size]...)
		}
		b = b[size:]
	}
	return out, nil
}
