package security

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"unicode/utf8"

	"github.com/devnest/devnest/internal/errors"
)

// Base64 alphabets.
const (
	AlphabetStandard = "standard"
	AlphabetURL      = "url-safe"
)

// EncodeRequest describes one Base64 encoding.
type EncodeRequest struct {
	Text string
	// URLSafe uses the alphabet with - and _ instead of + and /.
	URLSafe bool
	// NoPadding leaves off the trailing = characters.
	NoPadding bool
}

// EncodeResult is the encoded value.
type EncodeResult struct {
	Encoded     string `json:"encoded"`
	Alphabet    string `json:"alphabet"`
	Padded      bool   `json:"padded"`
	InputBytes  int    `json:"inputBytes"`
	OutputBytes int    `json:"outputBytes"`
}

// Encode Base64-encodes text.
//
// The input is treated as bytes. Go strings are already UTF-8, so text in any
// language encodes and round-trips without anything extra; there is no
// character set to configure and no place for one to be got wrong.
func Encode(request EncodeRequest) (EncodeResult, error) {
	encoding := encodingFor(request.URLSafe, request.NoPadding)

	encoded := encoding.EncodeToString([]byte(request.Text))

	return EncodeResult{
		Encoded:     encoded,
		Alphabet:    alphabetName(request.URLSafe),
		Padded:      !request.NoPadding,
		InputBytes:  len(request.Text),
		OutputBytes: len(encoded),
	}, nil
}

// DecodeRequest describes one Base64 decoding.
type DecodeRequest struct {
	Value string
}

// DecodeResult is the decoded value.
//
// Printable says whether the result is text a terminal can show. When it is
// not, Decoded is left empty and Hex carries the bytes instead: writing
// arbitrary binary to a terminal can change its state, move the cursor, or
// emit escape sequences the user never asked for, and a decode command is
// exactly where untrusted bytes arrive.
type DecodeResult struct {
	Decoded   string `json:"decoded,omitempty"`
	Hex       string `json:"hex,omitempty"`
	Bytes     int    `json:"bytes"`
	Alphabet  string `json:"alphabet"`
	Printable bool   `json:"printable"`
}

// Decode Base64-decodes a value.
//
// Both alphabets are accepted and padding is optional, because a value pasted
// out of a JWT, a URL, or a configuration file may be in any of those shapes
// and asking the user which one they have is asking them to know something the
// program can work out.
func Decode(request DecodeRequest) (DecodeResult, error) {
	value := strings.TrimSpace(request.Value)
	if value == "" {
		return DecodeResult{}, errors.New(errors.CodeInvalidInput, "no value was given").
			WithHint("pass the value to decode, or use --stdin to read it from a pipe")
	}

	// Whitespace inside the value is common when it was wrapped across lines.
	value = strings.NewReplacer("\n", "", "\r", "", " ", "", "\t", "").Replace(value)

	decoded, alphabet, err := decodeAny(value)
	if err != nil {
		return DecodeResult{}, err
	}

	result := DecodeResult{
		Bytes:    len(decoded),
		Alphabet: alphabet,
	}

	if printable(decoded) {
		result.Printable = true
		result.Decoded = string(decoded)
	} else {
		result.Hex = hex.EncodeToString(decoded)
	}

	return result, nil
}

// decodeAny tries the alphabets and padding combinations a value might be in.
func decodeAny(value string) ([]byte, string, error) {
	attempts := []struct {
		encoding *base64.Encoding
		alphabet string
	}{
		{base64.StdEncoding, AlphabetStandard},
		{base64.RawStdEncoding, AlphabetStandard},
		{base64.URLEncoding, AlphabetURL},
		{base64.RawURLEncoding, AlphabetURL},
	}

	for _, attempt := range attempts {
		if decoded, err := attempt.encoding.DecodeString(value); err == nil {
			return decoded, attempt.alphabet, nil
		}
	}

	return nil, "", errors.New(errors.CodeInvalidInput, "the value is not valid Base64").
		WithHint("check for missing characters; both the standard and URL-safe " +
			"alphabets are accepted, with or without padding")
}

// printable reports whether the bytes are safe to write to a terminal.
//
// Valid UTF-8 is not enough on its own: an escape character is perfectly valid
// UTF-8 and can repaint the screen. Tab, newline, and carriage return are
// allowed because text legitimately contains them.
func printable(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}

	for _, character := range string(data) {
		switch character {
		case '\t', '\n', '\r':
			continue
		}
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func encodingFor(urlSafe, noPadding bool) *base64.Encoding {
	switch {
	case urlSafe && noPadding:
		return base64.RawURLEncoding
	case urlSafe:
		return base64.URLEncoding
	case noPadding:
		return base64.RawStdEncoding
	default:
		return base64.StdEncoding
	}
}

func alphabetName(urlSafe bool) string {
	if urlSafe {
		return AlphabetURL
	}
	return AlphabetStandard
}
