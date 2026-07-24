package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newFileOrganizeCommand() *Command {
	var (
		selection  selectionFlags
		grouping   string
		onConflict string
		apply      bool
		force      bool
		assumeYes  bool
	)

	return &Command{
		Name:    "organize",
		Summary: "Group files into folders by category or extension",
		Usage:   "devnest file organize [path] [flags]",
		Description: "Group the files in a directory into folders. By default the layout is " +
			"a category folder with an extension folder inside it, so a photo lands in " +
			"Images/jpg and a manual lands in Documents/pdf.\n\n" +
			"Dry run is the default: without --apply the command prints the moves it " +
			"would make and changes nothing. Files are moved, never copied and never " +
			"deleted, and an existing file is never replaced.\n\n" +
			"Only the files directly in the directory are touched unless --recursive is " +
			"given, so an existing folder structure is left as it is. Hidden files are " +
			"skipped unless --include-hidden is given.",
		Examples: []Example{
			{
				Command:     "devnest file organize C:\\Users\\me\\Downloads",
				Description: "Show how Downloads would be organised, without changing anything.",
			},
			{
				Command:     "devnest file organize C:\\Users\\me\\Downloads --apply",
				Description: "Perform the moves, after showing the plan and asking for confirmation.",
			},
			{
				Command:     "devnest file organize . --by extension --on-conflict rename --apply --yes",
				Description: "Group into flat extension folders unattended, numbering any name clashes.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.register(set, false)
			set.StringVar(&grouping, "by", string(file.GroupByCategory),
				"folder layout: category or extension")
			set.StringVar(&onConflict, "on-conflict", string(file.ConflictSkip),
				"what to do about a taken name: skip, rename, or fail")
			set.BoolVar(&apply, "apply", false, "perform the moves (default is a dry run)")
			set.BoolVar(&force, "force", false, "permit running in a protected directory")
			set.BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
			set.BoolVar(&assumeYes, "y", false, "skip the confirmation prompt (shorthand)")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if len(args) > 1 {
				return tooManyPaths(args, "devnest file organize")
			}

			group, err := file.ParseGrouping(grouping)
			if err != nil {
				return err
			}
			conflict, err := file.ParseConflict(onConflict)
			if err != nil {
				return err
			}

			request := file.OrganizeRequest{
				Selection:  selection.selection(env, firstPath(args)),
				Grouping:   group,
				OnConflict: conflict,
				Force:      force,
			}

			// The plan is computed first and shown, so the confirmation is an
			// informed one rather than a yes to an unknown number of moves.
			plan, err := file.Organize(ctx, filesystem(), request)
			if err != nil {
				return err
			}

			if !apply {
				return env.Emit(plan, organizeText(plan))
			}
			if plan.Planned == 0 {
				return env.Emit(plan, organizeText(plan))
			}

			// The plan is only worth printing when someone is about to be
			// asked about it; unattended, it would just repeat the result.
			if env.NeedsConfirmation(assumeYes) {
				if err := writeOrganizePlan(env.Stderr, plan); err != nil {
					return err
				}
			}
			question := fmt.Sprintf("Move %s files (%s) in %s?",
				output.Count(plan.Planned), output.Bytes(plan.Bytes), plan.Root)
			if err := env.Confirm(question, assumeYes); err != nil {
				return err
			}

			request.Apply = true
			done, err := file.Organize(ctx, filesystem(), request)
			if err != nil {
				return err
			}
			reportProblems(env, done.Problems)
			return env.Emit(done, organizeText(done))
		},
	}
}

// writeOrganizePlan shows the plan on stderr before the prompt, so stdout
// still carries only the final result.
func writeOrganizePlan(w io.Writer, plan file.OrganizeResult) error {
	rows := make([][]string, 0, len(plan.Folders))
	for _, folder := range plan.Folders {
		rows = append(rows, []string{
			folder.Folder,
			output.Count(folder.Files),
			output.Bytes(folder.Bytes),
		})
	}

	fmt.Fprintf(w, "Planned layout for %s\n\n", plan.Root)
	err := output.WriteTable(w, []output.Column{
		{Title: "folder"},
		{Title: "files", Right: true},
		{Title: "size", Right: true},
	}, rows)
	if err != nil {
		return err
	}
	fmt.Fprintln(w)
	return nil
}

func organizeText(result file.OrganizeResult) output.TextFunc {
	return func(w io.Writer) error {
		if len(result.Moves) == 0 {
			fmt.Fprintf(w, "Nothing to organise in %s\n", result.Root)
			return nil
		}

		rows := make([][]string, 0, len(result.Folders))
		for _, folder := range result.Folders {
			rows = append(rows, []string{
				folder.Folder,
				output.Count(folder.Files),
				output.Bytes(folder.Bytes),
			})
		}

		heading := "Planned"
		if result.Applied {
			heading = "Organised"
		}
		fmt.Fprintf(w, "%s %s\n\n", heading, result.Root)

		if len(rows) > 0 {
			err := output.WriteTable(w, []output.Column{
				{Title: "folder"},
				{Title: "files", Right: true},
				{Title: "size", Right: true},
			}, rows)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		summary := fmt.Sprintf("%s to move", pluralFiles(result.Planned))
		if result.Applied {
			summary = fmt.Sprintf("%s moved", pluralFiles(result.Moved))
		}
		fmt.Fprintf(w, "%s, %s\n", summary, output.Bytes(result.Bytes))

		if result.Skipped > 0 {
			fmt.Fprintf(w, "%s skipped\n", output.Count(result.Skipped))
		}
		if result.Failed > 0 {
			fmt.Fprintf(w, "%s failed\n", output.Count(result.Failed))
		}
		if !result.Applied && result.Planned > 0 {
			fmt.Fprintln(w, "\nNothing has changed. Pass --apply to perform these moves.")
		}
		return writeProblems(w, result.Problems)
	}
}

func pluralFiles(count int) string {
	if count == 1 {
		return "1 file"
	}
	return output.Count(count) + " files"
}

func tooManyPaths(args []string, command string) error {
	return errors.New(errors.CodeInvalidInput,
		"expected at most one path, found %d", len(args)).
		WithHint("run \"%s --help\" for usage", command)
}
