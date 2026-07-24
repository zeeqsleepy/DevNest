package log

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"strconv"
	"testing"

	"github.com/devnest/devnest/internal/platform/fs"
)

// largeLines is the size of the synthetic log the scale tests read. Large
// enough that a per-line allocation would show up plainly in the numbers,
// small enough that the suite stays fast under the race detector.
const largeLines = 200_000

// generator is a Reader whose file is produced as it is read.
//
// The point is that the test never holds the log either: a fixture built as a
// twenty megabyte string in memory would prove nothing about a module whose
// whole claim is that it does not need one.
type generator struct {
	lines int
}

func (g generator) Resolve(path string) (string, error) { return path, nil }

func (g generator) Stat(path string) (fs.Entry, error) {
	return fs.Entry{Path: path, Name: path, Bytes: int64(g.lines) * 96}, nil
}

func (g generator) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(&syntheticLog{remaining: g.lines}), nil
}

// syntheticLog writes access log entries on demand.
//
// It builds each line into a slice it reuses rather than formatting with fmt,
// because the allocation test measures the whole process: a generator that
// allocated per line would be measuring itself.
type syntheticLog struct {
	remaining int
	written   int
	line      []byte
	buffer    bytes.Buffer
}

func (s *syntheticLog) Read(destination []byte) (int, error) {
	for s.buffer.Len() < len(destination) && s.written < s.remaining {
		s.line = appendSyntheticLine(s.line[:0], s.written)
		s.buffer.Write(s.line)
		s.written++
	}
	if s.buffer.Len() == 0 {
		return 0, io.EOF
	}
	return s.buffer.Read(destination)
}

func appendSyntheticLine(line []byte, index int) []byte {
	line = append(line, "10.0.0."...)
	line = strconv.AppendInt(line, int64(index%50), 10)
	line = append(line, " - - [24/Jul/2026:09:15:00 +0000] \"GET /api/item/"...)
	line = strconv.AppendInt(line, int64(index), 10)
	line = append(line, " HTTP/1.1\" "...)
	line = strconv.AppendInt(line, int64(syntheticStatus(index)), 10)
	line = append(line, " 1024\n"...)
	return line
}

// syntheticStatus gives the generated log a realistic mix: mostly 200, a few
// 404s, and the occasional 500.
func syntheticStatus(index int) int {
	switch {
	case index%97 == 0:
		return 500
	case index%13 == 0:
		return 404
	default:
		return 200
	}
}

func TestLargeFileIsCountedCorrectly(t *testing.T) {
	result, err := SummarizeHTTP(context.Background(), generator{lines: largeLines},
		HTTPRequest{Path: "huge.log"})
	if err != nil {
		t.Fatalf("SummarizeHTTP: %v", err)
	}

	if result.Requests != largeLines {
		t.Errorf("requests = %d, want %d", result.Requests, largeLines)
	}
	if result.Unparsed != 0 {
		t.Errorf("unparsed = %d, want 0", result.Unparsed)
	}
	if result.UniqueIPs != 50 {
		t.Errorf("unique clients = %d, want 50", result.UniqueIPs)
	}

	wantServerErrors := 0
	wantNotFound := 0
	for index := range largeLines {
		switch syntheticStatus(index) {
		case 500:
			wantServerErrors++
		case 404:
			wantNotFound++
		}
	}
	requireCount(t, result.StatusCodes, "500", wantServerErrors)
	requireCount(t, result.StatusCodes, "404", wantNotFound)
}

// The claim the module is designed around: memory use does not follow the size
// of the file. A log of a few hundred thousand lines must not cost anything
// like its own size in allocations.
func TestLargeFileDoesNotAllocateInProportionToItsSize(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation measurement is noisy under -short")
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	result, err := Stats(context.Background(), generator{lines: largeLines},
		StatsRequest{Path: "huge.log"})
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}

	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc

	// Stats keeps ten lines and a handful of counters, so the read buffer is
	// most of what it should ever allocate. A tenth of the file is a generous
	// ceiling that still fails loudly if a per-line allocation creeps in.
	//
	// The generator itself allocates while formatting each line, so this
	// measures the test harness as well as the module. That only makes the
	// bound harder to meet, not easier.
	ceiling := uint64(result.Bytes / 10)
	if allocated > ceiling {
		t.Errorf("allocated %d bytes reading a %d byte log, want under %d",
			allocated, result.Bytes, ceiling)
	}
	if result.Lines != largeLines {
		t.Errorf("lines = %d, want %d", result.Lines, largeLines)
	}
}

// A search over a large file reads all of it, so the count is real, while the
// listing stays bounded.
func TestLargeFileSearchIsBounded(t *testing.T) {
	result, err := Search(context.Background(), generator{lines: largeLines},
		SearchRequest{Path: "huge.log", Query: "GET /api/item/", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if result.Matches != largeLines {
		t.Errorf("matches = %d, want %d", result.Matches, largeLines)
	}
	if len(result.Results) != 10 {
		t.Errorf("listed %d matches, want 10", len(result.Results))
	}
	if !result.Limited {
		t.Error("a capped listing must say so")
	}
}
