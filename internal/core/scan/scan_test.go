package scan

import (
	"context"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/classify"
)

func TestSummarizeCountsWhatTheProjectHolds(t *testing.T) {
	result, err := Summarize(context.Background(), project(), SummaryRequest{
		Selection: Selection{Root: root()},
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	// node_modules and dist are skipped, so the two files in them are not
	// counted at all: ten files remain.
	if result.Files != 10 {
		t.Errorf("files = %d, want 10 with vendored and build output skipped", result.Files)
	}
	if !result.Ignored {
		t.Error("ignore rules were reported as off")
	}

	requireCount(t, result.Categories, string(classify.CategorySource), 3)
	requireCount(t, result.Categories, string(classify.CategoryTest), 2)
	requireCount(t, result.Categories, string(classify.CategoryGenerated), 1)
	requireCount(t, result.Categories, string(classify.CategoryDocs), 2)
	requireCount(t, result.Categories, string(classify.CategoryAsset), 1)
	requireCount(t, result.Categories, string(classify.CategoryConfig), 1)

	// Authored leaves out the generated file and the asset.
	if result.Authored != 8 {
		t.Errorf("authored = %d, want 8", result.Authored)
	}
}

// Every category is reported, including the ones with nothing in them, so a
// reader can tell "none" from "not measured".
func TestSummarizeReportsEveryCategory(t *testing.T) {
	result, err := Summarize(context.Background(), project(), SummaryRequest{
		Selection: Selection{Root: root()},
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if len(result.Categories) != len(classify.Categories()) {
		t.Fatalf("categories = %d, want all %d",
			len(result.Categories), len(classify.Categories()))
	}
	if vendored, _ := find(result.Categories, string(classify.CategoryVendored)); vendored.Files != 0 {
		t.Errorf("vendored = %d, want 0 and still listed", vendored.Files)
	}
}

// --no-ignore is the whole truth: vendored and build directories included.
func TestSummarizeWithoutIgnoreRulesSeesEverything(t *testing.T) {
	result, err := Summarize(context.Background(), project(), SummaryRequest{
		Selection: Selection{Root: root(), NoIgnore: true},
	})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if result.Files != 12 {
		t.Errorf("files = %d, want all 12", result.Files)
	}
	if result.Ignored {
		t.Error("ignore rules were reported as applied under --no-ignore")
	}
	requireCount(t, result.Categories, string(classify.CategoryVendored), 1)
	requireCount(t, result.Categories, string(classify.CategoryBuild), 1)
}

func TestSummarizeRejectsAFile(t *testing.T) {
	_, err := Summarize(context.Background(), project(), SummaryRequest{
		Selection: Selection{Root: root("main.go")},
	})
	assertCode(t, err, "INVALID_INPUT")
}

func TestTypesGroupsByExtensionOrLanguage(t *testing.T) {
	byExtension, err := Types(context.Background(), project(), TypesRequest{
		Selection: Selection{Root: root()},
	})
	if err != nil {
		t.Fatalf("Types: %v", err)
	}
	if byExtension.Subject != "extension" {
		t.Errorf("subject = %q, want extension", byExtension.Subject)
	}
	requireCount(t, byExtension.Entries, ".go", 4)
	requireCount(t, byExtension.Entries, ".md", 2)

	byLanguage, err := Types(context.Background(), project(), TypesRequest{
		Selection:  Selection{Root: root()},
		ByLanguage: true,
	})
	if err != nil {
		t.Fatalf("Types: %v", err)
	}
	requireCount(t, byLanguage.Entries, "Go", 4)
	requireCount(t, byLanguage.Entries, "TypeScript", 2)

	// The PNG has no language, and saying so is more useful than dropping it.
	if byLanguage.Unrecognised != 1 {
		t.Errorf("unrecognised = %d, want 1 (the png)", byLanguage.Unrecognised)
	}
}

func TestLinesSplitsCodeCommentAndBlank(t *testing.T) {
	source := "package main\n" +
		"\n" +
		"// a line comment\n" +
		"/* a block\n" +
		"   that spans lines */\n" +
		"func main() {\n" +
		"\treturn\n" +
		"}\n"

	fake := newFakeFS().with("main.go", source)
	result, err := Lines(context.Background(), fake, LinesRequest{
		Selection: Selection{Root: root()},
	})
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}

	if result.Total != 8 {
		t.Errorf("total = %d, want 8", result.Total)
	}
	if result.Blank != 1 {
		t.Errorf("blank = %d, want 1", result.Blank)
	}
	if result.Comment != 3 {
		t.Errorf("comment = %d, want 3: one line comment and two block lines", result.Comment)
	}
	if result.Code != 4 {
		t.Errorf("code = %d, want 4", result.Code)
	}
	if len(result.Languages) != 1 || result.Languages[0].Language != "Go" {
		t.Fatalf("languages = %v, want one Go entry", result.Languages)
	}
}

// Only files in a recognised language are opened. A PNG has no lines, and
// reading every binary in a tree to find that out is the slowest possible way
// to learn nothing.
func TestLinesIgnoresWhatItCannotCount(t *testing.T) {
	result, err := Lines(context.Background(), project(), LinesRequest{
		Selection: Selection{Root: root()},
	})
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}

	for _, language := range result.Languages {
		if language.Language == "" {
			t.Error("a file with no language was counted")
		}
	}
	if result.Total == 0 {
		t.Error("nothing was counted at all")
	}
}

// A file above the size cap is counted as a file and not read. A minified
// bundle is not something a person wrote.
func TestLinesSkipsHugeFiles(t *testing.T) {
	fake := newFakeFS().
		with("small.go", "package main\n").
		with("huge.go", strings.Repeat("x = 1\n", 500))

	result, err := Lines(context.Background(), fake, LinesRequest{
		Selection:    Selection{Root: root()},
		MaxFileBytes: 100,
	})
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}

	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
	}
	if result.Total != 1 {
		t.Errorf("total = %d, want only the small file's line", result.Total)
	}
}

