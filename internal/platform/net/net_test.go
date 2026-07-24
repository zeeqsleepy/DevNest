package net

import (
	"context"
	stdnet "net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// These tests run against a server on the loopback interface, never against
// the internet. The platform layer is the socket seam, so testing it against a
// fake would test the fake; testing it against a real host would make the
// suite fail whenever the network does.

func system() System {
	return System{Timeout: 5 * time.Second, FollowRedirects: true, MaxRedirects: 5}
}

func TestRequestReportsTheExchange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Test", "value")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	response, err := system().Request(context.Background(), Request{
		Method: "GET", URL: server.URL,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	if response.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want 201", response.StatusCode)
	}
	if response.Body != "hello" {
		t.Errorf("Body = %q", response.Body)
	}
	if response.BodyBytes != 5 {
		t.Errorf("BodyBytes = %d, want 5", response.BodyBytes)
	}
	if response.Timing.TotalMs < 0 {
		t.Errorf("TotalMs = %d", response.Timing.TotalMs)
	}

	found := false
	for _, header := range response.Headers {
		if header.Name == "X-Test" && header.Value == "value" {
			found = true
		}
	}
	if !found {
		t.Errorf("headers = %+v, want X-Test", response.Headers)
	}
}

func TestRequestSendsHeadersAndBody(t *testing.T) {
	var seenHeader, seenBody, seenMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Get("X-Custom")
		seenMethod = r.Method
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		seenBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := system().Request(context.Background(), Request{
		Method:  "POST",
		URL:     server.URL,
		Headers: []Header{{Name: "X-Custom", Value: "sent"}},
		Body:    []byte("payload"),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	if seenMethod != "POST" {
		t.Errorf("method = %q", seenMethod)
	}
	if seenHeader != "sent" {
		t.Errorf("header = %q", seenHeader)
	}
	if seenBody != "payload" {
		t.Errorf("body = %q", seenBody)
	}
}

// A server operator seeing traffic deserves to know what is making it.
func TestRequestSendsAUserAgent(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := system()
	client.UserAgent = "devnest/1.2.3"

	if _, err := client.Request(context.Background(), Request{Method: "GET", URL: server.URL}); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if seen != "devnest/1.2.3" {
		t.Errorf("User-Agent = %q, want devnest/1.2.3", seen)
	}
}

func TestRequestFollowsRedirectsAndRecordsEveryHop(t *testing.T) {
	var final *httptest.Server
	final = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/one":
			http.Redirect(w, r, final.URL+"/two", http.StatusFound)
		case "/two":
			http.Redirect(w, r, final.URL+"/end", http.StatusMovedPermanently)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer final.Close()

	response, err := system().Request(context.Background(), Request{
		Method: "GET", URL: final.URL + "/one",
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 at the end of the chain", response.StatusCode)
	}
	if len(response.Redirects) != 2 {
		t.Fatalf("redirects = %d, want 2", len(response.Redirects))
	}
	if response.Redirects[0].StatusCode != http.StatusFound {
		t.Errorf("first hop status = %d, want 302", response.Redirects[0].StatusCode)
	}
	if !strings.HasSuffix(response.FinalURL, "/end") {
		t.Errorf("FinalURL = %q", response.FinalURL)
	}
}

func TestRequestStopsAtTheFirstResponseWhenAsked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer server.Close()

	client := system()
	client.FollowRedirects = false

	response, err := client.Request(context.Background(), Request{Method: "GET", URL: server.URL})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if response.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want the redirect itself", response.StatusCode)
	}
	if len(response.Redirects) != 0 {
		t.Errorf("redirects = %+v, want none to have been followed", response.Redirects)
	}
}

func TestRequestRefusesAnEndlessRedirectLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	defer server.Close()

	client := system()
	client.MaxRedirects = 3

	_, err := client.Request(context.Background(), Request{Method: "GET", URL: server.URL})
	if err == nil {
		t.Fatal("Request followed a redirect loop forever")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("error = %q, want it to name the redirect limit", err.Error())
	}
}

