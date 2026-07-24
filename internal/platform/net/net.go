// Package net is the network half of the platform layer.
//
// Everything that opens a socket lives here, behind methods on System.
// Modules declare the narrow interfaces they need and receive a System in
// production and a fake in tests, so no module has to know how a redirect
// chain is followed or how a TLS handshake is timed.
//
// The standard library's own net package is imported as stdnet, since this
// package takes the name.
package net

import (
	"context"
	"crypto/tls"
	"io"
	stdnet "net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// Defaults applied when a caller leaves a field at its zero value.
const (
	DefaultTimeout      = 30 * time.Second
	DefaultMaxRedirects = 10
	DefaultMaxBody      = 64 * 1024
	DefaultPort         = 443
)

// System performs network operations. Unlike fs.System it carries settings,
// because a timeout and a redirect policy are decisions the caller makes and
// the platform layer merely honours.
type System struct {
	// Timeout bounds a whole operation. There is no unbounded default: a
	// command that hangs forever is worse than one that fails.
	Timeout time.Duration
	// FollowRedirects follows 3xx responses.
	FollowRedirects bool
	// MaxRedirects caps the chain.
	MaxRedirects int
	// MaxBodyBytes caps how much of a response body is read into memory.
	MaxBodyBytes int64
	// Insecure disables certificate verification for requests. It exists for
	// diagnosis; the ssl inspection path does not need it and does not use it.
	Insecure bool
	// UserAgent identifies DevNest to the server. A request with no
	// User-Agent at all is rude to whoever operates the other end.
	UserAgent string
}

// Header is one header field. A slice keeps the order the server sent, which a
// map would throw away.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Request describes one HTTP request.
type Request struct {
	Method  string
	URL     string
	Headers []Header
	Body    []byte
}

// Timing is the breakdown of where a request's time went.
//
// For a redirect chain the phases describe the final request; TotalMs covers
// the whole chain. Reporting an average of the phases across hops would be a
// number that describes nothing.
type Timing struct {
	DNSMs       int64 `json:"dnsMs"`
	ConnectMs   int64 `json:"connectMs"`
	TLSMs       int64 `json:"tlsMs"`
	FirstByteMs int64 `json:"firstByteMs"`
	TotalMs     int64 `json:"totalMs"`
}

// Hop is one step of a redirect chain.
type Hop struct {
	URL        string `json:"url"`
	StatusCode int    `json:"statusCode"`
	Location   string `json:"location"`
}

// Response is everything observed about one HTTP exchange.
type Response struct {
	Method        string   `json:"method"`
	URL           string   `json:"url"`
	FinalURL      string   `json:"finalUrl"`
	StatusCode    int      `json:"statusCode"`
	Status        string   `json:"status"`
	Protocol      string   `json:"protocol"`
	Headers       []Header `json:"headers"`
	ContentLength int64    `json:"contentLength"`
	Body          string   `json:"body,omitempty"`
	BodyBytes     int64    `json:"bodyBytes"`
	BodyTruncated bool     `json:"bodyTruncated"`
	Timing        Timing   `json:"timing"`
	Redirects     []Hop    `json:"redirects"`
	TLS           *Session `json:"tls,omitempty"`
}

func (s System) timeout() time.Duration {
	if s.Timeout <= 0 {
		return DefaultTimeout
	}
	return s.Timeout
}

func (s System) maxRedirects() int {
	if s.MaxRedirects <= 0 {
		return DefaultMaxRedirects
	}
	return s.MaxRedirects
}

func (s System) maxBody() int64 {
	if s.MaxBodyBytes <= 0 {
		return DefaultMaxBody
	}
	return s.MaxBodyBytes
}

// Request performs one HTTP exchange and reports everything about it.
//
// Discard is set by callers that only need the status and the timing; the body
// is still drained, because a request whose body is never read leaves the
// exchange incomplete and the timing meaningless.
func (s System) Request(ctx context.Context, request Request) (Response, error) {
	target, err := url.Parse(request.URL)
	if err != nil {
		return Response{}, errors.Wrap(err, errors.CodeInvalidInput,
			"cannot parse url %s", request.URL)
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	var hops []Hop
	client := s.client(&hops)

	tracker := &phases{}
	traced := httptrace.WithClientTrace(ctx, tracker.trace())

	outgoing, err := http.NewRequestWithContext(traced, request.Method, target.String(), bodyReader(request.Body))
	if err != nil {
		return Response{}, errors.Wrap(err, errors.CodeInvalidInput, "cannot build the request")
	}
	for _, header := range request.Headers {
		outgoing.Header.Set(header.Name, header.Value)
	}
	if outgoing.Header.Get("User-Agent") == "" {
		outgoing.Header.Set("User-Agent", s.userAgent())
	}

	started := time.Now()
	incoming, err := client.Do(outgoing)
	if err != nil {
		return Response{}, classifyRequestError(err, request.URL)
	}
	defer func() {
		// The body has already been drained below; a close failure on a read
		// side cannot lose anything.
		_ = incoming.Body.Close()
	}()

	body, read, truncated, err := readBody(incoming.Body, s.maxBody())
	if err != nil {
		return Response{}, err
	}
	total := time.Since(started)

	return Response{
		Method:        request.Method,
		URL:           request.URL,
		FinalURL:      incoming.Request.URL.String(),
		StatusCode:    incoming.StatusCode,
		Status:        incoming.Status,
		Protocol:      incoming.Proto,
		Headers:       headersOf(incoming.Header),
		ContentLength: incoming.ContentLength,
		Body:          string(body),
		BodyBytes:     read,
		BodyTruncated: truncated,
		Timing:        tracker.timing(total),
		Redirects:     hops,
		TLS:           sessionOf(incoming.TLS),
	}, nil
}

// client builds a client for one invocation.
//
// Keep-alives are disabled deliberately. Every command here makes one request
// per measurement, and a reused connection would report a setup cost of nearly
// zero for the second and later attempts, a number that flatters the result
// and tells the user nothing.
func (s System) client(hops *[]Hop) *http.Client {
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DisableKeepAlives: true,
		ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: s.Insecure,
		},
		DialContext: (&stdnet.Dialer{Timeout: s.timeout()}).DialContext,
	}

	return &http.Client{
		Transport:     transport,
		CheckRedirect: s.redirectPolicy(hops),
	}
}

