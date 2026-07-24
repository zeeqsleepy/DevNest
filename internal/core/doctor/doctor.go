// Package doctor is DevNest's self-check: whether this installation is in
// working order, and where it is not.
//
// # A broken installation is a result
//
// Nothing here returns an error. A configuration file that will not parse, a
// directory that cannot be written to, and a missing git are the answers this
// module exists to produce, and reporting them as failures of the command
// would mean the report never arrives. The caller decides what a failed check
// does to the exit code.
//
// # The output is for a bug report
//
// It is written to be pasted into an issue, which is why paths under the home
// directory are shortened to "~" and the hostname is not reported at all.
// Neither helps anyone read a stack of issues, and both identify the person
// who filed one.
package doctor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/platform/proc"
	"github.com/devnest/devnest/internal/version"
)

// Status is the outcome of one check.
type Status string

const (
	// StatusOK means the check found what it expected.
	StatusOK Status = "ok"
	// StatusWarning means something is absent or unusual but DevNest works.
	// A missing git is the ordinary case: most commands never call it.
	StatusWarning Status = "warning"
	// StatusFailed means something DevNest depends on is broken.
	StatusFailed Status = "failed"
)

// Check is one question and what it found.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	// Hint is what the user can do about it, on the checks where there is an
	// answer worth giving.
	Hint string `json:"hint,omitempty"`
}

// RuleSet is one compiled-in rule table, named and counted.
//
// The counts are supplied by the caller rather than read here, because a
// module may not import another module: the tables belong to secret and clean,
// and the layering that keeps those two independent is worth more than saving
// this struct.
type RuleSet struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Request is what the caller knows and this module cannot ask for itself.
type Request struct {
	// ConfigPath is the file named with --config. Empty means the default
	// location for this platform.
	ConfigPath string
	// ConfigExplicit is true when the user named the file, which is what makes
	// a missing one a failure rather than a fallback to the defaults.
	ConfigExplicit bool
	// RuleSets are the compiled-in rule tables.
	RuleSets []RuleSet
	// OutputFormat and Color are what the CLI resolved for this invocation.
	// They are reported rather than checked: "it printed no colour" is a
	// support question, and the answer is usually in these two values.
	OutputFormat string
	Color        bool
}

// Result is the whole report.
type Result struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Platform  string `json:"platform"`
	GoVersion string `json:"goVersion"`
	Shell     string `json:"shell,omitempty"`
	Terminal  string `json:"terminal,omitempty"`

	Checks []Check `json:"checks"`
	Failed int     `json:"failed"`
	Warned int     `json:"warned"`
	// Healthy is false when any check failed. Warnings do not clear it and do
	// not set it: a machine without git is a healthy machine.
	Healthy bool `json:"healthy"`
}

// toolTimeout bounds the version probe. A tool that will not answer in this
// long is reported as unresponsive, which is itself worth knowing.
const toolTimeout = 5 * time.Second

// externalTools are the programs optional features shell out to. Absence is a
// warning: everything that does not need them keeps working.
var externalTools = []struct {
	name string
	used string
}{
	{"git", "the git commands and \"secret history\""},
}

// Run performs every check.
func Run(ctx context.Context, deps Environment, request Request) Result {
	machine := deps.Describe()
	build := version.Get()

	result := Result{
		Version:   build.Version,
		Commit:    build.Commit,
		Platform:  machine.OS + "/" + machine.Architecture,
		GoVersion: machine.GoVersion,
		Shell:     machine.Shell,
		Terminal:  machine.Terminal,
		Checks: []Check{
			checkConfig(deps, request, machine.Home),
			checkConfigDirectory(deps, request, machine.Home),
			checkRuleSets(request),
			checkOutput(request, machine.Terminal),
		},
	}
	result.Checks = append(result.Checks, checkTools(ctx, deps, machine.Home)...)

	for _, check := range result.Checks {
		switch check.Status {
		case StatusFailed:
			result.Failed++
		case StatusWarning:
			result.Warned++
		}
	}
	result.Healthy = result.Failed == 0

	return result
}

// checkConfig loads the configuration the same way a real command does, which
// is the only way to find out whether a real command would start.
func checkConfig(deps Environment, request Request, home string) Check {
	const name = "configuration"

	path, err := configPath(request)
	if err != nil {
		return Check{
			Name:   name,
			Status: StatusWarning,
			Detail: "no configuration directory on this system; compiled defaults apply",
			Hint:   "pass --config with an explicit path to use a file",
		}
	}
	display := shorten(path, home)

	exists, err := deps.Exists(path)
	if err != nil {
		return Check{Name: name, Status: StatusFailed, Detail: err.Error()}
	}
	if !exists {
		if request.ConfigExplicit {
			return Check{
				Name:   name,
				Status: StatusFailed,
				Detail: display + " does not exist",
				Hint:   "check the path, or omit --config to use the default location",
			}
		}
		return Check{
			Name:   name,
			Status: StatusOK,
			Detail: "no file at " + display + "; compiled defaults apply",
		}
	}

	loaded, warnings, err := config.Load(config.Source{Path: path, Explicit: request.ConfigExplicit})
	if err != nil {
		return Check{
			Name:   name,
			Status: StatusFailed,
			Detail: err.Error(),
			Hint:   "fix the file, or move it aside to fall back to the defaults",
		}
	}
	if err := loaded.Validate(); err != nil {
		return Check{
			Name:   name,
			Status: StatusFailed,
			Detail: display + " parses but holds a value DevNest will not accept: " + err.Error(),
		}
	}
	if len(warnings) > 0 {
		return Check{
			Name:   name,
			Status: StatusWarning,
			Detail: fmt.Sprintf("%s parses, with %d thing(s) DevNest did not recognise: %s",
				display, len(warnings), warnings[0].Message),
			Hint: "a key from a newer version is ignored, not an error",
		}
	}

	return Check{Name: name, Status: StatusOK, Detail: display + " parses"}
}

