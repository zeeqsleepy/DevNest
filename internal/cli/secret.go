package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/core/secret"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
	"github.com/devnest/devnest/internal/platform/proc"
)

// newSecretCommand builds the "secret" group. The group is runnable and scans
// the working tree, because that is the question and the other commands are
// variations on it.
func newSecretCommand() *Command {
	return &Command{
		Name:    "secret",
		Summary: "Scan for credentials that should not be committed",
		Usage:   "devnest secret [command] [path] [flags]",
		Description: "Search a working tree, or a repository's history, for strings " +
			"shaped like credentials: provider keys, tokens, private key blocks, " +
			"passwords in connection strings.\n\n" +
			"**A matched value is never printed in full.** Findings carry the rule, the " +
			"file, the line, and the first few characters with a length. That holds in " +
			"the JSON output as well as the table, at every verbosity, because a report " +
			"gets exported and attached to a ticket.\n\n" +
			"What comes back is a list of candidates and the output says so. Every " +
			"scanner of this kind lives or dies on its false positives, and one people " +
			"have learned to ignore finds nothing at all. The defences against noise are " +
			"an entropy floor on every rule, so a placeholder full of X characters does " +
			"not fire; skipping the directories where test fixtures and dependencies " +
			"live; and a \"devnest:allow-secret\" comment that silences one line.\n\n" +
			"Read-only throughout. Nothing here writes, moves, or deletes anything.",
		Examples: []Example{
			{
				Command:     "devnest secret scan",
				Description: "Check the working tree before committing.",
			},
			{
				Command:     "devnest secret scan --fail-on high",
				Description: "Fail a CI job on anything high or critical.",
			},
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			return runSecretScan(ctx, env, args, secretFlags{})
		},
		Commands: []*Command{
			newSecretScanCommand(),
			newSecretHistoryCommand(),
			newSecretRulesCommand(),
			newSecretTestCommand(),
		},
	}
}

// secretFlags are the options the scanning commands share.
type secretFlags struct {
	rules        repeatable
	exclude      repeatable
	entropy      float64
	failOn       string
	includeTests bool
}

func (s *secretFlags) register(set *flag.FlagSet) {
	set.Var(&s.rules, "rule", "run only this rule; repeatable")
	set.Var(&s.exclude, "exclude", "skip entries matching a glob; repeatable")
	set.Float64Var(&s.entropy, "entropy", 0,
		"override the entropy floor every rule uses")
	set.StringVar(&s.failOn, "fail-on", "",
		"exit non-zero when a finding is at or above this severity")
	set.BoolVar(&s.includeTests, "include-tests", false,
		"scan testdata and fixtures, which are full of fake credentials")
}

func secretReader() secret.Reader { return fs.System{} }
func secretRunner() secret.Runner { return proc.System{} }

func newSecretScanCommand() *Command {
	var flags secretFlags

	return &Command{
		Name:    "scan",
		Summary: "Scan a working tree for credentials",
		Usage:   "devnest secret scan [path] [flags]",
		Description: "Search the files under a directory for credential-shaped " +
			"strings.\n\n" +
			"Every file is read once, line by line, so a large tree costs the same " +
			"memory as a small one. Binary files are skipped by looking at their first " +
			"bytes rather than at their names, files above two megabytes are skipped as " +
			"machine-written, and lock files and dependency directories are skipped " +
			"because they are full of hashes that look like keys.\n\n" +
			"testdata and fixtures directories are skipped by default: they hold fake " +
			"credentials on purpose, and reporting them is how a scanner earns its " +
			"reputation for noise. Pass --include-tests to look anyway.\n\n" +
			"--fail-on makes this a gate: with it, the command exits non-zero when " +
			"anything at or above that severity was found, which is what a pre-commit " +
			"hook or a CI step needs. Without it, finding something is still a " +
			"successful run.",
		Examples: []Example{
			{
				Command:     "devnest secret scan .",
				Description: "Check this project before pushing.",
			},
			{
				Command:     "devnest secret scan --rule aws-access-key-id --output json",
				Description: "Run one rule and hand the result to a script.",
			},
		},
		SetFlags: flags.register,
		Run: func(ctx context.Context, env *Env, args []string) error {
			return runSecretScan(ctx, env, args, flags)
		},
	}
}

