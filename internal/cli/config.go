package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	baseconfig "github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/core/config"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

// newConfigCommand builds the "config" group. The group is runnable and shows
// the resolved configuration, because "what is my configuration" is the
// question and the rest are ways of acting on the answer.
func newConfigCommand() *Command {
	return &Command{
		Name:    "config",
		Summary: "Show and change DevNest's own configuration",
		Usage:   "devnest config [command] [flags]",
		Description: "Show the resolved configuration and where each value came from, " +
			"and change it without opening the file.\n\n" +
			"Values are resolved from four layers, each overriding the one before: the " +
			"compiled defaults, the configuration file, the environment, and the flags " +
			"on the command line. The origin column is the fastest answer to \"why is it " +
			"behaving like that\".\n\n" +
			"Editing preserves the file. Setting one key rewrites one line and leaves " +
			"the comments and the layout as they were, and a value the schema rejects is " +
			"refused before anything is written.",
		Examples: []Example{
			{
				Command:     "devnest config",
				Description: "Every value with the layer it came from.",
			},
			{
				Command:     "devnest config set general.output json",
				Description: "Make JSON the default output format.",
			},
		},
		Run: runConfigShow,
		Commands: []*Command{
			newConfigListCommand(),
			newConfigGetCommand(),
			newConfigSetCommand(),
			newConfigUnsetCommand(),
			newConfigPathCommand(),
			newConfigInitCommand(),
			newConfigValidateCommand(),
		},
		RunsWithoutConfig: true,
	}
}

// configFilesystem is the real disk. Configuration writing goes through the
// same atomic write every export does.
type configFilesystem struct{}

func (configFilesystem) Exists(path string) (bool, error) { return fs.System{}.Exists(path) }

func (configFilesystem) ReadFile(path string) ([]byte, error) { return fs.System{}.ReadFile(path) }

func (configFilesystem) WriteAtomic(path string, data []byte) error {
	return fs.System{}.WriteAtomic(path, data)
}

func configRequest(env *Env) config.Request {
	return config.Request{
		Path:      env.ConfigPath,
		LookupEnv: os.LookupEnv,
	}
}

func runConfigShow(_ context.Context, env *Env, args []string) error {
	if len(args) > 0 {
		return errors.New(errors.CodeInvalidInput,
			"unknown command %q for \"devnest config\"", args[0]).
			WithHint("run \"devnest config --help\" to see the available commands")
	}

	result, err := config.Show(configFilesystem{}, configRequest(env))
	if err != nil {
		return err
	}
	return env.EmitTable(result, configShowText(result), configValuesTable(result.Values))
}

func newConfigListCommand() *Command {
	return &Command{
		Name:        "list",
		Summary:     "List every key with its current value",
		Usage:       "devnest config list [flags]",
		Description: "List every configuration key, its resolved value, and its type.",
		Examples: []Example{
			{
				Command:     "devnest config list",
				Description: "See every key DevNest has.",
			},
			{
				Command:     "devnest config list --output json",
				Description: "The same listing for a script.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput,
					"config list takes no arguments, found %q", args[0])
			}
			result, err := config.Show(configFilesystem{}, configRequest(env))
			if err != nil {
				return err
			}
			return env.EmitTable(result, configListText(result), configValuesTable(result.Values))
		},
		RunsWithoutConfig: true,
	}
}

func newConfigGetCommand() *Command {
	return &Command{
		Name:        "get",
		Summary:     "Show one value",
		Usage:       "devnest config get <key>",
		Description: "Show one configuration value and the layer it came from.",
		Examples: []Example{
			{
				Command:     "devnest config get general.output",
				Description: "The current output format.",
			},
			{
				Command:     "devnest config get network.timeout_ms",
				Description: "How long a network command waits before giving up.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			key, err := oneKey(args, "get")
			if err != nil {
				return err
			}
			result, err := config.Get(configRequest(env), key)
			if err != nil {
				return err
			}
			return env.Emit(result, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s\n", configValueText(result.Value.Value))
				return wrapWrite(err)
			})
		},
		RunsWithoutConfig: true,
	}
}

