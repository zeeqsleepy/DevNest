package doctor

import (
	"context"

	"github.com/devnest/devnest/internal/platform/proc"
	"github.com/devnest/devnest/internal/platform/sys"
)

// Filesystem is what the installation checks need of the disk: whether a path
// is there, and whether a directory can be written to.
type Filesystem interface {
	Exists(path string) (bool, error)
	// Writable reports whether a directory accepts a write, by attempting one.
	Writable(directory string) error
}

// Describer answers questions about the machine and the session.
type Describer interface {
	Describe() sys.Info
}

// Runner locates and runs the external tools that optional features need.
type Runner interface {
	Lookup(name string) []string
	Run(ctx context.Context, command proc.Command) (proc.Output, error)
}

// Environment is everything a self-check needs. It is one interface rather
// than three parameters because every check runs on every invocation: there is
// no doctor that looks at the disk and not at the machine.
type Environment interface {
	Filesystem
	Describer
	Runner
}