func runSecretScan(ctx context.Context, env *Env, args []string, flags secretFlags) error {
	root, err := secretPath(args)
	if err != nil {
		return err
	}

	threshold, err := secret.Threshold(flags.failOn)
	if err != nil {
		return err
	}

	exclude := append([]string(nil), env.Config.Secret.ExcludePaths...)
	exclude = append(exclude, flags.exclude...)

	entropy := flags.entropy
	if entropy == 0 && env.Config.Secret.EntropyThreshold > 0 && len(flags.rules) == 0 {
		// The configured threshold applies only when the user did not ask for
		// a specific rule: someone testing one rule wants that rule's own
		// floor, not a global override they set months ago.
		entropy = 0
	}

	result, err := secret.Scan(ctx, secretReader(), secret.ScanRequest{
		Root:         root,
		Rules:        flags.rules,
		Exclude:      exclude,
		Entropy:      entropy,
		IncludeTests: flags.includeTests,
	})
	if err != nil {
		return err
	}

	if err := env.EmitTable(result, secretScanText(result), secretFindingsTable(result.Findings)); err != nil {
		return err
	}

	// The exit code is the gate. It comes last, after the report, because a
	// user needs to see what failed the build.
	if secret.MeetsThreshold(result.BySeverity, threshold) {
		return errors.New(errors.CodeCheckFailed,
			"found %s candidate(s) at or above %s", output.Count(result.Count), threshold)
	}
	return nil
}

// secretPath takes the optional directory argument.
func secretPath(args []string) (string, error) {
	switch len(args) {
	case 0:
		return ".", nil
	case 1:
		return args[0], nil
	default:
		return "", errors.New(errors.CodeInvalidInput,
			"expected one directory, found %d arguments", len(args)).
			WithHint("run one scan per project")
	}
}

