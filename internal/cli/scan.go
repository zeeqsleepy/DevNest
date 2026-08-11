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

// newScanCommand builds the "scan" group, runnable itself: the summary is what
// people mean by "scan this project".
func newScanCommand() *Command {
	var (
		selection scanFlags
		top       int
	)

	return &Command{
		Name:    "scan",
		Summary: "Analyse a project tree: file types, languages, line counts",
		Usage:   "devnest scan [path] [flags]",
		Description: "Report what a directory tree is made of: how many files, of " +
			"what kinds, in what languages, and how much of it is code somebody on " +
			"this project actually wrote.\n\n" +
			"The walk skips what the project already ignores. Rules in .gitignore " +
			"are applied, along with the vendor and build directories every " +
			"ecosystem has whether or not they are written down, and .git is always " +
			"skipped. Without that, a small Node project reports four hundred " +
			"thousand files of which four hundred are the project. --no-ignore turns " +
			"it off when you want the whole truth.\n\n" +
			"Where the disk space went is a different question, answered by " +
			"\"devnest file size\". This command reports shape, not weight.\n\n" +
			"Read-only, always. Nothing here writes, moves, or removes anything.",
		Examples: []Example{
			{
				Command:     "devnest scan",
				Description: "What the project in this directory is made of.",
			},
			{
				Command:     "devnest scan ~/work/api --no-ignore --output json",
				Description: "Everything on disk, including vendored and generated files.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			selection.register(set)
			set.IntVar(&top, "top", 10, "how many entries each ranked listing reports")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			result, err := scan.Summarize(ctx, filesystem(), scan.SummaryRequest{
				Selection: selection.selection(env, firstPath(args)),
				Top:       top,
			})
			if err != nil {
				return err
			}

			reportScanProblems(env, result.Problems)
			return env.EmitTable(result, scanSummaryText(result), scanSummaryTable(result))
		},
		Commands: []*Command{
			newScanTypesCommand(),
			newScanLinesCommand(),
			newScanTreeCommand(),
			newScanCompareCommand(),
		},
	}
}

// scanFlags are the walk options every scan command shares, so --no-ignore
// means the same thing in all of them.
type scanFlags struct {
	depth          int
	includeHidden  bool
	followSymlinks bool
	noIgnore       bool
	exclude        repeatable
}

func (s *scanFlags) register(set *flag.FlagSet) {
	set.IntVar(&s.depth, "depth", 0, "limit how deep to descend (0 means unlimited)")
	s.registerWithoutDepth(set)
}

// registerWithoutDepth is for "tree", which walks the whole tree and uses
// --depth for how much of the result to draw. Registering both would be two
// flags with one name, so the command that owns the name registers it.
func (s *scanFlags) registerWithoutDepth(set *flag.FlagSet) {
	set.BoolVar(&s.includeHidden, "include-hidden", false, "include hidden files")
	set.BoolVar(&s.followSymlinks, "follow-symlinks", false,
		"descend into symlinked directories")
	set.BoolVar(&s.noIgnore, "no-ignore", false,
		"disregard .gitignore and the built-in vendor and build rules")
	set.Var(&s.exclude, "exclude", "skip entries matching a glob (repeatable)")
}

// selection turns the flags into a request, folding in the excludes the user
// configured. Configuration is read here rather than inside the module,
// because a module never reads configuration.
func (s *scanFlags) selection(env *Env, root string) scan.Selection {
	exclude := append([]string(nil), env.Config.Scan.Exclude...)
	exclude = append(exclude, s.exclude...)

	depth := s.depth
	if depth == 0 && env.Config.Scan.MaxDepth > 0 {
		depth = int(env.Config.Scan.MaxDepth)
	}

	return scan.Selection{
		Root:           root,
		MaxDepth:       depth,
		IncludeHidden:  s.includeHidden,
		FollowSymlinks: s.followSymlinks || env.Config.Scan.FollowSymlinks,
		Exclude:        exclude,
		NoIgnore:       s.noIgnore || !env.Config.Scan.RespectIgnore,
	}
}

// reportScanProblems turns unreadable entries into warnings. The scan
// succeeded; part of the tree could not be read, and the result says which
// part rather than pretending it was empty.
func reportScanProblems(env *Env, problems []scan.Problem) {
	for _, problem := range problems {
		env.Warn(errors.CodePermissionDenied, problem.Reason, "path", problem.Path)
	}
}

func scanSummaryText(result scan.SummaryResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "root", Value: result.Root},
			{Label: "files", Value: output.Count(result.Files)},
			{Label: "directories", Value: output.Count(result.Directories)},
			{Label: "size", Value: output.Bytes(result.Bytes)},
			{Label: "depth", Value: output.Count(result.Depth)},
			{Label: "authored", Value: fmt.Sprintf("%s files (%s)",
				output.Count(result.Authored), output.Bytes(result.AuthoredBytes))},
			{Label: "ignore rules", Value: appliedText(result.Ignored)},
			{Label: "scanned in", Value: fmt.Sprintf("%d ms", result.DurationMs)},
		}
		if err := output.WriteFields(w, fields); err != nil {
			return err
		}

		sections := []struct {
			title   string
			subject string
			counts  []scan.Count
		}{
			{"By category", "category", result.Categories},
			{"Top languages", "language", result.Languages},
			{"Top extensions", "extension", result.Extensions},
		}
		for _, section := range sections {
			if err := writeScanCounts(w, section.title, section.subject, section.counts); err != nil {
				return err
			}
		}
		return nil
	}
}

func appliedText(applied bool) string {
	if applied {
		return "applied"
	}
	return "off (--no-ignore)"
}

// writeScanCounts renders one ranked listing under a heading.
func writeScanCounts(w io.Writer, title, subject string, counts []scan.Count) error {
	if len(counts) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n%s\n", title); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}

	rows := make([][]string, 0, len(counts))
	for _, count := range counts {
		rows = append(rows, []string{
			count.Name,
			output.Count(count.Files),
			output.Bytes(count.Bytes),
			fmt.Sprintf("%.1f%%", count.Percent),
		})
	}
	return output.WriteTable(w, []output.Column{
		{Title: subject},
		{Title: "files", Right: true},
		{Title: "size", Right: true},
		{Title: "share", Right: true},
	}, rows)
}

// scanCountRows is the machine-readable form: no separators, no percent sign.
func scanCountRows(section string, counts []scan.Count) [][]string {
	rows := make([][]string, 0, len(counts))
	for _, count := range counts {
		rows = append(rows, []string{
			section,
			count.Name,
			strconv.Itoa(count.Files),
			strconv.FormatInt(count.Bytes, 10),
			strconv.FormatFloat(count.Percent, 'f', 1, 64),
		})
	}
	return rows
}

func scanCountColumns() []output.Column {
	return []output.Column{
		{Title: "section"},
		{Title: "name"},
		{Title: "files", Right: true},
		{Title: "bytes", Right: true},
		{Title: "percent", Right: true},
	}
}

func scanSummaryTable(result scan.SummaryResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0,
			len(result.Categories)+len(result.Languages)+len(result.Extensions))
		rows = append(rows, scanCountRows("category", result.Categories)...)
		rows = append(rows, scanCountRows("language", result.Languages)...)
		rows = append(rows, scanCountRows("extension", result.Extensions)...)
		return output.Table{Columns: scanCountColumns(), Rows: rows}
	}
}
