package network

import (
	"context"

	"github.com/devnest/devnest/internal/platform/net"
)

// FetchRequest describes one HTTP inspection.
type FetchRequest struct {
	URL     string
	Method  string
	Headers []net.Header
	Body    []byte
	// ShowSecrets prints credential-shaped header values in full. Without it
	// they are masked, in every output format.
	ShowSecrets bool
}

// FetchResult is everything observed about one exchange.
//
// RequestHeaders is included because a request that behaved unexpectedly is
// usually a request that carried something the user did not realise it was
// carrying, and a report showing only the response leaves that invisible.
type FetchResult struct {
	Method          string       `json:"method"`
	URL             string       `json:"url"`
	FinalURL        string       `json:"finalUrl"`
	StatusCode      int          `json:"statusCode"`
	Status          string       `json:"status"`
	Protocol        string       `json:"protocol"`
	RequestHeaders  []net.Header `json:"requestHeaders"`
	ResponseHeaders []net.Header `json:"responseHeaders"`
	ContentLength   int64        `json:"contentLength"`
	Body            string       `json:"body,omitempty"`
	BodyBytes       int64        `json:"bodyBytes"`
	BodyTruncated   bool         `json:"bodyTruncated"`
	Timing          net.Timing   `json:"timing"`
	Redirects       []net.Hop    `json:"redirects"`
	TLS             *net.Session `json:"tls,omitempty"`
}

// Fetch sends one request and reports what came back.
//
// Unlike Monitor, a non-2xx status is not a failure here: inspecting a 404 or
// a 500 is a perfectly ordinary reason to run this command, and the exit code
// reflects whether the request was made, not what the server thought of it.
func Fetch(ctx context.Context, requester Requester, request FetchRequest) (FetchResult, error) {
	target, err := ParseTarget(request.URL)
	if err != nil {
		return FetchResult{}, err
	}

	method, err := ParseMethod(request.Method)
	if err != nil {
		return FetchResult{}, err
	}

	response, err := requester.Request(ctx, net.Request{
		Method:  method,
		URL:     target.URL,
		Headers: request.Headers,
		Body:    request.Body,
	})
	if err != nil {
		return FetchResult{}, err
	}

	return FetchResult{
		Method:          response.Method,
		URL:             response.URL,
		FinalURL:        response.FinalURL,
		StatusCode:      response.StatusCode,
		Status:          response.Status,
		Protocol:        response.Protocol,
		RequestHeaders:  maskHeaders(request.Headers, request.ShowSecrets),
		ResponseHeaders: maskHeaders(response.Headers, request.ShowSecrets),
		ContentLength:   response.ContentLength,
		Body:            response.Body,
		BodyBytes:       response.BodyBytes,
		BodyTruncated:   response.BodyTruncated,
		Timing:          response.Timing,
		Redirects:       response.Redirects,
		TLS:             response.TLS,
	}, nil
}
