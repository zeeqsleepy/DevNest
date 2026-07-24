package encoding

import (
	"strings"
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

// assertCode fails the test unless err carries the expected classification.
func assertCode(t *testing.T, err error, want errors.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %q, got nil", want)
	}
	if got := errors.CodeOf(err); got != want {
		t.Fatalf("code = %q, want %q (%v)", got, want, err)
	}
}

func TestEncodeHex(t *testing.T) {
	result, err := EncodeHex(HexEncodeRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("EncodeHex: %v", err)
	}

	if result.Encoded != "68656c6c6f" {
		t.Errorf("encoded = %q, want %q", result.Encoded, "68656c6c6f")
	}
	if result.InputBytes != 5 || result.OutputBytes != 10 {
		t.Errorf("sizes = %d/%d, want 5/10", result.InputBytes, result.OutputBytes)
	}
}

func TestEncodeHexUppercase(t *testing.T) {
	result, err := EncodeHex(HexEncodeRequest{Text: "hello", Upper: true})
	if err != nil {
		t.Fatalf("EncodeHex: %v", err)
	}
	if result.Encoded != "68656C6C6F" {
		t.Errorf("encoded = %q, want uppercase digits", result.Encoded)
	}
}

func TestEncodeHexRejectsEmptyInput(t *testing.T) {
	_, err := EncodeHex(HexEncodeRequest{})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestDecodeHexAcceptsTheShapesAValueArrivesIn(t *testing.T) {
	cases := map[string]string{
		"lowercase":  "68656c6c6f",
		"uppercase":  "68656C6C6F",
		"prefixed":   "0x68656c6c6f",
		"spaced":     "68 65 6c 6c 6f",
		"colons":     "68:65:6c:6c:6f",
		"dashes":     "68-65-6c-6c-6f",
		"wrapped":    "68656c\n6c6f",
		"surrounded": "  68656c6c6f  ",
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := DecodeHex(HexDecodeRequest{Value: value})
			if err != nil {
				t.Fatalf("DecodeHex(%q): %v", value, err)
			}
			if result.Decoded != "hello" {
				t.Errorf("decoded = %q, want %q", result.Decoded, "hello")
			}
			if !result.Printable || result.Bytes != 5 {
				t.Errorf("printable = %v, bytes = %d, want true/5", result.Printable, result.Bytes)
			}
		})
	}
}

func TestDecodeHexCarriesNonPrintableBytesAsBase64(t *testing.T) {
	result, err := DecodeHex(HexDecodeRequest{Value: "001b5b324a"})
	if err != nil {
		t.Fatalf("DecodeHex: %v", err)
	}

	if result.Printable {
		t.Fatal("bytes containing an escape character were reported as printable")
	}
	if result.Decoded != "" {
		t.Errorf("decoded = %q, want it left empty for bytes a terminal should not be given",
			result.Decoded)
	}
	if result.Base64 == "" {
		t.Error("base64 is empty; the bytes have nowhere to go")
	}
}

func TestDecodeHexRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty":      "",
		"separators": ": :",
		"odd length": "68656c6c6",
		"not hex":    "68zz6c6c6f",
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeHex(HexDecodeRequest{Value: value})
			assertCode(t, err, errors.CodeInvalidInput)
		})
	}
}

func TestHexRoundTrips(t *testing.T) {
	const text = "kucing makan ikan — ünïcödé"

	encoded, err := EncodeHex(HexEncodeRequest{Text: text})
	if err != nil {
		t.Fatalf("EncodeHex: %v", err)
	}

	decoded, err := DecodeHex(HexDecodeRequest{Value: encoded.Encoded})
	if err != nil {
		t.Fatalf("DecodeHex: %v", err)
	}
	if decoded.Decoded != text {
		t.Errorf("round trip = %q, want %q", decoded.Decoded, text)
	}
}

func TestDecodeHexHintNamesTheProblem(t *testing.T) {
	_, err := DecodeHex(HexDecodeRequest{Value: "abc"})

	var typed *errors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error is not a DevNest error: %v", err)
	}
	if !strings.Contains(typed.Message, "even number") {
		t.Errorf("message = %q, want it to say what is wrong with the length", typed.Message)
	}
}
