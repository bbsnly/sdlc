//go:build go1.27

package store

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
)

// encodeIndented is json.MarshalIndent(v, "", "  ") into one buffer. hint is
// how many bytes the file held before, where it was read, and 0 where nothing
// says how big the value is.
//
// encoding/json/v2 under encoding/json's own options gives the same bytes as
// json.Marshal, and hands them to a writer a few kilobytes at a time as it
// goes, so an indentWriter lays them out into a buffer sized by the hint plus
// a little, and nothing else of the value's size is allocated.
// json.MarshalIndent grows a pooled buffer by doubling, clones it, and
// reformats it through a second buffer grown the same way: for a 190 KB gate
// record in a cold process, 1.6 MB where this is 0.2 MB (US-129, AC-8).
// Without a hint the value is encoded twice, the first time into a count of
// what its layout comes to, so that the one buffer is still exactly sized:
// the second encode costs a millisecond of CPU for the 190 KB record, where a
// buffer grown by doubling costs twice the value in bytes. The bytes are the
// same both times: under these options maps are written in key order.
//
// The module builds with Go 1.26 too, which has no encoding/json/v2:
// encode_other.go is the same function by way of json.Encoder. That one is
// not enough here, though it is one file where this is two: json.Encoder
// holds the whole value in a buffer it takes from a pool before it writes
// any of it, and a hook process is new at every call, so its pool is empty
// and the buffer is grown from nothing. With it as the only encoder (the
// code review's F-6, tried) the first SubagentStart of a process allocated
// 1.31 MB where this allocates 0.62 MB; the row that measures it builds its
// fixture in the same process, which fills the pool, so it saw the 1.31 MB
// only where the scheduler had moved it to another processor in between,
// which is to say on a busy machine.
func encodeIndented(hint int, v any) ([]byte, error) {
	if hint == 0 {
		var count indentCounter
		if err := jsonv2.MarshalWrite(&count, v, json.DefaultOptionsV1()); err != nil {
			return nil, err
		}
		hint = count.n
	}
	w := indentWriter{out: make([]byte, 0, hint+1024)}
	if err := jsonv2.MarshalWrite(&w, v, json.DefaultOptionsV1()); err != nil {
		return nil, err
	}
	return w.out, nil
}
