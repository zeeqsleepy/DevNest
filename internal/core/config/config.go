// Package config is the command-facing view of DevNest's own configuration:
// what the values are, where each one came from, and how to change one.
//
// Thin by design. Resolving and merging live in internal/config, which every
// command already goes through; this module adds only what a person asking
// "why is it behaving like that" needs, which is the origin of each value and a
// way to edit the file without opening it.
//
// # Editing preserves the file
//
// A configuration file is hand-written and hand-commented. Setting one key
// rewrites one line and leaves everything else, including the comments, exactly
// as it was, rather than re-emitting the file from a parsed model.
//
// # A change is validated before it is written
//
// The edited file is parsed and validated in memory first, so a rejected value
// leaves the previous file in place. Writing a file that then stops DevNest
// from starting would break the one command able to fix it.
package config

import (
	"path/filepath"

	"github.com/devnest/devnest/internal/config"
	"github.com/devnest/devnest/internal/errors"
)

// Request identifies the file a configuration command works on.
type Request struct {
	// Path is the file named with --config. Empty means the default location.
	Path string
	// LookupEnv reads environment variables. Nil means the process environment.
	LookupEnv func(string) (string, bool)
}

// ShowResult is the resolved configuration with the origin of each value.
type ShowResult struct {
	Path   string         `json:"path"`
	Exists bool           `json:"exists"`
	Values []config.Value `json:"values"`
	// FromFile and FromEnvironment count the values that are not defaults,
	// which is the short answer to "is anything overriding this".
	FromFile        int `json:"fromFile"`
	FromEnvironment int `json:"fromEnvironment"`
}

// GetResult is one key.
type GetResult struct {
	config.Value
}

// PathResult is where the configuration file is, and whether it is there.
type PathResult struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// WriteResult reports an edit.
type WriteResult struct {
	Path     string `json:"path"`
	Key      string `json:"key,omitempty"`
	Value    any    `json:"value,omitempty"`
	Previous any    `json:"previous,omitempty"`
	// Changed is false when the file already said what was asked for, which is
	// a success and not a write.
	Changed bool `json:"changed"`
	Created bool `json:"created"`
}

// ValidateResult is what "config validate" found.
type ValidateResult struct {
	Path     string           `json:"path"`
	Exists   bool             `json:"exists"`
	Valid    bool             `json:"valid"`
	Keys     int              `json:"keys"`
	Warnings []config.Warning `json:"warnings"`
}

// Show resolves the configuration and reports where each value came from.
func Show(deps Filesystem, request Request) (ShowResult, error) {
	path, err := resolvePath(request)
	if err != nil {
		return ShowResult{}, err
	}

	resolved, _, origins, err := config.LoadDetailed(source(request, path))
	if err != nil {
		return ShowResult{}, err
	}

	exists, err := deps.Exists(path)
	if err != nil {
		return ShowResult{}, err
	}

	result := ShowResult{Path: path, Exists: exists, Values: config.Describe(resolved, origins)}
	for _, value := range result.Values {
		switch value.Origin {
		case config.OriginFile:
			result.FromFile++
		case config.OriginEnvironment:
			result.FromEnvironment++
		}
	}
	return result, nil
}

// Get returns one key.
func Get(request Request, key string) (GetResult, error) {
	path, err := resolvePath(request)
	if err != nil {
		return GetResult{}, err
	}

	resolved, _, origins, err := config.LoadDetailed(source(request, path))
	if err != nil {
		return GetResult{}, err
	}

	value, err := config.Lookup(resolved, origins, key)
	if err != nil {
		return GetResult{}, err
	}
	return GetResult{Value: value}, nil
}

// Path reports which file is in use.
func Path(deps Filesystem, request Request) (PathResult, error) {
	path, err := resolvePath(request)
	if err != nil {
		return PathResult{}, err
	}

	exists, err := deps.Exists(path)
	if err != nil {
		return PathResult{}, err
	}
	return PathResult{Path: path, Exists: exists}, nil
}

