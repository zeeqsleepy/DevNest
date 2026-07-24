package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/logging"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/version"
)

// Options configures one run. Streams and the environment are injected so the
// whole application can be exercised by a test without spawning a process.
type Options struct {
	Args      []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	LookupEnv func(string) (string, bool)
}

// Execute runs one invocation and returns its error. Mapping that error to a
// process exit code is the entrypoint's job; nothing here calls os.Exit.
func Execute(ctx context.Context, opts Options) error {
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}

	started := time.Now()
	root := NewRoot()

	var globals globalFlags
	result := split(root, opts.Args, &globals)
	command := result.command

	set := command.flagSet(&globals)
	if err := set.Parse(result.flagArgs); err != nil {
		return errors.Wrap(err, errors.CodeInvalidInput, "%s", err.Error()).
			WithHint("run \"%s --help\" to see the available flags", command.path())
	}

	if globals.showVersion && command == root {
		command = root.find("version")
		result.positional = nil
	}

	// "devnest <group> help" is the same request as "devnest <group> --help".
	// The rule is general rather than a help subcommand on each group, so it
	// works the moment a group exists.
	//
	// It applies to any node with subcommands, including the ones that are
	// runnable themselves. "devnest env" prints a summary and "devnest env
	// help" prints help, which is what everybody expects; the cost is that a
	// runnable group cannot take "help" as its argument, and a directory
	// genuinely called help is reachable as "./help".
	if len(command.Commands) > 0 && len(result.positional) == 1 && result.positional[0] == "help" {
		return writeHelp(opts.Stdout, command)
	}

	if globals.help || command.Run == nil {
		if err := unknownCommand(command, result.positional); err != nil {
			return err
		}
		return writeHelp(opts.Stdout, command)
	}

	env, err := newEnv(command, started, &globals, opts)
	if err != nil {
		return err
	}

	return command.Run(ctx, env, result.positional)
}

// unknownCommand turns a leftover positional on a group into a usage error,
// rather than quietly printing help for something the user did not ask for.
func unknownCommand(command *Command, positional []string) error {
	if command.Run != nil || len(positional) == 0 {
		return nil
	}
	return errors.New(errors.CodeInvalidInput,
		"unknown command %q for %q", positional[0], command.path()).
		WithHint("run \"%s --help\" to see the available commands", command.path())
}

// newEnv resolves configuration, then builds the logger and the renderer from
// it. The order matters: verbosity comes from configuration, so a logger built
// any earlier would log at the wrong level.
func newEnv(command *Command, started time.Time, globals *globalFlags, opts Options) (*Env, error) {
	resolved, warnings, err := config.Load(config.Source{
		Path:      globals.configPath,
		Explicit:  globals.configPath != "",
		LookupEnv: opts.LookupEnv,
	})
	if err != nil {
		return nil, err
	}

	applyFlags(&resolved, globals)
	if err := resolved.Validate(); err != nil {
		return nil, err
	}

	logger, err := newLogger(resolved, globals, opts)
	if err != nil {
		return nil, err
	}
	for _, warning := range warnings {
		logger.Warn(warning.Message, "source", warning.Source)
	}

	renderer, err := output.NewRenderer(resolved.General.Output)
	if err != nil {
		return nil, err
	}

	return &Env{
		Config:   resolved,
		Logger:   logger,
		Renderer: renderer,
		Stdin:    opts.Stdin,
		Stdout:   opts.Stdout,
		Stderr:   opts.Stderr,
		command:  strings.TrimPrefix(command.path(), "devnest "),
		started:  started,
	}, nil
}

// applyFlags overlays command-line flags, the highest-precedence source.
func applyFlags(resolved *config.Config, globals *globalFlags) {
	if globals.output != "" {
		resolved.General.Output = globals.output
	}
	if globals.noColor {
		resolved.General.Color = "never"
	}
	switch {
	case globals.verbose:
		resolved.General.Verbosity = "debug"
	case globals.quiet:
		resolved.General.Verbosity = "error"
	}
}

func newLogger(resolved config.Config, globals *globalFlags, opts Options) (*slog.Logger, error) {
	level, err := logging.ParseLevel(resolved.General.Verbosity)
	if err != nil {
		return nil, err
	}

	format := logging.FormatText
	if resolved.General.Output == "json" {
		format = logging.FormatJSON
	}
	if globals.logFormat != "" {
		if format, err = logging.ParseFormat(globals.logFormat); err != nil {
			return nil, err
		}
	}

	// Logs always go to stderr, so colour follows stderr's capabilities and
	// never those of a redirected stdout.
	return logging.New(opts.Stderr, logging.Options{
		Level:      level,
		Format:     format,
		Color:      output.UseColor(resolved.General.Color, opts.Stderr, opts.LookupEnv),
		Timestamps: globals.logTimestamps,
	}), nil
}

// ReportError writes a failure for the user. Text output goes to stderr so
// stdout stays clean; JSON output goes to stdout, because a consumer parsing
// stdout should never find something there that is not JSON.
func ReportError(err error, opts Options) {
	if err == nil {
		return
	}
	report := errors.Classify(err)
	format, verbose := preflight(opts.Args)

	if format == "json" {
		writeErrorJSON(report, opts, verbose)
		return
	}

	fprintf(opts.Stderr, "Error: %s\n", report.Message)
	if report.Hint != "" {
		fprintf(opts.Stderr, "  %s\n", report.Hint)
	}
	if verbose && report.Detail != "" && report.Detail != report.Message {
		fprintf(opts.Stderr, "  %s\n", report.Detail)
	}
}

func writeErrorJSON(report errors.Report, opts Options, verbose bool) {
	info := output.ErrorInfo{
		Code:    string(report.Code),
		Message: report.Message,
		Hint:    report.Hint,
	}
	if verbose {
		info.Detail = report.Detail
	}

	envelope := output.NewEnvelope(output.Meta{Version: version.Short()}, nil).WithError(info)
	renderer, err := output.NewRenderer("json")
	if err != nil {
		fprintf(opts.Stderr, "Error: %s\n", report.Message)
		return
	}
	if err := renderer.Render(opts.Stdout, envelope, nil); err != nil {
		fprintf(opts.Stderr, "Error: %s\n", report.Message)
	}
}

// preflight reads the output format and verbosity straight from argv.
//
// A run can fail before configuration is resolved (a malformed flag, an
// unreadable config file) and the error still has to be reported in the shape
// the user asked for. At that point argv is the only source available.
func preflight(args []string) (format string, verbose bool) {
	for index, argument := range args {
		switch {
		case argument == "-v", argument == "--verbose":
			verbose = true
		case argument == "-o", argument == "--output":
			if index+1 < len(args) {
				format = args[index+1]
			}
		case strings.HasPrefix(argument, "--output="):
			format = strings.TrimPrefix(argument, "--output=")
		case strings.HasPrefix(argument, "-o="):
			format = strings.TrimPrefix(argument, "-o=")
		}
	}
	return format, verbose
}

func fprintf(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	// Nothing useful can be done if writing the error message itself fails.
	_, _ = fmt.Fprintf(w, format, args...)
}
