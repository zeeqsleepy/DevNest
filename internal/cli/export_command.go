package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newExportCommand() *Command {
	return &Command{
		Name:    "export",
		Summary: "Run several commands and write one combined report",
		Usage:   "devnest export <command...> --export <path>",
		Description: "Run each named command in order and write one document holding all " +
			"of their results.\n\n" +
			"A command with a space in it is one argument: \"devnest export \\\"secret " +
			"scan\\\" scan\" runs two commands. Each runs with its defaults, because a " +
			"flag written here could not be told apart from the next command's name; " +
			"anything that needs flags is a single run with --export.\n\n" +
			"A command that fails does not stop the ones after it. A partial report with " +
			"a section clearly marked as failed is more useful than no report, and the " +
			"exit code is the worst of the individual ones.",
		Examples: []Example{
			{
				Command:     "devnest export scan clean --export reports/project-health.md",
				Description: "One report holding a project summary and what could be cleaned.",
			},
			{
				Command:     "devnest export env doctor --export support.json",
				Description: "Everything worth attaching to a bug report, in one file.",
			},
		},
		Run: runExport,
	}
}

// combinedReport is the document "devnest export" produces.
type combinedReport struct {
	Commands []commandReport `json:"commands"`
	// Status is the worst of the individual statuses, so a consumer can branch
	// on one field without walking the list.
	Status string `json:"status"`
	Failed int    `json:"failed"`
}

// commandReport is one command's result inside the combined document. It holds
// what that command's own envelope held, so a section of a report and a single
// run of the command say exactly the same thing.
type commandReport struct {
	Command    string            `json:"command"`
	Status     string            `json:"status"`
	DurationMs int64             `json:"durationMs"`
	Data       any               `json:"data,omitempty"`
	Warnings   []output.Warning  `json:"warnings"`
	Error      *output.ErrorInfo `json:"error,omitempty"`
}

func runExport(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return errors.New(errors.CodeInvalidInput, "export needs at least one command").
			WithHint("for example: devnest export scan clean --export reports/health.md")
	}

	commands := make([]*Command, 0, len(args))
	for _, argument := range args {
		command, err := resolveForExport(argument)
		if err != nil {
			return err
		}
		commands = append(commands, command)
	}

	report := combinedReport{
		Commands: make([]commandReport, 0, len(commands)),
		Status:   output.StatusOK,
	}
	var worst error

	for _, command := range commands {
		section, err := runSection(ctx, env, command)
		report.Commands = append(report.Commands, section)

		if section.Status == output.StatusError {
			report.Failed++
			if errors.ExitCode(err) >= errors.ExitCode(worst) {
				worst = err
			}
		}
		report.Status = worseStatus(report.Status, section.Status)
	}

	if err := env.Emit(report, exportReportText(report)); err != nil {
		return err
	}
	return worst
}

// runSection runs one command with a renderer that keeps the envelope instead
// of printing it, so the combined document is built from the same value the
// command would have shown on its own.
func runSection(ctx context.Context, env *Env, command *Command) (commandReport, error) {
	renderer := &capturingRenderer{}
	started := time.Now()

	child := *env
	child.Renderer = renderer
	child.Stdout = io.Discard
	child.Stdin = strings.NewReader("")
	child.warnings = nil
	child.export = nil
	child.command = strings.TrimPrefix(command.path(), "devnest ")
	child.started = started

	err := command.Run(ctx, &child, nil)

	section := commandReport{
		Command:    child.command,
		Status:     renderer.envelope.Status,
		DurationMs: time.Since(started).Milliseconds(),
		Data:       renderer.envelope.Data,
		Warnings:   renderer.envelope.Warnings,
	}
	if section.Warnings == nil {
		section.Warnings = []output.Warning{}
	}
	if section.Status == "" {
		section.Status = output.StatusOK
	}

	if err != nil {
		report := errors.Classify(err)
		section.Status = output.StatusError
		section.Error = &output.ErrorInfo{
			Code:    string(report.Code),
			Message: report.Message,
			Hint:    report.Hint,
		}
	}
	return section, err
}

// resolveForExport turns one argument into a runnable command.
func resolveForExport(argument string) (*Command, error) {
	words := strings.Fields(argument)
	if len(words) == 0 {
		return nil, errors.New(errors.CodeInvalidInput, "empty command name")
	}
	if words[0] == "export" {
		return nil, errors.New(errors.CodeInvalidInput, "export cannot export itself")
	}

	command := NewRoot()
	for _, word := range words {
		sub := command.find(word)
		if sub == nil {
			return nil, errors.New(errors.CodeInvalidInput,
				"unknown command %q", argument).
				WithHint("run \"devnest help\" to see the available commands")
		}
		command = sub
	}

	if command.Run == nil {
		return nil, errors.New(errors.CodeInvalidInput,
			"%q is a group of commands rather than one that runs", argument).
			WithHint("name one of its subcommands, for example \"%s %s\"",
				argument, command.Commands[0].Name)
	}
	return command, nil
}

// worseStatus keeps the worst of two statuses: an error beats a warning, and a
// warning beats a clean run.
func worseStatus(current, next string) string {
	rank := map[string]int{output.StatusOK: 0, output.StatusWarning: 1, output.StatusError: 2}
	if rank[next] > rank[current] {
		return next
	}
	return current
}

// capturingRenderer keeps an envelope rather than writing it.
type capturingRenderer struct {
	envelope output.Envelope
}

// Name reports json, because that is the shape a section of the combined
// document is in. A command that asks its renderer what it is gets a truthful
// answer about where its result is going.
func (c *capturingRenderer) Name() string { return "json" }

func (c *capturingRenderer) Render(_ io.Writer, envelope output.Envelope, _ output.TextFunc) error {
	c.envelope = envelope
	return nil
}

func exportReportText(report combinedReport) output.TextFunc {
	return func(w io.Writer) error {
		rows := make([][]string, 0, len(report.Commands))
		for _, section := range report.Commands {
			detail := ""
			if section.Error != nil {
				detail = section.Error.Message
			} else if count := len(section.Warnings); count > 0 {
				detail = fmt.Sprintf("%d warning(s)", count)
			}
			rows = append(rows, []string{
				section.Command,
				section.Status,
				fmt.Sprintf("%d ms", section.DurationMs),
				detail,
			})
		}

		err := output.WriteTable(w, []output.Column{
			{Title: "command"},
			{Title: "status"},
			{Title: "took", Right: true},
			{Title: "detail"},
		}, rows)
		if err != nil {
			return err
		}

		summary := fmt.Sprintf("\n%d command(s), %d failed.\n", len(report.Commands), report.Failed)
		if _, err := io.WriteString(w, summary); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}
		return nil
	}
}
