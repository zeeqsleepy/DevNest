// Package cli owns everything the user types and everything the user reads:
// the command tree, flags, help text, and the choice of renderer.
//
// It is the only layer aware of how commands are parsed. Command handlers
// translate flags into a request, call one domain function, and hand the
// result to a renderer.
package cli

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/version"
)

// Example is a realistic invocation shown in help text. Every runnable command
// carries at least two, because examples are the part people actually read.
type Example struct {
	Command     string
	Description string
}

// Command is one node in the command tree. A node with children is a group; a
// node with a Run function is runnable. A node can be both.
type Command struct {
	// Name is the single word that selects this command.
	Name string
	// Summary is the one-line description shown in listings.
	Summary string
	// Description is the fuller explanation shown in this command's own help.
	Description string
	// Usage is the usage line, for example "devnest version [flags]".
	Usage string
	// Examples are shown in help. Runnable commands need at least two.
	Examples []Example
	// SetFlags registers flags specific to this command.
	SetFlags func(*flag.FlagSet)
	// Run executes the command. Nil means this node is a group only.
	Run func(ctx context.Context, env *Env, args []string) error
	// RunsWithoutConfig lets a command start when the configuration cannot be
	// loaded, with the compiled defaults in its place. Only "doctor" sets it:
	// the command whose job is to diagnose a broken configuration file is the
	// one command a broken configuration file must not stop.
	RunsWithoutConfig bool
	// Commands are the subcommands of this node.
	Commands []*Command

	parent *Command
}

// Env carries everything a command handler needs that is not a flag.
type Env struct {
	Config   config.Config
	Logger   *slog.Logger
	Renderer output.Renderer
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer

	// ConfigPath is the file --config named, empty for the default location,
	// and ConfigExplicit says which of the two it was. Only "doctor" needs
	// them: every other command receives resolved values and has no business
	// knowing where they came from.
	ConfigPath     string
	ConfigExplicit bool

	command  string
	started  time.Time
	warnings []output.Warning
	export   *export
}

// Warn records a non-fatal problem. It appears in the result envelope and is
// logged, so it survives both a script reading JSON and a person watching.
func (e *Env) Warn(code errors.Code, message string, attrs ...any) {
	e.warnings = append(e.warnings, output.Warning{Code: string(code), Message: message})
	e.Logger.Warn(message, attrs...)
}

// Emit renders a successful result: data for machines, text for people.
func (e *Env) Emit(data any, text output.TextFunc) error {
	return e.emit(data, text, nil)
}

// EmitTable renders a result that also has a row view, so that --output csv
// works for it.
//
// Commands whose result is genuinely a set of rows call this instead of Emit.
// Anything else keeps calling Emit and reports honestly that it has no csv
// form, which is better than inventing one.
func (e *Env) EmitTable(data any, text output.TextFunc, table output.TableFunc) error {
	return e.emit(data, text, table)
}

// emit writes the result to the terminal and, when --export was given, to a
// file as well. The file is written after the terminal output, so a person
// watching sees the result even when the export fails; the export failing is
// still fatal, because the user asked for a file.
func (e *Env) emit(data any, text output.TextFunc, table output.TableFunc) error {
	if e.export != nil && e.export.exists() {
		e.Warn(errors.CodeIO, "overwriting "+e.export.path)
	}

	envelope := output.NewEnvelope(e.meta(), data).WithWarnings(e.warnings)

	rows, ok := e.Renderer.(output.RowRenderer)
	switch {
	case ok && table != nil:
		if err := rows.RenderRows(e.Stdout, envelope, table); err != nil {
			return err
		}
	default:
		if err := e.Renderer.Render(e.Stdout, envelope, text); err != nil {
			return err
		}
	}

	if e.export == nil {
		return nil
	}
	return e.export.write(envelope, text, table)
}

func (e *Env) meta() output.Meta {
	return output.Meta{
		Version:    version.Short(),
		Command:    e.command,
		StartedAt:  e.started.UTC(),
		DurationMs: time.Since(e.started).Milliseconds(),
	}
}

// path returns the full command path, for example "config list".
func (c *Command) path() string {
	if c.parent == nil {
		return c.Name
	}
	return c.parent.path() + " " + c.Name
}

func (c *Command) find(name string) *Command {
	for _, sub := range c.Commands {
		if sub.Name == name {
			return sub
		}
	}
	return nil
}

// link sets parent pointers so a command can report its own path.
func (c *Command) link() {
	for _, sub := range c.Commands {
		sub.parent = c
		sub.link()
	}
}

// flagSet builds the flag set for a command: the global flags, which mean the
// same thing everywhere, plus whatever the command adds.
func (c *Command) flagSet(globals *globalFlags) *flag.FlagSet {
	set := flag.NewFlagSet(c.path(), flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.Usage = func() {}
	globals.register(set)
	if c.SetFlags != nil {
		c.SetFlags(set)
	}
	return set
}

// parsed is the result of splitting argv into a command, its flags, and its
// positional arguments.
type parsed struct {
	command    *Command
	flagArgs   []string
	positional []string
}

// split walks argv, descending into subcommands and separating flags from
// positional arguments.
//
// Go's flag package stops parsing at the first non-flag argument, so both
// "devnest version --output json" and "devnest --output json version" would
// otherwise behave differently. Splitting first makes flag position irrelevant,
// which is what users expect.
func split(root *Command, args []string, globals *globalFlags) parsed {
	result := parsed{command: root}
	set := root.flagSet(globals)
	descending := true

	for index := 0; index < len(args); index++ {
		argument := args[index]

		switch {
		case argument == "--":
			result.positional = append(result.positional, args[index+1:]...)
			return result

		case len(argument) > 1 && strings.HasPrefix(argument, "-"):
			result.flagArgs = append(result.flagArgs, argument)
			name, inline := flagName(argument)
			if inline || !takesValue(set, name) || index+1 >= len(args) {
				continue
			}
			index++
			result.flagArgs = append(result.flagArgs, args[index])

		default:
			if descending {
				if sub := result.command.find(argument); sub != nil {
					result.command = sub
					set = sub.flagSet(globals)
					continue
				}
				descending = false
			}
			result.positional = append(result.positional, argument)
		}
	}

	return result
}

// flagName strips leading dashes and reports whether the value was given
// inline as "-name=value".
func flagName(argument string) (name string, inline bool) {
	name = strings.TrimLeft(argument, "-")
	if before, _, found := strings.Cut(name, "="); found {
		return before, true
	}
	return name, false
}

// takesValue reports whether a flag consumes the following argument. Boolean
// flags do not; unknown flags are assumed not to, which leaves the flag package
// to produce the "not defined" error with its own wording.
func takesValue(set *flag.FlagSet, name string) bool {
	found := set.Lookup(name)
	if found == nil {
		return false
	}
	boolean, ok := found.Value.(interface{ IsBoolFlag() bool })
	return !ok || !boolean.IsBoolFlag()
}
