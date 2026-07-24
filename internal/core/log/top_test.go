package log

import (
	"context"
	"testing"
)

func TestTopRequestsRanksEndpoints(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := TopRequests(context.Background(), reader, TopRequest{Path: path, Limit: 3})
	if err != nil {
		t.Fatalf("TopRequests: %v", err)
	}

	if result.Subject != "endpoint" {
		t.Errorf("subject = %q, want endpoint", result.Subject)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(result.Entries))
	}
	if result.Entries[0].Value != "/api/users" || result.Entries[0].Count != 4 {
		t.Errorf("busiest = %q with %d, want /api/users with 4",
			result.Entries[0].Value, result.Entries[0].Count)
	}

	// Ranked highest first, and the shares are of the request total.
	for index := 1; index < len(result.Entries); index++ {
		if result.Entries[index].Count > result.Entries[index-1].Count {
			t.Errorf("entry %d has a higher count than the one before it", index)
		}
	}
	if want := percent(4, 24); result.Entries[0].Percent != want {
		t.Errorf("share = %.1f, want %.1f", result.Entries[0].Percent, want)
	}
}

func TestTopRequestsRanksClientsOnDemand(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := TopRequests(context.Background(), reader,
		TopRequest{Path: path, Limit: 2, Clients: true})
	if err != nil {
		t.Fatalf("TopRequests: %v", err)
	}

	if result.Subject != "client" {
		t.Errorf("subject = %q, want client", result.Subject)
	}
	if result.Entries[0].Value != "198.51.100.7" {
		t.Errorf("busiest client = %q, want 198.51.100.7", result.Entries[0].Value)
	}
	if result.Unique != 4 {
		t.Errorf("unique clients = %d, want 4", result.Unique)
	}
}

// Equal counts break on the value itself, so two runs over one file produce
// identical output and the report can be diffed.
func TestTopRequestsIsDeterministic(t *testing.T) {
	content := ""
	for _, path := range []string{"/b", "/a", "/c", "/a", "/b", "/c"} {
		content += `1.1.1.1 - - [24/Jul/2026:09:00:00 +0000] "GET ` + path + ` HTTP/1.1" 200 1` + "\n"
	}

	first, err := TopRequests(context.Background(),
		newFakeReader().with("a.log", content), TopRequest{Path: "a.log"})
	if err != nil {
		t.Fatalf("TopRequests: %v", err)
	}
	second, err := TopRequests(context.Background(),
		newFakeReader().with("a.log", content), TopRequest{Path: "a.log"})
	if err != nil {
		t.Fatalf("TopRequests: %v", err)
	}

	for index := range first.Entries {
		if first.Entries[index] != second.Entries[index] {
			t.Fatalf("two runs disagree at %d: %v and %v",
				index, first.Entries[index], second.Entries[index])
		}
	}
	if first.Entries[0].Value != "/a" {
		t.Errorf("first entry = %q, want /a: equal counts sort by value",
			first.Entries[0].Value)
	}
}
