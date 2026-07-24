// Package log is DevNest's log analysis module: reading a text log file and
// reporting what is in it.
//
// # One pass, fixed memory
//
// Every operation here reads the file exactly once, through a buffer it reuses
// for every line. Nothing loads a file into memory, and nothing keeps a line
// after it has been counted. A four gigabyte access log costs the same
// resident memory as a four kilobyte one, and the only thing that grows with
// the input is the number of distinct keys being counted, which is capped and
// reported. This is the property the whole module is designed around: the logs
// worth analysing are the ones too large to open in an editor.
//
// A consequence worth stating: the byte slice handed to a line visitor points
// into the scanner's buffer and is valid only until the next line. Anything
// kept has to be copied, and every place that keeps something does so
// deliberately.
//
// # Malformed input is normal
//
// Log files are truncated mid-write, contain lines from three different
// programs, and carry entries no format documents. None of that is an error
// here. A line that does not parse is counted as unparsed and the run
// continues, because a summary of the ninety-eight percent that did parse is
// exactly what the user came for. Only a failure to read the file at all comes
// back as an error.
//
// # Parsing lives in one place
//
// The HTTP commands (http, status, top) share a single access-log parser and a
// single pass that fills the same counters; each is a different projection of
// that one collection. Adding a second parser for status codes is how two
// commands end up disagreeing about how many requests a file holds.
package log

import (
	"io"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// File types recognised by Analyze and reported by every command.
const (
	TypeAccess      = "http-access"
	TypeApplication = "application"
	TypeJSONLines   = "json-lines"
	TypeText        = "text"
)

// defaultTop is how many entries a ranked listing reports when the caller does
// not say. Ten fits on a screen and answers the question; a hundred is a file
// the user should have piped into something else.
const defaultTop = 10

// Count is one key and how often it occurred, with its share of the whole.
//
// Every ranked listing in this module uses it, so a consumer writes one piece
// of handling code for methods, status codes, paths, clients, and categories.
type Count struct {
	Value   string  `json:"value"`
	Count   int     `json:"count"`
	Percent float64 `json:"percent"`
}

// source is the file an operation reads, opened and ready.
type source struct {
	path   string
	bytes  int64
	reader io.ReadCloser
}

// open resolves, checks, and opens the file named in a request.
//
// The checks are here rather than in each operation because all seven need the
// same ones, and an error that names the path is worth more than a wrapped
// syscall failure from somewhere further down.
func open(reader Reader, path string) (*source, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New(errors.CodeInvalidInput, "no log file was given").
			WithHint("pass the path of the log file to read")
	}

	resolved, err := reader.Resolve(path)
	if err != nil {
		return nil, err
	}

	entry, err := reader.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if entry.IsDir {
		return nil, errors.New(errors.CodeInvalidInput, "%s is a directory", resolved).
			WithHint("pass a single log file; a directory of logs is one command per file")
	}

	handle, err := reader.Open(resolved)
	if err != nil {
		return nil, err
	}

	return &source{path: resolved, bytes: entry.Bytes, reader: handle}, nil
}

// close releases the file. The handle is read-only, so a failed close cannot
// lose anything and there is nothing useful to report.
func (s *source) close() {
	_ = s.reader.Close()
}

// millis reports elapsed time the way every result carries it.
func millis(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}

// round1 keeps percentages to one decimal place. Two runs over one file must
// produce byte-identical output, and a full float64 in a report is noise.
func round1(value float64) float64 {
	return float64(int64(value*10+0.5)) / 10
}

// percent is the share of total that value represents.
func percent(value, total int) float64 {
	if total <= 0 {
		return 0
	}
	return round1(float64(value) * 100 / float64(total))
}

// ensure guards against a nil slice reaching the JSON encoder, where it would
// render as null and force every consumer to check before iterating.
func ensure(counts []Count) []Count {
	if counts == nil {
		return []Count{}
	}
	return counts
}
