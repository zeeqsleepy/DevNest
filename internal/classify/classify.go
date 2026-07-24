// Package classify answers two questions about a path: what kind of file it
// is, and what language it is written in.
//
// It sits below the module layer because more than one module needs the same
// answers and modules may not import each other. `scan` uses it now; `clean`
// and `secret` are the reason it is a package rather than a table inside
// `scan`.
//
// It holds rules and nothing else. No walking, no reading, no policy about
// what to do with the answer. A caller that wants to skip generated files asks
// what a path is and decides for itself.
//
// # Not the same table as core/file
//
// `core/file` has its own extension table, and this is not a duplicate of it.
// That one answers "is this a photo or a document", for sorting a downloads
// folder into shelves. This one answers "is this authored source or something
// a build produced", for reporting what a project is made of. A .png is an
// image to one and an asset to the other, and merging them would mean one
// table serving two questions and answering neither well.
package classify

import (
	"path/filepath"
	"strings"
)

// Category names what a file is for.
type Category string

const (
	// CategorySource is code somebody wrote.
	CategorySource Category = "source"
	// CategoryTest is code that tests the source.
	CategoryTest Category = "test"
	// CategoryGenerated is code a tool wrote and a person should not edit.
	CategoryGenerated Category = "generated"
	// CategoryVendored is somebody else's code, copied in.
	CategoryVendored Category = "vendored"
	// CategoryBuild is output: compiled, bundled, or otherwise produced.
	CategoryBuild Category = "build"
	// CategoryAsset is an image, font, or other binary the project ships.
	CategoryAsset Category = "asset"
	// CategoryDocs is documentation.
	CategoryDocs Category = "docs"
	// CategoryConfig is configuration, lock files, and project metadata.
	CategoryConfig Category = "config"
	// CategoryOther is everything the rules do not recognise.
	CategoryOther Category = "other"
)

// Categories lists every category in reporting order, so a summary shows the
// same rows in the same places whatever a particular project happens to hold.
func Categories() []Category {
	return []Category{
		CategorySource, CategoryTest, CategoryGenerated, CategoryVendored,
		CategoryBuild, CategoryAsset, CategoryDocs, CategoryConfig, CategoryOther,
	}
}

// vendoredDirectories are directories holding code from somewhere else.
//
// These are the directories that make an unfiltered scan meaningless: a small
// Node project reports four hundred thousand files, of which four hundred are
// the project.
var vendoredDirectories = []string{
	"node_modules", "vendor", "bower_components", "jspm_packages",
	"site-packages", "third_party", "thirdparty", "external",
	".venv", "venv", "virtualenv", ".bundle", "packages",
}

// buildDirectories are directories a build writes into.
var buildDirectories = []string{
	"dist", "build", "out", "target", "bin", "obj", "output",
	".next", ".nuxt", ".svelte-kit", ".output", ".parcel-cache",
	"__pycache__", ".pytest_cache", ".gradle", ".terraform",
	"coverage", "htmlcov", ".nyc_output", "reports",
}

// docsDirectories hold documentation whatever the file extensions inside say.
var docsDirectories = []string{"docs", "doc", "documentation"}

// testDirectories hold tests whatever the file names inside say.
var testDirectories = []string{
	"test", "tests", "spec", "specs", "__tests__", "testdata", "fixtures", "e2e",
}

// generatedMarkers are name fragments that mean a tool wrote this file.
var generatedMarkers = []string{
	".pb.go", "_pb2.py", ".pb.cc", ".pb.h", "_generated.", ".generated.",
	".g.dart", ".freezed.dart", "_gen.go", ".designer.cs", ".min.js", ".min.css",
}

// configNames are whole file names that are configuration wherever they sit.
var configNames = []string{
	"go.mod", "go.sum", "go.work", "package.json", "package-lock.json",
	"yarn.lock", "pnpm-lock.yaml", "cargo.toml", "cargo.lock", "gemfile",
	"gemfile.lock", "requirements.txt", "pyproject.toml", "poetry.lock",
	"pipfile", "pipfile.lock", "composer.json", "composer.lock",
	"dockerfile", "docker-compose.yml", "docker-compose.yaml", "makefile",
	".gitignore", ".gitattributes", ".editorconfig", ".dockerignore",
	".env", ".env.example", "license", "licence",
}

