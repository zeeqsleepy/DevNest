package log

import (
	"context"
	"testing"
)

func TestSummarizeHTTPCountsTheTraffic(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := SummarizeHTTP(context.Background(), reader, HTTPRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeHTTP: %v", err)
	}

	if result.Requests != 24 {
		t.Errorf("requests = %d, want 24", result.Requests)
	}
	// The rotation notice and the stray line are counted, not fatal.
	if result.Unparsed != 2 {
		t.Errorf("unparsed = %d, want 2", result.Unparsed)
	}
	if result.UniqueIPs != 4 {
		t.Errorf("unique clients = %d, want 4", result.UniqueIPs)
	}

	requireCount(t, result.Methods, "GET", 18)
	requireCount(t, result.Methods, "POST", 2)
	requireCount(t, result.Methods, "HEAD", 2)
	requireCount(t, result.StatusCodes, "200", 12)
	requireCount(t, result.StatusCodes, "404", 3)
	requireCount(t, result.TopClients, "198.51.100.7", 9)
}

// Query strings are stripped before endpoints are counted, so three requests
// to different pages of one listing are one endpoint seen three times.
func TestSummarizeHTTPGroupsAnEndpointAcrossItsQueryStrings(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := SummarizeHTTP(context.Background(), reader, HTTPRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeHTTP: %v", err)
	}

	requireCount(t, result.TopPaths, "/api/users", 4)
	if result.TopPaths[0].Value != "/api/users" {
		t.Errorf("busiest endpoint = %q, want /api/users", result.TopPaths[0].Value)
	}
}

// Responses that carried no body are excluded from the average. Including the
// 304s would make a working cache look like a server that stopped sending.
func TestSummarizeHTTPAveragesOnlyResponsesWithABody(t *testing.T) {
	log := `1.1.1.1 - - [24/Jul/2026:09:00:00 +0000] "GET /a HTTP/1.1" 200 1000` + "\n" +
		`1.1.1.1 - - [24/Jul/2026:09:00:01 +0000] "GET /a HTTP/1.1" 200 3000` + "\n" +
		`1.1.1.1 - - [24/Jul/2026:09:00:02 +0000] "GET /a HTTP/1.1" 304 -` + "\n"

	reader := newFakeReader().with("access.log", log)
	result, err := SummarizeHTTP(context.Background(), reader, HTTPRequest{Path: "access.log"})
	if err != nil {
		t.Fatalf("SummarizeHTTP: %v", err)
	}

	if result.TotalResponseBytes != 4000 {
		t.Errorf("total response bytes = %d, want 4000", result.TotalResponseBytes)
	}
	if result.AverageResponseBytes != 2000 {
		t.Errorf("average response bytes = %d, want 2000", result.AverageResponseBytes)
	}
}

// A file that is not an access log produces a summary of zero requests rather
// than an error. The command worked; the file is not what was expected.
func TestSummarizeHTTPReportsNothingRatherThanFailing(t *testing.T) {
	reader, path := fixture(t, "app.log")

	result, err := SummarizeHTTP(context.Background(), reader, HTTPRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeHTTP: %v", err)
	}

	if result.Requests != 0 {
		t.Errorf("requests = %d, want 0", result.Requests)
	}
	if result.Unparsed != result.Lines {
		t.Errorf("unparsed = %d, want all %d lines", result.Unparsed, result.Lines)
	}
	if result.Methods == nil || result.TopPaths == nil {
		t.Error("empty listings must be empty slices, never null in JSON")
	}
}

func TestSummarizeHTTPHonoursTheTopLimit(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := SummarizeHTTP(context.Background(), reader, HTTPRequest{Path: path, Top: 2})
	if err != nil {
		t.Fatalf("SummarizeHTTP: %v", err)
	}

	if len(result.TopPaths) != 2 {
		t.Errorf("top paths = %d, want 2", len(result.TopPaths))
	}
	// Methods are never truncated: there are nine of them in the world and
	// the whole point of the listing is that it is complete.
	if len(result.Methods) != 5 {
		t.Errorf("methods = %d, want all 5 seen", len(result.Methods))
	}
}
