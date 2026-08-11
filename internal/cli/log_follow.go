package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newLogFollowCommand() *Command {
	var (
		lines    int
		interval time.Duration
	)

	return &Command{
		Name:    "follow",
		Summary: "Show a log's tail, then keep up with it as it grows",
		Usage:   "devnest log follow <file> [flags]",
		Description: "Print the last few lines of a log, then print every line " +
			"appended to it until you stop the command.\n\n" +
			"The tail is read once and every check after that reads only the bytes " +
			"that appeared since the last check, so a multi-gigabyte log costs the " +
			"same memory as a small one. A log that is rotated mid-run is picked " +
			"up from the start of its replacement.\n\n" +
			"This streams: lines are written as they appear, so there is no summary " +
			"envelope and the machine-readable formats have nothing meaningful to " +
			"carry. It is a person watching a log, or a process feeding one line to " +
			"another.\n\n" +
			"Stop it with Ctrl+C; the exit code is then 5 (cancelled).",
		Examples: []Example{
			{
				Command:     "devnest log follow server.log",
				Description: "Watch the last part of a log and everything new in it.",
			},
			{
				Command:     "devnest log follow app.log --lines 0",
				Description: "Show only the lines that appear after the command starts.",
			},
			{
				Command:     "devnest log follow access.log --lines 50 --interval 500ms",
				Description: "Seed from fifty existing lines and poll every half second.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&lines, "lines", 10, "how many existing lines to show before following")
			set.IntVar(&lines, "n", 10, "how many existing lines to show before following (shorthand)")
			set.DurationVar(&interval, "interval", 0, "how often to check for new lines")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			// A stream has no envelope, so the formats that exist for envelopes
			// have nothing to say here. Saying so up front is better than
			// emitting a document that never ends.
			switch env.Config.General.Output {
			case "json", "csv", "markdown":
				return errors.New(errors.CodeInvalidInput,
					"log follow streams lines, so --output %s has nothing to carry",
					env.Config.General.Output).
					WithHint("log follow is a person watching a log, or a pipeline fed line by line; " +
						"use the default table output")
			}
			if env.export != nil {
				return errors.New(errors.CodeInvalidInput,
					"log follow streams lines, so --export has nothing to write").
					WithHint("watch the output instead of exporting it, or use a command that produces a report")
			}

			file := ""
			switch len(args) {
			case 1:
				file = args[0]
			case 0:
				return errors.New(errors.CodeInvalidInput, "no log file was given").
					WithHint("pass the path of the log file to follow")
			default:
				return errors.New(errors.CodeInvalidInput,
					"expected one log file, found %d arguments", len(args)).
					WithHint("run one command per file, or quote a path containing spaces")
			}

			if lines < 0 {
				return errors.New(errors.CodeInvalidInput, "--lines cannot be negative").
					WithHint("pass how many existing lines to show, for example 20")
			}

			writer := &followWriter{w: env.Stdout}
			if lines > 0 {
				env.Progress(fmt.Sprintf("following %s, showing the last %s line(s)",
					file, output.Count(lines)))
			} else {
				env.Progress(fmt.Sprintf("following %s from its end", file))
			}

			err := log.Follow(ctx, logReader(), log.FollowRequest{
				Path:     file,
				Count:    lines,
				Interval: interval,
			}, writer.write)
			if err != nil {
				return err
			}
			env.Progress("stopped following " + file)
			return nil
		},
	}
}

// followWriter writes batches of followed lines straight to stdout. Every line
// already ends in a newline, so a batch can go out in one call.
type followWriter struct {
	w io.Writer
}

func (f *followWriter) write(lines []string) {
	if len(lines) == 0 {
		return
	}
	_, _ = io.WriteString(f.w, strings.Join(lines, ""))
}
