package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/core/git"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
	"github.com/devnest/devnest/internal/platform/proc"
)

// newGitCommand builds the "git" group. The group itself is runnable and
// reports a summary, because "what is this repository" is the question people
// ask most and should not need a subcommand.
func newGitCommand() *Command {
	return &Command{
		Name:    "git",
		Summary: "Repository summary, branches, contributors, large objects",
		Usage:   "devnest git [command] [path] [flags]",
		Description: "Report on a git repository: what it is, which branches have gone " +
			"quiet, who has been committing, and what is making the history large.\n\n" +
			"Everything here is read-only. Nothing commits, pushes, fetches, rebases, or " +
			"deletes a branch. \"git stale\" will print the commands that would delete " +
			"the branches it found, and printing them is as far as it goes: you read the " +
			"list and decide.\n\n" +
			"These commands ask the git executable rather than reading the repository " +
			"format directly, so they need git on PATH and say so plainly when it is " +
			"missing. The path defaults to the current directory, and any path inside a " +
			"repository works: the top level is found from it.",
		Examples: []Example{
			{
				Command:     "devnest git",
				Description: "What this repository is: branch, remotes, counts, age.",
			},
			{
				Command:     "devnest git stale --days 60 --print-commands",
				Description: "Branches quiet for two months, with the commands to remove them.",
			},
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := gitPath(args)
			if err != nil {
				return err
			}

			runner, locator := gitSystem()
			result, err := git.Summary(ctx, runner, locator, git.SummaryRequest{
				Path: path,
				Now:  time.Now(),
			})
			if err != nil {
				return err
			}

			return env.Emit(result, gitSummaryText(result))
		},
		Commands: []*Command{
			newGitBranchesCommand(),
			newGitStaleCommand(),
			newGitContributorsCommand(),
			newGitLargeCommand(),
			newGitHotspotCommand(),
		},
	}
}

// gitSystem is the real machine. Tests call the module directly with a fake.
func gitSystem() (git.Runner, git.Locator) { return proc.System{}, fs.System{} }

// gitPath takes the optional path argument, defaulting to the current
// directory, which is where somebody standing in a repository is.
func gitPath(args []string) (string, error) {
	switch len(args) {
	case 0:
		return ".", nil
	case 1:
		return args[0], nil
	default:
		return "", errors.New(errors.CodeInvalidInput,
			"expected one repository, found %d arguments", len(args)).
			WithHint("run one command per repository")
	}
}

func gitSummaryText(result git.SummaryResult) output.TextFunc {
	return func(w io.Writer) error {
		branch := result.Branch
		if result.Detached {
			branch = "(detached HEAD)"
		}

		fields := []output.Field{
			{Label: "repository", Value: result.Root},
			{Label: "branch", Value: branch},
		}
		if result.Head != "" {
			fields = append(fields, output.Field{Label: "head", Value: result.Head})
		}

		for index, remote := range result.Remotes {
			label := "remote"
			if index > 0 {
				label = "remote " + strconv.Itoa(index+1)
			}
			fields = append(fields, output.Field{
				Label: label,
				Value: remote.Name + "  " + remote.URL,
			})
		}

		fields = append(fields,
			output.Field{Label: "commits", Value: output.Count(result.Commits)},
			output.Field{Label: "branches", Value: output.Count(result.Branches)},
			output.Field{Label: "tags", Value: output.Count(result.Tags)},
			output.Field{Label: "working tree", Value: treeSummary(result.Tree)},
		)

		if result.FirstCommit != nil {
			fields = append(fields, output.Field{
				Label: "first commit",
				Value: fmt.Sprintf("%s (%s days ago)",
					result.FirstCommit.Format("2006-01-02"), output.Count(result.AgeDays)),
			})
		}
		if result.LastCommit != nil {
			fields = append(fields, output.Field{
				Label: "last commit",
				Value: fmt.Sprintf("%s (%s days ago)",
					result.LastCommit.Format("2006-01-02"), output.Count(result.IdleDays)),
			})
		}

		return output.WriteFields(w, fields)
	}
}

// treeSummary reads as a sentence rather than four numbers, because "clean" is
// what a person is looking for and the counts only matter when it is not.
func treeSummary(tree git.WorkingTree) string {
	if tree.Clean && tree.Untracked == 0 {
		return "clean"
	}

	parts := make([]string, 0, 4)
	if tree.Staged > 0 {
		parts = append(parts, fmt.Sprintf("%d staged", tree.Staged))
	}
	if tree.Modified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", tree.Modified))
	}
	if tree.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", tree.Untracked))
	}
	if tree.Conflicts > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicted", tree.Conflicts))
	}

	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, ", ")
}

