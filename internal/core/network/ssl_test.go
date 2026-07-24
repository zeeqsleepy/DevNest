package network

import (
	"context"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

func TestInspectReportsAValidCertificate(t *testing.T) {
	inspector := &fakeInspector{chain: certificateChain(200, true)}

	result, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	if result.Validity != ValidityValid || !result.Valid {
		t.Errorf("validity = %q, valid = %v", result.Validity, result.Valid)
	}
	if !result.Trusted {
		t.Error("Trusted = false for a verified chain")
	}
	if result.DaysRemaining < 199 || result.DaysRemaining > 200 {
		t.Errorf("DaysRemaining = %d, want about 200", result.DaysRemaining)
	}
	if result.Subject == "" || result.Issuer == "" {
		t.Error("the subject or issuer is missing")
	}
	if len(result.DNSNames) != 2 {
		t.Errorf("DNSNames = %v", result.DNSNames)
	}
}

// An expired certificate is a result, not an error. This is the command you
// run precisely when something is wrong with one.
func TestInspectReportsAnExpiredCertificate(t *testing.T) {
	inspector := &fakeInspector{chain: certificateChain(-5, false)}

	result, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	if err != nil {
		t.Fatalf("Inspect returned an error for an expired certificate: %v", err)
	}

	if result.Validity != ValidityExpired {
		t.Errorf("validity = %q, want %q", result.Validity, ValidityExpired)
	}
	if result.Valid {
		t.Error("Valid = true for an expired certificate")
	}
	if result.DaysRemaining >= 0 {
		t.Errorf("DaysRemaining = %d, want a negative number", result.DaysRemaining)
	}
}

func TestInspectReportsAnExpiringCertificate(t *testing.T) {
	inspector := &fakeInspector{chain: certificateChain(10, true)}

	result, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	if result.Validity != ValidityExpiringSoon {
		t.Errorf("validity = %q, want %q", result.Validity, ValidityExpiringSoon)
	}
	// Expiring soon is a warning, not a failure: the certificate still works.
	if !result.Valid {
		t.Error("Valid = false for a certificate that has not expired yet")
	}
}

func TestInspectWarningWindowIsConfigurable(t *testing.T) {
	inspector := &fakeInspector{chain: certificateChain(40, true)}

	relaxed, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if relaxed.Validity != ValidityValid {
		t.Errorf("validity = %q, want %q at the default window", relaxed.Validity, ValidityValid)
	}

	strict, err := Inspect(context.Background(), inspector, InspectRequest{
		Host: "example.com", ExpiryWarningDays: 60,
	})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if strict.Validity != ValidityExpiringSoon {
		t.Errorf("validity = %q, want %q at a 60 day window", strict.Validity, ValidityExpiringSoon)
	}
}

func TestInspectReportsAnUntrustedCertificate(t *testing.T) {
	inspector := &fakeInspector{chain: certificateChain(100, false)}

	result, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	if result.Validity != ValidityUntrusted {
		t.Errorf("validity = %q, want %q", result.Validity, ValidityUntrusted)
	}
	if result.Valid {
		t.Error("Valid = true for an untrusted certificate")
	}
	if result.TrustError == "" {
		t.Error("TrustError is empty; the user needs to know why it is not trusted")
	}
}

// Expired and untrusted at once is reported as expired: that is the actionable
// fact, and the trust failure is usually a consequence of it.
func TestInspectPrefersExpiredOverUntrusted(t *testing.T) {
	inspector := &fakeInspector{chain: certificateChain(-30, false)}

	result, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if result.Validity != ValidityExpired {
		t.Errorf("validity = %q, want %q", result.Validity, ValidityExpired)
	}
}

func TestInspectReportsACertificateThatIsNotYetValid(t *testing.T) {
	now := time.Now().UTC()
	chain := certificateChain(200, true)
	chain.Certificates[0].NotBefore = now.AddDate(0, 0, 10)

	result, err := Inspect(context.Background(), &fakeInspector{chain: chain},
		InspectRequest{Host: "example.com"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if result.Validity != ValidityNotYetValid {
		t.Errorf("validity = %q, want %q", result.Validity, ValidityNotYetValid)
	}
}

func TestInspectFullChainIsOptional(t *testing.T) {
	inspector := &fakeInspector{chain: certificateChain(100, true)}

	brief, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(brief.Chain) != 0 {
		t.Errorf("Chain = %v, want it omitted by default", brief.Chain)
	}

	full, err := Inspect(context.Background(), inspector, InspectRequest{
		Host: "example.com", FullChain: true,
	})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(full.Chain) == 0 {
		t.Error("--chain did not include the certificates")
	}
}

func TestInspectAcceptsAURLAsTheHost(t *testing.T) {
	inspector := &fakeInspector{chain: certificateChain(100, true)}

	result, err := Inspect(context.Background(), inspector, InspectRequest{
		Host: "https://example.com/path",
	})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if result.Host != "example.com" {
		t.Errorf("Host = %q, want example.com", result.Host)
	}
}

func TestInspectRejectsABadPort(t *testing.T) {
	for _, port := range []int{-1, 99999} {
		_, err := Inspect(context.Background(), &fakeInspector{}, InspectRequest{
			Host: "example.com", Port: port,
		})
		assertCode(t, err, errors.CodeInvalidInput)
	}
}

// A handshake that never happened is an error; there is nothing to report on.
func TestInspectPropagatesAHandshakeFailure(t *testing.T) {
	inspector := &fakeInspector{
		failure: errors.New(errors.CodeNetwork, "cannot connect to example.com:443"),
	}

	_, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	assertCode(t, err, errors.CodeNetwork)
}

func TestInspectRejectsAnEmptyChain(t *testing.T) {
	inspector := &fakeInspector{chain: net.Chain{Host: "example.com"}}

	_, err := Inspect(context.Background(), inspector, InspectRequest{Host: "example.com"})
	assertCode(t, err, errors.CodeNetwork)
}

func TestDaysUntilRoundsTowardsZero(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// Eleven hours left is zero whole days, which is the honest answer.
	if got := daysUntil(now, now.Add(11*time.Hour)); got != 0 {
		t.Errorf("daysUntil(11 hours) = %d, want 0", got)
	}
	if got := daysUntil(now, now.Add(49*time.Hour)); got != 2 {
		t.Errorf("daysUntil(49 hours) = %d, want 2", got)
	}
	if got := daysUntil(now, now.Add(-25*time.Hour)); got != -1 {
		t.Errorf("daysUntil(-25 hours) = %d, want -1", got)
	}
}
