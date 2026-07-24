package git

import (
	"context"
	"strings"
	"testing"
)

func commitLine(name, email, date string) string {
	return strings.Join([]string{name, email, date}, separator)
}

func contributorFake() *fakeGit {
	return repositoryFake().answers("log", strings.Join([]string{
		commitLine("Ana", "ana@example.com", "2026-07-20T09:00:00Z"),
		commitLine("Ana", "ana@example.com", "2026-01-05T09:00:00Z"),
		commitLine("Ana", "ana@example.com", "2025-06-01T09:00:00Z"),
		commitLine("Budi", "budi@example.com", "2026-03-01T09:00:00Z"),
		// The same person, spelled differently, with the same address: one
		// contributor, three commits, not two people.
		commitLine("ana", "ANA@example.com", "2025-02-01T09:00:00Z"),
	}, "\n")+"\n")
}

func TestContributorsCountsAndRanks(t *testing.T) {
	system := contributorFake()

	result, err := Contributors(context.Background(), system, system,
		ContributorRequest{Now: reference})
	if err != nil {
		t.Fatalf("Contributors: %v", err)
	}

	if result.Commits != 5 {
		t.Fatalf("commits = %d, want every commit counted", result.Commits)
	}
	if result.Count != 2 {
		t.Fatalf("contributors = %d, want two people", result.Count)
	}

	top := result.Contributors[0]
	if top.Email != "ana@example.com" || top.Commits != 4 {
		t.Errorf("top = %+v, want ana with four commits", top)
	}
	if top.Percent != 80 {
		t.Errorf("percent = %v, want 80", top.Percent)
	}
}

// An address in two cases is one person. Splitting them produces a listing
// where the same human appears twice, which is worse than useless.
func TestContributorsFoldAddressCase(t *testing.T) {
	system := contributorFake()

	result, err := Contributors(context.Background(), system, system,
		ContributorRequest{Now: reference})
	if err != nil {
		t.Fatalf("Contributors: %v", err)
	}

	for _, contributor := range result.Contributors {
		if contributor.Email != strings.ToLower(contributor.Email) {
			t.Errorf("email = %q, want it folded to lower case", contributor.Email)
		}
	}
}

func TestContributorsReportFirstAndLastActivity(t *testing.T) {
	system := contributorFake()

	result, err := Contributors(context.Background(), system, system,
		ContributorRequest{Now: reference})
	if err != nil {
		t.Fatalf("Contributors: %v", err)
	}

	top := result.Contributors[0]
	if top.First.Year() != 2025 || top.First.Month() != 2 {
		t.Errorf("first = %v, want the earliest commit", top.First)
	}
	if top.Last.Year() != 2026 || top.Last.Month() != 7 {
		t.Errorf("last = %v, want the latest commit", top.Last)
	}
	if top.IdleDays != 4 {
		t.Errorf("idleDays = %d, want it measured against the given time", top.IdleDays)
	}
}

func TestContributorsTruncateAndSaySo(t *testing.T) {
	system := contributorFake()

	result, err := Contributors(context.Background(), system, system,
		ContributorRequest{Now: reference, Limit: 1})
	if err != nil {
		t.Fatalf("Contributors: %v", err)
	}

	if len(result.Contributors) != 1 || !result.Truncated {
		t.Errorf("result = %+v, want one entry and the truncation reported", result)
	}
	if result.Count != 2 {
		t.Errorf("count = %d, want the real number of contributors", result.Count)
	}
}

func TestLargeReportsBlobsWithTheirPaths(t *testing.T) {
	system := repositoryFake().
		answers("rev-list", strings.Join([]string{
			"aaa1111 assets/video.mp4",
			"bbb2222 src/main.go",
			"ccc3333 docs/diagram.png",
		}, "\n")+"\n").
		answers("cat-file", strings.Join([]string{
			"aaa1111 blob 50000000",
			"bbb2222 blob 4096",
			"ccc3333 blob 250000",
			"ddd4444 blob 900000", // unreachable: no path in rev-list
			"eee5555 tree 128",    // not file content
		}, "\n")+"\n")

	result, err := Large(context.Background(), system, system, LargeRequest{})
	if err != nil {
		t.Fatalf("Large: %v", err)
	}

	if result.Count != 3 {
		t.Fatalf("count = %d, want the three reachable blobs", result.Count)
	}
	if result.Objects[0].Path != "assets/video.mp4" || result.Objects[0].Bytes != 50000000 {
		t.Errorf("first = %+v, want the largest object first", result.Objects[0])
	}
	if result.TotalBytes != 50000000+4096+250000 {
		t.Errorf("totalBytes = %d, want the listed objects added up", result.TotalBytes)
	}
}

func TestLargeRespectsItsLimit(t *testing.T) {
	system := repositoryFake().
		answers("rev-list", "aaa1111 a\nbbb2222 b\nccc3333 c\n").
		answers("cat-file", "aaa1111 blob 30\nbbb2222 blob 20\nccc3333 blob 10\n")

	result, err := Large(context.Background(), system, system, LargeRequest{Limit: 2})
	if err != nil {
		t.Fatalf("Large: %v", err)
	}

	if len(result.Objects) != 2 {
		t.Fatalf("objects = %d, want the limit respected", len(result.Objects))
	}
	if result.Scanned != 3 {
		t.Errorf("scanned = %d, want the whole set reported as examined", result.Scanned)
	}
}

func TestLargeHandlesAnEmptyRepository(t *testing.T) {
	system := repositoryFake().answers("rev-list", "")

	result, err := Large(context.Background(), system, system, LargeRequest{})
	if err != nil {
		t.Fatalf("Large: %v", err)
	}
	if result.Count != 0 || len(result.Objects) != 0 {
		t.Errorf("result = %+v, want an empty report", result)
	}
}
