package classify

import (
	"path/filepath"
	"strings"
)

// Language is a programming language, plus what a comment looks like in it.
//
// The comment syntax is here rather than in the line counter because it is a
// property of the language, and because a counter that carried its own table
// would drift from this one the first time somebody added a language to only
// one of them.
type Language struct {
	// Name is what the language is called in a report.
	Name string
	// Line are the tokens that start a comment running to end of line.
	Line []string
	// Block are the open and close pairs for a comment spanning lines.
	Block [][2]string
}

// Comment syntaxes, named so the table below reads as a list of languages
// rather than a wall of punctuation.
var (
	cStyle    = Language{Line: []string{"//"}, Block: [][2]string{{"/*", "*/"}}}
	hashStyle = Language{Line: []string{"#"}}
	sqlStyle  = Language{Line: []string{"--"}, Block: [][2]string{{"/*", "*/"}}}
	xmlStyle  = Language{Block: [][2]string{{"<!--", "-->"}}}
	lispStyle = Language{Line: []string{";"}}
)

// with returns a copy of a syntax carrying a language name.
func with(syntax Language, name string) Language {
	syntax.Name = name
	return syntax
}

// languages maps an extension to the language written in it.
//
// Adding a language is one entry. The list is deliberately not exhaustive:
// every entry is a language somebody working on this project might plausibly
// have in a tree, and an unknown extension is reported as unknown rather than
// guessed at.
var languages = map[string]Language{
	".go":    with(cStyle, "Go"),
	".rs":    with(cStyle, "Rust"),
	".c":     with(cStyle, "C"),
	".h":     with(cStyle, "C"),
	".cc":    with(cStyle, "C++"),
	".cpp":   with(cStyle, "C++"),
	".cxx":   with(cStyle, "C++"),
	".hpp":   with(cStyle, "C++"),
	".cs":    with(cStyle, "C#"),
	".java":  with(cStyle, "Java"),
	".kt":    with(cStyle, "Kotlin"),
	".kts":   with(cStyle, "Kotlin"),
	".swift": with(cStyle, "Swift"),
	".scala": with(cStyle, "Scala"),
	".dart":  with(cStyle, "Dart"),
	".js":    with(cStyle, "JavaScript"),
	".mjs":   with(cStyle, "JavaScript"),
	".cjs":   with(cStyle, "JavaScript"),
	".jsx":   with(cStyle, "JavaScript"),
	".ts":    with(cStyle, "TypeScript"),
	".tsx":   with(cStyle, "TypeScript"),
	".php":   with(cStyle, "PHP"),
	".m":     with(cStyle, "Objective-C"),
	".mm":    with(cStyle, "Objective-C"),
	".zig":   with(cStyle, "Zig"),
	".v":     with(cStyle, "V"),
	".proto": with(cStyle, "Protocol Buffers"),
	".css":   {Name: "CSS", Block: [][2]string{{"/*", "*/"}}},
	".scss":  with(cStyle, "SCSS"),
	".less":  with(cStyle, "Less"),

	".py":   with(hashStyle, "Python"),
	".rb":   with(hashStyle, "Ruby"),
	".sh":   with(hashStyle, "Shell"),
	".bash": with(hashStyle, "Shell"),
	".zsh":  with(hashStyle, "Shell"),
	".fish": with(hashStyle, "Shell"),
	".pl":   with(hashStyle, "Perl"),
	".r":    with(hashStyle, "R"),
	".ex":   with(hashStyle, "Elixir"),
	".exs":  with(hashStyle, "Elixir"),
	".nim":  with(hashStyle, "Nim"),
	".jl":   {Name: "Julia", Line: []string{"#"}, Block: [][2]string{{"#=", "=#"}}},
	".tf":   with(hashStyle, "Terraform"),
	".ps1":  {Name: "PowerShell", Line: []string{"#"}, Block: [][2]string{{"<#", "#>"}}},

	".sql": with(sqlStyle, "SQL"),
	".lua": {Name: "Lua", Line: []string{"--"}, Block: [][2]string{{"--[[", "]]"}}},
	".hs":  {Name: "Haskell", Line: []string{"--"}, Block: [][2]string{{"{-", "-}"}}},
	".elm": with(sqlStyle, "Elm"),

	".html": with(xmlStyle, "HTML"),
	".htm":  with(xmlStyle, "HTML"),
	".vue":  with(xmlStyle, "Vue"),
	".svelte": {
		Name:  "Svelte",
		Line:  []string{"//"},
		Block: [][2]string{{"<!--", "-->"}, {"/*", "*/"}},
	},

	".clj":  with(lispStyle, "Clojure"),
	".el":   with(lispStyle, "Emacs Lisp"),
	".lisp": with(lispStyle, "Lisp"),
	".scm":  with(lispStyle, "Scheme"),
}

// namedFiles are files whose language cannot be read off an extension, either
// because they have none or because the extension lies.
var namedFiles = map[string]Language{
	"makefile":           with(hashStyle, "Make"),
	"gnumakefile":        with(hashStyle, "Make"),
	"dockerfile":         with(hashStyle, "Docker"),
	"containerfile":      with(hashStyle, "Docker"),
	"rakefile":           with(hashStyle, "Ruby"),
	"gemfile":            with(hashStyle, "Ruby"),
	"vagrantfile":        with(hashStyle, "Ruby"),
	"cmakelists.txt":     with(hashStyle, "CMake"),
	"go.mod":             with(cStyle, "Go module"),
	"go.sum":             {Name: "Go module"},
	"justfile":           with(hashStyle, "Just"),
	"jenkinsfile":        with(cStyle, "Groovy"),
	"docker-compose.yml": with(hashStyle, "YAML"),
}

// dataLanguages are formats that are not programming languages but are worth
// counting, because a project's shape includes them. They are kept apart from
// the table above so that "source" stays a meaningful word.
var dataLanguages = map[string]Language{
	".json": {Name: "JSON"},
	".yml":  with(hashStyle, "YAML"),
	".yaml": with(hashStyle, "YAML"),
	".toml": with(hashStyle, "TOML"),
	".xml":  with(xmlStyle, "XML"),
	".ini":  {Name: "INI", Line: []string{";", "#"}},
	".md":   {Name: "Markdown", Block: [][2]string{{"<!--", "-->"}}},
	".rst":  {Name: "reStructuredText"},
	".csv":  {Name: "CSV"},
	".bat":  {Name: "Batch", Line: []string{"::", "rem ", "REM "}},
	".cmd":  {Name: "Batch", Line: []string{"::", "rem ", "REM "}},
}

// LanguageOf identifies the language a file is written in.
//
// A whole-name match wins over an extension: "Makefile" has no extension, and
// "go.sum" is not Go however much its name suggests otherwise.
func LanguageOf(path string) (Language, bool) {
	name := strings.ToLower(filepath.Base(filepath.ToSlash(path)))

	if language, known := namedFiles[name]; known {
		return language, true
	}

	extension := strings.ToLower(filepath.Ext(name))
	if extension == "" {
		return Language{}, false
	}
	if language, known := languages[extension]; known {
		return language, true
	}
	if language, known := dataLanguages[extension]; known {
		return language, true
	}
	return Language{}, false
}

// IsCode reports whether an extension belongs to a programming language rather
// than a data or documentation format. The line counter reports both; the
// summary counts only this as source.
func IsCode(path string) bool {
	name := strings.ToLower(filepath.Base(filepath.ToSlash(path)))
	if _, known := namedFiles[name]; known {
		return true
	}
	_, known := languages[strings.ToLower(filepath.Ext(name))]
	return known
}
