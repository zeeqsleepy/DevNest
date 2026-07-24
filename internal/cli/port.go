package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/devnest/devnest/internal/core/port"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/net"
	"github.com/devnest/devnest/internal/platform/proc"
)

// newPortCommand builds the "port" group.
func newPortCommand() *Command {
	return &Command{
		Name:    "port",
		Summary: "See what is listening, check a port, free one",
		Usage:   "devnest port <command> [port] [flags]",
		Description: "Report the listening sockets on this machine, answer whether one " +
			"particular port is taken, and end the process holding a port when you ask " +
			"for it.\n\n" +
			"\"port list\" and \"port check\" are read-only. \"port free\" terminates a " +
			"process: it names the process first, asks before doing anything, and asks " +
			"the process to exit rather than killing it unless --force is passed.\n\n" +
			"Sockets whose owning process the system will not name are still listed, " +
			"marked as unknown. A listing that quietly dropped them would answer \"what " +
			"is listening\" with something untrue.\n\n" +
			"Ports below 1024 are left out of a listing unless --all is passed. They are " +
			"the system's own services and they are the same on every machine; the count " +
			"of what was hidden is part of the result, so nothing disappears silently.",
		Commands: []*Command{
			newPortListCommand(),
			newPortCheckCommand(),
			newPortFreeCommand(),
		},
	}
}

// portSystem is the real machine, which is what every port command gets in
// production. Tests call the module directly with a fake.
func portSystem() (port.Enumerator, port.Terminator) { return net.System{}, proc.System{} }

// portArgument reads the one port number these commands take.
func portArgument(args []string) (int, error) {
	switch len(args) {
	case 1:
		number, err := strconv.Atoi(args[0])
		if err != nil {
			return 0, errors.New(errors.CodeInvalidInput, "%q is not a port number", args[0]).
				WithHint("ports run from %d to %d", port.MinPort, port.MaxPort)
		}
		return number, port.ValidatePort(number)
	case 0:
		return 0, errors.New(errors.CodeInvalidInput, "no port was given").
			WithHint("pass the port number, for example: devnest port check 3000")
	default:
		return 0, errors.New(errors.CodeInvalidInput,
			"expected one port, found %d arguments", len(args)).
			WithHint("this command acts on a single port")
	}
}

// listenerColumns is the shape every listing renders as, so the same five
// columns mean the same thing in all three commands.
func listenerColumns() []output.Column {
	return []output.Column{
		{Title: "proto"},
		{Title: "port", Right: true},
		{Title: "address"},
		{Title: "pid", Right: true},
		{Title: "process"},
	}
}

func listenerRows(listeners []port.Listener) [][]string {
	rows := make([][]string, 0, len(listeners))
	for _, listener := range listeners {
		rows = append(rows, []string{
			listener.Protocol,
			strconv.Itoa(listener.Port),
			address(listener),
			pidOf(listener),
			processOf(listener),
		})
	}
	return rows
}

// address renders where a socket can be reached from, which is the fact people
// are looking for and the one a raw address makes them work out.
func address(listener port.Listener) string {
	switch listener.Scope {
	case port.ScopeAllInterfaces:
		return listener.Address + " (all interfaces)"
	case port.ScopeLoopback:
		return listener.Address + " (this machine only)"
	default:
		return listener.Address
	}
}

func pidOf(listener port.Listener) string {
	if listener.PID <= 0 {
		return "-"
	}
	return strconv.Itoa(listener.PID)
}

// processOf never invents a name. "unknown" is a real answer here: it means
// the operating system would not say, usually because the socket belongs to
// another user.
func processOf(listener port.Listener) string {
	if listener.Process != "" {
		return listener.Process
	}
	return "unknown"
}

