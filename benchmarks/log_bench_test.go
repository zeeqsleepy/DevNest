// Package benchmarks holds the measurements behind the targets in
// docs/performance.md. They run against real files in a temporary directory,
// because a benchmark against a fake measures the fake.
//
// Run with: make bench
package benchmarks

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/platform/fs"
)

// benchLines is the size of the generated access log. Two hundred thousand
// lines is around twenty megabytes, which is large enough for per-line costs
// to dominate and small enough to generate quickly.
const benchLines = 200_000

// writeAccessLog generates a realistic access log and returns its path.
func writeAccessLog(b *testing.B, lines int) string {
	b.Helper()

	path := filepath.Join(b.TempDir(), "access.log")
	file, err := os.Create(path)
	if err != nil {
		b.Fatalf("create: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			b.Fatalf("close: %v", err)
		}
	}()

	line := make([]byte, 0, 160)
	for index := range lines {
		line = line[:0]
		line = append(line, "10.0.0."...)
		line = strconv.AppendInt(line, int64(index%64), 10)
		line = append(line, " - - [24/Jul/2026:09:15:00 +0000] \"GET /api/item/"...)
		line = strconv.AppendInt(line, int64(index%500), 10)
		line = append(line, "?page="...)
		line = strconv.AppendInt(line, int64(index%7), 10)
		line = append(line, " HTTP/1.1\" "...)
		line = strconv.AppendInt(line, int64(benchStatus(index)), 10)
		line = append(line, " 4096 \"-\" \"curl/8.4.0\"\n"...)

		if _, err := file.Write(line); err != nil {
			b.Fatalf("write: %v", err)
		}
	}
	return path
}

func benchStatus(index int) int {
	switch {
	case index%97 == 0:
		return 500
	case index%13 == 0:
		return 404
	default:
		return 200
	}
}

// report records throughput in bytes per second, which is the number that
// matters for a command whose cost is dominated by reading the file.
func report(b *testing.B, path string) {
	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat: %v", err)
	}
	b.SetBytes(info.Size())
}

func BenchmarkLogAnalyze(b *testing.B) {
	path := writeAccessLog(b, benchLines)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := log.Analyze(context.Background(), fs.System{},
			log.AnalyzeRequest{Path: path}); err != nil {
			b.Fatalf("Analyze: %v", err)
		}
	}
}

func BenchmarkLogHTTPSummary(b *testing.B) {
	path := writeAccessLog(b, benchLines)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := log.SummarizeHTTP(context.Background(), fs.System{},
			log.HTTPRequest{Path: path}); err != nil {
			b.Fatalf("SummarizeHTTP: %v", err)
		}
	}
}

func BenchmarkLogErrors(b *testing.B) {
	path := writeAccessLog(b, benchLines)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := log.SummarizeErrors(context.Background(), fs.System{},
			log.ErrorsRequest{Path: path}); err != nil {
			b.Fatalf("SummarizeErrors: %v", err)
		}
	}
}

func BenchmarkLogSearch(b *testing.B) {
	path := writeAccessLog(b, benchLines)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := log.Search(context.Background(), fs.System{},
			log.SearchRequest{Path: path, Query: "/api/item/499"}); err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}

func BenchmarkLogStats(b *testing.B) {
	path := writeAccessLog(b, benchLines)
	report(b, path)

	b.ResetTimer()
	for range b.N {
		if _, err := log.Stats(context.Background(), fs.System{},
			log.StatsRequest{Path: path}); err != nil {
			b.Fatalf("Stats: %v", err)
		}
	}
}
