//go:build !race

package main

// us129Race is whether this test binary was built with the race detector,
// under which the US-129 allocation rows count in a build of their own.
const us129Race = false
