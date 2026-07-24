package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/output"
)

func newFileSizeCommand() *Command {
	var (
		selection selectionFlags
		depth     int
		topDirs   int
		topFiles  int
	)

	return &Command{
		Name:    "size",
		Summary: "Show where the disk space in a directory went",
		Usage:   "devnest file size [path] [flags]",
		Description: "Measure a directory tree and report the largest directories and the " +
			"largest files, with each directory's share of the total.\n\n" +
			"The measurement is always recursive; --depth controls how much detail is " +
			"reported, not how much is measured, so the figures always add up. Only the " +
			"running totals and the top files are kept in memory, so a tree with a " +
			"million files costs no more than a tree with a thousand.",
		Examples: []Example{
			{
				Command:     "devnest file size C:\\projects",
				Description: "Show which project directories are using the space.",
			},
			{
				Command:     "devnest file size . --depth 2 --top-files 20",
				Description: "Break the result down two levels deep and list the twenty largest files.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.registerWithoutDepth(set, true)
			set.IntVar(&depth, "depth", 1, "how many directory levels to report")
			set.IntVar(&topDirs, "top-directories", 10, "how many directories to list")
			set.IntVar(&topFiles, "top-files", 10, "how many files to list")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if len(args) > 1 {
				return tooManyPaths(args, "devnest file size")
			}

			result, err := file.Size(ctx, filesystem(), file.SizeRequest{
				Selection:      selection.selection(env, firstPath(args)),
				Depth:          depth,
				TopDirectories: topDirs,
				TopFiles:       topFiles,
			})
			if err != nil {
				return err
			}

			reportProblems(env, result.Problems)
			return env.Emit(result, sizeText(result))
		},
	}
}

func sizeText(result file.SizeResult) output.TextFunc {
	return func(w io.Writer) error {
		fmt.Fprintf(w, "%s\n\n", result.Root)

		if len(result.Directories) > 0 {
			rows := make([][]string, 0, len(result.Directories))
			for _, directory := range result.Directories {
				rows = append(rows, []string{
					directory.Relative,
					output.Bytes(directory.Bytes),
					strconv.FormatFloat(directory.Percent, 'f', 1, 64) + "%",
					output.Count(directory.Files),
				})
			}

			fmt.Fprintln(w, "Largest directories")
			err := output.WriteTable(w, []output.Column{
				{Title: "directory"},
				{Title: "size", Right: true},
				{Title: "share", Right: true},
				{Title: "files", Right: true},
			}, rows)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		if len(result.LargestFiles) > 0 {
			rows := make([][]string, 0, len(result.LargestFiles))
			for _, item := range result.LargestFiles {
				rows = append(rows, []string{item.Relative, output.Bytes(item.Bytes)})
			}

			fmt.Fprintln(w, "Largest files")
			err := output.WriteTable(w, []output.Column{
				{Title: "file"},
				{Title: "size", Right: true},
			}, rows)
			if err != nil {
				return err
			}
			fmt.Fprintln(w)
		}

		fmt.Fprintf(w, "%s in %s across %s\n",
			output.Bytes(result.TotalBytes),
			pluralFiles(result.TotalFiles),
			pluralDirectories(result.TotalDirectories))

		return writeProblems(w, result.Problems)
	}
}

func pluralDirectories(count int) string {
	if count == 1 {
		return "1 directory"
	}
	return output.Count(count) + " directories"
}
