package log

import (
	"context"
	"strings"
	"testing"
)

// collect reads a string through the scanner and returns the lines it saw.
func collect(t *testing.T, content string) (*scanner, []string) {
	t.Helper()

	reader := newFakeReader().with("log", content)
	from, err := open(reader, "log")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer from.close()

	var lines []string
	scanned, err := scan(context.Background(), from, func(s *scanner) error {
		lines = append(lines, string(s.line))
		return nil
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return scanned, lines
}

func TestScanSplitsLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"unix endings", "one\ntwo\nthree\n", []string{"one", "two", "three"}},
		{"windows endings", "one\r\ntwo\r\n", []string{"one", "two"}},
		{"no trailing newline", "one\ntwo", []string{"one", "two"}},
		{"blank lines are lines", "one\n\ntwo\n", []string{"one", "", "two"}},
		{"a single line", "only", []string{"only"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scanned, lines := collect(t, test.content)

			if len(lines) != len(test.want) {
				t.Fatalf("lines = %q, want %q", lines, test.want)
			}
			for index, want := range test.want {
				if lines[index] != want {
					t.Errorf("line %d = %q, want %q", index+1, lines[index], want)
				}
			}
			if scanned.number != len(test.want) {
				t.Errorf("line count = %d, want %d", scanned.number, len(test.want))
			}
			if scanned.bytes != int64(len(test.content)) {
				t.Errorf("bytes = %d, want %d", scanned.bytes, len(test.content))
			}
		})
	}
}

// An empty file is not an error. It has no lines, which is the answer.
func TestScanAcceptsAnEmptyFile(t *testing.T) {
	scanned, lines := collect(t, "")

	if len(lines) != 0 {
		t.Errorf("lines = %q, want none", lines)
	}
	if scanned.number != 0 || scanned.bytes != 0 {
		t.Errorf("counted %d lines and %d bytes, want zero of each", scanned.number, scanned.bytes)
	}
}

// A line longer than one read still arrives whole, because a log line that
// spans the buffer is ordinary rather than exceptional.
func TestScanAssemblesALineLongerThanTheBuffer(t *testing.T) {
	long := strings.Repeat("x", readBuffer*2+17)
	scanned, lines := collect(t, "short\n"+long+"\nafter\n")

	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if lines[1] != long {
		t.Errorf("the long line came back with %d bytes, want %d", len(lines[1]), len(long))
	}
	if scanned.long != 0 {
		t.Errorf("long lines = %d, want 0: this line is under the cap", scanned.long)
	}
}

// Past the cap the content is cut, the line is still counted, and its real
// length is still reported. Memory use must not follow the input.
func TestScanTruncatesAnAbsurdlyLongLine(t *testing.T) {
	absurd := strings.Repeat("y", maxLine+5000)

	reader := newFakeReader().with("log", "first\n"+absurd+"\nlast\n")
	from, err := open(reader, "log")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer from.close()

	var lengths []int
	var kept []int
	scanned, err := scan(context.Background(), from, func(s *scanner) error {
		lengths = append(lengths, s.length)
		kept = append(kept, len(s.line))
		return nil
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if scanned.number != 3 {
		t.Fatalf("lines = %d, want 3", scanned.number)
	}
	if lengths[1] != len(absurd) {
		t.Errorf("reported length = %d, want the real %d", lengths[1], len(absurd))
	}
	if kept[1] != maxLine {
		t.Errorf("kept %d bytes, want the cap of %d", kept[1], maxLine)
	}
	if scanned.long != 1 {
		t.Errorf("over-long lines = %d, want 1", scanned.long)
	}
	if lengths[2] != len("last") {
		t.Errorf("the line after a truncated one = %d bytes, want %d", lengths[2], len("last"))
	}
}

// A binary file has no lines to report on, and saying so is more useful than
// a summary of one enormous "line".
func TestScanRefusesBinaryContent(t *testing.T) {
	reader := newFakeReader().with("log", "text\x00\x01\x02more")
	from, err := open(reader, "log")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer from.close()

	_, err = scan(context.Background(), from, func(*scanner) error { return nil })
	assertCode(t, err, "INVALID_INPUT")
}

// A long scan has to be interruptible: a cancelled context stops the read
// rather than finishing a four gigabyte file first.
func TestScanStopsWhenTheContextIsCancelled(t *testing.T) {
	reader := newFakeReader().with("log", strings.Repeat("line\n", cancelEvery*3))
	from, err := open(reader, "log")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer from.close()

	ctx, cancel := context.WithCancel(context.Background())
	seen := 0
	_, err = scan(ctx, from, func(*scanner) error {
		seen++
		if seen == cancelEvery {
			cancel()
		}
		return nil
	})

	assertCode(t, err, "CANCELLED")
	if seen > cancelEvery*2 {
		t.Errorf("read %d lines after cancellation, want it to stop within one check interval", seen)
	}
}

func TestOpenRejectsWhatCannotBeRead(t *testing.T) {
	reader := newFakeReader().with("log", "one\n").withDir("logs")

	if _, err := open(reader, ""); err == nil {
		t.Error("an empty path was accepted")
	} else {
		assertCode(t, err, "INVALID_INPUT")
	}

	if _, err := open(reader, "logs"); err == nil {
		t.Error("a directory was accepted")
	} else {
		assertCode(t, err, "INVALID_INPUT")
	}

	if _, err := open(reader, "missing.log"); err == nil {
		t.Error("a missing file was accepted")
	} else {
		assertCode(t, err, "NOT_FOUND")
	}
}
