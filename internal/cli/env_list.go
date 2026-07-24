package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/devnest/devnest/internal/core/env"
	"github.com/devnest/devnest/internal/output"
)

func newEnvListCommand() *Command {
	var (
		timeout time.Duration
		missing bool
		only    repeatable
	)

	return &Command{
		Name:    "list",
		Summary: "Detected toolchains with versions and paths",
		Usage:   "devnest env list [flags]",
		Description: "List the toolchains installed on this machine, with the version " +
			"each one reports and the location that would run.\n\n" +
			"A tool is looked up on PATH first and only run if it is there, so a " +
			"machine with three of the thirty starts three processes. Every probe is " +
			"bounded by --timeout and runs without a shell.\n\n" +
			"--tool restricts the run to the names you give, including names the " +
			"built-in table has never heard of: those are located but not run, " +
			"because inventing a version flag for an unknown program is how a " +
			"listing turns into something with side effects.\n\n" +
			"A tool that is installed and shadowed lists the copies that lose. That " +
			"is the finding that explains most reports of an unexpected version.",
		Examples: []Example{
			{
				Command:     "devnest env list",
				Description: "Everything installed, with versions.",
			},
			{
				Command:     "devnest env list --tool go --tool node --output json",
				Description: "Check two specific toolchains, for a setup script.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.DurationVar(&timeout, "timeout", 0,
				"how long to wait for each tool to report its version")
			set.BoolVar(&missing, "missing", false, "include tools that were not found")
			set.Var(&only, "tool", "restrict detection to this tool (repeatable)")
		},
		Run: func(ctx context.Context, cliEnv *Env, args []string) error {
			if err := noArguments(args, "devnest env list"); err != nil {
				return err
			}

			result, err := env.List(ctx, environment{}, env.ListRequest{
				Only:           only,
				IncludeMissing: missing,
				Timeout:        timeout,
			})
			if err != nil {
				return err
			}

			return cliEnv.EmitTable(result, envListText(result), envToolTable(result.Tools))
		},
	}
}

func envListText(result env.ListResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "found", Value: output.Count(result.Found)},
			{Label: "not found", Value: output.Count(result.Missing)},
			{Label: "probed in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		if err := writeTools(w, result.Tools); err != nil {
			return err
		}
		return writeShadowedTools(w, result.Tools)
	}
}

// writeShadowedTools lists the copies that lose, which is the whole reason
// anybody runs this command twice.
func writeShadowedTools(w io.Writer, tools []env.Tool) error {
	rows := make([][]string, 0, len(tools))
	for _, tool := range tools {
		for _, hidden := range tool.Shadowed {
			rows = append(rows, []string{tool.Name, hidden})
		}
	}
	if len(rows) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nShadowed copies (these do not run)\n"); err != nil {
		return err
	}
	return output.WriteTable(w, []output.Column{
		{Title: "tool"},
		{Title: "hidden copy"},
	}, rows)
}
