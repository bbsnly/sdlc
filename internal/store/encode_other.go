//go:build !go1.27

package store

import "encoding/json"

// encodeIndented is encode_go127.go's for a Go without encoding/json/v2: the
// value is encoded whole by json.Encoder, which hands its buffer to the writer
// as it is where json.Marshal would clone it first, and laid out into exactly
// the room its layout needs. The newline Encode adds is white space outside a
// string, which the layout drops. json.Marshal's clone of a 190 KB record put
// AC-8's first-call row 5% over under Go 1.26 (US-129 verification, N-1).
//
// The encoder's own buffer comes from a pool, which is empty in a new
// process, so a hook built with such a Go grows it from nothing at every
// call: what encode_go127.go says of it. No release is built with one.
func encodeIndented(hint int, v any) ([]byte, error) {
	if hint == 0 {
		var count indentCounter
		if err := json.NewEncoder(&count).Encode(v); err != nil {
			return nil, err
		}
		hint = count.n
	}
	w := indentWriter{out: make([]byte, 0, hint+1024)}
	if err := json.NewEncoder(&w).Encode(v); err != nil {
		return nil, err
	}
	return w.out, nil
}
