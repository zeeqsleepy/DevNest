package network

import (
	"context"
	"testing"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
)

func exampleResolver() *fakeResolver {
	return &fakeResolver{answers: map[net.Kind][]net.Record{
		net.KindA:    {{Value: "93.184.216.34"}},
		net.KindAAAA: {{Value: "2606:2800:220:1:248:1893:25c8:1946"}},
		net.KindMX:   {{Value: "mail.example.com.", Priority: 10}},
		net.KindTXT:  {{Value: "v=spf1 -all"}},
	}}
}

func TestLookupResolvesEveryTypeByDefault(t *testing.T) {
	resolver := exampleResolver()

	result, err := Lookup(context.Background(), resolver, LookupRequest{Domain: "example.com"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	if len(result.Answers) != len(net.Kinds()) {
		t.Errorf("answers = %d, want one per supported type", len(result.Answers))
	}
	if !result.Resolved {
		t.Error("Resolved = false for a domain with records")
	}
	if result.Found != 4 {
		t.Errorf("Found = %d, want 4", result.Found)
	}
}

func TestLookupHonoursRequestedTypes(t *testing.T) {
	resolver := exampleResolver()

	result, err := Lookup(context.Background(), resolver, LookupRequest{
		Domain: "example.com",
		Kinds:  []net.Kind{net.KindMX, net.KindTXT},
	})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	if len(result.Answers) != 2 {
		t.Fatalf("answers = %d, want 2", len(result.Answers))
	}
	if result.Answers[0].Kind != string(net.KindMX) {
		t.Errorf("first answer = %q, want MX", result.Answers[0].Kind)
	}
	if result.Answers[0].Records[0].Priority != 10 {
		t.Errorf("MX priority = %d, want 10", result.Answers[0].Records[0].Priority)
	}
}

// A domain with no MX record is an ordinary domain. Reporting that as a failed
// command would be wrong.
func TestLookupReportsAMissingTypeWithoutFailing(t *testing.T) {
	resolver := &fakeResolver{answers: map[net.Kind][]net.Record{
		net.KindA: {{Value: "93.184.216.34"}},
	}}

	result, err := Lookup(context.Background(), resolver, LookupRequest{
		Domain: "example.com",
		Kinds:  []net.Kind{net.KindA, net.KindMX},
	})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	if !result.Resolved {
		t.Error("Resolved = false although the A record came back")
	}

	for _, answer := range result.Answers {
		if answer.Kind != string(net.KindMX) {
			continue
		}
		if len(answer.Records) != 0 {
			t.Errorf("MX records = %v, want none", answer.Records)
		}
		if answer.Error == "" {
			t.Error("the empty MX answer carries no explanation")
		}
	}
}

// Resolved is what separates "this domain has no mail server" from "this
// domain does not exist".
func TestLookupReportsADomainWithNothingAtAll(t *testing.T) {
	resolver := &fakeResolver{answers: map[net.Kind][]net.Record{}}

	result, err := Lookup(context.Background(), resolver, LookupRequest{Domain: "nowhere.invalid"})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if result.Resolved {
		t.Error("Resolved = true for a domain with no records at all")
	}
	if result.Found != 0 {
		t.Errorf("Found = %d, want 0", result.Found)
	}
}

func TestLookupAcceptsAURL(t *testing.T) {
	resolver := exampleResolver()

	result, err := Lookup(context.Background(), resolver, LookupRequest{
		Domain: "https://example.com/some/path",
	})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if result.Domain != "example.com" {
		t.Errorf("Domain = %q, want example.com", result.Domain)
	}
}

func TestLookupRejectsABadDomain(t *testing.T) {
	_, err := Lookup(context.Background(), exampleResolver(), LookupRequest{Domain: "example..com"})
	assertCode(t, err, errors.CodeInvalidInput)
}

func TestLookupPropagatesAResolverFailure(t *testing.T) {
	resolver := &fakeResolver{
		failure: errors.New(errors.CodeTimeout, "the lookup did not finish in time"),
	}

	_, err := Lookup(context.Background(), resolver, LookupRequest{Domain: "example.com"})
	assertCode(t, err, errors.CodeTimeout)
}

func TestLookupAnswersAreNeverNull(t *testing.T) {
	resolver := &fakeResolver{answers: map[net.Kind][]net.Record{}}

	result, err := Lookup(context.Background(), resolver, LookupRequest{
		Domain: "example.com", Kinds: []net.Kind{net.KindA},
	})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if result.Answers == nil {
		t.Fatal("Answers is null; it must always be an array")
	}
	if result.Answers[0].Records == nil {
		t.Error("Records is null; it must always be an array")
	}
}
