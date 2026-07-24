package encoding

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// JWTRequest describes one token inspection.
type JWTRequest struct {
	Token string
	// Now is the moment expiry is judged against. The zero value means the
	// current time; a test supplies a fixed one, which is the only way an
	// expiry test can be deterministic.
	Now time.Time
}

// JWTResult is everything a token says about itself.
//
// Header and Payload are the decoded segments as they were written, so key
// order and formatting survive into the output. The named fields alongside
// them are the claims worth acting on, parsed once here rather than by every
// consumer.
type JWTResult struct {
	Algorithm string          `json:"algorithm"`
	Type      string          `json:"type,omitempty"`
	KeyID     string          `json:"keyId,omitempty"`
	Header    json.RawMessage `json:"header"`
	Payload   json.RawMessage `json:"payload"`
	Claims    JWTClaims       `json:"claims"`

	// SignatureBytes is the length of the third segment once decoded. A
	// length of zero means the token carries no signature at all.
	SignatureBytes int `json:"signatureBytes"`

	// SignatureVerified is always false, and is a field rather than a note in
	// the documentation because a consumer reading this output should have to
	// see it. DevNest has no key, so it cannot verify anything.
	SignatureVerified bool `json:"signatureVerified"`

	// Unsecured reports the alg=none token, which carries no signature and
	// which any correct verifier rejects.
	Unsecured bool `json:"unsecured"`
}

// JWTClaims are the registered claims, decoded.
type JWTClaims struct {
	Issuer   string   `json:"issuer,omitempty"`
	Subject  string   `json:"subject,omitempty"`
	Audience []string `json:"audience,omitempty"`
	ID       string   `json:"id,omitempty"`

	IssuedAt  *time.Time `json:"issuedAt,omitempty"`
	NotBefore *time.Time `json:"notBefore,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`

	// HasExpiry separates "does not expire" from "expiry unknown". A token
	// without an exp claim is a different problem from one that expired, and
	// reporting both as expired=false would hide it.
	HasExpiry bool `json:"hasExpiry"`
	Expired   bool `json:"expired"`
	// NotYetValid is a token whose nbf claim is in the future, which is what a
	// clock skew problem looks like from here.
	NotYetValid bool `json:"notYetValid"`
	// SecondsUntilExpiry is negative once the token has expired.
	SecondsUntilExpiry int64 `json:"secondsUntilExpiry,omitempty"`
}

// DecodeJWT decodes the header and payload of a JSON Web Token.
//
// It never verifies the signature. Verification needs the signing key, a
// policy for which algorithms are acceptable, and a decision about audience
// and issuer; a tool that checks the shape of a signature without any of that
// teaches people to trust a result that means nothing. What this does instead
// is answer the questions that actually come up while debugging: which key
// signed it, who it is for, and whether it has expired.
func DecodeJWT(request JWTRequest) (JWTResult, error) {
	token := strings.TrimSpace(request.Token)
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimSpace(token)

	if token == "" {
		return JWTResult{}, errors.New(errors.CodeInvalidInput, "no token was given").
			WithHint("pass the token, or use --stdin to read it from a pipe")
	}

	segments := strings.Split(token, ".")
	if len(segments) == 5 {
		return JWTResult{}, errors.New(errors.CodeUnsupported,
			"this is a JWE: an encrypted token, not a signed one").
			WithHint("its contents cannot be read without the decryption key")
	}
	if len(segments) != 3 {
		return JWTResult{}, errors.New(errors.CodeInvalidInput,
			"a JWT has three dot-separated segments; this has %d", len(segments)).
			WithHint("check that the whole token was copied, including both dots")
	}

	header, err := decodeSegment(segments[0], "header")
	if err != nil {
		return JWTResult{}, err
	}
	payload, err := decodeSegment(segments[1], "payload")
	if err != nil {
		return JWTResult{}, err
	}

	signature, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segments[2], "="))
	if err != nil {
		return JWTResult{}, errors.Wrap(err, errors.CodeInvalidInput,
			"the signature segment is not valid Base64url")
	}

	var head struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
		KeyID     string `json:"kid"`
	}
	if err := json.Unmarshal(header, &head); err != nil {
		return JWTResult{}, errors.Wrap(err, errors.CodeParse,
			"the header is not a JSON object")
	}

	claims, err := decodeClaims(payload, request.now())
	if err != nil {
		return JWTResult{}, err
	}

	return JWTResult{
		Algorithm:         head.Algorithm,
		Type:              head.Type,
		KeyID:             head.KeyID,
		Header:            header,
		Payload:           payload,
		Claims:            claims,
		SignatureBytes:    len(signature),
		SignatureVerified: false,
		Unsecured:         strings.EqualFold(head.Algorithm, "none"),
	}, nil
}

