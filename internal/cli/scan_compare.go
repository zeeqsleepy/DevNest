package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/devnest/devnest/internal/core/scan"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

func newScanCompareCommand() *Command {
	var selection scanFlags

	return &Command{
		Name:    "compare",
		Summary: "How a project grew between two scans",
		Usage:   "devnest scan compare <snapshot.json> [path] [flags]",
		Description: "Compare a saved scan of a project with the same tree now, " +
			"and report how it grew.\n\n" +
			"Pass the snapshot you saved earlier — with \"devnest scan --output json > " +
			"snapshot.json\" or \"devnest scan --export snapshot.json\" — and this runs " +
			"a fresh scan of the tree with the same walk settings and shows the " +
			"difference: more or fewer files, larger or smaller, and which categories " +
			"grew.\n\n" +
			"The comparison is of aggregates, not of individual files: it answers " +
			"\"is this project getting bigger and where\", not \"which file was " +
			"renamed\". For a repository nobody tracks, that is the question that " +
			"matters. Results are rows, so --output csv works.\n\n" +
			"Read-only, always. Nothing here writes, moves, or removes anything.",
		Examples: []Example{
			{
				Command:     "devnest scan --output json > baseline.json",
				Description: "Save today's scan as the baseline.",
			},
			{
				Command:     "devnest scan compare baseline.json",
				Description: "How the project has grown since that baseline.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.register(set)
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			snapshot, path, err := scanCompareTargets(args)
			if err != nil {
				return err
			}

			data, err := fs.System{}.ReadFile(snapshot)
			if err != nil {
				return errors.Wrap(err, errors.CodeIO,
					"cannot read the saved scan %s", snapshot)
			}
			before, err := scan.Load(data)
			if err != nil {
				return err
			}

			result, err := scan.Compare(ctx, filesystem(), scan.CompareRequest{
				Selection: selection.selection(env, path),
				Before:    before,
				Now:       path,
			})
			if err != nil {
				return err
			}

			reportScanProblems(env, result.Problems)
			return env.EmitTable(result, scanCompareText(result), scanCompareTable(result))
		},
	}
}

// scanCompareTargets splits the snapshot file and the optional tree path.
func scanCompareTargets(args []string) (snapshot, path string, err error) {
	switch len(args) {
	case 0:
		return "", "", errors.New(errors.CodeInvalidInput,
			"no snapshot was given").
			WithHint("run \"devnest scan --output json > baseline.json\" first, " +
				"then \"devnest scan compare baseline.json\"")
	case 1:
		return args[0], ".", nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", errors.New(errors.CodeInvalidInput,
			"expected a snapshot and at most one path, found %d arguments", len(args)).
			WithHint("run one comparison per tree")
	}
}

func scanCompareText(result scan.CompareResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "root", Value: result.Root},
			{Label: "files", Value: fmt.Sprintf("%s -> %s (%s)",
				output.Count(result.FilesBefore), output.Count(result.FilesAfter),
				countDelta(result.FilesDelta))},
			{Label: "directories", Value: fmt.Sprintf("%s -> %s (%s)",
				output.Count(result.DirectoriesBefore), output.Count(result.DirectoriesAfter),
				countDelta(result.DirectoriesDelta))},
			{Label: "size", Value: fmt.Sprintf("%s -> %s (%s)",
				output.Bytes(result.BytesBefore), output.Bytes(result.BytesAfter),
				bytesDelta(result.BytesDelta))},
			{Label: "authored", Value: fmt.Sprintf("%s -> %s files (%s), %s -> %s (%s)",
				output.Count(result.AuthoredBefore), output.Count(result.AuthoredAfter),
				countDelta(result.AuthoredDelta),
				output.Bytes(result.AuthoredBytesBefore), output.Bytes(result.AuthoredBytesAfter),
				bytesDelta(result.AuthoredBytesDelta))},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		if len(result.Categories) > 0 {
			if err := writeCompareDeltas(w, "By category", result.Categories); err != nil {
				return err
			}
		}
		if len(result.Languages) > 0 {
			if err := writeCompareDeltas(w, "Languages that changed", result.Languages); err != nil {
				return err
			}
		}

		_, err := fmt.Fprintf(w, "\ncompared in %d ms\n", result.DurationMs)
		return err
	}
}

func writeCompareDeltas(w io.Writer, title string, deltas []scan.CountDelta) error {
	if _, err := fmt.Fprintf(w, "\n%s\n", title); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}
	rows := make([][]string, 0, len(deltas))
	for _, delta := range deltas {
		rows = append(rows, []string{
			delta.Name,
			output.Count(delta.FilesBefore),
			output.Count(delta.FilesAfter),
			countDelta(delta.FilesDelta),
			output.Bytes(delta.BytesBefore),
			output.Bytes(delta.BytesAfter),
			bytesDelta(delta.BytesDelta),
		})
	}
	return output.WriteTable(w, []output.Column{
		{Title: "name"},
		{Title: "files before", Right: true},
		{Title: "files after", Right: true},
		{Title: "files Δ", Right: true},
		{Title: "size before", Right: true},
		{Title: "size after", Right: true},
		{Title: "size Δ", Right: true},
	}, rows)
}

// countDelta renders a signed whole-number difference, so "0" reads as no
// change and "-3" reads as fewer.
func countDelta(delta int) string {
	if delta > 0 {
		return "+" + output.Count(delta)
	}
	if delta == 0 {
		return "0"
	}
	return "-" + output.Count(-delta)
}

// bytesDelta renders a signed byte difference.
func bytesDelta(delta int64) string {
	if delta > 0 {
		return "+" + output.Bytes(delta)
	}
	if delta == 0 {
		return "0"
	}
	return "-" + output.Bytes(-delta)
}

func scanCompareTable(result scan.CompareResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0,
			len(result.Categories)+len(result.Languages))
		add := func(section string, deltas []scan.CountDelta) {
			for _, delta := range deltas {
				rows = append(rows, []string{
					section,
					delta.Name,
					strconv.Itoa(delta.FilesBefore),
					strconv.Itoa(delta.FilesAfter),
					fmt.Sprintf("%+d", delta.FilesDelta),
					strconv.FormatInt(delta.BytesBefore, 10),
					strconv.FormatInt(delta.BytesAfter, 10),
					fmt.Sprintf("%+d", delta.BytesDelta),
				})
			}
		}
		add("category", result.Categories)
		add("language", result.Languages)
		return output.Table{
			Columns: []output.Column{
				{Title: "section"},
				{Title: "name"},
				{Title: "files_before", Right: true},
				{Title: "files_after", Right: true},
				{Title: "files_delta", Right: true},
				{Title: "bytes_before", Right: true},
				{Title: "bytes_after", Right: true},
				{Title: "bytes_delta", Right: true},
			},
			Rows: rows,
		}
	}
}
