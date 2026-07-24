package security

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestEncodeMatchesTheStandardLibrary(t *testing.T) {
	inputs := []string{
		"hello world",
		"",
		"a",
		"ab",
		"abc",
		"the quick brown fox jumps over the lazy dog",
	}

	for _, input := range inputs {
		result, err := Encode(EncodeRequest{Text: input})
		if err != nil {
			t.Fatalf("Encode(%q): %v", input, err)
		}
		if want := base64.StdEncoding.EncodeToString([]byte(input)); result.Encoded != want {
			t.Errorf("Encode(%q) = %q, want %q", input, result.Encoded, want)
		}
	}
}

// Go strings are UTF-8, so text in any language round-trips with nothing to
// configure.
func TestEncodeHandlesNonASCII(t *testing.T) {
	inputs := []string{
		"selamat pagi",
		"日本語のテキスト",
		"Ç'kemi, si je?",
		"emoji: \U0001F510",
		"mixed: café + 日本 + ñ",
	}

	for _, input := range inputs {
		encoded, err := Encode(EncodeRequest{Text: input})
		if err != nil {
			t.Fatalf("Encode(%q): %v", input, err)
		}

		decoded, err := Decode(DecodeRequest{Value: encoded.Encoded})
		if err != nil {
			t.Fatalf("Decode(%q): %v", encoded.Encoded, err)
		}
		if decoded.Decoded != input {
			t.Errorf("round trip of %q produced %q", input, decoded.Decoded)
		}
		if !decoded.Printable {
			t.Errorf("%q was reported as non-printable", input)
		}
	}
}

