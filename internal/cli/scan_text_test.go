package cli

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/scan"
)

func sampleSummary() scan.SummaryResult {
	return scan.SummaryResult{
		Root:          "/work/api",
		Files:         120,
		Directories:   18,
		Bytes:         2048000,
		Depth:         4,
		Authored:      96,
		AuthoredBytes: 512000,
		Ignored:       true,
		Categories: []scan.Count{
			{Name: "source", Files: 80, Bytes: 400000, Percent: 66.7},
			{Name: "test", Files: 16, Bytes: 100000, Percent: 13.3},
			{Name: "vendored", Files: 0, Bytes: 0, Percent: 0},
		},
		Languages:  []scan.Count{{Name: "Go", Files: 80, Bytes: 400000, Percent: 66.7}},
		Extensions: []scan.Count{{Name: ".go", Files: 80, Bytes: 400000, Percent: 66.7}},
		DurationMs: 42,
	}
}

func TestScanSummaryTextReportsShape(t *testing.T) {
	got := render(t, scanSummaryText(sampleSummary()))

	for _, want := range []string{
		"/work/api", "120", "2.0 MB", "96 files", "applied",
		"By category", "Top languages", "Top extensions", "66.7%", "42 ms",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}

	// Every category is listed, including the empty ones: a reader has to be
	// able to tell "none" from "not measured".
	if !strings.Contains(got, "vendored") {
		t.Errorf("output = %q, want the empty category still listed", got)
	}
}

func TestScanSummaryTextSaysWhenIgnoreRulesAreOff(t *testing.T) {
	result := sampleSummary()
	result.Ignored = false

	got := render(t, scanSummaryText(result))
	if !strings.Contains(got, "--no-ignore") {
		t.Errorf("output = %q, want it to name the flag that turned the rules off", got)
	}
}

func TestScanTypesTextNamesWhatItGroupedBy(t *testing.T) {
	result := scan.TypesResult{
		Root:         "/work/api",
		Files:        40,
		Bytes:        100000,
		Subject:      "language",
		Unrecognised: 3,
		Entries:      []scan.Count{{Name: "Go", Files: 30, Bytes: 90000, Percent: 75}},
		DurationMs:   11,
	}

	got := render(t, scanTypesText(result))
	for _, want := range []string{"language", "unrecognised", "3 files", "Go", "75.0%"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}

	clean := render(t, scanTypesText(scan.TypesResult{Subject: "extension"}))
	if strings.Contains(clean, "unrecognised") {
		t.Errorf("output = %q, want no note when everything was recognised", clean)
	}
}

