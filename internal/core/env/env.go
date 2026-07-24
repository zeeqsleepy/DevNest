// Package env is DevNest's environment inspection module: what is installed
// on this machine, where it resolves from, and what the environment says.
//
// Everything here is read-only. Nothing in this package writes a file, sets a
// variable, or changes anything about the machine it is describing.
//
// # A missing tool is an answer
//
// Most of what this module reports is absence. A machine without Rust is an
// ordinary machine, and reporting "not found" for it is the correct result
// rather than a failure. Only a request that cannot be carried out at all
// comes back as an error.
//
// The same applies to a probe that hangs or prints something unrecognisable.
// The tool is reported as present with an unknown version, because knowing
// that a binary is there and would not say what it is beats knowing nothing.
//
// # Nothing is trusted to be quick
//
// Every probe is bounded by a timeout and runs without a shell. Toolchain
// version flags are famous for opening a network connection, printing an
// update notice, or waiting on a lock, and a summary that hangs on one of them
// is a summary nobody runs twice.
package env

import (
	"sort"
	"strings"
)

// Kind groups a detected tool, so a summary can show languages apart from the
// build tools and the container tooling rather than as one list of thirty.
type Kind string

const (
	KindLanguage  Kind = "language"
	KindPackage   Kind = "package-manager"
	KindBuild     Kind = "build"
	KindVersion   Kind = "version-control"
	KindContainer Kind = "container"
	KindCloud     Kind = "cloud"
)

// Kinds lists every kind in reporting order.
func Kinds() []Kind {
	return []Kind{KindLanguage, KindPackage, KindBuild, KindVersion, KindContainer, KindCloud}
}

// Tool is one detected, or undetected, program.
type Tool struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Found says whether the executable exists on PATH at all.
	Found bool `json:"found"`
	// Version is what the tool reported, or empty when it would not say.
	Version string `json:"version,omitempty"`
	// Path is the location that would run, which is the first PATH match.
	Path string `json:"path,omitempty"`
	// Shadowed lists the other locations the same name resolves to. A tool
	// reporting an unexpected version nearly always has something here.
	Shadowed []string `json:"shadowed,omitempty"`
	// Detail explains a probe that did not go to plan: a timeout, a non-zero
	// exit, or output nothing could be read out of.
	Detail string `json:"detail,omitempty"`
}

// version extracts a version number from what a tool printed.
//
// Every tool announces itself differently: "go version go1.25 windows/amd64",
// "v22.1.0", "Python 3.12.1", "git version 2.44.0.windows.1". What they share
// is that the first run of digits and dots in the first line is the version.
//
// Hand-written rather than a regular expression, because a package-level
// compiled pattern is work done on every invocation of DevNest including the
// ones that never look at a version. See performance.md on startup.
func version(output string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")

	// A token holding a digit, a dot, and another digit. Every version has
	// that shape and almost nothing else on a version line does:
	// "windows/amd64" has the digit without the dot, "go1.26.2" has both,
	// and stripping the letters off the front is what turns it into a
	// version.
	//
	// A tool whose version is a bare integer therefore reports nothing, and
	// the caller says so. That is the right way round: an unrecognised
	// version is visibly unknown, where taking the first digits from
	// "x86_64-pc-linux-gnu" would confidently report 86.
	for _, token := range strings.Fields(line) {
		if dotted(token) {
			return trim(token)
		}
	}
	return ""
}

// dotted reports whether a token holds a digit, a dot, and another digit.
func dotted(token string) bool {
	for index := 0; index+2 < len(token); index++ {
		if isDigit(token[index]) && token[index+1] == '.' && isDigit(token[index+2]) {
			return true
		}
	}
	return false
}

// trim reduces a token to the version inside it: the leading prefix ("go",
// "v", an opening quote) is dropped, and everything after the digits and dots
// goes with it.
func trim(token string) string {
	start := 0
	for start < len(token) && !isDigit(token[start]) {
		start++
	}

	end := start
	for end < len(token) && (isDigit(token[end]) || token[end] == '.') {
		end++
	}
	return strings.TrimRight(token[start:end], ".")
}

func isDigit(character byte) bool { return character >= '0' && character <= '9' }

// sortTools puts tools in a stable reporting order: by kind as listed in
// Kinds, then by name. Two runs on an unchanged machine produce identical
// output, which is what makes a saved report worth diffing.
func sortTools(tools []Tool) {
	rank := make(map[Kind]int, len(Kinds()))
	for index, kind := range Kinds() {
		rank[kind] = index
	}

	sort.SliceStable(tools, func(i, j int) bool {
		if tools[i].Kind != tools[j].Kind {
			return rank[tools[i].Kind] < rank[tools[j].Kind]
		}
		return tools[i].Name < tools[j].Name
	})
}
