package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/devnest/devnest/internal/core/clean"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

// newCleanCommand builds the "clean" group.
//
// The group itself is runnable and is the dry run: "devnest clean ." reports
// what could be removed and removes nothing. Deleting is a different word,
// typed on purpose.
func newCleanCommand() *Command {
	var (
		patterns  repeatable
		protect   repeatable
		force     bool
		apply     bool
		assumeYes bool
	)

	return &Command{
		Name:    "clean",
		Summary: "Find build output and dependency directories, and reclaim the space",
		Usage:   "devnest clean [path] [flags]",
		Description: "Find the directories a build regenerates (node_modules, target, " +
			"dist, __pycache__, and the rest) and report what they cost.\n\n" +
			"Nothing is deleted without --apply. This is the only command in DevNest " +
			"that destroys data, and the defaults are built around that: a run with no " +
			"flags reports and stops.\n\n" +
			"A directory is a candidate only when its name is in the rule set. Generic " +
			"names need evidence beside them: \"build\" counts next to a package.json or " +
			"a Cargo.toml, and not in a directory of photographs that happens to have " +
			"one. Size, age, and emptiness are never used as evidence, because a wrong " +
			"guess here deletes somebody's work.\n\n" +
			"Version control directories are never entered. Symbolic links are never " +
			"followed or removed. Nothing outside the directory you named is touched, " +
			"and a candidate on a different filesystem is left alone. Running at a " +
			"filesystem root or in your home directory is refused unless you pass " +
			"--force, and no configuration file can lift that.\n\n" +
			"Run \"devnest clean rules\" to see exactly what would ever be considered.",
		Examples: []Example{
			{
				Command:     "devnest clean .",
				Description: "See what could be reclaimed here. Nothing is deleted.",
			},
			{
				Command:     "devnest clean ~/projects/api --apply",
				Description: "Remove the artifacts after confirming the list.",
			},
			{
				Command:     "devnest clean . --pattern node_modules --apply --yes",
				Description: "Remove one kind of directory without being asked.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.Var(&patterns, "pattern", "restrict to a named rule; repeatable")
			set.Var(&protect, "protect", "a path that must never be removed; repeatable")
			set.BoolVar(&force, "force", false,
				"allow a run in a home directory or at a filesystem root")
			set.BoolVar(&apply, "apply", false, "actually remove what was found")
			set.BoolVar(&assumeYes, "yes", false, "do not ask for confirmation")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			root, err := cleanRoot(args)
			if err != nil {
				return err
			}

			request := clean.ScanRequest{
				Root:       root,
				Patterns:   patterns,
				Configured: env.Config.Clean.Patterns,
				Protect:    append(protect, env.Config.Clean.Protect...),
				Force:      force,
			}

			if !apply {
				return runCleanScan(ctx, env, request)
			}
			return runCleanApply(ctx, env, request, assumeYes)
		},
		Commands: []*Command{
			newCleanApplyCommand(),
			newCleanRulesCommand(),
		},
	}
}

// cleanReader is the real filesystem. It satisfies both interfaces the module
// declares; which one a function gets is decided by that function's signature,
// not here.
func cleanReader() fs.System { return fs.System{} }

// cleanRoot takes the optional path argument. The current directory is the
// default, because that is where somebody standing in a project is.
func cleanRoot(args []string) (string, error) {
	switch len(args) {
	case 0:
		return ".", nil
	case 1:
		return args[0], nil
	default:
		return "", errors.New(errors.CodeInvalidInput,
			"expected one directory, found %d arguments", len(args)).
			WithHint("run one command per project, or quote a path containing spaces")
	}
}

func runCleanScan(ctx context.Context, env *Env, request clean.ScanRequest) error {
	result, err := clean.Scan(ctx, cleanReader(), request)
	if err != nil {
		return err
	}

	warnSkipped(env, result.Skipped)
	return env.EmitTable(result, cleanScanText(result), cleanScanTable(result))
}

func runCleanApply(ctx context.Context, env *Env, request clean.ScanRequest, assumeYes bool) error {
	// The plan is built and shown before the question is asked. "Delete 1.2 GB
	// from this project?" is not a question anybody can answer honestly
	// without seeing the list.
	planned, err := clean.Scan(ctx, cleanReader(), request)
	if err != nil {
		return err
	}

	if planned.Count == 0 {
		warnSkipped(env, planned.Skipped)
		return env.EmitTable(planned, cleanScanText(planned), cleanScanTable(planned))
	}

	if env.NeedsConfirmation(assumeYes) {
		if err := cleanScanText(planned)(env.Stderr); err != nil {
			return err
		}
	}
	question := fmt.Sprintf("Remove %s directory(s) and free %s?",
		output.Count(planned.Count), output.Bytes(planned.TotalBytes))
	if err := env.Confirm(question, assumeYes); err != nil {
		return err
	}

	result, err := clean.Apply(ctx, cleanReader(), clean.ApplyRequest{
		ScanRequest: request,
		Confirmed:   true,
	})
	if err != nil {
		return err
	}

	warnSkipped(env, result.Skipped)
	warnFailures(env, result.Failed)

	return env.EmitTable(result, cleanApplyText(result), cleanApplyTable(result))
}

// warnSkipped surfaces every guard that fired. A candidate silently dropped is
// indistinguishable from a bug, and the reasons are the interesting part of a
// safety design.
func warnSkipped(env *Env, skipped []clean.Skip) {
	for _, skip := range skipped {
		env.Warn(errors.CodeInvalidInput,
			fmt.Sprintf("left %s alone: %s", skip.Path, skip.Reason))
	}
}

func warnFailures(env *Env, failed []clean.Failure) {
	for _, failure := range failed {
		env.Warn(errors.CodeIO,
			fmt.Sprintf("could not remove %s: %s", failure.Path, failure.Reason))
	}
}

func newCleanApplyCommand() *Command {
	var (
		patterns  repeatable
		protect   repeatable
		force     bool
		assumeYes bool
	)

	return &Command{
		Name:    "apply",
		Summary: "Remove what clean would report",
		Usage:   "devnest clean apply [path] [flags]",
		Description: "Remove the artifacts under a directory. Identical to " +
			"\"devnest clean --apply\", and it exists because a destructive command " +
			"reads better as a verb than as a flag on a listing.\n\n" +
			"The plan is shown and confirmed before anything is deleted. Every guard " +
			"described in \"devnest clean --help\" applies here, and each candidate is " +
			"checked again in the moment before it is removed, because a tree can change " +
			"between being listed and being deleted.",
		Examples: []Example{
			{
				Command:     "devnest clean apply .",
				Description: "Remove the artifacts in this project after confirming.",
			},
			{
				Command:     "devnest clean apply ~/projects/api --yes",
				Description: "Remove them without being asked, for a scripted cleanup.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.Var(&patterns, "pattern", "restrict to a named rule; repeatable")
			set.Var(&protect, "protect", "a path that must never be removed; repeatable")
			set.BoolVar(&force, "force", false,
				"allow a run in a home directory or at a filesystem root")
			set.BoolVar(&assumeYes, "yes", false, "do not ask for confirmation")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			root, err := cleanRoot(args)
			if err != nil {
				return err
			}

			return runCleanApply(ctx, env, clean.ScanRequest{
				Root:       root,
				Patterns:   patterns,
				Configured: env.Config.Clean.Patterns,
				Protect:    append(protect, env.Config.Clean.Protect...),
				Force:      force,
			}, assumeYes)
		},
	}
}

func newCleanRulesCommand() *Command {
	return &Command{
		Name:    "rules",
		Summary: "The directories clean would ever consider",
		Usage:   "devnest clean rules [flags]",
		Description: "List the built-in rules: the directory name, what produces it, " +
			"what has to sit beside it before the name counts, and what it costs to " +
			"regenerate.\n\n" +
			"This is the whole surface of what \"devnest clean\" can ever remove. " +
			"Nothing outside this table and your configured patterns is a candidate, " +
			"which is worth being able to read before running a destructive command.",
		Examples: []Example{
			{
				Command:     "devnest clean rules",
				Description: "See everything clean would consider removing.",
			},
			{
				Command:     "devnest clean rules --output json",
				Description: "The same table for a script or a review.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput, "this command takes no arguments")
			}

			listing := clean.Rules()
			return env.EmitTable(listing, cleanRulesText(listing), cleanRulesTable(listing))
		},
	}
}

func cleanScanText(result clean.ScanResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 {
			_, err := fmt.Fprintf(w, "Nothing to clean in %s.\n", result.Root)
			return err
		}

		if err := output.WriteTable(w, cleanColumns(), cleanRows(result.Candidates)); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s director(s), %s across %s file(s)\n",
			output.Count(result.Count),
			output.Bytes(result.TotalBytes),
			output.Count(result.TotalFiles))
		_, err := fmt.Fprintln(w, "Nothing has been deleted. Pass --apply to remove them.")
		return err
	}
}