func newPortListCommand() *Command {
	var (
		tcpOnly bool
		udpOnly bool
		all     bool
	)

	return &Command{
		Name:    "list",
		Summary: "Listening sockets with the process holding each",
		Usage:   "devnest port list [flags]",
		Description: "List every socket this machine is listening on, with the process " +
			"that owns it where the system will say.\n\n" +
			"The address column says how reachable each one is: a socket on all " +
			"interfaces is reachable from the network, a loopback socket only from this " +
			"machine. That is usually the thing worth noticing about a development " +
			"server.\n\n" +
			"Ports below 1024 are hidden unless --all is passed, and the number hidden is " +
			"reported. Results are rows, so --output csv works.",
		Examples: []Example{
			{
				Command:     "devnest port list",
				Description: "Everything listening above port 1024.",
			},
			{
				Command:     "devnest port list --tcp --all --output json",
				Description: "Every TCP listener including system services, for a script.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&tcpOnly, "tcp", false, "TCP sockets only")
			set.BoolVar(&udpOnly, "udp", false, "UDP sockets only")
			set.BoolVar(&all, "all", false, "include ports below 1024")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput,
					"this command takes no arguments").
					WithHint("to ask about one port, run \"devnest port check %s\"", args[0])
			}

			enumerator, inspector := portSystem()
			result, err := port.List(ctx, enumerator, inspector, port.ListRequest{
				TCP:           tcpOnly,
				UDP:           udpOnly,
				IncludeSystem: all,
			})
			if err != nil {
				return err
			}

			warnUnknownOwners(env, result.UnknownOwners)

			return env.EmitTable(result, portListText(result), portListTable(result))
		},
	}
}

// warnUnknownOwners says so when part of the listing could not be attributed,
// because an incomplete answer that looks complete is the failure mode here.
func warnUnknownOwners(env *Env, unknown int) {
	if unknown == 0 {
		return
	}
	env.Warn(errors.CodePermissionDenied,
		fmt.Sprintf("%d socket(s) could not be attributed to a process; "+
			"they belong to another user and DevNest does not ask for elevation", unknown))
}

func portListText(result port.ListResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 {
			fmt.Fprintln(w, "Nothing is listening.")
			return hiddenNote(w, result.SystemHidden)
		}

		if err := output.WriteTable(w, listenerColumns(), listenerRows(result.Listeners)); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s listener(s)\n", output.Count(result.Count))
		return hiddenNote(w, result.SystemHidden)
	}
}

func hiddenNote(w io.Writer, hidden int) error {
	if hidden == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w,
		"%s system port(s) below 1024 were not shown; pass --all to include them\n",
		output.Count(hidden))
	return err
}

func portListTable(result port.ListResult) output.TableFunc {
	return func() output.Table {
		return output.Table{Columns: listenerColumns(), Rows: listenerRows(result.Listeners)}
	}
}

