package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/core/env"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newEnvPathCommand() *Command {
	var shadows bool

	return &Command{
		Name:    "path",
		Summary: "PATH entries with problems flagged",
		Usage:   "devnest env path [flags]",
		Description: "List the directories on PATH in order, flagging the ones that are " +
			"listed twice, point at nothing, or point at a file.\n\n" +
			"--shadows adds the expensive check: reading every directory on PATH to " +
			"find executables resolvable from more than one of them. That is the " +
			"finding behind most reports of \"but I installed the new version\", and " +
			"it is off by default because it is the only part that costs anything.\n\n" +
			"A problem here is a finding, not a failure. A PATH with three dead " +
			"entries works perfectly well, and the command's job is to say so rather " +
			"than to refuse.",
		Examples: []Example{
			{
				Command:     "devnest env path",
				Description: "The order PATH is searched in, with dead entries flagged.",
			},
			{
				Command:     "devnest env path --shadows",
				Description: "Find executables that exist in more than one place.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&shadows, "shadows", false,
				"find executables resolvable from more than one entry")
		},
		Run: func(ctx context.Context, cliEnv *Env, args []string) error {
			if err := noArguments(args, "devnest env path"); err != nil {
				return err
			}

			result, err := env.InspectPath(ctx, environment{}, env.PathRequest{Shadows: shadows})
			if err != nil {
				return err
			}

			return cliEnv.EmitTable(result, envPathText(result), envPathTable(result))
		},
	}
}

func envPathText(result env.PathResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "entries", Value: output.Count(len(result.Entries))},
			{Label: "problems", Value: output.Count(result.Problems)},
		}
		if len(result.Shadowed) > 0 {
			fields = append(fields, output.Field{
				Label: "shadowed", Value: output.Count(len(result.Shadowed)),
			})
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		if _, err := fmt.Fprintln(w); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}

		rows := make([][]string, 0, len(result.Entries))
		for _, entry := range result.Entries {
			rows = append(rows, []string{
				strconv.Itoa(entry.Position),
				entry.Path,
				strconv.Itoa(entry.Executables),
				problemText(entry.Problems),
			})
		}
		err := output.WriteTable(w, []output.Column{
			{Title: "#", Right: true},
			{Title: "directory"},
			{Title: "files", Right: true},
			{Title: "problems"},
		}, rows)
		if err != nil {
			return err
		}

		return writeShadows(w, result.Shadowed)
	}
}

// writeShadows lists each shadowed name with the copy that wins and the copies
// that do not.
func writeShadows(w io.Writer, shadowed []env.Shadow) error {
	if len(shadowed) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nShadowed executables\n"); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}

	rows := make([][]string, 0, len(shadowed))
	for _, shadow := range shadowed {
		rows = append(rows, []string{
			shadow.Name,
			shadow.Winner,
			strconv.Itoa(len(shadow.Hidden)),
		})
	}
	return output.WriteTable(w, []output.Column{
		{Title: "name"},
		{Title: "runs from"},
		{Title: "hidden", Right: true},
	}, rows)
}

func problemText(problems []env.PathProblem) string {
	if len(problems) == 0 {
		return "ok"
	}
	names := make([]string, 0, len(problems))
	for _, problem := range problems {
		names = append(names, string(problem))
	}
	return strings.Join(names, ", ")
}

func envPathTable(result env.PathResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Entries))
		for _, entry := range result.Entries {
			rows = append(rows, []string{
				strconv.Itoa(entry.Position),
				entry.Path,
				strconv.Itoa(entry.Executables),
				problemText(entry.Problems),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "position", Right: true},
				{Title: "directory"},
				{Title: "executables", Right: true},
				{Title: "problems"},
			},
			Rows: rows,
		}
	}
}