// Set writes one key, creating the file if it is not there yet.
func Set(deps Filesystem, request Request, key, text string) (WriteResult, error) {
	path, err := resolvePath(request)
	if err != nil {
		return WriteResult{}, err
	}

	value, err := config.Parse(key, text)
	if err != nil {
		return WriteResult{}, err
	}

	current, _, origins, err := config.LoadDetailed(source(request, path))
	if err != nil {
		return WriteResult{}, err
	}
	previous, err := config.Lookup(current, origins, key)
	if err != nil {
		return WriteResult{}, err
	}

	// The value is checked against the whole configuration, because a value
	// that is only invalid in combination with another is still invalid.
	candidate, err := config.Apply(current, key, value)
	if err != nil {
		return WriteResult{}, err
	}
	if err := candidate.Validate(); err != nil {
		return WriteResult{}, err
	}

	contents, existed, err := read(deps, path)
	if err != nil {
		return WriteResult{}, err
	}

	edited, err := config.SetInText(contents, key, value)
	if err != nil {
		return WriteResult{}, err
	}
	if err := deps.WriteAtomic(path, edited); err != nil {
		return WriteResult{}, err
	}

	return WriteResult{
		Path:     path,
		Key:      key,
		Value:    value,
		Previous: previous.Value,
		Changed:  true,
		Created:  !existed,
	}, nil
}

// Unset removes one key, so that the default applies again.
func Unset(deps Filesystem, request Request, key string) (WriteResult, error) {
	path, err := resolvePath(request)
	if err != nil {
		return WriteResult{}, err
	}
	if _, err := config.Lookup(config.Default(), nil, key); err != nil {
		return WriteResult{}, err
	}

	contents, existed, err := read(deps, path)
	if err != nil {
		return WriteResult{}, err
	}
	if !existed {
		return WriteResult{Path: path, Key: key}, nil
	}

	edited, removed, err := config.UnsetInText(contents, key)
	if err != nil {
		return WriteResult{}, err
	}
	if !removed {
		return WriteResult{Path: path, Key: key}, nil
	}
	if err := deps.WriteAtomic(path, edited); err != nil {
		return WriteResult{}, err
	}

	restored, _ := config.Lookup(config.Default(), nil, key)
	return WriteResult{Path: path, Key: key, Value: restored.Value, Changed: true}, nil
}

// Init writes an annotated file holding the current defaults. An existing file
// is never overwritten: it is the one file on the machine that holds decisions
// somebody made by hand.
func Init(deps Filesystem, request Request) (WriteResult, error) {
	path, err := resolvePath(request)
	if err != nil {
		return WriteResult{}, err
	}

	exists, err := deps.Exists(path)
	if err != nil {
		return WriteResult{}, err
	}
	if exists {
		return WriteResult{Path: path}, errors.New(errors.CodeConflict,
			"%s already exists", path).
			WithHint("edit it, or use \"devnest config set\" to change one value")
	}

	if err := deps.WriteAtomic(path, config.Template()); err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Path: path, Changed: true, Created: true}, nil
}

// Validate reports whether the file parses, holds acceptable values, and uses
// keys DevNest recognises.
func Validate(deps Filesystem, request Request) (ValidateResult, error) {
	path, err := resolvePath(request)
	if err != nil {
		return ValidateResult{}, err
	}

	exists, err := deps.Exists(path)
	if err != nil {
		return ValidateResult{}, err
	}
	result := ValidateResult{Path: path, Exists: exists, Warnings: []config.Warning{}}
	if !exists {
		result.Valid = true
		return result, nil
	}

	resolved, warnings, origins, err := config.LoadDetailed(source(request, path))
	if err != nil {
		return result, err
	}
	if warnings != nil {
		result.Warnings = warnings
	}
	for _, origin := range origins {
		if origin == config.OriginFile {
			result.Keys++
		}
	}

	if err := resolved.Validate(); err != nil {
		return result, err
	}
	result.Valid = true
	return result, nil
}

func resolvePath(request Request) (string, error) {
	if request.Path != "" {
		return filepath.Clean(request.Path), nil
	}
	return config.DefaultPath()
}

// source never marks the file as explicit, even when the user named it. For
// every other command a named file that is not there is a fatal error; here it
// is the ordinary state of a machine nobody has configured yet, and the file
// not existing is something these commands report rather than fail on.
func source(request Request, path string) config.Source {
	return config.Source{Path: path, LookupEnv: request.LookupEnv}
}

// read returns the file's contents, treating a missing file as empty, because
// setting a key in a configuration nobody has written yet is the ordinary way
// to start one.
func read(deps Filesystem, path string) (contents []byte, existed bool, err error) {
	exists, err := deps.Exists(path)
	if err != nil || !exists {
		return nil, false, err
	}

	contents, err = deps.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	return contents, true, nil
}
