package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

func newFileDuplicateCommand() *Command {
	var (
		selection selectionFlags
		algorithm string
		minSize   int64
	)

	return &Command{
		Name:    "duplicate",
		Summary: "Find files with identical content",
		Usage:   "devnest file duplicate [path] [flags]",
		Description: "Find duplicate files by comparing content, not names. Two files with " +
			"different names and identical bytes are duplicates; two files with the same " +
			"name and different bytes are not.\n\n" +
			"Files are grouped by size first, and only groups that could possibly match " +
			"are read and hashed, so most files are never opened. Large files stream " +
			"through a fixed buffer, so memory use does not depend on file size.\n\n" +
			"Nothing is deleted. The oldest file in each group is reported as the " +
			"original and the rest as duplicates; what to do about them is your call.",
		Examples: []Example{
			{
				Command:     "devnest file duplicate C:\\Users\\me\\Pictures",
				Description: "List duplicate photos, largest waste first.",
			},
			{
				Command:     "devnest file duplicate . --min-size 1MB --output json",
				Description: "Report duplicates over a megabyte as JSON, for a script to act on.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.register(set, true)
			set.StringVar(&algorithm, "algorithm", string(fs.SHA256),
				"digest used to compare content: sha256, sha512, or md5")
			set.Var(newSizeValue(&minSize, 1), "min-size",
				"ignore files smaller than this, for example 1MB")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if len(args) > 1 {
				return tooManyPaths(args, "devnest file duplicate")
			}

			chosen, err := fs.ParseAlgorithm(algorithm)
			if err != nil {
				return err
			}

			result, err := file.Duplicates(ctx, filesystem(), file.DuplicateRequest{
				Selection: selection.selection(env, firstPath(args)),
				Algorithm: chosen,
				MinBytes:  minSize,
			})
			if err != nil {
				return err
			}

			reportProblems(env, result.Problems)
			return env.Emit(result, duplicateText(result))
		},
	}
}

func duplicateText(result file.DuplicateResult) output.TextFunc {
	return func(w io.Writer) error {
		if len(result.Groups) == 0 {
			fmt.Fprintf(w, "No duplicates found in %s (%s scanned)\n",
				result.Root, pluralFiles(result.FilesScanned))
			return writeProblems(w, result.Problems)
		}

		fmt.Fprintf(w, "Duplicates in %s\n\n", result.Root)

		for _, group := range result.Groups {
			fmt.Fprintf(w, "%s  %s\n", output.Bytes(group.Bytes), shortHash(group.Hash))
			fmt.Fprintf(w, "  original   %s\n", group.Original.Relative)
			for _, duplicate := range group.Duplicates {
				fmt.Fprintf(w, "  duplicate  %s\n", duplicate.Relative)
			}
			fmt.Fprintln(w)
		}

		fmt.Fprintf(w, "%s, %s duplicated, %s reclaimable\n",
			pluralGroups(len(result.Groups)), pluralFiles(result.Duplicates),
			output.Bytes(result.Wasted))
		fmt.Fprintf(w, "%s scanned, %s hashed\n",
			pluralFiles(result.FilesScanned), pluralFiles(result.FilesHashed))

		return writeProblems(w, result.Problems)
	}
}

// shortHash trims a digest for the terminal. The full value is always in the
// structured output, which is what anything automated should be reading.
func shortHash(hash string) string {
	if len(hash) <= 16 {
		return hash
	}
	return hash[:16] + "..."
}
