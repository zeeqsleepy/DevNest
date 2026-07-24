package encoding

import (
	"encoding/hex"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// HexEncodeRequest describes one hex encoding.
type HexEncodeRequest struct {
	Text string
	// Upper uses A-F rather than a-f. Both decode identically; some tools
	// print one and some the other, and a comparison against a value from
	// elsewhere is easier when the case matches.
	Upper bool
}

// HexEncodeResult is the encoded value.
type HexEncodeResult struct {
	Encoded     string `json:"encoded"`
	InputBytes  int    `json:"inputBytes"`
	OutputBytes int    `json:"outputBytes"`
}

// EncodeHex hex-encodes text.
//
// The input is treated as bytes. Go strings are already UTF-8, so text in any
// language encodes and round-trips without a character set to configure.
func EncodeHex(request HexEncodeRequest) (HexEncodeResult, error) {
	if request.Text == "" {
		return HexEncodeResult{}, errors.New(errors.CodeInvalidInput, "no text was given").
			WithHint("pass the text to encode, or use --stdin to read it from a pipe")
	}

	encoded := hex.EncodeToString([]byte(request.Text))
	if request.Upper {
		encoded = strings.ToUpper(encoded)
	}

	return HexEncodeResult{
		Encoded:     encoded,
		InputBytes:  len(request.Text),
		OutputBytes: len(encoded),
	}, nil
}

// HexDecodeRequest describes one hex decoding.
type HexDecodeRequest struct {
	Value string
}

// HexDecodeResult is the decoded value.
//
// Decoded holds the result when it is printable text. When it is not, Base64
// carries the bytes instead, because writing arbitrary bytes to a terminal can
// change how the terminal behaves.
type HexDecodeResult struct {
	Decoded   string `json:"decoded,omitempty"`
	Base64    string `json:"base64,omitempty"`
	Bytes     int    `json:"bytes"`
	Printable bool   `json:"printable"`
}

// DecodeHex decodes a hex value.
//
// Either case is accepted, as is a value split across lines or spaced a byte
// at a time, because that is how a hex dump arrives. A leading 0x is stripped:
// it is how every language writes a literal and nobody means it as data.
func DecodeHex(request HexDecodeRequest) (HexDecodeResult, error) {
	value := clean(request.Value)
	if value == "" {
		return HexDecodeResult{}, errors.New(errors.CodeInvalidInput, "no value was given").
			WithHint("pass the value to decode, or use --stdin to read it from a pipe")
	}

	value = strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")

	if len(value)%2 == 1 {
		return HexDecodeResult{}, errors.New(errors.CodeInvalidInput,
			"a hex value has an even number of digits; this one has %d", len(value)).
			WithHint("a digit is probably missing from the start or the end")
	}

	decoded, err := hex.DecodeString(value)
	if err != nil {
		return HexDecodeResult{}, errors.Wrap(err, errors.CodeInvalidInput,
			"the value is not valid hex").
			WithHint("hex digits are 0-9 and a-f; separators and 0x are ignored")
	}

	text, base64Bytes, isText := carry(decoded)
	return HexDecodeResult{
		Decoded:   text,
		Base64:    base64Bytes,
		Bytes:     len(decoded),
		Printable: isText,
	}, nil
}

// clean removes the separators a hex value picks up on its way through a dump,
// a log line, or a wrapped email.
func clean(value string) string {
	return strings.NewReplacer(
		"\n", "", "\r", "", "\t", "", " ", "", ":", "", "-", "",
	).Replace(strings.TrimSpace(value))
}
