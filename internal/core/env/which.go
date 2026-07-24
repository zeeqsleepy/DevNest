package env

import (
	"context"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/proc"
)

// WhichRequest describes one lookup.
type WhichRequest struct {
	Name string
	// Version runs the tool to report what each copy says it is, which is
	// the point of the command when two copies disagree.
	Version bool
	// Timeout bounds each version probe. Zero means the platform default.
	Timeout time.Duration
}

// Location is one place a name resolves to.
type Location struct {
	Position int    `json:"position"`
	Path     string `json:"path"`
	// Version is what this copy reported, when asked and when it answered.
	Version string `json:"version,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// WhichResult lists every location, in PATH order.
type WhichResult struct {
	Name string `json:"name"`
	// Winner is the copy that runs when the name is typed.
	Winner    string     `json:"winner,omitempty"`
	Locations []Location `json:"locations"`
	// Shadowed says more than one copy exists, which is the finding this
	// command is usually run to confirm.
	Shadowed bool `json:"shadowed"`
}

// Which reports every location a name resolves to.
//
// Not the first: all of them. The shell built-in gives the winner, and the
// winner is the one piece of information that does not help when a tool
// reports a version nobody expected. What helps is seeing the other three
// copies and which directory each came from.
func Which(ctx context.Context, deps interface {
	Runner
	Locator
}, request WhichRequest) (WhichResult, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return WhichResult{}, errors.New(errors.CodeInvalidInput, "no tool name was given").
			WithHint("pass the name of the tool to look for, for example: devnest env which go")
	}

	paths := deps.Lookup(name)
	result := WhichResult{
		Name:      name,
		Locations: make([]Location, 0, len(paths)),
		Shadowed:  len(paths) > 1,
	}
	if len(paths) > 0 {
		result.Winner = paths[0]
	}

	arguments := versionArguments(name)

	for index, path := range paths {
		if err := ctx.Err(); err != nil {
			return WhichResult{}, err
		}

		location := Location{Position: index + 1, Path: path}
		if request.Version && len(arguments) > 0 {
			location.Version, location.Detail = ask(ctx, deps, path, arguments, request.Timeout)
		}
		result.Locations = append(result.Locations, location)
	}

	return result, nil
}

// versionArguments returns the flag that makes a known tool print its version.
//
// An unknown tool gets nothing rather than a guess. Running an arbitrary
// program on a user's PATH with an invented flag is how a lookup turns into
// something that has side effects.
func versionArguments(name string) []string {
	if tool, known := findToolchain(name); known {
		return tool.args
	}
	return nil
}

// ask runs one copy and reads a version out of what it printed.
func ask(ctx context.Context, runner Runner, path string, args []string, timeout time.Duration) (string, string) {
	output, err := runner.Run(ctx, proc.Command{Name: path, Args: args, Timeout: timeout})
	if err != nil {
		return "", err.Error()
	}

	found := version(output.Combined())
	if found == "" {
		return "", "no version could be read from its output"
	}
	return found, ""
}
