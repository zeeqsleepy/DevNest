package scan

import (
	"context"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestCompareReportsGrowth(t *testing.T) {
	fake := project()

	before := SummaryResult{
		Files: 8, Directories: 1, Bytes: 400,
		Authored: 6, AuthoredBytes: 300,
	}

	result, err := Compare(context.Background(), fake, CompareRequest{
		Selection: Selection{Root: root()},
		Before:    before,
	})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	if result.FilesAfter != scanCount(fake) {
		t.Errorf("filesAfter = %d, want %d", result.FilesAfter, scanCount(fake))
	}
	wantDelta := scanCount(fake) - 8
	if result.FilesDelta != wantDelta {
		t.Errorf("filesDelta = %d, want after minus before (%d)", result.FilesDelta, wantDelta)
	}
	if result.BytesDelta != result.BytesAfter-400 {
		t.Errorf("bytesDelta = %d, want after minus before", result.BytesDelta)
	}
}

// scanCount reports how many files a Summarize over the fake would count, which
// is not simply len(fake.files): the walk skips vendored and build directories.
func scanCount(fake *fakeFS) int {
	snapshot, err := Summarize(context.Background(), fake, SummaryRequest{Selection: Selection{Root: root()}})
	if err != nil {
		return 0
	}
	return snapshot.Files
}

func TestCompareReportsACategoryThatGrew(t *testing.T) {
	fake := newFakeFS().
		with("a.go", "package a\n").
		with("a_test.go", "package a\nfunc TestX(t *testing.T){}\n")

	// The "before" says two source files and no tests; now there is a test.
	before := SummaryResult{
		Categories: []Count{
			{Name: "source", Files: 2},
			{Name: "test", Files: 0},
		},
	}

	result, err := Compare(context.Background(), fake, CompareRequest{
		Selection: Selection{Root: root()},
		Before:    before,
	})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	testDelta, found := findDelta(result.Categories, "test")
	if !found {
		t.Fatalf("test category is missing from %+v", result.Categories)
	}
	if testDelta.FilesDelta != 1 {
		t.Errorf("test filesDelta = %d, want 1 (one test appeared)", testDelta.FilesDelta)
	}
}

func TestCompareReportsALanguageThatDisappeared(t *testing.T) {
	fake := newFakeFS().with("a.go", "package a\n")

	before := SummaryResult{
		Languages: []Count{{Name: "Go", Files: 1}, {Name: "TypeScript", Files: 5}},
	}

	result, err := Compare(context.Background(), fake, CompareRequest{
		Selection: Selection{Root: root()},
		Before:    before,
	})
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}

	ts, found := findDelta(result.Languages, "TypeScript")
	if !found {
		t.Fatalf("TypeScript is missing from %+v", result.Languages)
	}
	if ts.FilesDelta != -5 {
		t.Errorf("TypeScript filesDelta = %d, want -5 (it is gone)", ts.FilesDelta)
	}
}

func TestLoadReadsTheEnvelopeForm(t *testing.T) {
	encoded := `{"devnest":{"version":"dev"},"status":"ok","data":` +
		`{"root":"/x","files":4,"bytes":100},"warnings":[]}`

	loaded, err := Load([]byte(encoded))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Files != 4 || loaded.Bytes != 100 {
		t.Errorf("loaded = %+v, want the scan inside the envelope", loaded)
	}
}

func TestLoadReadsABareResult(t *testing.T) {
	loaded, err := Load([]byte(`{"root":"/x","files":2,"bytes":50}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Files != 2 {
		t.Errorf("loaded = %+v, want the bare result", loaded)
	}
}

func TestLoadRejectsSomethingThatIsNotAScan(t *testing.T) {
	_, err := Load([]byte(`{"hello":"world"}`))
	assertCode(t, err, errors.CodeParse)
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	_, err := Load([]byte(`not json`))
	assertCode(t, err, errors.CodeParse)
}

func findDelta(deltas []CountDelta, name string) (CountDelta, bool) {
	for _, delta := range deltas {
		if delta.Name == name {
			return delta, true
		}
	}
	return CountDelta{}, false
}