func (r JWTRequest) now() time.Time {
	if r.Now.IsZero() {
		return time.Now()
	}
	return r.Now
}

// decodeSegment decodes one Base64url segment and checks that it is JSON.
//
// Padding is tolerated. A token is supposed to carry none, but tokens arrive
// through configuration files and copy-and-paste, and rejecting one over a
// trailing equals sign helps nobody.
func decodeSegment(segment, name string) (json.RawMessage, error) {
	if segment == "" {
		return nil, errors.New(errors.CodeInvalidInput, "the %s segment is empty", name)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segment, "="))
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput,
			"the %s segment is not valid Base64url", name).
			WithHint("a JWT uses the URL-safe alphabet; check that nothing was cut off")
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, decoded); err != nil {
		return nil, errors.Wrap(err, errors.CodeParse,
			"the %s segment did not decode to JSON", name).
			WithHint("this may not be a JWT")
	}

	return json.RawMessage(compact.Bytes()), nil
}

// decodeClaims reads the registered claims from a payload.
//
// Every field is optional: a token is free to carry none of them, and plenty
// carry only the ones its own service invented. Unknown claims are not lost,
// because the payload itself is part of the result.
func decodeClaims(payload json.RawMessage, now time.Time) (JWTClaims, error) {
	var raw struct {
		Issuer    string   `json:"iss"`
		Subject   string   `json:"sub"`
		Audience  audience `json:"aud"`
		ID        string   `json:"jti"`
		IssuedAt  *float64 `json:"iat"`
		NotBefore *float64 `json:"nbf"`
		ExpiresAt *float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return JWTClaims{}, errors.Wrap(err, errors.CodeParse,
			"the payload is not a JSON object DevNest can read").
			WithHint("registered claims have known types; iss, sub, and jti are strings, " +
				"exp, nbf, and iat are seconds since the epoch")
	}

	claims := JWTClaims{
		Issuer:    raw.Issuer,
		Subject:   raw.Subject,
		Audience:  raw.Audience,
		ID:        raw.ID,
		IssuedAt:  epoch(raw.IssuedAt),
		NotBefore: epoch(raw.NotBefore),
		ExpiresAt: epoch(raw.ExpiresAt),
	}

	if claims.ExpiresAt != nil {
		claims.HasExpiry = true
		claims.Expired = now.After(*claims.ExpiresAt)
		claims.SecondsUntilExpiry = int64(claims.ExpiresAt.Sub(now).Seconds())
	}
	if claims.NotBefore != nil {
		claims.NotYetValid = now.Before(*claims.NotBefore)
	}

	return claims, nil
}

// epoch turns a numeric claim into a time in UTC.
//
// The claim is seconds since the epoch and is allowed to be fractional, which
// is why it is read as a float rather than an integer.
func epoch(seconds *float64) *time.Time {
	if seconds == nil {
		return nil
	}
	whole, fraction := int64(*seconds), *seconds-float64(int64(*seconds))
	moment := time.Unix(whole, int64(fraction*float64(time.Second))).UTC()
	return &moment
}

// audience is the aud claim, which the specification allows to be either one
// string or an array of them. Both shapes appear in the wild and a decoder
// that handles only one fails on real tokens.
type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = []string{single}
		return nil
	}

	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*a = many
	return nil
}
