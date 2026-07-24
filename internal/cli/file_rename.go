package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

func newFileRenameCommand() *Command {
	var (
		selection selectionFlags
		prefix    string
		suffix    string
		replace   repeatable
		match     string
		sequence  bool
		seqStart  int
		seqPad    int
		seqSep    string
		seqPos    string
		lowercase bool
		uppercase bool
		apply     bool
		force     bool
		assumeYes bool
	)

	return &Command{
		Name:    "rename",
		Summary: "Rename many files at once",
		Usage:   "devnest file rename [path] [flags]",
		Description: "Rename files in bulk. Rules are applied to the name without its " +
			"extension, in a fixed order: replacements, then case, then the sequence " +
			"number, then the prefix and suffix. The order is fixed so the same flags " +
			"always produce the same names.\n\n" +
			"The preview is the default: without --apply nothing is renamed. The whole " +
			"plan is checked for conflicts first, and if any two files would end up with " +
			"the same name, or a name is already taken, the entire operation is refused " +
			"with nothing changed.\n\n" +
			"The result lists every old and new name. Run with --output json and keep " +
			"the file to have a record you can undo the batch from.",
		Examples: []Example{
			{
				Command:     "devnest file rename ./photos --prefix holiday- --sequence",
				Description: "Preview renaming every file to holiday-0001, holiday-0002, and so on.",
			},
			{
				Command:     "devnest file rename ./photos --replace \"IMG_=\" --apply",
				Description: "Strip the IMG_ prefix from every name, after confirming.",
			},
			{
				Command:     "devnest file rename . --match \"*.txt\" --lowercase --apply --output json > rollback.json",
				Description: "Lowercase every text file name and keep a record for undoing it.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.register(set, false)
			set.StringVar(&prefix, "prefix", "", "text to put before every name")
			set.StringVar(&suffix, "suffix", "", "text to put after every name, before the extension")
			set.Var(&replace, "replace", "literal substitution as \"from=to\" (repeatable)")
			set.StringVar(&match, "match", "", "only rename names matching this glob")
			set.BoolVar(&sequence, "sequence", false, "add a running number to every name")
			set.IntVar(&seqStart, "sequence-start", 1, "first number in the sequence")
			set.IntVar(&seqPad, "sequence-pad", 4, "minimum digits in the sequence number")
			set.StringVar(&seqSep, "sequence-separator", "", "text between the number and the name")
			set.StringVar(&seqPos, "sequence-position", file.SequenceAfter,
				"where the number goes: prefix or suffix")
			set.BoolVar(&lowercase, "lowercase", false, "lowercase every name")
			set.BoolVar(&uppercase, "uppercase", false, "uppercase every name")
			set.BoolVar(&apply, "apply", false, "perform the renames (default is a preview)")
			set.BoolVar(&force, "force", false, "permit running in a protected directory")
			set.BoolVar(&assumeYes, "yes", false, "skip the confirmation prompt")
			set.BoolVar(&assumeYes, "y", false, "skip the confirmation prompt (shorthand)")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if len(args) > 1 {
				return tooManyPaths(args, "devnest file rename")
			}

			replacements, err := parseReplacements(replace)
			if err != nil {
				return err
			}
			position, err := parseSequencePosition(seqPos)
			if err != nil {
				return err
			}
			if seqPad < 0 {
				return errors.New(errors.CodeInvalidInput, "--sequence-pad cannot be negative")
			}

			request := file.RenameRequest{
				Selection: selection.selection(env, firstPath(args)),
				Match:     match,
				Prefix:    prefix,
				Suffix:    suffix,
				Replace:   replacements,
				Sequence: file.Sequence{
					Enabled:   sequence,
					Start:     seqStart,
					Padding:   seqPad,
					Separator: seqSep,
					Position:  position,
				},
				Lowercase: lowercase,
				Uppercase: uppercase,
				Force:     force,
			}

			plan, err := file.RenameFiles(ctx, filesystem(), request)
			if err != nil {
				return err
			}

			if !apply || plan.Planned == 0 {
				return env.Emit(plan, renameText(plan))
			}

			if env.NeedsConfirmation(assumeYes) {
				if err := writeRenamePreview(env.Stderr, plan); err != nil {
					return err
				}
			}
			question := fmt.Sprintf("Rename %s in %s?",
				pluralFiles(plan.Planned), plan.Root)
			if err := env.Confirm(question, assumeYes); err != nil {
				return err
			}

			request.Apply = true
			done, err := file.RenameFiles(ctx, filesystem(), request)
			if err != nil {
				return err
			}
			reportProblems(env, done.Problems)
			return env.Emit(done, renameText(done))
		},
	}
}

// parseReplacements reads "from=to" pairs. An empty replacement is how a
// substring is deleted, so "IMG_=" is a valid and common request.
func parseReplacements(values []string) ([]file.Replacement, error) {
	replacements := make([]file.Replacement, 0, len(values))

	for _, value := range values {
		from, to, found := strings.Cut(value, "=")
		if !found || from == "" {
			return nil, errors.New(errors.CodeInvalidInput,
				"invalid replacement %q", value).
				WithHint("expected \"from=to\", for example --replace \"IMG_=photo-\"")
		}
		replacements = append(replacements, file.Replacement{From: from, To: to})
	}

	return replacements, nil
}

func parseSequencePosition(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", file.SequenceAfter:
		return file.SequenceAfter, nil
	case file.SequenceBefore:
		return file.SequenceBefore, nil
	}
	return "", errors.New(errors.CodeInvalidInput,
		"unknown sequence position %q", value).
		WithHint("expected one of: prefix, suffix")
}

