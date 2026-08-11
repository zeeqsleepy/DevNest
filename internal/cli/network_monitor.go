package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

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
		interval     time.Duration
		count        int
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
			"unhealthy, when it answers more slowly than you allow.\n\n" +
			"With --interval the check becomes a continuous monitoring loop: each " +
			"result is printed as it happens, and --count stops it after that many " +
			"checks. Without a count it runs until you stop it with Ctrl+C, " +
			"reporting one summary at the end. The exit code reflects the last " +
			"check, so a site that recovers exits 0 and a cron entry does not " +
			"stay red after the outage is over.",
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
				Command:     "devnest network monitor https://api.example.com/health --interval 30s",
				Description: "Keep checking every thirty seconds until you stop it.",
			},
			{
				Command:     "devnest network monitor example.com --interval 5s --count 12 --output json",
				Description: "Run a twelve-check health probe over a minute and read it as JSON.",
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
			set.DurationVar(&interval, "interval", 0, "keep checking, waiting this long between checks")
			set.IntVar(&count, "count", 0, "how many checks to run (default: until stopped)")
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

			request := network.MonitorRequest{
				URL:           target,
				Method:        method,
				Headers:       sent,
				ExpectStatus:  expectStatus,
				MaxResponseMs: maxResponse,
			}

			// A single check keeps the one-shot behaviour that predates
			// polling: exactly one result, no loop, nothing to watch.
			if interval <= 0 {
				result, err := network.Monitor(ctx, system, request)
				if err != nil {
					return err
				}

				if err := env.Emit(result, monitorText(result)); err != nil {
					return err
				}

				// The exit code is the answer. A monitoring job should not
				// have to read the output to learn whether the site is up.
				if !result.Healthy {
					return errors.New(errors.CodeCheckFailed, "%s is %s", result.URL, result.Status).
						WithHint("%s", result.Reason)
				}
				return nil
			}

			series, err := network.Poll(ctx, system, network.PollRequest{
				Monitor:  request,
				Interval: interval,
				Count:    count,
				OnCheck: func(checked network.MonitorResult) {
					env.Progress(monitorLiveLine(checked))
				},
			})
			if err != nil {
				return err
			}

			if err := env.Emit(series, pollText(series)); err != nil {
				return err
			}

			// Cancelled is how an uncapped run finishes; the summary has
			// already been emitted, and the cancellation is still reported so
			// scripts can tell a complete run from an interrupted one.
			if ctx.Err() != nil {
				return errors.Wrap(ctx.Err(), errors.CodeCancelled, "monitoring stopped")
			}

			if !series.Healthy {
				return errors.New(errors.CodeCheckFailed, "%s is %s", series.URL, series.Latest.Status).
					WithHint("%s", series.Latest.Reason)
			}
			return nil
		},
	}
}

// monitorLiveLine is the one-line status shown as each check of a monitoring
// loop completes. Live updates belong on stderr, so stdout keeps carrying only
// the final summary.
func monitorLiveLine(result network.MonitorResult) string {
	when := result.CheckedAt.Format("15:04:05")
	if result.StatusCode > 0 {
		return fmt.Sprintf("%s  %s  %s in %s", when, result.Status, result.StatusText, milliseconds(result.ResponseMs))
	}
	return fmt.Sprintf("%s  %s  (%s)", when, result.Status, result.Reason)
}

func pollText(result network.PollResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "url", Value: result.URL},
			{Label: "checks", Value: output.Count(result.Checks)},
			{Label: "up", Value: output.Count(result.Up)},
			{Label: "slow", Value: output.Count(result.Slow)},
			{Label: "down", Value: output.Count(result.Down)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}
		fmt.Fprintln(w)
		return monitorText(result.Latest)(w)
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
