package net

import (
	"context"
	stdnet "net"
	"sort"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Kind names a DNS record type.
type Kind string

const (
	KindA     Kind = "A"
	KindAAAA  Kind = "AAAA"
	KindMX    Kind = "MX"
	KindTXT   Kind = "TXT"
	KindNS    Kind = "NS"
	KindCNAME Kind = "CNAME"
)

// Kinds lists every supported record type, in the order results are reported.
func Kinds() []Kind {
	return []Kind{KindA, KindAAAA, KindCNAME, KindMX, KindTXT, KindNS}
}

// ParseKind resolves a record type given on the command line.
func ParseKind(name string) (Kind, error) {
	candidate := Kind(strings.ToUpper(strings.TrimSpace(name)))
	for _, supported := range Kinds() {
		if candidate == supported {
			return supported, nil
		}
	}

	names := make([]string, 0, len(Kinds()))
	for _, supported := range Kinds() {
		names = append(names, string(supported))
	}
	return "", errors.New(errors.CodeInvalidInput, "unknown record type %q", name).
		WithHint("expected one of: %s", strings.Join(names, ", "))
}

// Record is one answer.
//
// Priority is only meaningful for MX. The standard library's resolver does not
// expose TTL, so DevNest does not report one rather than inventing a number
// that looks authoritative.
type Record struct {
	Value    string `json:"value"`
	Priority int    `json:"priority,omitempty"`
}

// Answer is every record of one type, or the reason there are none.
//
// A per-type error rather than a failed lookup matters: a domain with no MX
// record is an ordinary domain, and reporting that as a failure of the whole
// command would be wrong.
type Answer struct {
	Kind    string   `json:"type"`
	Records []Record `json:"records"`
	Error   string   `json:"error,omitempty"`
}

// Resolve looks up the requested record types for a domain.
//
// The lookups run in sequence rather than in parallel. Six queries against one
// resolver is not where the time goes, and sequential order keeps the output
// deterministic without a sort over results that arrive in a race.
//
// The timeout bounds the whole operation, not each query. When it runs out
// part way through, the answers already gathered are returned and the rest are
// marked as having timed out. Throwing away four good answers because the
// fifth query was slow would be the wrong trade: partial results are what the
// user came for, and the marked entries say plainly what is missing.
//
// A cancelled run is different: nobody is waiting for the result, so it comes
// back as an error.
func (s System) Resolve(ctx context.Context, domain string, kinds []Kind) ([]Answer, error) {
	if len(kinds) == 0 {
		kinds = Kinds()
	}

	deadline, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	resolver := &stdnet.Resolver{}
	answers := make([]Answer, 0, len(kinds))

	for index, kind := range kinds {
		if err := ctx.Err(); err != nil {
			return nil, errors.Wrap(err, errors.CodeCancelled, "cancelled")
		}
		if deadline.Err() != nil {
			answers = append(answers, timedOut(kinds[index:])...)
			break
		}

		records, err := lookup(deadline, resolver, domain, kind)
		answer := Answer{Kind: string(kind), Records: records}
		if err != nil {
			answer.Records = []Record{}
			answer.Error = describeLookupError(err)
		}
		answers = append(answers, answer)
	}

	return answers, nil
}

func timedOut(kinds []Kind) []Answer {
	answers := make([]Answer, 0, len(kinds))
	for _, kind := range kinds {
		answers = append(answers, Answer{
			Kind:    string(kind),
			Records: []Record{},
			Error:   "the lookup timed out",
		})
	}
	return answers
}

func lookup(ctx context.Context, resolver *stdnet.Resolver, domain string, kind Kind) ([]Record, error) {
	switch kind {
	case KindA, KindAAAA:
		return lookupAddresses(ctx, resolver, domain, kind)

	case KindCNAME:
		name, err := resolver.LookupCNAME(ctx, domain)
		if err != nil {
			return nil, err
		}
		// The resolver returns the domain itself when there is no alias.
		if strings.EqualFold(strings.TrimSuffix(name, "."), strings.TrimSuffix(domain, ".")) {
			return []Record{}, nil
		}
		return []Record{{Value: name}}, nil

	case KindMX:
		hosts, err := resolver.LookupMX(ctx, domain)
		if err != nil {
			return nil, err
		}
		records := make([]Record, 0, len(hosts))
		for _, host := range hosts {
			records = append(records, Record{Value: host.Host, Priority: int(host.Pref)})
		}
		sort.Slice(records, func(i, j int) bool {
			if records[i].Priority != records[j].Priority {
				return records[i].Priority < records[j].Priority
			}
			return records[i].Value < records[j].Value
		})
		return records, nil

	case KindTXT:
		values, err := resolver.LookupTXT(ctx, domain)
		if err != nil {
			return nil, err
		}
		return sortedRecords(values), nil

	case KindNS:
		servers, err := resolver.LookupNS(ctx, domain)
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(servers))
		for _, server := range servers {
			values = append(values, server.Host)
		}
		return sortedRecords(values), nil
	}

	return nil, errors.New(errors.CodeUnsupported, "unsupported record type %q", kind)
}

// lookupAddresses splits the resolver's answers by address family, so an A
// query never reports an IPv6 address and the other way round.
func lookupAddresses(ctx context.Context, resolver *stdnet.Resolver, domain string, kind Kind) ([]Record, error) {
	network := "ip4"
	if kind == KindAAAA {
		network = "ip6"
	}

	addresses, err := resolver.LookupIP(ctx, network, domain)
	if err != nil {
		return nil, err
	}

	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		values = append(values, address.String())
	}
	return sortedRecords(values), nil
}

func sortedRecords(values []string) []Record {
	sort.Strings(values)
	records := make([]Record, 0, len(values))
	for _, value := range values {
		records = append(records, Record{Value: value})
	}
	return records
}

// describeLookupError turns a resolver failure into one short line. "No such
// host" and "no records of this type" mean different things and a user needs
// to be able to tell them apart.
func describeLookupError(err error) string {
	var dnsErr *stdnet.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			return "no records of this type"
		case dnsErr.IsTimeout:
			return "the lookup timed out"
		case dnsErr.IsTemporary:
			return "the resolver reported a temporary failure"
		}
		return dnsErr.Err
	}
	return err.Error()
}

// Reverse looks up the names for an address.
func (s System) Reverse(ctx context.Context, address string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	names, err := (&stdnet.Resolver{}).LookupAddr(ctx, address)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeNotFound, "cannot look up %s", address)
	}
	sort.Strings(names)
	return names, nil
}
