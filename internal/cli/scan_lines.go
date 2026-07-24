package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/devnest/devnest/internal/core/scan"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newScanLinesCommand() *Command {
	var (
		selection scanFlags
		limit     int
		maxSize   int64
	)

	return &Command{
		Name:    "lines",
		Summary: "Line counts, split into code, comment, and blank",
		Usage:   "devnest scan lines [path] [flags]",
		Description: "Count the lines in a project, split into code, comment, and " +
			"blank, and grouped by language.\n\n" +
			"Only files in a recognised language are opened. A .png has no lines, and " +
			"reading every binary in a tree to establish that is the slowest possible " +
			"way to learn nothing. Files above --max-file-size are counted and " +
			"skipped: a minified bundle or a checked-in dump is not something a " +
			"person wrote.\n\n" +
			"The comment detection is deliberately simple. A line whose first " +
			"characters open a comment is a comment, and a block comment runs to its " +
			"terminator. It does not parse the language, so a comment marker inside a " +
			"string literal counts as a comment. Parsing forty languages properly " +
			"means forty parsers to maintain, and these numbers are used to compare " +
			"parts of a tree with each other, where a small consistent error cancels " +
			"out.",
		Examples: []Example{
			{
				Command:     "devnest scan lines",
				Description: "How much code is in this project, by language.",
			},
			{
				Command:     "devnest scan lines ~/work/api --output csv",
				Description: "The same numbers as rows, for a spreadsheet.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.register(set)
			set.IntVar(&limit, "limit", 0, "how many languages to report (0 means all)")
			set.Var(newSizeValue(&maxSize, 0), "max-file-size",
				"skip files larger than this, for example 1MB")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			result, err := scan.Lines(ctx, filesystem(), scan.LinesRequest{
				Selection:    selection.selection(env, firstPath(args)),
				Limit:        limit,
				MaxFileBytes: maxSize,
			})
			if err != nil {
				return err
			}

			reportScanProblems(env, result.Problems)
			return env.EmitTable(result, scanLinesText(result), scanLinesTable(result))
		},
	}
}

func scanLinesText(result scan.LinesResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "root", Value: result.Root},
			{Label: "files counted", Value: output.Count(result.Files - result.Skipped)},
			{Label: "files skipped", Value: output.Count(result.Skipped)},
			{Label: "total lines", Value: output.Count(result.Total)},
			{Label: "code", Value: linesShare(result.Code, result.Total)},
			{Label: "comment", Value: linesShare(result.Comment, result.Total)},
			{Label: "blank", Value: linesShare(result.Blank, result.Total)},
			{Label: "counted in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		if len(result.Languages) == 0 {
			return nil
		}

		if _, err := fmt.Fprintf(w, "\nBy language\n"); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}

		rows := make([][]string, 0, len(result.Languages))
		for _, language := range result.Languages {
			rows = append(rows, []string{
				language.Language,
				output.Count(language.Files),
				output.Count(language.Code),
				output.Count(language.Comment),
				output.Count(language.Blank),
				output.Count(language.Total),
			})
		}
		return output.WriteTable(w, []output.Column{
			{Title: "language"},
			{Title: "files", Right: true},
			{Title: "code", Right: true},
			{Title: "comment", Right: true},
			{Title: "blank", Right: true},
			{Title: "total", Right: true},
		}, rows)
	}
}

// linesShare renders a count with its share of the total, because "412,000
// lines of comment" means nothing without knowing what the total was.
func linesShare(value, total int) string {
	if total <= 0 {
		return output.Count(value)
	}
	return fmt.Sprintf("%s (%.1f%%)", output.Count(value), float64(value)*100/float64(total))
}

func scanLinesTable(result scan.LinesResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Languages))
		for _, language := range result.Languages {
			rows = append(rows, []string{
				language.Language,
				strconv.Itoa(language.Files),
				strconv.Itoa(language.Code),
				strconv.Itoa(language.Comment),
				strconv.Itoa(language.Blank),
				strconv.Itoa(language.Total),
				strconv.FormatInt(language.Bytes, 10),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "language"},
				{Title: "files", Right: true},
				{Title: "code", Right: true},
				{Title: "comment", Right: true},
				{Title: "blank", Right: true},
				{Title: "total", Right: true},
				{Title: "bytes", Right: true},
			},
			Rows: rows,
		}
	}
}
