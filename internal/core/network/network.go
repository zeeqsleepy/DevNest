// Package network is DevNest's networking module: checking whether a site is
// up, inspecting an HTTP exchange, measuring latency, probing a host, looking
// up DNS records, and inspecting a TLS certificate.
//
// Every operation takes a request and returns a result. Nothing here prints,
// exits, reads configuration, or knows that a command line exists.
//
// Two rules run through all of it. Every operation is bounded: there is no
// path that waits forever, because a command that hangs is worse than one that
// fails. And a network failure is an ordinary outcome, not a crash: an
// unreachable host, a refused connection, and a broken certificate are all
// things these commands are for, so they are reported rather than thrown.
package network

import (
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

// Target is a validated URL.
type Target struct {
	URL    string
	Scheme string
	Host   string
	Port   int
}

// ParseTarget validates a URL and fills in what the user left out.
//
// A bare host is accepted and given https, because "devnest network monitor
// example.com" is what people type and refusing it to make a point about URL
// syntax helps nobody.
//
// Only http and https are allowed. A scheme like file:// inside an HTTP
// command is a confused-deputy problem waiting to happen: the command would
// read local files while the user believes it is making a network request.
func ParseTarget(raw string) (Target, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Target{}, errors.New(errors.CodeInvalidInput, "no url was given").
			WithHint("pass a url, for example https://example.com")
	}

	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return Target{}, errors.Wrap(err, errors.CodeInvalidInput, "invalid url %q", raw).
			WithHint("expected something like https://example.com/path")
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return Target{}, errors.New(errors.CodeInvalidInput,
			"unsupported scheme %q in %q", parsed.Scheme, raw).
			WithHint("only http and https are supported")
	}
	if parsed.Hostname() == "" {
		return Target{}, errors.New(errors.CodeInvalidInput, "invalid url %q: no host", raw).
			WithHint("expected something like https://example.com/path")
	}

	target := Target{
		URL:    parsed.String(),
		Scheme: scheme,
		Host:   parsed.Hostname(),
		Port:   defaultPort(scheme),
	}
	if explicit := parsed.Port(); explicit != "" {
		port, err := strconv.Atoi(explicit)
		if err != nil || port < 1 || port > 65535 {
			return Target{}, errors.New(errors.CodeInvalidInput,
				"invalid port %q in %q", explicit, raw)
		}
		target.Port = port
	}

	return target, nil
}

func defaultPort(scheme string) int {
	if scheme == "http" {
		return 80
	}
	return 443
}

// ParseHost validates a bare host name or address, for the commands that take
// one rather than a URL.
//
// A URL is accepted and reduced to its host, because someone who has just run
// "devnest network ssl https://example.com" should not be told off for the
// scheme they were told to use a moment earlier.
func ParseHost(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New(errors.CodeInvalidInput, "no host was given").
			WithHint("pass a host name or address, for example example.com")
	}

	if strings.Contains(trimmed, "://") {
		target, err := ParseTarget(trimmed)
		if err != nil {
			return "", err
		}
		return target.Host, nil
	}

	// A bare "host:port" is common enough to accept, and taking the host from
	// it is less surprising than rejecting it.
	if host, _, found := strings.Cut(trimmed, "/"); found {
		trimmed = host
	}
	if strings.Count(trimmed, ":") == 1 {
		trimmed = strings.SplitN(trimmed, ":", 2)[0]
	}

	if trimmed == "" || strings.ContainsAny(trimmed, " \t\\?#") {
		return "", errors.New(errors.CodeInvalidInput, "invalid host %q", raw).
			WithHint("expected a host name or address, for example example.com")
	}

	return trimmed, nil
}

// Methods lists the HTTP methods DevNest will send.
func Methods() []string {
	return []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
}