// A count without its share means nothing: "412 lines of comment" needs the
// total beside it to be a fact rather than a number.
func TestScanLinesTextReportsShares(t *testing.T) {
	result := scan.LinesResult{
		Root:    "/work/api",
		Files:   50,
		Skipped: 2,
		Total:   1000,
		Code:    700,
		Comment: 100,
		Blank:   200,
		Languages: []scan.LanguageLines{
			{Language: "Go", Files: 40, Code: 700, Comment: 100, Blank: 200, Total: 1000},
		},
		DurationMs: 30,
	}

	got := render(t, scanLinesText(result))
	for _, want := range []string{"70.0%", "10.0%", "20.0%", "By language", "Go", "48"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestLinesShareHandlesAnEmptyTree(t *testing.T) {
	if got := linesShare(0, 0); got != "0" {
		t.Errorf("linesShare(0, 0) = %q, want a bare zero rather than a division by zero", got)
	}
	if got := linesShare(1, 4); got != "1 (25.0%)" {
		t.Errorf("linesShare(1, 4) = %q, want the count and its share", got)
	}
}

func TestScanTreeTextDrawsTheShape(t *testing.T) {
	result := scan.TreeResult{
		Root:        "/work/api",
		Directories: 3,
		Files:       10,
		Bytes:       4096,
		Depth:       2,
		Nodes: []scan.Node{
			{
				Name: "internal", Path: "internal", IsDir: true, Files: 8, Bytes: 3000, Depth: 1,
				Nodes: []scan.Node{
					{Name: "cli", Path: "internal/cli", IsDir: true, Files: 8, Bytes: 3000, Depth: 2},
				},
			},
			{Name: "main.go", Path: "main.go", Files: 1, Bytes: 1096, Depth: 1},
		},
	}

	got := render(t, scanTreeText(result))
	for _, want := range []string{"internal/", "cli/", "main.go", "8 files", "1 file"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
	// The last child of a branch closes it, so the shape reads correctly.
	if !strings.Contains(got, "└──") || !strings.Contains(got, "├──") {
		t.Errorf("output = %q, want both branch characters", got)
	}
}

// A listing cut at the cap says so, or a short tree reads as a small project.
func TestScanTreeTextMarksATruncatedListing(t *testing.T) {
	result := scan.TreeResult{
		Nodes:     []scan.Node{{Name: "a", Path: "a", IsDir: true, Depth: 1}},
		Truncated: true,
	}

	got := render(t, scanTreeText(result))
	if !strings.Contains(got, "...") {
		t.Errorf("output = %q, want the cut marked", got)
	}
}

// Row views carry unformatted numbers, and the tree flattens: a nested
// structure is not a CSV, and the path carries the nesting instead.
func TestScanTablesAreMachineReadable(t *testing.T) {
	summary := scanSummaryTable(sampleSummary())()
	if len(summary.Rows) != 5 {
		t.Fatalf("rows = %d, want every listing flattened", len(summary.Rows))
	}
	if summary.Rows[0][0] != "category" || summary.Rows[0][2] != "80" {
		t.Errorf("row = %v, want a section tag and an unformatted count", summary.Rows[0])
	}

	tree := scanTreeTable(scan.TreeResult{
		Nodes: []scan.Node{{
			Name: "internal", Path: "internal", IsDir: true, Files: 8, Bytes: 3000, Depth: 1,
			Nodes: []scan.Node{{Name: "cli", Path: "internal/cli", IsDir: true, Depth: 2}},
		}},
	})()
	if len(tree.Rows) != 2 {
		t.Fatalf("rows = %d, want the nested node flattened in", len(tree.Rows))
	}
	if tree.Rows[1][0] != "internal/cli" {
		t.Errorf("path = %q, want the nesting carried by the path", tree.Rows[1][0])
	}
	if tree.Rows[0][1] != "directory" {
		t.Errorf("kind = %q, want directory", tree.Rows[0][1])
	}

	lines := scanLinesTable(scan.LinesResult{
		Languages: []scan.LanguageLines{{Language: "Go", Files: 2, Code: 100, Total: 130}},
	})()
	if lines.Rows[0][2] != "100" {
		t.Errorf("code = %q, want an unformatted number", lines.Rows[0][2])
	}
}

func TestScanCompareTextShowsGrowthWithSigns(t *testing.T) {
	result := scan.CompareResult{
		Root:        "/work/api",
		FilesBefore: 100, FilesAfter: 150, FilesDelta: 50,
		BytesBefore: 1000, BytesAfter: 2000, BytesDelta: 1000,
		Categories: []scan.CountDelta{
			{Name: "source", FilesBefore: 50, FilesAfter: 80, FilesDelta: 30,
				BytesBefore: 500, BytesAfter: 800, BytesDelta: 300},
		},
		DurationMs: 5,
	}

	got := render(t, scanCompareText(result))
	for _, want := range []string{"/work/api", "100 -> 150", "+50", "+1000 B", "By category", "source"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestScanCompareTextShowsAShrinkAndAHeadingForChangedLanguages(t *testing.T) {
	result := scan.CompareResult{
		FilesBefore: 10, FilesAfter: 7, FilesDelta: -3,
		Languages: []scan.CountDelta{
			{Name: "TypeScript", FilesBefore: 5, FilesAfter: 0, FilesDelta: -5},
		},
	}

	got := render(t, scanCompareText(result))
	if !strings.Contains(got, "-3") {
		t.Errorf("output = %q, want a shrink shown as negative", got)
	}
	if !strings.Contains(got, "Languages that changed") {
		t.Errorf("output = %q, want the changed-languages heading", got)
	}
}

func TestScanCompareTableCarriesUnformattedNumbers(t *testing.T) {
	result := scan.CompareResult{
		Categories: []scan.CountDelta{
			{Name: "source", FilesBefore: 50, FilesAfter: 80, FilesDelta: 30,
				BytesBefore: 500, BytesAfter: 800, BytesDelta: 300},
		},
	}

	table := scanCompareTable(result)()
	if table.Rows[0][2] != "50" {
		t.Errorf("files_before = %q, want an unformatted number", table.Rows[0][2])
	}
	if table.Rows[0][6] != "800" {
		t.Errorf("bytes_before = %q, want an unformatted number", table.Rows[0][6])
	}
}

func TestScanCompareTargetsSplitsSnapshotFromTree(t *testing.T) {
	snapshot, tree, err := scanCompareTargets([]string{"baseline.json"})
	if err != nil || snapshot != "baseline.json" || tree != "." {
		t.Errorf("one arg = %q, %q, %v, want the snapshot and the current directory", snapshot, tree, err)
	}

	snapshot, tree, err = scanCompareTargets([]string{"baseline.json", "src"})
	if err != nil || snapshot != "baseline.json" || tree != "src" {
		t.Errorf("two args = %q, %q, %v, want snapshot and tree", snapshot, tree, err)
	}

	if _, _, err := scanCompareTargets(nil); err == nil {
		t.Error("no snapshot was accepted")
	}
}
