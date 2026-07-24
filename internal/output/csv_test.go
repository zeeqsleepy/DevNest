package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func rows() TableFunc {
	return func() Table {
		return Table{
			Columns: []Column{{Title: "endpoint"}, {Title: "count", Right: true}},
			Rows: [][]string{
				{"/api/users", "1204"},
				{"/search?q=a,b", "12"},
				{"with \"quotes\"", "1"},
			},
		}
	}
}

func TestCSVRendererIsSelectedByName(t *testing.T) {
	renderer, err := NewRenderer("csv")
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	if renderer.Name() != "csv" {
		t.Errorf("Name = %q, want \"csv\"", renderer.Name())
	}
	if _, ok := renderer.(RowRenderer); !ok {
		t.Error("the csv renderer must accept a row view")
	}
}

// A CSV file carries the header and the rows and nothing else. A preamble of
// metadata is not something any spreadsheet or shell tool will read.
func TestCSVRendererWritesOnlyRows(t *testing.T) {
	renderer := csvRenderer{}

	var buffer bytes.Buffer
	envelope := NewEnvelope(Meta{Version: "1.2.3", Command: "log top"}, map[string]int{"a": 1})
	if err := renderer.RenderRows(&buffer, envelope, rows()); err != nil {
		t.Fatalf("RenderRows: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buffer.String(), "\r\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want a header and three rows: %q", len(lines), buffer.String())
	}
	if strings.TrimRight(lines[0], "\r") != "endpoint,count" {
		t.Errorf("header = %q", lines[0])
	}
	for _, unwanted := range []string{"1.2.3", "log top", "status", "warnings"} {
		if strings.Contains(buffer.String(), unwanted) {
			t.Errorf("output carries envelope metadata %q:\n%s", unwanted, buffer.String())
		}
	}
}

// Values holding the delimiter or a quote have to survive it, or the file is
// worse than useless: it parses, into the wrong columns.
func TestCSVRendererQuotesAwkwardValues(t *testing.T) {
	renderer := csvRenderer{}

	var buffer bytes.Buffer
	if err := renderer.RenderRows(&buffer, NewEnvelope(Meta{}, nil), rows()); err != nil {
		t.Fatalf("RenderRows: %v", err)
	}

	got := buffer.String()
	if !strings.Contains(got, `"/search?q=a,b"`) {
		t.Errorf("output = %q, want the comma quoted", got)
	}
	if !strings.Contains(got, `"with ""quotes"""`) {
		t.Errorf("output = %q, want the quotes doubled", got)
	}
}

// A command whose result is not rows says so. Inventing a shape produces a
// file somebody then writes a script against.
func TestCSVRendererRefusesAResultWithoutRows(t *testing.T) {
	renderer := csvRenderer{}
	var buffer bytes.Buffer

	err := renderer.Render(&buffer, NewEnvelope(Meta{}, map[string]int{"a": 1}), nil)
	if err == nil {
		t.Fatal("a result with no row view was rendered as csv")
	}
	if got := errors.CodeOf(err); got != errors.CodeUnsupported {
		t.Errorf("code = %q, want %q", got, errors.CodeUnsupported)
	}

	if err := renderer.RenderRows(&buffer, NewEnvelope(Meta{}, nil), nil); err == nil {
		t.Error("RenderRows accepted a nil row view")
	}
	if buffer.Len() != 0 {
		t.Errorf("stdout = %q, want nothing written on refusal", buffer.String())
	}
}

// A failed command writes nothing here. The error goes to stderr, and a
// half-written CSV would be parsed as data.
func TestCSVRendererWritesNothingForAFailure(t *testing.T) {
	renderer := csvRenderer{}
	envelope := NewEnvelope(Meta{}, nil).WithError(ErrorInfo{Code: "IO_ERROR", Message: "broken"})

	var buffer bytes.Buffer
	if err := renderer.Render(&buffer, envelope, nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if err := renderer.RenderRows(&buffer, envelope, rows()); err != nil {
		t.Fatalf("RenderRows: %v", err)
	}
	if buffer.Len() != 0 {
		t.Errorf("stdout = %q, want nothing on a failed command", buffer.String())
	}
}
