package config

import (
	"strconv"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// kind is the value type a configuration key accepts.
type kind int

const (
	kindString kind = iota
	kindBool
	kindInt
	kindFloat
	kindStringList
)

func (k kind) String() string {
	switch k {
	case kindBool:
		return "boolean"
	case kindInt:
		return "integer"
	case kindFloat:
		return "float"
	case kindStringList:
		return "array of strings"
	default:
		return "string"
	}
}

// field describes one configuration key. One table drives both file binding
// and environment variable binding, so the two can never disagree about which
// keys exist or what they accept.
type field struct {
	section string
	key     string
	kind    kind
	set     func(*Config, any)
}

func fields() []field {
	return []field{
		{"general", "output", kindString, func(c *Config, v any) { c.General.Output = v.(string) }},
		{"general", "color", kindString, func(c *Config, v any) { c.General.Color = v.(string) }},
		{"general", "verbosity", kindString, func(c *Config, v any) { c.General.Verbosity = v.(string) }},
		{"general", "confirm", kindBool, func(c *Config, v any) { c.General.Confirm = v.(bool) }},

		{"scan", "follow_symlinks", kindBool, func(c *Config, v any) { c.Scan.FollowSymlinks = v.(bool) }},
		{"scan", "respect_ignore", kindBool, func(c *Config, v any) { c.Scan.RespectIgnore = v.(bool) }},
		{"scan", "max_depth", kindInt, func(c *Config, v any) { c.Scan.MaxDepth = v.(int64) }},
		{"scan", "exclude", kindStringList, func(c *Config, v any) { c.Scan.Exclude = v.([]string) }},

		{"clean", "patterns", kindStringList, func(c *Config, v any) { c.Clean.Patterns = v.([]string) }},
		{"clean", "protect", kindStringList, func(c *Config, v any) { c.Clean.Protect = v.([]string) }},
		{"clean", "require_confirm", kindBool, func(c *Config, v any) { c.Clean.RequireConfirm = v.(bool) }},

		// devnest:allow-secret
		{"secret", "entropy_threshold", kindFloat, func(c *Config, v any) { c.Secret.EntropyThreshold = v.(float64) }},
		{"secret", "exclude_paths", kindStringList, func(c *Config, v any) { c.Secret.ExcludePaths = v.([]string) }},
		{"secret", "custom_rules", kindStringList, func(c *Config, v any) { c.Secret.CustomRules = v.([]string) }},

		{"security", "password_length", kindInt, func(c *Config, v any) { c.Security.PasswordLength = v.(int64) }},
		{"security", "password_symbols", kindBool, func(c *Config, v any) { c.Security.PasswordSymbols = v.(bool) }},
		{"security", "password_exclude_ambiguous", kindBool, func(c *Config, v any) {
			c.Security.PasswordExcludeAmbiguous = v.(bool)
		}},

		{"network", "timeout_ms", kindInt, func(c *Config, v any) { c.Network.TimeoutMs = v.(int64) }},
		{"network", "follow_redirect", kindBool, func(c *Config, v any) { c.Network.FollowRedirect = v.(bool) }},
		{"network", "max_redirects", kindInt, func(c *Config, v any) { c.Network.MaxRedirects = v.(int64) }},
		{"network", "attempts", kindInt, func(c *Config, v any) { c.Network.Attempts = v.(int64) }},
		{"network", "interval_ms", kindInt, func(c *Config, v any) { c.Network.IntervalMs = v.(int64) }},

		{"export", "directory", kindString, func(c *Config, v any) { c.Export.Directory = v.(string) }},
		{"export", "timestamp_files", kindBool, func(c *Config, v any) { c.Export.TimestampFiles = v.(bool) }},
	}
}

func fieldIndex() map[string]field {
	index := make(map[string]field, len(fields()))
	for _, f := range fields() {
		index[f.section+"."+f.key] = f
	}
	return index
}

// envName returns the environment variable that sets this key, for example
// DEVNEST_SCAN_MAX_DEPTH.
func (f field) envName() string {
	return "DEVNEST_" + strings.ToUpper(f.section) + "_" + strings.ToUpper(f.key)
}

// coerce converts a value that came from a configuration file into the type
// this key accepts, rejecting anything that does not match.
func (f field) coerce(value any, where string) (any, error) {
	switch f.kind {
	case kindString:
		if v, ok := value.(string); ok {
			return v, nil
		}
	case kindBool:
		if v, ok := value.(bool); ok {
			return v, nil
		}
	case kindInt:
		if v, ok := value.(int64); ok {
			return v, nil
		}
	case kindFloat:
		switch v := value.(type) {
		case float64:
			return v, nil
		case int64:
			return float64(v), nil
		}
	case kindStringList:
		list, ok := value.([]any)
		if !ok {
			break
		}
		out := make([]string, 0, len(list))
		for _, item := range list {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New(errors.CodeParse,
					"%s: %s.%s expects an array of strings", where, f.section, f.key)
			}
			out = append(out, text)
		}
		return out, nil
	}
	return nil, errors.New(errors.CodeParse,
		"%s: %s.%s expects a %s, found %s", where, f.section, f.key, f.kind, typeName(value))
}

// parseString converts an environment variable value into the type this key
// accepts. Lists are comma-separated.
func (f field) parseString(text, where string) (any, error) {
	switch f.kind {
	case kindString:
		return text, nil
	case kindBool:
		value, err := strconv.ParseBool(strings.TrimSpace(text))
		if err != nil {
			return nil, errors.New(errors.CodeInvalidInput,
				"%s expects a boolean, found %q", where, text)
		}
		return value, nil
	case kindInt:
		value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, errors.New(errors.CodeInvalidInput,
				"%s expects an integer, found %q", where, text)
		}
		return value, nil
	case kindFloat:
		value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil {
			return nil, errors.New(errors.CodeInvalidInput,
				"%s expects a number, found %q", where, text)
		}
		return value, nil
	default:
		text = strings.TrimSpace(text)
		if text == "" {
			return []string{}, nil
		}
		parts := strings.Split(text, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out, nil
	}
}

func typeName(value any) string {
	switch value.(type) {
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case int64:
		return "an integer"
	case float64:
		return "a float"
	case []any:
		return "an array"
	default:
		return "an unrecognised value"
	}
}

// bind applies parsed file entries to config, reporting keys DevNest does not
// recognise as warnings rather than failing.
func bind(config *Config, entries []entry) ([]Warning, error) {
	index := fieldIndex()
	var warnings []Warning

	for _, e := range entries {
		where := e.where()
		f, known := index[e.section+"."+e.key]
		if !known {
			warnings = append(warnings, Warning{
				Message: "unknown configuration key " + e.section + "." + e.key,
				Source:  where,
			})
			continue
		}
		value, err := f.coerce(e.value, where)
		if err != nil {
			return warnings, err
		}
		f.set(config, value)
	}

	return warnings, nil
}

// applyEnv overlays environment variables on top of the file values.
func applyEnv(config *Config, lookup func(string) (string, bool)) ([]Warning, error) {
	for _, f := range fields() {
		name := f.envName()
		text, ok := lookup(name)
		if !ok {
			continue
		}
		value, err := f.parseString(text, name)
		if err != nil {
			return nil, err
		}
		f.set(config, value)
	}
	return nil, nil
}
