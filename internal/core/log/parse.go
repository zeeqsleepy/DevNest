package log

import (
	"bytes"
	"strconv"
)

// access is one parsed line of an HTTP access log.
//
// The fields are slices into the scanner's buffer, so they last exactly as
// long as the line does. Anything that outlives the line is copied by the
// caller, at the point where it decides to keep it.
type access struct {
	Client   []byte
	Method   []byte
	Path     []byte
	Protocol []byte
	Status   int
	Bytes    int64
}

// parseAccess reads a line in the Common or Combined Log Format.
//
//	127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /a HTTP/1.0" 200 2326
//
// The combined format adds a quoted referer and user agent, which are simply
// left unread: nothing in this module reports on them, and parsing fields
// nobody asks for is work done on every line of a very large file.
//
// It is hand-written rather than a regular expression. A regexp over ten
// million lines is the difference between a command that answers while you
// wait and one you go and make coffee for, and this format is a handful of
// space-separated fields.
//
// A line that does not fit the format returns false. That is an ordinary
// outcome, not an error: real log files carry startup banners, rotation
// markers, and entries from whatever else was writing to the same file.
func parseAccess(line []byte) (access, bool) {
	var entry access

	client, rest, ok := field(line)
	if !ok {
		return entry, false
	}
	entry.Client = client

	// The identity and user fields are almost always "-". They are skipped
	// rather than validated, because a log that fills them in is still a log
	// this should read.
	for range 2 {
		if _, rest, ok = field(rest); !ok {
			return entry, false
		}
	}

	if _, rest, ok = delimited(rest, '[', ']'); !ok {
		return entry, false
	}

	request, rest, ok := delimited(rest, '"', '"')
	if !ok {
		return entry, false
	}
	entry.Method, entry.Path, entry.Protocol = splitRequest(request)
	if len(entry.Method) == 0 {
		return entry, false
	}

	status, rest, ok := field(rest)
	if !ok {
		return entry, false
	}
	code, err := strconv.Atoi(string(status))
	if err != nil || code < 100 || code > 599 {
		return entry, false
	}
	entry.Status = code

	// The size may be "-" when the server sent no body, which is a valid
	// entry and counts as zero bytes rather than as a parse failure.
	size, _, ok := field(rest)
	if ok && !bytes.Equal(size, []byte{'-'}) {
		if sent, err := strconv.ParseInt(string(size), 10, 64); err == nil {
			entry.Bytes = sent
		}
	}

	return entry, true
}

// field takes the next space-separated token and returns what follows it.
func field(data []byte) (token, rest []byte, ok bool) {
	data = bytes.TrimLeft(data, " \t")
	if len(data) == 0 {
		return nil, nil, false
	}
	if index := bytes.IndexAny(data, " \t"); index >= 0 {
		return data[:index], data[index+1:], true
	}
	return data, nil, true
}

// delimited takes the next token enclosed by a pair of characters.
func delimited(data []byte, open, closing byte) (token, rest []byte, ok bool) {
	data = bytes.TrimLeft(data, " \t")
	if len(data) == 0 || data[0] != open {
		return nil, nil, false
	}
	end := bytes.IndexByte(data[1:], closing)
	if end < 0 {
		return nil, nil, false
	}
	return data[1 : 1+end], data[end+2:], true
}

// splitRequest breaks `GET /path HTTP/1.1` into its three parts.
//
// The protocol is taken from the end rather than the second space, because a
// path can contain a space: a request line for `/a b.html` is unusual but it
// is what some client actually sent, and dropping the entry would understate
// the request count.
func splitRequest(request []byte) (method, path, protocol []byte) {
	request = bytes.TrimSpace(request)
	if len(request) == 0 {
		return nil, nil, nil
	}

	space := bytes.IndexByte(request, ' ')
	if space < 0 {
		return request, nil, nil
	}
	method = request[:space]
	rest := bytes.TrimSpace(request[space+1:])

	if last := bytes.LastIndexByte(rest, ' '); last >= 0 &&
		bytes.HasPrefix(rest[last+1:], []byte("HTTP/")) {
		return method, rest[:last], rest[last+1:]
	}
	return method, rest, nil
}

// endpoint strips the query string from a path.
//
// Two requests to /search that differ only in what was searched for are the
// same endpoint, and a top-paths listing that reports them separately answers
// a question nobody asked. The full path is still in the file for anyone who
// wants it.
func endpoint(path []byte) []byte {
	if index := bytes.IndexByte(path, '?'); index >= 0 {
		return path[:index]
	}
	return path
}

// statusClass names the family a status code belongs to.
func statusClass(code int) string {
	switch {
	case code >= 100 && code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	case code < 600:
		return "5xx"
	default:
		return "other"
	}
}

// looksJSON reports whether a line is a JSON object, which is how structured
// loggers write.
func looksJSON(line []byte) bool {
	trimmed := bytes.TrimLeft(line, " \t")
	return len(trimmed) > 1 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}
