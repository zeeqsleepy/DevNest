package encoding

import (
	"net/url"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Percent-encoding modes. They differ in one visible way and it is the one
// that catches people out: a space becomes + in a query value and %20 in a
// path segment.
const (
	ModeQuery = "query"
	ModePath  = "path"
)

// URLEncodeRequest describes one percent-encoding.
type URLEncodeRequest struct {
	Text string
	// Path encodes for a path segment rather than a query value.
	Path bool
}

// URLEncodeResult is the encoded value.
type URLEncodeResult struct {
	Encoded string `json:"encoded"`
	Mode    string `json:"mode"`
}

// EncodeURL percent-encodes text.
//
// The whole input is treated as one value, never as a URL. Encoding a complete
// URL would escape the colons and slashes that make it a URL, which is not
// what anybody wants; this escapes a value that is about to be put inside one.
func EncodeURL(request URLEncodeRequest) (URLEncodeResult, error) {
	if request.Text == "" {
		return URLEncodeResult{}, errors.New(errors.CodeInvalidInput, "no text was given").
			WithHint("pass the text to encode, or use --stdin to read it from a pipe")
	}

	if request.Path {
		return URLEncodeResult{Encoded: url.PathEscape(request.Text), Mode: ModePath}, nil
	}
	return URLEncodeResult{Encoded: url.QueryEscape(request.Text), Mode: ModeQuery}, nil
}

// URLDecodeRequest describes one percent-decoding.
type URLDecodeRequest struct {
	Value string
	// Path treats + as a literal plus rather than as a space.
	Path bool
}

// URLDecodeResult is the decoded value.
type URLDecodeResult struct {
	Decoded string `json:"decoded"`
	Mode    string `json:"mode"`
}

// DecodeURL percent-decodes a value.
//
// The default reads + as a space, which is what a query value means by it. Use
// Path when the value came from a path segment, where + is a plus sign and
// treating it as a space silently corrupts the result.
func DecodeURL(request URLDecodeRequest) (URLDecodeResult, error) {
	value := strings.TrimSpace(request.Value)
	if value == "" {
		return URLDecodeResult{}, errors.New(errors.CodeInvalidInput, "no value was given").
			WithHint("pass the value to decode, or use --stdin to read it from a pipe")
	}

	mode := ModeQuery
	decode := url.QueryUnescape
	if request.Path {
		mode = ModePath
		decode = url.PathUnescape
	}

	decoded, err := decode(value)
	if err != nil {
		return URLDecodeResult{}, errors.Wrap(err, errors.CodeInvalidInput,
			"the value is not valid percent-encoding").
			WithHint("a %% must be followed by two hex digits; a literal percent sign is %%25")
	}

	return URLDecodeResult{Decoded: decoded, Mode: mode}, nil
}
