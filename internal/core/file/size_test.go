package file

import (
	"context"
	"strings"
	"testing"
)

func sizeFixture() *fakeFS {
	return newFakeFS().
		addFile(root("small.txt"), strings.Repeat("a", 10)).
		addFile(root("media", "clip.mp4"), strings.Repeat("b", 500)).
		addFile(root("media", "raw", "take.mov"), strings.Repeat("c", 300)).
		addFile(root("docs", "manual.pdf"), strings.Repeat("d", 100))
}

func sizeRequest() SizeRequest {
	return SizeRequest{Selection: Selection{Root: root()}}
}

func TestSizeTotals(t *testing.T) {
	result, err := Size(context.Background(), sizeFixture(), sizeRequest())
	if err != nil {
		t.Fatalf("Size: %v", err)
	}

	if result.TotalBytes != 910 {
		t.Errorf("TotalBytes = %d, want 910", result.TotalBytes)
	}
	if result.TotalFiles != 4 {
		t.Errorf("TotalFiles = %d, want 4", result.TotalFiles)
	}
}

// The measurement is always recursive, whatever the selection says, or the
// reported figures would not add up.
func TestSizeMeasuresRecursivelyRegardlessOfSelection(t *testing.T) {
	request := sizeRequest()
	request.Recursive = false

	result, err := Size(context.Background(), sizeFixture(), request)
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if result.TotalFiles != 4 {
		t.Errorf("TotalFiles = %d, want every file counted", result.TotalFiles)
	}
}

// A nested file counts towards every directory between it and the root, so a
// parent's total includes its children.
func TestSizeDirectoryTotalsNest(t *testing.T) {
	request := sizeRequest()
	request.Depth = 1

	result, err := Size(context.Background(), sizeFixture(), request)
	if err != nil {
		t.Fatalf("Size: %v", err)
	}

	byName := make(map[string]int64, len(result.Directories))
	for _, directory := range result.Directories {
		byName[directory.Relative] = directory.Bytes
	}

	if byName["media"] != 800 {
		t.Errorf("media = %d, want 800 (clip plus the nested take)", byName["media"])
	}
	if byName["docs"] != 100 {
		t.Errorf("docs = %d, want 100", byName["docs"])
	}
	if _, reported := byName["media/raw"]; reported {
		t.Error("a second-level directory was reported at depth 1")
	}
}

func TestSizeDepthControlsDetailNotMeasurement(t *testing.T) {
	request := sizeRequest()
	request.Depth = 2
	request.TopDirectories = 20

	result, err := Size(context.Background(), sizeFixture(), request)
	if err != nil {
		t.Fatalf("Size: %v", err)
	}

	found := false
	for _, directory := range result.Directories {
		if directory.Relative == "media/raw" {
			found = true
			if directory.Bytes != 300 {
				t.Errorf("media/raw = %d, want 300", directory.Bytes)
			}
		}
	}
	if !found {
		t.Error("the second-level directory was not reported at depth 2")
	}
	if result.TotalBytes != 910 {
		t.Errorf("TotalBytes = %d, the total must not change with depth", result.TotalBytes)
	}
}

func TestSizeRanksDirectoriesLargestFirst(t *testing.T) {
	result, err := Size(context.Background(), sizeFixture(), sizeRequest())
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	for index := 1; index < len(result.Directories); index++ {
		if result.Directories[index-1].Bytes < result.Directories[index].Bytes {
			t.Fatalf("not ranked by size: %v", result.Directories)
		}
	}
}

func TestSizeReportsShareOfTotal(t *testing.T) {
	result, err := Size(context.Background(), sizeFixture(), sizeRequest())
	if err != nil {
		t.Fatalf("Size: %v", err)
	}

	for _, directory := range result.Directories {
		if directory.Relative != "media" {
			continue
		}
		// 800 of 910 is a little under 88 per cent.
		if directory.Percent < 87 || directory.Percent > 88.5 {
			t.Errorf("media share = %v%%, want about 87.9", directory.Percent)
		}
	}
}

func TestSizeListsLargestFiles(t *testing.T) {
	request := sizeRequest()
	request.TopFiles = 2

	result, err := Size(context.Background(), sizeFixture(), request)
	if err != nil {
		t.Fatalf("Size: %v", err)
	}

	if len(result.LargestFiles) != 2 {
		t.Fatalf("LargestFiles = %d, want 2", len(result.LargestFiles))
	}
	if result.LargestFiles[0].Name != "clip.mp4" {
		t.Errorf("largest = %q, want clip.mp4", result.LargestFiles[0].Name)
	}
	if result.LargestFiles[1].Name != "take.mov" {
		t.Errorf("second = %q, want take.mov", result.LargestFiles[1].Name)
	}
}

// The top-N list must not grow with the tree; that is the whole reason it
// exists instead of sorting everything at the end.
func TestLargestFilesKeepsOnlyTheLimit(t *testing.T) {
	keeper := newLargestFiles(3)

	for size := int64(1); size <= 100; size++ {
		keeper.offer(Info{Name: "f", Bytes: size})
		if len(keeper.files) > 3 {
			t.Fatalf("kept %d files, want at most 3", len(keeper.files))
		}
	}

	sorted := keeper.sorted()
	if len(sorted) != 3 {
		t.Fatalf("sorted returned %d, want 3", len(sorted))
	}
	if sorted[0].Bytes != 100 || sorted[2].Bytes != 98 {
		t.Errorf("kept %v, want the three largest", sorted)
	}
}

func TestLargestFilesHandlesFewerThanTheLimit(t *testing.T) {
	keeper := newLargestFiles(10)
	keeper.offer(Info{Name: "a", Bytes: 5})
	keeper.offer(Info{Name: "b", Bytes: 9})

	sorted := keeper.sorted()
	if len(sorted) != 2 || sorted[0].Bytes != 9 {
		t.Errorf("sorted = %v, want the two files largest first", sorted)
	}
}

func TestSizeOnEmptyDirectory(t *testing.T) {
	system := newFakeFS().addDir(root())

	result, err := Size(context.Background(), system, sizeRequest())
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if result.TotalBytes != 0 || result.TotalFiles != 0 {
		t.Errorf("result = %+v, want zeroes", result)
	}
	if result.LargestFiles == nil || result.Directories == nil {
		t.Error("empty lists must be empty arrays, not null")
	}
}
