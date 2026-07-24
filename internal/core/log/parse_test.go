package log

import "testing"

func TestParseAccessReadsTheCommonAndCombinedFormats(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		client   string
		method   string
		path     string
		protocol string
		status   int
		bytes    int64
	}{
		{
			name:     "common log format",
			line:     `127.0.0.1 - frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
			client:   "127.0.0.1",
			method:   "GET",
			path:     "/apache_pb.gif",
			protocol: "HTTP/1.0",
			status:   200,
			bytes:    2326,
		},
		{
			name:     "combined adds a referer and a user agent",
			line:     `203.0.113.5 - - [24/Jul/2026:09:15:01 +0000] "POST /api/users HTTP/1.1" 201 512 "https://example.com/" "curl/8.4.0"`,
			client:   "203.0.113.5",
			method:   "POST",
			path:     "/api/users",
			protocol: "HTTP/1.1",
			status:   201,
			bytes:    512,
		},
		{
			// A path containing a space is unusual and is what somebody
			// actually requested. Taking the protocol from the end rather than
			// the second space is what keeps the entry.
			name:   "a path containing a space",
			line:   `192.0.2.1 - - [24/Jul/2026:09:15:01 +0000] "GET /docs/getting started.html HTTP/1.1" 200 15360`,
			client: "192.0.2.1",
			method: "GET",
			path:   "/docs/getting started.html",
			status: 200,
			bytes:  15360,
		},
		{
			// A dash where the size should be means the server sent no body.
			// That is a valid entry worth counting, not a parse failure.
			name:   "no response body",
			line:   `192.0.2.1 - - [24/Jul/2026:09:15:01 +0000] "GET /index.html HTTP/1.1" 304 -`,
			client: "192.0.2.1",
			method: "GET",
			path:   "/index.html",
			status: 304,
			bytes:  0,
		},
		{
			name:   "an ipv6 client",
			line:   `2001:db8::1 - - [24/Jul/2026:09:15:01 +0000] "GET / HTTP/2.0" 200 12`,
			client: "2001:db8::1",
			method: "GET",
			path:   "/",
			status: 200,
			bytes:  12,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, ok := parseAccess([]byte(test.line))
			if !ok {
				t.Fatalf("the line was not recognised: %s", test.line)
			}

			if got := string(entry.Client); got != test.client {
				t.Errorf("client = %q, want %q", got, test.client)
			}
			if got := string(entry.Method); got != test.method {
				t.Errorf("method = %q, want %q", got, test.method)
			}
			if got := string(entry.Path); got != test.path {
				t.Errorf("path = %q, want %q", got, test.path)
			}
			if test.protocol != "" {
				if got := string(entry.Protocol); got != test.protocol {
					t.Errorf("protocol = %q, want %q", got, test.protocol)
				}
			}
			if entry.Status != test.status {
				t.Errorf("status = %d, want %d", entry.Status, test.status)
			}
			if entry.Bytes != test.bytes {
				t.Errorf("bytes = %d, want %d", entry.Bytes, test.bytes)
			}
		})
	}
}

// A line that is not an access log entry is reported as such and never as an
// error. Real access logs carry rotation notices and startup banners.
func TestParseAccessRejectsWhatIsNotAnEntry(t *testing.T) {
	lines := []string{
		"",
		"log rotated by logrotate",
		"2026-07-24T09:15:42Z INFO starting up",
		`127.0.0.1 - - [24/Jul/2026:09:15:01 +0000] "GET / HTTP/1.1"`,
		`127.0.0.1 - - 24/Jul/2026 "GET / HTTP/1.1" 200 12`,
		`127.0.0.1 - - [24/Jul/2026:09:15:01 +0000] GET / HTTP/1.1 200 12`,
		`127.0.0.1 - - [24/Jul/2026:09:15:01 +0000] "GET / HTTP/1.1" 999 12`,
		`127.0.0.1 - - [24/Jul/2026:09:15:01 +0000] "GET / HTTP/1.1" OK 12`,
		`127.0.0.1 - - [unterminated "GET / HTTP/1.1" 200 12`,
	}

	for _, line := range lines {
		if _, ok := parseAccess([]byte(line)); ok {
			t.Errorf("this was accepted as an access log entry: %q", line)
		}
	}
}

func TestEndpointStripsTheQueryString(t *testing.T) {
	tests := map[string]string{
		"/search?q=cats":  "/search",
		"/search":         "/search",
		"/a?b=1?c=2":      "/a",
		"?only-a-query":   "",
		"/trailing?":      "/trailing",
		"/path/with#frag": "/path/with#frag",
	}

	for path, want := range tests {
		if got := string(endpoint([]byte(path))); got != want {
			t.Errorf("endpoint(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestStatusClassNamesTheFamily(t *testing.T) {
	tests := map[int]string{
		100: "1xx", 200: "2xx", 204: "2xx", 301: "3xx", 404: "4xx",
		451: "4xx", 500: "5xx", 599: "5xx", 700: "other",
	}

	for code, want := range tests {
		if got := statusClass(code); got != want {
			t.Errorf("statusClass(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestLooksJSONRecognisesAStructuredLine(t *testing.T) {
	if !looksJSON([]byte(`{"level":"info","msg":"ok"}`)) {
		t.Error("a JSON object line was not recognised")
	}
	if looksJSON([]byte(`{"level":"info"`)) {
		t.Error("an unterminated object was treated as JSON")
	}
	if looksJSON([]byte(`plain text`)) {
		t.Error("a plain line was treated as JSON")
	}
}
