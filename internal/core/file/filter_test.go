package file

import (
	"context"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func filterFixture() *fakeFS {
	return newFakeFS().
		addFile(root("manual.pdf"), "0123456789").
		addFile(root("report.pdf"), "01234").
		addFile(root("photo.jpg"), "012").
		addFile(root("src", "main.go"), "package main").
		addFile(root("src", "style.css"), "body{}").
		addFile(root("notes"), "no extension")
}

func filterRequest() FilterRequest {
	return FilterRequest{Selection: Selection{Root: root(), Recursive: true}}
}

func namesOf(result FilterResult) []string {
	names := make([]string, 0, len(result.Files))
	for _, item := range result.Files {
		names = append(names, item.Name)
	}
	return names
}

func TestFilterByExtension(t *testing.T) {
	request := filterRequest()
	request.Extensions = []string{"pdf"}

	result, err := Filter(context.Background(), filterFixture(), request)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if result.Matched != 2 {
		t.Errorf("matched = %d (%v), want 2", result.Matched, namesOf(result))
	}
	if result.TotalBytes != 15 {
		t.Errorf("TotalBytes = %d, want 15", result.TotalBytes)
	}
}

// The dot is optional, because both spellings are natural to type.
func TestFilterAcceptsExtensionWithOrWithoutDot(t *testing.T) {
	for _, spelling := range []string{"pdf", ".pdf", "PDF", " .Pdf "} {
		request := filterRequest()
		request.Extensions = []string{spelling}

		result, err := Filter(context.Background(), filterFixture(), request)
		if err != nil {
			t.Fatalf("Filter(%q): %v", spelling, err)
		}
		if result.Matched != 2 {
			t.Errorf("Filter(%q) matched %d, want 2", spelling, result.Matched)
		}
	}
}

func TestFilterBySeveralExtensions(t *testing.T) {
	request := filterRequest()
	request.Extensions = []string{"pdf", "jpg"}

	result, err := Filter(context.Background(), filterFixture(), request)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if result.Matched != 3 {
		t.Errorf("matched = %d (%v), want 3", result.Matched, namesOf(result))
	}
}

func TestFilterByCategory(t *testing.T) {
	request := filterRequest()
	request.Category = "code"

	result, err := Filter(context.Background(), filterFixture(), request)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if result.Matched != 2 {
		t.Errorf("matched = %d (%v), want main.go and style.css", result.Matched, namesOf(result))
	}
}

func TestFilterRejectsUnknownCategory(t *testing.T) {
	request := filterRequest()
	request.Category = "Photographs"

	_, err := Filter(context.Background(), filterFixture(), request)
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestFilterBySizeRange(t *testing.T) {
	request := filterRequest()
	request.MinBytes = 5
	request.MaxBytes = 10

	result, err := Filter(context.Background(), filterFixture(), request)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	for _, item := range result.Files {
		if item.Bytes < 5 || item.Bytes > 10 {
			t.Errorf("%s is %d bytes, outside the range", item.Name, item.Bytes)
		}
	}
	if result.Matched == 0 {
		t.Error("nothing matched a range that should include several files")
	}
}

func TestFilterRejectsInvertedSizeRange(t *testing.T) {
	request := filterRequest()
	request.MinBytes = 100
	request.MaxBytes = 10

	_, err := Filter(context.Background(), filterFixture(), request)
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestFilterByNameGlob(t *testing.T) {
	request := filterRequest()
	request.Match = "*.go"

	result, err := Filter(context.Background(), filterFixture(), request)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if result.Matched != 1 || result.Files[0].Name != "main.go" {
		t.Errorf("matched %v, want main.go", namesOf(result))
	}
}

func TestFilterSortsBySize(t *testing.T) {
	request := filterRequest()
	request.SortBy = SortBySize

	result, err := Filter(context.Background(), filterFixture(), request)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	for index := 1; index < len(result.Files); index++ {
		if result.Files[index-1].Bytes < result.Files[index].Bytes {
			t.Fatalf("not sorted by size: %v", result.Files)
		}
	}
}

func TestFilterLimitTruncatesAndSaysSo(t *testing.T) {
	request := filterRequest()
	request.Limit = 2

	result, err := Filter(context.Background(), filterFixture(), request)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if len(result.Files) != 2 {
		t.Errorf("returned %d files, want 2", len(result.Files))
	}
	if !result.Truncated {
		t.Error("Truncated = false after a limit was applied")
	}
	if result.Matched <= 2 {
		t.Error("Matched should count everything, not just what was returned")
	}
}

func TestFilterRespectsExclusions(t *testing.T) {
	request := filterRequest()
	request.Exclude = []string{"src"}

	result, err := Filter(context.Background(), filterFixture(), request)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	for _, item := range result.Files {
		if item.Name == "main.go" {
			t.Error("a file inside an excluded directory was returned")
		}
	}
}

func TestFilterWithoutConditionsListsEverything(t *testing.T) {
	result, err := Filter(context.Background(), filterFixture(), filterRequest())
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if result.Matched != 6 {
		t.Errorf("matched = %d (%v), want all six files", result.Matched, namesOf(result))
	}
}

func TestFilterClassifiesFiles(t *testing.T) {
	result, err := Filter(context.Background(), filterFixture(), filterRequest())
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}

	byName := make(map[string]Info, len(result.Files))
	for _, item := range result.Files {
		byName[item.Name] = item
	}

	if byName["manual.pdf"].Category != CategoryDocuments {
		t.Errorf("manual.pdf category = %q", byName["manual.pdf"].Category)
	}
	if byName["notes"].Extension != "" {
		t.Errorf("notes extension = %q, want empty", byName["notes"].Extension)
	}
	if byName["notes"].Category != CategoryOther {
		t.Errorf("notes category = %q, want Other", byName["notes"].Category)
	}
	if byName["main.go"].Relative != "src/main.go" {
		t.Errorf("relative path = %q, want forward slashes", byName["main.go"].Relative)
	}
}
