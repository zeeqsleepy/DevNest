package git

import (
	"context"
	"testing"
)

func TestHotspotCountsFilesByChangedCommits(t *testing.T) {
	system := repositoryFake().answers("log hotspot",
		"internal/core/git/git.go\ninternal/core/git/hotspot.go\n\n"+
			"internal/core/git/git.go\n\n"+
			"docs/commands.md\ninternal/core/git/git.go\n")

	result, err := Hotspot(context.Background(), system, system, HotspotRequest{})
	if err != nil {
		t.Fatalf("Hotspot: %v", err)
	}

	if result.Commits != 3 {
		t.Errorf("commits = %d, want 3", result.Commits)
	}
	if result.DistinctFiles != 3 {
		t.Errorf("distinctFiles = %d, want 3", result.DistinctFiles)
	}

	// git.go changed in three commits, the other two files in one each.
	want := []struct {
		path    string
		commits int
	}{
		{"internal/core/git/git.go", 3},
		{"docs/commands.md", 1},
		{"internal/core/git/hotspot.go", 1},
	}
	if len(result.Files) != len(want) {
		t.Fatalf("files = %+v, want %d entries", result.Files, len(want))
	}
	for index, entry := range want {
		if got := result.Files[index]; got.Path != entry.path || got.Commits != entry.commits {
			t.Errorf("file[%d] = %+v, want %s (%d)", index, got, entry.path, entry.commits)
		}
	}
}

func TestHotspotCanLimitTheListing(t *testing.T) {
	system := repositoryFake().answers("log hotspot",
		"a.go\nb.go\n\n"+
			"a.go\n\n"+
			"a.go\n")

	result, err := Hotspot(context.Background(), system, system, HotspotRequest{Limit: 1})
	if err != nil {
		t.Fatalf("Hotspot: %v", err)
	}

	if result.Truncated != true {
		t.Error("a limited listing was not marked truncated")
	}
	if result.DistinctFiles != 2 {
		t.Errorf("distinctFiles = %d, want 2 (the whole history, not the subset)", result.DistinctFiles)
	}
	if len(result.Files) != 1 || result.Files[0].Path != "a.go" {
		t.Errorf("files = %+v, want only the most-changed file", result.Files)
	}
}

// An empty history has no files to report, which is a state, not a failure.
func TestHotspotHandlesAnEmptyRepository(t *testing.T) {
	system := repositoryFake().answers("log hotspot", "")

	result, err := Hotspot(context.Background(), system, system, HotspotRequest{})
	if err != nil {
		t.Fatalf("Hotspot: %v", err)
	}

	if result.Commits != 0 || result.DistinctFiles != 0 || len(result.Files) != 0 {
		t.Errorf("result = %+v, want an empty listing", result)
	}
}

func TestHotspotPassesASinceNarrowingHistory(t *testing.T) {
	system := repositoryFake().answers("log hotspot",
		"a.go\n\nb.go\n")

	_, err := Hotspot(context.Background(), system, system, HotspotRequest{Since: "6 months ago"})
	if err != nil {
		t.Fatalf("Hotspot: %v", err)
	}
}
