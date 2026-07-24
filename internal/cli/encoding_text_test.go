package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/core/encoding"
)

func TestHexDecodeTextPrintsTextWhenItIsText(t *testing.T) {
	result := encoding.HexDecodeResult{Decoded: "hello", Bytes: 5, Printable: true}

	if got := render(t, hexDecodeText(result)); got != "hello\n" {
		t.Errorf("output = %q, want the decoded text alone", got)
	}
}

// Arbitrary bytes must never reach the terminal by default: an escape sequence
// in a decoded value can repaint the screen of whoever ran the command.
func TestHexDecodeTextWithholdsBytesThatAreNotText(t *testing.T) {
	result := encoding.HexDecodeResult{Base64: "ABtbMko=", Bytes: 5}

	got := render(t, hexDecodeText(result))
	if !strings.Contains(got, "ABtbMko=") {
		t.Errorf("output = %q, want the Base64 form", got)
	}
	if !strings.Contains(got, "not printable text") {
		t.Errorf("output = %q, want it to say why", got)
	}
}

func jwtResult(claims encoding.JWTClaims) encoding.JWTResult {
	return encoding.JWTResult{
		Algorithm:      "HS256",
		Type:           "JWT",
		KeyID:          "key-1",
		Header:         json.RawMessage(`{"alg":"HS256"}`),
		Payload:        json.RawMessage(`{"sub":"ana"}`),
		Claims:         claims,
		SignatureBytes: 32,
	}
}

func TestJWTTextAlwaysSaysTheSignatureIsUnverified(t *testing.T) {
	got := render(t, jwtText(jwtResult(encoding.JWTClaims{Subject: "ana"})))

	if !strings.Contains(got, "NOT verified") {
		t.Errorf("output = %q, want the signature marked unverified", got)
	}
	for _, want := range []string{"HS256", "key-1", "subject", "ana"} {
		if !strings.Contains(got, want) {
			t.Errorf("output = %q, want it to contain %q", got, want)
		}
	}
}

func TestJWTTextPrintsBothSegments(t *testing.T) {
	got := render(t, jwtText(jwtResult(encoding.JWTClaims{})))

	if !strings.Contains(got, "header\n{\n  \"alg\": \"HS256\"\n}") {
		t.Errorf("output = %q, want an indented header", got)
	}
	if !strings.Contains(got, "payload\n{\n  \"sub\": \"ana\"\n}") {
		t.Errorf("output = %q, want an indented payload", got)
	}
}

func TestJWTTextSeparatesExpiredFromNeverExpiring(t *testing.T) {
	moment := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

	expired := render(t, jwtText(jwtResult(encoding.JWTClaims{
		ExpiresAt:          &moment,
		HasExpiry:          true,
		Expired:            true,
		SecondsUntilExpiry: -7200,
	})))
	if !strings.Contains(expired, "EXPIRED 2h ago") {
		t.Errorf("output = %q, want it to say how long ago", expired)
	}

	valid := render(t, jwtText(jwtResult(encoding.JWTClaims{
		ExpiresAt:          &moment,
		HasExpiry:          true,
		SecondsUntilExpiry: 3 * 86400,
	})))
	if !strings.Contains(valid, "(in 3d)") {
		t.Errorf("output = %q, want the time left", valid)
	}

	forever := render(t, jwtText(jwtResult(encoding.JWTClaims{})))
	if !strings.Contains(forever, "never: no exp claim") {
		t.Errorf("output = %q, want the missing claim distinguished from a valid one", forever)
	}
}

func TestSinceUsesTheLargestReadableUnit(t *testing.T) {
	cases := map[int64]string{
		0:      "0s",
		45:     "45s",
		90:     "1m",
		7200:   "2h",
		200000: "2d",
		-5:     "0s",
	}

	for seconds, want := range cases {
		if got := since(seconds); got != want {
			t.Errorf("since(%d) = %q, want %q", seconds, got, want)
		}
	}
}