func cleanColumns() []output.Column {
	return []output.Column{
		{Title: "directory"},
		{Title: "produced by"},
		{Title: "size", Right: true},
		{Title: "files", Right: true},
	}
}

func cleanRows(candidates []clean.Candidate) [][]string {
	rows := make([][]string, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, []string{
			candidate.Relative,
			candidate.Ecosystem,
			output.Bytes(candidate.Bytes),
			output.Count(candidate.Files),
		})
	}
	return rows
}

// cleanScanTable writes plain numbers rather than "1.2 GB", because a
// spreadsheet given a formatted size has to be told it is a number.
func cleanScanTable(result clean.ScanResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Candidates))
		for _, candidate := range result.Candidates {
			rows = append(rows, []string{
				candidate.Relative,
				candidate.Ecosystem,
				strconv.FormatInt(candidate.Bytes, 10),
				strconv.Itoa(candidate.Files),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "directory"},
				{Title: "ecosystem"},
				{Title: "bytes", Right: true},
				{Title: "files", Right: true},
			},
			Rows: rows,
		}
	}
}

func cleanApplyText(result clean.ApplyResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 && len(result.Failed) == 0 {
			_, err := fmt.Fprintf(w, "Nothing was removed from %s.\n", result.Root)
			return err
		}

		for _, removed := range result.Removed {
			fmt.Fprintf(w, "removed  %s  (%s)\n", removed.Relative, output.Bytes(removed.Bytes))
		}

		fmt.Fprintf(w, "\n%s director(s) removed, %s freed\n",
			output.Count(result.Count), output.Bytes(result.BytesFreed))

		if len(result.Failed) > 0 {
			_, err := fmt.Fprintf(w, "%s could not be removed; see the warnings above\n",
				output.Count(len(result.Failed)))
			return err
		}
		return nil
	}
}

