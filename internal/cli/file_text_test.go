package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

// render runs a text view and returns what it wrote. These functions are the
// user-facing half of every command, and a formatting mistake in one of them
// is as real a defect as a wrong result.
func render(t *testing.T, text output.TextFunc) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := text(&buffer); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buffer.String()
}

func TestOrganizeTextDryRunSaysNothingChanged(t *testing.T) {
	result := file.OrganizeResult{
		Root:    "/work",
		Applied: false,
		Planned: 2,
		Bytes:   2048,
		Moves: []file.Move{
			{Source: "/work/a.jpg", Destination: "/work/Images/jpg/a.jpg", Status: file.MovePlanned},
			{Source: "/work/b.pdf", Destination: "/work/Documents/pdf/b.pdf", Status: file.MovePlanned},
		},
		Folders: []file.FolderSummary{{Folder: "Images/jpg", Files: 1, Bytes: 1024}},
	}

	got := render(t, organizeText(result))
	if !strings.Contains(got, "Planned") {
		t.Errorf("output = %q, want it marked as a plan", got)
	}
	if !strings.Contains(got, "--apply") {
		t.Errorf("output = %q, want it to name --apply", got)
	}
	if !strings.Contains(got, "Images/jpg") {
		t.Errorf("output = %q, want the folder listing", got)
	}
}

func TestOrganizeTextAppliedReportsWhatMoved(t *testing.T) {
	result := file.OrganizeResult{
		Root:    "/work",
		Applied: true,
		Moved:   3,
		Skipped: 1,
		Failed:  1,
		Bytes:   4096,
		Moves:   []file.Move{{Source: "/work/a.jpg", Status: file.MoveDone}},
	}

	got := render(t, organizeText(result))
	for _, want := range []string{"Organised", "3 files moved", "1 skipped", "1 failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "--apply") {
		t.Errorf("output = %q, should not suggest --apply after applying", got)
	}
}

func TestOrganizeTextOnAnEmptyDirectory(t *testing.T) {
	got := render(t, organizeText(file.OrganizeResult{Root: "/work"}))
	if !strings.Contains(got, "Nothing to organise") {
		t.Errorf("output = %q", got)
	}
}