func newPortCheckCommand() *Command {
	var (
		tcpOnly bool
		udpOnly bool
	)

	return &Command{
		Name:    "check",
		Summary: "Whether one port is in use, and by what",
		Usage:   "devnest port check <port> [flags]",
		Description: "Answer whether a port is taken.\n\n" +
			"The exit code carries the answer: 0 when the port is free and 3 when it is " +
			"in use, so a script can branch without parsing anything.\n\n" +
			"Unlike the listing, this answers about ports below 1024 without --all. " +
			"Somebody asking about port 80 knows what port 80 is.",
		Examples: []Example{
			{
				Command:     "devnest port check 3000",
				Description: "Find out what is holding the port your server wants.",
			},
			{
				Command:     "devnest port check 5432 --output json",
				Description: "The same answer for a script, with the owning process.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&tcpOnly, "tcp", false, "TCP sockets only")
			set.BoolVar(&udpOnly, "udp", false, "UDP sockets only")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			number, err := portArgument(args)
			if err != nil {
				return err
			}

			enumerator, inspector := portSystem()
			result, err := port.Check(ctx, enumerator, inspector, port.CheckRequest{
				Port: number,
				TCP:  tcpOnly,
				UDP:  udpOnly,
			})
			if err != nil {
				return err
			}

			if err := env.EmitTable(result, portCheckText(result), portCheckTable(result)); err != nil {
				return err
			}

			// The answer is the exit code as much as the output. A port in use
			// is a result rather than a failure, which is why this is the last
			// thing that happens rather than an error that skips the report.
			if result.InUse {
				return errors.New(errors.CodeNotFound, "port %d is in use", result.Port)
			}
			return nil
		},
	}
}

func portCheckText(result port.CheckResult) output.TextFunc {
	return func(w io.Writer) error {
		if !result.InUse {
			_, err := fmt.Fprintf(w, "Port %d is free.\n", result.Port)
			return err
		}

		fmt.Fprintf(w, "Port %d is in use.\n\n", result.Port)
		return output.WriteTable(w, listenerColumns(), listenerRows(result.Listeners))
	}
}

func portCheckTable(result port.CheckResult) output.TableFunc {
	return func() output.Table {
		return output.Table{Columns: listenerColumns(), Rows: listenerRows(result.Listeners)}
	}
}

func newPortFreeCommand() *Command {
	var (
		tcpOnly   bool
		udpOnly   bool
		force     bool
		assumeYes bool
		grace     time.Duration
	)

	return &Command{
		Name:    "free",
		Summary: "End the process holding a port",
		Usage:   "devnest port free <port> [flags]",
		Description: "Terminate the process listening on a port.\n\n" +
			"This is the most destructive command in DevNest and it behaves like it. " +
			"The process is identified by pid and name and shown to you, then you are " +
			"asked to confirm. The process is asked to exit and given time to do so; it " +
			"is killed only when --force is passed, and a killed process gets no chance " +
			"to flush or save anything.\n\n" +
			"A port held by more than one process is refused rather than guessed at. " +
			"Process 0 and process 1 are refused unconditionally. A process belonging to " +
			"another user is refused by the operating system, and DevNest never asks for " +
			"elevation.\n\n" +
			"On Windows there is no way for one process to ask another to exit politely, " +
			"so --force is required there and the command says so rather than pretending " +
			"the request was made.",
		Examples: []Example{
			{
				Command:     "devnest port free 3000",
				Description: "Ask whatever is holding port 3000 to exit.",
			},
			{
				Command:     "devnest port free 3000 --force --yes",
				Description: "Kill it without asking, for a script that knows what it wants.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.BoolVar(&tcpOnly, "tcp", false, "TCP sockets only")
			set.BoolVar(&udpOnly, "udp", false, "UDP sockets only")
			set.BoolVar(&force, "force", false, "kill the process if it does not exit on request")
			set.BoolVar(&assumeYes, "yes", false, "do not ask for confirmation")
			set.DurationVar(&grace, "grace", 0,
				"how long to wait for the process to exit (default 2s)")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			number, err := portArgument(args)
			if err != nil {
				return err
			}

			enumerator, terminator := portSystem()

			// The holder is identified and shown before the question is asked.
			// "Terminate the process on port 3000?" is not a question anybody
			// can answer; "terminate node, pid 8124?" is.
			holder, err := port.Check(ctx, enumerator, terminator, port.CheckRequest{
				Port: number,
				TCP:  tcpOnly,
				UDP:  udpOnly,
			})
			if err != nil {
				return err
			}
			if !holder.InUse {
				return errors.New(errors.CodeNotFound, "nothing is listening on port %d", number)
			}

			if env.NeedsConfirmation(assumeYes) {
				if err := output.WriteTable(env.Stderr, listenerColumns(),
					listenerRows(holder.Listeners)); err != nil {
					return err
				}
			}
			if err := env.Confirm(freeQuestion(holder, force), assumeYes); err != nil {
				return err
			}

			result, err := port.Free(ctx, enumerator, terminator, port.FreeRequest{
				Port:  number,
				TCP:   tcpOnly,
				UDP:   udpOnly,
				Force: force,
				Grace: grace,
			})
			if err != nil {
				return err
			}

			if !result.Freed {
				env.Warn(errors.CodeConflict,
					"the process is gone but the port is still held; "+
						"a socket can linger for a moment after the process that opened it")
			}

			return env.Emit(result, portFreeText(result))
		},
	}
}

func freeQuestion(holder port.CheckResult, force bool) string {
	action := "Ask"
	if force {
		action = "Kill"
	}

	if len(holder.Listeners) == 1 && holder.Listeners[0].Process != "" {
		listener := holder.Listeners[0]
		if force {
			return fmt.Sprintf("Kill %s (pid %d) holding port %d, losing anything unsaved?",
				listener.Process, listener.PID, holder.Port)
		}
		return fmt.Sprintf("Ask %s (pid %d) holding port %d to exit?",
			listener.Process, listener.PID, holder.Port)
	}

	return fmt.Sprintf("%s the process holding port %d?", action, holder.Port)
}

func portFreeText(result port.FreeResult) output.TextFunc {
	return func(w io.Writer) error {
		manner := "exited on request"
		if !result.Graceful {
			manner = "was killed"
		}

		return output.WriteFields(w, []output.Field{
			{Label: "port", Value: strconv.Itoa(result.Port)},
			{Label: "process", Value: processOf(result.Target)},
			{Label: "pid", Value: pidOf(result.Target)},
			{Label: "outcome", Value: manner},
			{Label: "took", Value: fmt.Sprintf("%d ms", result.WaitedMs)},
			{Label: "port now", Value: freedText(result.Freed)},
		})
	}
}

func freedText(freed bool) string {
	if freed {
		return "free"
	}
	return "still held"
}
