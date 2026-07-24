package log

import (
	"context"
	"strings"
	"testing"
)

func TestSummarizeErrorsFindsAndGroupsFailures(t *testing.T) {
	reader, path := fixture(t, "app.log")

	result, err := SummarizeErrors(context.Background(), reader, ErrorsRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeErrors: %v", err)
	}

	if result.Errors != 11 {
		t.Errorf("findings = %d, want 11", result.Errors)
	}
	requireCount(t, result.Severities, SeverityError, 10)
	requireCount(t, result.Severities, SeverityFatal, 1)

	requireCount(t, result.Categories, CategoryConnection, 3)
	requireCount(t, result.Categories, CategoryNotFound, 3)
	requireCount(t, result.Categories, CategoryTimeout, 2)
	requireCount(t, result.Categories, CategoryMemory, 1)
}

// Warnings are counted but stay out of the findings unless asked for. A
// summary that lists every deprecation notice buries what matters.
func TestSummarizeErrorsKeepsWarningsSeparate(t *testing.T) {
	reader, path := fixture(t, "app.log")

	quiet, err := SummarizeErrors(context.Background(), reader, ErrorsRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeErrors: %v", err)
	}
	if quiet.Warnings != 3 {
		t.Errorf("warnings seen = %d, want 3", quiet.Warnings)
	}
	if _, found := find(quiet.Severities, SeverityWarning); found {
		t.Error("warnings were listed as findings without being asked for")
	}

	reader, path = fixture(t, "app.log")
	loud, err := SummarizeErrors(context.Background(), reader,
		ErrorsRequest{Path: path, IncludeWarnings: true})
	if err != nil {
		t.Fatalf("SummarizeErrors: %v", err)
	}
	if loud.Errors != 14 {
		t.Errorf("findings with warnings = %d, want 14", loud.Errors)
	}
	requireCount(t, loud.Severities, SeverityWarning, 3)
}

// Messages differing only by an identifier are one finding seen several times.
// The line numbers are what makes the raw entries findable again.
func TestSummarizeErrorsGroupsRepeatedMessages(t *testing.T) {
	reader, path := fixture(t, "app.log")

	result, err := SummarizeErrors(context.Background(), reader, ErrorsRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeErrors: %v", err)
	}

	first := result.Messages[0]
	if first.Count != 3 {
		t.Errorf("most common message occurred %d times, want 3", first.Count)
	}
	if !strings.Contains(first.Message, "database connection failed") {
		t.Errorf("most common message = %q, want the database failure", first.Message)
	}
	if first.FirstLine != 5 || first.LastLine != 7 {
		t.Errorf("lines %d to %d, want 5 to 7", first.FirstLine, first.LastLine)
	}

	notFound, found := findMessage(result.Messages, "not found")
	if !found {
		t.Fatal("the repeated \"user not found\" failures were not grouped")
	}
	if notFound.Count != 3 {
		t.Errorf("grouped %d \"not found\" failures, want 3", notFound.Count)
	}
}

// A 5xx in an access log is a failure too, and reaches the same summary. An
// incident is investigated across both kinds of file.
func TestSummarizeErrorsReadsServerErrorsFromAnAccessLog(t *testing.T) {
	reader, path := fixture(t, "access.log")

	result, err := SummarizeErrors(context.Background(), reader, ErrorsRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeErrors: %v", err)
	}

	if result.Errors != 3 {
		t.Errorf("findings = %d, want the 3 5xx responses", result.Errors)
	}
	requireCount(t, result.Categories, CategoryServerError, 3)
}

// A clean log is a clean result, not an empty one that renders as null.
func TestSummarizeErrorsOnACleanLog(t *testing.T) {
	reader, path := fixture(t, "plain.log")

	result, err := SummarizeErrors(context.Background(), reader, ErrorsRequest{Path: path})
	if err != nil {
		t.Fatalf("SummarizeErrors: %v", err)
	}

	if result.Errors != 0 {
		t.Errorf("findings = %d, want 0", result.Errors)
	}
	if result.Messages == nil || result.Severities == nil || result.Categories == nil {
		t.Error("empty listings must be empty slices, never null in JSON")
	}
}

func findMessage(messages []ErrorMessage, contains string) (ErrorMessage, bool) {
	for _, message := range messages {
		if strings.Contains(message.Message, contains) {
			return message, true
		}
	}
	return ErrorMessage{}, false
}
