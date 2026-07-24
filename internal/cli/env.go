package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/devnest/devnest/internal/core/env"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/proc"
	"github.com/devnest/devnest/internal/platform/sys"
)

// newEnvCommand builds the "env" group, which is runnable itself: the summary
// is what people want nine times out of ten, and making them type a second
// word for it would be ceremony.
func newEnvCommand() *Command {
	var (
		timeout time.Duration
		missing bool
	)

	return &Command{
		Name:    "env",
		Summary: "Inspect this machine: toolchains, PATH, environment",
		Usage:   "devnest env [command] [flags]",
		Description: "Report what is installed on this machine and what the environment " +
			"says: operating system, architecture, shell, detected toolchains with " +
			"their versions, and the state of PATH.\n\n" +
			"Run on its own it prints the summary. This is the command to run on a " +
			"machine that is not yours, or on your own after something stopped " +
			"working.\n\n" +
			"A tool that is not installed is an ordinary result, not a failure. So is " +
			"one that is installed and will not say what version it is: both are " +
			"reported, and neither stops the run.\n\n" +
			"Nothing here changes anything. No variable is set, no file is written, " +
			"and every program that is run is run with a timeout and without a shell.",
		Examples: []Example{
			{
				Command:     "devnest env",
				Description: "Everything about this machine on one screen.",
			},
			{
				Command:     "devnest env --missing --output json",
				Description: "Include the tools that are absent, for a build agent report.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.DurationVar(&timeout, "timeout", 0,
				"how long to wait for each tool to report its version")
			set.BoolVar(&missing, "missing", false, "include tools that were not found")
		},
		Run: func(ctx context.Context, cliEnv *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput,
					"unknown command %q for \"devnest env\"", args[0]).
					WithHint("run \"devnest env --help\" to see the available commands")
			}

			result, err := env.Summarize(ctx, environment{}, env.SummaryRequest{
				Timeout:        timeout,
				IncludeMissing: missing,
			})
			if err != nil {
				return err
			}

			return cliEnv.EmitTable(result, envSummaryText(result), envToolTable(result.Tools))
		},
		Commands: []*Command{
			newEnvListCommand(),
			newEnvPathCommand(),
			newEnvWhichCommand(),
			newEnvVarsCommand(),
		},
	}
}

// environment is the real machine, which is what every env command gets in
// production. Tests call the module directly with a fake.
//
// Two platform types behind one interface. They are separate packages because
// running a process and asking what operating system this is have nothing to
// do with each other; they are joined here because one command needs both.
type environment struct{}

func (environment) Run(ctx context.Context, command proc.Command) (proc.Output, error) {
	return proc.System{}.Run(ctx, command)
}

func (environment) Lookup(name string) []string { return proc.System{}.Lookup(name) }

func (environment) PathEntries() []string { return proc.System{}.PathEntries() }

func (environment) Stat(path string) (proc.Entry, error) { return proc.System{}.Stat(path) }

func (environment) Executables(directory string) []proc.Executable {
	return proc.System{}.Executables(directory)
}

func (environment) Describe() sys.Info { return sys.System{}.Describe() }

func (environment) Environ() map[string]string { return sys.System{}.Environ() }

func envSummaryText(result env.SummaryResult) output.TextFunc {
	return func(w io.Writer) error {
		machine := result.Machine
		fields := []output.Field{
			{Label: "os", Value: machine.OS + "/" + machine.Architecture},
			{Label: "cpus", Value: strconv.Itoa(machine.CPUs)},
			{Label: "hostname", Value: orUnknown(machine.Hostname)},
			{Label: "shell", Value: orUnknown(machine.Shell)},
			{Label: "terminal", Value: orUnknown(machine.Terminal)},
			{Label: "home", Value: orUnknown(machine.Home)},
			{Label: "path entries", Value: strconv.Itoa(result.PathEntries)},
			{Label: "path problems", Value: strconv.Itoa(result.PathProblems)},
			{Label: "tools found", Value: fmt.Sprintf("%d of %d",
				result.Found, result.Found+result.Missing)},
			{Label: "probed in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		return writeTools(w, result.Tools)
	}
}

// writeTools renders the toolchain listing grouped by kind, so languages do
// not read as one list of thirty unrelated programs.
func writeTools(w io.Writer, tools []env.Tool) error {
	for _, kind := range env.Kinds() {
		rows := make([][]string, 0, len(tools))
		for _, tool := range tools {
			if tool.Kind != kind {
				continue
			}
			rows = append(rows, []string{tool.Name, toolVersion(tool), tool.Path})
		}
		if len(rows) == 0 {
			continue
		}

		if _, err := fmt.Fprintf(w, "\n%s\n", kindTitle(kind)); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}
		err := output.WriteTable(w, []output.Column{
			{Title: "tool"},
			{Title: "version"},
			{Title: "path"},
		}, rows)
		if err != nil {
			return err
		}
	}
	return nil
}

// toolVersion is what a person reads in the version column: the version, or
// the reason there is not one.
func toolVersion(tool env.Tool) string {
	switch {
	case !tool.Found:
		return "not found"
	case tool.Version != "":
		return tool.Version
	case tool.Detail != "":
		return "unknown"
	default:
		return "installed"
	}
}

func kindTitle(kind env.Kind) string {
	switch kind {
	case env.KindLanguage:
		return "Languages and runtimes"
	case env.KindPackage:
		return "Package managers"
	case env.KindBuild:
		return "Build tools"
	case env.KindVersion:
		return "Version control"
	case env.KindContainer:
		return "Containers"
	case env.KindCloud:
		return "Cloud tools"
	default:
		return string(kind)
	}
}

// envToolTable is the row view shared by the summary and the listing, so
// "devnest env --output csv" and "devnest env list --output csv" produce the
// same columns.
func envToolTable(tools []env.Tool) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(tools))
		for _, tool := range tools {
			rows = append(rows, []string{
				tool.Name,
				string(tool.Kind),
				strconv.FormatBool(tool.Found),
				tool.Version,
				tool.Path,
				strconv.Itoa(len(tool.Shadowed)),
				tool.Detail,
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "tool"},
				{Title: "kind"},
				{Title: "found"},
				{Title: "version"},
				{Title: "path"},
				{Title: "shadowed", Right: true},
				{Title: "detail"},
			},
			Rows: rows,
		}
	}
}

func orUnknown(value string) string {
	if value == "" {
		return "(unknown)"
	}
	return value
}
