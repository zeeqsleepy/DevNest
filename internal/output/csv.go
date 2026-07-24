package output

import (
	"encoding/csv"
	"io"

	"github.com/devnest/devnest/internal/errors"
)

// Table is the row view of a result: the same data the text renderer shows,
// expressed as a header and rows.
//
// It exists for the renderers that emit rows rather than prose. Only the CLI
// layer builds one, because deciding which of a result's numbers belong in a
// spreadsheet is a presentation decision and never the module's.
type Table struct {
	Columns []Column
	Rows    [][]string
}

// TableFunc returns the row view of a result.
//
// A command supplies one only when its result really is a set of rows. A
// command that does not is asked for csv and says so, which is better than
// inventing a shape and letting somebody build a script on it.
type TableFunc func() Table

// RowRenderer is implemented by renderers that need the row view rather than
// the text one. Env.Emit checks for it, so a command that has no rows keeps
// working with the renderers that do not need them.
type RowRenderer interface {
	Renderer
	RenderRows(w io.Writer, envelope Envelope, table TableFunc) error
}

// csvRenderer writes rows for a spreadsheet or a shell pipeline.
//
// The envelope's metadata and warnings are deliberately absent: a CSV file with
// a preamble is not a CSV file any tool will read. Warnings still reach the
// user on stderr, and anyone who needs the metadata has --output json.
type csvRenderer struct{}

func (csvRenderer) Name() string { return "csv" }

func (csvRenderer) Render(_ io.Writer, envelope Envelope, _ TextFunc) error {
	if envelope.Status == StatusError {
		return nil
	}
	return unsupported()
}

func (csvRenderer) RenderRows(w io.Writer, envelope Envelope, table TableFunc) error {
	if envelope.Status == StatusError {
		return nil
	}
	if table == nil {
		return unsupported()
	}

	view := table()
	writer := csv.NewWriter(w)

	header := make([]string, 0, len(view.Columns))
	for _, column := range view.Columns {
		header = append(header, column.Title)
	}
	if err := writer.Write(header); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write csv output")
	}
	for _, row := range view.Rows {
		if err := writer.Write(row); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write csv output")
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write csv output")
	}
	return nil
}

func unsupported() error {
	return errors.New(errors.CodeUnsupported, "this command has no csv output").
		WithHint("use --output table for people or --output json for scripts")
}
