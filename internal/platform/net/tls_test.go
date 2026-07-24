package net

import (
	"context"
	"crypto/tls"
	stdnet "net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// hostPort splits a test server's URL into the pieces Certificates takes.
func hostPort(t *testing.T, raw string) (string, int) {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse port of %s: %v", raw, err)
	}
	return parsed.Hostname(), port
}

// A self-signed certificate is exactly the case this command exists for: the
// chain has to come back and be reported, with the trust failure named.
func TestCertificatesReportsAnUntrustedChain(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := hostPort(t, server.URL)

	chain, err := system().Certificates(context.Background(), host, port)
	if err != nil {
		t.Fatalf("Certificates: %v", err)
	}

	if len(chain.Certificates) == 0 {
		t.Fatal("no certificates were reported")
	}
	if chain.Verified {
		t.Error("a self-signed certificate was reported as verified")
	}
	if chain.VerifyError == "" {
		t.Error("VerifyError is empty; the user needs to know why it failed")
	}
	if chain.Version == "" || chain.CipherSuite == "" {
		t.Errorf("session details are missing: %+v", chain)
	}
	if chain.HandshakeMs < 0 {
		t.Errorf("HandshakeMs = %d", chain.HandshakeMs)
	}
}

func TestCertificatesReportsTheLeafDetails(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := hostPort(t, server.URL)

	chain, err := system().Certificates(context.Background(), host, port)
	if err != nil {
		t.Fatalf("Certificates: %v", err)
	}

	leaf := chain.Certificates[0]
	if leaf.Subject == "" {
		t.Error("the subject is empty")
	}
	if leaf.NotAfter.IsZero() || leaf.NotBefore.IsZero() {
		t.Errorf("validity dates are missing: %+v", leaf)
	}
	if !leaf.NotAfter.After(leaf.NotBefore) {
		t.Error("NotAfter is not after NotBefore")
	}
	if leaf.SignatureAlgorithm == "" {
		t.Error("the signature algorithm is missing")
	}
	// httptest's certificate is its own issuer.
	if !leaf.SelfSigned {
		t.Error("SelfSigned = false for httptest's own certificate")
	}
}

func TestCertificatesFailsWhenNothingIsListening(t *testing.T) {
	listener, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	host, port := hostPort(t, "https://"+listener.Addr().String())
	_ = listener.Close()

	_, err = system().Certificates(context.Background(), host, port)
	if errors.CodeOf(err) != errors.CodeNetwork {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeNetwork, err)
	}
}

// A plain HTTP server on the port is a common mistake, and the message should
// point at it rather than saying something vague.
func TestCertificatesFailsClearlyOnAPortThatDoesNotSpeakTLS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := hostPort(t, server.URL)

	_, err := system().Certificates(context.Background(), host, port)
	if err == nil {
		t.Fatal("a plain HTTP port completed a TLS handshake")
	}
	if errors.CodeOf(err) != errors.CodeNetwork {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeNetwork)
	}
	if hint := errors.Classify(err).Hint; hint == "" {
		t.Error("the failure should suggest what to check")
	}
}

func TestCertificatesTimesOut(t *testing.T) {
	// A listener that accepts and never speaks leaves the handshake hanging.
	listener, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	go func() {
		connection, err := listener.Accept()
		if err == nil {
			time.Sleep(2 * time.Second)
			_ = connection.Close()
		}
	}()

	host, port := hostPort(t, "https://"+listener.Addr().String())

	client := system()
	client.Timeout = 100 * time.Millisecond

	_, err = client.Certificates(context.Background(), host, port)
	if errors.CodeOf(err) != errors.CodeTimeout {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeTimeout, err)
	}
}

// The inspection handshake must not depend on the caller having set
// --insecure: the ssl command has no such flag, and needs none.
func TestCertificatesWorksWithoutTheInsecureFlag(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := hostPort(t, server.URL)

	client := system()
	client.Insecure = false

	chain, err := client.Certificates(context.Background(), host, port)
	if err != nil {
		t.Fatalf("Certificates: %v", err)
	}
	if len(chain.Certificates) == 0 {
		t.Error("no certificate was returned")
	}
}

// A verified request against an untrusted server must fail, and the message
// should point at the command that explains why.
func TestRequestRefusesAnUntrustedCertificateByDefault(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := system().Request(context.Background(), Request{Method: "GET", URL: server.URL})
	if err == nil {
		t.Fatal("a request to a self-signed server succeeded without --insecure")
	}
	if hint := errors.Classify(err).Hint; !strings.Contains(hint, "ssl") {
		t.Errorf("hint = %q, want it to point at the ssl command", hint)
	}
}

func TestRequestWithInsecureAcceptsAnUntrustedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := system()
	client.Insecure = true

	response, err := client.Request(context.Background(), Request{Method: "GET", URL: server.URL})
	if err != nil {
		t.Fatalf("Request with --insecure: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d", response.StatusCode)
	}
	if response.TLS == nil {
		t.Error("the TLS session was not reported")
	}
}

func TestVersionName(t *testing.T) {
	tests := map[uint16]string{
		tls.VersionTLS13: "TLS 1.3",
		tls.VersionTLS12: "TLS 1.2",
		tls.VersionTLS11: "TLS 1.1",
		tls.VersionTLS10: "TLS 1.0",
	}
	for version, want := range tests {
		if got := versionName(version); got != want {
			t.Errorf("versionName(%#x) = %q, want %q", version, got, want)
		}
	}
	if got := versionName(0x0999); !strings.Contains(got, "unknown") {
		t.Errorf("versionName(unknown) = %q", got)
	}
}
