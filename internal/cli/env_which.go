package cli

import (
	"context"
	"flag"
	"io"
	"strconv"
	"time"

	"github.com/devnest/devnest/internal/core/env"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newEnvWhichCommand() *Command {
	var (
		versions bool
		timeout  time.Duration
	)

	return &Command{
		Name:    "which",
		Summary: "Every location a tool resolves from, in PATH order",
		Usage:   "devnest env which <tool> [flags]",
		Description: "Show every place a name resolves to, in the order PATH is " +
			"searched, with the copy that actually runs marked first.\n\n" +
			"This differs from the shell built-in on purpose. The built-in gives you " +
			"the winner, and the winner is the one piece of information that does not " +
			"help when a tool reports a version nobody expected. What helps is seeing " +
			"the other three copies and which directory each came from.\n\n" +
			"--versions runs each copy to report what it says it is, which is how you " +
			"settle an argument about which one is old. Only tools the built-in table " +
			"describes are run: an unknown program gets located, never invoked.\n\n" +
			"**Exits 3 when the name resolves to nothing**, so a setup script can " +
			"branch on it without parsing anything.",
		Examples: []Example{
			{
				Command:     "devnest env which go",
				Description: "Where go runs from, and what else is called go.",
			},
			{
				Command:     "devnest env which python --versions",
				Description: "Every python on PATH and the version each one reports.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&versions, "versions", false, "run each copy to report its version")
			set.DurationVar(&timeout, "timeout", 0, "how long to wait for each version probe")
		},
		Run: func(ctx context.Context, cliEnv *Env, args []string) error {
			if len(args) != 1 {
				return errors.New(errors.CodeInvalidInput,
					"expected one tool name, found %d arguments", len(args)).
					WithHint("run \"devnest env which go\" for an example")
			}

			result, err := env.Which(ctx, environment{}, env.WhichRequest{
				Name:    args[0],
				Version: versions,
				Timeout: timeout,
			})
			if err != nil {
				return err
			}

			if err := cliEnv.EmitTable(result, envWhichText(result), envWhichTable(result)); err != nil {
				return err
			}

			// Nothing found is a legitimate answer that scripts branch on, so
			// it gets the not-found code rather than a general failure.
			if len(result.Locations) == 0 {
				return errors.New(errors.CodeNotFound, "%s is not on PATH", result.Name)
			}
			return nil
		},
	}
}

func envWhichText(result env.WhichResult) output.TextFunc {
	return func(w io.Writer) error {
		verdict := "not on PATH"
		switch {
		case result.Shadowed:
			verdict = "found in " + strconv.Itoa(len(result.Locations)) + " places"
		case len(result.Locations) == 1:
			verdict = "found"
		}

		fields := []output.Field{
			{Label: "tool", Value: result.Name},
			{Label: "result", Value: verdict},
		}
		if result.Winner != "" {
			fields = append(fields, output.Field{Label: "runs from", Value: result.Winner})
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		if len(result.Locations) == 0 {
			return nil
		}

		rows := make([][]string, 0, len(result.Locations))
		for _, location := range result.Locations {
			note := location.Version
			if note == "" {
				note = location.Detail
			}
			rows = append(rows, []string{strconv.Itoa(location.Position), location.Path, note})
		}
		return output.WriteTable(w, []output.Column{
			{Title: "#", Right: true},
			{Title: "path"},
			{Title: "version"},
		}, rows)
	}
}

func envWhichTable(result env.WhichResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Locations))
		for _, location := range result.Locations {
			rows = append(rows, []string{
				result.Name,
				strconv.Itoa(location.Position),
				location.Path,
				location.Version,
				location.Detail,
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "tool"},
				{Title: "position", Right: true},
				{Title: "path"},
				{Title: "version"},
				{Title: "detail"},
			},
			Rows: rows,
		}
	}
}