func newConfigSetCommand() *Command {
	return &Command{
		Name:    "set",
		Summary: "Set a value in the configuration file",
		Usage:   "devnest config set <key> <value>",
		Description: "Write one value into the configuration file, creating the file if " +
			"there is not one yet.\n\n" +
			"The value is checked against the schema before anything is written, so a " +
			"rejected one leaves the previous file exactly as it was. A list is written " +
			"as comma-separated values.",
		Examples: []Example{
			{
				Command:     "devnest config set general.output json",
				Description: "Make JSON the default output format.",
			},
			{
				Command:     "devnest config set scan.exclude node_modules,dist,.git",
				Description: "Set a list; the parts are separated by commas.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) != 2 {
				return errors.New(errors.CodeInvalidInput,
					"config set takes a key and a value").
					WithHint("for example: devnest config set general.output json")
			}
			result, err := config.Set(configFilesystem{}, configRequest(env), args[0], args[1])
			if err != nil {
				return err
			}
			return env.Emit(result, configWriteText(result, "set"))
		},
		RunsWithoutConfig: true,
	}
}

func newConfigUnsetCommand() *Command {
	return &Command{
		Name:    "unset",
		Summary: "Remove a key, reverting to the default",
		Usage:   "devnest config unset <key>",
		Description: "Remove one key from the configuration file, so the compiled default " +
			"applies again. A key that is not in the file is not an error: the file " +
			"already says what was asked for.",
		Examples: []Example{
			{
				Command:     "devnest config unset general.output",
				Description: "Go back to the default output format.",
			},
			{
				Command:     "devnest config unset scan.exclude",
				Description: "Use the built-in exclusion list again.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			key, err := oneKey(args, "unset")
			if err != nil {
				return err
			}
			result, err := config.Unset(configFilesystem{}, configRequest(env), key)
			if err != nil {
				return err
			}
			return env.Emit(result, configWriteText(result, "unset"))
		},
		RunsWithoutConfig: true,
	}
}

func newConfigPathCommand() *Command {
	return &Command{
		Name:        "path",
		Summary:     "Show the configuration file in use",
		Usage:       "devnest config path",
		Description: "Print the path of the configuration file, and whether it exists.",
		Examples: []Example{
			{
				Command:     "devnest config path",
				Description: "Find the file, to open it in an editor.",
			},
			{
				Command:     "devnest config path --output json",
				Description: "The same for a script that edits the file itself.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput,
					"config path takes no arguments, found %q", args[0])
			}
			result, err := config.Path(configFilesystem{}, configRequest(env))
			if err != nil {
				return err
			}
			return env.Emit(result, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s\n", result.Path)
				return wrapWrite(err)
			})
		},
		RunsWithoutConfig: true,
	}
}

func newConfigInitCommand() *Command {
	return &Command{
		Name:    "init",
		Summary: "Write an annotated default configuration file",
		Usage:   "devnest config init",
		Description: "Write a configuration file holding every key at its current default, " +
			"with comments.\n\n" +
			"An existing file is never overwritten. It holds decisions somebody made by " +
			"hand, and replacing it because a command was typed twice is not a trade " +
			"this tool makes.",
		Examples: []Example{
			{
				Command:     "devnest config init",
				Description: "Start a configuration file at the default location.",
			},
			{
				Command:     "devnest config init --config ./devnest.toml",
				Description: "Write one somewhere else, to keep it with a project.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput,
					"config init takes no arguments, found %q", args[0])
			}
			result, err := config.Init(configFilesystem{}, configRequest(env))
			if err != nil {
				return err
			}
			return env.Emit(result, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "wrote %s\n", result.Path)
				return wrapWrite(err)
			})
		},
		RunsWithoutConfig: true,
	}
}

