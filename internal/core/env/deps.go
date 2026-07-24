package env

import (
	"context"

	"github.com/devnest/devnest/internal/platform/proc"
	"github.com/devnest/devnest/internal/platform/sys"
)

// Runner runs an external program.
//
// One method, because that is all a version probe is. A test that fakes it
// returns a canned line and never starts a process, which is what keeps this
// module's tests instant and identical on every machine.
type Runner interface {
	Run(ctx context.Context, command proc.Command) (proc.Output, error)
}

// Locator answers questions about PATH.
type Locator interface {
	// Lookup returns every location a name resolves to, in PATH order.
	Lookup(name string) []string
	// PathEntries returns the directories on PATH, in order.
	PathEntries() []string
	// Stat describes one PATH entry.
	Stat(path string) (proc.Entry, error)
	// Executables lists the runnable files in a directory, under the name a
	// shell would use to run each one.
	Executables(directory string) []proc.Executable
}

// Describer answers questions about the machine and the session.
type Describer interface {
	Describe() sys.Info
	Environ() map[string]string
}

// Inspector is everything the full summary needs. The three interfaces above
// stay separate because most commands need only one of them, and an operation
// that cannot start a process should not be handed something that can.
type Inspector interface {
	Runner
	Locator
	Describer
}
