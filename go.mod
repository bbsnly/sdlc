module github.com/bbsnly/sdlc

go 1.26.0

// Pinned so that a rebuild from this tag links the same compiler and
// produces the same bytes. Without it setup-go resolves whatever release is
// current that day, and nobody can reproduce a released archive. CI runs with
// GOTOOLCHAIN=local, so this must also be new enough to build the pinned
// GoReleaser, whose own go.mod asks for 1.27.1; the cross-build job checks it.
toolchain go1.27.1

require (
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.9
	golang.org/x/text v0.42.0
)

require github.com/inconshreveable/mousetrap v1.1.0 // indirect
