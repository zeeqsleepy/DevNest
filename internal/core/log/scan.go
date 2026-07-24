package log

import (
	"bufio"
	"bytes"
	"context"
	"io"

	"github.com/devnest/devnest/internal/errors"
)

const (
	// readBuffer is the read size. Large enough that syscall overhead
	// disappears on a multi-gigabyte file, small enough that it is not worth
	// thinking about.
	readBuffer = 256 * 1024

	// maxLine caps how much of one line is kept. A log line longer than this
	// is a stack trace with the newlines stripped or a program writing a blob
	// into a log, and holding all of it would make memory use depend on the
	// input after all. The line is still counted, its true length is still
	// reported, and it is marked as truncated.
	maxLine = 1 << 20

	// cancelEvery is how often cancellation is checked. Checking per line is
	// measurable on a file with ten million of them; checking every few
	// thousand still returns within a few milliseconds of a Ctrl+C.
	cancelEvery = 4096

	// textProbe is how much of the head of the file is examined before
	// deciding it is text at all.
	textProbe = 8 * 1024
)

// scanner reads a file line by line through a buffer it reuses.
//
// The current line points into that buffer and is valid only until the next
// call to next. Nothing here allocates per line, which is the whole reason
// this type exists rather than a bufio.Scanner: Scanner gives up on a line
// longer than its buffer, and giving up on a file because one line is odd is
// not acceptable behaviour for a log tool.
type scanner struct {
	reader   *bufio.Reader
	overflow []byte

	line      []byte
	number    int
	length    int
	truncated bool

	bytes int64
	long  int
	raw   int
}

func newScanner(reader io.Reader) *scanner {
	return &scanner{reader: bufio.NewReaderSize(reader, readBuffer)}
}

// checkText refuses a file that is not text.
//
// Every command here reports on lines, and a binary file has none: it has one
// enormous "line" and a summary that means nothing. A NUL byte near the start
// is the same test every other tool uses, and it is cheap because the bytes
// are already in the buffer the scan is about to read from.
func (s *scanner) checkText(path string) error {
	head, err := s.reader.Peek(textProbe)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return errors.Wrap(err, errors.CodeIO, "cannot read %s", path)
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return errors.New(errors.CodeInvalidInput, "%s is not a text file", path).
			WithHint("the log commands read text logs; this file contains binary data")
	}
	return nil
}

// next advances to the following line, reporting false at end of file.
func (s *scanner) next() (bool, error) {
	s.overflow = s.overflow[:0]
	s.line = nil
	s.length = 0
	s.truncated = false
	s.raw = 0

	for {
		fragment, err := s.reader.ReadSlice('\n')
		s.bytes += int64(len(fragment))
		s.raw += len(fragment)

		switch {
		case err == nil:
			s.finish(fragment)
			return true, nil

		case errors.Is(err, bufio.ErrBufferFull):
			s.keep(fragment)

		case errors.Is(err, io.EOF):
			if len(fragment) == 0 && len(s.overflow) == 0 {
				return false, nil
			}
			s.finish(fragment)
			return true, nil

		default:
			return false, errors.Wrap(err, errors.CodeIO, "cannot read the log file")
		}
	}
}

// finish completes the current line from the last fragment read.
func (s *scanner) finish(fragment []byte) {
	s.number++

	// The common case by far: the whole line came out of one read, so it is
	// used where it lies and nothing is copied.
	if len(s.overflow) == 0 {
		s.line = trimEOL(fragment)
		s.length = len(s.line)
		return
	}

	s.keep(fragment)
	s.line = s.overflow
	if s.truncated {
		s.long++
		s.length = s.raw - (len(fragment) - len(trimEOL(fragment)))
		return
	}
	s.line = trimEOL(s.overflow)
	s.length = len(s.line)
}

// keep appends a fragment of a line that spanned more than one read, up to the
// cap. Past the cap the bytes are dropped and the line is marked; the count of
// how many bytes there were is kept from the raw total.
func (s *scanner) keep(fragment []byte) {
	room := maxLine - len(s.overflow)
	if room <= 0 {
		s.truncated = true
		return
	}
	if len(fragment) > room {
		fragment = fragment[:room]
		s.truncated = true
	}
	s.overflow = append(s.overflow, fragment...)
}

// trimEOL removes a line terminator, either flavour.
func trimEOL(data []byte) []byte {
	data = bytes.TrimSuffix(data, []byte{'\n'})
	return bytes.TrimSuffix(data, []byte{'\r'})
}

// scan reads every line of a file, calling visit for each.
//
// It owns the parts every operation repeats: the text check, cancellation, and
// the counting of lines and bytes. An operation supplies only what it does
// with a line.
func scan(ctx context.Context, from *source, visit func(*scanner) error) (*scanner, error) {
	reader := newScanner(from.reader)
	if err := reader.checkText(from.path); err != nil {
		return nil, err
	}

	for {
		if reader.number%cancelEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		more, err := reader.next()
		if err != nil {
			return nil, err
		}
		if !more {
			return reader, nil
		}
		if err := visit(reader); err != nil {
			return nil, err
		}
	}
}