func newGitBranchesCommand() *Command {
	var (
		days   int
		merged bool
	)

	return &Command{
		Name:    "branches",
		Summary: "Local branches with their last commit and age",
		Usage:   "devnest git branches [path] [flags]",
		Description: "List the local branches, most recently active first, with who " +
			"touched each one last and how long ago.\n\n" +
			"A branch past the staleness window is marked, and the count of those is in " +
			"the result, so one listing answers both \"what branches are there\" and " +
			"\"how many should I look at\". --merged narrows the list to branches already " +
			"merged into the current one.\n\n" +
			"Results are rows, so --output csv works.",
		Examples: []Example{
			{
				Command:     "devnest git branches",
				Description: "Every local branch, newest activity first.",
			},
			{
				Command:     "devnest git branches --merged --days 30",
				Description: "Merged branches, flagging anything quiet for a month.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&days, "days", 0,
				"days of quiet before a branch counts as stale (default 90)")
			set.BoolVar(&merged, "merged", false, "only branches merged into the current one")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := gitPath(args)
			if err != nil {
				return err
			}

			runner, locator := gitSystem()
			result, err := git.Branches(ctx, runner, locator, git.BranchRequest{
				Path:      path,
				StaleDays: days,
				Merged:    merged,
				Now:       time.Now(),
			})
			if err != nil {
				return err
			}

			return env.EmitTable(result, gitBranchesText(result), gitBranchesTable(result.Branches))
		},
	}
}

func newGitStaleCommand() *Command {
	var (
		days     int
		merged   bool
		commands bool
	)

	return &Command{
		Name:    "stale",
		Summary: "Branches nobody has touched for a while",
		Usage:   "devnest git stale [path] [flags]",
		Description: "List the branches with no commits for --days, default 90.\n\n" +
			"The branch you are standing on is never listed, because deleting it is not " +
			"something you can do anyway.\n\n" +
			"--print-commands adds the git commands that would delete them. They are " +
			"printed, never run: this command reports and you decide. The commands use " +
			"\"branch -d\" rather than \"-D\", so one copied without reading it still " +
			"refuses to throw away unmerged work.\n\n" +
			"--merged is the flag that makes this listing actionable: a merged branch " +
			"quiet for a quarter is one nobody will miss.",
		Examples: []Example{
			{
				Command:     "devnest git stale --merged",
				Description: "Merged branches nobody has touched in three months.",
			},
			{
				Command:     "devnest git stale --days 180 --print-commands",
				Description: "Half-year-old branches, with the deletion commands to review.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&days, "days", 0, "days of quiet before a branch counts as stale (default 90)")
			set.BoolVar(&merged, "merged", false, "only branches merged into the current one")
			set.BoolVar(&commands, "print-commands", false,
				"print the git commands that would delete these branches")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := gitPath(args)
			if err != nil {
				return err
			}

			runner, locator := gitSystem()
			result, err := git.Stale(ctx, runner, locator, git.BranchRequest{
				Path:      path,
				StaleDays: days,
				Merged:    merged,
				Now:       time.Now(),
			}, commands)
			if err != nil {
				return err
			}

			return env.EmitTable(result, gitStaleText(result), gitBranchesTable(result.Branches))
		},
	}
}

func gitBranchesText(result git.BranchResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 {
			_, err := fmt.Fprintln(w, "No branches.")
			return err
		}

		if err := output.WriteTable(w, branchColumns(), branchRows(result.Branches)); err != nil {
			return err
		}

		_, err := fmt.Fprintf(w, "\n%s branch(es), %s quiet for %s days or more\n",
			output.Count(result.Count),
			output.Count(result.StaleCount),
			output.Count(result.StaleDays))
		return err
	}
}

func gitStaleText(result git.StaleResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 {
			_, err := fmt.Fprintf(w, "No branch has been quiet for %s days.\n",
				output.Count(result.StaleDays))
			return err
		}

		if err := output.WriteTable(w, branchColumns(), branchRows(result.Branches)); err != nil {
			return err
		}
		fmt.Fprintf(w, "\n%s branch(es) quiet for %s days or more\n",
			output.Count(result.Count), output.Count(result.StaleDays))

		if len(result.Commands) == 0 {
			return nil
		}

		fmt.Fprintln(w, "\nTo remove them, review these and run the ones you want:")
		for _, command := range result.Commands {
			fmt.Fprintf(w, "  %s\n", command)
		}
		_, err := fmt.Fprintln(w, "\nDevNest does not run them.")
		return err
	}
}

func branchColumns() []output.Column {
	return []output.Column{
		{Title: "branch"},
		{Title: "last commit"},
		{Title: "age", Right: true},
		{Title: "author"},
		{Title: "upstream"},
	}
}

