// Command devnest is the DevNest command line toolkit.
//
// This file owns process concerns only: argv, signals, and the exit code.
// Everything past startup lives in internal/cli.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/devnest/devnest/internal/cli"
	"github.com/devnest/devnest/internal/errors"
)

const issuesURL = "https://github.com/devnest/devnest/issues"

func main() {
	os.Exit(run())
}

func run() (code int) {
	opts := cli.Options{
		Args:      os.Args[1:],
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		LookupEnv: os.LookupEnv,
	}

	// A panic is a bug, never a bad input. Recovering here gives the user
	// something they can file instead of a stack trace.
	defer func() {
		if recovered := recover(); recovered != nil {
			reportPanic(recovered, opts)
			code = errors.ExitFailure
		}
	}()

	ctx, stop := signalContext()
	defer stop()

	err := cli.Execute(ctx, opts)
	cli.ReportError(err, opts)
	return errors.ExitCode(err)
}

// signalContext cancels its context on the first interrupt so work can unwind
// cleanly, and exits immediately on the second. Someone pressing Ctrl+C twice
// means it, and the first press has already had its chance.
func signalContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-signals
		cancel()
		<-signals
		os.Exit(errors.ExitCancelled)
	}()

	return ctx, func() {
		signal.Stop(signals)
		cancel()
	}
}

func reportPanic(recovered any, opts cli.Options) {
	fmt.Fprintf(opts.Stderr, "Error: internal error in DevNest\n")
	fmt.Fprintf(opts.Stderr, "  %v\n", recovered)
	fmt.Fprintf(opts.Stderr, "  This is a bug. Please report it: %s\n", issuesURL)
}
