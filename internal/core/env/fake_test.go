package env

import (
	"context"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/proc"
	"github.com/devnest/devnest/internal/platform/sys"
)

// fakeMachine is an in-memory machine implementing Inspector.
//
// No process is ever started, no PATH is ever read, and the answers are the
// same on every operating system. That is the point: these tests are about
// what the module does with an answer, not about what a real toolchain
// happens to print on the machine running them.
type fakeMachine struct {
	// locations maps a tool name to every path it resolves to.
	locations map[string][]string
	// outputs maps a path to what running it prints.
	outputs map[string]string
	// failures maps a path to an error instead of output.
	failures map[string]error
	// entries are the PATH directories, in order.
	entries []string
	// directories describes each PATH entry.
	directories map[string]proc.Entry
	// executables maps a directory to what it holds.
	executables map[string][]proc.Executable
	// environ is the environment.
	environ map[string]string
	// info is what the machine says it is.
	info sys.Info

	// ran records every command that was run, so a test can assert that a
	// tool which is not installed is never started.
	ran []string
}

func newFakeMachine() *fakeMachine {
	return &fakeMachine{
		locations:   make(map[string][]string),
		outputs:     make(map[string]string),
		failures:    make(map[string]error),
		directories: make(map[string]proc.Entry),
		executables: make(map[string][]proc.Executable),
		environ:     make(map[string]string),
		info:        sys.Info{OS: "linux", Architecture: "amd64", CPUs: 8, Shell: "bash"},
	}
}

// withTool installs a tool at a path that prints the given version line.
func (f *fakeMachine) withTool(name, path, output string) *fakeMachine {
	f.locations[name] = append(f.locations[name], path)
	f.outputs[path] = output
	return f
}

// withPath adds a directory to PATH.
func (f *fakeMachine) withPath(path string, exists, isDir bool) *fakeMachine {
	f.entries = append(f.entries, path)
	f.directories[path] = proc.Entry{Path: path, Exists: exists, IsDir: isDir}
	return f
}

// withExecutables places runnable files in a PATH directory.
func (f *fakeMachine) withExecutables(directory string, names ...string) *fakeMachine {
	for _, name := range names {
		f.executables[directory] = append(f.executables[directory], proc.Executable{
			Name: name,
			Path: directory + "/" + name,
		})
	}
	return f
}

func (f *fakeMachine) withEnv(name, value string) *fakeMachine {
	f.environ[name] = value
	return f
}

func (f *fakeMachine) Run(_ context.Context, command proc.Command) (proc.Output, error) {
	f.ran = append(f.ran, command.Name)

	if err, failing := f.failures[command.Name]; failing {
		return proc.Output{}, err
	}
	output, known := f.outputs[command.Name]
	if !known {
		return proc.Output{}, errors.New(errors.CodeNotFound, "%s is not on PATH", command.Name)
	}
	return proc.Output{Stdout: output}, nil
}

func (f *fakeMachine) Lookup(name string) []string { return f.locations[name] }

func (f *fakeMachine) PathEntries() []string { return f.entries }

func (f *fakeMachine) Stat(path string) (proc.Entry, error) {
	if entry, known := f.directories[path]; known {
		return entry, nil
	}
	return proc.Entry{Path: path}, nil
}

func (f *fakeMachine) Executables(directory string) []proc.Executable {
	return f.executables[directory]
}

func (f *fakeMachine) Describe() sys.Info { return f.info }

func (f *fakeMachine) Environ() map[string]string { return f.environ }

// findTool returns the reported tool with the given name.
func findTool(tools []Tool, name string) (Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

func requireVersion(t *testing.T, tools []Tool, name, want string) {
	t.Helper()
	tool, found := findTool(tools, name)
	if !found {
		t.Fatalf("%q is missing from the listing", name)
	}
	if tool.Version != want {
		t.Errorf("version of %q = %q, want %q", name, tool.Version, want)
	}
}

func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}
