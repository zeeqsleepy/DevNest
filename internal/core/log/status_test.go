package log

import (
	"context"
	"testing"
)

func TestSummarizeStatusBreaksDownByFamily(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := SummarizeStatus(context.Background(), reader, StatusRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeStatus: %v", err)
	}

	if result.Requests != 24 {
		t.Errorf("requests = %d, want 24", result.Requests)
	}

	requireCount(t, result.Classes, "2xx", 14)
	requireCount(t, result.Classes, "3xx", 2)
	requireCount(t, result.Classes, "4xx", 5)
	requireCount(t, result.Classes, "5xx", 3)

	if result.Errors != 8 {
		t.Errorf("4xx and 5xx = %d, want 8", result.Errors)
	}
}

// All five families are always reported, including the empty ones. Omitting
// 5xx leaves a reader unable to tell "none" from "not measured".
func TestSummarizeStatusAlwaysReportsEveryFamily(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := SummarizeStatus(context.Background(), reader, StatusRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeStatus: %v", err)
	}

	want := []string{"1xx", "2xx", "3xx", "4xx", "5xx"}
	if len(result.Classes) != len(want) {
		t.Fatalf("classes = %d, want %d", len(result.Classes), len(want))
	}
	for index, name := range want {
		if result.Classes[index].Value != name {
			t.Errorf("class %d = %q, want %q", index, result.Classes[index].Value, name)
		}
	}

	informational, _ := find(result.Classes, "1xx")
	if informational.Count != 0 {
		t.Errorf("1xx = %d, want 0 and still listed", informational.Count)
	}
}

func TestSummarizeStatusPercentagesAddUp(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := SummarizeStatus(context.Background(), reader, StatusRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeStatus: %v", err)
	}

	total := 0.0
	for _, class := range result.Classes {
		total += class.Percent
	}
	if total < 99.5 || total > 100.5 {
		t.Errorf("class percentages total %.1f, want about 100", total)
	}
}

// The two HTTP summaries read one collection, so they cannot report different
// request counts for one file.
func TestStatusAndHTTPAgree(t *testing.T) {
	reader, path := fixture(t, "access.log")

	summary, err := SummarizeHTTP(context.Background(), reader, HTTPRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeHTTP: %v", err)
	}

	reader, path = fixture(t, "access.log")
	status, err := SummarizeStatus(context.Background(), reader, StatusRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeStatus: %v", err)
	}

	if summary.Requests != status.Requests || summary.Unparsed != status.Unparsed {
		t.Errorf("http reports %d requests and %d unparsed; status reports %d and %d",
			summary.Requests, summary.Unparsed, status.Requests, status.Unparsed)
	}
	for index, class := range summary.StatusClasses {
		if class != status.Classes[index] {
			t.Errorf("class %q differs between the two commands", class.Value)
		}
	}
}