func newSecretHistoryCommand() *Command {
	var (
		flags secretFlags
		depth int
		all   bool
	)

	return &Command{
		Name:    "history",
		Summary: "Scan a repository's history for credentials",
		Usage:   "devnest secret history [path] [flags]",
		Description: "Search the patches in a repository's history for credentials " +
			"that were committed, including ones later removed.\n\n" +
			"A credential deleted in a later commit is still in the history and still " +
			"leaked. That is the whole reason this command exists, and it is why " +
			"removing the file is not the fix: the fix is rotating the credential.\n\n" +
			"Only added lines are examined; the removal of a secret is not a second " +
			"leak. A credential added, reverted, and re-added is reported once.\n\n" +
			"This reads every patch and is much slower than scanning the tree, which is " +
			"why it is a separate command rather than a flag: a pre-commit hook wants " +
			"the working tree, and an audit wants this. The last 500 commits are covered " +
			"by default; --all reaches the whole history.\n\n" +
			"Needs git on PATH. The working-tree scan does not.",
		Examples: []Example{
			{
				Command:     "devnest secret history",
				Description: "Audit the recent history of this repository.",
			},
			{
				Command:     "devnest secret history --all --output json",
				Description: "The whole history, for a report.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			flags.register(set)
			set.IntVar(&depth, "depth", 0, "how many commits to examine (default 500)")
			set.BoolVar(&all, "all", false, "examine the whole history")
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			root, err := secretPath(args)
			if err != nil {
				return err
			}

			threshold, err := secret.Threshold(flags.failOn)
			if err != nil {
				return err
			}

			if all {
				depth = -1
			}

			result, err := secret.History(ctx, secretRunner(), secret.HistoryRequest{
				Root:    root,
				Depth:   depth,
				Rules:   flags.rules,
				Entropy: flags.entropy,
			})
			if err != nil {
				return err
			}

			if result.Truncated {
				env.Warn(errors.CodeUnsupported,
					"the history was larger than this command reads in one pass; "+
						"the scan covers less than the depth requested")
			}

			if err := env.EmitTable(result, secretHistoryText(result),
				secretHistoryTable(result.Findings)); err != nil {
				return err
			}

			if secret.MeetsThreshold(result.BySeverity, threshold) {
				return errors.New(errors.CodeCheckFailed,
					"found %s candidate(s) at or above %s in the history",
					output.Count(result.Count), threshold)
			}
			return nil
		},
	}
}

func newSecretRulesCommand() *Command {
	return &Command{
		Name:    "rules",
		Summary: "The detectors a scan would run",
		Usage:   "devnest secret rules [flags]",
		Description: "List the built-in rules: what each one detects, how serious it " +
			"is, and what entropy a match has to clear before it is reported.\n\n" +
			"This is the whole surface of what a scan can find. Worth reading before " +
			"trusting a clean result, and worth reading again when tuning: the rule " +
			"names here are what --rule takes.",
		Examples: []Example{
			{
				Command:     "devnest secret rules",
				Description: "See every detector and its severity.",
			},
			{
				Command:     "devnest secret rules --output json",
				Description: "The same table for a script or a review.",
			},
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			if len(args) > 0 {
				return errors.New(errors.CodeInvalidInput, "this command takes no arguments")
			}

			listing := secret.Rules()
			return env.EmitTable(listing, secretRulesText(listing), secretRulesTable(listing))
		},
	}
}

func newSecretTestCommand() *Command {
	var (
		rules    repeatable
		useStdin bool
	)

	return &Command{
		Name:    "test",
		Summary: "Check whether one string would be reported",
		Usage:   "devnest secret test <string> [flags]",
		Description: "Report which rules a string matches and what it scored, for " +
			"tuning a rule set.\n\n" +
			"The value is never echoed back, in any output format: somebody testing a " +
			"scanner is testing real credentials as often as not, and a command that " +
			"printed them would be the leak it exists to prevent. Prefer --stdin, which " +
			"also keeps the value out of your shell history.\n\n" +
			"Use this when a scan missed something, or reported something it should not " +
			"have: the entropy it reports is the number a threshold is compared against.",
		Examples: []Example{
			{
				Command:     "devnest secret test 'AKIA...' --stdin",
				Description: "See which rule catches a value, and what it scored.",
			},
			{
				Command:     "cat suspect.txt | devnest secret test --stdin",
				Description: "Test a value without it reaching your shell history.",
			},
		},
		SetFlags: func(set *flag.FlagSet) {
			set.Var(&rules, "rule", "test against this rule only; repeatable")
			set.BoolVar(&useStdin, "stdin", false, "read the value from standard input")
		},
		Run: func(_ context.Context, env *Env, args []string) error {
			value, err := readSecret(env, args, useStdin, "value")
			if err != nil {
				return err
			}

			result, err := secret.Inspect(secret.InspectRequest{Value: value, Rules: rules})
			if err != nil {
				return err
			}

			return env.Emit(result, secretTestText(result))
		},
	}
}

func secretScanText(result secret.ScanResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 {
			fmt.Fprintf(w, "No candidates in %s.\n", result.Root)
			return scannedNote(w, result)
		}

		if err := output.WriteTable(w, findingColumns(), findingRows(result.Findings)); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s candidate(s): %s\n",
			output.Count(result.Count), severitySummary(result.BySeverity))
		fmt.Fprintln(w, "These are candidates, not confirmed secrets. Check each one.")
		return scannedNote(w, result)
	}
}

// scannedNote says how much of the tree was looked at. A clean result over
// four files is not the same claim as a clean result over four thousand.
func scannedNote(w io.Writer, result secret.ScanResult) error {
	fmt.Fprintf(w, "%s file(s) scanned, %s skipped, %s rule(s) run",
		output.Count(result.FilesScanned),
		output.Count(result.FilesSkipped),
		output.Count(result.RulesUsed))

	if result.Suppressed > 0 {
		fmt.Fprintf(w, ", %s suppressed by a comment", output.Count(result.Suppressed))
	}
	_, err := fmt.Fprintln(w, ".")
	return err
}

func findingColumns() []output.Column {
	return []output.Column{
		{Title: "severity"},
		{Title: "rule"},
		{Title: "file"},
		{Title: "line", Right: true},
		{Title: "match"},
	}
}

func findingRows(findings []secret.Finding) [][]string {
	rows := make([][]string, 0, len(findings))
	for _, finding := range findings {
		rows = append(rows, []string{
			finding.Severity,
			finding.Rule,
			finding.Path,
			strconv.Itoa(finding.Line),
			finding.Redacted,
		})
	}
	return rows
}

func secretFindingsTable(findings []secret.Finding) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(findings))
		for _, finding := range findings {
			rows = append(rows, []string{
				finding.Severity,
				finding.Rule,
				finding.Path,
				strconv.Itoa(finding.Line),
				strconv.Itoa(finding.Column),
				finding.Redacted,
				strconv.FormatFloat(finding.Entropy, 'f', 2, 64),
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "severity"},
				{Title: "rule"},
				{Title: "file"},
				{Title: "line", Right: true},
				{Title: "column", Right: true},
				{Title: "match"},
				{Title: "entropy", Right: true},
			},
			Rows: rows,
		}
	}
}

