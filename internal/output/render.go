package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// jsonRenderer emits the envelope. It works for every result by construction,
// because results are plain serialisable data.
type jsonRenderer struct{}

func (jsonRenderer) Name() string { return "json" }

func (jsonRenderer) Render(w io.Writer, envelope Envelope, _ TextFunc) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(envelope); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write json output")
	}
	return nil
}

// textRenderer presents a result to a person. A failed command prints nothing
// here: the CLI layer reports errors on stderr so that stdout stays clean for
// whatever is consuming it.
type textRenderer struct{}

func (textRenderer) Name() string { return "table" }

func (textRenderer) Render(w io.Writer, envelope Envelope, text TextFunc) error {
	if envelope.Status == StatusError || text == nil {
		return nil
	}

	buffered := bufio.NewWriter(w)
	if err := text(buffered); err != nil {
		return err
	}
	if err := buffered.Flush(); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}
	return nil
}

// Field is one label and value in a key-value listing.
type Field struct {
	Label string
	Value string
}

// WriteFields renders a key-value listing with the values aligned. It is the
// human view for results that are a handful of named values rather than rows.
func WriteFields(w io.Writer, fields []Field) error {
	width := 0
	for _, field := range fields {
		if len(field.Label) > width {
			width = len(field.Label)
		}
	}
	for _, field := range fields {
		padding := strings.Repeat(" ", width-len(field.Label)+2)
		if _, err := fmt.Fprintf(w, "%s%s%s\n", field.Label, padding, field.Value); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}
	}
	return nil
}