func TestEncodeAlphabets(t *testing.T) {
	// This input produces + and / in the standard alphabet.
	const input = "\xfb\xff\xbf"

	standard, err := Encode(EncodeRequest{Text: input})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.ContainsAny(standard.Encoded, "+/") {
		t.Fatalf("standard encoding = %q, expected + or /", standard.Encoded)
	}
	if standard.Alphabet != AlphabetStandard {
		t.Errorf("Alphabet = %q", standard.Alphabet)
	}

	urlSafe, err := Encode(EncodeRequest{Text: input, URLSafe: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.ContainsAny(urlSafe.Encoded, "+/") {
		t.Errorf("url-safe encoding = %q, still contains + or /", urlSafe.Encoded)
	}
	if urlSafe.Alphabet != AlphabetURL {
		t.Errorf("Alphabet = %q", urlSafe.Alphabet)
	}
}

func TestEncodePadding(t *testing.T) {
	padded, err := Encode(EncodeRequest{Text: "a"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.HasSuffix(padded.Encoded, "==") {
		t.Errorf("encoded = %q, want padding", padded.Encoded)
	}
	if !padded.Padded {
		t.Error("Padded = false")
	}

	unpadded, err := Encode(EncodeRequest{Text: "a", NoPadding: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(unpadded.Encoded, "=") {
		t.Errorf("encoded = %q, want no padding", unpadded.Encoded)
	}
}

func TestEncodeReportsSizes(t *testing.T) {
	result, err := Encode(EncodeRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if result.InputBytes != 5 {
		t.Errorf("InputBytes = %d, want 5", result.InputBytes)
	}
	if result.OutputBytes != len(result.Encoded) {
		t.Errorf("OutputBytes = %d, want %d", result.OutputBytes, len(result.Encoded))
	}
}

// A value pasted from a JWT, a URL, or a config file may be in any of these
// shapes, and asking the user which one is asking them to know something the
// program can work out.
func TestDecodeAcceptsEveryShape(t *testing.T) {
	const plain = "hello world?"

	variants := []string{
		base64.StdEncoding.EncodeToString([]byte(plain)),
		base64.RawStdEncoding.EncodeToString([]byte(plain)),
		base64.URLEncoding.EncodeToString([]byte(plain)),
		base64.RawURLEncoding.EncodeToString([]byte(plain)),
	}

	for _, variant := range variants {
		result, err := Decode(DecodeRequest{Value: variant})
		if err != nil {
			t.Fatalf("Decode(%q): %v", variant, err)
		}
		if result.Decoded != plain {
			t.Errorf("Decode(%q) = %q, want %q", variant, result.Decoded, plain)
		}
	}
}

func TestDecodeIgnoresWrappingWhitespace(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("a longer value that got wrapped"))
	wrapped := encoded[:8] + "\n" + encoded[8:16] + "\r\n  " + encoded[16:]

	result, err := Decode(DecodeRequest{Value: wrapped})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if result.Decoded != "a longer value that got wrapped" {
		t.Errorf("Decode = %q", result.Decoded)
	}
}

func TestDecodeRejectsInvalidInput(t *testing.T) {
	for _, input := range []string{"not base64!!!", "%%%%", "a===="} {
		t.Run(input, func(t *testing.T) {
			_, err := Decode(DecodeRequest{Value: input})
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestDecodeRejectsAnEmptyValue(t *testing.T) {
	for _, input := range []string{"", "   ", "\n"} {
		_, err := Decode(DecodeRequest{Value: input})
		assertCode(t, err, errors.CodeInvalidInput)
	}
}

// Arbitrary bytes can carry escape sequences that repaint a terminal, and a
// decode command is exactly where untrusted bytes arrive.
func TestDecodeWithholdsNonPrintableBytes(t *testing.T) {
	binary := []byte{0x00, 0x01, 0x02, 0xff, 0x1b, 0x5b, 0x33, 0x31, 0x6d}
	encoded := base64.StdEncoding.EncodeToString(binary)

	result, err := Decode(DecodeRequest{Value: encoded})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if result.Printable {
		t.Error("Printable = true for binary data")
	}
	if result.Decoded != "" {
		t.Errorf("Decoded = %q, want it withheld", result.Decoded)
	}
	if result.Hex == "" {
		t.Error("Hex is empty; the bytes should still be available")
	}
	if result.Bytes != len(binary) {
		t.Errorf("Bytes = %d, want %d", result.Bytes, len(binary))
	}
}

// An escape character is valid UTF-8 and can still repaint a terminal, so
// UTF-8 validity alone is not the test.
func TestDecodeTreatsEscapeSequencesAsNonPrintable(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("safe\x1b[31mred"))

	result, err := Decode(DecodeRequest{Value: encoded})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if result.Printable {
		t.Error("Printable = true for a value containing an escape character")
	}
}

// Tabs and newlines legitimately appear in text.
func TestDecodeAllowsOrdinaryWhitespace(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("line one\nline two\tindented\r\n"))

	result, err := Decode(DecodeRequest{Value: encoded})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !result.Printable {
		t.Errorf("Printable = false for ordinary text with whitespace: %q", result.Decoded)
	}
}

func TestDecodeReportsTheAlphabet(t *testing.T) {
	urlSafe := base64.RawURLEncoding.EncodeToString([]byte{0xfb, 0xff, 0xbf})

	result, err := Decode(DecodeRequest{Value: urlSafe})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if result.Alphabet == "" {
		t.Error("Alphabet is empty")
	}
}

func TestEncodeDecodeRoundTripOnBinary(t *testing.T) {
	original := make([]byte, 256)
	for index := range original {
		original[index] = byte(index)
	}

	encoded, err := Encode(EncodeRequest{Text: string(original)})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := Decode(DecodeRequest{Value: encoded.Encoded})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Bytes != len(original) {
		t.Errorf("round trip changed the length: %d, want %d", decoded.Bytes, len(original))
	}
	if decoded.Printable {
		t.Error("every byte value should not be reported as printable text")
	}
}

func TestPrintable(t *testing.T) {
	printableInputs := [][]byte{
		[]byte("hello"),
		[]byte("with\ttab"),
		[]byte("with\nnewline"),
		[]byte("日本語"),
		{},
	}
	for _, input := range printableInputs {
		if !printable(input) {
			t.Errorf("printable(%q) = false, want true", input)
		}
	}

	unprintableInputs := [][]byte{
		{0x00},
		{0x1b},
		{0x7f},
		{0xff, 0xfe},
	}
	for _, input := range unprintableInputs {
		if printable(input) {
			t.Errorf("printable(%x) = true, want false", input)
		}
	}
}
