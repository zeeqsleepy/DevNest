package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/output"
)

func newLogTopCommand() *Command {
	var (
		limit   int
		clients bool
	)

	return &Command{
		Name:    "top",
		Summary: "Most requested endpoints",
		Usage:   "devnest log top <file> [flags]",
		Description: "List the endpoints an access log saw most often, with their " +
			"request counts and their share of the traffic.\n\n" +
			"Query strings are stripped before counting: /search?q=cats and " +
			"/search?q=dogs are the same endpoint. Without that, a busy search page " +
			"looks like thousands of unrelated URLs and never appears in the " +
			"listing at all.\n\n" +
			"--clients ranks client addresses instead, from the same single pass over " +
			"the file.",
		Examples: []Example{
			{
				Command:     "devnest log top access.log --limit 20",
				Description: "The twenty busiest endpoints.",
			},
			{
				Command:     "devnest log top access.log --clients",
				Description: "The addresses that made the most requests.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&limit, "limit", 10, "how many entries to report")
			set.IntVar(&limit, "n", 10, "how many entries to report (shorthand)")
			set.BoolVar(&clients, "clients", false, "rank client addresses instead of endpoints")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := logFile(args)
			if err != nil {
				return err
			}

			result, err := log.TopRequests(ctx, logReader(), log.TopRequest{
				Path:    path,
				Limit:   limit,
				Clients: clients,
			})
			if err != nil {
				return err
			}

			warnEmptyAccess(env, result.Requests, result.Lines)
			warnTruncatedRanking(env, result.RankingTruncated)

			return env.EmitTable(result, logTopText(result), logTopTable(result))
		},
	}
}

func logTopText(result log.TopResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "requests", Value: output.Count(result.Requests) + logUnparsedNote(result.Unparsed)},
			{Label: "unique " + result.Subject + "s", Value: output.Count(result.Unique)},
			{Label: "read in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		return logWriteCounts(w, "Most requested", result.Subject, result.Entries)
	}
}

func logTopTable(result log.TopResult) output.TableFunc {
	return func() output.Table {
		return output.Table{
			Columns: logPlainCountColumns(result.Subject),
			Rows:    logPlainCountRows(result.Entries),
		}
	}
}
