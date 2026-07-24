package network

import (
	"context"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

func TestFetchReportsTheExchange(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 55)}}

	result, err := Fetch(context.Background(), requester, FetchRequest{URL: "example.com"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d", result.StatusCode)
	}
	if result.Timing.TotalMs != 55 {
		t.Errorf("TotalMs = %d, want 55", result.Timing.TotalMs)
	}
	if len(result.ResponseHeaders) == 0 {
		t.Error("no response headers were reported")
	}
}

// Inspecting a 404 or a 500 is an ordinary reason to run this command, so a
// non-2xx status must not be an error.
func TestFetchDoesNotFailOnAnErrorStatus(t *testing.T) {
	for _, status := range []int{404, 500} {
		requester := &fakeRequester{responses: []net.Response{okResponse(status, 10)}}

		result, err := Fetch(context.Background(), requester, FetchRequest{URL: "example.com"})
		if err != nil {
			t.Fatalf("Fetch on %d: %v", status, err)
		}
		if result.StatusCode != status {
			t.Errorf("StatusCode = %d, want %d", result.StatusCode, status)
		}
	}
}

// A report gets attached to a ticket and a ticket gets shared. Masking has to
// happen in the result, not in one output path.
func TestFetchMasksCredentialHeadersByDefault(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	result, err := Fetch(context.Background(), requester, FetchRequest{
		URL: "example.com",
		Headers: []net.Header{
			{Name: "Authorization", Value: "Bearer sk-live-secret-value"},
			{Name: "Accept", Value: "application/json"},
		},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	for _, header := range result.RequestHeaders {
		if header.Name == "Authorization" && strings.Contains(header.Value, "sk-live") {
			t.Errorf("the request credential survived: %q", header.Value)
		}
		if header.Name == "Accept" && header.Value != "application/json" {
			t.Errorf("an ordinary request header was masked: %q", header.Value)
		}
	}
	for _, header := range result.ResponseHeaders {
		if strings.EqualFold(header.Name, "Set-Cookie") && strings.Contains(header.Value, "abcdef") {
			t.Errorf("the response cookie survived: %q", header.Value)
		}
	}
}

func TestFetchShowSecretsRevealsThem(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	result, err := Fetch(context.Background(), requester, FetchRequest{
		URL:         "example.com",
		Headers:     []net.Header{{Name: "Authorization", Value: "Bearer token"}},
		ShowSecrets: true,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	found := false
	for _, header := range result.RequestHeaders {
		if header.Name == "Authorization" && header.Value == "Bearer token" {
			found = true
		}
	}
	if !found {
		t.Error("--show-secrets did not show the value")
	}
}

// The request headers are reported because a request that behaved unexpectedly
// is usually one carrying something the user did not realise it carried.
func TestFetchReportsTheHeadersItSent(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	result, err := Fetch(context.Background(), requester, FetchRequest{
		URL:     "example.com",
		Headers: []net.Header{{Name: "Accept", Value: "text/plain"}},
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if len(result.RequestHeaders) != 1 || result.RequestHeaders[0].Name != "Accept" {
		t.Errorf("RequestHeaders = %+v", result.RequestHeaders)
	}
}

func TestFetchPassesTheBodyThrough(t *testing.T) {
	requester := &fakeRequester{responses: []net.Response{okResponse(200, 10)}}

	_, err := Fetch(context.Background(), requester, FetchRequest{
		URL:    "example.com",
		Method: "POST",
		Body:   []byte(`{"name":"x"}`),
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if requester.calls[0].Method != "POST" {
		t.Errorf("method = %q", requester.calls[0].Method)
	}
	if string(requester.calls[0].Body) != `{"name":"x"}` {
		t.Errorf("body = %q", requester.calls[0].Body)
	}
}

func TestFetchRejectsABadMethod(t *testing.T) {
	_, err := Fetch(context.Background(), &fakeRequester{}, FetchRequest{
		URL: "example.com", Method: "BREW",
	})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestFetchPropagatesATransportFailure(t *testing.T) {
	requester := &fakeRequester{
		errs: []error{errors.New(errors.CodeTimeout, "example.com did not respond in time")},
	}

	_, err := Fetch(context.Background(), requester, FetchRequest{URL: "example.com"})
	assertCode(t, err, errors.CodeTimeout)
}