// configExtensions are the shapes configuration is usually written in. They
// only apply when nothing more specific matched, because plenty of source
// trees hold data files in the same formats.
var configExtensions = []string{
	".toml", ".ini", ".cfg", ".conf", ".properties", ".editorconfig",
	".yml", ".yaml", ".json", ".json5", ".xml", ".plist",
}

// docExtensions are documentation formats.
var docExtensions = []string{".md", ".markdown", ".rst", ".adoc", ".txt", ".org"}

// assetExtensions are files a project ships rather than compiles.
var assetExtensions = []string{
	".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp", ".bmp", ".avif",
	".woff", ".woff2", ".ttf", ".otf", ".eot",
	".mp3", ".mp4", ".wav", ".webm", ".mov", ".ogg",
	".pdf", ".zip", ".tar", ".gz", ".7z", ".jar", ".wasm",
}

// Of names what a path is, from the path alone.
//
// The path is expected to be relative to the root being scanned, using either
// separator. Only the path is consulted: no file is opened, because a
// classification that costs a read cannot run on every entry of a large tree.
//
// Order is the whole design. A test file inside node_modules is vendored, not
// a test, because the question being answered is "whose code is this" before
// "what is it for". Every rule below is checked in the order written, and
// moving one changes what a project reports.
func Of(path string) Category {
	cleaned := strings.ToLower(filepath.ToSlash(path))
	segments := strings.Split(cleaned, "/")
	name := segments[len(segments)-1]
	directories := segments[:len(segments)-1]

	if containsAny(directories, vendoredDirectories) {
		return CategoryVendored
	}
	if containsAny(directories, buildDirectories) {
		return CategoryBuild
	}
	if hasAnyFragment(name, generatedMarkers) {
		return CategoryGenerated
	}
	if isTestName(name) || containsAny(directories, testDirectories) {
		return CategoryTest
	}

	extension := filepath.Ext(name)

	if containsAny(directories, docsDirectories) || matches(extension, docExtensions) {
		return CategoryDocs
	}
	if matches(name, configNames) {
		return CategoryConfig
	}
	if matches(extension, assetExtensions) {
		return CategoryAsset
	}

	// The configuration shapes are checked before the language table, because
	// the table knows YAML and TOML as languages and a settings file is not
	// source however well-understood its syntax is. The cost is that a JSON
	// fixture counts as configuration, which is still closer to the truth
	// than counting it as code somebody wrote.
	if matches(extension, configExtensions) {
		return CategoryConfig
	}
	if _, known := LanguageOf(name); known {
		return CategorySource
	}
	return CategoryOther
}

// isTestName recognises the naming conventions that mean "this is a test".
func isTestName(name string) bool {
	base := strings.TrimSuffix(name, filepath.Ext(name))

	for _, suffix := range []string{"_test", ".test", ".spec", "_spec", "-test", "-spec"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return strings.HasPrefix(base, "test_")
}

// IsVendoredDirectory reports whether a directory name holds somebody else's
// code. A walker uses this to skip the subtree rather than classify every file
// in it afterwards, which is the difference between a scan that takes a second
// and one that takes a minute.
func IsVendoredDirectory(name string) bool {
	return matches(strings.ToLower(name), vendoredDirectories)
}

// IsBuildDirectory reports whether a directory name is build output.
func IsBuildDirectory(name string) bool {
	return matches(strings.ToLower(name), buildDirectories)
}

func containsAny(segments []string, wanted []string) bool {
	for _, segment := range segments {
		if matches(segment, wanted) {
			return true
		}
	}
	return false
}

func matches(value string, wanted []string) bool {
	for _, candidate := range wanted {
		if value == candidate {
			return true
		}
	}
	return false
}

func hasAnyFragment(value string, fragments []string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