// redirectPolicy records every hop and refuses to carry credentials to a host
// the user did not choose.
//
// Go already strips Authorization and Cookie on a cross-domain redirect. This
// repeats that and adds Proxy-Authorization, because relying on a default that
// might change for something this consequential is not a trade worth making.
func (s System) redirectPolicy(hops *[]Hop) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if !s.FollowRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) > s.maxRedirects() {
			return errors.New(errors.CodeNetwork,
				"stopped after %d redirects", s.maxRedirects()).
				WithHint("pass --max-redirects to allow more, or --no-redirect to stop at the first")
		}

		previous := via[len(via)-1]
		hop := Hop{URL: previous.URL.String(), Location: request.URL.String()}
		if request.Response != nil {
			hop.StatusCode = request.Response.StatusCode
		}
		*hops = append(*hops, hop)

		if !sameOrigin(previous.URL, request.URL) {
			for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
				request.Header.Del(name)
			}
		}
		return nil
	}
}

func sameOrigin(from, to *url.URL) bool {
	return strings.EqualFold(from.Hostname(), to.Hostname()) &&
		from.Scheme == to.Scheme &&
		from.Port() == to.Port()
}

func bodyReader(body []byte) io.Reader {
	if len(body) == 0 {
		return nil
	}
	return strings.NewReader(string(body))
}

// readBody reads up to limit bytes and reports whether there was more.
//
// The remainder is drained rather than abandoned so the exchange completes;
// a request left half-read makes the recorded timing meaningless.
func readBody(source io.Reader, limit int64) ([]byte, int64, bool, error) {
	body, err := io.ReadAll(io.LimitReader(source, limit))
	if err != nil {
		return nil, 0, false, errors.Wrap(err, errors.CodeNetwork, "cannot read the response body")
	}

	extra, err := io.Copy(io.Discard, source)
	if err != nil {
		return nil, 0, false, errors.Wrap(err, errors.CodeNetwork, "cannot read the response body")
	}

	return body, int64(len(body)) + extra, extra > 0, nil
}

func headersOf(source http.Header) []Header {
	headers := make([]Header, 0, len(source))
	for name, values := range source {
		for _, value := range values {
			headers = append(headers, Header{Name: name, Value: value})
		}
	}
	sortHeaders(headers)
	return headers
}

// classifyRequestError turns a transport failure into something a user can act
// on. The raw error from the http client names internal types and repeats the
// URL three times.
func classifyRequestError(err error, target string) error {
	switch {
	case errors.Is(err, context.Canceled):
		return errors.Wrap(err, errors.CodeCancelled, "cancelled")

	case errors.Is(err, context.DeadlineExceeded):
		return errors.Wrap(err, errors.CodeTimeout, "%s did not respond in time", target).
			WithHint("pass --timeout to wait longer")
	}

	var dnsErr *stdnet.DNSError
	if errors.As(err, &dnsErr) {
		return errors.Wrap(err, errors.CodeNotFound, "cannot resolve %s", dnsErr.Name).
			WithHint("check the host name, and that this machine has working DNS")
	}

	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return errors.Wrap(err, errors.CodeNetwork, "the certificate for %s could not be verified", target).
			WithHint("run \"devnest network ssl\" on this host to see why, " +
				"or pass --insecure to connect anyway")
	}

	var opErr *stdnet.OpError
	if errors.As(err, &opErr) {
		return errors.Wrap(err, errors.CodeNetwork, "cannot reach %s", target).
			WithHint("check the host and port, and whether a firewall or proxy is in the way")
	}

	return errors.Wrap(err, errors.CodeNetwork, "request to %s failed", target)
}

func (s System) userAgent() string {
	if trimmed := strings.TrimSpace(s.UserAgent); trimmed != "" {
		return trimmed
	}
	return "devnest"
}

// Address joins a host and port, bracketing an IPv6 literal.
func Address(host string, port int) string {
	return stdnet.JoinHostPort(host, strconv.Itoa(port))
}