func writeRenamePreview(w io.Writer, plan file.RenameResult) error {
	fmt.Fprintf(w, "Planned renames in %s\n\n", plan.Root)
	if err := output.WriteTable(w, renameColumns(), renameRows(plan.Renames, 20)); err != nil {
		return err
	}
	if plan.Planned > 20 {
		fmt.Fprintf(w, "... and %s more\n", output.Count(plan.Planned-20))
	}
	fmt.Fprintln(w)
	return nil
}

func renameText(result file.RenameResult) output.TextFunc {
	return func(w io.Writer) error {
		if len(result.Renames) == 0 {
			fmt.Fprintf(w, "Nothing to rename in %s\n", result.Root)
			return writeProblems(w, result.Problems)
		}

		heading := "Planned renames in"
		if result.Applied {
			heading = "Renamed in"
		}
		fmt.Fprintf(w, "%s %s\n\n", heading, result.Root)

		if err := output.WriteTable(w, renameColumns(), renameRows(result.Renames, 0)); err != nil {
			return err
		}
		fmt.Fprintln(w)

		if result.Applied {
			fmt.Fprintf(w, "%s renamed\n", pluralFiles(result.Renamed))
		} else {
			fmt.Fprintf(w, "%s to rename\n", pluralFiles(result.Planned))
		}
		if result.Unchanged > 0 {
			fmt.Fprintf(w, "%s already named that way\n", pluralFiles(result.Unchanged))
		}
		if result.Failed > 0 {
			fmt.Fprintf(w, "%s failed\n", pluralFiles(result.Failed))
		}
		if !result.Applied && result.Planned > 0 {
			fmt.Fprintln(w, "\nNothing has changed. Pass --apply to perform these renames.")
			fmt.Fprintln(w, "Add --output json and redirect it to a file to keep a rollback record.")
		}
		return writeProblems(w, result.Problems)
	}
}

func renameColumns() []output.Column {
	return []output.Column{
		{Title: "from"},
		{Title: "to"},
		{Title: "status"},
	}
}

func renameRows(renames []file.Rename, limit int) [][]string {
	rows := make([][]string, 0, len(renames))
	for _, rename := range renames {
		if limit > 0 && len(rows) == limit {
			break
		}
		rows = append(rows, []string{rename.OldName, rename.NewName, rename.Status})
	}
	return rows
}
