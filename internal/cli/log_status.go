package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/output"
)

func newLogStatusCommand() *Command {
	var top int

	return &Command{
		Name:    "status",
		Summary: "Response status code distribution",
		Usage:   "devnest log status <file> [flags]",
		Description: "Break an access log down by response status: how many 1xx, 2xx, " +
			"3xx, 4xx, and 5xx, each as a share of the total, followed by the " +
			"individual codes that came up most.\n\n" +
			"All five families are always listed, including the ones with no " +
			"requests. A summary that quietly omits 5xx leaves the reader unable to " +
			"tell whether there were none or whether the command missed them.\n\n" +
			"This reads the same collection as \"devnest log http\", so the two can " +
			"never disagree about how many requests a file holds.",
		Examples: []Example{
			{
				Command:     "devnest log status access.log",
				Description: "How much of the traffic failed, and with what.",
			},
			{
				Command:     "devnest log status access.log --top 20 --output json",
				Description: "The twenty most common codes, for a dashboard.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&top, "top", 10, "how many individual status codes to report")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := logFile(args)
			if err != nil {
				return err
			}

			result, err := log.SummarizeStatus(ctx, logReader(), log.StatusRequest{
				Path: path,
				Top:  top,
			})
			if err != nil {
				return err
			}

			warnEmptyAccess(env, result.Requests, result.Lines)

			return env.EmitTable(result, logStatusText(result), logStatusTable(result))
		},
	}
}

func logStatusText(result log.StatusResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "requests", Value: output.Count(result.Requests) + logUnparsedNote(result.Unparsed)},
			{Label: "4xx and 5xx", Value: fmt.Sprintf("%s (%s)",
				output.Count(result.Errors), logShare(logPercentOf(result.Errors, result.Requests)))},
			{Label: "read in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		if err := logWriteCounts(w, "Status classes", "class", result.Classes); err != nil {
			return err
		}
		return logWriteCounts(w, "Most common codes", "code", result.Codes)
	}
}

func logStatusTable(result log.StatusResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Classes)+len(result.Codes))
		rows = append(rows, logSectionRows("class", result.Classes)...)
		rows = append(rows, logSectionRows("code", result.Codes)...)
		return output.Table{Columns: logSectionColumns("value"), Rows: rows}
	}
}

// logPercentOf is the rendering-side share calculation, for figures the module
// reports as counts rather than as a ranked listing.
func logPercentOf(value, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(value) * 100 / float64(total)
}
