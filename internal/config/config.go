// Package config resolves DevNest's configuration from four sources, in
// increasing order of precedence: compiled defaults, a configuration file,
// environment variables, and command-line flags.
//
// Flag overrides are applied by the CLI layer after Load returns, because only
// that layer knows which flags the user actually set. Everything below the CLI
// layer receives resolved values and never reads configuration itself.
package config

import (
	"os"
	"path/filepath"

	"github.com/devnest/devnest/internal/errors"
)

// Config is the resolved configuration for one invocation.
type Config struct {
	General  General  `json:"general"`
	Scan     Scan     `json:"scan"`
	Clean    Clean    `json:"clean"`
	Secret   Secret   `json:"secret"`
	Security Security `json:"security"`
	Network  Network  `json:"network"`
	Export   Export   `json:"export"`
}

// General holds settings that apply to every command.
type General struct {
	Output    string `json:"output"`
	Color     string `json:"color"`
	Verbosity string `json:"verbosity"`
	Confirm   bool   `json:"confirm"`
}

// Scan configures project analysis. Consumed from the scan module onward.
type Scan struct {
	FollowSymlinks bool     `json:"followSymlinks"`
	RespectIgnore  bool     `json:"respectIgnore"`
	MaxDepth       int64    `json:"maxDepth"`
	Exclude        []string `json:"exclude"`
}

// Clean configures artifact removal.
type Clean struct {
	Patterns       []string `json:"patterns"`
	Protect        []string `json:"protect"`
	RequireConfirm bool     `json:"requireConfirm"`
}

// Secret configures credential scanning.
//
// There is deliberately no key for user-supplied rules. A rule is a name, a
// severity, an entropy floor, and a pattern with a capture group, and this
// file's format holds flat sections of scalars and string lists: a rule cannot
// be written here without either losing the fields that make it usable or
// teaching the decoder arrays of tables, which it refuses on purpose. If custom
// rules are ever built they get their own file and their own format.
type Secret struct {
	EntropyThreshold float64  `json:"entropyThreshold"`
	ExcludePaths     []string `json:"excludePaths"`
}

// Security configures the defaults for generated passwords.
//
// Nothing secret is stored here, and nothing ever will be. These are the shape
// of a password, not a password: someone who wants twenty-four characters with
// no ambiguous glyphs should say so once rather than on every invocation.
type Security struct {
	PasswordLength           int64 `json:"passwordLength"`
	PasswordSymbols          bool  `json:"passwordSymbols"`
	PasswordExcludeAmbiguous bool  `json:"passwordExcludeAmbiguous"`
}

// Network configures every command that opens a socket.
//
// The section is named for the whole layer rather than for HTTP, because the
// timeout here also bounds a DNS lookup and a TLS handshake. A key called
// "http.timeout_ms" governing how long a name resolution may take would be a
// small lie in a file people hand-edit.
type Network struct {
	TimeoutMs      int64 `json:"timeoutMs"`
	FollowRedirect bool  `json:"followRedirect"`
	MaxRedirects   int64 `json:"maxRedirects"`
	Attempts       int64 `json:"attempts"`
	IntervalMs     int64 `json:"intervalMs"`
}

// Export configures report output.
type Export struct {
	Directory      string `json:"directory"`
	TimestampFiles bool   `json:"timestampFiles"`
}

// Default returns the compiled defaults. A missing configuration file leaves
// exactly these values in place, which is why DevNest works with no setup.
func Default() Config {
	return Config{
		General: General{
			Output:    "table",
			Color:     "auto",
			Verbosity: "warn",
			Confirm:   true,
		},
		Scan: Scan{
			FollowSymlinks: false,
			RespectIgnore:  true,
			MaxDepth:       0,
			Exclude:        []string{".git", "node_modules"},
		},
		Clean: Clean{
			Patterns:       []string{"node_modules", "dist", "build", "target", "__pycache__"},
			Protect:        []string{},
			RequireConfirm: true,
		},
		Secret: Secret{
			// Zero leaves every rule on the floor it was written with. A
			// compiled default higher than those would quietly drop candidates
			// on a machine with no configuration file at all, which is a
			// strange way for a scanner to arrive.
			EntropyThreshold: 0,
			ExcludePaths:     []string{"testdata/", "fixtures/", "*.lock"},
		},
		Security: Security{
			PasswordLength:           20,
			PasswordSymbols:          true,
			PasswordExcludeAmbiguous: false,
		},
		Network: Network{
			TimeoutMs:      30000,
			FollowRedirect: true,
			MaxRedirects:   10,
			Attempts:       3,
			IntervalMs:     200,
		},
		Export: Export{
			Directory:      "reports",
			TimestampFiles: true,
		},
	}
}

