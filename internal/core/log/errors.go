package log

import (
	"context"
	"sort"
	"time"
	"unicode/utf8"
)

// ErrorsRequest describes one error summary.
type ErrorsRequest struct {
	Path string
	// Top caps the list of most common messages. Zero means the default.
	Top int
	// IncludeWarnings counts warning-level lines as findings too. Off by
	// default: a summary that reports every deprecation notice buries the
	// three lines that matter.
	IncludeWarnings bool
}

// ErrorMessage is one recurring failure.
type ErrorMessage struct {
	// Message is the first line that produced this grouping, kept verbatim.
	// The grouping key is a normalised form of it, so a message that differs
	// only by an identifier still counts as the same failure.
	Message   string  `json:"message"`
	Severity  string  `json:"severity"`
	Category  string  `json:"category"`
	Count     int     `json:"count"`
	Percent   float64 `json:"percent"`
	FirstLine int     `json:"firstLine"`
	LastLine  int     `json:"lastLine"`
}

// ErrorsResult is what an error scan found.
type ErrorsResult struct {
	Path     string `json:"path"`
	Lines    int    `json:"lines"`
	Errors   int    `json:"errors"`
	Warnings int    `json:"warnings"`

	Severities []Count        `json:"severities"`
	Categories []Count        `json:"categories"`
	Messages   []ErrorMessage `json:"messages"`

	DurationMs int64 `json:"durationMs"`
}

// messageLength caps how much of a sample message is kept. Long enough to be
// recognisable, short enough that a stack trace on one line does not become
// the whole report.
const messageLength = 200

// SummarizeErrors finds the failures in a log.
//
// Two kinds of line count as a finding, and both go through the same grouping:
// a line announcing a severity (an application log) and a request that came
// back 5xx (an access log). Mixing the two is deliberate, because a real
// incident is investigated across both and nobody wants to remember which
// command reads which file.
func SummarizeErrors(ctx context.Context, reader Reader, request ErrorsRequest) (ErrorsResult, error) {
	started := time.Now()

	from, err := open(reader, request.Path)
	if err != nil {
		return ErrorsResult{}, err
	}
	defer from.close()

	collector := newErrorCollector(request.IncludeWarnings)

	scanned, err := scan(ctx, from, func(s *scanner) error {
		collector.offer(s.line, s.number)
		return nil
	})
	if err != nil {
		return ErrorsResult{}, err
	}

	top := request.Top
	if top < 1 {
		top = defaultTop
	}

	return ErrorsResult{
		Path:       from.path,
		Lines:      scanned.number,
		Errors:     collector.findings,
		Warnings:   collector.warnings,
		Severities: collector.severities.top(0, collector.findings),
		Categories: collector.categories.top(0, collector.findings),
		Messages:   collector.ranked(top),
		DurationMs: millis(started),
	}, nil
}

// errorCollector groups findings as the scan runs.
//
// It holds one entry per distinct failure rather than one per line, so a file
// with two million identical timeouts costs one map entry.
type errorCollector struct {
	includeWarnings bool

	findings int
	warnings int

	severities *counter
	categories *counter

	groups map[string]*ErrorMessage
	order  []string

	lowerBuffer     []byte
	signatureBuffer []byte
}

func newErrorCollector(includeWarnings bool) *errorCollector {
	return &errorCollector{
		includeWarnings: includeWarnings,
		severities:      newCounter(),
		categories:      newCounter(),
		groups:          make(map[string]*ErrorMessage),
	}
}

// offer examines one line and records it if it is a finding.
func (e *errorCollector) offer(line []byte, number int) {
	if len(line) == 0 {
		return
	}

	severity, category, ok := e.judge(line)
	if !ok {
		return
	}
	if severity == SeverityWarning {
		e.warnings++
		if !e.includeWarnings {
			return
		}
	}

	e.findings++
	e.severities.addText(severity)
	e.categories.addText(category)
	e.record(line, number, severity, category)
}

// judge decides whether a line is a finding, and what kind.
func (e *errorCollector) judge(line []byte) (severity, category string, ok bool) {
	if entry, parsed := parseAccess(line); parsed {
		if entry.Status < 500 {
			return "", "", false
		}
		return SeverityError, CategoryServerError, true
	}

	severity, found := detectSeverity(line)
	if !found {
		return "", "", false
	}
	category, e.lowerBuffer = classify(line, e.lowerBuffer)
	return severity, category, true
}

// record adds the line to its group, creating the group on first sight.
func (e *errorCollector) record(line []byte, number int, severity, category string) {
	e.signatureBuffer = signature(line, e.signatureBuffer)
	key := string(e.signatureBuffer)

	if group, seen := e.groups[key]; seen {
		group.Count++
		group.LastLine = number
		return
	}
	if len(e.groups) >= maxKeys {
		return
	}

	e.groups[key] = &ErrorMessage{
		Message:   truncate(line, messageLength),
		Severity:  severity,
		Category:  category,
		Count:     1,
		FirstLine: number,
		LastLine:  number,
	}
	e.order = append(e.order, key)
}

// ranked returns the most common messages, most frequent first.
//
// Ties break on the line the message was first seen at, so the order is stable
// across runs and reads in file order when counts are equal.
func (e *errorCollector) ranked(limit int) []ErrorMessage {
	messages := make([]ErrorMessage, 0, len(e.groups))
	for _, key := range e.order {
		group := e.groups[key]
		group.Percent = percent(group.Count, e.findings)
		messages = append(messages, *group)
	}

	sort.SliceStable(messages, func(i, j int) bool {
		if messages[i].Count != messages[j].Count {
			return messages[i].Count > messages[j].Count
		}
		return messages[i].FirstLine < messages[j].FirstLine
	})

	if limit > 0 && len(messages) > limit {
		messages = messages[:limit]
	}
	if messages == nil {
		return []ErrorMessage{}
	}
	return messages
}

// truncate copies at most limit bytes of a line, marking a cut with an
// ellipsis so nobody reads a shortened message as the whole of one.
//
// The cut backs off to a rune boundary. Half a multi-byte character renders as
// a replacement glyph and looks like corruption in the log rather than in the
// report.
func truncate(line []byte, limit int) string {
	if len(line) <= limit {
		return string(line)
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return string(line[:cut]) + "..."
}
