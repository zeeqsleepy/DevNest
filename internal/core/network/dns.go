package network

import (
	"context"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

// LookupRequest describes one DNS query.
type LookupRequest struct {
	Domain string
	// Kinds are the record types to look up. Empty means every supported
	// type, which is what someone typing "devnest network dns example.com"
	// is asking for.
	Kinds []net.Kind
}

// LookupResult is the answer for every type requested.
type LookupResult struct {
	Domain   string       `json:"domain"`
	Answers  []net.Answer `json:"answers"`
	Found    int          `json:"recordsFound"`
	Resolved bool         `json:"resolved"`
	Duration int64        `json:"durationMs"`
}

// Lookup resolves DNS records for a domain.
//
// A domain with no MX record is an ordinary domain, so a type with no answers
// is reported per type rather than failing the command. Resolved says whether
// anything at all came back, which is the difference between "this domain has
// no mail server" and "this domain does not exist".
func Lookup(ctx context.Context, resolver Resolver, request LookupRequest) (LookupResult, error) {
	domain, err := ParseDomain(request.Domain)
	if err != nil {
		return LookupResult{}, err
	}

	started := time.Now()
	answers, err := resolver.Resolve(ctx, domain, request.Kinds)
	if err != nil {
		return LookupResult{}, err
	}

	result := LookupResult{
		Domain:   domain,
		Answers:  answers,
		Duration: time.Since(started).Milliseconds(),
	}
	for _, answer := range result.Answers {
		result.Found += len(answer.Records)
	}
	result.Resolved = result.Found > 0

	if result.Answers == nil {
		result.Answers = []net.Answer{}
	}
	return result, nil
}

// ParseDomain validates a domain name.
//
// The check is deliberately shallow: it rejects the shapes that are certainly
// wrong (empty labels, spaces, a scheme, a path) and leaves the rest to the
// resolver. A full grammar here would reject valid internal names, and
// "example.corp" resolving on a company network is not DevNest's business to
// second-guess.
func ParseDomain(raw string) (string, error) {
	domain := strings.TrimSpace(raw)
	if domain == "" {
		return "", errors.New(errors.CodeInvalidInput, "no domain was given").
			WithHint("pass a domain, for example example.com")
	}

	if strings.Contains(domain, "://") {
		target, err := ParseTarget(domain)
		if err != nil {
			return "", err
		}
		domain = target.Host
	}

	domain = strings.TrimSuffix(domain, ".")
	if index := strings.IndexAny(domain, "/?#"); index >= 0 {
		domain = domain[:index]
	}
	if host, _, found := strings.Cut(domain, ":"); found {
		domain = host
	}

	if domain == "" || strings.ContainsAny(domain, " \t\\@") {
		return "", errors.New(errors.CodeInvalidInput, "invalid domain %q", raw).
			WithHint("expected something like example.com")
	}
	if strings.Contains(domain, "..") || strings.HasPrefix(domain, ".") {
		return "", errors.New(errors.CodeInvalidInput,
			"invalid domain %q: it has an empty label", raw)
	}
	if len(domain) > 253 {
		return "", errors.New(errors.CodeInvalidInput,
			"invalid domain %q: it is longer than 253 characters", raw)
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) > 63 {
			return "", errors.New(errors.CodeInvalidInput,
				"invalid domain %q: the label %q is longer than 63 characters", raw, label)
		}
	}

	return domain, nil
}
