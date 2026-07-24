package env

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Variable is one environment variable.
type Variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Masked says the value was replaced because the name looks like a
	// credential. The result is exported and pasted into tickets, so this is
	// a property of the data rather than of one rendering of it.
	Masked bool `json:"masked"`
	// Entries is how many items a path-like variable holds, so PATH can be
	// reported as a count instead of two thousand characters.
	Entries int `json:"entries,omitempty"`
}

// VarsRequest describes one environment listing.
type VarsRequest struct {
	// Pattern filters by name, matched case-insensitively as a substring or
	// a glob. Empty lists the development-relevant set.
	Pattern string
	// All lists every variable rather than the relevant ones.
	All bool
	// Reveal prints credential-shaped values in full. Off by default, and
	// the command that offers it says why.
	Reveal bool
}

// VarsResult is the listing.
type VarsResult struct {
	Variables []Variable `json:"variables"`
	Total     int        `json:"total"`
	Masked    int        `json:"masked"`
}

// secretMarkers are the name fragments that mean a value should not be shown.
//
// Matching on the name rather than the value is deliberate. Deciding that a
// value looks like a credential means guessing, and a guess that is wrong in
// the safe direction prints somebody's token into a terminal recording.
var secretMarkers = []string{
	"secret", "token", "password", "passwd", "pwd", "key", "credential",
	"auth", "apikey", "api_key", "access", "private", "signature", "session",
	"cookie", "salt", "cert", "license", "licence",
}

// relevantPrefixes are the variables a developer actually cares about. The
// full environment on a modern desktop is two hundred entries of which four
// are interesting.
var relevantPrefixes = []string{
	"GO", "NODE", "NPM", "PNPM", "YARN", "BUN", "DENO",
	"PYTHON", "PIP", "VIRTUAL_ENV", "CONDA",
	"JAVA", "MAVEN", "GRADLE", "DOTNET", "NUGET",
	"RUST", "CARGO", "RUBY", "GEM", "BUNDLE", "PHP", "COMPOSER",
	"DOCKER", "KUBE", "AWS", "AZURE", "GOOGLE", "GCLOUD", "TF_",
	"GIT", "EDITOR", "VISUAL", "PAGER", "SHELL", "TERM", "LANG", "LC_",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "CI", "DEVNEST",
}

// exactRelevant are names with no useful prefix that still belong in the
// listing.
var exactRelevant = []string{
	"PATH", "HOME", "USERPROFILE", "TMPDIR", "TEMP", "TMP",
	"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "PWD", "OS", "PROCESSOR_ARCHITECTURE",
}

// Vars lists environment variables.
//
// Values whose name looks like a credential are masked in the result itself,
// not in one rendering of it. A report gets attached to a ticket and a ticket
// gets shared, so masking in the table and leaving the JSON unredacted would
// be a leak with a delay on it.
func Vars(ctx context.Context, deps Describer, request VarsRequest) (VarsResult, error) {
	if err := ctx.Err(); err != nil {
		return VarsResult{}, err
	}

	pattern := strings.ToLower(strings.TrimSpace(request.Pattern))
	result := VarsResult{Variables: []Variable{}}

	for name, value := range deps.Environ() {
		if !wanted(name, pattern, request.All) {
			continue
		}

		variable := Variable{Name: name, Value: value}
		if looksLikePath(name, value) {
			variable.Entries = len(splitList(value))
		}
		if isSecret(name) && !request.Reveal {
			variable.Value = mask(value)
			variable.Masked = true
			result.Masked++
		}

		result.Variables = append(result.Variables, variable)
	}

	sort.Slice(result.Variables, func(i, j int) bool {
		return result.Variables[i].Name < result.Variables[j].Name
	})
	result.Total = len(result.Variables)
	return result, nil
}

// wanted decides whether a variable belongs in the listing.
func wanted(name, pattern string, all bool) bool {
	if pattern != "" {
		lowered := strings.ToLower(name)
		if strings.Contains(lowered, pattern) {
			return true
		}
		matched, err := filepath.Match(pattern, lowered)
		return err == nil && matched
	}
	if all {
		return true
	}
	return isRelevant(name)
}

func isRelevant(name string) bool {
	upper := strings.ToUpper(name)
	for _, exact := range exactRelevant {
		if upper == exact {
			return true
		}
	}
	for _, prefix := range relevantPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func isSecret(name string) bool {
	lowered := strings.ToLower(name)
	for _, marker := range secretMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// mask replaces a value with its length, keeping nothing of the value itself.
//
// Not the first few characters. A prefix is enough to identify which key it is
// and, for some formats, enough to start guessing the rest.
func mask(value string) string {
	if value == "" {
		return ""
	}
	if len(value) == 1 {
		return "(hidden, 1 character)"
	}
	return fmt.Sprintf("(hidden, %d characters)", len(value))
}

// looksLikePath reports whether a value is a list of directories.
func looksLikePath(name, value string) bool {
	upper := strings.ToUpper(name)
	if upper == "PATH" || strings.HasSuffix(upper, "PATH") {
		return strings.Contains(value, string(os.PathListSeparator))
	}
	return false
}

func splitList(value string) []string {
	var entries []string
	for _, entry := range strings.Split(value, string(os.PathListSeparator)) {
		if trimmed := strings.TrimSpace(entry); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}
