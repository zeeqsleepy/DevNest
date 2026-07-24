package encoding

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// reference is the moment every expiry assertion is made against, so a test
// that passes today passes in a year.
var reference = time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)

// token assembles a JWT from two JSON segments and a signature that is never
// checked, which is the point: nothing here needs a real key.
func token(header, payload string) string {
	segment := func(text string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(text))
	}
	return segment(header) + "." + segment(payload) + "." +
		base64.RawURLEncoding.EncodeToString([]byte("not-a-real-signature"))
}

func TestDecodeJWTReadsHeaderAndClaims(t *testing.T) {
	value := token(
		`{"alg":"HS256","typ":"JWT","kid":"key-1"}`,
		`{"iss":"devnest","sub":"user-7","aud":"api","jti":"abc","iat":1750000000,"exp":1790000000}`,
	)

	result, err := DecodeJWT(JWTRequest{Token: value, Now: reference})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}

	if result.Algorithm != "HS256" || result.Type != "JWT" || result.KeyID != "key-1" {
		t.Errorf("header = %+v, want HS256/JWT/key-1", result)
	}
	if result.Claims.Issuer != "devnest" || result.Claims.Subject != "user-7" {
		t.Errorf("claims = %+v, want issuer devnest and subject user-7", result.Claims)
	}
	if len(result.Claims.Audience) != 1 || result.Claims.Audience[0] != "api" {
		t.Errorf("audience = %v, want [api]", result.Claims.Audience)
	}
	if result.SignatureBytes == 0 {
		t.Error("signature length is zero for a token that carries one")
	}
	if result.SignatureVerified {
		t.Fatal("the result claims the signature was verified; nothing here verifies anything")
	}
}

func TestDecodeJWTKeepsTheSegmentsAsWritten(t *testing.T) {
	value := token(`{"alg":"HS256"}`, `{"zeta":1,"alpha":2}`)

	result, err := DecodeJWT(JWTRequest{Token: value, Now: reference})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}

	// Key order survives because the payload is carried as raw JSON rather
	// than decoded into a map and re-encoded.
	if got := string(result.Payload); got != `{"zeta":1,"alpha":2}` {
		t.Errorf("payload = %s, want the original key order", got)
	}
}

func TestDecodeJWTJudgesExpiryAgainstTheGivenTime(t *testing.T) {
	past := reference.Add(-time.Hour).Unix()
	future := reference.Add(time.Hour).Unix()

	expired, err := DecodeJWT(JWTRequest{
		Token: token(`{"alg":"HS256"}`, `{"exp":`+itoa(past)+`}`),
		Now:   reference,
	})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}
	if !expired.Claims.Expired || !expired.Claims.HasExpiry {
		t.Errorf("claims = %+v, want an expired token", expired.Claims)
	}
	if expired.Claims.SecondsUntilExpiry >= 0 {
		t.Errorf("secondsUntilExpiry = %d, want it negative once expired",
			expired.Claims.SecondsUntilExpiry)
	}

	valid, err := DecodeJWT(JWTRequest{
		Token: token(`{"alg":"HS256"}`, `{"exp":`+itoa(future)+`}`),
		Now:   reference,
	})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}
	if valid.Claims.Expired {
		t.Error("a token expiring in an hour was reported as expired")
	}
	if valid.Claims.ExpiresAt == nil || !valid.Claims.ExpiresAt.Equal(time.Unix(future, 0).UTC()) {
		t.Errorf("expiresAt = %v, want %v", valid.Claims.ExpiresAt, time.Unix(future, 0).UTC())
	}
}

func TestDecodeJWTSeparatesNoExpiryFromNotExpired(t *testing.T) {
	result, err := DecodeJWT(JWTRequest{
		Token: token(`{"alg":"HS256"}`, `{"sub":"forever"}`),
		Now:   reference,
	})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}

	if result.Claims.HasExpiry {
		t.Error("hasExpiry is true for a token with no exp claim")
	}
	if result.Claims.Expired {
		t.Error("a token with no exp claim was reported as expired")
	}
}

func TestDecodeJWTReportsATokenNotYetValid(t *testing.T) {
	result, err := DecodeJWT(JWTRequest{
		Token: token(`{"alg":"HS256"}`, `{"nbf":`+itoa(reference.Add(time.Hour).Unix())+`}`),
		Now:   reference,
	})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}
	if !result.Claims.NotYetValid {
		t.Error("a token whose nbf is an hour away was not reported as not yet valid")
	}
}

func TestDecodeJWTAcceptsBothAudienceShapes(t *testing.T) {
	result, err := DecodeJWT(JWTRequest{
		Token: token(`{"alg":"HS256"}`, `{"aud":["api","admin"]}`),
		Now:   reference,
	})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}
	if len(result.Claims.Audience) != 2 {
		t.Errorf("audience = %v, want two entries", result.Claims.Audience)
	}
}

func TestDecodeJWTFlagsTheUnsecuredAlgorithm(t *testing.T) {
	result, err := DecodeJWT(JWTRequest{
		Token: token(`{"alg":"none"}`, `{"sub":"nobody"}`) + "",
		Now:   reference,
	})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}
	if !result.Unsecured {
		t.Error("alg=none was not reported as unsecured")
	}
}

func TestDecodeJWTToleratesPaddingAndABearerPrefix(t *testing.T) {
	value := "Bearer " + token(`{"alg":"HS256"}`, `{"sub":"padded"}`)

	result, err := DecodeJWT(JWTRequest{Token: value, Now: reference})
	if err != nil {
		t.Fatalf("DecodeJWT: %v", err)
	}
	if result.Claims.Subject != "padded" {
		t.Errorf("subject = %q, want padded", result.Claims.Subject)
	}
}

func TestDecodeJWTRejectsWhatIsNotAToken(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  errors.Code
	}{
		{"empty", "", errors.CodeInvalidInput},
		{"two segments", "aaaa.bbbb", errors.CodeInvalidInput},
		{"jwe", "a.b.c.d.e", errors.CodeUnsupported},
		{"bad base64", "!!!.bbbb.cccc", errors.CodeInvalidInput},
		{"empty header", ".bbbb.cccc", errors.CodeInvalidInput},
		{"not json", token("hello", "{}"), errors.CodeParse},
		{"claim types", token(`{"alg":"HS256"}`, `{"iss":5}`), errors.CodeParse},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := DecodeJWT(JWTRequest{Token: testCase.value, Now: reference})
			assertCode(t, err, testCase.want)
		})
	}
}

func TestDecodeJWTSaysWhichSegmentFailed(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))

	_, err := DecodeJWT(JWTRequest{Token: header + ".!!!!.cccc", Now: reference})

	var typed *errors.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error is not a DevNest error: %v", err)
	}
	if !strings.Contains(typed.Message, "payload") {
		t.Errorf("message = %q, want it to name the payload segment", typed.Message)
	}
}

// itoa keeps the token literals above readable.
func itoa(value int64) string { return strconv.FormatInt(value, 10) }
