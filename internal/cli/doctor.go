package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/devnest/devnest/internal/core/clean"
	"github.com/devnest/devnest/internal/core/doctor"
	"github.com/devnest/devnest/internal/core/secret"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
	"github.com/devnest/devnest/internal/platform/proc"
	"github.com/devnest/devnest/internal/platform/sys"
)

func newDoctorCommand() *Command {
	return &Command{
		Name:    "doctor",
		Summary: "Check that DevNest itself is in working order",
		Usage:   "devnest doctor [flags]",
		Description: "Check this installation: whether the configuration file parses, " +
			"whether the directory it lives in can be written to, whether the rule " +
			"tables are compiled in, and whether the external tools optional features " +
			"need are present.\n\n" +
			"The output is meant to be pasted into a bug report. Paths under the home " +
			"directory are shortened to \"~\" and the hostname is not reported, so a " +
			"report says what is wrong without saying who is running it.\n\n" +
			"A warning is something absent that DevNest works without, such as git on a " +
			"machine that never runs the git commands. Only a failure exits non-zero.",
		Examples: []Example{
			{
				Command:     "devnest doctor",
				Description: "Check the installation before filing a bug report.",
			},
			{
				Command:     "devnest doctor --output json",
				Description: "The same checks as JSON, for a script that gates on them.",
			},
		},
		Run:               runDoctor,
		RunsWithoutConfig: true,
	}
}

func runDoctor(ctx context.Context, env *Env, args []string) error {
	if len(args) > 0 {
		return errors.New(errors.CodeInvalidInput,
			"doctor takes no arguments, found %q", args[0]).
			WithHint("run \"devnest doctor --help\" for usage")
	}

	result := doctor.Run(ctx, installation{}, doctor.Request{
		ConfigPath:     env.ConfigPath,
		ConfigExplicit: env.ConfigExplicit,
		RuleSets: []doctor.RuleSet{
			{Name: "secret", Count: len(secret.Rules())},
			{Name: "clean", Count: len(clean.Rules())},
		},
		OutputFormat: env.Renderer.Name(),
		Color:        output.UseColor(env.Config.General.Color, env.Stdout, os.LookupEnv),
	})

	if err := env.Emit(result, doctorText(result)); err != nil {
		return err
	}

	// The exit code comes after the report, so that whoever is looking at a
	// broken installation gets to see which part of it is broken.
	if !result.Healthy {
		return errors.New(errors.CodeCheckFailed,
			"%d check(s) failed", result.Failed)
	}
	return nil
}

// installation is the real machine. The methods are written out rather than
// embedded, because fs.System and proc.System both have a Stat and a Describe
// and embedding both would make either one ambiguous.
type installation struct{}

func (installation) Exists(path string) (bool, error) { return fs.System{}.Exists(path) }

func (installation) Writable(directory string) error { return fs.System{}.Writable(directory) }

func (installation) Describe() sys.Info { return sys.System{}.Describe() }

func (installation) Lookup(name string) []string { return proc.System{}.Lookup(name) }

func (installation) Run(ctx context.Context, command proc.Command) (proc.Output, error) {
	return proc.System{}.Run(ctx, command)
}

func doctorText(result doctor.Result) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "version", Value: result.Version},
			{Label: "commit", Value: result.Commit},
			{Label: "platform", Value: result.Platform},
			{Label: "go", Value: result.GoVersion},
			{Label: "shell", Value: orUnknown(result.Shell)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		rows := make([][]string, 0, len(result.Checks))
		for _, check := range result.Checks {
			rows = append(rows, []string{string(check.Status), check.Name, check.Detail})
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}
		err := output.WriteTable(w, []output.Column{
			{Title: "status"},
			{Title: "check"},
			{Title: "detail"},
		}, rows)
		if err != nil {
			return err
		}

		// Hints go under the table rather than in it: a column wide enough to
		// hold one wraps every other line in the report.
		for _, check := range result.Checks {
			if check.Hint == "" || check.Status == doctor.StatusOK {
				continue
			}
			if _, err := fmt.Fprintf(w, "\n%s: %s", check.Name, check.Hint); err != nil {
				return errors.Wrap(err, errors.CodeIO, "cannot write output")
			}
		}

		if _, err := fmt.Fprintf(w, "\n\n%s\n", doctorSummary(result)); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}
		return nil
	}
}

func doctorSummary(result doctor.Result) string {
	passed := len(result.Checks) - result.Failed - result.Warned

	switch {
	case result.Failed > 0:
		return fmt.Sprintf("%d passed, %d warning(s), %d failed. Something here needs fixing.",
			passed, result.Warned, result.Failed)
	case result.Warned > 0:
		return fmt.Sprintf("%d passed, %d warning(s), nothing failed. A warning is something "+
			"DevNest works without.", passed, result.Warned)
	default:
		return fmt.Sprintf("%d passed. This installation is in working order.", passed)
	}
}