func TestTreeReportsTotalsBelowTheDisplayDepth(t *testing.T) {
	result, err := Tree(context.Background(), project(), TreeRequest{
		Selection: Selection{Root: root()},
		Depth:     1,
	})
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	var src *Node
	for index := range result.Nodes {
		if result.Nodes[index].Name == "src" {
			src = &result.Nodes[index]
		}
	}
	if src == nil {
		t.Fatalf("src is missing from %v", result.Nodes)
	}

	// Depth 1 shows src without its contents, and still reports what is in
	// it. A collapsed branch that read as empty would be worse than useless.
	if len(src.Nodes) != 0 {
		t.Errorf("src has %d children at depth 1, want none shown", len(src.Nodes))
	}
	if src.Files != 2 {
		t.Errorf("src reports %d files, want the 2 inside it", src.Files)
	}
}

func TestTreeIncludesFilesOnRequest(t *testing.T) {
	fake := newFakeFS().with("main.go", "package main\n").with("src/app.ts", "const a = 1;\n")

	withFiles, err := Tree(context.Background(), fake, TreeRequest{
		Selection: Selection{Root: root()},
		Files:     true,
		Depth:     2,
	})
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	names := make([]string, 0, len(withFiles.Nodes))
	for _, node := range withFiles.Nodes {
		names = append(names, node.Name)
	}
	if len(names) != 2 {
		t.Fatalf("nodes = %v, want the directory and the file", names)
	}
	// Directories sort before files, so the shape reads top-down.
	if names[0] != "src" || names[1] != "main.go" {
		t.Errorf("nodes = %v, want src before main.go", names)
	}
}

func TestTreeCapsChildrenPerDirectory(t *testing.T) {
	fake := newFakeFS()
	for index := range 10 {
		fake = fake.with("pkg/file"+string(rune('a'+index))+".go", "package pkg\n")
	}

	result, err := Tree(context.Background(), fake, TreeRequest{
		Selection:  Selection{Root: root("pkg")},
		Files:      true,
		MaxEntries: 3,
	})
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}

	if len(result.Nodes) != 3 {
		t.Errorf("nodes = %d, want the cap of 3", len(result.Nodes))
	}
	if !result.Truncated {
		t.Error("a listing cut at the cap must say so")
	}
}