func branchRows(branches []git.Branch) [][]string {
	rows := make([][]string, 0, len(branches))
	for _, branch := range branches {
		name := branch.Name
		if branch.Current {
			name = "* " + name
		}

		upstream := branch.Upstream
		if upstream == "" {
			upstream = "never pushed"
		}

		rows = append(rows, []string{
			name,
			branch.LastCommit.Format("2006-01-02"),
			strconv.Itoa(branch.AgeDays) + "d",
			branch.Author,
			upstream,
		})
	}
	return rows
}

func gitBranchesTable(branches []git.Branch) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(branches))
		for _, branch := range branches {
			rows = append(rows, []string{
				branch.Name,
				branch.LastCommit.Format(time.RFC3339),
				strconv.Itoa(branch.AgeDays),
				branch.Author,
				branch.Upstream,
				strconv.FormatBool(branch.Stale),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "branch"},
				{Title: "lastCommit"},
				{Title: "ageDays", Right: true},
				{Title: "author"},
				{Title: "upstream"},
				{Title: "stale"},
			},
			Rows: rows,
		}
	}
}

func newGitContributorsCommand() *Command {
	var (
		limit int
		since string
	)

	return &Command{
		Name:    "contributors",
		Summary: "Commit counts and activity by author",
		Usage:   "devnest git contributors [path] [flags]",
		Description: "Report who has committed, how often, and when they last did.\n\n" +
			"People are identified by email address, folded to lower case, because names " +
			"are spelled several ways in every repository of any age and the address is " +
			"what stays constant. The address is already in every commit object; nothing " +
			"here discovers anything private.\n\n" +
			"--since narrows the history to recent work, in any form git accepts: a date, " +
			"or something like \"3 months ago\". Results are rows, so --output csv works.",
		Examples: []Example{
			{
				Command:     "devnest git contributors --limit 10",
				Description: "The ten most active contributors in the whole history.",
			},
			{
				Command:     "devnest git contributors --since '6 months ago'",
				Description: "Who has actually been working on this lately.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&limit, "limit", 0, "how many contributors to report (default all)")
			set.StringVar(&since, "since", "", "only commits after this date")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := gitPath(args)
			if err != nil {
				return err
			}

			runner, locator := gitSystem()
			result, err := git.Contributors(ctx, runner, locator, git.ContributorRequest{
				Path:  path,
				Limit: limit,
				Since: since,
				Now:   time.Now(),
			})
			if err != nil {
				return err
			}

			return env.EmitTable(result, gitContributorsText(result), gitContributorsTable(result))
		},
	}
}

func gitContributorsText(result git.ContributorResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 {
			_, err := fmt.Fprintln(w, "No commits.")
			return err
		}

		columns := []output.Column{
			{Title: "contributor"},
			{Title: "commits", Right: true},
			{Title: "share", Right: true},
			{Title: "first"},
			{Title: "last"},
		}

		rows := make([][]string, 0, len(result.Contributors))
		for _, contributor := range result.Contributors {
			rows = append(rows, []string{
				contributor.Name,
				output.Count(contributor.Commits),
				fmt.Sprintf("%.1f%%", contributor.Percent),
				contributor.First.Format("2006-01-02"),
				fmt.Sprintf("%s (%dd ago)", contributor.Last.Format("2006-01-02"), contributor.IdleDays),
			})
		}

		if err := output.WriteTable(w, columns, rows); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s contributor(s) over %s commit(s)\n",
			output.Count(result.Count), output.Count(result.Commits))
		if result.Truncated {
			_, err := fmt.Fprintln(w, "This is the top of the list; pass --limit 0 for all of it.")
			return err
		}
		return nil
	}
}

func gitContributorsTable(result git.ContributorResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Contributors))
		for _, contributor := range result.Contributors {
			rows = append(rows, []string{
				contributor.Name,
				contributor.Email,
				strconv.Itoa(contributor.Commits),
				strconv.FormatFloat(contributor.Percent, 'f', 1, 64),
				contributor.First.Format(time.RFC3339),
				contributor.Last.Format(time.RFC3339),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "name"},
				{Title: "email"},
				{Title: "commits", Right: true},
				{Title: "percent", Right: true},
				{Title: "firstCommit"},
				{Title: "lastCommit"},
			},
			Rows: rows,
		}
	}
}

