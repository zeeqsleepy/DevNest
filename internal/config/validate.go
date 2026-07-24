package config

import (
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Output formats DevNest can currently render. CSV arrived with the log
// module, the first group whose results are rows; markdown with --export,
// where it is the format for pasting a report into a ticket.
var outputFormats = []string{"table", "json", "csv", "markdown"}

var (
	colorModes  = []string{"auto", "always", "never"}
	verbosities = []string{"error", "warn", "info", "debug"}
)

// Validate checks the fully merged configuration. It runs after flag overrides
// so that a value only valid in combination with another is checked correctly.
func (c Config) Validate() error {
	if err := oneOf("general.output", c.General.Output, outputFormats); err != nil {
		return err
	}
	if err := oneOf("general.color", c.General.Color, colorModes); err != nil {
		return err
	}
	if err := oneOf("general.verbosity", c.General.Verbosity, verbosities); err != nil {
		return err
	}
	if c.Scan.MaxDepth < 0 {
		return invalid("scan.max_depth", "must be 0 (unlimited) or greater")
	}
	if c.Secret.EntropyThreshold < 0 || c.Secret.EntropyThreshold > 8 {
		return invalid("secret.entropy_threshold", "must be between 0 and 8")
	}
	if c.Security.PasswordLength < 8 || c.Security.PasswordLength > 4096 {
		return invalid("security.password_length", "must be between 8 and 4096")
	}
	if c.Network.TimeoutMs <= 0 {
		return invalid("network.timeout_ms", "must be greater than 0")
	}
	if c.Network.MaxRedirects < 0 {
		return invalid("network.max_redirects", "must be 0 or greater")
	}
	if c.Network.Attempts < 1 || c.Network.Attempts > 1000 {
		return invalid("network.attempts", "must be between 1 and 1000")
	}
	if c.Network.IntervalMs < 0 {
		return invalid("network.interval_ms", "must be 0 or greater")
	}
	if strings.TrimSpace(c.Export.Directory) == "" {
		return invalid("export.directory", "must not be empty")
	}
	return nil
}

func oneOf(key, value string, allowed []string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return errors.New(errors.CodeInvalidInput,
		"invalid value %q for %s", value, key).
		WithHint("expected one of: %s", strings.Join(allowed, ", "))
}

func invalid(key, requirement string) error {
	return errors.New(errors.CodeInvalidInput, "invalid value for %s: %s", key, requirement)
}
