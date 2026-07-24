package net

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devnest/devnest/internal/errors"
)

func TestParseKind(t *testing.T) {
	for _, input := range []string{"a", "A", " mx ", "TXT", "aaaa", "cname", "ns"} {
		kind, err := ParseKind(input)
		if err != nil {
			t.Fatalf("ParseKind(%q): %v", input, err)
		}
		if string(kind) != strings.ToUpper(strings.TrimSpace(input)) {
			t.Errorf("ParseKind(%q) = %q", input, kind)
		}
	}

	_, err := ParseKind("SOA")
	if errors.CodeOf(err) != errors.CodeInvalidInput {
		t.Fatalf("code = %q, want %q", errors.CodeOf(err), errors.CodeInvalidInput)
	}
	if hint := errors.Classify(err).Hint; !strings.Contains(hint, "AAAA") {
		t.Errorf("hint = %q, want it to list the supported types", hint)
	}
}

func TestKindsAreAllParseable(t *testing.T) {
	for _, kind := range Kinds() {
		parsed, err := ParseKind(string(kind))
		if err != nil || parsed != kind {
			t.Errorf("ParseKind(%q) = %q, %v", kind, parsed, err)
		}
	}
}

// localhost resolves from the hosts file, so this exercises the real resolver
// path without touching the network.
func TestResolveLocalhost(t *testing.T) {
	client := system()
	client.Timeout = 2 * time.Second

	answers, err := client.Resolve(context.Background(), "localhost", []Kind{KindA})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(answers) != 1 {
		t.Fatalf("answers = %d, want 1", len(answers))
	}
	if answers[0].Kind != string(KindA) {
		t.Errorf("Kind = %q", answers[0].Kind)
	}
	if answers[0].Error != "" && len(answers[0].Records) == 0 {
		t.Skipf("localhost has no A record on this machine: %s", answers[0].Error)
	}
	if len(answers[0].Records) > 0 && answers[0].Records[0].Value != "127.0.0.1" {
		t.Errorf("records = %+v, want 127.0.0.1", answers[0].Records)
	}
}

// An A query must never report an IPv6 address, or the record type would be a
// label rather than a fact.
func TestResolveSeparatesAddressFamilies(t *testing.T) {
	client := system()
	client.Timeout = 2 * time.Second

	answers, err := client.Resolve(context.Background(), "localhost", []Kind{KindA, KindAAAA})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for _, answer := range answers {
		for _, record := range answer.Records {
			isIPv6 := strings.Contains(record.Value, ":")
			if answer.Kind == string(KindA) && isIPv6 {
				t.Errorf("an A answer contains an IPv6 address: %q", record.Value)
			}
			if answer.Kind == string(KindAAAA) && !isIPv6 {
				t.Errorf("an AAAA answer contains an IPv4 address: %q", record.Value)
			}
		}
	}
}

// A domain that does not exist is reported per type, not as a failure of the
// whole lookup.
func TestResolveReportsAMissingDomainPerType(t *testing.T) {
	client := system()
	client.Timeout = 2 * time.Second

	answers, err := client.Resolve(context.Background(), "devnest-test.invalid", []Kind{KindA, KindMX})
	if err != nil {
		t.Fatalf("Resolve returned an error rather than per-type answers: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("answers = %d, want 2", len(answers))
	}

	for _, answer := range answers {
		if len(answer.Records) != 0 {
			t.Errorf("%s returned records for a reserved invalid domain: %+v",
				answer.Kind, answer.Records)
		}
		if answer.Error == "" {
			t.Errorf("%s has no explanation for having no records", answer.Kind)
		}
		if answer.Records == nil {
			t.Errorf("%s has a null record list; it must be an array", answer.Kind)
		}
	}
}

func TestResolveDefaultsToEveryKind(t *testing.T) {
	client := system()
	client.Timeout = 2 * time.Second

	answers, err := client.Resolve(context.Background(), "localhost", nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(answers) != len(Kinds()) {
		t.Errorf("answers = %d, want one per supported type", len(answers))
	}
}

func TestResolveRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := system().Resolve(ctx, "localhost", []Kind{KindA})
	if err == nil {
		t.Fatal("Resolve succeeded with a cancelled context")
	}
}

func TestDescribeLookupErrorIsShortAndUseful(t *testing.T) {
	client := system()
	client.Timeout = 2 * time.Second

	answers, err := client.Resolve(context.Background(), "devnest-test.invalid", []Kind{KindA})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	message := answers[0].Error
	if message == "" {
		t.Fatal("no explanation was given")
	}
	if strings.Contains(message, "\n") {
		t.Errorf("explanation = %q, want a single line", message)
	}
}
