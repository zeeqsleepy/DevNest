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
)

func newScanTreeCommand() *Command {
	var (
		selection scanFlags
		depth     int
		files     bool
		maxEntry  int
	)

	return &Command{
		Name:    "tree",
		Summary: "Directory tree, with totals for each branch",
		Usage:   "devnest scan tree [path] [flags]",
		Description: "Print the shape of a directory, with the file count and size of " +
			"everything under each branch.\n\n" +
			"The whole tree is walked whatever --depth says. A directory that is not " +
			"expanded still reports what is inside it, so the numbers beside a " +
			"collapsed branch are the real ones rather than the part that fitted.\n\n" +
			"Directories only by default, because that is the shape people are " +
			"looking for; --files includes the files. Both are capped per directory " +
			"by --max-entries, and a listing that was cut says so.\n\n" +
			"The same ignore rules apply as everywhere else in this group, which is " +
			"what keeps a tree of a Node project readable.",
		Examples: []Example{
			{
				Command:     "devnest scan tree --depth 2",
				Description: "The top two levels of this project, with totals.",
			},
			{
				Command:     "devnest scan tree ~/work/api --files --depth 1",
				Description: "What sits directly inside a directory, files included.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.registerWithoutDepth(set)
			set.IntVar(&depth, "depth", 3, "how many levels to show")
			set.BoolVar(&files, "files", false, "include files as well as directories")
			set.IntVar(&maxEntry, "max-entries", 100,
				"how many children to list per directory")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			request := scan.TreeRequest{
				Selection:  selection.selection(env, firstPath(args)),
				Depth:      depth,
				Files:      files,
				MaxEntries: maxEntry,
			}
			// --depth belongs to the display here, not to the walk. The
			// shared flag set registers it for the other commands, and the
			// tree registers its own, so the walk stays unlimited.
			request.MaxDepth = 0

			result, err := scan.Tree(ctx, filesystem(), request)
			if err != nil {
				return err
			}

			reportScanProblems(env, result.Problems)
			return env.EmitTable(result, scanTreeText(result), scanTreeTable(result))
		},
	}
}

func scanTreeText(result scan.TreeResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "root", Value: result.Root},
			{Label: "directories", Value: output.Count(result.Directories)},
			{Label: "files", Value: output.Count(result.Files)},
			{Label: "size", Value: output.Bytes(result.Bytes)},
			{Label: "shown depth", Value: output.Count(result.Depth)},
			{Label: "scanned in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		if _, err := fmt.Fprintln(w); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}
		return writeNodes(w, result.Nodes, "", result.Truncated)
	}
}

// writeNodes draws the tree.
//
// Box-drawing characters, which every terminal DevNest supports can render,
// and no colour: the shape is the information, and a tree that only reads
// correctly in one terminal is worse than a plain one.
func writeNodes(w io.Writer, nodes []scan.Node, prefix string, truncated bool) error {
	for index, node := range nodes {
		last := index == len(nodes)-1 && !truncated

		branch, continuation := "├── ", "│   "
		if last {
			branch, continuation = "└── ", "    "
		}

		name := node.Name
		if node.IsDir {
			name += "/"
		}

		_, err := fmt.Fprintf(w, "%s%s%s  %s, %s\n", prefix, branch, name,
			pluralFiles(node.Files), output.Bytes(node.Bytes))
		if err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}

		if err := writeNodes(w, node.Nodes, prefix+continuation, node.Truncated); err != nil {
			return err
		}
	}

	if truncated {
		if _, err := fmt.Fprintf(w, "%s└── ...\n", prefix); err != nil {
			return errors.Wrap(err, errors.CodeIO, "cannot write output")
		}
	}
	return nil
}

// scanTreeTable flattens the tree into rows, because a nested structure is not
// a CSV and pretending otherwise produces a file nobody can read. The path
// carries the nesting.
func scanTreeTable(result scan.TreeResult) output.TableFunc {
	return func() output.Table {
		var rows [][]string
		var walk func(nodes []scan.Node)

		walk = func(nodes []scan.Node) {
			for _, node := range nodes {
				kind := "file"
				if node.IsDir {
					kind = "directory"
				}
				rows = append(rows, []string{
					node.Path,
					kind,
					strconv.Itoa(node.Depth),
					strconv.Itoa(node.Files),
					strconv.FormatInt(node.Bytes, 10),
				})
				walk(node.Nodes)
			}
		}
		walk(result.Nodes)

		return output.Table{
			Columns: []output.Column{
				{Title: "path"},
				{Title: "kind"},
				{Title: "depth", Right: true},
				{Title: "files", Right: true},
				{Title: "bytes", Right: true},
			},
			Rows: rows,
		}
	}
}
