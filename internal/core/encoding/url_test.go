package encoding

import (
	"testing"

	"github.com/devnest/devnest/internal/errors"
)

func TestEncodeURLQueryAndPathDifferOnSpaces(t *testing.T) {
	query, err := EncodeURL(URLEncodeRequest{Text: "a b&c=d"})
	if err != nil {
		t.Fatalf("EncodeURL: %v", err)
	}
	if query.Encoded != "a+b%26c%3Dd" {
		t.Errorf("query encoding = %q, want %q", query.Encoded, "a+b%26c%3Dd")
	}
	if query.Mode != ModeQuery {
		t.Errorf("mode = %q, want %q", query.Mode, ModeQuery)
	}

	path, err := EncodeURL(URLEncodeRequest{Text: "a b&c=d", Path: true})
	if err != nil {
		t.Fatalf("EncodeURL: %v", err)
	}
	if path.Encoded != "a%20b&c=d" {
		t.Errorf("path encoding = %q, want %q", path.Encoded, "a%20b&c=d")
	}
	if path.Mode != ModePath {
		t.Errorf("mode = %q, want %q", path.Mode, ModePath)
	}
}

func TestDecodeURLReadsPlusAccordingToTheMode(t *testing.T) {
	query, err := DecodeURL(URLDecodeRequest{Value: "one+two%21"})
	if err != nil {
		t.Fatalf("DecodeURL: %v", err)
	}
	if query.Decoded != "one two!" {
		t.Errorf("query decoding = %q, want %q", query.Decoded, "one two!")
	}

	path, err := DecodeURL(URLDecodeRequest{Value: "one+two%21", Path: true})
	if err != nil {
		t.Fatalf("DecodeURL: %v", err)
	}
	if path.Decoded != "one+two!" {
		t.Errorf("path decoding = %q, want the plus left alone", path.Decoded)
	}
}

func TestURLRoundTrips(t *testing.T) {
	const text = "kucing/makan?ikan=segar&waktu=petang petang"

	encoded, err := EncodeURL(URLEncodeRequest{Text: text})
	if err != nil {
		t.Fatalf("EncodeURL: %v", err)
	}

	decoded, err := DecodeURL(URLDecodeRequest{Value: encoded.Encoded})
	if err != nil {
		t.Fatalf("DecodeURL: %v", err)
	}
	if decoded.Decoded != text {
		t.Errorf("round trip = %q, want %q", decoded.Decoded, text)
	}
}

func TestURLRejectsBadInput(t *testing.T) {
	if _, err := EncodeURL(URLEncodeRequest{}); err == nil {
		t.Error("EncodeURL accepted an empty input")
	} else {
		assertCode(t, err, errors.CodeInvalidInput)
	}

	if _, err := DecodeURL(URLDecodeRequest{}); err == nil {
		t.Error("DecodeURL accepted an empty input")
	} else {
		assertCode(t, err, errors.CodeInvalidInput)
	}

	_, err := DecodeURL(URLDecodeRequest{Value: "%zz"})
	assertCode(t, err, errors.CodeInvalidInput)
}
