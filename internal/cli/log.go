package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/devnest/devnest/internal/core/log"
	"github.com/devnest/devnest/internal/errors"
	"github.com/devnest/devnest/internal/output"
	"github.com/devnest/devnest/internal/platform/fs"
)

// newLogCommand builds the "log" group.
func newLogCommand() *Command {
	return &Command{
		Name:    "log",
		Summary: "Analyse log files: requests, errors, status codes, line statistics",
		Usage:   "devnest log <command> <file> [flags]",
		Description: "Read a text log file and report what is in it: how large it is, " +
			"which endpoints were busiest, which responses came back, what failed, " +
			"and where a keyword appears.\n\n" +
			"Every command here reads the file once, streaming it through a buffer it " +
			"reuses, so a log too large to open in an editor costs the same memory as " +
			"a small one. Nothing is loaded whole and nothing is written back: the log " +
			"commands never modify the file they read.\n\n" +
			"Lines that do not fit the expected format are counted as unparsed and the " +
			"run continues. A log with a startup banner in it is still a log worth " +
			"summarising, and every result reports how many lines it could not read so " +
			"you can judge the rest.\n\n" +
			"Results are rows, so --output csv works throughout.",
		Commands: []*Command{
			newLogAnalyzeCommand(),
			newLogHTTPCommand(),
			newLogErrorsCommand(),
			newLogStatusCommand(),
			newLogTopCommand(),
			newLogSearchCommand(),
			newLogStatsCommand(),
			newLogFollowCommand(),
		},
	}
}

// logFile takes the single file argument every log command needs.
//
// A log command with no file has no sensible default: unlike the file
// commands, "the current directory" is not a log. Saying so is better than
// guessing.
func logFile(args []string) (string, error) {
	switch len(args) {
	case 1:
		return args[0], nil
	case 0:
		return "", errors.New(errors.CodeInvalidInput, "no log file was given").
			WithHint("pass the path of the log file to read")
	default:
		return "", errors.New(errors.CodeInvalidInput,
			"expected one log file, found %d arguments", len(args)).
			WithHint("run one command per file, or quote a path containing spaces")
	}
}

// logReader is the real filesystem, which is what every log command gets in
// production. Tests call the module directly with a fake.
func logReader() log.Reader { return fs.System{} }

// logCountColumns is the shape every ranked listing renders as, so the same three
// columns mean the same thing in all seven commands.
func logCountColumns(subject string) []output.Column {
	return []output.Column{
		{Title: subject},
		{Title: "count", Right: true},
		{Title: "share", Right: true},
	}
}

func logCountRows(counts []log.Count) [][]string {
	rows := make([][]string, 0, len(counts))
	for _, count := range counts {
		rows = append(rows, []string{count.Value, output.Count(count.Count), logShare(count.Percent)})
	}
	return rows
}

// logPlainCountRows is the same listing without the thousands separators and the
// percent sign. Those are for a person reading a terminal; a spreadsheet given
// "1,204" has to be told it is a number.
func logPlainCountRows(counts []log.Count) [][]string {
	rows := make([][]string, 0, len(counts))
	for _, count := range counts {
		rows = append(rows, []string{
			count.Value,
			strconv.Itoa(count.Count),
			strconv.FormatFloat(count.Percent, 'f', 1, 64),
		})
	}
	return rows
}

// logPlainCountColumns is the header for logPlainCountRows.
func logPlainCountColumns(subject string) []output.Column {
	return []output.Column{
		{Title: subject},
		{Title: "count", Right: true},
		{Title: "percent", Right: true},
	}
}

// logWriteCounts renders one ranked listing under a heading, or nothing at all
// when there is nothing to rank.
func logWriteCounts(w io.Writer, title, subject string, counts []log.Count) error {
	if len(counts) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n%s\n", title); err != nil {
		return errors.Wrap(err, errors.CodeIO, "cannot write output")
	}
	return output.WriteTable(w, logCountColumns(subject), logCountRows(counts))
}

// logSectionRows flattens a ranked listing into rows carrying the section name,
// which is how a result with several listings becomes one CSV file.
func logSectionRows(section string, counts []log.Count) [][]string {
	rows := make([][]string, 0, len(counts))
	for _, count := range counts {
		rows = append(rows, []string{
			section,
			count.Value,
			fmt.Sprintf("%d", count.Count),
			fmt.Sprintf("%.1f", count.Percent),
		})
	}
	return rows
}

// logSectionColumns is the header for a flattened multi-listing result.
func logSectionColumns(subject string) []output.Column {
	return []output.Column{
		{Title: "section"},
		{Title: subject},
		{Title: "count", Right: true},
		{Title: "percent", Right: true},
	}
}

// warnEmptyAccess says so when a file held no access-log entries at all.
//
// This is a warning rather than an error: the command worked, the file simply
// is not what the user thought. Telling them that is more useful than a page
// of zeroes, and more useful than a failure that makes them doubt the file.
func warnEmptyAccess(env *Env, requests, lines int) {
	if requests > 0 || lines == 0 {
		return
	}
	env.Warn(errors.CodeParse,
		"no access log entries were recognised in this file; "+
			"the http, status, and top commands read the Common and Combined Log Formats")
}

// warnTruncatedRanking says so when a file held more distinct values than the
// counter tracks, because a ranking of a subset must not read as a ranking of
// the whole.
func warnTruncatedRanking(env *Env, truncated bool) {
	if !truncated {
		return
	}
	env.Warn(errors.CodeUnsupported,
		"this file holds more distinct paths or clients than are tracked; "+
			"the ranking covers the ones that were")
}

func logShare(percent float64) string {
	return fmt.Sprintf("%.1f%%", percent)
}

// logUnparsedNote describes the lines a parser could not read, or says nothing
// when it read them all.
func logUnparsedNote(unparsed int) string {
	if unparsed == 0 {
		return ""
	}
	return fmt.Sprintf(" (%s unreadable line(s) skipped)", output.Count(unparsed))
}
