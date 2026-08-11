package config

import (
	"os"
	"path/filepath"

	"github.com/devnest/devnest/internal/errors"
)

// projectFileName is the file a project commits to carry its settings.
//
// It is found by walking up from the working directory, so running DevNest
// from anywhere inside a project picks up its settings without a flag.
const projectFileName = ".devnest.toml"

// projectAllowed lists the keys a project file may set.
//
// Everything safety-relevant is deliberately absent. A file that travels with
// a clone must never widen what a delete command will remove, must never turn
// off a confirmation, and must never hide paths from a secret scan — those are
// decisions for the machine a command actually runs on, not for code that
// arrives with a repository. What is left is presentation and inspection:
// which format to print, how deep or how complete a scan should be, how long a
// network call may take, and the shape of a generated password. None of it can
// destroy data, start a network call on its own (DevNest never does), or make
// a machine behave differently in a way that costs anything.
var projectAllowed = map[string]bool{
	"general.output":    true,
	"general.color":     true,
	"general.verbosity": true,

	"scan.follow_symlinks": true,
	"scan.respect_ignore":  true,
	"scan.max_depth":       true,
	"scan.exclude":         true,

	"network.timeout_ms":      true,
	"network.follow_redirect": true,
	"network.max_redirects":   true,
	"network.attempts":        true,
	"network.interval_ms":     true,

	"security.password_length":            true,
	"security.password_symbols":           true,
	"security.password_exclude_ambiguous": true,
}

// applyProject overlays a project-local .devnest.toml, found by walking up
// from the working directory, on top of the file values already applied.
//
// Only the allowed keys are bound. A key DevNest does not recognise is a
// warning, exactly as in the main file; a key it recognises but does not allow
// in a project file is a warning too, saying why, so a repository cannot
// quietly carry a setting that does not apply.
func applyProject(config *Config, startDir string) ([]string, []Warning, error) {
	path, err := findProjectFile(startDir)
	if err != nil || path == "" {
		return nil, nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, errors.Wrap(err, errors.CodeIO,
			"cannot read project configuration file %s", path)
	}

	entries, err := parseTOML(path, data)
	if err != nil {
		return nil, nil, err
	}

	index := fieldIndex()
	var warnings []Warning
	var applied []string

	for _, e := range entries {
		key := e.section + "." + e.key
		where := e.where()

		f, known := index[key]
		if !known {
			warnings = append(warnings, Warning{
				Message: "unknown configuration key " + key,
				Source:  where,
			})
			continue
		}
		if !projectAllowed[key] {
			warnings = append(warnings, Warning{
				Message: key + " is not allowed in a project file and has been ignored",
				Source:  where,
			})
			continue
		}

		value, err := f.coerce(e.value, where)
		if err != nil {
			return applied, warnings, err
		}
		f.set(config, value)
		applied = append(applied, key)
	}

	return applied, warnings, nil
}

// findProjectFile walks up from a directory looking for a project file.
//
// The walk stops at the filesystem root. It never reads the file a user named
// with --config; discovery and an explicit path are different questions, and
// a named file wins: applyProject is only reached when LoadDetailed has
// already handled the named or default file.
func findProjectFile(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", errors.Wrap(err, errors.CodeIO,
			"cannot resolve the working directory for project configuration")
	}

	for {
		candidate := filepath.Join(dir, projectFileName)
		if entry, err := os.Stat(candidate); err == nil && !entry.IsDir() {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}
