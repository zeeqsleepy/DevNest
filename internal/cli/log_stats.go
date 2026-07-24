package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newLogStatsCommand() *Command {
	var top int

	return &Command{
		Name:    "stats",
		Summary: "Line length statistics and the longest lines",
		Usage:   "devnest log stats <file> [flags]",
		Description: "Measure the lines of a log file: how many, how long on average, " +
			"the longest and shortest, and where the longest ones are.\n\n" +
			"This is the command for \"why is this file eight gigabytes\". The answer " +
			"is usually a handful of lines with a serialised payload in them, and " +
			"their line numbers are what makes them findable.\n\n" +
			"The average and the shortest line are taken over lines that hold " +
			"something. Blank lines are counted and reported separately: the shortest " +
			"line in almost every log file is empty, and reporting zero answers " +
			"nothing.",
		Examples: []Example{
			{
				Command:     "devnest log stats app.log",
				Description: "Line counts, averages, and the ten longest lines.",
			},
			{
				Command:     "devnest log stats app.log --top 3",
				Description: "Just the three worst offenders.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&top, "top", 10, "how many of the longest lines to report")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := logFile(args)
			if err != nil {
				return err
			}

			result, err := log.Stats(ctx, logReader(), log.StatsRequest{Path: path, Top: top})
			if err != nil {
				return err
			}

			return env.EmitTable(result, logStatsText(result), logStatsTable(result))
		},
	}
}

func logStatsText(result log.StatsResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "size", Value: output.Bytes(result.Bytes)},
			{Label: "lines", Value: output.Count(result.Lines)},
			{Label: "blank lines", Value: output.Count(result.Blank)},
			{Label: "average line", Value: fmt.Sprintf("%.1f bytes", result.AverageLineBytes)},
			{Label: "longest line", Value: fmt.Sprintf("%s bytes", output.Count(result.LongestLineBytes))},
			{Label: "shortest line", Value: fmt.Sprintf("%s bytes (line %s)",
				output.Count(result.ShortestLineBytes), output.Count(result.ShortestLine))},
			{Label: "over-long lines", Value: output.Count(result.LongLines)},
			{Label: "read in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		if len(result.LongestLines) == 0 {
			return nil
		}

		if _, err := fmt.Fprintf(w, "\nLongest lines\n"); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}

		rows := make([][]string, 0, len(result.LongestLines))
		for _, line := range result.LongestLines {
			rows = append(rows, []string{
				strconv.Itoa(line.Line),
				output.Count(line.Bytes),
				line.Text,
			})
		}
		return output.WriteTable(w, []output.Column{
			{Title: "line", Right: true},
			{Title: "bytes", Right: true},
			{Title: "excerpt"},
		}, rows)
	}
}

func logStatsTable(result log.StatsResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.LongestLines))
		for _, line := range result.LongestLines {
			rows = append(rows, []string{
				strconv.Itoa(line.Line),
				strconv.Itoa(line.Bytes),
				line.Text,
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "line", Right: true},
				{Title: "bytes", Right: true},
				{Title: "excerpt"},
			},
			Rows: rows,
		}
	}
}