// Carrying credentials to a host the user did not choose is a real leak, and
// it is the default behaviour of several well-known tools.
func TestRequestDropsCredentialsOnACrossOriginRedirect(t *testing.T) {
	var receivedAuth, receivedCookie string

	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer origin.Close()

	_, err := system().Request(context.Background(), Request{
		Method: "GET",
		URL:    origin.URL,
		Headers: []Header{
			{Name: "Authorization", Value: "Bearer secret-token"},
			{Name: "Cookie", Value: "session=secret"},
		},
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	if receivedAuth != "" {
		t.Errorf("Authorization reached the redirect target: %q", receivedAuth)
	}
	if receivedCookie != "" {
		t.Errorf("Cookie reached the redirect target: %q", receivedCookie)
	}
}

func TestRequestKeepsCredentialsOnASameOriginRedirect(t *testing.T) {
	var received string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/end" {
			received = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/end", http.StatusFound)
	}))
	defer server.Close()

	_, err := system().Request(context.Background(), Request{
		Method:  "GET",
		URL:     server.URL,
		Headers: []Header{{Name: "Authorization", Value: "Bearer same-origin"}},
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if received != "Bearer same-origin" {
		t.Errorf("Authorization = %q, want it kept within one origin", received)
	}
}

// A body read only to a limit leaves the exchange incomplete, so the remainder
// is drained and the total is still reported.
func TestRequestCapsTheBodyAndSaysSo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	}))
	defer server.Close()

	client := system()
	client.MaxBodyBytes = 100

	response, err := client.Request(context.Background(), Request{Method: "GET", URL: server.URL})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	if len(response.Body) != 100 {
		t.Errorf("read %d bytes into the body, want the cap of 100", len(response.Body))
	}
	if !response.BodyTruncated {
		t.Error("BodyTruncated = false although the body was cut off")
	}
	if response.BodyBytes != 5000 {
		t.Errorf("BodyBytes = %d, want the full size 5000", response.BodyBytes)
	}
}

// There is no unbounded default: a command that hangs forever is worse than
// one that fails.
func TestRequestTimesOut(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	client := system()
	client.Timeout = 50 * time.Millisecond

	_, err := client.Request(context.Background(), Request{Method: "GET", URL: server.URL})
	if errors.CodeOf(err) != errors.CodeTimeout {
		t.Fatalf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeTimeout, err)
	}
	if hint := errors.Classify(err).Hint; !strings.Contains(hint, "--timeout") {
		t.Errorf("hint = %q, want it to name --timeout", hint)
	}
}

func TestRequestCancellationIsNotAFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Second)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := system().Request(ctx, Request{Method: "GET", URL: server.URL})
	if errors.CodeOf(err) != errors.CodeCancelled {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeCancelled, err)
	}
}

func TestRequestRejectsAMalformedURL(t *testing.T) {
	_, err := system().Request(context.Background(), Request{Method: "GET", URL: "://nonsense"})
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Errorf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
}

func TestRequestOnARefusedConnection(t *testing.T) {
	// Bind a port and release it, so the address is almost certainly free and
	// nothing is listening on it.
	listener, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()

	_, err = system().Request(context.Background(), Request{
		Method: "GET", URL: "http://" + address,
	})
	if errors.CodeOf(err) != errors.CodeNetwork {
		t.Errorf("code = %q, want %q (%v)", errors.CodeOf(err), errors.CodeNetwork, err)
	}
	if hint := errors.Classify(err).Hint; hint == "" {
		t.Error("a connection failure should suggest what to check")
	}
}

func TestRequestOnAHostThatDoesNotResolve(t *testing.T) {
	client := system()
	client.Timeout = 2 * time.Second

	// .invalid is reserved by RFC 2606 and never resolves.
	_, err := client.Request(context.Background(), Request{
		Method: "GET", URL: "https://devnest-test.invalid",
	})
	if err == nil {
		t.Fatal("a request to a reserved invalid domain succeeded")
	}
	code := errors.CodeOf(err)
	if code != errors.CodeNotFound && code != errors.CodeNetwork && code != errors.CodeTimeout {
		t.Errorf("code = %q, want a network-shaped classification (%v)", code, err)
	}
}

// Headers arrive from a map, whose iteration order Go deliberately randomises.
func TestHeadersAreOrderedDeterministically(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("B-Header", "2")
		w.Header().Set("A-Header", "1")
		w.Header().Set("C-Header", "3")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	first, err := system().Request(context.Background(), Request{Method: "GET", URL: server.URL})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	second, err := system().Request(context.Background(), Request{Method: "GET", URL: server.URL})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	if len(first.Headers) != len(second.Headers) {
		t.Fatalf("header counts differ: %d and %d", len(first.Headers), len(second.Headers))
	}
	for index := range first.Headers {
		if first.Headers[index].Name != second.Headers[index].Name {
			t.Fatalf("header order is not deterministic:\n%+v\n%+v", first.Headers, second.Headers)
		}
	}
}

func TestAddressBracketsIPv6(t *testing.T) {
	if got := Address("example.com", 443); got != "example.com:443" {
		t.Errorf("Address = %q", got)
	}
	if got := Address("::1", 8080); got != "[::1]:8080" {
		t.Errorf("Address = %q, want the literal bracketed", got)
	}
}

func TestSpanIgnoresStagesThatDidNotHappen(t *testing.T) {
	now := time.Now()

	if got := span(time.Time{}, now); got != 0 {
		t.Errorf("span with no start = %d, want 0", got)
	}
	if got := span(now, time.Time{}); got != 0 {
		t.Errorf("span with no end = %d, want 0", got)
	}
	if got := span(now, now.Add(-time.Second)); got != 0 {
		t.Errorf("span running backwards = %d, want 0", got)
	}
	if got := span(now, now.Add(250*time.Millisecond)); got != 250 {
		t.Errorf("span = %d, want 250", got)
	}
}
