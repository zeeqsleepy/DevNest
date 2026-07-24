package log

import (
	"context"
	"sort"
	"time"
)

// StatsRequest describes one line-length analysis.
type StatsRequest struct {
	Path string
	// Top caps the list of longest lines. Zero means the default.
	Top int
}

// LineLength is one line and how long it is.
type LineLength struct {
	Line  int    `json:"line"`
	Bytes int    `json:"bytes"`
	Text  string `json:"text"`
}

// StatsResult describes the shape of a log file's lines.
type StatsResult struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Lines int    `json:"lines"`
	Blank int    `json:"blankLines"`

	AverageLineBytes float64 `json:"averageLineBytes"`
	LongestLineBytes int     `json:"longestLineBytes"`

	// ShortestLineBytes ignores blank lines. The shortest line in almost every
	// log file is empty, and reporting zero answers nothing.
	ShortestLineBytes int `json:"shortestLineBytes"`
	ShortestLine      int `json:"shortestLineNumber"`

	LongestLines []LineLength `json:"longestLines"`

	LongLines  int   `json:"longLines"`
	DurationMs int64 `json:"durationMs"`
}

// excerptLength is how much of a long line the listing shows. The point of the
// listing is which lines are long and where they are, not what is in them.
const excerptLength = 120

// Stats measures the lines of a log file.
//
// This is the command for the question "why is this file eight gigabytes".
// The answer is usually a handful of lines with a serialised payload in them,
// and their line numbers are what makes them findable.
func Stats(ctx context.Context, reader Reader, request StatsRequest) (StatsResult, error) {
	started := time.Now()

	from, err := open(reader, request.Path)
	if err != nil {
		return StatsResult{}, err
	}
	defer from.close()

	top := request.Top
	if top < 1 {
		top = defaultTop
	}

	longest := newLongestLines(top)
	result := StatsResult{Path: from.path, Bytes: from.bytes}
	var content int64

	scanned, err := scan(ctx, from, func(s *scanner) error {
		if s.length == 0 {
			result.Blank++
			return nil
		}

		content += int64(s.length)
		longest.offer(s)

		if s.length > result.LongestLineBytes {
			result.LongestLineBytes = s.length
		}
		if result.ShortestLineBytes == 0 || s.length < result.ShortestLineBytes {
			result.ShortestLineBytes = s.length
			result.ShortestLine = s.number
		}
		return nil
	})
	if err != nil {
		return StatsResult{}, err
	}

	result.Lines = scanned.number
	result.LongLines = scanned.long
	result.LongestLines = longest.sorted()

	// The average is over lines that hold something. Blank lines are counted
	// and reported separately, and folding them into the average makes the
	// number smaller without making it more informative.
	if filled := result.Lines - result.Blank; filled > 0 {
		result.AverageLineBytes = round1(float64(content) / float64(filled))
	}

	result.DurationMs = millis(started)
	return result, nil
}

// longestLines keeps only the longest few lines rather than sorting every line
// at the end.
//
// On a file with ten million lines that is the difference between a few
// kilobytes and a few hundred megabytes, which is the whole point of the
// module.
type longestLines struct {
	limit int
	lines []LineLength
}

func newLongestLines(limit int) *longestLines {
	return &longestLines{limit: limit, lines: make([]LineLength, 0, limit+1)}
}

func (l *longestLines) offer(s *scanner) {
	if len(l.lines) == l.limit && s.length <= l.lines[len(l.lines)-1].Bytes {
		return
	}

	position := sort.Search(len(l.lines), func(i int) bool {
		return l.lines[i].Bytes < s.length
	})

	l.lines = append(l.lines, LineLength{})
	copy(l.lines[position+1:], l.lines[position:])
	l.lines[position] = LineLength{
		Line:  s.number,
		Bytes: s.length,
		Text:  truncate(s.line, excerptLength),
	}

	if len(l.lines) > l.limit {
		l.lines = l.lines[:l.limit]
	}
}

func (l *longestLines) sorted() []LineLength {
	lines := append([]LineLength(nil), l.lines...)
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].Bytes != lines[j].Bytes {
			return lines[i].Bytes > lines[j].Bytes
		}
		return lines[i].Line < lines[j].Line
	})
	if lines == nil {
		return []LineLength{}
	}
	return lines
}
