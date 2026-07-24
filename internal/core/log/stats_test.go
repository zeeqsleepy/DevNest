package log

import (
	"context"
	"strings"
	"testing"
)

func TestStatsMeasuresLines(t *testing.T) {
	reader := newFakeReader().with("app.log", "aaa\nbbbbbbbbbb\n\ncc\n")

	result, err := Stats(context.Background(), reader, StatsRequest{Path: "app.log"})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if result.Lines != 4 {
		t.Errorf("lines = %d, want 4", result.Lines)
	}
	if result.Blank != 1 {
		t.Errorf("blank lines = %d, want 1", result.Blank)
	}
	if result.LongestLineBytes != 10 {
		t.Errorf("longest line = %d bytes, want 10", result.LongestLineBytes)
	}

	// The shortest line ignores the blank one: the shortest line in almost
	// every log file is empty, and reporting zero answers nothing.
	if result.ShortestLineBytes != 2 || result.ShortestLine != 4 {
		t.Errorf("shortest = %d bytes on line %d, want 2 on line 4",
			result.ShortestLineBytes, result.ShortestLine)
	}

	// (3 + 10 + 2) / 3 non-blank lines.
	if result.AverageLineBytes != 5 {
		t.Errorf("average = %.1f, want 5.0", result.AverageLineBytes)
	}
}

func TestStatsRanksTheLongestLines(t *testing.T) {
	content := "short\n" +
		strings.Repeat("a", 300) + "\n" +
		"tiny\n" +
		strings.Repeat("b", 900) + "\n" +
		strings.Repeat("c", 600) + "\n"

	reader := newFakeReader().with("app.log", content)
	result, err := Stats(context.Background(), reader, StatsRequest{Path: "app.log", Top: 2})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if len(result.LongestLines) != 2 {
		t.Fatalf("longest lines = %d, want 2", len(result.LongestLines))
	}
	if result.LongestLines[0].Line != 4 || result.LongestLines[0].Bytes != 900 {
		t.Errorf("longest = line %d of %d bytes, want line 4 of 900",
			result.LongestLines[0].Line, result.LongestLines[0].Bytes)
	}
	if result.LongestLines[1].Line != 5 {
		t.Errorf("second longest = line %d, want line 5", result.LongestLines[1].Line)
	}

	// The excerpt is a sample, not the line. This is a listing of where the
	// bytes went, and printing them all would put them in the report too.
	if len(result.LongestLines[0].Text) > excerptLength+3 {
		t.Errorf("excerpt is %d bytes, want at most %d",
			len(result.LongestLines[0].Text), excerptLength)
	}
}

func TestStatsOnTheSampleLog(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := Stats(context.Background(), reader, StatsRequest{Path: path})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if result.Lines != 27 {
		t.Errorf("lines = %d, want 27", result.Lines)
	}
	if result.AverageLineBytes <= 0 {
		t.Errorf("average = %.1f, want a positive length", result.AverageLineBytes)
	}
	if result.LongestLineBytes < result.ShortestLineBytes {
		t.Error("the longest line is shorter than the shortest one")
	}
}

func TestStatsOnAnEmptyFile(t *testing.T) {
	reader := newFakeReader().with("empty.log", "")

	result, err := Stats(context.Background(), reader, StatsRequest{Path: "empty.log"})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	if result.Lines != 0 || result.AverageLineBytes != 0 {
		t.Errorf("lines = %d, average = %.1f, want zeroes", result.Lines, result.AverageLineBytes)
	}
	if result.LongestLines == nil {
		t.Error("an empty listing must be an empty slice, never null in JSON")
	}
}
