package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTableAlignsColumns(t *testing.T) {
	var buffer bytes.Buffer

	err := WriteTable(&buffer, []Column{
		{Title: "path"},
		{Title: "size", Right: true},
	}, [][]string{
		{"a-very-long-name.txt", "1.2 KB"},
		{"short.txt", "10 B"},
	})
	if err != nil {
		t.Fatalf("WriteTable: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buffer.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("wrote %d lines, want a header, a rule, and two rows:\n%s", len(lines), buffer.String())
	}
	if !strings.HasPrefix(lines[0], "path") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "----") {
		t.Errorf("rule = %q", lines[1])
	}

	// A right-aligned column ends at the same offset on every row.
	if len(lines[2]) != len(lines[3]) {
		t.Errorf("right-aligned column is not aligned:\n%q\n%q", lines[2], lines[3])
	}
}

func TestWriteTableHandlesShortRows(t *testing.T) {
	var buffer bytes.Buffer

	err := WriteTable(&buffer, []Column{{Title: "a"}, {Title: "b"}}, [][]string{{"only"}})
	if err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	if !strings.Contains(buffer.String(), "only") {
		t.Errorf("output = %q", buffer.String())
	}
}

func TestWriteTableWithoutColumnsWritesNothing(t *testing.T) {
	var buffer bytes.Buffer

	if err := WriteTable(&buffer, nil, [][]string{{"a"}}); err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	if buffer.Len() != 0 {
		t.Errorf("output = %q, want nothing", buffer.String())
	}
}

// No trailing whitespace: it shows up in diffs and in pasted output.
func TestWriteTableDoesNotPadTheEndOfALine(t *testing.T) {
	var buffer bytes.Buffer

	err := WriteTable(&buffer, []Column{{Title: "name"}, {Title: "note"}}, [][]string{
		{"a", "short"},
		{"b", "a much longer note"},
	})
	if err != nil {
		t.Fatalf("WriteTable: %v", err)
	}

	for _, line := range strings.Split(buffer.String(), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line has trailing whitespace: %q", line)
		}
	}
}

func TestBytes(t *testing.T) {
	tests := map[int64]string{
		0:                "0 B",
		512:              "512 B",
		1023:             "1023 B",
		1024:             "1.0 KB",
		1536:             "1.5 KB",
		1024 * 1024:      "1.0 MB",
		1024 * 1024 * 10: "10.0 MB",
		1610612736:       "1.5 GB",
	}

	for input, want := range tests {
		if got := Bytes(input); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", input, got, want)
		}
	}
}

// Three digits plus a decimal point is wider than the column deserves.
func TestBytesDropsTheDecimalAboveOneHundred(t *testing.T) {
	if got := Bytes(150 * 1024); got != "150 KB" {
		t.Errorf("Bytes = %q, want %q", got, "150 KB")
	}
}

func TestCount(t *testing.T) {
	tests := map[int]string{
		0:       "0",
		1:       "1",
		999:     "999",
		1000:    "1,000",
		38412:   "38,412",
		1000000: "1,000,000",
		-1500:   "-1,500",
	}

	for input, want := range tests {
		if got := Count(input); got != want {
			t.Errorf("Count(%d) = %q, want %q", input, got, want)
		}
	}
}