func cleanApplyTable(result clean.ApplyResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Removed))
		for _, removed := range result.Removed {
			rows = append(rows, []string{
				removed.Relative,
				removed.Ecosystem,
				strconv.FormatInt(removed.Bytes, 10),
				strconv.Itoa(removed.Files),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "directory"},
				{Title: "ecosystem"},
				{Title: "bytes", Right: true},
				{Title: "files", Right: true},
			},
			Rows: rows,
		}
	}
}

func cleanRulesText(rules []clean.Rule) output.TextFunc {
	return func(w io.Writer) error {
		return output.WriteTable(w, cleanRulesColumns(), cleanRulesRows(rules))
	}
}

func cleanRulesTable(rules []clean.Rule) output.TableFunc {
	return func() output.Table {
		return output.Table{Columns: cleanRulesColumns(), Rows: cleanRulesRows(rules)}
	}
}

func cleanRulesColumns() []output.Column {
	return []output.Column{
		{Title: "directory"},
		{Title: "produced by"},
		{Title: "needs beside it"},
		{Title: "regenerated by"},
	}
}

func cleanRulesRows(rules []clean.Rule) [][]string {
	rows := make([][]string, 0, len(rules))
	for _, rule := range rules {
		rows = append(rows, []string{
			rule.Name,
			rule.Ecosystem,
			markerSummary(rule),
			rule.Regenerable,
		})
	}
	return rows
}

// markerSummary keeps the column readable. The full list for a generic name is
// a dozen build files, and printing all of them turns a table into a wall.
func markerSummary(rule clean.Rule) string {
	switch count := len(rule.Markers); {
	case count == 0:
		return "nothing: the name is unambiguous"
	case count <= 2:
		return joinMarkers(rule.Markers)
	default:
		return fmt.Sprintf("%s, or %d other project files",
			joinMarkers(rule.Markers[:2]), count-2)
	}
}

func joinMarkers(markers []string) string {
	switch len(markers) {
	case 0:
		return ""
	case 1:
		return markers[0]
	default:
		return markers[0] + ", " + markers[1]
	}
}
