package log

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/fs"
)

// The tail shows the last Count lines that were already in the file, and then
// every line appended after the command started.
func TestFollowSeedsItsTailAndReportsGrowth(t *testing.T) {
	reader := newFakeReader().with("logs/app.log", "line 1\nline 2\nline 3\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var batches [][]string
	done := make(chan error, 1)
	go func() {
		done <- Follow(ctx, reader, FollowRequest{
			Path:     "logs/app.log",
			Count:    2,
			Interval: 5 * time.Millisecond,
		}, func(lines []string) {
			batches = append(batches, lines)
		})
	}()

	waitFor(t, func() bool { return len(batches) > 0 })
	reader.adjust("logs/app.log", "line 1\nline 2\nline 3\nline 4\n")
	waitFor(t, func() bool { return strings.Contains(join(batches), "line 4") })
	reader.adjust("logs/app.log", "line 1\nline 2\nline 3\nline 4\nline 5\n")
	waitFor(t, func() bool { return strings.Contains(join(batches), "line 5") })
	cancel()

	if err := <-done; err != nil && errors.CodeOf(err) != errors.CodeCancelled {
		t.Fatalf("Follow: %v", err)
	}

	all := join(batches)
	for _, want := range []string{"line 2", "line 3", "line 4", "line 5"} {
		if !strings.Contains(all, want+"\n") {
			t.Errorf("line %q not reported: %q", want, all)
		}
	}
	if strings.Contains(all, "line 1\n") {
		t.Errorf("reported an old line outside the seeded tail: %q", all)
	}
}

// A count of zero means "only what appears after I start looking", which suits
// a log that rotates or a service still booting.
func TestFollowWithZeroCountShowsNothingExisting(t *testing.T) {
	reader := newFakeReader().with("logs/app.log", "old 1\nold 2\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var batches [][]string
	done := make(chan error, 1)
	go func() {
		done <- Follow(ctx, reader, FollowRequest{
			Path:     "logs/app.log",
			Count:    0,
			Interval: 5 * time.Millisecond,
		}, func(lines []string) {
			batches = append(batches, lines)
		})
	}()

	time.Sleep(30 * time.Millisecond)
	reader.adjust("logs/app.log", "old 1\nold 2\nnew\n")
	waitFor(t, func() bool { return strings.Contains(join(batches), "new") })
	cancel()
	<-done

	all := join(batches)
	if !strings.Contains(all, "new\n") {
		t.Errorf("new line not reported: %q", all)
	}
	if strings.Contains(all, "old") {
		t.Errorf("existing lines were reported with --lines 0: %q", all)
	}
}

// A file that is rotated (its size shrinks) is picked up from the start of the
// replacement, which is what a person tailing a rotated log expects.
func TestFollowPicksUpARotatedLog(t *testing.T) {
	reader := newFakeReader().with("logs/app.log", "a long original log line\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var batches [][]string
	done := make(chan error, 1)
	go func() {
		done <- Follow(ctx, reader, FollowRequest{
			Path:     "logs/app.log",
			Count:    0,
			Interval: 5 * time.Millisecond,
		}, func(lines []string) {
			batches = append(batches, lines)
		})
	}()

	time.Sleep(30 * time.Millisecond)
	reader.adjust("logs/app.log", "fresh start\n")
	waitFor(t, func() bool { return strings.Contains(join(batches), "fresh start") })
	cancel()
	<-done

	all := join(batches)
	if !strings.Contains(all, "fresh start\n") {
		t.Errorf("rotated content not reported: %q", all)
	}
}

func TestFollowRefusesANegativeCount(t *testing.T) {
	reader := newFakeReader().with("logs/app.log", "a\n")
	err := Follow(context.Background(), reader, FollowRequest{
		Path:  "logs/app.log",
		Count: -1,
	}, func([]string) {})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestFollowRefusesANegativeInterval(t *testing.T) {
	// No way to ask for one today, but the guard exists so a caller cannot
	// spin the poll loop by accident.
	reader := newFakeReader().with("logs/app.log", "a\n")
	err := Follow(context.Background(), reader, FollowRequest{
		Path:     "logs/app.log",
		Count:    1,
		Interval: -time.Second,
	}, func([]string) {})
	assertCode(t, err, errors.CodeInvalidInput)
}

// The real filesystem is the path every poll takes after the seed: a seek to
// where the last read stopped and a scan of only what was added. The fake's
// readers cannot seek, so this path needs a real file to be sure the offset
// arithmetic is right — the regression this catches reported relative offsets
// back to the loop, which made the next poll seek into the middle of a line
// and re-emit half the file.
func TestFollowOnARealDiskReadsOnlyWhatsNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	writeFollowFixture(t, path, "start line 1\nstart line 2\nstart line 3\nstart line 4\nstart line 5\nstart line 6\n")

	ctx, cancel := context.WithCancel(context.Background())

	var batches [][]string
	done := make(chan error, 1)
	go func() {
		done <- Follow(ctx, fs.System{}, FollowRequest{
			Path:     path,
			Count:    3,
			Interval: 10 * time.Millisecond,
		}, func(lines []string) {
			batches = append(batches, lines)
		})
	}()

	waitFor(t, func() bool { return strings.Contains(join(batches), "start line 6") })
	appendFollowFixture(t, path, "appended line A\n")
	waitFor(t, func() bool { return strings.Contains(join(batches), "appended line A") })
	appendFollowFixture(t, path, "appended line B\n")
	waitFor(t, func() bool { return strings.Contains(join(batches), "appended line B") })
	cancel()
	<-done

	all := join(batches)
	want := []string{"start line 4", "start line 5", "start line 6", "appended line A", "appended line B"}
	for _, line := range want {
		if !strings.Contains(all, line+"\n") {
			t.Errorf("line %q not reported: %q", line, all)
		}
	}
	for _, line := range []string{"start line 1", "start line 2", "start line 3"} {
		if strings.Contains(all, line+"\n") {
			t.Errorf("a seeded-out line %q was reported: %q", line, all)
		}
	}
	// No line, and no part of a line, is reported twice.
	remaining := all
	for _, line := range want {
		remaining = strings.Replace(remaining, line+"\n", "", 1)
	}
	if strings.Contains(remaining, "start line") || strings.Contains(remaining, "appended line") {
		t.Errorf("lines were reported more than once: %q", all)
	}
}

func writeFollowFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func appendFollowFixture(t *testing.T, path, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func join(batches [][]string) string {
	var builder strings.Builder
	for _, batch := range batches {
		for _, line := range batch {
			builder.WriteString(line)
		}
	}
	return builder.String()
}

// waitFor polls a condition, so a test does not race the goroutine it seeded.
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never became true before the deadline")
}
