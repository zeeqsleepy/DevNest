package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/core/network"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newNetworkPingCommand() *Command {
	var (
		flags    networkFlags
		port     int
		attempts int
		interval time.Duration
		showAll  bool
	)

	return &Command{
		Name:    "ping",
		Summary: "Check whether a host is reachable",
		Usage:   "devnest network ping <host> [flags]",
		Description: "Check that a host is reachable and how long a connection takes.\n\n" +
			"This opens a TCP connection rather than sending an ICMP echo, and every " +
			"result says so. Sending ICMP needs a raw socket and therefore " +
			"administrator rights on every platform, and DevNest never asks for " +
			"elevation; shelling out to the system ping instead would mean parsing " +
			"output that changes with the machine's language.\n\n" +
			"A TCP probe also answers the question people usually mean. Plenty of " +
			"hosts drop ICMP while accepting connections on 443 perfectly well, and " +
			"\"is the service answering\" is normally what is being asked.\n\n" +
			"An unreachable host is a result, not a failure of the command: the exit " +
			"code is 0 when the host answered at least once and 1 when it never did.",
		Examples: []Example{
			{
				Command:     "devnest network ping example.com",
				Description: "Check reachability on port 443, the default.",
			},
			{
				Command:     "devnest network ping db.internal --port 5432 --attempts 10",
				Description: "Check that a database is accepting connections.",
			},
			{
				Command:     "devnest network ping example.com --show-attempts --output json",
				Description: "Report every probe, for a script or a monitoring job.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			flags.register(set)
			set.IntVar(&port, "port", 443, "TCP port to connect to")
			set.IntVar(&port, "p", 443, "TCP port to connect to (shorthand)")
			set.IntVar(&attempts, "attempts", 0, "how many probes to send")
			set.IntVar(&attempts, "count", 0, "how many probes to send")
			set.DurationVar(&interval, "interval", 0, "wait this long between probes")
			set.BoolVar(&showAll, "show-attempts", false, "list every probe, not just the summary")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			target, err := firstTarget(args, "devnest network ping")
			if err != nil {
				return err
			}

			system := flags.system(env, false, 0)

			result, err := network.Ping(ctx, system, network.PingRequest{
				Host:     target,
				Port:     port,
				Attempts: attemptsOf(env, attempts),
				Interval: intervalOf(env, interval),
			})
			if err != nil {
				return err
			}

			if err := env.Emit(result, pingText(result, showAll)); err != nil {
				return err
			}

			if !result.Reachable {
				return errors.New(errors.CodeCheckFailed,
					"%s is not reachable on port %d", result.Host, result.Port).
					WithHint("the host may be down, or a firewall may be blocking this port")
			}
			return nil
		},
	}
}

func pingText(result network.PingResult, showAll bool) output.TextFunc {
	return func(w io.Writer) error {
		fmt.Fprintf(w, "%s port %d (%s probe)\n", result.Host, result.Port, result.Method)
		if len(result.Addresses) > 0 {
			fmt.Fprintf(w, "resolves to %s\n", strings.Join(result.Addresses, ", "))
		}
		fmt.Fprintln(w)

		if showAll && len(result.Probes) > 0 {
			rows := make([][]string, 0, len(result.Probes))
			for _, probe := range result.Probes {
				outcome := probe.Error
				if probe.OK {
					outcome = milliseconds(probe.ResponseMs)
				}
				rows = append(rows, []string{fmt.Sprintf("%d", probe.Number), outcome})
			}
			err := output.WriteTable(w, []output.Column{
				{Title: "probe", Right: true},
				{Title: "result"},
			}, rows)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		if result.Received > 0 {
			err := output.WriteFields(w, []output.Field{
				{Label: "minimum", Value: milliseconds(result.Statistics.MinMs)},
				{Label: "average", Value: milliseconds(result.Statistics.AverageMs)},
				{Label: "maximum", Value: milliseconds(result.Statistics.MaxMs)},
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		fmt.Fprintf(w, "%d sent, %d received, %.0f%% loss\n",
			result.Sent, result.Received, result.LossPercent)

		if !result.Reachable && !showAll && len(result.Probes) > 0 {
			fmt.Fprintf(w, "\n%s\n", result.Probes[0].Error)
		}
		return nil
	}
}
