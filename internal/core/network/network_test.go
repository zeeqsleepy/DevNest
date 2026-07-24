package network

import (
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

func TestParseTargetAcceptsWhatPeopleType(t *testing.T) {
	tests := []struct {
		input      string
		wantURL    string
		wantHost   string
		wantScheme string
		wantPort   int
	}{
		{"https://example.com", "https://example.com", "example.com", "https", 443},
		{"http://example.com", "http://example.com", "example.com", "http", 80},
		{"example.com", "https://example.com", "example.com", "https", 443},
		{"example.com/health", "https://example.com/health", "example.com", "https", 443},
		{"https://example.com:8443/x", "https://example.com:8443/x", "example.com", "https", 8443},
		{"http://localhost:3000", "http://localhost:3000", "localhost", "http", 3000},
		{"  https://example.com  ", "https://example.com", "example.com", "https", 443},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			target, err := ParseTarget(test.input)
			if err != nil {
				t.Fatalf("ParseTarget(%q): %v", test.input, err)
			}
			if target.URL != test.wantURL {
				t.Errorf("URL = %q, want %q", target.URL, test.wantURL)
			}
			if target.Host != test.wantHost {
				t.Errorf("Host = %q, want %q", target.Host, test.wantHost)
			}
			if target.Scheme != test.wantScheme {
				t.Errorf("Scheme = %q, want %q", target.Scheme, test.wantScheme)
			}
			if target.Port != test.wantPort {
				t.Errorf("Port = %d, want %d", target.Port, test.wantPort)
			}
		})
	}
}