func newConfigValidateCommand() *Command {
	return &Command{
		Name:    "validate",
		Summary: "Check the configuration file for errors",
		Usage:   "devnest config validate",
		Description: "Parse the configuration file and check every value against the " +
			"schema. Keys DevNest does not recognise are reported as warnings rather " +
			"than errors, because an older binary must still run against a newer file.",
		Examples: []Example{
			{
				Command:     "devnest config validate",
				Description: "Check the file after editing it by hand.",
			},
			{
				Command:     "devnest config validate --config ./devnest.toml",
				Description: "Check a file before putting it on a machine.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput,
					"config validate takes no arguments, found %q", args[0])
			}
			result, err := config.Validate(configFilesystem{}, configRequest(env))
			if err != nil {
				return err
			}
			return env.Emit(result, configValidateText(result))
		},
		RunsWithoutConfig: true,
	}
}

func oneKey(args []string, command string) (string, error) {
	if len(args) != 1 {
		return "", errors.New(errors.CodeInvalidInput,
			"config %s takes one key", command).
			WithHint("for example: devnest config %s general.output", command)
	}
	return args[0], nil
}

func wrapWrite(err error) error {
	if err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}
	return nil
}

func configShowText(result config.ShowResult) output.TextFunc {
	return func(w io.Writer) error {
		state := "does not exist yet"
		if result.Exists {
			state = "in use"
		}
		if _, err := fmt.Fprintf(w, "%s (%s)\n\n", result.Path, state); err != nil {
			return wrapWrite(err)
		}

		if err := writeConfigValues(w, result.Values); err != nil {
			return err
		}

		_, err := fmt.Fprintf(w, "\n%d value(s) from the file, %d from the environment, "+
			"the rest compiled in.\n", result.FromFile, result.FromEnvironment)
		return wrapWrite(err)
	}
}

func configListText(result config.ShowResult) output.TextFunc {
	return func(w io.Writer) error {
		return writeConfigValues(w, result.Values)
	}
}

func writeConfigValues(w io.Writer, values []baseconfig.Value) error {
	rows := make([][]string, 0, len(values))
	for _, value := range values {
		rows = append(rows, []string{value.Key, configValueText(value.Value), value.Origin})
	}
	return output.WriteTable(w, []output.Column{
		{Title: "key"},
		{Title: "value"},
		{Title: "origin"},
	}, rows)
}

func configValuesTable(values []baseconfig.Value) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(values))
		for _, value := range values {
			rows = append(rows, []string{value.Key, configValueText(value.Value), value.Origin})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "key"},
				{Title: "value"},
				{Title: "origin"},
			},
			Rows: rows,
		}
	}
}

// configValueText renders a value the way a person reads it, which for a list
// is its parts rather than Go's own formatting.
func configValueText(value any) string {
	if list, ok := value.([]string); ok {
		if len(list) == 0 {
			return "(empty)"
		}
		return strings.Join(list, ", ")
	}
	return fmt.Sprint(value)
}

func configWriteText(result config.WriteResult, verb string) output.TextFunc {
	return func(w io.Writer) error {
		if !result.Changed {
			_, err := fmt.Fprintf(w, "%s was already unset in %s\n", result.Key, result.Path)
			return wrapWrite(err)
		}

		if verb == "unset" {
			_, err := fmt.Fprintf(w, "%s removed from %s; the default %s applies again\n",
				result.Key, result.Path, configValueText(result.Value))
			return wrapWrite(err)
		}

		if _, err := fmt.Fprintf(w, "%s = %s in %s\n",
			result.Key, configValueText(result.Value), result.Path); err != nil {
			return wrapWrite(err)
		}
		if result.Previous != nil && !result.Created {
			_, err := fmt.Fprintf(w, "was %s\n", configValueText(result.Previous))
			return wrapWrite(err)
		}
		return nil
	}
}

func configValidateText(result config.ValidateResult) output.TextFunc {
	return func(w io.Writer) error {
		if !result.Exists {
			_, err := fmt.Fprintf(w, "no file at %s; the compiled defaults apply\n", result.Path)
			return wrapWrite(err)
		}

		if _, err := fmt.Fprintf(w, "%s is valid: %d key(s) set\n", result.Path, result.Keys); err != nil {
			return wrapWrite(err)
		}
		for _, warning := range result.Warnings {
			if _, err := fmt.Fprintf(w, "  %s (%s)\n", warning.Message, warning.Source); err != nil {
				return wrapWrite(err)
			}
		}
		return nil
	}
}
