package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/core/network"
	"github.com/devnest/devnest/internal/output"
)

func newNetworkScanCommand() *Command {
	var (
		flags        networkFlags
		portsSpec    string
		concurrency  int
		probeTimeout time.Duration
	)

	return &Command{
		Name:    "scan",
		Summary: "Scan a host for open TCP ports",
		Usage:   "devnest network scan <host> [flags]",
		Description: "Find which TCP ports a host is listening on, in parallel, and " +
			"report what each one is probably for.\n\n" +
			"This is a connect scan: every port gets a normal TCP connection attempt, " +
			"and a success means the service accepted it. It is not a SYN scan and it " +
			"never sends the half-open packets that need a raw socket and therefore " +
			"administrator rights; DevNest never asks for elevation.\n\n" +
			"Three outcomes are reported. A port that accepts the connection is open, " +
			"a port that refuses it is closed, and a port that stays silent until the " +
			"probe timeout is filtered — which is how a host that drops packets " +
			"differs from one that rejects them.\n\n" +
			"The service names come from a static registry of well-known ports, never " +
			"from connecting to the service, so they are hints rather than detections.\n\n" +
			"Probes run in parallel but the concurrency is bounded, so a scan finishes " +
			"quickly without opening thousands of connections at once at the host it is " +
			"pointed at — rude even when the machine belongs to you.",
		Examples: []Example{
			{
				Command:     "devnest network scan example.com",
				Description: "Probe the curated set of common ports.",
			},
			{
				Command:     "devnest network scan db.internal --ports 5432,6379,27017",
				Description: "Ask only about the database ports you care about.",
			},
			{
				Command:     "devnest network scan 10.0.0.5 --ports 1-1024 --probe-timeout 1s",
				Description: "Sweep the well-known range on a machine on your own network.",
			},
			{
				Command:     "devnest network scan example.com --ports 80,443 --output json",
				Description: "Get the result for a script or a monitoring job.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			flags.register(set)
			set.StringVar(&portsSpec, "ports", "",
				"ports or ranges to probe, for example 22,80,443 or 8000-8010 (default: the common set)")
			set.StringVar(&portsSpec, "p", "",
				"ports or ranges to probe (shorthand)")
			set.IntVar(&concurrency, "concurrency", 100,
				"how many probes to run at the same time")
			set.DurationVar(&probeTimeout, "probe-timeout", 3*time.Second,
				"give up on a single port after this long")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			target, err := firstTarget(args, "devnest network scan")
			if err != nil {
				return err
			}

			var ports []int
			if strings.TrimSpace(portsSpec) != "" {
				ports, err = network.ExpandPorts(portsSpec)
				if err != nil {
					return err
				}
			}

			system := flags.system(env, false, 0)

			result, err := network.Scan(ctx, system, network.ScanRequest{
				Host:         target,
				Ports:        ports,
				Concurrency:  concurrency,
				ProbeTimeout: probeTimeout,
			})
			if err != nil {
				return err
			}

			if err := env.Emit(result, scanText(result)); err != nil {
				return err
			}
			return nil
		},
	}
}

func scanText(result network.ScanResult) output.TextFunc {
	return func(w io.Writer) error {
		fmt.Fprintf(w, "%s resolves to %s\n", result.Host, strings.Join(result.Addresses, ", "))
		fmt.Fprintln(w)

		if len(result.Open) == 0 {
			fmt.Fprintln(w, "No open ports were found.")
			fmt.Fprintln(w)
		} else {
			rows := make([][]string, 0, len(result.Open))
			for _, open := range result.Open {
				service := open.Service
				if service == "" {
					service = "-"
				}
				rows = append(rows, []string{
					strconv.Itoa(open.Port),
					service,
					milliseconds(open.ResponseMs),
				})
			}
			err := output.WriteTable(w, []output.Column{
				{Title: "port", Right: true},
				{Title: "service"},
				{Title: "response"},
			}, rows)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		fmt.Fprintf(w, "%d ports scanned, %d open, %d closed, %d filtered, took %s\n",
			result.TotalPorts, result.OpenCount, result.ClosedCount, result.FilteredCount,
			milliseconds(result.DurationMs))
		return nil
	}
}
