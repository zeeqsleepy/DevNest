package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/scan"
	"github.com/devnest/devnest/internal/output"
)

func newScanTypesCommand() *Command {
	var (
		selection  scanFlags
		limit      int
		byLanguage bool
	)

	return &Command{
		Name:    "types",
		Summary: "File counts and sizes by extension or language",
		Usage:   "devnest scan types [path] [flags]",
		Description: "Break a tree down by file type: how many of each, how much space " +
			"each accounts for, and what share of the files that is.\n\n" +
			"By extension, which is what \"what is this project written in\" usually " +
			"means in practice. --by-language folds .js, .mjs, .cjs, and .jsx into " +
			"one row and is the more honest answer, at the cost of not showing you " +
			"that somebody is still writing .cjs.\n\n" +
			"Files whose language is not recognised are counted and reported as a " +
			"total rather than dropped. A large number there means the language " +
			"table is missing something this project uses, which is worth knowing.",
		Examples: []Example{
			{
				Command:     "devnest scan types",
				Description: "Every extension in this project, most common first.",
			},
			{
				Command:     "devnest scan types ~/work/api --by-language --limit 5",
				Description: "The five biggest languages, folding extensions together.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.register(set)
			set.IntVar(&limit, "limit", 0, "how many entries to report (0 means all)")
			set.BoolVar(&byLanguage, "by-language", false,
				"group by detected language instead of by extension")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			result, err := scan.Types(ctx, filesystem(), scan.TypesRequest{
				Selection:  selection.selection(env, firstPath(args)),
				Limit:      limit,
				ByLanguage: byLanguage,
			})
			if err != nil {
				return err
			}

			reportScanProblems(env, result.Problems)
			return env.EmitTable(result, scanTypesText(result), scanTypesTable(result))
		},
	}
}

func scanTypesText(result scan.TypesResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "root", Value: result.Root},
			{Label: "files", Value: output.Count(result.Files)},
			{Label: "size", Value: output.Bytes(result.Bytes)},
			{Label: "grouped by", Value: result.Subject},
		}
		if result.Unrecognised > 0 {
			fields = append(fields, output.Field{
				Label: "unrecognised",
				Value: fmt.Sprintf("%s files", output.Count(result.Unrecognised)),
			})
		}
		fields = append(fields, output.Field{
			Label: "scanned in", Value: fmt.Sprintf("%d ms", result.DurationMs),
		})

		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		return writeScanCounts(w, "Breakdown", result.Subject, result.Entries)
	}
}

func scanTypesTable(result scan.TypesResult) output.TableFunc {
	return func() output.Table {
		return output.Table{
			Columns: scanCountColumns(),
			Rows:    scanCountRows(result.Subject, result.Entries),
		}
	}
}