func newGitLargeCommand() *Command {
	var limit int

	return &Command{
		Name:    "large",
		Summary: "The biggest objects in the history",
		Usage:   "devnest git large [path] [flags]",
		Description: "Report the largest file objects anywhere in the repository's " +
			"history, not only in the current checkout.\n\n" +
			"This is the command for \"why does cloning this take ten minutes\". A file " +
			"deleted two years ago still costs every clone what it weighed, and it will " +
			"not appear in any listing of the working tree.\n\n" +
			"It walks every object in the repository, so it is the slowest command here. " +
			"Objects that are not reachable from any ref are left out: they are usually " +
			"waiting to be garbage collected, and a row nobody can act on is noise.\n\n" +
			"Removing something from history means rewriting it, which is a decision with " +
			"consequences for everyone who has cloned. DevNest reports; it does not offer " +
			"to do that.",
		Examples: []Example{
			{
				Command:     "devnest git large",
				Description: "The ten largest objects in the history.",
			},
			{
				Command:     "devnest git large --limit 25 --output json",
				Description: "A longer list, for a script or a ticket.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&limit, "limit", 0, "how many objects to report (default 10)")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := gitPath(args)
			if err != nil {
				return err
			}

			runner, locator := gitSystem()
			result, err := git.Large(ctx, runner, locator, git.LargeRequest{
				Path:  path,
				Limit: limit,
			})
			if err != nil {
				return err
			}

			return env.EmitTable(result, gitLargeText(result), gitLargeTable(result))
		},
	}
}

func gitLargeText(result git.LargeResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 {
			_, err := fmt.Fprintln(w, "No objects.")
			return err
		}

		columns := []output.Column{
			{Title: "size", Right: true},
			{Title: "path"},
			{Title: "object"},
		}

		rows := make([][]string, 0, len(result.Objects))
		for _, object := range result.Objects {
			path := object.Path
			if path == "" {
				path = "(no path)"
			}
			rows = append(rows, []string{output.Bytes(object.Bytes), path, short(object.Hash)})
		}

		if err := output.WriteTable(w, columns, rows); err != nil {
			return err
		}

		_, err := fmt.Fprintf(w, "\n%s object(s) listed, %s in total, out of %s examined\n",
			output.Count(result.Count),
			output.Bytes(result.TotalBytes),
			output.Count(result.Scanned))
		return err
	}
}

func gitLargeTable(result git.LargeResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Objects))
		for _, object := range result.Objects {
			rows = append(rows, []string{
				object.Hash,
				object.Path,
				strconv.FormatInt(object.Bytes, 10),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "object"},
				{Title: "path"},
				{Title: "bytes", Right: true},
			},
			Rows: rows,
		}
	}
}

// short trims an object hash to the length git itself prints.
func short(hash string) string {
	const width = 8
	if len(hash) > width {
		return hash[:width]
	}
	return hash
}

func newGitHotspotCommand() *Command {
	var (
		limit int
		since string
	)

	return &Command{
		Name:    "hotspot",
		Summary: "The files a repository changes most often",
		Usage:   "devnest git hotspot [path] [flags]",
		Description: "Report which files the history touches most, newest activity first.\n\n" +
			"Change frequency is a proxy for where the risk concentrates: a file that half of " +
			"every change edits, or one that never stops being rewritten, is where a regression " +
			"is most likely to land. This reports the count, which is the question — it does not " +
			"opine on whether the churn is good or bad.\n\n" +
			"--since narrows the window to recent work, in any form git accepts: a date, or " +
			"something like \"3 months ago\", so the answer is \"what is changing now\" rather " +
			"than \"what has always changed\". Results are rows, so --output csv works.",
		Examples: []Example{
			{
				Command:     "devnest git hotspot --limit 10",
				Description: "The ten most-changed files in the whole history.",
			},
			{
				Command:     "devnest git hotspot --since '6 months ago'",
				Description: "Where the change is concentrating this year.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.IntVar(&limit, "limit", 0, "how many files to report (default all)")
			set.StringVar(&since, "since", "", "only commits after this date")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			path, err := gitPath(args)
			if err != nil {
				return err
			}

			runner, locator := gitSystem()
			result, err := git.Hotspot(ctx, runner, locator, git.HotspotRequest{
				Path:  path,
				Limit: limit,
				Since: since,
			})
			if err != nil {
				return err
			}

			return env.EmitTable(result, gitHotspotText(result), gitHotspotTable(result))
		},
	}
}

func gitHotspotText(result git.HotspotResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.DistinctFiles == 0 {
			_, err := fmt.Fprintln(w, "No changed files in that window.")
			return err
		}

		columns := []output.Column{
			{Title: "path"},
			{Title: "commits", Right: true},
		}

		rows := make([][]string, 0, len(result.Files))
		for _, file := range result.Files {
			rows = append(rows, []string{file.Path, output.Count(file.Commits)})
		}

		if err := output.WriteTable(w, columns, rows); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s file(s) changed in %s commit(s), showing %s\n",
			output.Count(result.DistinctFiles),
			output.Count(result.Commits),
			output.Count(len(result.Files)))
		if result.Truncated {
			_, err := fmt.Fprintln(w, "This is the top of the list; pass --limit 0 for all of it.")
			return err
		}
		return nil
	}
}

func gitHotspotTable(result git.HotspotResult) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(result.Files))
		for _, file := range result.Files {
			rows = append(rows, []string{file.Path, strconv.Itoa(file.Commits)})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "path"},
				{Title: "commits", Right: true},
			},
			Rows: rows,
		}
	}
}