// A scheme like file:// inside an HTTP command would read local files while
// the user believes a request is being made.
func TestParseTargetRejectsUnsupportedSchemes(t *testing.T) {
	for _, input := range []string{
		"file:///etc/passwd",
		"ftp://example.com",
		"gopher://example.com",
		"data:text/plain,hello",
	} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseTarget(input)
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestParseTargetRejectsMalformedInput(t *testing.T) {
	for _, input := range []string{"", "   ", "https://", "http://:8080"} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseTarget(input)
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestParseHost(t *testing.T) {
	tests := map[string]string{
		"example.com":             "example.com",
		"https://example.com/x":   "example.com",
		"example.com:8443":        "example.com",
		"example.com/path":        "example.com",
		"  example.com  ":         "example.com",
		"192.168.1.1":             "192.168.1.1",
		"https://example.com:993": "example.com",
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := ParseHost(input)
			if err != nil {
				t.Fatalf("ParseHost(%q): %v", input, err)
			}
			if got != want {
				t.Errorf("ParseHost(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestParseHostRejectsNonsense(t *testing.T) {
	for _, input := range []string{"", "   ", "has space", "file:///x"} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseHost(input)
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestParseDomain(t *testing.T) {
	tests := map[string]string{
		"example.com":           "example.com",
		"example.com.":          "example.com",
		"https://example.com/x": "example.com",
		"example.com:443":       "example.com",
		"sub.example.co.uk":     "sub.example.co.uk",
		"example.corp":          "example.corp",
	}

	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := ParseDomain(input)
			if err != nil {
				t.Fatalf("ParseDomain(%q): %v", input, err)
			}
			if got != want {
				t.Errorf("ParseDomain(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestParseDomainRejectsMalformedNames(t *testing.T) {
	long := strings.Repeat("a", 64) + ".com"

	for _, input := range []string{"", "  ", "example..com", ".example.com", "a b.com", long} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseDomain(input)
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestParseMethod(t *testing.T) {
	for _, input := range []string{"get", "GET", " post ", "DELETE"} {
		got, err := ParseMethod(input)
		if err != nil {
			t.Fatalf("ParseMethod(%q): %v", input, err)
		}
		if got != strings.ToUpper(strings.TrimSpace(input)) {
			t.Errorf("ParseMethod(%q) = %q", input, got)
		}
	}

	if got, _ := ParseMethod(""); got != "GET" {
		t.Errorf("an empty method should default to GET, got %q", got)
	}
	if _, err := ParseMethod("BREW"); err == nil {
		t.Error("ParseMethod accepted a method DevNest does not send")
	}
}

func TestParseHeader(t *testing.T) {
	header, err := ParseHeader("Accept: application/json")
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if header.Name != "Accept" || header.Value != "application/json" {
		t.Errorf("header = %+v", header)
	}

	// A header with an empty value is legitimate and used to unset one.
	empty, err := ParseHeader("X-Trace:")
	if err != nil || empty.Name != "X-Trace" || empty.Value != "" {
		t.Errorf("ParseHeader(\"X-Trace:\") = %+v, %v", empty, err)
	}

	for _, input := range []string{"no colon", ": value"} {
		if _, err := ParseHeader(input); err == nil {
			t.Errorf("ParseHeader(%q) returned no error", input)
		}
	}
}

func TestIsSensitive(t *testing.T) {
	sensitive := []string{
		"Authorization", "authorization", "Cookie", "Set-Cookie",
		"Proxy-Authorization", "X-Api-Key", "x-auth-token",
		"X-Company-Secret", "Session-Password",
	}
	for _, name := range sensitive {
		if !IsSensitive(name) {
			t.Errorf("IsSensitive(%q) = false, want true", name)
		}
	}

	ordinary := []string{"Content-Type", "Accept", "User-Agent", "Cache-Control", "ETag"}
	for _, name := range ordinary {
		if IsSensitive(name) {
			t.Errorf("IsSensitive(%q) = true, want false", name)
		}
	}
}

// A masked value must not leak its content in any form, and it has to be
// obvious that something was removed rather than absent.
func TestMaskHeaders(t *testing.T) {
	headers := []net.Header{
		{Name: "Authorization", Value: "Bearer sk-live-0123456789abcdef"},
		{Name: "Content-Type", Value: "application/json"},
	}

	masked := maskHeaders(headers, false)
	if strings.Contains(masked[0].Value, "sk-live") {
		t.Errorf("the secret survived masking: %q", masked[0].Value)
	}
	if !strings.HasPrefix(masked[0].Value, "***") {
		t.Errorf("masked value = %q, want it visibly redacted", masked[0].Value)
	}
	if masked[1].Value != "application/json" {
		t.Errorf("an ordinary header was masked: %q", masked[1].Value)
	}

	shown := maskHeaders(headers, true)
	if shown[0].Value != headers[0].Value {
		t.Errorf("--show-secrets did not show the value: %q", shown[0].Value)
	}
}

func TestSummarise(t *testing.T) {
	samples := []time.Duration{
		30 * time.Millisecond,
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
	}

	stats := summarise(samples)
	if stats.MinMs != 10 {
		t.Errorf("MinMs = %d, want 10", stats.MinMs)
	}
	if stats.MaxMs != 40 {
		t.Errorf("MaxMs = %d, want 40", stats.MaxMs)
	}
	if stats.AverageMs != 25 {
		t.Errorf("AverageMs = %d, want 25", stats.AverageMs)
	}
	if stats.MedianMs != 25 {
		t.Errorf("MedianMs = %d, want 25", stats.MedianMs)
	}
	if stats.StdDevMs <= 0 {
		t.Errorf("StdDevMs = %v, want a positive spread", stats.StdDevMs)
	}
}

func TestSummariseOnASingleSample(t *testing.T) {
	stats := summarise([]time.Duration{15 * time.Millisecond})

	if stats.MinMs != 15 || stats.MaxMs != 15 || stats.AverageMs != 15 || stats.MedianMs != 15 {
		t.Errorf("stats = %+v, want every figure to be 15", stats)
	}
	if stats.StdDevMs != 0 {
		t.Errorf("StdDevMs = %v, want 0 for one sample", stats.StdDevMs)
	}
}

func TestSummariseOnNoSamples(t *testing.T) {
	if stats := summarise(nil); stats != (Statistics{}) {
		t.Errorf("stats = %+v, want zeroes", stats)
	}
}

// The median is reported precisely because a single slow attempt drags the
// average; a test that did not check they can disagree would miss the point.
func TestSummariseMedianResistsAnOutlier(t *testing.T) {
	stats := summarise([]time.Duration{
		10 * time.Millisecond,
		10 * time.Millisecond,
		10 * time.Millisecond,
		2000 * time.Millisecond,
	})

	if stats.MedianMs != 10 {
		t.Errorf("MedianMs = %d, want 10", stats.MedianMs)
	}
	if stats.AverageMs <= stats.MedianMs {
		t.Errorf("average %d should be dragged above the median %d",
			stats.AverageMs, stats.MedianMs)
	}
}
