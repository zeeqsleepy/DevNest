package network

import (
	"context"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

// The module's tests run against these fakes rather than a network. Real
// sockets in a unit test mean a suite that fails when the office wifi does,
// and there is no way to ask a real server for a certificate that expired
// yesterday. The platform layer has its own tests against a loopback server,
// which is where socket behaviour belongs.

// fakeRequester answers HTTP requests from a script.
type fakeRequester struct {
	responses []net.Response
	errs      []error
	calls     []net.Request
}

func (f *fakeRequester) Request(_ context.Context, request net.Request) (net.Response, error) {
	index := len(f.calls)
	f.calls = append(f.calls, request)

	if index < len(f.errs) && f.errs[index] != nil {
		return net.Response{}, f.errs[index]
	}
	if index < len(f.responses) {
		return f.responses[index], nil
	}
	if len(f.responses) > 0 {
		// Repeat the last scripted response, so a test of twenty attempts
		// does not have to script twenty identical answers.
		return f.responses[len(f.responses)-1], nil
	}
	return net.Response{}, errors.New(errors.CodeNetwork, "no response was scripted")
}

// okResponse builds a plausible successful response.
func okResponse(status int, totalMs int64) net.Response {
	return net.Response{
		Method:     "GET",
		URL:        "https://example.com",
		FinalURL:   "https://example.com",
		StatusCode: status,
		Status:     statusText(status),
		Protocol:   "HTTP/2.0",
		Headers: []net.Header{
			{Name: "Content-Type", Value: "text/html"},
			{Name: "Set-Cookie", Value: "session=abcdef0123456789"},
		},
		Timing: net.Timing{TotalMs: totalMs, DNSMs: 2, ConnectMs: 5, TLSMs: 20, FirstByteMs: totalMs},
	}
}

func statusText(status int) string {
	switch status {
	case 200:
		return "200 OK"
	case 301:
		return "301 Moved Permanently"
	case 404:
		return "404 Not Found"
	case 500:
		return "500 Internal Server Error"
	default:
		return "000 Unknown"
	}
}

// fakeResolver answers DNS lookups from a table.
type fakeResolver struct {
	answers map[net.Kind][]net.Record
	failure error
	asked   []net.Kind
}

func (f *fakeResolver) Resolve(_ context.Context, _ string, kinds []net.Kind) ([]net.Answer, error) {
	if f.failure != nil {
		return nil, f.failure
	}
	if len(kinds) == 0 {
		kinds = net.Kinds()
	}

	answers := make([]net.Answer, 0, len(kinds))
	for _, kind := range kinds {
		f.asked = append(f.asked, kind)
		records := f.answers[kind]
		answer := net.Answer{Kind: string(kind), Records: records}
		if records == nil {
			answer.Records = []net.Record{}
			answer.Error = "no records of this type"
		}
		answers = append(answers, answer)
	}
	return answers, nil
}

// fakeProber answers TCP probes from a script.
type fakeProber struct {
	durations  []time.Duration
	errs       []error
	addresses  []string
	resolveErr error
	calls      int
}

func (f *fakeProber) Probe(_ context.Context, _ string, _ int) (time.Duration, error) {
	index := f.calls
	f.calls++

	if index < len(f.errs) && f.errs[index] != nil {
		return 0, f.errs[index]
	}
	if index < len(f.durations) {
		return f.durations[index], nil
	}
	if len(f.durations) > 0 {
		return f.durations[len(f.durations)-1], nil
	}
	return 10 * time.Millisecond, nil
}

func (f *fakeProber) ResolveHost(_ context.Context, _ string) ([]string, error) {
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return f.addresses, nil
}

// fakeInspector answers certificate inspections from a script.
type fakeInspector struct {
	chain   net.Chain
	failure error
}

func (f *fakeInspector) Certificates(_ context.Context, _ string, _ int) (net.Chain, error) {
	if f.failure != nil {
		return net.Chain{}, f.failure
	}
	return f.chain, nil
}

// certificateChain builds a chain whose leaf expires in the given number of
// days. Negative means it expired that many days ago.
func certificateChain(daysFromNow int, verified bool) net.Chain {
	now := time.Now().UTC()

	chain := net.Chain{
		Host:        "example.com",
		Port:        443,
		Version:     "TLS 1.3",
		CipherSuite: "TLS_AES_128_GCM_SHA256",
		Verified:    verified,
		Certificates: []net.Certificate{{
			Subject:   "CN=example.com",
			Issuer:    "CN=Example CA",
			NotBefore: now.AddDate(0, -1, 0),
			NotAfter:  now.AddDate(0, 0, daysFromNow),
			DNSNames:  []string{"example.com", "www.example.com"},
		}},
	}
	if !verified {
		chain.VerifyError = "x509: certificate signed by unknown authority"
	}
	return chain
}

func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}
