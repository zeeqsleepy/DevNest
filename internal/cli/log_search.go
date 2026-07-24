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

func newLogSearchCommand() *Command {
	var (
		ignoreCase bool
		limit      int
	)

	return &Command{
		Name:    "search",
		Summary: "Find the lines of a log containing a keyword",
		Usage:   "devnest log search <file> <keyword> [flags]",
		Description: "Find every line of a log that contains a keyword, and report it " +
			"with its line number.\n\n" +
			"Matching is on plain text and is case-sensitive by default, which is what " +
			"you want when the keyword is an identifier. --ignore-case folds both " +
			"sides.\n\n" +
			"This is not a regular expression engine, deliberately. A log search is " +
			"nearly always for a request identifier, an address, or a user name, and " +
			"the one time it is not, grep is already installed. Keeping it to a " +
			"substring is what keeps it fast enough to be the obvious thing to " +
			"reach for.\n\n" +
			"The whole file is always read, so the match count is the real one even " +
			"when the listing stops at --limit. A listing cut short says so.",
		Examples: []Example{
			{
				Command:     "devnest log search app.log \"connection refused\"",
				Description: "Every line mentioning the phrase, with line numbers.",
			},
			{
				Command:     "devnest log search access.log 500 --limit 20 --output csv",
				Description: "The first twenty matches as rows.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&ignoreCase, "ignore-case", false, "match without regard to case")
			set.BoolVar(&ignoreCase, "i", false, "match without regard to case (shorthand)")
			set.IntVar(&limit, "limit", 100, "how many matching lines to report")
			set.IntVar(&limit, "n", 100, "how many matching lines to report (shorthand)")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if len(args) != 2 {
				return errors.New(errors.CodeInvalidInput,
					"expected a log file and a keyword, found %d argument(s)", len(args)).
					WithHint("run \"devnest log search --help\" for usage")
			}

			result, err := log.Search(ctx, logReader(), log.SearchRequest{
				Path:       args[0],
				Query:      args[1],
				IgnoreCase: ignoreCase,
				Limit:      limit,
			})
			if err != nil {
				return err
			}

			if err := env.EmitTable(result, logSearchText(result), logSearchTable(result)); err != nil {
				return err
			}

			// Nothing found is a legitimate answer, and scripts branch on it.
			// It is not a failure of the command, so the code says "not found"
			// rather than "error".
			if result.Matches == 0 {
				return errors.New(errors.CodeNotFound,
					"%q was not found in %s", result.Query, result.Path)
			}
			return nil
		},
	}
}

func logSearchText(result log.SearchResult) output.TextFunc {
	return func(w io.Writer) error {
		listed := len(result.Results)
		matches := output.Count(result.Matches)
		if result.Limited {
			matches = fmt.Sprintf("%s (showing the first %s)", matches, output.Count(listed))
		}

		fields := []output.Field{
			{Label: "file", Value: result.Path},
			{Label: "keyword", Value: result.Query},
			{Label: "lines searched", Value: output.Count(result.Lines)},
			{Label: "matches", Value: matches},
			{Label: "read in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		if listed == 0 {
			return nil
		}

		if _, err := fmt.Fprintln(w); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}

		rows := make([][]string, 0, listed)
		for _, match := range result.Results {
			rows = append(rows, []string{strconv.Itoa(match.Line), match.Text})
		}
		return output.WriteTable(w, []output.Column{
			{Title: "line", Right: true},
			{Title: "text"},
		}, rows)
	}
}

func logSearchTable(result log.SearchResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Results))
		for _, match := range result.Results {
			rows = append(rows, []string{strconv.Itoa(match.Line), match.Text})
		}
		return output.Table{
			Columns: []output.Column{{Title: "line", Right: true}, {Title: "text"}},
			Rows:    rows,
		}
	}
}
