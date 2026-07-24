package cli

import (
	"context"
	"flag"
	"io"
	"strconv"

	"github.com/devnest/devnest/internal/core/env"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newEnvVarsCommand() *Command {
	var (
		all    bool
		reveal bool
	)

	return &Command{
		Name:    "vars",
		Summary: "Development-relevant environment variables",
		Usage:   "devnest env vars [pattern] [flags]",
		Description: "List environment variables that matter to development: toolchain " +
			"settings, proxies, editors, locale, and the paths.\n\n" +
			"A pattern filters by name, matched as a substring or a glob and without " +
			"regard to case. --all lists everything, which on a desktop is two " +
			"hundred entries of which four are interesting.\n\n" +
			"**Values whose name looks like a credential are hidden**, and hidden in " +
			"the result itself rather than in one rendering of it. A listing gets " +
			"redirected to a file and attached to a ticket, and masking the table " +
			"while leaving the JSON readable would be a leak with a delay on it. " +
			"What is shown in place of the value is its length, not a prefix: a " +
			"prefix is enough to identify which key it is.\n\n" +
			"--reveal prints them in full, for when you are looking at your own " +
			"machine and know what is on your screen.",
		Examples: []Example{
			{
				Command:     "devnest env vars",
				Description: "The variables worth knowing about, with secrets hidden.",
			},
			{
				Command:     "devnest env vars GO",
				Description: "Everything whose name mentions GO.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&all, "all", false, "list every variable, not only the relevant ones")
			set.BoolVar(&reveal, "reveal", false, "show credential-shaped values in full")
		},
		Run: func(ctx context.Context, cliEnv *Env, args []string) error {
			if len(args) > 1 {
				return errors.New(errors.CodeInvalidInput,
					"expected at most one pattern, found %d arguments", len(args)).
					WithHint("quote a pattern containing spaces or wildcards")
			}

			pattern := ""
			if len(args) == 1 {
				pattern = args[0]
			}
			if reveal {
				cliEnv.Warn(errors.CodeInvalidInput,
					"--reveal prints credential-shaped values in full; "+
						"redirecting this output writes them to a file")
			}

			result, err := env.Vars(ctx, environment{}, env.VarsRequest{
				Pattern: pattern,
				All:     all,
				Reveal:  reveal,
			})
			if err != nil {
				return err
			}

			return cliEnv.EmitTable(result, envVarsText(result), envVarsTable(result))
		},
	}
}

func envVarsText(result env.VarsResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "variables", Value: output.Count(result.Total)},
			{Label: "hidden", Value: output.Count(result.Masked)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		if result.Total == 0 {
			return nil
		}

		rows := make([][]string, 0, len(result.Variables))
		for _, variable := range result.Variables {
			rows = append(rows, []string{variable.Name, varValue(variable)})
		}
		return output.WriteTable(w, []output.Column{
			{Title: "name"},
			{Title: "value"},
		}, rows)
	}
}

// varValue is what a person reads. A list-shaped variable is reported by its
// length rather than printed: PATH on a developer machine is two thousand
// characters, and pasting it into the middle of a table helps nobody.
func varValue(variable env.Variable) string {
	if variable.Entries > 0 {
		return "(" + strconv.Itoa(variable.Entries) + " entries, see \"devnest env path\")"
	}
	return variable.Value
}

func envVarsTable(result env.VarsResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Variables))
		for _, variable := range result.Variables {
			rows = append(rows, []string{
				variable.Name,
				variable.Value,
				strconv.FormatBool(variable.Masked),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "name"},
				{Title: "value"},
				{Title: "hidden"},
			},
			Rows: rows,
		}
	}
}

// noArguments rejects positional arguments on a command that takes none.
func noArguments(args []string, command string) error {
	if len(args) == 0 {
		return nil
	}
	return errors.New(errors.CodeInvalidInput,
		"%q takes no arguments, found %q", command, args[0]).
		WithHint("run \"%s --help\" to see the available flags", command)
}
