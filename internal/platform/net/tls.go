package net

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	stdnet "net"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

// Session summarises the TLS connection a request travelled over.
type Session struct {
	Version     string `json:"version"`
	CipherSuite string `json:"cipherSuite"`
	ServerName  string `json:"serverName"`
}

// Certificate is one certificate from a served chain, flattened into plain
// data so it serialises without anything clever.
type Certificate struct {
	Subject            string    `json:"subject"`
	Issuer             string    `json:"issuer"`
	SerialNumber       string    `json:"serialNumber"`
	NotBefore          time.Time `json:"notBefore"`
	NotAfter           time.Time `json:"notAfter"`
	DNSNames           []string  `json:"dnsNames"`
	SignatureAlgorithm string    `json:"signatureAlgorithm"`
	PublicKeyAlgorithm string    `json:"publicKeyAlgorithm"`
	IsCA               bool      `json:"isCertificateAuthority"`
	SelfSigned         bool      `json:"selfSigned"`
}

// Chain is everything observed about a host's TLS configuration.
type Chain struct {
	Host         string        `json:"host"`
	Port         int           `json:"port"`
	Version      string        `json:"version"`
	CipherSuite  string        `json:"cipherSuite"`
	Certificates []Certificate `json:"certificates"`
	Verified     bool          `json:"verified"`
	VerifyError  string        `json:"verifyError,omitempty"`
	HandshakeMs  int64         `json:"handshakeMs"`
}

// Certificates performs a TLS handshake and reports the served chain.
//
// Verification is deliberately switched off for the handshake itself and then
// performed separately on the chain that came back. That is the only way to
// report *why* a certificate is bad: a verifying handshake fails and hands
// back an error instead of the certificate the user asked to look at, which is
// useless for the one command whose whole job is inspecting broken
// certificates.
//
// Nothing is sent over the connection. The handshake completes, the chain is
// copied out, and the connection closes.
func (s System) Certificates(ctx context.Context, host string, port int) (Chain, error) {
	if port <= 0 {
		port = DefaultPort
	}
	address := Address(host, port)

	dialer := &stdnet.Dialer{Timeout: s.timeout()}
	config := &tls.Config{
		// See the comment above: this is an inspection handshake, and the
		// verification below is what decides whether the chain is acceptable.
		InsecureSkipVerify: true,
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
	}

	started := time.Now()
	connection, err := (&tls.Dialer{NetDialer: dialer, Config: config}).DialContext(ctx, "tcp", address)
	if err != nil {
		return Chain{}, classifyDialError(err, address)
	}
	handshake := time.Since(started)
	defer func() {
		// Nothing was written, so a close failure cannot lose anything.
		_ = connection.Close()
	}()

	secure, ok := connection.(*tls.Conn)
	if !ok {
		return Chain{}, errors.New(errors.CodeInternal,
			"%s did not return a TLS connection", address)
	}

	state := secure.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Chain{}, errors.New(errors.CodeNetwork,
			"%s completed a handshake but served no certificate", address)
	}

	chain := Chain{
		Host:         host,
		Port:         port,
		Version:      versionName(state.Version),
		CipherSuite:  tls.CipherSuiteName(state.CipherSuite),
		Certificates: describeChain(state.PeerCertificates),
		HandshakeMs:  handshake.Milliseconds(),
	}

	if err := verify(host, state.PeerCertificates); err != nil {
		chain.VerifyError = err.Error()
	} else {
		chain.Verified = true
	}

	return chain, nil
}

// verify checks the served chain against the system trust store and the host
// name, exactly as a normal client would.
func verify(host string, served []*x509.Certificate) error {
	intermediates := x509.NewCertPool()
	for _, certificate := range served[1:] {
		intermediates.AddCert(certificate)
	}

	_, err := served[0].Verify(x509.VerifyOptions{
		DNSName:       host,
		Intermediates: intermediates,
	})
	return err
}

func describeChain(served []*x509.Certificate) []Certificate {
	certificates := make([]Certificate, 0, len(served))

	for _, certificate := range served {
		certificates = append(certificates, Certificate{
			Subject:            certificate.Subject.String(),
			Issuer:             certificate.Issuer.String(),
			SerialNumber:       certificate.SerialNumber.String(),
			NotBefore:          certificate.NotBefore.UTC(),
			NotAfter:           certificate.NotAfter.UTC(),
			DNSNames:           append([]string(nil), certificate.DNSNames...),
			SignatureAlgorithm: certificate.SignatureAlgorithm.String(),
			PublicKeyAlgorithm: certificate.PublicKeyAlgorithm.String(),
			IsCA:               certificate.IsCA,
			SelfSigned:         certificate.Subject.String() == certificate.Issuer.String(),
		})
	}

	return certificates
}

func sessionOf(state *tls.ConnectionState) *Session {
	if state == nil {
		return nil
	}
	return &Session{
		Version:     versionName(state.Version),
		CipherSuite: tls.CipherSuiteName(state.CipherSuite),
		ServerName:  state.ServerName,
	}
}

func versionName(version uint16) string {
	switch version {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS10:
		return "TLS 1.0"
	default:
		return fmt.Sprintf("unknown (0x%04x)", version)
	}
}

// classifyDialError turns a connection failure into something actionable.
func classifyDialError(err error, address string) error {
	switch {
	case errors.Is(err, context.Canceled):
		return errors.Wrap(err, errors.CodeCancelled, "cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.Wrap(err, errors.CodeTimeout, "%s did not answer in time", address).
			WithHint("pass --timeout to wait longer")
	}

	var dnsErr *stdnet.DNSError
	if errors.As(err, &dnsErr) {
		return errors.Wrap(err, errors.CodeNotFound, "cannot resolve %s", dnsErr.Name).
			WithHint("check the host name, and that this machine has working DNS")
	}

	if strings.Contains(err.Error(), "tls:") || strings.Contains(err.Error(), "handshake") {
		return errors.Wrap(err, errors.CodeNetwork, "the TLS handshake with %s failed", address).
			WithHint("the host may not speak TLS on this port; pass --port to choose another")
	}

	return errors.Wrap(err, errors.CodeNetwork, "cannot connect to %s", address).
		WithHint("check the host and port, and whether a firewall or proxy is in the way")
}
