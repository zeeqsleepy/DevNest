package log

import (
	"context"
	"strings"
	"testing"
)

func TestAnalyzeCountsAndIdentifiesAnAccessLog(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := Analyze(context.Background(), reader, AnalyzeRequest{Path: path})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if result.Lines != 27 {
		t.Errorf("lines = %d, want 27", result.Lines)
	}
	if result.Blank != 1 {
		t.Errorf("blank lines = %d, want 1", result.Blank)
	}
	if result.Type != TypeAccess {
		t.Errorf("type = %q, want %q", result.Type, TypeAccess)
	}
	if result.Bytes == 0 {
		t.Error("size = 0, want the size of the fixture")
	}
	if result.Sampled != 26 {
		t.Errorf("sampled = %d, want the 26 non-blank lines", result.Sampled)
	}
}

func TestAnalyzeIdentifiesTheOtherShapes(t *testing.T) {
	tests := []struct {
		fixture string
		want    string
	}{
		{"app.log", TypeApplication},
		{"plain.log", TypeText},
	}

	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			reader, path := fixture(t, test.fixture)

			result, err := Analyze(context.Background(), reader, AnalyzeRequest{Path: path})
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if result.Type != test.want {
				t.Errorf("type = %q, want %q", result.Type, test.want)
			}
		})
	}
}

func TestAnalyzeIdentifiesStructuredLogging(t *testing.T) {
	var content strings.Builder
	for range 20 {
		content.WriteString(`{"time":"2026-07-24T09:00:00Z","level":"info","msg":"served"}` + "\n")
	}

	reader := newFakeReader().with("app.jsonl", content.String())
	result, err := Analyze(context.Background(), reader, AnalyzeRequest{Path: "app.jsonl"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if result.Type != TypeJSONLines {
		t.Errorf("type = %q, want %q", result.Type, TypeJSONLines)
	}
}

// An empty file is a valid answer, not a failure.
func TestAnalyzeAcceptsAnEmptyFile(t *testing.T) {
	reader := newFakeReader().with("empty.log", "")

	result, err := Analyze(context.Background(), reader, AnalyzeRequest{Path: "empty.log"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Lines != 0 {
		t.Errorf("lines = %d, want 0", result.Lines)
	}
	if result.Type != TypeText {
		t.Errorf("type = %q, want %q for a file with nothing to judge", result.Type, TypeText)
	}
}
