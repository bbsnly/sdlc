package model

// US-129 AC-2 for the record's decoder. DecodeRecord sizes the events slice
// from a scan of the bytes before json.Unmarshal reads them, and the scan
// looked one byte past an "events":[ that only white space followed: the
// record a hand edit or a cut-off write leaves made every reader of it panic,
// where the base said SDLC-E0005 (US-129 verification, F-1). Whatever the
// bytes, the decoder answers what json.Unmarshal answers into a Record, the
// base's reading, error and value alike.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestAC2EveryRecordShapeTheDecodersScanLooksAtDecodesAsJSONUnmarshalDecodesIt(t *testing.T) {
	t.Parallel()
	shapes := map[string]string{
		"an events array opened and followed by a space":     `{"story":"US-001","events":[ `,
		"an events array opened and followed by line breaks": "{\"events\":[\n\n",
		"an events array opened and followed by a tab":       "{\"events\":[\t",
		"an events array opened and followed by a CRLF":      "{\"events\":[\r\n",
		"an events array opened at the end":                  `{"events":[`,
		"an events key at the end":                           `{"events":`,
		"an events key and white space at the end":           "{\"events\": \n",
		"an events array of one event cut off":               `{"events":[ {"type":"gate"`,
		"an empty events array":                              `{"story":"US-001","events":[ ]}`,
		"an events array of two events":                      `{"events":[{"type":"gate","at":"x"},{"type":"agent_end"}]}`,
		"the events key inside a string":                     `{"notes":"\"events\":[ "}`,
		"the events key inside a string and nothing after":   `{"notes":"\"events\":[ `,
		"events that are not an array":                       `{"events":{"type":"gate"}}`,
		"events that are null":                               `{"events":null}`,
		"a gate before an events array opened and left":      "{\"gates\":{},\"events\":[ \n",
		"a record that is not an object":                     `["events",[ `,
		"nothing at all":                                     ``,
	}
	for name, raw := range shapes {
		t.Run("AC-2 "+name, func(t *testing.T) {
			t.Parallel()
			var want Record
			wantErr := json.Unmarshal([]byte(raw), &want)
			var got Record
			gotErr := decodeWithoutPanic(t, raw, &got)
			if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
				t.Errorf("AC-2 DecodeRecord(%q) = %v, want what json.Unmarshal says: %v", raw, gotErr, wantErr)
			}
			// On an error the record is thrown away by every caller, and
			// json.Unmarshal of bytes that do not parse leaves it as it was.
			if wantErr == nil && !reflect.DeepEqual(got, want) {
				t.Errorf("AC-2 DecodeRecord(%q) leaves %+v, json.Unmarshal %+v", raw, got, want)
			}
		})
	}
}

// decodeWithoutPanic is DecodeRecord, with a panic failing the test as one:
// the hook answers a panic by letting the call through.
func decodeWithoutPanic(t *testing.T, raw string, r *Record) (err error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("AC-2 DecodeRecord(%q) panicked: %v", raw, p)
			err = fmt.Errorf("panicked: %v", p)
		}
	}()
	return DecodeRecord([]byte(raw), r)
}
