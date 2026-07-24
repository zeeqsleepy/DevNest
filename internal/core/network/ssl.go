package network

import (
	"context"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

// InspectRequest describes one certificate inspection.
type InspectRequest struct {
	Host string
	Port int
	// ExpiryWarningDays marks a certificate as expiring soon when it has
	// fewer days left than this. Zero uses the default of 30.
	ExpiryWarningDays int
	// FullChain reports every certificate served, not only the leaf.
	FullChain bool
}

// Certificate validity states.
const (
	ValidityValid        = "valid"
	ValidityExpiringSoon = "expiring soon"
	ValidityExpired      = "expired"
	ValidityNotYetValid  = "not yet valid"
	ValidityUntrusted    = "untrusted"
)

// InspectResult is everything worth knowing about a host's certificate.
type InspectResult struct {
	Host          string            `json:"host"`
	Port          int               `json:"port"`
	Validity      string            `json:"validity"`
	Valid         bool              `json:"valid"`
	Trusted       bool              `json:"trusted"`
	TrustError    string            `json:"trustError,omitempty"`
	Subject       string            `json:"subject"`
	Issuer        string            `json:"issuer"`
	SerialNumber  string            `json:"serialNumber"`
	NotBefore     time.Time         `json:"notBefore"`
	NotAfter      time.Time         `json:"notAfter"`
	DaysRemaining int               `json:"daysRemaining"`
	DNSNames      []string          `json:"dnsNames"`
	SelfSigned    bool              `json:"selfSigned"`
	TLSVersion    string            `json:"tlsVersion"`
	CipherSuite   string            `json:"cipherSuite"`
	HandshakeMs   int64             `json:"handshakeMs"`
	Chain         []net.Certificate `json:"chain,omitempty"`
	CheckedAt     time.Time         `json:"checkedAt"`
}

// Inspect reports a host's TLS certificate.
//
// An expired or untrusted certificate is a result, not an error. This is the
// one command whose whole purpose is looking at certificates that are wrong,
// so failing on the thing the user came to see would be useless. Only a
// failure to complete a handshake at all comes back as an error.
//
// The handshake itself is performed without verification so that the chain can
// be retrieved and then judged separately; see the platform layer for why. No
// data is sent over the connection.
func Inspect(ctx context.Context, inspector Inspector, request InspectRequest) (InspectResult, error) {
	host, err := ParseHost(request.Host)
	if err != nil {
		return InspectResult{}, err
	}

	port := request.Port
	if port == 0 {
		port = 443
	}
	if port < 1 || port > 65535 {
		return InspectResult{}, errors.New(errors.CodeInvalidInput,
			"invalid port %d", port).
			WithHint("expected a value between 1 and 65535")
	}

	warningDays := request.ExpiryWarningDays
	if warningDays <= 0 {
		warningDays = 30
	}

	chain, err := inspector.Certificates(ctx, host, port)
	if err != nil {
		return InspectResult{}, err
	}
	if len(chain.Certificates) == 0 {
		return InspectResult{}, errors.New(errors.CodeNetwork,
			"%s served no certificate", host)
	}

	leaf := chain.Certificates[0]
	now := time.Now().UTC()

	result := InspectResult{
		Host:          host,
		Port:          port,
		Trusted:       chain.Verified,
		TrustError:    chain.VerifyError,
		Subject:       leaf.Subject,
		Issuer:        leaf.Issuer,
		SerialNumber:  leaf.SerialNumber,
		NotBefore:     leaf.NotBefore,
		NotAfter:      leaf.NotAfter,
		DaysRemaining: daysUntil(now, leaf.NotAfter),
		DNSNames:      leaf.DNSNames,
		SelfSigned:    leaf.SelfSigned,
		TLSVersion:    chain.Version,
		CipherSuite:   chain.CipherSuite,
		HandshakeMs:   chain.HandshakeMs,
		CheckedAt:     now,
	}
	if request.FullChain {
		result.Chain = chain.Certificates
	}
	if result.DNSNames == nil {
		result.DNSNames = []string{}
	}

	result.Validity, result.Valid = validity(now, leaf, chain.Verified, warningDays)
	return result, nil
}

// validity reduces the several ways a certificate can be wrong to one word.
//
// Order matters. A certificate that is both expired and untrusted is reported
// as expired, because that is the actionable fact and the trust failure is
// usually a consequence of it.
func validity(now time.Time, leaf net.Certificate, trusted bool, warningDays int) (string, bool) {
	switch {
	case now.After(leaf.NotAfter):
		return ValidityExpired, false
	case now.Before(leaf.NotBefore):
		return ValidityNotYetValid, false
	case !trusted:
		return ValidityUntrusted, false
	case daysUntil(now, leaf.NotAfter) <= warningDays:
		return ValidityExpiringSoon, true
	default:
		return ValidityValid, true
	}
}

// daysUntil counts whole days remaining, rounding towards zero. A certificate
// with eleven hours left has zero days remaining, which is the honest answer.
func daysUntil(now, deadline time.Time) int {
	return int(deadline.Sub(now).Hours() / 24)
}
