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

func newNetworkLatencyCommand() *Command {
	var (
		flags    networkFlags
		method   string
		headers  repeatable
		attempts int
		interval time.Duration
		showAll  bool
	)

	return &Command{
		Name:    "latency",
		Summary: "Measure how long a url takes to answer, several times over",
		Usage:   "devnest network latency <url> [flags]",
		Description: "Send the same request several times and report the minimum, average, " +
			"median, and maximum response time.\n\n" +
			"Attempts run one after another with a short pause between them, and " +
			"connections are never reused. Reusing a connection would report almost " +
			"no setup cost after the first attempt, which flatters the numbers and " +
			"hides the part most likely to be slow.\n\n" +
			"The median is reported alongside the average because a single slow " +
			"attempt drags an average badly, and the two disagreeing is the whole " +
			"signal that something is intermittent.\n\n" +
			"This measures latency. It is not a load testing tool and will not " +
			"pretend to be one.",
		Examples: []Example{
			{
				Command:     "devnest network latency https://example.com",
				Description: "Measure the response time over the default number of attempts.",
			},
			{
				Command:     "devnest network latency https://api.example.com --attempts 20 --interval 500ms",
				Description: "Take twenty measurements half a second apart.",
			},
			{
				Command:     "devnest network latency example.com --attempts 10 --show-attempts",
				Description: "See every individual measurement, not only the summary.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			flags.register(set)
			flags.registerInsecure(set)
			set.StringVar(&method, "method", "GET", "HTTP method to use")
			set.Var(&headers, "header", "send a header as \"Name: value\" (repeatable)")
			set.IntVar(&attempts, "attempts", 0, "how many measurements to take")
			set.DurationVar(&interval, "interval", 0, "wait this long between attempts")
			set.BoolVar(&showAll, "show-attempts", false, "list every attempt, not just the summary")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			target, err := firstTarget(args, "devnest network latency")
			if err != nil {
				return err
			}
			sent, err := parseHeaders(headers)
			if err != nil {
				return err
			}
			flags.warnInsecure(env)

			system := flags.system(env, env.Config.Network.FollowRedirect, 0)

			result, err := network.Latency(ctx, system, network.LatencyRequest{
				URL:      target,
				Method:   method,
				Headers:  sent,
				Attempts: attemptsOf(env, attempts),
				Interval: intervalOf(env, interval),
			})
			if err != nil {
				return err
			}

			if err := env.Emit(result, latencyText(result, showAll)); err != nil {
				return err
			}

			// Every attempt failing is a negative finding, and the exit code
			// says so without anyone reading the output.
			if result.Successful == 0 {
				return errors.New(errors.CodeCheckFailed,
					"none of the %d attempts to %s succeeded", result.Attempts, result.URL)
			}
			return nil
		},
	}
}

func latencyText(result network.LatencyResult, showAll bool) output.TextFunc {
	return func(w io.Writer) error {
		fmt.Fprintf(w, "%s %s\n\n", result.Method, result.URL)

		if showAll && len(result.Samples) > 0 {
			rows := make([][]string, 0, len(result.Samples))
			for _, sample := range result.Samples {
				status := "failed"
				if sample.OK {
					status = fmt.Sprintf("%d", sample.StatusCode)
				}
				detail := sample.Error
				if sample.OK {
					detail = milliseconds(sample.ResponseMs)
				}
				rows = append(rows, []string{
					fmt.Sprintf("%d", sample.Number), status, detail,
				})
			}
			err := output.WriteTable(w, []output.Column{
				{Title: "attempt", Right: true},
				{Title: "status"},
				{Title: "result"},
			}, rows)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		if result.Successful > 0 {
			err := output.WriteFields(w, []output.Field{
				{Label: "minimum", Value: milliseconds(result.Statistics.MinMs)},
				{Label: "average", Value: milliseconds(result.Statistics.AverageMs)},
				{Label: "median", Value: milliseconds(result.Statistics.MedianMs)},
				{Label: "maximum", Value: milliseconds(result.Statistics.MaxMs)},
				{Label: "deviation", Value: fmt.Sprintf("%.2f ms", result.Statistics.StdDevMs)},
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		fmt.Fprintf(w, "%d attempts, %d succeeded, %d failed\n",
			result.Attempts, result.Successful, result.Failed)

		if result.Failed > 0 && !showAll {
			fmt.Fprintln(w, "Pass --show-attempts to see which ones failed and why.")
		}
		return nil
	}
}