// checkConfigDirectory answers the question behind "why did my setting not
// stick": whether the directory the file lives in accepts a write at all.
func checkConfigDirectory(deps Environment, request Request, home string) Check {
	const name = "configuration directory"

	path, err := configPath(request)
	if err != nil {
		return Check{Name: name, Status: StatusWarning, Detail: "this system has no configuration directory"}
	}

	directory := filepath.Dir(path)
	display := shorten(directory, home)

	exists, err := deps.Exists(directory)
	if err != nil {
		return Check{Name: name, Status: StatusFailed, Detail: err.Error()}
	}
	if !exists {
		return Check{
			Name:   name,
			Status: StatusWarning,
			Detail: display + " does not exist yet",
			Hint:   "it is created the first time a setting is written",
		}
	}

	if err := deps.Writable(directory); err != nil {
		return Check{
			Name:   name,
			Status: StatusFailed,
			Detail: display + " is not writable: " + err.Error(),
			Hint:   "DevNest reads configuration from it and writes nothing else there",
		}
	}

	return Check{Name: name, Status: StatusOK, Detail: display + " is writable"}
}

// checkRuleSets verifies that the compiled-in tables arrived. An empty one
// means a scan that finds nothing and says so cheerfully, which is the worst
// failure this tool has.
func checkRuleSets(request Request) Check {
	const name = "rule sets"

	if len(request.RuleSets) == 0 {
		return Check{Name: name, Status: StatusFailed, Detail: "no rule sets are compiled into this binary"}
	}

	parts := make([]string, 0, len(request.RuleSets))
	empty := make([]string, 0, len(request.RuleSets))
	for _, set := range request.RuleSets {
		parts = append(parts, fmt.Sprintf("%s %d", set.Name, set.Count))
		if set.Count == 0 {
			empty = append(empty, set.Name)
		}
	}

	if len(empty) > 0 {
		return Check{
			Name:   name,
			Status: StatusFailed,
			Detail: "empty rule set(s): " + strings.Join(empty, ", "),
			Hint:   "this binary is broken; reinstall it",
		}
	}

	return Check{Name: name, Status: StatusOK, Detail: strings.Join(parts, ", ")}
}

// checkOutput reports what the terminal was detected as and what that led to.
// It never fails: there is no wrong answer here, only a surprising one.
func checkOutput(request Request, terminal string) Check {
	colour := "off"
	if request.Color {
		colour = "on"
	}
	if terminal == "" {
		terminal = "not detected"
	}

	return Check{
		Name:   "output",
		Status: StatusOK,
		Detail: fmt.Sprintf("format %s, colour %s, terminal %s", request.OutputFormat, colour, terminal),
	}
}

// checkTools probes the external programs optional features need. Absence is
// reported once, with what it costs.
func checkTools(ctx context.Context, deps Environment, home string) []Check {
	checks := make([]Check, 0, len(externalTools))

	for _, tool := range externalTools {
		locations := deps.Lookup(tool.name)
		if len(locations) == 0 {
			checks = append(checks, Check{
				Name:   tool.name,
				Status: StatusWarning,
				Detail: "not found on PATH",
				Hint:   tool.used + " need it; everything else works without it",
			})
			continue
		}

		location := shorten(locations[0], home)
		output, err := deps.Run(ctx, proc.Command{
			Name:    tool.name,
			Args:    []string{"--version"},
			Timeout: toolTimeout,
		})
		if err != nil || output.ExitCode != 0 {
			checks = append(checks, Check{
				Name:   tool.name,
				Status: StatusWarning,
				Detail: location + " would not report a version",
			})
			continue
		}

		checks = append(checks, Check{
			Name:   tool.name,
			Status: StatusOK,
			Detail: firstLine(output.Stdout) + " at " + location,
		})
	}

	return checks
}

func configPath(request Request) (string, error) {
	if request.ConfigPath != "" {
		return request.ConfigPath, nil
	}
	return config.DefaultPath()
}

// shorten replaces the home directory with "~", so a report can be pasted into
// an issue without naming the person who filed it.
func shorten(path, home string) string {
	if home == "" || len(path) < len(home) {
		return path
	}
	if !strings.EqualFold(path[:len(home)], home) {
		return path
	}
	return "~" + path[len(home):]
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}
