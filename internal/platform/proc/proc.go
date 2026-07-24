// Package proc is the process half of the platform layer: running an external
// program and finding one on PATH.
//
// Everything here is bounded. A probe that hangs is worse than one that fails,
// because a hang has no error message and no exit code, and the user is left
// looking at a cursor. Every invocation carries a deadline, and the deadline
// has a default rather than relying on every caller to remember one.
//
// Nothing here interprets what it runs. Deciding that "go version go1.25
// windows/amd64" contains a version number is the module's job; this package
// runs a program and hands back what it wrote.
package proc

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// DefaultTimeout bounds an invocation whose caller did not set one.
//
// Two seconds is long enough for a toolchain to print its version on a cold
// cache and short enough that probing a dozen of them stays interactive. A
// tool that cannot answer in two seconds is reported as not answering, which
// is more useful than a summary that arrives a minute late.
const DefaultTimeout = 2 * time.Second

// waitDelay is how long a killed process has to release its output pipes
// before they are closed for it.
const waitDelay = 200 * time.Millisecond

// outputLimit caps what is read from a probed program.
//
// A version flag prints one line. Anything printing megabytes is either the
// wrong flag or a program doing something the caller did not intend, and
// neither is worth buffering.
const outputLimit = 64 * 1024

// Command describes one invocation.
type Command struct {
	// Name is the executable, resolved through PATH unless it is a path.
	Name string
	// Args are passed through unchanged.
	Args []string
	// Timeout bounds the run. Zero means DefaultTimeout.
	Timeout time.Duration
	// Dir is the working directory. Empty means the current one.
	Dir string
}

// Output is what a finished program produced.
//
// A non-zero exit is reported here rather than as an error: a tool answering
// "unrecognised flag" has answered, and the caller decides what that means.
// Only a failure to run the program at all comes back as an error.
type Output struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Combined returns stdout, or stderr when stdout was empty.
//
// Version flags are split between the two streams by different tools for no
// reason anybody remembers, so a caller that wants "what did it print" wants
// both, in that order.
func (o Output) Combined() string {
	if trimmed := strings.TrimSpace(o.Stdout); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(o.Stderr)
}

// System runs processes. Its zero value is ready to use.
type System struct{}

// Run executes a command and waits for it, bounded by its timeout.
//
// The process is started in its own environment with no shell involved.
// Passing a string to a shell is how an argument containing a space becomes
// two arguments, and how a path containing a semicolon becomes two commands.
func (s System) Run(ctx context.Context, command Command) (Output, error) {
	if strings.TrimSpace(command.Name) == "" {
		return Output{}, errors.New(errors.CodeInvalidInput, "no command was given")
	}

	timeout := command.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// #nosec G204 -- the command and its arguments come from a table in the
	// module layer, never from user input. There is no shell in this path.
	running := exec.CommandContext(bounded, command.Name, command.Args...)
	running.Dir = command.Dir

	// Killing a process does not kill its children, and a grandchild holding
	// the output pipe open keeps Wait blocked long after the deadline has
	// passed. A wrapper script that spawns the real tool is the common shape
	// of that, and without this the timeout would be advisory. WaitDelay
	// closes the pipes shortly after the kill and lets Run return.
	running.WaitDelay = waitDelay

	var stdout, stderr bytes.Buffer
	running.Stdout = &limitedWriter{buffer: &stdout, remaining: outputLimit}
	running.Stderr = &limitedWriter{buffer: &stderr, remaining: outputLimit}

	started := time.Now()
	err := running.Run()
	output := Output{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(started),
	}

	switch {
	case err == nil:
		return output, nil

	case bounded.Err() != nil && ctx.Err() == nil:
		// The bounded context expired while the caller's did not, so this is
		// the timeout rather than a cancelled run.
		return output, errors.Wrap(bounded.Err(), errors.CodeTimeout,
			"%s did not answer within %s", command.Name, timeout)

	case ctx.Err() != nil:
		return output, ctx.Err()
	}

	var exit *exec.ExitError
	if errors.As(err, &exit) {
		output.ExitCode = exit.ExitCode()
		return output, nil
	}

	if errors.Is(err, exec.ErrNotFound) {
		return output, errors.Wrap(err, errors.CodeNotFound,
			"%s is not on PATH", command.Name)
	}
	return output, errors.Wrap(err, errors.CodeIO, "cannot run %s", command.Name)
}

// Lookup returns every location a name resolves to, in PATH order.
//
// Not the first: all of them. A tool reporting the wrong version is nearly
// always a second copy earlier in PATH, and the first match is the one piece
// of information that does not help with that.
//
// A name containing a separator is treated as a path and checked directly,
// which is what a shell does.
func (s System) Lookup(name string) []string {
	if strings.TrimSpace(name) == "" {
		return nil
	}

	if strings.ContainsAny(name, `/\`) {
		if isExecutableFile(name) {
			return []string{filepath.Clean(name)}
		}
		return nil
	}

	var found []string
	seen := make(map[string]bool)

	for _, directory := range s.PathEntries() {
		for _, candidate := range candidates(name) {
			path := filepath.Join(directory, candidate)
			key := pathKey(path)
			if seen[key] || !isExecutableFile(path) {
				continue
			}
			seen[key] = true
			found = append(found, path)
		}
	}
	return found
}

// PathEntries splits PATH, dropping empty entries.
//
// An empty entry means the current directory on most platforms, which is a
// security problem rather than a feature: it lets a file in whatever directory
// you happen to be in shadow a real tool.
func (s System) PathEntries() []string {
	raw := os.Getenv("PATH")
	if raw == "" {
		return nil
	}

	var entries []string
	for _, entry := range strings.Split(raw, string(os.PathListSeparator)) {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}

// Stat describes a PATH entry, which is how the inspector tells a missing
// directory from a file that somebody put on PATH by mistake.
func (s System) Stat(path string) (Entry, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Entry{Path: path}, nil
		}
		return Entry{Path: path}, errors.Wrap(err, errors.CodeIO, "cannot read %s", path)
	}
	return Entry{Path: path, Exists: true, IsDir: info.IsDir()}, nil
}

// Entry is what is known about one path on PATH.
type Entry struct {
	Path   string
	Exists bool
	IsDir  bool
}

// Executable is one runnable file found in a directory on PATH.
type Executable struct {
	// Name is what a shell would type to run it. On Windows that is the
	// stem, because the extension comes from PATHEXT: go.exe and go.cmd are
	// two copies of "go", and treating them as unrelated files would hide
	// exactly the shadowing the caller is looking for.
	Name string
	// Path is the file itself.
	Path string
}

// Executables lists the runnable files in a directory.
//
// An unreadable directory returns nothing rather than an error. A PATH entry
// nobody can read is a finding the caller already has from Stat, and failing
// the whole inspection over one of them helps nobody.
func (s System) Executables(directory string) []Executable {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}

	found := make([]Executable, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if !isExecutableFile(path) {
			continue
		}
		found = append(found, Executable{Name: lookupName(entry.Name()), Path: path})
	}
	return found
}

// limitedWriter stops recording after a cap, so a program that writes without
// end cannot use this process's memory to do it.
type limitedWriter struct {
	buffer    *bytes.Buffer
	remaining int
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	if w.remaining <= 0 {
		return len(data), nil
	}
	if len(data) > w.remaining {
		data = data[:w.remaining]
	}
	written, err := w.buffer.Write(data)
	w.remaining -= written
	// The caller is told everything was written. It was not recorded, but a
	// short write here would make the probed program fail on a broken pipe,
	// and the probe is not what is being tested.
	return len(data), err
}
