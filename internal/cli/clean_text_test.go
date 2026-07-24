package cli

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/core/clean"
)

func candidate(relative, ecosystem string, bytes int64, files int) clean.Candidate {
	return clean.Candidate{
		Path:      "/project/" + relative,
		Name:      relative,
		Relative:  relative,
		Ecosystem: ecosystem,
		Bytes:     bytes,
		Files:     files,
	}
}

// A dry run has to say, in as many words, that it did nothing. The whole
// safety design rests on the user knowing which mode they are in.
func TestCleanScanTextSaysNothingWasDeleted(t *testing.T) {
	result := clean.ScanResult{
		Root:       "/project",
		Candidates: []clean.Candidate{candidate("node_modules", "node", 5<<20, 1200)},
		Count:      1,
		TotalBytes: 5 << 20,
		TotalFiles: 1200,
	}

	got := render(t, cleanScanText(result))
	for _, want := range []string{"node_modules", "5.0 MB", "1,200", "Nothing has been deleted", "--apply"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestCleanScanTextHandlesACleanProject(t *testing.T) {
	got := render(t, cleanScanText(clean.ScanResult{Root: "/project"}))

	if !strings.Contains(got, "Nothing to clean") {
		t.Errorf("output = %q, want a sentence rather than an empty table", got)
	}
	if strings.Contains(got, "--apply") {
		t.Errorf("output = %q, want no invitation to delete nothing", got)
	}
}

// The csv view carries raw numbers. A spreadsheet given "5.0 MB" has to be
// told it is a number, and mostly is not.
func TestCleanScanTableIsMachineReadable(t *testing.T) {
	result := clean.ScanResult{
		Candidates: []clean.Candidate{candidate("dist", "build output", 2048, 3)},
	}

	table := cleanScanTable(result)()
	if len(table.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(table.Rows))
	}
	if table.Rows[0][2] != "2048" || table.Rows[0][3] != "3" {
		t.Errorf("row = %v, want unformatted numbers", table.Rows[0])
	}
}

func TestCleanApplyTextReportsWhatWentAndWhatDidNot(t *testing.T) {
	result := clean.ApplyResult{
		Root: "/project",
		Removed: []clean.Removal{
			{Relative: "node_modules", Ecosystem: "node", Bytes: 3 << 20, Files: 900},
		},
		Failed:     []clean.Failure{{Path: "/project/dist", Reason: "the directory is in use"}},
		Count:      1,
		BytesFreed: 3 << 20,
	}

	got := render(t, cleanApplyText(result))
	for _, want := range []string{"removed", "node_modules", "3.0 MB", "could not be removed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestCleanRulesTextExplainsWhatEachRuleNeeds(t *testing.T) {
	got := render(t, cleanRulesText(clean.Rules()))

	if !strings.Contains(got, "node_modules") {
		t.Errorf("output = %q, want the rules listed", got)
	}
	if !strings.Contains(got, "nothing: the name is unambiguous") {
		t.Errorf("output = %q, want a marker-free rule explained", got)
	}
}

func TestMarkerSummaryStaysReadable(t *testing.T) {
	none := markerSummary(clean.Rule{Name: "node_modules"})
	if !strings.Contains(none, "unambiguous") {
		t.Errorf("summary = %q, want it to say the name is enough", none)
	}

	few := markerSummary(clean.Rule{Name: "target", Markers: []string{"Cargo.toml", "pom.xml"}})
	if few != "Cargo.toml, pom.xml" {
		t.Errorf("summary = %q, want both markers listed", few)
	}

	many := markerSummary(clean.Rule{
		Name:    "dist",
		Markers: []string{"package.json", "go.mod", "Cargo.toml", "pom.xml"},
	})
	if !strings.Contains(many, "2 other project files") {
		t.Errorf("summary = %q, want a long list summarised", many)
	}
}

func TestCleanRootDefaultsToHere(t *testing.T) {
	root, err := cleanRoot(nil)
	if err != nil || root != "." {
		t.Errorf("cleanRoot(nil) = %q, %v, want the current directory", root, err)
	}

	if _, err := cleanRoot([]string{"a", "b"}); err == nil {
		t.Error("two directories were accepted")
	}
}
