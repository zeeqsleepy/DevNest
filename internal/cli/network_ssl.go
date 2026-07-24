package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/devnest/devnest/internal/core/network"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newNetworkSSLCommand() *Command {
	var (
		flags       networkFlags
		port        int
		warningDays int
		fullChain   bool
	)

	return &Command{
		Name:    "ssl",
		Summary: "Inspect a host's TLS certificate",
		Usage:   "devnest network ssl <host> [flags]",
		Description: "Report a host's certificate: who issued it, who it is for, when it " +
			"expires, how many days are left, and whether it is trusted.\n\n" +
			"An expired or untrusted certificate is a result, not a failure: this is " +
			"the command you run precisely when something is wrong with one. The exit " +
			"code is non-zero when the certificate is not valid, so this works as a " +
			"scheduled expiry check without anyone parsing the output.\n\n" +
			"To report why a certificate is bad, the handshake has to complete without " +
			"verifying it first; verification is then performed separately on the " +
			"chain that came back. Nothing is sent over the connection, and it is " +
			"closed as soon as the certificate has been read. There is no --insecure " +
			"flag here, and none is needed.",
		Examples: []Example{
			{
				Command:     "devnest network ssl example.com",
				Description: "Check a certificate's issuer, expiry, and trust status.",
			},
			{
				Command:     "devnest network ssl example.com --warn-days 45",
				Description: "Treat a certificate with fewer than 45 days left as expiring soon.",
			},
			{
				Command:     "devnest network ssl mail.example.com --port 993 --chain",
				Description: "Inspect a non-HTTPS service and print the whole certificate chain.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			flags.register(set)
			set.IntVar(&port, "port", 443, "TCP port to connect to")
			set.IntVar(&port, "p", 443, "TCP port to connect to (shorthand)")
			set.IntVar(&warningDays, "warn-days", 30,
				"report a certificate as expiring soon below this many days")
			set.BoolVar(&fullChain, "chain", false, "print every certificate served, not just the first")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			target, err := firstTarget(args, "devnest network ssl")
			if err != nil {
				return err
			}

			system := flags.system(env, false, 0)

			result, err := network.Inspect(ctx, system, network.InspectRequest{
				Host:              target,
				Port:              port,
				ExpiryWarningDays: warningDays,
				FullChain:         fullChain,
			})
			if err != nil {
				return err
			}

			if err := env.Emit(result, sslText(result)); err != nil {
				return err
			}

			if !result.Valid {
				return errors.New(errors.CodeCheckFailed,
					"the certificate for %s is %s", result.Host, result.Validity).
					WithHint("%s", sslHint(result))
			}
			return nil
		},
	}
}

func sslHint(result network.InspectResult) string {
	if result.TrustError != "" {
		return result.TrustError
	}
	if result.Validity == network.ValidityExpired {
		return fmt.Sprintf("it expired on %s", result.NotAfter.Format("2 January 2006"))
	}
	return "run with --chain to see every certificate the host served"
}

func sslText(result network.InspectResult) output.TextFunc {
	return func(w io.Writer) error {
		fmt.Fprintf(w, "%s:%d\n\n", result.Host, result.Port)

		fields := []output.Field{
			{Label: "validity", Value: result.Validity},
			{Label: "subject", Value: result.Subject},
			{Label: "issuer", Value: result.Issuer},
			{Label: "valid from", Value: result.NotBefore.Format("2006-01-02")},
			{Label: "expires", Value: result.NotAfter.Format("2006-01-02")},
			{Label: "days left", Value: fmt.Sprintf("%d", result.DaysRemaining)},
			{Label: "trusted", Value: yesNo(result.Trusted)},
		}
		if result.SelfSigned {
			fields = append(fields, output.Field{Label: "self-signed", Value: "yes"})
		}
		if result.TLSVersion != "" {
			fields = append(fields, output.Field{Label: "tls", Value: result.TLSVersion})
		}
		if result.CipherSuite != "" {
			fields = append(fields, output.Field{Label: "cipher", Value: result.CipherSuite})
		}
		if len(result.DNSNames) > 0 {
			fields = append(fields, output.Field{
				Label: "names", Value: strings.Join(result.DNSNames, ", "),
			})
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		if result.TrustError != "" {
			fmt.Fprintf(w, "\nNot trusted: %s\n", result.TrustError)
		}

		if len(result.Chain) > 0 {
			fmt.Fprintln(w, "\nChain")
			rows := make([][]string, 0, len(result.Chain))
			for index, certificate := range result.Chain {
				rows = append(rows, []string{
					fmt.Sprintf("%d", index),
					certificate.Subject,
					certificate.NotAfter.Format("2006-01-02"),
				})
			}
			if err := output.WriteTable(w, []output.Column{
				{Title: "#", Right: true},
				{Title: "subject"},
				{Title: "expires"},
			}, rows); err != nil {
				return err
			}
		}

		return nil
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
