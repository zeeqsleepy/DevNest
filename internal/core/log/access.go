package log

import (
	"context"
	"strconv"
)

// accessTotals is everything one pass over an access log collects.
//
// The three HTTP commands are three projections of this. Collecting a status
// code costs the same whether or not this run is going to report it, and one
// collection is how "requests" means the same number in all three.
type accessTotals struct {
	lines     int
	bytes     int64
	requests  int
	unparsed  int
	truncated int

	methods  *counter
	codes    *counter
	classes  *counter
	paths    *counter
	clients  *counter
	sent     int64
	withSize int
}

func newAccessTotals() *accessTotals {
	return &accessTotals{
		methods: newCounter(),
		codes:   newCounter(),
		classes: newCounter(),
		paths:   newCounter(),
		clients: newCounter(),
	}
}

// collectAccess reads the file once and fills every counter.
func collectAccess(ctx context.Context, from *source) (*accessTotals, error) {
	totals := newAccessTotals()

	// A small stack buffer for turning a status code back into text. Doing it
	// with strconv.Itoa would allocate on every line of the file.
	var code [3]byte

	reader, err := scan(ctx, from, func(s *scanner) error {
		if len(s.line) == 0 {
			return nil
		}

		entry, ok := parseAccess(s.line)
		if !ok {
			totals.unparsed++
			return nil
		}

		totals.requests++
		totals.methods.add(entry.Method)
		totals.clients.add(entry.Client)
		totals.paths.add(endpoint(entry.Path))
		totals.classes.addText(statusClass(entry.Status))
		totals.codes.add(strconv.AppendInt(code[:0], int64(entry.Status), 10))

		totals.sent += entry.Bytes
		if entry.Bytes > 0 {
			totals.withSize++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	totals.lines = reader.number
	totals.bytes = reader.bytes
	totals.truncated = reader.long
	return totals, nil
}

// average response size, over the responses that actually carried a body.
//
// Dividing by every request instead would let a run of 304s drag the figure
// towards zero and make it look like the server started sending less, which is
// the opposite of what happened.
func (a *accessTotals) averageSent() int64 {
	if a.withSize == 0 {
		return 0
	}
	return a.sent / int64(a.withSize)
}