// ParseMethod validates an HTTP method.
func ParseMethod(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "GET", nil
	}

	candidate := strings.ToUpper(strings.TrimSpace(name))
	for _, supported := range Methods() {
		if candidate == supported {
			return supported, nil
		}
	}

	return "", errors.New(errors.CodeInvalidInput, "unsupported method %q", name).
		WithHint("expected one of: %s", strings.Join(Methods(), ", "))
}

// ParseHeader reads a "Name: value" pair as given on the command line.
func ParseHeader(raw string) (net.Header, error) {
	name, value, found := strings.Cut(raw, ":")
	if !found || strings.TrimSpace(name) == "" {
		return net.Header{}, errors.New(errors.CodeInvalidInput,
			"invalid header %q", raw).
			WithHint("expected \"Name: value\", for example --header \"Accept: application/json\"")
	}

	return net.Header{
		Name:  strings.TrimSpace(name),
		Value: strings.TrimSpace(value),
	}, nil
}

// sensitiveHeaders are masked in output unless the caller asks otherwise.
// Exact names first; anything containing one of the fragments below is masked
// too, which catches the endless variations on X-Company-Api-Token.
var (
	sensitiveNames = map[string]bool{
		"authorization":       true,
		"proxy-authorization": true,
		"cookie":              true,
		"set-cookie":          true,
		"www-authenticate":    true,
		"proxy-authenticate":  true,
	}
	sensitiveFragments = []string{"token", "secret", "password", "passwd", "api-key", "apikey", "auth"}
)

// IsSensitive reports whether a header's value should be masked.
func IsSensitive(name string) bool {
	lowered := strings.ToLower(strings.TrimSpace(name))
	if sensitiveNames[lowered] {
		return true
	}
	for _, fragment := range sensitiveFragments {
		if strings.Contains(lowered, fragment) {
			return true
		}
	}
	return false
}

// maskHeaders replaces credential-shaped values with a placeholder.
//
// The masking happens here, in the result the renderer and the export both
// read, rather than in one output path. A report gets attached to a ticket and
// a ticket gets shared; a secret that survives into either is a leak whatever
// format it was printed in.
func maskHeaders(headers []net.Header, show bool) []net.Header {
	masked := make([]net.Header, 0, len(headers))

	for _, header := range headers {
		if !show && IsSensitive(header.Name) {
			header.Value = "*** (" + strconv.Itoa(len(header.Value)) + " characters)"
		}
		masked = append(masked, header)
	}

	return masked
}

// Statistics summarises a set of measurements.
type Statistics struct {
	MinMs     int64   `json:"minMs"`
	MaxMs     int64   `json:"maxMs"`
	AverageMs int64   `json:"averageMs"`
	MedianMs  int64   `json:"medianMs"`
	StdDevMs  float64 `json:"standardDeviationMs"`
}

// summarise reduces a set of durations to the numbers people actually read.
//
// The median is included alongside the average because a single slow attempt
// drags an average badly, and knowing the two disagree is the whole signal
// that something is intermittent.
func summarise(samples []time.Duration) Statistics {
	if len(samples) == 0 {
		return Statistics{}
	}

	ordered := make([]int64, 0, len(samples))
	var total int64
	for _, sample := range samples {
		milliseconds := sample.Milliseconds()
		ordered = append(ordered, milliseconds)
		total += milliseconds
	}
	slices.Sort(ordered)

	average := total / int64(len(ordered))

	var variance float64
	for _, value := range ordered {
		difference := float64(value - average)
		variance += difference * difference
	}
	variance /= float64(len(ordered))

	return Statistics{
		MinMs:     ordered[0],
		MaxMs:     ordered[len(ordered)-1],
		AverageMs: average,
		MedianMs:  median(ordered),
		StdDevMs:  round2(math.Sqrt(variance)),
	}
}

func median(ordered []int64) int64 {
	middle := len(ordered) / 2
	if len(ordered)%2 == 1 {
		return ordered[middle]
	}
	return (ordered[middle-1] + ordered[middle]) / 2
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}