func TestDuplicateTextListsGroups(t *testing.T) {
	result := file.DuplicateResult{
		Root:         "/work",
		FilesScanned: 10,
		FilesHashed:  4,
		Duplicates:   1,
		Wasted:       1024,
		Groups: []file.DuplicateGroup{{
			Hash:       strings.Repeat("a", 64),
			Bytes:      1024,
			Original:   file.Info{Relative: "manual.pdf"},
			Duplicates: []file.Info{{Relative: "copy.pdf"}},
			Wasted:     1024,
		}},
	}

	got := render(t, duplicateText(result))
	for _, want := range []string{"original", "duplicate", "manual.pdf", "copy.pdf", "1 group,"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
	// The full digest belongs in the structured output, not the terminal.
	if strings.Contains(got, strings.Repeat("a", 64)) {
		t.Error("the full hash was printed; it should be shortened for the terminal")
	}
}

func TestDuplicateTextWhenNothingFound(t *testing.T) {
	got := render(t, duplicateText(file.DuplicateResult{Root: "/work", FilesScanned: 7}))
	if !strings.Contains(got, "No duplicates") {
		t.Errorf("output = %q", got)
	}
}

func TestRenameTextPreviewMentionsRollback(t *testing.T) {
	result := file.RenameResult{
		Root:    "/work",
		Planned: 1,
		Renames: []file.Rename{
			{OldName: "IMG_1.jpg", NewName: "photo-1.jpg", Status: file.RenamePlanned},
		},
	}

	got := render(t, renameText(result))
	for _, want := range []string{"IMG_1.jpg", "photo-1.jpg", "--apply", "rollback"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestRenameTextAppliedCountsOutcomes(t *testing.T) {
	result := file.RenameResult{
		Root:      "/work",
		Applied:   true,
		Renamed:   2,
		Unchanged: 1,
		Failed:    1,
		Renames: []file.Rename{
			{OldName: "a.txt", NewName: "x-a.txt", Status: file.RenameDone},
		},
	}

	got := render(t, renameText(result))
	for _, want := range []string{"2 files renamed", "1 file already named", "1 file failed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestFilterTextShowsTheTableAndTotals(t *testing.T) {
	result := file.FilterResult{
		Root:       "/work",
		Matched:    2,
		Scanned:    9,
		TotalBytes: 3072,
		Files: []file.Info{
			{Relative: "manual.pdf", Bytes: 2048, Category: "Documents"},
			{Relative: "notes.txt", Bytes: 1024, Category: "Documents"},
		},
	}

	got := render(t, filterText(result))
	for _, want := range []string{"manual.pdf", "2.0 KB", "Documents", "2 files matched"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestFilterTextReportsTruncation(t *testing.T) {
	result := file.FilterResult{
		Root:      "/work",
		Matched:   50,
		Truncated: true,
		Files:     []file.Info{{Relative: "a.txt", Bytes: 1}},
	}

	got := render(t, filterText(result))
	if !strings.Contains(got, "Showing") {
		t.Errorf("output = %q, want it to say the list was cut short", got)
	}
}

func TestFilterTextWhenNothingMatches(t *testing.T) {
	got := render(t, filterText(file.FilterResult{Root: "/work", Scanned: 12}))
	if !strings.Contains(got, "No matching files") {
		t.Errorf("output = %q", got)
	}
}

func TestSizeTextShowsBothTables(t *testing.T) {
	result := file.SizeResult{
		Root:             "/work",
		TotalBytes:       1048576,
		TotalFiles:       4,
		TotalDirectories: 2,
		Directories: []file.DirectoryUsage{
			{Relative: "media", Bytes: 900000, Files: 2, Percent: 85.8},
		},
		LargestFiles: []file.Info{{Relative: "media/clip.mp4", Bytes: 800000}},
	}

	got := render(t, sizeText(result))
	for _, want := range []string{"Largest directories", "Largest files", "media", "85.8%", "1.0 MB"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// One file with one digest is the common case, and a table for a single value
// is noise.
func TestHashTextPrintsASingleDigestPlainly(t *testing.T) {
	result := file.HashResult{
		Files: []file.Digest{{
			Name:      "installer.exe",
			Bytes:     1024,
			Checksums: []fs.Checksum{{Algorithm: "sha256", Value: "abc123"}},
		}},
	}

	got := strings.TrimSpace(render(t, hashText(result)))
	if got != "abc123  installer.exe" {
		t.Errorf("output = %q, want the digest and the name on one line", got)
	}
}

func TestHashTextTabulatesSeveralDigests(t *testing.T) {
	result := file.HashResult{
		Files: []file.Digest{{
			Name:  "installer.exe",
			Bytes: 1024,
			Checksums: []fs.Checksum{
				{Algorithm: "sha256", Value: "aaa"},
				{Algorithm: "md5", Value: "bbb"},
			},
		}},
	}

	got := render(t, hashText(result))
	for _, want := range []string{"algorithm", "sha256", "md5", "1.0 KB"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestWriteProblemsSummarisesAndCapsTheList(t *testing.T) {
	problems := make([]file.Problem, 0, 9)
	for index := 0; index < 9; index++ {
		problems = append(problems, file.Problem{
			Path:    "/work/file",
			Message: "access is denied",
		})
	}

	var buffer bytes.Buffer
	if err := writeProblems(&buffer, problems); err != nil {
		t.Fatalf("writeProblems: %v", err)
	}

	got := buffer.String()
	if !strings.Contains(got, "9 entries could not be read") {
		t.Errorf("output = %q", got)
	}
	if !strings.Contains(got, "and 4 more") {
		t.Errorf("output = %q, want the list capped", got)
	}
}

func TestWriteProblemsWritesNothingWhenThereAreNone(t *testing.T) {
	var buffer bytes.Buffer
	if err := writeProblems(&buffer, nil); err != nil {
		t.Fatalf("writeProblems: %v", err)
	}
	if buffer.Len() != 0 {
		t.Errorf("output = %q, want nothing", buffer.String())
	}
}

func TestShortHash(t *testing.T) {
	if got := shortHash("abc"); got != "abc" {
		t.Errorf("shortHash(short) = %q, want it unchanged", got)
	}
	long := strings.Repeat("f", 64)
	if got := shortHash(long); len(got) != 19 || !strings.HasSuffix(got, "...") {
		t.Errorf("shortHash(long) = %q", got)
	}
}

func TestFirstPathDefaultsToTheCurrentDirectory(t *testing.T) {
	if got := firstPath(nil); got != "." {
		t.Errorf("firstPath(nil) = %q, want %q", got, ".")
	}
	if got := firstPath([]string{"/work"}); got != "/work" {
		t.Errorf("firstPath = %q", got)
	}
}

func TestSelectionFoldsInConfiguredExclusions(t *testing.T) {
	env, _, _ := newTestEnv(t, "table")
	env.Config.Scan.Exclude = []string{".git"}
	env.Config.Scan.FollowSymlinks = true

	flags := selectionFlags{exclude: repeatable{"dist"}}
	selection := flags.selection(env, "/work")

	if len(selection.Exclude) != 2 {
		t.Errorf("Exclude = %v, want the configured and the flag values", selection.Exclude)
	}
	if !selection.FollowSymlinks {
		t.Error("FollowSymlinks was not taken from the configuration")
	}
	if selection.Root != "/work" {
		t.Errorf("Root = %q", selection.Root)
	}
}

func TestFileRowsAndColumnsLineUp(t *testing.T) {
	rows := fileRows([]file.Info{
		{Relative: "a.txt", Bytes: 1024, Category: "Documents", ModifiedAt: time.Now()},
	})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if len(rows[0]) != len(fileColumns()) {
		t.Errorf("a row has %d cells but there are %d columns", len(rows[0]), len(fileColumns()))
	}
}
