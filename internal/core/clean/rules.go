package clean

import (
	"path/filepath"
	"sort"
	"strings"
)

// Rule is one kind of removable directory.
//
// Markers are the safety mechanism that makes a generic name usable. A
// directory called "build" is build output in a project and somebody's
// woodworking photographs in a home directory; the difference is whether the
// directory containing it also holds a file that says a build system lives
// there. A rule with no markers is one whose name means only one thing
// anywhere: nobody has a personal directory called node_modules.
type Rule struct {
	// Name is the directory name, matched exactly (case-insensitively on
	// platforms whose filesystems are).
	Name string
	// Ecosystem is what produced it, shown in output so a listing explains
	// itself.
	Ecosystem string
	// Markers are globs matched against the names of files in the *parent* of
	// a candidate. Any one of them satisfies the rule; an empty list means the
	// name alone is enough.
	Markers []string
	// Regenerable describes what it costs to remove this, which is the
	// question a user is actually asking when they hesitate.
	Regenerable string
}

// projectMarkers are the files that say "a build system runs here". They are
// shared by the rules whose directory names are too generic to stand alone.
var projectMarkers = []string{
	"package.json", "pnpm-lock.yaml", "yarn.lock",
	"pyproject.toml", "setup.py", "setup.cfg",
	"Cargo.toml", "go.mod",
	"pom.xml", "build.gradle", "build.gradle.kts",
	"CMakeLists.txt", "Makefile", "makefile",
	"*.csproj", "*.fsproj", "*.vbproj", "*.sln",
	"composer.json", "Gemfile", "mix.exs",
}

// rules is the built-in table. Nothing outside it, plus whatever the user
// configured, is ever a candidate: this module never guesses from a heuristic
// such as size or age, because a wrong guess here deletes somebody's work.
//
// Two things are deliberately absent. "vendor" is not here: Go's vendor
// directory is checked into plenty of repositories on purpose and deleting it
// breaks an offline build. Nor is any cache outside the project tree, such as
// ~/.npm or ~/.cargo: this command cleans a project you point it at, and a
// tool that reaches into a home directory to free space is a different, more
// dangerous tool.
var rules = []Rule{
	{
		Name:        "node_modules",
		Ecosystem:   "node",
		Regenerable: "npm install, or the equivalent for your package manager",
	},
	{Name: ".next", Ecosystem: "next.js", Regenerable: "the next build"},
	{Name: ".nuxt", Ecosystem: "nuxt", Regenerable: "the next build"},
	{Name: ".svelte-kit", Ecosystem: "sveltekit", Regenerable: "the next build"},
	{Name: ".parcel-cache", Ecosystem: "parcel", Regenerable: "the next build"},
	{Name: ".turbo", Ecosystem: "turborepo", Regenerable: "the next build"},
	{Name: ".angular", Ecosystem: "angular", Regenerable: "the next build"},

	{Name: "__pycache__", Ecosystem: "python", Regenerable: "the next import"},
	{Name: ".pytest_cache", Ecosystem: "python", Regenerable: "the next test run"},
	{Name: ".mypy_cache", Ecosystem: "python", Regenerable: "the next type check"},
	{Name: ".ruff_cache", Ecosystem: "python", Regenerable: "the next lint"},
	{Name: ".tox", Ecosystem: "python", Regenerable: "the next tox run, which is slow"},

	{
		Name:        "target",
		Ecosystem:   "rust or java",
		Markers:     []string{"Cargo.toml", "pom.xml", "build.gradle", "build.gradle.kts"},
		Regenerable: "the next build, which is slow for a large crate graph",
	},
	{
		Name:        ".gradle",
		Ecosystem:   "gradle",
		Markers:     []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"},
		Regenerable: "the next build",
	},
	{
		Name:        "bin",
		Ecosystem:   "dotnet",
		Markers:     []string{"*.csproj", "*.fsproj", "*.vbproj", "*.sln"},
		Regenerable: "the next build",
	},
	{
		Name:        "obj",
		Ecosystem:   "dotnet",
		Markers:     []string{"*.csproj", "*.fsproj", "*.vbproj", "*.sln"},
		Regenerable: "the next build",
	},
	{
		Name:        "dist",
		Ecosystem:   "build output",
		Markers:     projectMarkers,
		Regenerable: "the next build",
	},
	{
		Name:        "build",
		Ecosystem:   "build output",
		Markers:     projectMarkers,
		Regenerable: "the next build",
	},
	{
		Name:        "out",
		Ecosystem:   "build output",
		Markers:     projectMarkers,
		Regenerable: "the next build",
	},
	{
		Name:        "coverage",
		Ecosystem:   "test output",
		Markers:     projectMarkers,
		Regenerable: "the next test run with coverage",
	},
	{
		Name:        "Pods",
		Ecosystem:   "cocoapods",
		Markers:     []string{"Podfile", "Podfile.lock"},
		Regenerable: "pod install",
	},
}

// Rules returns the built-in table, sorted by name, for the command that lists
// what this module would consider.
func Rules() []Rule {
	listing := make([]Rule, len(rules))
	copy(listing, rules)

	sort.Slice(listing, func(first, second int) bool {
		return listing[first].Name < listing[second].Name
	})
	return listing
}

// ruleSet is the rules in effect for one run: the built-in table, narrowed by
// --pattern, plus anything the user configured.
type ruleSet struct {
	byName map[string]Rule
}

// newRuleSet builds the effective rules.
//
// A configured pattern that names a built-in rule keeps that rule's markers,
// rather than becoming a marker-free rule with the same name. Someone adding
// "build" to their configuration means "yes, also clean build directories",
// not "delete anything called build anywhere".
func newRuleSet(selected, configured []string) *ruleSet {
	set := &ruleSet{byName: make(map[string]Rule, len(rules))}

	wanted := make(map[string]bool, len(selected))
	for _, name := range selected {
		wanted[strings.ToLower(strings.TrimSpace(name))] = true
	}

	for _, rule := range rules {
		if len(wanted) > 0 && !wanted[strings.ToLower(rule.Name)] {
			continue
		}
		set.byName[strings.ToLower(rule.Name)] = rule
	}

	for _, name := range configured {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if len(wanted) > 0 && !wanted[key] {
			continue
		}
		if _, builtIn := set.byName[key]; builtIn {
			continue
		}
		set.byName[key] = Rule{
			Name:        name,
			Ecosystem:   "configured",
			Markers:     projectMarkers,
			Regenerable: "whatever produced it",
		}
	}

	return set
}

// match returns the rule a directory name falls under.
func (r *ruleSet) match(name string) (Rule, bool) {
	rule, found := r.byName[strings.ToLower(name)]
	return rule, found
}

// unknown returns the selected names that match no rule, so a typed pattern
// fails loudly instead of quietly matching nothing.
func (r *ruleSet) unknown(selected []string) []string {
	missing := make([]string, 0, len(selected))

	for _, name := range selected {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		if _, found := r.byName[key]; !found {
			missing = append(missing, name)
		}
	}
	return missing
}

// satisfied reports whether a rule's markers are present among the names of
// the files sitting beside the candidate.
func satisfied(rule Rule, siblings []string) bool {
	if len(rule.Markers) == 0 {
		return true
	}

	for _, sibling := range siblings {
		for _, marker := range rule.Markers {
			if matched, err := filepath.Match(marker, sibling); err == nil && matched {
				return true
			}
		}
	}
	return false
}
