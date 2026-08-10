package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/core/network"
	"github.com/devnest/devnest/internal/platform/net"
)

func TestMonitorTextReportsAHealthySite(t *testing.T) {
	result := network.MonitorResult{
		URL:        "https://example.com",
		Status:     network.StatusUp,
		Healthy:    true,
		StatusCode: 200,
		StatusText: "200 OK",
		ResponseMs: 142,
		CheckedAt:  time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
	}

	got := render(t, monitorText(result))
	for _, want := range []string{"https://example.com", "up", "200 OK", "142 ms"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestMonitorTextExplainsWhyASiteIsDown(t *testing.T) {
	result := network.MonitorResult{
		URL:       "https://example.com",
		Status:    network.StatusDown,
		Reason:    "cannot reach https://example.com",
		CheckedAt: time.Now().UTC(),
	}

	got := render(t, monitorText(result))
	if !strings.Contains(got, "down") {
		t.Errorf("output = %q, want the status", got)
	}
	if !strings.Contains(got, "cannot reach") {
		t.Errorf("output = %q, want the reason", got)
	}
}

func TestMonitorTextShowsTheFinalURLAfterARedirect(t *testing.T) {
	result := network.MonitorResult{
		URL:        "https://example.com",
		FinalURL:   "https://www.example.com/",
		Status:     network.StatusUp,
		Healthy:    true,
		StatusCode: 200,
		StatusText: "200 OK",
		Redirects:  1,
		CheckedAt:  time.Now().UTC(),
	}

	got := render(t, monitorText(result))
	if !strings.Contains(got, "www.example.com") {
		t.Errorf("output = %q, want the final url", got)
	}
}

func TestHTTPTextShowsStatusTimingAndHeaders(t *testing.T) {
	result := network.FetchResult{
		Method:     "GET",
		URL:        "https://example.com",
		Status:     "200 OK",
		Protocol:   "HTTP/2.0",
		StatusCode: 200,
		ResponseHeaders: []net.Header{
			{Name: "Content-Type", Value: "text/html"},
		},
		Timing:    net.Timing{DNSMs: 4, ConnectMs: 12, TLSMs: 30, FirstByteMs: 88, TotalMs: 95},
		TLS:       &net.Session{Version: "TLS 1.3", CipherSuite: "TLS_AES_128_GCM_SHA256"},
		BodyBytes: 2048,
	}

	got := render(t, httpText(result, false))
	for _, want := range []string{
		"GET https://example.com", "200 OK", "Timing", "dns", "tls",
		"TLS 1.3", "Content-Type", "2.0 KB",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "\nBody\n") {
		t.Error("the body was printed without --body")
	}
}

// A phase that did not happen must not be shown as zero, which would read as
// "instant" rather than "not applicable".
func TestHTTPTextOmitsPhasesThatDidNotHappen(t *testing.T) {
	result := network.FetchResult{
		Method: "GET", URL: "http://example.com", Status: "200 OK",
		Timing: net.Timing{TotalMs: 20},
	}

	got := render(t, httpText(result, false))
	if strings.Contains(got, "tls") {
		t.Errorf("output = %q, want no TLS line for a plain request", got)
	}
	if !strings.Contains(got, "total") {
		t.Errorf("output = %q, want the total", got)
	}
}

func TestHTTPTextShowsTheBodyOnRequest(t *testing.T) {
	result := network.FetchResult{
		Method: "GET", URL: "https://example.com", Status: "200 OK",
		Body: "hello world", BodyBytes: 11,
	}

	got := render(t, httpText(result, true))
	if !strings.Contains(got, "hello world") {
		t.Errorf("output = %q, want the body", got)
	}
}

func TestHTTPTextListsTheRedirectChain(t *testing.T) {
	result := network.FetchResult{
		Method: "GET", URL: "https://example.com", Status: "200 OK",
		Redirects: []net.Hop{
			{URL: "https://example.com", StatusCode: 301, Location: "https://www.example.com"},
		},
	}

	got := render(t, httpText(result, false))
	for _, want := range []string{"Redirects", "301", "www.example.com"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestLatencyTextShowsTheSummary(t *testing.T) {
	result := network.LatencyResult{
		URL: "https://example.com", Method: "GET",
		Attempts: 3, Successful: 3,
		Statistics: network.Statistics{MinMs: 10, AverageMs: 20, MedianMs: 18, MaxMs: 30, StdDevMs: 8.16},
	}

	got := render(t, latencyText(result, false))
	for _, want := range []string{"minimum", "average", "median", "maximum", "3 attempts"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestLatencyTextPointsAtShowAttemptsWhenSomethingFailed(t *testing.T) {
	result := network.LatencyResult{
		URL: "https://example.com", Method: "GET",
		Attempts: 3, Successful: 2, Failed: 1,
		Statistics: network.Statistics{MinMs: 10, AverageMs: 20, MedianMs: 20, MaxMs: 30},
	}

	got := render(t, latencyText(result, false))
	if !strings.Contains(got, "--show-attempts") {
		t.Errorf("output = %q, want it to suggest --show-attempts", got)
	}
}

func TestLatencyTextListsEveryAttemptOnRequest(t *testing.T) {
	result := network.LatencyResult{
		URL: "https://example.com", Method: "GET",
		Attempts: 2, Successful: 1, Failed: 1,
		Samples: []network.Attempt{
			{Number: 1, ResponseMs: 12, StatusCode: 200, OK: true},
			{Number: 2, Error: "connection reset"},
		},
		Statistics: network.Statistics{MinMs: 12, AverageMs: 12, MedianMs: 12, MaxMs: 12},
	}

	got := render(t, latencyText(result, true))
	for _, want := range []string{"attempt", "12 ms", "connection reset"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// The output has to say the probe was TCP, or the reader cannot tell which
// question was answered.
func TestPingTextSaysItUsedTCP(t *testing.T) {
	result := network.PingResult{
		Host: "example.com", Port: 443, Method: "tcp",
		Addresses: []string{"93.184.216.34"},
		Sent:      4, Received: 4, Reachable: true,
		Statistics: network.Statistics{MinMs: 10, AverageMs: 15, MaxMs: 22},
	}

	got := render(t, pingText(result, false))
	for _, want := range []string{"tcp probe", "93.184.216.34", "4 sent, 4 received", "0% loss"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestPingTextExplainsAnUnreachableHost(t *testing.T) {
	result := network.PingResult{
		Host: "example.com", Port: 443, Method: "tcp",
		Sent: 2, Received: 0, LossPercent: 100,
		Probes: []network.Probe{
			{Number: 1, Error: "connection refused"},
			{Number: 2, Error: "connection refused"},
		},
	}

	got := render(t, pingText(result, false))
	if !strings.Contains(got, "100% loss") {
		t.Errorf("output = %q, want the loss figure", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("output = %q, want the reason", got)
	}
}

func TestDNSTextListsRecordsAndEmptyTypes(t *testing.T) {
	result := network.LookupResult{
		Domain: "example.com", Found: 2, Resolved: true, Duration: 30,
		Answers: []net.Answer{
			{Kind: "A", Records: []net.Record{{Value: "93.184.216.34"}}},
			{Kind: "MX", Records: []net.Record{{Value: "mail.example.com.", Priority: 10}}},
			{Kind: "TXT", Records: []net.Record{}, Error: "no records of this type"},
		},
	}

	got := render(t, dnsText(result))
	for _, want := range []string{"93.184.216.34", "priority 10", "no records of this type", "2 records"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestSSLTextReportsAValidCertificate(t *testing.T) {
	result := network.InspectResult{
		Host: "example.com", Port: 443,
		Validity: network.ValidityValid, Valid: true, Trusted: true,
		Subject: "CN=example.com", Issuer: "CN=Example CA",
		NotBefore:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:      time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		DaysRemaining: 162,
		DNSNames:      []string{"example.com", "www.example.com"},
		TLSVersion:    "TLS 1.3",
	}

	got := render(t, sslText(result))
	for _, want := range []string{"example.com:443", "valid", "CN=Example CA", "2027-01-01", "162"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestSSLTextExplainsWhyACertificateIsNotTrusted(t *testing.T) {
	result := network.InspectResult{
		Host: "example.com", Port: 443,
		Validity:   network.ValidityUntrusted,
		Trusted:    false,
		TrustError: "x509: certificate signed by unknown authority",
		NotAfter:   time.Now().AddDate(0, 6, 0),
		SelfSigned: true,
	}

	got := render(t, sslText(result))
	if !strings.Contains(got, "unknown authority") {
		t.Errorf("output = %q, want the trust failure explained", got)
	}
	if !strings.Contains(got, "self-signed") {
		t.Errorf("output = %q, want the self-signed note", got)
	}
}

func TestSSLTextPrintsTheChainOnRequest(t *testing.T) {
	result := network.InspectResult{
		Host: "example.com", Port: 443, Validity: network.ValidityValid, Valid: true,
		NotAfter: time.Now().AddDate(1, 0, 0),
		Chain: []net.Certificate{
			{Subject: "CN=example.com", NotAfter: time.Now().AddDate(1, 0, 0)},
			{Subject: "CN=Example CA", NotAfter: time.Now().AddDate(5, 0, 0)},
		},
	}

	got := render(t, sslText(result))
	if !strings.Contains(got, "Chain") {
		t.Errorf("output = %q, want the chain section", got)
	}
	if !strings.Contains(got, "CN=Example CA") {
		t.Errorf("output = %q, want the intermediate", got)
	}
}

func TestSSLHint(t *testing.T) {
	untrusted := network.InspectResult{TrustError: "bad signature"}
	if got := sslHint(untrusted); got != "bad signature" {
		t.Errorf("sslHint = %q", got)
	}

	expired := network.InspectResult{
		Validity: network.ValidityExpired,
		NotAfter: time.Date(2020, 3, 4, 0, 0, 0, 0, time.UTC),
	}
	if got := sslHint(expired); !strings.Contains(got, "2020") {
		t.Errorf("sslHint = %q, want the expiry date", got)
	}
}

func TestMillisecondsFormatting(t *testing.T) {
	tests := map[int64]string{
		0:    "0 ms",
		42:   "42 ms",
		999:  "999 ms",
		1000: "1.00 s",
		2500: "2.50 s",
	}
	for input, want := range tests {
		if got := milliseconds(input); got != want {
			t.Errorf("milliseconds(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestParseHeadersRejectsMalformedInput(t *testing.T) {
	headers, err := parseHeaders([]string{"Accept: text/plain", "X-Trace: 1"})
	if err != nil {
		t.Fatalf("parseHeaders: %v", err)
	}
	if len(headers) != 2 || headers[0].Name != "Accept" {
		t.Errorf("headers = %+v", headers)
	}

	if _, err := parseHeaders([]string{"no colon"}); err == nil {
		t.Error("parseHeaders accepted a header with no colon")
	}
}

func TestFirstTarget(t *testing.T) {
	if _, err := firstTarget(nil, "devnest network ping"); err == nil {
		t.Error("firstTarget accepted no argument")
	}
	if _, err := firstTarget([]string{"a", "b"}, "devnest network ping"); err == nil {
		t.Error("firstTarget accepted two arguments")
	}
	got, err := firstTarget([]string{"example.com"}, "devnest network ping")
	if err != nil || got != "example.com" {
		t.Errorf("firstTarget = %q, %v", got, err)
	}
}

func TestParseKinds(t *testing.T) {
	kinds, err := parseKinds([]string{"a", "MX"})
	if err != nil {
		t.Fatalf("parseKinds: %v", err)
	}
	if len(kinds) != 2 || kinds[0] != net.KindA || kinds[1] != net.KindMX {
		t.Errorf("kinds = %v", kinds)
	}

	if _, err := parseKinds([]string{"SOA"}); err == nil {
		t.Error("parseKinds accepted an unsupported type")
	}
}

func TestDurationMsFlagValue(t *testing.T) {
	var target int64
	value := newDurationMsValue(&target, 0)

	if err := value.Set("500ms"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if target != 500 {
		t.Errorf("target = %d, want 500", target)
	}
	if err := value.Set("2s"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if target != 2000 {
		t.Errorf("target = %d, want 2000", target)
	}

	for _, bad := range []string{"soon", "", "-5s"} {
		if err := value.Set(bad); err == nil {
			t.Errorf("Set(%q) returned no error", bad)
		}
	}
}

func TestScanTextListsOpenPorts(t *testing.T) {
	result := network.ScanResult{
		Host:      "example.com",
		Addresses: []string{"93.184.216.34"},
		Open: []network.OpenPort{
			{Port: 22, Service: "ssh", ResponseMs: 12},
			{Port: 443, Service: "https", ResponseMs: 30},
		},
		OpenCount: 2, ClosedCount: 60, FilteredCount: 0,
		TotalPorts: 62, DurationMs: 800,
	}

	got := render(t, scanText(result))
	for _, want := range []string{
		"93.184.216.34", "22", "ssh", "443", "https",
		"62 ports scanned", "2 open", "60 closed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

// The table carries the service name next to the port, but a port the registry
// does not know must not get an invented name — it gets a dash instead.
func TestScanTextReportsAnUnknownServiceAsADash(t *testing.T) {
	result := network.ScanResult{
		Host:      "example.com",
		Addresses: []string{"93.184.216.34"},
		Open: []network.OpenPort{
			{Port: 62000, ResponseMs: 8},
		},
		OpenCount: 1, TotalPorts: 1,
	}

	got := render(t, scanText(result))
	if !strings.Contains(got, "62000  -") {
		t.Errorf("output = %q, want the service cell to show a plain dash", got)
	}
	if strings.Contains(got, "no-such-service") {
		t.Errorf("output = %q, an invented service name was printed", got)
	}
}

func TestScanTextReportsANoOpenPortsRun(t *testing.T) {
	result := network.ScanResult{
		Host: "example.com", Addresses: []string{"93.184.216.34"},
		ClosedCount: 50, TotalPorts: 50, DurationMs: 400,
	}

	got := render(t, scanText(result))
	for _, want := range []string{"No open ports were found.", "50 closed"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}
