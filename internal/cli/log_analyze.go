package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/output"
)

func newLogAnalyzeCommand() *Command {
	return &Command{
		Name:    "analyze",
		Summary: "Size, line count, and detected type of a log file",
		Usage:   "devnest log analyze <file> [flags]",
		Description: "Report what a log file is: how big, how many lines, how many of " +
			"them are blank, what format it appears to be in, and how long the read " +
			"took.\n\n" +
			"The type is detected from the first two hundred non-blank lines and the " +
			"result says how many it looked at, so you can judge the guess. A file " +
			"whose lines mostly parse as access-log entries is reported as " +
			"http-access; one where a third of the lines announce a severity is " +
			"application; one of JSON objects is json-lines; anything else is text.\n\n" +
			"This is the command to run first. It reads the whole file, so the timing " +
			"it reports is also a fair estimate of what the other log commands will " +
			"cost on the same file.",
		Examples: []Example{
			{
				Command:     "devnest log analyze /var/log/nginx/access.log",
				Description: "Size, line count, and detected format.",
			},
			{
				Command:     "devnest log analyze app.log --output json",
				Description: "The same figures for a script.",
			},
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := logFile(args)
			if err != nil {
				return err
			}

			result, err := log.Analyze(ctx, logReader(), log.AnalyzeRequest{Path: path})
			if err != nil {
				return err
			}

			return env.EmitTable(result, logAnalyzeText(result), logAnalyzeTable(result))
		},
	}
}

func logAnalyzeText(result log.AnalyzeResult) output.TextFunc {
	return func(w io.Writer) error {
		return output.WriteFields(w, []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "size", Value: output.Bytes(result.Bytes)},
			{Label: "lines", Value: output.Count(result.Lines)},
			{Label: "blank lines", Value: output.Count(result.Blank)},
			{Label: "type", Value: fmt.Sprintf("%s (from %s sampled lines)",
				result.Type, output.Count(result.Sampled))},
			{Label: "over-long lines", Value: output.Count(result.LongLines)},
			{Label: "read in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		})
	}
}

// logAnalyzeTable is one row, because the result is one file. It is still worth
// having: a loop over a directory of logs appending to one CSV is exactly the
// use this shape serves.
func logAnalyzeTable(result log.AnalyzeResult) output.TableFunc {
	return func() output.Table {
		return output.Table{
			Columns: []output.Column{
				{Title: "file"},
				{Title: "bytes", Right: true},
				{Title: "lines", Right: true},
				{Title: "blankLines", Right: true},
				{Title: "type"},
				{Title: "durationMs", Right: true},
			},
			Rows: [][]string{{
				result.Path,
				strconv.FormatInt(result.Bytes, 10),
				strconv.Itoa(result.Lines),
				strconv.Itoa(result.Blank),
				result.Type,
				strconv.FormatInt(result.DurationMs, 10),
			}},
		}
	}
}
