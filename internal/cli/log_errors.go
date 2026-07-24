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

func newLogErrorsCommand() *Command {
	var (
		top      int
		warnings bool
	)

	return &Command{
		Name:    "errors",
		Summary: "Failures in a log, grouped and counted",
		Usage:   "devnest log errors <file> [flags]",
		Description: "Find the failures in a log and group the repetitions together.\n\n" +
			"Two kinds of line count as a finding: one announcing a severity, which " +
			"is how an application log reports a problem, and a request that came " +
			"back 5xx, which is how an access log does. Both go through the same " +
			"grouping, because a real incident is investigated across both files and " +
			"nobody should have to remember which command reads which.\n\n" +
			"Messages are grouped by a normalised form of themselves: runs of digits " +
			"become a placeholder, so \"user 4821 not found\" and \"user 9930 not " +
			"found\" are one finding seen twice rather than two seen once. The first " +
			"and last line numbers are reported so the raw entries stay findable.\n\n" +
			"Warnings are counted but not listed unless --warnings is given. A " +
			"summary that reports every deprecation notice buries the three lines " +
			"that matter.",
		Examples: []Example{
			{
				Command:     "devnest log errors app.log",
				Description: "What failed, how often, and where to look.",
			},
			{
				Command:     "devnest log errors app.log --warnings --top 25",
				Description: "Include warning-level lines and report more of them.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&top, "top", 10, "how many distinct messages to report")
			set.BoolVar(&warnings, "warnings", false, "count warning-level lines as findings too")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := logFile(args)
			if err != nil {
				return err
			}

			result, err := log.SummarizeErrors(ctx, logReader(), log.ErrorsRequest{
				Path:            path,
				Top:             top,
				IncludeWarnings: warnings,
			})
			if err != nil {
				return err
			}

			return env.EmitTable(result, logErrorsText(result), logErrorsTable(result))
		},
	}
}

func logErrorsText(result log.ErrorsResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "lines", Value: output.Count(result.Lines)},
			{Label: "findings", Value: output.Count(result.Errors)},
			{Label: "warnings seen", Value: output.Count(result.Warnings)},
			{Label: "read in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		if err := logWriteCounts(w, "By severity", "severity", result.Severities); err != nil {
			return err
		}
		if err := logWriteCounts(w, "By category", "category", result.Categories); err != nil {
			return err
		}
		return logWriteMessages(w, result.Messages)
	}
}

// logWriteMessages renders the most common findings. The line number comes first
// because the next thing anyone does with this listing is open the file there.
func logWriteMessages(w io.Writer, messages []log.ErrorMessage) error {
	if len(messages) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\nMost common messages\n"); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}

	rows := make([][]string, 0, len(messages))
	for _, message := range messages {
		rows = append(rows, []string{
			output.Count(message.Count),
			strconv.Itoa(message.FirstLine),
			message.Category,
			message.Message,
		})
	}

	return output.WriteTable(w, []output.Column{
		{Title: "count", Right: true},
		{Title: "first line", Right: true},
		{Title: "category"},
		{Title: "message"},
	}, rows)
}

// logErrorsTable emits the findings themselves rather than the two summaries.
// The counts by severity and category are four rows each and readable in the
// terminal; the listing of messages is the part that gets sorted, filtered,
// and pasted into a ticket.
func logErrorsTable(result log.ErrorsResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Messages))
		for _, message := range result.Messages {
			rows = append(rows, []string{
				strconv.Itoa(message.Count),
				strconv.FormatFloat(message.Percent, 'f', 1, 64),
				message.Severity,
				message.Category,
				strconv.Itoa(message.FirstLine),
				strconv.Itoa(message.LastLine),
				message.Message,
			})
		}

		return output.Table{
			Columns: []output.Column{
				{Title: "count", Right: true},
				{Title: "percent", Right: true},
				{Title: "severity"},
				{Title: "category"},
				{Title: "firstLine", Right: true},
				{Title: "lastLine", Right: true},
				{Title: "message"},
			},
			Rows: rows,
		}
	}
}