// Warning reports a non-fatal configuration problem, such as a key DevNest
// does not recognise. An older binary reading a newer file must still run.
type Warning struct {
	Message string `json:"message"`
	Source  string `json:"source"`
}

// Source describes where configuration should be read from.
type Source struct {
	// Path of the configuration file. An empty path means the default
	// location for this platform.
	Path string
	// Explicit is true when the user named the file with --config. A named
	// file that does not exist is a fatal error; the default one is not.
	Explicit bool
	// ProjectDir is where discovery of a project-local .devnest.toml starts.
	// An empty value disables project configuration entirely, which the
	// configuration-management commands use: they edit the one file, and a
	// project file read into that edit would fight the caller.
	ProjectDir string
	// LookupEnv reads environment variables. Nil means os.LookupEnv.
	LookupEnv func(string) (string, bool)
}

// Load resolves defaults, then the configuration file, then the environment.
// The caller applies flag overrides afterwards and then calls Validate.
func Load(source Source) (Config, []Warning, error) {
	config, warnings, _, err := LoadDetailed(source)
	return config, warnings, err
}

// LoadDetailed is Load with the layer each value came from, keyed by
// "section.key". A key absent from the map came from the compiled defaults.
//
// Only the configuration commands need this. Everything else receives resolved
// values, and a command that behaved differently depending on where a value
// came from would be a command nobody could reason about.
func LoadDetailed(source Source) (Config, []Warning, map[string]string, error) {
	config := Default()
	origins := map[string]string{}

	path := source.Path
	if path == "" {
		resolved, err := DefaultPath()
		if err != nil {
			if source.Explicit {
				return config, nil, origins, err
			}
			// No config directory on this system is not fatal; defaults apply.
			return config, nil, origins, nil
		}
		path = resolved
	}

	fromFile, warnings, err := applyFile(&config, path, source.Explicit)
	for _, key := range fromFile {
		origins[key] = OriginFile
	}
	if err != nil {
		return config, warnings, origins, err
	}

	if source.ProjectDir != "" {
		fromProject, projectWarnings, err := applyProject(&config, source.ProjectDir)
		for _, key := range fromProject {
			origins[key] = OriginProject
		}
		warnings = append(warnings, projectWarnings...)
		if err != nil {
			return config, warnings, origins, err
		}
	}

	lookup := source.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	fromEnv, envWarnings, err := applyEnv(&config, lookup)
	for _, key := range fromEnv {
		origins[key] = OriginEnvironment
	}
	warnings = append(warnings, envWarnings...)
	if err != nil {
		return config, warnings, origins, err
	}

	return config, warnings, origins, nil
}

func applyFile(config *Config, path string, explicit bool) ([]string, []Warning, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if explicit {
				return nil, nil, errors.Wrap(err, errors.CodeNotFound,
					"configuration file %s does not exist", path).
					WithHint("check the path, or omit --config to use the default location")
			}
			return nil, nil, nil
		}
		if os.IsPermission(err) {
			return nil, nil, errors.Wrap(err, errors.CodePermissionDenied,
				"cannot read configuration file %s", path)
		}
		return nil, nil, errors.Wrap(err, errors.CodeIO, "cannot read configuration file %s", path)
	}

	entries, err := parseTOML(path, data)
	if err != nil {
		return nil, nil, err
	}
	return bind(config, entries)
}

// DefaultPath returns the configuration file path for this platform:
// %APPDATA%\devnest on Windows, ~/.config/devnest on Linux, and
// ~/Library/Application Support/devnest on macOS.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", errors.Wrap(err, errors.CodeNotFound, "cannot determine the configuration directory").
			WithHint("pass --config with an explicit path")
	}
	return filepath.Join(dir, "devnest", "config.toml"), nil
}
