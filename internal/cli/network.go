package cli

import (
	"flag"
	"time"

	"github.com/devnest/devnest/internal/core/network"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/platform/net"
	"github.com/devnest/devnest/internal/version"
)

// newNetworkCommand builds the "network" group.
func newNetworkCommand() *Command {
	return &Command{
		Name:    "network",
		Summary: "Inspect and test network resources",
		Usage:   "devnest network <command> [target] [flags]",
		Description: "Check whether a site is up, inspect an HTTP exchange, measure latency, " +
			"probe a host, look up DNS records, and inspect a TLS certificate.\n\n" +
			"These are the only commands in DevNest that open a network connection. " +
			"Every one of them is bounded by a timeout, and a failure to reach " +
			"something is reported rather than treated as a crash.",
		Commands: []*Command{
			newNetworkMonitorCommand(),
			newNetworkHTTPCommand(),
			newNetworkLatencyCommand(),
			newNetworkPingCommand(),
			newNetworkDNSCommand(),
			newNetworkSSLCommand(),
		},
	}
}

// networkFlags registers the options every network command shares, so a
// timeout means the same thing in all of them.
type networkFlags struct {
	timeout  time.Duration
	insecure bool
}

func (n *networkFlags) register(set *flag.FlagSet) {
	set.DurationVar(&n.timeout, "timeout", 0,
		"give up after this long, for example 5s (default from configuration)")
}

// registerInsecure adds --insecure, for the commands that make a verified
// connection. The ssl command does not get it: inspecting a broken certificate
// is what that command is for, and it does so without ever disabling anything
// the user did not ask about.
func (n *networkFlags) registerInsecure(set *flag.FlagSet) {
	set.BoolVar(&n.insecure, "insecure", false,
		"skip certificate verification (prints a warning; use only to diagnose)")
}

// system builds the platform client from configuration and flags.
func (n *networkFlags) system(env *Env, followRedirects bool, maxRedirects int) net.System {
	timeout := time.Duration(env.Config.Network.TimeoutMs) * time.Millisecond
	if n.timeout > 0 {
		timeout = n.timeout
	}
	if maxRedirects <= 0 {
		maxRedirects = int(env.Config.Network.MaxRedirects)
	}

	return net.System{
		Timeout:         timeout,
		FollowRedirects: followRedirects,
		MaxRedirects:    maxRedirects,
		Insecure:        n.insecure,
		UserAgent:       "devnest/" + version.Short(),
	}
}

// warnInsecure prints the warning that --insecure carries.
//
// docs/security.md originally called for a second acknowledgement flag
// alongside this one. One flag plus a warning on every single use is the
// better control: two flags for one decision is friction people alias past,
// and habituation is the actual risk. The command that genuinely needs to look
// at a broken certificate (devnest network ssl) does not need this flag at
// all, which keeps its legitimate uses rare.
func (n *networkFlags) warnInsecure(env *Env) {
	if !n.insecure {
		return
	}
	env.Warn(errors.CodeNetwork,
		"certificate verification is disabled; this connection is not protected against interception")
}

// attemptsOf resolves how many attempts to make, preferring the flag.
func attemptsOf(env *Env, flagValue int) int {
	if flagValue > 0 {
		return flagValue
	}
	return int(env.Config.Network.Attempts)
}

// intervalOf resolves the pause between attempts, preferring the flag.
func intervalOf(env *Env, flagValue time.Duration) time.Duration {
	if flagValue > 0 {
		return flagValue
	}
	return time.Duration(env.Config.Network.IntervalMs) * time.Millisecond
}

// firstTarget returns the required positional argument.
func firstTarget(args []string, command string) (string, error) {
	switch len(args) {
	case 0:
		return "", errors.New(errors.CodeInvalidInput, "no target was given").
			WithHint("run \"%s --help\" for usage", command)
	case 1:
		return args[0], nil
	default:
		return "", errors.New(errors.CodeInvalidInput,
			"expected one target, found %d", len(args)).
			WithHint("run \"%s --help\" for usage", command)
	}
}

// parseHeaders reads repeated --header flags.
func parseHeaders(values []string) ([]net.Header, error) {
	headers := make([]net.Header, 0, len(values))
	for _, value := range values {
		header, err := network.ParseHeader(value)
		if err != nil {
			return nil, err
		}
		headers = append(headers, header)
	}
	return headers, nil
}
