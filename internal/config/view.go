package config

import (
	"sort"
	"strings"

	"github.com/devnest/devnest/internal/errors"
)

// Origins name the layer a value came from. "Why is it behaving like that" is
// the question the configuration commands exist to answer, and the layer is
// most of the answer.
const (
	OriginDefault     = "default"
	OriginFile        = "file"
	OriginEnvironment = "environment"
	OriginFlag        = "flag"
)

// Value is one resolved configuration key.
type Value struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Kind   string `json:"kind"`
	Origin string `json:"origin"`
}

// Keys returns every configuration key, in the order the file declares them.
func Keys() []string {
	all := fields()
	keys := make([]string, 0, len(all))
	for _, f := range all {
		keys = append(keys, f.section+"."+f.key)
	}
	return keys
}

// Describe pairs every key with its resolved value and where that value came
// from. A key missing from origins is a default, which is the ordinary case.
func Describe(config Config, origins map[string]string) []Value {
	all := fields()
	values := make([]Value, 0, len(all))

	for _, f := range all {
		key := f.section + "." + f.key
		origin := origins[key]
		if origin == "" {
			origin = OriginDefault
		}
		values = append(values, Value{
			Key:    key,
			Value:  f.get(config),
			Kind:   f.kind.String(),
			Origin: origin,
		})
	}
	return values
}

// Lookup returns one key's resolved value, or an error naming the nearest
// keys when the name is not one DevNest has.
func Lookup(config Config, origins map[string]string, key string) (Value, error) {
	for _, value := range Describe(config, origins) {
		if value.Key == key {
			return value, nil
		}
	}

	err := errors.New(errors.CodeNotFound, "unknown configuration key %q", key)
	if suggestions := nearest(key); len(suggestions) > 0 {
		return Value{}, err.WithHint("did you mean %s?", strings.Join(suggestions, ", "))
	}
	return Value{}, err.WithHint("run \"devnest config list\" to see every key")
}

// nearest returns the keys that share a section or an ending with the name
// given, which covers the two ways a key is usually mistyped: the wrong
// section, and the right idea under a different name.
func nearest(key string) []string {
	section, name, _ := strings.Cut(key, ".")
	if name == "" {
		section, name = "", key
	}

	var suggestions []string
	for _, candidate := range Keys() {
		candidateSection, candidateName, _ := strings.Cut(candidate, ".")
		if candidateName == name || (section != "" && candidateSection == section) {
			suggestions = append(suggestions, candidate)
		}
	}

	sort.Strings(suggestions)
	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}
	return suggestions
}

// Parse converts a value written on the command line into the type a key
// accepts, so that "devnest config set" rejects a bad value before it reaches
// the file rather than after.
func Parse(key, text string) (any, error) {
	f, ok := fieldIndex()[key]
	if !ok {
		if _, err := Lookup(Default(), nil, key); err != nil {
			return nil, err
		}
	}
	return f.parseString(text, key)
}

// Apply sets one key on a configuration, for validating a change before it is
// written.
func Apply(config Config, key string, value any) (Config, error) {
	f, ok := fieldIndex()[key]
	if !ok {
		return config, errors.New(errors.CodeNotFound, "unknown configuration key %q", key)
	}
	f.set(&config, value)
	return config, nil
}
