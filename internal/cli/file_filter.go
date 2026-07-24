package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/devnest/devnest/internal/core/file"
	"github.com/devnest/devnest/internal/output"
)

func newFileFilterCommand() *Command {
	var (
		selection      selectionFlags
		extensions     repeatable
		category       string
		match          string
		sortBy         string
		limit          int
		minSize        int64
		maxSize        int64
		showCategories bool
	)

	return &Command{
		Name:    "filter",
		Summary: "Search for files by extension, category, name, or size",
		Usage:   "devnest file filter [path] [flags]",
		Description: "Search a directory tree for files matching an extension, a category, " +
			"a name pattern, a size range, or any combination of them. Every condition " +
			"given must hold; giving none lists everything.\n\n" +
			"Categories are groups of extensions (Images, Documents, Code, and so on) " +
			"so \"--category Code\" finds source files without listing forty extensions. " +
			"Run \"devnest file filter --categories\" to see them all.",
		Examples: []Example{
			{
				Command:     "devnest file filter . --extension pdf",
				Description: "Find every PDF under the current directory.",
			},
			{
				Command:     "devnest file filter C:\\projects --category Code --min-size 100KB",
				Description: "Find source files over a hundred kilobytes.",
			},
			{
				Command:     "devnest file filter . --extension jpg --extension png --sort size",
				Description: "List images, largest first.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.register(set, true)
			set.Var(&extensions, "extension", "match this extension, with or without the dot (repeatable)")
			set.Var(&extensions, "e", "match this extension (shorthand, repeatable)")
			set.StringVar(&category, "category", "", "match a whole category, such as Images or Code")
			set.StringVar(&match, "match", "", "match names against this glob")
			set.StringVar(&sortBy, "sort", file.SortByPath, "order results by: path, name, size, or modified")
			set.IntVar(&limit, "limit", 0, "show at most this many results (0 means all)")
			set.Var(newSizeValue(&minSize, 0), "min-size", "ignore files smaller than this")
			set.Var(newSizeValue(&maxSize, 0), "max-size", "ignore files larger than this")
			set.BoolVar(&showCategories, "categories", false,
				"list the available categories and their extensions")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			if showCategories {
				return emitCategories(env)
			}
			if len(args) > 1 {
				return tooManyPaths(args, "devnest file filter")
			}

			order, err := file.ParseSort(sortBy)
			if err != nil {
				return err
			}

			result, err := file.Filter(ctx, filesystem(), file.FilterRequest{
				Selection:  selection.selection(env, firstPath(args)),
				Extensions: extensions,
				Category:   category,
				Match:      match,
				MinBytes:   minSize,
				MaxBytes:   maxSize,
				SortBy:     order,
				Limit:      limit,
			})
			if err != nil {
				return err
			}

			reportProblems(env, result.Problems)
			return env.Emit(result, filterText(result))
		},
	}
}

func filterText(result file.FilterResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Matched == 0 {
			fmt.Fprintf(w, "No matching files in %s (%s scanned)\n",
				result.Root, pluralFiles(result.Scanned))
			return writeProblems(w, result.Problems)
		}

		if err := output.WriteTable(w, fileColumns(), fileRows(result.Files)); err != nil {
			return err
		}
		fmt.Fprintln(w)

		if result.Truncated {
			fmt.Fprintf(w, "Showing %s of %s.\n",
				pluralFiles(len(result.Files)), pluralFiles(result.Matched))
		}
		fmt.Fprintf(w, "%s matched, %s total\n",
			pluralFiles(result.Matched), output.Bytes(result.TotalBytes))

		return writeProblems(w, result.Problems)
	}
}

// categoryListing is the data behind --categories, so the listing is available
// as JSON as well as text.
type categoryListing struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// emitCategories answers --categories. It lives inside filter rather than
// being a command of its own: a command whose whole job is printing a constant
// is not worth a place in the tree.
func emitCategories(env *Env) error {
	names := file.CategoryNames()
	listing := make([]categoryListing, 0, len(names))
	for _, name := range names {
		listing = append(listing, categoryListing{
			Name:       name,
			Extensions: file.ExtensionsIn(name),
		})
	}

	return env.Emit(map[string]any{"categories": listing}, func(w io.Writer) error {
		for _, item := range listing {
			if len(item.Extensions) == 0 {
				fmt.Fprintf(w, "%-12s anything the other categories do not claim\n", item.Name)
				continue
			}
			fmt.Fprintf(w, "%-12s %s\n", item.Name, strings.Join(item.Extensions, " "))
		}
		return nil
	})
}
