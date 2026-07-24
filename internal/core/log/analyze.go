package log

import (
	"context"
	"time"
)

// AnalyzeRequest describes one file overview.
type AnalyzeRequest struct {
	Path string
}

// AnalyzeResult is the overview of a log file.
type AnalyzeResult struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Lines int    `json:"lines"`
	Blank int    `json:"blankLines"`

	// Type is the detected shape of the file, and Sampled is how many lines
	// that decision was made from. Reporting the sample size lets a reader
	// judge the guess rather than take it on faith.
	Type    string `json:"type"`
	Sampled int    `json:"sampledLines"`

	// LongLines counts lines past the length cap. They are counted and
	// measured; only their content is cut short.
	LongLines  int   `json:"longLines"`
	DurationMs int64 `json:"durationMs"`
}

// sampleLines is how many lines the type detection looks at. A log is written
// by the same program throughout; if the first two hundred lines do not say
// what it is, two hundred thousand will not either.
const sampleLines = 200

// Analyze reports what a log file is and how big it is.
//
// The whole file is read, because the line count has to be exact, but the type
// detection only looks at the head of it. One pass either way.
func Analyze(ctx context.Context, reader Reader, request AnalyzeRequest) (AnalyzeResult, error) {
	started := time.Now()

	from, err := open(reader, request.Path)
	if err != nil {
		return AnalyzeResult{}, err
	}
	defer from.close()

	sample := &typeSample{}
	blank := 0

	scanned, err := scan(ctx, from, func(s *scanner) error {
		if len(s.line) == 0 {
			blank++
			return nil
		}
		if sample.considered < sampleLines {
			sample.offer(s.line)
		}
		return nil
	})
	if err != nil {
		return AnalyzeResult{}, err
	}

	return AnalyzeResult{
		Path:       from.path,
		Bytes:      from.bytes,
		Lines:      scanned.number,
		Blank:      blank,
		Type:       sample.verdict(),
		Sampled:    sample.considered,
		LongLines:  scanned.long,
		DurationMs: millis(started),
	}, nil
}

// typeSample decides what kind of log a file is from its first lines.
type typeSample struct {
	considered int
	access     int
	structured int
	levelled   int
}

func (t *typeSample) offer(line []byte) {
	t.considered++
	switch {
	case looksJSON(line):
		t.structured++
	default:
		if _, ok := parseAccess(line); ok {
			t.access++
		}
		if _, ok := detectSeverity(line); ok {
			t.levelled++
		}
	}
}

// verdict names the file type.
//
// The thresholds differ on purpose. An access log is a strict format, so most
// lines have to parse before it is called one. A severity level is optional in
// every application log ever written, so a third of the lines carrying one is
// plenty of evidence.
func (t *typeSample) verdict() string {
	if t.considered == 0 {
		return TypeText
	}

	share := func(count int) float64 { return float64(count) / float64(t.considered) }

	switch {
	case share(t.access) >= 0.6:
		return TypeAccess
	case share(t.structured) >= 0.6:
		return TypeJSONLines
	case share(t.levelled) >= 0.3:
		return TypeApplication
	default:
		return TypeText
	}
}
