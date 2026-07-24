package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/output"
)

func newLogHTTPCommand() *Command {
	var top int

	return &Command{
		Name:    "http",
		Summary: "Summarise an HTTP access log",
		Usage:   "devnest log http <file> [flags]",
		Description: "Summarise a web server access log: how many requests, which " +
			"methods, what came back, which endpoints were busiest, which clients " +
			"were loudest, and how large the responses were.\n\n" +
			"The Common and Combined Log Formats are understood, which is what nginx " +
			"and Apache write by default. Query strings are stripped before endpoints " +
			"are counted, so /search?q=cats and /search?q=dogs are one endpoint rather " +
			"than two requests that look unrelated.\n\n" +
			"The average response size is taken over the responses that carried a " +
			"body. Including the 304s would drag it towards zero and make a working " +
			"cache look like a server that stopped sending anything.",
		Examples: []Example{
			{
				Command:     "devnest log http /var/log/nginx/access.log",
				Description: "Full summary of a day's traffic.",
			},
			{
				Command:     "devnest log http access.log --top 25 --output csv",
				Description: "The top twenty-five of each listing, as rows for a spreadsheet.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&top, "top", 10, "how many entries each ranked listing reports")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := logFile(args)
			if err != nil {
				return err
			}

			result, err := log.SummarizeHTTP(ctx, logReader(), log.HTTPRequest{
				Path: path,
				Top:  top,
			})
			if err != nil {
				return err
			}

			warnEmptyAccess(env, result.Requests, result.Lines)
			warnTruncatedRanking(env, result.RankingTruncated)

			return env.EmitTable(result, logHTTPText(result), logHTTPTable(result))
		},
	}
}

func logHTTPText(result log.HTTPResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "size", Value: output.Bytes(result.Bytes)},
			{Label: "lines", Value: output.Count(result.Lines)},
			{Label: "requests", Value: output.Count(result.Requests) + logUnparsedNote(result.Unparsed)},
			{Label: "unique clients", Value: output.Count(result.UniqueIPs)},
			{Label: "response bytes", Value: output.Bytes(result.TotalResponseBytes)},
			{Label: "average response", Value: output.Bytes(result.AverageResponseBytes)},
			{Label: "read in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		sections := []struct {
			title   string
			subject string
			counts  []log.Count
		}{
			{"Methods", "method", result.Methods},
			{"Status classes", "class", result.StatusClasses},
			{"Status codes", "code", result.StatusCodes},
			{"Top endpoints", "endpoint", result.TopPaths},
			{"Top clients", "client", result.TopClients},
		}
		for _, section := range sections {
			if err := logWriteCounts(w, section.title, section.subject, section.counts); err != nil {
				return err
			}
		}
		return nil
	}
}

// logHTTPTable flattens the five listings into one set of rows tagged with the
// section they came from. A CSV file per listing would mean five files or five
// runs; one file with a section column is what a spreadsheet filters on.
func logHTTPTable(result log.HTTPResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Methods)+len(result.StatusClasses)+
			len(result.StatusCodes)+len(result.TopPaths)+len(result.TopClients))

		rows = append(rows, logSectionRows("method", result.Methods)...)
		rows = append(rows, logSectionRows("class", result.StatusClasses)...)
		rows = append(rows, logSectionRows("status", result.StatusCodes)...)
		rows = append(rows, logSectionRows("endpoint", result.TopPaths)...)
		rows = append(rows, logSectionRows("client", result.TopClients)...)

		return output.Table{Columns: logSectionColumns("value"), Rows: rows}
	}
}
