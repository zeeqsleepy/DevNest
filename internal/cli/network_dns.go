package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/network"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/net"
)

func newNetworkDNSCommand() *Command {
	var (
		flags networkFlags
		types repeatable
	)

	return &Command{
		Name:    "dns",
		Summary: "Look up DNS records for a domain",
		Usage:   "devnest network dns <domain> [flags]",
		Description: "Resolve A, AAAA, CNAME, MX, TXT, and NS records for a domain.\n\n" +
			"Every type is looked up unless --type says otherwise. A type with no " +
			"answers is reported as such rather than failing the command: a domain " +
			"with no MX record is an ordinary domain, and treating that as an error " +
			"would be wrong.\n\n" +
			"Answers come from this machine's configured resolver, so the result is " +
			"what this machine would actually use, including any split-horizon or " +
			"internal DNS in the way. TTL is not reported: the resolver the standard " +
			"library uses does not expose it, and inventing a number that looks " +
			"authoritative would be worse than leaving it out.",
		Examples: []Example{
			{
				Command:     "devnest network dns example.com",
				Description: "Look up every supported record type.",
			},
			{
				Command:     "devnest network dns example.com --type MX --type TXT",
				Description: "Check the mail and verification records only.",
			},
			{
				Command:     "devnest network dns example.com --output json",
				Description: "Produce output a script can read.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			flags.register(set)
			set.Var(&types, "type", "record type to look up: A, AAAA, CNAME, MX, TXT, NS (repeatable)")
			set.Var(&types, "t", "record type to look up (shorthand, repeatable)")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			target, err := firstTarget(args, "devnest network dns")
			if err != nil {
				return err
			}

			kinds, err := parseKinds(types)
			if err != nil {
				return err
			}

			system := flags.system(env, false, 0)

			result, err := network.Lookup(ctx, system, network.LookupRequest{
				Domain: target,
				Kinds:  kinds,
			})
			if err != nil {
				return err
			}

			if err := env.Emit(result, dnsText(result)); err != nil {
				return err
			}

			if !result.Resolved {
				return errors.New(errors.CodeNotFound,
					"no records found for %s", result.Domain).
					WithHint("check the domain, and that this machine has working DNS")
			}
			return nil
		},
	}
}

func parseKinds(values []string) ([]net.Kind, error) {
	kinds := make([]net.Kind, 0, len(values))
	for _, value := range values {
		kind, err := net.ParseKind(value)
		if err != nil {
			return nil, err
		}
		kinds = append(kinds, kind)
	}
	return kinds, nil
}

func dnsText(result network.LookupResult) output.TextFunc {
	return func(w io.Writer) error {
		fmt.Fprintf(w, "%s\n\n", result.Domain)

		rows := make([][]string, 0, len(result.Answers))
		for _, answer := range result.Answers {
			if len(answer.Records) == 0 {
				// A type with no answers is worth a line: knowing a domain has
				// no MX record is often the reason someone ran this.
				rows = append(rows, []string{answer.Kind, "", noteFor(answer)})
				continue
			}
			for _, record := range answer.Records {
				priority := ""
				if record.Priority > 0 {
					priority = fmt.Sprintf("priority %d", record.Priority)
				}
				rows = append(rows, []string{answer.Kind, record.Value, priority})
			}
		}

		if err := output.WriteTable(w, []output.Column{
			{Title: "type"},
			{Title: "value"},
			{Title: "note"},
		}, rows); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s in %s\n", pluralRecords(result.Found), milliseconds(result.Duration))
		return nil
	}
}

func pluralRecords(count int) string {
	if count == 1 {
		return "1 record"
	}
	return output.Count(count) + " records"
}

func noteFor(answer net.Answer) string {
	if answer.Error != "" {
		return answer.Error
	}
	return "none"
}
