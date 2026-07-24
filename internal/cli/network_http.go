package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devnest/devnest/internal/core/network"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/net"
)

func newNetworkHTTPCommand() *Command {
	var (
		flags        networkFlags
		method       string
		headers      repeatable
		data         string
		dataFile     string
		noRedirect   bool
		maxRedirects int
		showSecrets  bool
		showBody     bool
	)

	return &Command{
		Name:    "http",
		Summary: "Send an HTTP request and report everything about it",
		Usage:   "devnest network http <url> [flags]",
		Description: "Send one request and report the status, a timing breakdown, the " +
			"headers, the redirect chain, and the TLS session it travelled over.\n\n" +
			"This is a diagnostic tool, not an API client: one request, full detail, " +
			"no saved collections or environments.\n\n" +
			"A non-2xx status is not a failure here: inspecting a 404 or a 500 is an " +
			"ordinary reason to run this. Header values that look like credentials are " +
			"masked in every output format unless --show-secrets is given, because a " +
			"report gets attached to a ticket and a ticket gets shared.",
		Examples: []Example{
			{
				Command:     "devnest network http https://example.com",
				Description: "Send a GET and see the status, headers, and where the time went.",
			},
			{
				Command:     "devnest network http https://api.example.com/items --method POST --data '{\"name\":\"x\"}' --header \"Content-Type: application/json\"",
				Description: "Send a JSON body and inspect the response.",
			},
			{
				Command:     "devnest network http https://example.com --no-redirect --output json",
				Description: "Stop at the first response and produce output a script can read.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			flags.register(set)
			flags.registerInsecure(set)
			set.StringVar(&method, "method", "GET", "HTTP method to use")
			set.StringVar(&method, "X", "GET", "HTTP method to use (shorthand)")
			set.Var(&headers, "header", "send a header as \"Name: value\" (repeatable)")
			set.Var(&headers, "H", "send a header (shorthand, repeatable)")
			set.StringVar(&data, "data", "", "send this string as the request body")
			set.StringVar(&dataFile, "file", "", "send the contents of this file as the request body")
			set.BoolVar(&noRedirect, "no-redirect", false, "do not follow redirects")
			set.IntVar(&maxRedirects, "max-redirects", 0, "stop after this many redirects")
			set.BoolVar(&showSecrets, "show-secrets", false,
				"print credential-shaped header values in full")
			set.BoolVar(&showBody, "body", false, "print the response body")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			target, err := firstTarget(args, "devnest network http")
			if err != nil {
				return err
			}
			sent, err := parseHeaders(headers)
			if err != nil {
				return err
			}
			body, err := requestBody(data, dataFile)
			if err != nil {
				return err
			}
			flags.warnInsecure(env)
			if showSecrets {
				env.Warn(errors.CodeInvalidInput,
					"credential-shaped headers will be printed in full")
			}

			system := flags.system(env,
				!noRedirect && env.Config.Network.FollowRedirect, maxRedirects)

			result, err := network.Fetch(ctx, system, network.FetchRequest{
				URL:         target,
				Method:      method,
				Headers:     sent,
				Body:        body,
				ShowSecrets: showSecrets,
			})
			if err != nil {
				return err
			}

			return env.Emit(result, httpText(result, showBody))
		},
	}
}

// requestBody reads the body from a flag or a file, refusing both at once
// rather than silently preferring one.
func requestBody(data, path string) ([]byte, error) {
	if data != "" && path != "" {
		return nil, errors.New(errors.CodeInvalidInput,
			"--data and --file cannot both be used").
			WithHint("pass the body one way or the other")
	}
	if data != "" {
		return []byte(data), nil
	}
	if path == "" {
		return nil, nil
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.Wrap(err, errors.CodeNotFound, "cannot read %s", path)
		}
		if os.IsPermission(err) {
			return nil, errors.Wrap(err, errors.CodePermissionDenied, "cannot read %s", path)
		}
		return nil, errors.Wrap(err, errors.CodeIO, "cannot read %s", path)
	}
	return contents, nil
}

func httpText(result network.FetchResult, showBody bool) output.TextFunc {
	return func(w io.Writer) error {
		fmt.Fprintf(w, "%s %s\n", result.Method, result.URL)
		fmt.Fprintf(w, "%s %s\n\n", result.Protocol, result.Status)

		if len(result.Redirects) > 0 {
			fmt.Fprintln(w, "Redirects")
			rows := make([][]string, 0, len(result.Redirects))
			for _, hop := range result.Redirects {
				rows = append(rows, []string{
					fmt.Sprintf("%d", hop.StatusCode), hop.URL, hop.Location,
				})
			}
			err := output.WriteTable(w, []output.Column{
				{Title: "status", Right: true},
				{Title: "from"},
				{Title: "to"},
			}, rows)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		if err := writeTiming(w, result.Timing); err != nil {
			return err
		}

		if result.TLS != nil {
			fmt.Fprintf(w, "\nTLS  %s, %s\n", result.TLS.Version, result.TLS.CipherSuite)
		}

		if len(result.ResponseHeaders) > 0 {
			fmt.Fprintln(w, "\nResponse headers")
			rows := make([][]string, 0, len(result.ResponseHeaders))
			for _, header := range result.ResponseHeaders {
				rows = append(rows, []string{header.Name, header.Value})
			}
			if err := output.WriteTable(w, []output.Column{
				{Title: "header"}, {Title: "value"},
			}, rows); err != nil {
				return err
			}
		}

		if showBody && result.Body != "" {
			fmt.Fprintln(w, "\nBody")
			fmt.Fprintln(w, strings.TrimRight(result.Body, "\n"))
			if result.BodyTruncated {
				fmt.Fprintf(w, "\n... truncated, %s in total\n", output.Bytes(result.BodyBytes))
			}
		} else if result.BodyBytes > 0 {
			fmt.Fprintf(w, "\nBody  %s (pass --body to print it)\n", output.Bytes(result.BodyBytes))
		}

		return nil
	}
}

// writeTiming shows where the time went. Phases that did not happen (TLS on a
// plain request, DNS on a cached name) are left out rather than shown as
// zero, which would read as "instant" instead of "not applicable".
func writeTiming(w io.Writer, timing net.Timing) error {
	fields := make([]output.Field, 0, 5)

	if timing.DNSMs > 0 {
		fields = append(fields, output.Field{Label: "dns", Value: milliseconds(timing.DNSMs)})
	}
	if timing.ConnectMs > 0 {
		fields = append(fields, output.Field{Label: "connect", Value: milliseconds(timing.ConnectMs)})
	}
	if timing.TLSMs > 0 {
		fields = append(fields, output.Field{Label: "tls", Value: milliseconds(timing.TLSMs)})
	}
	if timing.FirstByteMs > 0 {
		fields = append(fields, output.Field{Label: "first byte", Value: milliseconds(timing.FirstByteMs)})
	}
	fields = append(fields, output.Field{Label: "total", Value: milliseconds(timing.TotalMs)})

	fmt.Fprintln(w, "Timing")
	return output.WriteFields(w, fields)
}
