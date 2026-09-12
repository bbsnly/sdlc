module github.com/bbsnly/sdlc

go 1.26

// Pinned so that a rebuild from this tag links the same compiler and
// produces the same bytes. Without it setup-go resolves whatever 1.26.x is
// current that day, and nobody can reproduce a released archive.
toolchain go1.26.6

require (
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.9
)

require github.com/inconshreveable/mousetrap v1.1.0 // indirect
