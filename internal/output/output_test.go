package output

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestNewRenderer(t *testing.T) {
	for _, name := range []string{"table", "json", "csv"} {
		renderer, err := NewRenderer(name)
		if err != nil {
			t.Fatalf("NewRenderer(%q): %v", name, err)
		}
		if renderer == nil {
			t.Fatalf("NewRenderer(%q) returned nil", name)
		}
	}

	_, err := NewRenderer("markdown")
	if err == nil {
		t.Fatal("NewRenderer accepted a format that is not implemented yet")
	}
	if got := errors.CodeOf(err); got != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", got, errors.CodeInvalidInput)
	}
}

func TestEnvelopeAlwaysHasAWarningArray(t *testing.T) {
	encoded, err := json.Marshal(NewEnvelope(Meta{}, map[string]int{"count": 1}))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"warnings":[]`) {
		t.Errorf("encoded = %s, want an empty array rather than null", encoded)
	}
}

func TestWithWarningsPromotesStatus(t *testing.T) {
	envelope := NewEnvelope(Meta{}, nil)
	if envelope.Status != StatusOK {
		t.Fatalf("status = %q, want %q", envelope.Status, StatusOK)
	}

	envelope = envelope.WithWarnings([]Warning{{Code: "IO_ERROR", Message: "skipped"}})
	if envelope.Status != StatusWarning {
		t.Errorf("status = %q, want %q", envelope.Status, StatusWarning)
	}
}

func TestWithErrorClearsData(t *testing.T) {
	envelope := NewEnvelope(Meta{}, map[string]int{"count": 1}).
		WithError(ErrorInfo{Code: "NOT_FOUND", Message: "missing"})

	if envelope.Status != StatusError {
		t.Errorf("status = %q, want %q", envelope.Status, StatusError)
	}
	if envelope.Data != nil {
		t.Errorf("data = %v, want nil; a failed command has no result", envelope.Data)
	}
	if envelope.Error == nil || envelope.Error.Code != "NOT_FOUND" {
		t.Errorf("error = %v, want the failure recorded", envelope.Error)
	}
}

func TestJSONRendererIgnoresTheTextView(t *testing.T) {
	renderer, _ := NewRenderer("json")
	var buffer bytes.Buffer

	err := renderer.Render(&buffer, NewEnvelope(Meta{Command: "version"}, map[string]string{"a": "b"}),
		func(io.Writer) error {
			t.Fatal("the text view must not run in json mode")
			return nil
		})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestTextRendererWritesNothingOnError(t *testing.T) {
	renderer, _ := NewRenderer("table")
	var buffer bytes.Buffer

	envelope := NewEnvelope(Meta{}, nil).WithError(ErrorInfo{Code: "NOT_FOUND", Message: "missing"})
	err := renderer.Render(&buffer, envelope, func(w io.Writer) error {
		_, err := io.WriteString(w, "should not appear")
		return err
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if buffer.Len() != 0 {
		t.Errorf("stdout = %q, want it empty; errors are reported on stderr", buffer.String())
	}
}

func TestTextRendererWritesTheTextView(t *testing.T) {
	renderer, _ := NewRenderer("table")
	var buffer bytes.Buffer

	err := renderer.Render(&buffer, NewEnvelope(Meta{}, nil), func(w io.Writer) error {
		_, err := io.WriteString(w, "version  1.0.0\n")
		return err
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if buffer.String() != "version  1.0.0\n" {
		t.Errorf("stdout = %q, want the text view", buffer.String())
	}
}

func TestTextRendererToleratesNoTextView(t *testing.T) {
	renderer, _ := NewRenderer("table")
	var buffer bytes.Buffer

	if err := renderer.Render(&buffer, NewEnvelope(Meta{}, nil), nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if buffer.Len() != 0 {
		t.Errorf("stdout = %q, want it empty", buffer.String())
	}
}

func TestRendererNamesMatchTheOutputFlag(t *testing.T) {
	for _, name := range []string{"table", "json"} {
		renderer, err := NewRenderer(name)
		if err != nil {
			t.Fatalf("NewRenderer(%q): %v", name, err)
		}
		if renderer.Name() != name {
			t.Errorf("Name() = %q, want %q", renderer.Name(), name)
		}
	}
}

func TestWithWarningsIgnoresAnEmptyList(t *testing.T) {
	envelope := NewEnvelope(Meta{}, nil).WithWarnings(nil)

	if envelope.Status != StatusOK {
		t.Errorf("status = %q, want %q", envelope.Status, StatusOK)
	}
	if envelope.Warnings == nil {
		t.Error("warnings became null")
	}
}

func TestWriteFieldsAlignsValues(t *testing.T) {
	var buffer bytes.Buffer

	err := WriteFields(&buffer, []Field{
		{Label: "version", Value: "1.0.0"},
		{Label: "go", Value: "go1.25.0"},
	})
	if err != nil {
		t.Fatalf("WriteFields: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2", len(lines))
	}
	if strings.Index(lines[0], "1.0.0") != strings.Index(lines[1], "go1.25.0") {
		t.Errorf("values are not aligned:\n%s", buffer.String())
	}
}

func TestUseColor(t *testing.T) {
	noEnv := func(string) (string, bool) { return "", false }
	noColorSet := func(name string) (string, bool) { return "", name == "NO_COLOR" }

	tests := []struct {
		name   string
		mode   string
		lookup func(string) (string, bool)
		want   bool
	}{
		{"always wins over NO_COLOR", "always", noColorSet, true},
		{"never is never", "never", noEnv, false},
		{"auto off when not a terminal", "auto", noEnv, false},
		{"auto off when NO_COLOR is set", "auto", noColorSet, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := UseColor(test.mode, io.Discard, test.lookup); got != test.want {
				t.Errorf("UseColor = %v, want %v", got, test.want)
			}
		})
	}
}

func TestIsTerminalIsFalseForFilesAndBuffers(t *testing.T) {
	if IsTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is not a terminal")
	}

	file, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer file.Close()

	if IsTerminal(file) {
		t.Error("a regular file is not a terminal")
	}
}
