package secret

import (
	"regexp"
	"sort"
	"strings"
)

// Severity ranks a finding. It is a small closed set so that --fail-on can
// compare them, and the order below is the comparison.
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// severityRank orders the severities. Anything unknown sorts lowest, so a
// mistyped configuration cannot accidentally fail a build.
var severityRank = map[string]int{
	SeverityLow:      1,
	SeverityMedium:   2,
	SeverityHigh:     3,
	SeverityCritical: 4,
}

// Rule is one detector.
//
// Keyword is a plain substring that must appear in a line before the pattern
// is tried. Regular expressions are the expensive part of scanning and most
// lines match none of them; a cheap substring test in front of an expensive
// pattern is what makes scanning a large tree tolerable.
//
// Entropy, when non-zero, is a floor the matched value has to clear. It is what
// separates a real key from a placeholder: "sk_live_XXXXXXXXXXXXXXXXXXXX" has
// the right shape and almost no information in it.
type Rule struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Severity    string  `json:"severity"`
	Keyword     string  `json:"keyword,omitempty"`
	Entropy     float64 `json:"entropy,omitempty"`
	// Pattern is the expression as text, which is what the rules listing
	// shows. Compiling it is done once at start-up.
	Pattern string `json:"pattern"`

	compiled *regexp.Regexp
	// group names which capture group holds the credential, when the pattern
	// needs context around it. Zero means the whole match.
	group int
	// tunable marks a rule whose entropy floor is the thing separating a
	// credential from an ordinary string, so a caller may move it.
	//
	// A rule matching a provider's prefix is not tunable: AKIA followed by
	// sixteen uppercase characters is an AWS key identifier whatever it scores,
	// and letting a configured floor hide it would turn a knob for reducing
	// noise into one that silently disables detection.
	tunable bool
}

// rules is the built-in set.
//
// Every entry here is a shape that is a credential and nothing else: a prefix a
// provider assigns, or a header a key file starts with. The generic rules at
// the end, which look for an assignment to something named like a secret, are
// where false positives come from, and they carry an entropy floor for exactly
// that reason.
//
// The set is deliberately not exhaustive. A scanner that recognises two hundred
// providers and cries wolf on every second file gets switched off, and a
// scanner that is switched off finds nothing at all.
var rules = []Rule{
	{
		Name:        "aws-access-key-id",
		Description: "AWS access key identifier",
		Severity:    SeverityHigh,
		Keyword:     "A",
		Entropy:     3.0,
		Pattern:     `\b((?:AKIA|ASIA|ABIA|ACCA)[0-9A-Z]{16})\b`,
		group:       1,
	},
	{
		Name:        "aws-secret-access-key",
		Description: "AWS secret access key",
		Severity:    SeverityCritical,
		Keyword:     "aws",
		Entropy:     4.2,
		Pattern:     `(?i)aws[^\n]{0,30}?['"\s:=]+([A-Za-z0-9/+=]{40})\b`,
		group:       1,
	},
	{
		Name:        "github-token",
		Description: "GitHub personal access, OAuth, or app token",
		Severity:    SeverityCritical,
		Keyword:     "gh",
		Entropy:     3.0,
		Pattern:     `\b((?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,})\b`,
		group:       1,
	},
	{
		Name:        "github-fine-grained-token",
		Description: "GitHub fine-grained personal access token",
		Severity:    SeverityCritical,
		Keyword:     "github_pat_",
		Entropy:     3.0,
		Pattern:     `\b(github_pat_[A-Za-z0-9_]{50,})\b`,
		group:       1,
	},
	{
		Name:        "slack-token",
		Description: "Slack API token",
		Severity:    SeverityHigh,
		Keyword:     "xox",
		Entropy:     3.0,
		Pattern:     `\b(xox[baprs]-[A-Za-z0-9-]{10,})\b`,
		group:       1,
	},
	{
		Name:        "google-api-key",
		Description: "Google API key",
		Severity:    SeverityHigh,
		Keyword:     "AIza",
		Entropy:     3.0,
		Pattern:     `\b(AIza[0-9A-Za-z_-]{35})\b`,
		group:       1,
	},
	{
		Name:        "stripe-secret-key",
		Description: "Stripe live secret or restricted key",
		Severity:    SeverityCritical,
		Keyword:     "_live_",
		Entropy:     3.0,
		Pattern:     `\b((?:sk|rk)_live_[0-9A-Za-z]{16,})\b`,
		group:       1,
	},
	{
		Name:        "openai-api-key",
		Description: "OpenAI API key",
		Severity:    SeverityCritical,
		Keyword:     "sk-",
		Entropy:     3.5,
		Pattern:     `\b(sk-(?:proj-)?[A-Za-z0-9_-]{20,})\b`,
		group:       1,
	},
	{
		Name:        "anthropic-api-key",
		Description: "Anthropic API key",
		Severity:    SeverityCritical,
		Keyword:     "sk-ant-",
		Entropy:     3.0,
		Pattern:     `\b(sk-ant-[A-Za-z0-9_-]{20,})\b`,
		group:       1,
	},
	{
		Name:        "npm-token",
		Description: "npm access token",
		Severity:    SeverityHigh,
		Keyword:     "npm_",
		Entropy:     3.0,
		Pattern:     `\b(npm_[A-Za-z0-9]{36})\b`,
		group:       1,
	},
	{
		Name:        "sendgrid-api-key",
		Description: "SendGrid API key",
		Severity:    SeverityHigh,
		Keyword:     "SG.",
		Entropy:     3.0,
		Pattern:     `\b(SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,})\b`,
		group:       1,
	},
	{
		Name:        "twilio-api-key",
		Description: "Twilio account or API key identifier",
		Severity:    SeverityHigh,
		Keyword:     "S",
		Entropy:     3.0,
		Pattern:     `\b((?:AC|SK)[0-9a-fA-F]{32})\b`,
		group:       1,
	},
	{
		Name:        "private-key",
		Description: "Private key block",
		Severity:    SeverityCritical,
		Keyword:     "PRIVATE KEY",
		Pattern:     `-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY(?: BLOCK)?-----`,
	},
	{
		Name:        "jwt",
		Description: "JSON Web Token",
		Severity:    SeverityMedium,
		Keyword:     "eyJ",
		Entropy:     3.0,
		Pattern:     `\b(eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,})\b`,
		group:       1,
	},
	{
		Name:        "connection-string-password",
		Description: "Password inside a database connection string",
		Severity:    SeverityHigh,
		Keyword:     "://",
		Entropy:     2.5,
		Pattern:     `(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|amqp)://[^\s:@/]+:([^\s:@/]{6,})@`,
		group:       1,
	},
	{
		Name:        "generic-assignment",
		Description: "A value assigned to something named like a secret",
		Severity:    SeverityMedium,
		Entropy:     3.5,
		Pattern: `(?i)\b(?:api[_-]?key|secret|token|passwd|password|credential|auth)\b` +
			`[^\n]{0,20}?['"\s:=]+['"]?([A-Za-z0-9/+_=-]{16,})['"]?`,
		group:   1,
		tunable: true,
	},
}

