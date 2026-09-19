//go:build windows

package fsx

import (
	"errors"
	"syscall"
	"testing"
)

// A file another process has open can be replaced everywhere but Windows,
// where it waits instead of failing part-way through a write; inUse
// recognises both errors Windows can raise for that -- an outright access
// denial and the sharing violation -- and nothing else.
func TestInUseTellsAReaderHoldingTheFileFromAnyOtherFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"access denied", syscall.Errno(5), true},      // ERROR_ACCESS_DENIED
		{"sharing violation", syscall.Errno(32), true}, // ERROR_SHARING_VIOLATION
		{"an unrelated error", errors.New("boom"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inUse(tc.err); got != tc.want {
				t.Errorf("inUse(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
