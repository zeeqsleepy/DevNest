package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/network"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newNetworkMonitorCommand() *Command {
	var (
		flags        networkFlags
		method       string
		headers      repeatable
		expectStatus int
		maxResponse  int64
		noRedirect   bool
	)

	return &Command{
		Name:    "monitor",
		Summary: "Check whether a site is up and how quickly it answers",
		Usage:   "devnest network monitor <url> [flags]",
		Description: "Check a site's availability, response time, and HTTP status.\n\n" +
			"A site being down is a result, not a failure of the command: the exit " +
			"code is 0 when the site is healthy and 1 when it is not, so this works " +
			"as a cron entry or a CI gate without anyone parsing the output.\n\n" +
			"Any 2xx or 3xx counts as healthy unless --expect-status says otherwise. " +
			"With --max-response the site is reported as slow, and treated as " +
			"unhealthy, when it answers more slowly than you allow.",
		Examples: []Example{
			{
				Command:     "devnest network monitor https://example.com",
				Description: "Check that a site is answering, and see how long it took.",
			},
			{
				Command:     "devnest network monitor example.com --expect-status 200 --max-response 500ms",
				Description: "Fail unless the site returns exactly 200 within half a second.",
			},
			{
				Command:     "devnest network monitor https://api.example.com/health --output json",
				Description: "Produce a health check a script or a monitoring job can read.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			flags.register(set)
			flags.registerInsecure(set)
			set.StringVar(&method, "method", "GET", "HTTP method to use")
			set.Var(&headers, "header", "send a header as \"Name: value\" (repeatable)")
			set.IntVar(&expectStatus, "expect-status", 0, "the status code that counts as healthy")
			set.Var(newDurationMsValue(&maxResponse, 0), "max-response",
				"report the site as slow if it takes longer than this, for example 500ms")
			set.BoolVar(&noRedirect, "no-redirect", false, "do not follow redirects")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			target, err := firstTarget(args, "devnest network monitor")
			if err != nil {
				return err
			}
			sent, err := parseHeaders(headers)
			if err != nil {
				return err
			}
			flags.warnInsecure(env)

			system := flags.system(env, !noRedirect && env.Config.Network.FollowRedirect, 0)

			result, err := network.Monitor(ctx, system, network.MonitorRequest{
				URL:           target,
				Method:        method,
				Headers:       sent,
				ExpectStatus:  expectStatus,
				MaxResponseMs: maxResponse,
			})
			if err != nil {
				return err
			}

			if err := env.Emit(result, monitorText(result)); err != nil {
				return err
			}

			// The exit code is the answer. A monitoring job should not have to
			// read the output to learn whether the site is up.
			if !result.Healthy {
				return errors.New(errors.CodeCheckFailed, "%s is %s", result.URL, result.Status).
					WithHint("%s", result.Reason)
			}
			return nil
		},
	}
}

func monitorText(result network.MonitorResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "url", Value: result.URL},
			{Label: "status", Value: result.Status},
		}
		if result.StatusCode > 0 {
			fields = append(fields, output.Field{
				Label: "http", Value: result.StatusText,
			})
		}
		if result.FinalURL != "" && result.FinalURL != result.URL {
			fields = append(fields, output.Field{Label: "final url", Value: result.FinalURL})
		}
		if result.Redirects > 0 {
			fields = append(fields, output.Field{
				Label: "redirects", Value: output.Count(result.Redirects),
			})
		}
		fields = append(fields,
			output.Field{Label: "response", Value: milliseconds(result.ResponseMs)},
			output.Field{Label: "checked", Value: result.CheckedAt.Format("2006-01-02 15:04:05 MST")},
		)
		if result.Reason != "" {
			fields = append(fields, output.Field{Label: "reason", Value: result.Reason})
		}

		return output.WriteFields(w, fields)
	}
}

// milliseconds formats a duration for a person. Sub-second values stay in
// milliseconds, where the difference between 40 and 400 is what matters.
func milliseconds(value int64) string {
	if value < 1000 {
		return fmt.Sprintf("%d ms", value)
	}
	return fmt.Sprintf("%.2f s", float64(value)/1000)
}
