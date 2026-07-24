package log

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// SearchRequest describes one keyword search.
type SearchRequest struct {
	Path  string
	Query string
	// IgnoreCase folds both sides before comparing.
	IgnoreCase bool
	// Limit caps how many matching lines are kept. Zero means the default.
	// The whole file is still read, so the reported total is the real one.
	Limit int
}

// Match is one line that contained the keyword.
type Match struct {
	Line      int    `json:"line"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// SearchResult is what a search found.
type SearchResult struct {
	Path       string `json:"path"`
	Query      string `json:"query"`
	IgnoreCase bool   `json:"ignoreCase"`

	Lines   int `json:"lines"`
	Matches int `json:"matches"`

	// Limited says the listing was cut at the limit while the count above
	// covers the whole file. Reporting "50 matches" for a file with nine
	// thousand would be a lie told by an implementation detail.
	Limited bool `json:"limited"`

	Results    []Match `json:"results"`
	DurationMs int64   `json:"durationMs"`
}

// defaultMatches is how many matching lines are kept when the caller does not
// say. A hundred is a screen or two; a search that wants more is a search that
// wants a pipe.
const defaultMatches = 100

// matchLength caps the text kept for one match.
const matchLength = 500

// Search finds the lines of a log containing a keyword.
//
// Plain substring matching, not a regular expression. A log search is nearly
// always for a request identifier, an address, or a user name, and the one
// time it is not, the shell already has grep. Refusing to grow a second
// pattern language keeps the search fast enough to be the obvious thing to
// reach for.
func Search(ctx context.Context, reader Reader, request SearchRequest) (SearchResult, error) {
	started := time.Now()

	if strings.TrimSpace(request.Query) == "" {
		return SearchResult{}, errors.New(errors.CodeInvalidInput, "no search term was given").
			WithHint("pass the text to look for, for example: devnest log search app.log timeout")
	}

	from, err := open(reader, request.Path)
	if err != nil {
		return SearchResult{}, err
	}
	defer from.close()

	limit := request.Limit
	if limit < 1 {
		limit = defaultMatches
	}

	needle := []byte(request.Query)
	if request.IgnoreCase {
		needle = bytes.ToLower(needle)
	}

	result := SearchResult{
		Path:       from.path,
		Query:      request.Query,
		IgnoreCase: request.IgnoreCase,
		Results:    make([]Match, 0, min(limit, 64)),
	}

	var folded []byte

	scanned, err := scan(ctx, from, func(s *scanner) error {
		haystack := s.line
		if request.IgnoreCase {
			folded = lower(s.line, folded)
			haystack = folded
		}
		if !bytes.Contains(haystack, needle) {
			return nil
		}

		result.Matches++
		if len(result.Results) >= limit {
			result.Limited = true
			return nil
		}

		result.Results = append(result.Results, Match{
			Line:      s.number,
			Text:      truncate(s.line, matchLength),
			Truncated: s.truncated || s.length > matchLength,
		})
		return nil
	})
	if err != nil {
		return SearchResult{}, err
	}

	result.Lines = scanned.number
	result.DurationMs = millis(started)
	return result, nil
}
