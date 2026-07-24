package cli

import (
	"context"
	"io"

	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/version"
)

func newVersionCommand() *Command {
	return &Command{
		Name:    "version",
		Summary: "Show version and build information",
		Usage:   "devnest version [flags]",
		Description: "Show the version, commit, and build date of this binary, " +
			"along with the Go version and platform it was built for.",
		Examples: []Example{
			{
				Command:     "devnest version",
				Description: "Show build information as a readable listing.",
			},
			{
				Command:     "devnest version --output json",
				Description: "Show the same information as JSON, for a script or a CI step.",
			},
		},
		Run: runVersion,
	}
}

func runVersion(_ context.Context, env *Env, args []string) error {
	if len(args) > 0 {
		return errors.New(errors.CodeInvalidInput,
			"version takes no arguments, found %q", args[0]).
			WithHint("run \"devnest version --help\" for usage")
	}

	info := version.Get()
	return env.Emit(info, func(w io.Writer) error {
		return output.WriteFields(w, []output.Field{
			{Label: "version", Value: info.Version},
			{Label: "commit", Value: info.Commit},
			{Label: "built", Value: info.BuildDate},
			{Label: "go", Value: info.GoVersion},
			{Label: "platform", Value: info.OS + "/" + info.Arch},
		})
	})
}
