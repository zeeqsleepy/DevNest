package log

import (
	"context"
	"io"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// FollowRequest describes one tail.
type FollowRequest struct {
	Path string
	// Count is how many lines already in the file to report before following
	// the file as it grows. The last lines are kept, in order.
	Count int
	// Interval is how often the file is checked for new content. The default
	// is 250ms, which is responsive without waking up on an empty file a
	// hundred times a second.
	Interval time.Duration
}

// defaultFollowInterval is the check cadence when none is given.
const defaultFollowInterval = 250 * time.Millisecond

// Follow reports a log's last lines and then every line appended to it, until
// the context is cancelled.
//
// The tail is efficient: the file is read once to find the last Count lines,
// and every poll after that seeks to where the previous read stopped and reads
// only what was added. A file that is rotated (its size shrinks) is treated as
// a new file and read from its start, which is what a person tailing a log
// whose daemon rotates it expects.
//
// onLines receives batches of complete lines. Each line includes its newline,
// so the caller can write it straight down.
func Follow(ctx context.Context, reader Reader, request FollowRequest, onLines func([]string)) error {
	if request.Interval < 0 {
		return errors.New(errors.CodeInvalidInput, "a follow interval cannot be negative").
			WithHint("pass a duration like --interval 500ms, or omit it")
	}
	if request.Interval == 0 {
		request.Interval = defaultFollowInterval
	}
	if request.Count < 0 {
		return errors.New(errors.CodeInvalidInput, "a tail count cannot be negative").
			WithHint("pass how many existing lines to show, for example 20")
	}

	resolved, err := reader.Resolve(request.Path)
	if err != nil {
		return err
	}
	entry, err := reader.Stat(resolved)
	if err != nil {
		return err
	}
	if entry.IsDir {
		return errors.New(errors.CodeInvalidInput, "%s is a directory", resolved).
			WithHint("pass a single log file; a directory of logs is one command per file")
	}

	// First pass: find the tail of what is already there, in one streaming
	// read that never holds more than the lines it reports.
	handle, err := reader.Open(resolved)
	if err != nil {
		return err
	}
	s := newScanner(handle)
	if err := s.checkText(resolved); err != nil {
		_ = handle.Close()
		return err
	}
	tail := newRing(request.Count)
	var end int64
	for {
		more, nextErr := s.next()
		if nextErr != nil {
			_ = handle.Close()
			return nextErr
		}
		if !more {
			break
		}
		end = s.bytes
		if request.Count > 0 {
			line := make([]byte, len(s.line)+1)
			copy(line, s.line)
			line[len(s.line)] = '\n'
			tail.push(line)
		}
	}
	_ = handle.Close()

	if request.Count > 0 && tail.len() > 0 {
		onLines(tail.slice())
	}

	// Then follow the file as it grows. Every poll re-stats the file, which is
	// how rotation is spotted, and reads only the bytes that have appeared
	// since the last poll.
	timer := time.NewTimer(request.Interval)
	defer timer.Stop()

	for {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, errors.CodeCancelled, "cancelled")
		}

		current, err := reader.Stat(resolved)
		if err != nil {
			return err
		}
		if current.IsDir {
			return errors.New(errors.CodeInvalidInput, "%s is now a directory", resolved)
		}

		if current.Bytes < end {
			// Rotated: a shorter file in the same place. Read the new file
			// from its start.
			end = 0
		}

		if current.Bytes > end {
			end, err = readFrom(ctx, reader, resolved, end, onLines)
			if err != nil {
				return err
			}
		}

		select {
		case <-ctx.Done():
			return errors.Wrap(ctx.Err(), errors.CodeCancelled, "cancelled")
		case <-timer.C:
			timer.Reset(request.Interval)
		}
	}
}

// readFrom appends the lines starting at offset to onLines, returning the new
// end offset.
//
// A reader that can seek (every real file) is positioned at the offset and
// reads only what comes after it. A reader that cannot, some test doubles,
// is read from its start and every line that began before the offset is
// skipped by position, so the caller still receives exactly the new lines.
func readFrom(
	ctx context.Context,
	reader Reader,
	path string,
	offset int64,
	onLines func([]string),
) (int64, error) {
	handle, err := reader.Open(path)
	if err != nil {
		return offset, err
	}
	defer func() { _ = handle.Close() }()

	seeked := false
	if seeker, ok := handle.(io.Seeker); ok {
		if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
			return offset, errors.Wrap(err, errors.CodeIO, "cannot seek into %s", path)
		}
		seeked = true
	}

	s := newScanner(handle)
	batch := make([]string, 0, 64)
	flush := func() {
		if len(batch) > 0 {
			onLines(batch)
			batch = make([]string, 0, 64)
		}
	}

	// The scanner counts bytes from its own start, which is the seek position
	// here, not from the file's start. The caller's notion of "end of what has
	// been read" is absolute, so every early return has to add the offset back.
	consumed := func() int64 {
		if seeked {
			return offset + s.bytes
		}
		return s.bytes
	}

	for {
		if err := ctx.Err(); err != nil {
			return consumed(), errors.Wrap(ctx.Err(), errors.CodeCancelled, "cancelled")
		}

		start := s.bytes
		more, nextErr := s.next()
		if nextErr != nil {
			return consumed(), nextErr
		}
		if !more {
			break
		}

		// Without a seeker the offset can only be reached by skipping lines,
		// and a line is skipped when it began before the offset.
		if !seeked && start < offset {
			continue
		}

		line := make([]byte, len(s.line)+1)
		copy(line, s.line)
		line[len(s.line)] = '\n'
		batch = append(batch, string(line))

		if len(batch) == cap(batch) {
			flush()
		}
	}
	flush()
	return consumed(), nil
}

// ring keeps the last n lines, so a tail of a huge file costs memory for the
// kept tail only.
type ring struct {
	capacity int
	lines    [][]byte
	index    int
}

func newRing(capacity int) *ring {
	return &ring{capacity: capacity}
}

func (r *ring) push(line []byte) {
	if r.capacity <= 0 {
		return
	}
	if len(r.lines) < r.capacity {
		r.lines = append(r.lines, line)
		return
	}
	r.lines[r.index] = line
	r.index = (r.index + 1) % r.capacity
}

func (r *ring) len() int { return len(r.lines) }

// slice returns the kept lines in the order they appeared in the file.
func (r *ring) slice() []string {
	out := make([]string, 0, len(r.lines))
	for i := 0; i < len(r.lines); i++ {
		out = append(out, string(r.lines[(r.index+i)%len(r.lines)]))
	}
	return out
}