func secretHistoryText(result secret.HistoryResult) output.TextFunc {
	return func(w io.Writer) error {
		if result.Count == 0 {
			_, err := fmt.Fprintf(w, "No candidates in %s commit(s) of history.\n",
				output.Count(result.Commits))
			return err
		}

		columns := []output.Column{
			{Title: "severity"},
			{Title: "rule"},
			{Title: "commit"},
			{Title: "file"},
			{Title: "match"},
		}

		rows := make([][]string, 0, len(result.Findings))
		for _, finding := range result.Findings {
			rows = append(rows, []string{
				finding.Severity,
				finding.Rule,
				short(finding.Commit),
				finding.Path,
				finding.Redacted,
			})
		}

		if err := output.WriteTable(w, columns, rows); err != nil {
			return err
		}

		fmt.Fprintf(w, "\n%s candidate(s) across %s commit(s): %s\n",
			output.Count(result.Count),
			output.Count(result.Commits),
			severitySummary(result.BySeverity))
		_, err := fmt.Fprintln(w,
			"A credential in history stays there until the history is rewritten. "+
				"Rotating it is the fix; removing the file is not.")
		return err
	}
}

func secretHistoryTable(findings []secret.HistoryFinding) output.TableFunc {
	return func() output.Table {
		rows := make([][]string, 0, len(findings))
		for _, finding := range findings {
			rows = append(rows, []string{
				finding.Severity,
				finding.Rule,
				finding.Commit,
				finding.Author,
				finding.Date.Format("2006-01-02"),
				finding.Path,
				finding.Redacted,
			})
		}
		return output.Table{
			Columns: []output.Column{
				{Title: "severity"},
				{Title: "rule"},
				{Title: "commit"},
				{Title: "author"},
				{Title: "date"},
				{Title: "file"},
				{Title: "match"},
			},
			Rows: rows,
		}
	}
}

func secretRulesText(rules []secret.Rule) output.TextFunc {
	return func(w io.Writer) error {
		return output.WriteTable(w, secretRulesColumns(), secretRulesRows(rules))
	}
}

func secretRulesTable(rules []secret.Rule) output.TableFunc {
	return func() output.Table {
		return output.Table{Columns: secretRulesColumns(), Rows: secretRulesRows(rules)}
	}
}

func secretRulesColumns() []output.Column {
	return []output.Column{
		{Title: "rule"},
		{Title: "severity"},
		{Title: "detects"},
		{Title: "entropy floor", Right: true},
	}
}

func secretRulesRows(rules []secret.Rule) [][]string {
	rows := make([][]string, 0, len(rules))
	for _, rule := range rules {
		floor := "none"
		if rule.Entropy > 0 {
			floor = strconv.FormatFloat(rule.Entropy, 'f', 1, 64)
		}
		rows = append(rows, []string{rule.Name, rule.Severity, rule.Description, floor})
	}
	return rows
}

func secretTestText(result secret.InspectResult) output.TextFunc {
	return func(w io.Writer) error {
		fields := []output.Field{
			{Label: "value", Value: result.Redacted},
			{Label: "length", Value: strconv.Itoa(result.Length)},
			{Label: "entropy", Value: fmt.Sprintf("%.2f bits per character", result.Entropy)},
			{Label: "rules run", Value: strconv.Itoa(result.RulesUsed)},
		}

		if !result.Matched {
			fields = append(fields, output.Field{Label: "matched", Value: "nothing"})
			return output.WriteFields(w, fields)
		}

		names := make([]string, 0, len(result.Findings))
		for _, finding := range result.Findings {
			names = append(names, finding.Rule+" ("+finding.Severity+")")
		}
		fields = append(fields, output.Field{Label: "matched", Value: strings.Join(names, ", ")})

		return output.WriteFields(w, fields)
	}
}

// severitySummary renders the counts in a fixed order, so two runs read the
// same way and the worst thing found is first.
func severitySummary(counts map[string]int) string {
	order := []string{
		secret.SeverityCritical, secret.SeverityHigh,
		secret.SeverityMedium, secret.SeverityLow,
	}

	parts := make([]string, 0, len(order))
	for _, severity := range order {
		if count := counts[severity]; count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, severity))
		}
	}

	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}
