package cli

import (
	"fmt"
	"io"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
)

// reportProblems records the non-fatal failures a walk collected, so they
// appear in the result envelope as well as in the log. A file that could not
// be read is something the user needs to know about even when the command as a
// whole succeeded.
func reportProblems(env *Env, problems []file.Problem) {
	for _, problem := range problems {
		env.Warn(errors.Code(problem.Code), problem.Message, "path", problem.Path)
	}
}

// writeProblems adds a short summary of non-fatal failures to the human view.
// The full list is in the log and in the JSON output; repeating forty
// permission errors in a terminal helps nobody.
func writeProblems(w io.Writer, problems []file.Problem) error {
	if len(problems) == 0 {
		return nil
	}

	limit := 5
	fmt.Fprintf(w, "\n%s could not be read:\n", pluralEntries(len(problems)))
	for index, problem := range problems {
		if index == limit {
			fmt.Fprintf(w, "  ... and %s more\n", output.Count(len(problems)-limit))
			break
		}
		fmt.Fprintf(w, "  %s: %s\n", problem.Path, problem.Message)
	}
	return nil
}

func pluralEntries(count int) string {
	if count == 1 {
		return "1 entry"
	}
	return output.Count(count) + " entries"
}

func pluralGroups(count int) string {
	if count == 1 {
		return "1 group"
	}
	return output.Count(count) + " groups"
}

// fileRows turns a file list into table rows, which four of the six commands
// need in the same shape.
func fileRows(files []file.Info) [][]string {
	rows := make([][]string, 0, len(files))
	for _, item := range files {
		rows = append(rows, []string{
			item.Relative,
			output.Bytes(item.Bytes),
			item.Category,
		})
	}
	return rows
}

func fileColumns() []output.Column {
	return []output.Column{
		{Title: "path"},
		{Title: "size", Right: true},
		{Title: "category"},
	}
}