// compileRules prepares the built-in set once.
//
// A rule whose pattern does not compile is a bug in this file rather than in a
// user's input, so it panics at start-up rather than failing a scan halfway
// through. There is no path here that compiles a pattern from user input.
func compileRules() []Rule {
	prepared := make([]Rule, 0, len(rules))

	for _, rule := range rules {
		rule.compiled = regexp.MustCompile(rule.Pattern)
		prepared = append(prepared, rule)
	}
	return prepared
}

var compiled = compileRules()

// Rules returns the built-in set, sorted by name, for the listing command.
func Rules() []Rule {
	listing := make([]Rule, len(compiled))
	copy(listing, compiled)

	sort.Slice(listing, func(first, second int) bool {
		return listing[first].Name < listing[second].Name
	})
	return listing
}

// selected narrows the rule set to the names a caller asked for.
func selected(names []string) ([]Rule, []string) {
	if len(names) == 0 {
		return compiled, nil
	}

	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		if trimmed := strings.ToLower(strings.TrimSpace(name)); trimmed != "" {
			wanted[trimmed] = true
		}
	}

	chosen := make([]Rule, 0, len(wanted))
	found := make(map[string]bool, len(wanted))

	for _, rule := range compiled {
		if wanted[strings.ToLower(rule.Name)] {
			chosen = append(chosen, rule)
			found[strings.ToLower(rule.Name)] = true
		}
	}

	missing := make([]string, 0)
	for _, name := range names {
		key := strings.ToLower(strings.TrimSpace(name))
		if key != "" && !found[key] {
			missing = append(missing, name)
		}
	}

	return chosen, missing
}

// atLeast reports whether a severity meets a threshold.
func atLeast(severity, threshold string) bool {
	return severityRank[strings.ToLower(severity)] >= severityRank[strings.ToLower(threshold)]
}

// validSeverity reports whether a string names a severity.
func validSeverity(value string) bool {
	_, known := severityRank[strings.ToLower(strings.TrimSpace(value))]
	return known
}
