//go:build !race

package hook

// us129Race is whether the race detector is built in, which AC-8's base
// figures depend on (us129_memory_test.go).
const us129Race = false
