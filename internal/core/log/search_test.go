package log

import (
	"context"
	"strings"
	"testing"
)

func TestSearchFindsLinesWithLineNumbers(t *testing.T) {
	reader, path := fixture(t, "app.log")

	result, err := Search(context.Background(), reader,
		SearchRequest{Path: path, Query: "connection refused"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if result.Matches != 3 {
		t.Errorf("matches = %d, want 3", result.Matches)
	}
	if len(result.Results) != 3 {
		t.Fatalf("listed %d matches, want 3", len(result.Results))
	}
	if result.Results[0].Line != 5 {
		t.Errorf("first match on line %d, want 5", result.Results[0].Line)
	}
	if !strings.Contains(result.Results[0].Text, "connection refused") {
		t.Errorf("match text = %q, want the matching line", result.Results[0].Text)
	}
	if result.Lines != 24 {
		t.Errorf("lines searched = %d, want the whole file", result.Lines)
	}
}

// Case sensitivity is the default, because a log search is usually for an
// identifier. Both behaviours are one flag apart.
func TestSearchCaseSensitivity(t *testing.T) {
	reader := newFakeReader().with("app.log", "Timeout reached\ntimeout reached\nTIMEOUT reached\n")

	sensitive, err := Search(context.Background(), reader,
		SearchRequest{Path: "app.log", Query: "timeout"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if sensitive.Matches != 1 {
		t.Errorf("case-sensitive matches = %d, want 1", sensitive.Matches)
	}

	insensitive, err := Search(context.Background(), reader,
		SearchRequest{Path: "app.log", Query: "timeout", IgnoreCase: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if insensitive.Matches != 3 {
		t.Errorf("case-insensitive matches = %d, want 3", insensitive.Matches)
	}
	if insensitive.Results[0].Line != 1 {
		t.Errorf("first insensitive match on line %d, want 1", insensitive.Results[0].Line)
	}
}

// The whole file is read even when the listing is capped, so the count is the
// real one. Reporting "5 matches" for a file with nine thousand would be a lie
// told by an implementation detail.
func TestSearchCountsEveryMatchEvenWhenTheListingIsCapped(t *testing.T) {
	reader := newFakeReader().with("app.log", strings.Repeat("timeout\n", 50))

	result, err := Search(context.Background(), reader,
		SearchRequest{Path: "app.log", Query: "timeout", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if result.Matches != 50 {
		t.Errorf("matches = %d, want 50", result.Matches)
	}
	if len(result.Results) != 5 {
		t.Errorf("listed %d, want 5", len(result.Results))
	}
	if !result.Limited {
		t.Error("a capped listing must say that it was capped")
	}
}

func TestSearchFindingNothingIsAResult(t *testing.T) {
	reader, path := fixture(t, "plain.log")

	result, err := Search(context.Background(), reader,
		SearchRequest{Path: path, Query: "catastrophe"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if result.Matches != 0 {
		t.Errorf("matches = %d, want 0", result.Matches)
	}
	if result.Results == nil {
		t.Error("an empty listing must be an empty slice, never null in JSON")
	}
}

func TestSearchRejectsAnEmptyQuery(t *testing.T) {
	reader, path := fixture(t, "plain.log")

	_, err := Search(context.Background(), reader, SearchRequest{Path: path, Query: "   "})
	assertCode(t, err, "INVALID_INPUT")
}

// A matching line longer than the cap is reported cut short and says so, so
// nobody reads the excerpt as the whole line.
func TestSearchTruncatesAnEnormousMatch(t *testing.T) {
	line := "needle " + strings.Repeat("x", matchLength*2)
	reader := newFakeReader().with("app.log", line+"\n")

	result, err := Search(context.Background(), reader,
		SearchRequest{Path: "app.log", Query: "needle"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(result.Results[0].Text) > matchLength+3 {
		t.Errorf("kept %d bytes, want the cap of %d", len(result.Results[0].Text), matchLength)
	}
	if !result.Results[0].Truncated {
		t.Error("a shortened match must say that it was shortened")
	}
}
