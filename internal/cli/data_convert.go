package cli

import (
	"context"
	"encoding/csv"
	"flag"
	"io"

	"github.com/devnest/devnest/internal/core/data"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newJSONToYAMLCommand() *Command {
	var useStdin bool

	return &Command{
		Name:    "to-yaml",
		Summary: "Convert JSON to YAML",
		Usage:   "devnest json to-yaml <file> [flags]",
		Description: "Print a JSON document as YAML.\n\n" +
			"Keys keep the order they were written in, which matters here more than " +
			"anywhere else: a converted file is usually about to be committed, and a " +
			"version with the keys rearranged is unreviewable.\n\n" +
			"The file is not modified; redirect the output to write the result.",
		Examples: []Example{
			{
				Command:     "devnest json to-yaml config.json",
				Description: "Print the document as YAML.",
			},
			{
				Command:     "devnest json to-yaml config.json > config.yaml",
				Description: "Write the converted document to a file.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&useStdin, "stdin", false, "read the document from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			request, err := dataRequest(env, args, useStdin)
			if err != nil {
				return err
			}

			result, err := data.ToYAML(dataReader(), data.ConvertRequest{Request: request})
			if err != nil {
				return err
			}

			return env.Emit(result, writeDocument(result.Output))
		},
	}
}

func newYAMLToJSONCommand() *Command {
	var (
		indent   int
		useStdin bool
	)

	return &Command{
		Name:    "to-json",
		Summary: "Convert YAML to JSON",
		Usage:   "devnest yaml to-json <file> [flags]",
		Description: "Print a YAML document as JSON.\n\n" +
			"A file holding several documents becomes a JSON array, because JSON has " +
			"one top-level value and a stream of documents has to become something. " +
			"Anchors and aliases are resolved on the way through, since JSON has no " +
			"way to express them, and comments are lost, since JSON has no way to hold " +
			"them either.\n\n" +
			"The file is not modified; redirect the output to write the result.",
		Examples: []Example{
			{
				Command:     "devnest yaml to-json docker-compose.yml",
				Description: "Print the document as JSON.",
			},
			{
				Command:     "devnest yaml to-json manifest.yaml --indent 4",
				Description: "Convert a multi-document manifest into a JSON array.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&indent, "indent", 0, "spaces per level (default 2)")
			set.BoolVar(&useStdin, "stdin", false, "read the document from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			request, err := dataRequest(env, args, useStdin)
			if err != nil {
				return err
			}

			result, err := data.ToJSON(dataReader(), data.ConvertRequest{
				Request: request,
				Indent:  indent,
			})
			if err != nil {
				return err
			}

			return env.Emit(result, writeDocument(result.Output))
		},
	}
}

func newJSONToCSVCommand() *Command {
	var (
		flatten  bool
		useStdin bool
	)

	return &Command{
		Name:    "to-csv",
		Summary: "Convert JSON to CSV",
		Usage:   "devnest json to-csv <file> [flags]",
		Description: "Print a JSON document as CSV.\n\n" +
			"What converts cleanly is an array of objects, which is what an API " +
			"returns and what a spreadsheet expects. A single object becomes one row, " +
			"and an array of plain values becomes one column.\n\n" +
			"A nested object or array is reported rather than forced into a cell. " +
			"Pass --flatten to spread it across columns named with a dot, so that " +
			"{\"address\":{\"city\":\"Ipoh\"}} becomes a column called address.city. " +
			"Stringifying a nested value instead would produce a spreadsheet that " +
			"looks converted and is not, and nobody notices until it is in a report.\n\n" +
			"Columns are the union of the keys across every record, so a record " +
			"missing one gets an empty cell rather than a shifted row.",
		Examples: []Example{
			{
				Command:     "devnest json to-csv users.json > users.csv",
				Description: "Convert an array of objects into a spreadsheet.",
			},
			{
				Command:     "devnest json to-csv orders.json --flatten",
				Description: "Convert records holding nested objects.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&flatten, "flatten", false, "spread nested values across dotted columns")
			set.BoolVar(&useStdin, "stdin", false, "read the document from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			request, err := dataRequest(env, args, useStdin)
			if err != nil {
				return err
			}

			result, err := data.ToCSV(dataReader(), data.CSVRequest{
				Request: request,
				Flatten: flatten,
			})
			if err != nil {
				return err
			}

			// The text view is CSV as well. A command asked for CSV should not
			// produce an aligned table just because the default renderer draws
			// one; --output json is there for the structured form.
			return env.EmitTable(result, csvText(result), csvTable(result))
		},
	}
}

func csvText(result data.CSVResult) output.TextFunc {
	return func(w io.Writer) error {
		writer := csv.NewWriter(w)
		if err := writer.Write(result.Columns); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write csv output")
		}
		if err := writer.WriteAll(result.Rows); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write csv output")
		}
		return nil
	}
}

func csvTable(result data.CSVResult) output.TableFunc {
	return func() output.Table {
		columns := make([]output.Column, 0, len(result.Columns))
		for _, name := range result.Columns {
			columns = append(columns, output.Column{Title: name})
		}
		return output.Table{Columns: columns, Rows: result.Rows}
	}
}
