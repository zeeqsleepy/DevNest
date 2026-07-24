package cli

import "flag"

// globalFlags are declared once and registered on every command, so a flag
// means the same thing wherever it appears.
//
// The flags described in docs/cli-reference.md that are not here yet
// (--export, --dry-run, --yes, --compact) land with the first command that
// has something to export, delete, or confirm. A global flag that silently
// does nothing is worse than one that does not exist.
type globalFlags struct {
	output        string
	quiet         bool
	verbose       bool
	noColor       bool
	configPath    string
	logFormat     string
	logTimestamps bool
	help          bool
	showVersion   bool
}

func (g *globalFlags) register(set *flag.FlagSet) {
	set.StringVar(&g.output, "output", "", "output format: table, json, csv")
	set.StringVar(&g.output, "o", "", "output format (shorthand)")

	set.BoolVar(&g.quiet, "quiet", false, "suppress all non-error output")
	set.BoolVar(&g.quiet, "q", false, "suppress all non-error output (shorthand)")

	set.BoolVar(&g.verbose, "verbose", false, "debug-level logging to stderr")
	set.BoolVar(&g.verbose, "v", false, "debug-level logging to stderr (shorthand)")

	set.BoolVar(&g.noColor, "no-color", false, "disable coloured output")
	set.StringVar(&g.configPath, "config", "", "path to a configuration file")

	set.StringVar(&g.logFormat, "log-format", "", "log format: text, json (defaults to the output format)")
	set.BoolVar(&g.logTimestamps, "log-timestamps", false, "include timestamps in text logs")

	set.BoolVar(&g.help, "help", false, "show help for this command")
	set.BoolVar(&g.help, "h", false, "show help for this command (shorthand)")

	set.BoolVar(&g.showVersion, "version", false, "show version information")
}

// globalFlagHelp describes the global flags for help output. It is written out
// rather than read back from the flag set so that shorthands stay grouped with
// their long forms and the order is stable.
func globalFlagHelp() []flagHelp {
	return []flagHelp{
		{"-o, --output <format>", "Output format: table, json, csv (default \"table\")"},
		{"    --config <path>", "Path to a configuration file"},
		{"-q, --quiet", "Suppress all non-error output"},
		{"-v, --verbose", "Debug-level logging to stderr"},
		{"    --no-color", "Disable coloured output"},
		{"    --log-format <format>", "Log format: text, json"},
		{"    --log-timestamps", "Include timestamps in text logs"},
		{"-h, --help", "Show help for a command"},
		{"    --version", "Show version information"},
	}
}

type flagHelp struct {
	Name        string
	Description string
}
