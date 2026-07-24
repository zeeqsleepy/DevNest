package env

import "strings"

// toolchain describes how to detect one tool.
//
// A table entry, not code. Adding a toolchain is a line here and nothing else,
// which is the property that keeps this module from growing a special case per
// language.
type toolchain struct {
	name string
	kind Kind
	// executable is the name looked up on PATH, when it differs from name.
	executable string
	// args produce a version. Almost always --version; the exceptions are
	// the tools that predate the convention.
	args []string
}

// lookupName is the executable to search for.
func (t toolchain) lookupName() string {
	if t.executable != "" {
		return t.executable
	}
	return t.name
}

// toolchains is the detection table, in the order a summary reports it.
//
// The list is deliberately finite. Detecting everything means probing a
// hundred programs on every run, and the cost lands on the person who has
// three of them installed. These are the toolchains a developer working on a
// mixed team plausibly has, and an unknown tool is one table entry away.
func toolchains() []toolchain {
	return []toolchain{
		{name: "go", kind: KindLanguage, args: []string{"version"}},
		{name: "node", kind: KindLanguage, args: []string{"--version"}},
		{name: "deno", kind: KindLanguage, args: []string{"--version"}},
		{name: "bun", kind: KindLanguage, args: []string{"--version"}},
		{name: "python", kind: KindLanguage, args: []string{"--version"}},
		{name: "python3", kind: KindLanguage, args: []string{"--version"}},
		{name: "ruby", kind: KindLanguage, args: []string{"--version"}},
		{name: "php", kind: KindLanguage, args: []string{"--version"}},
		{name: "java", kind: KindLanguage, args: []string{"-version"}},
		{name: "dotnet", kind: KindLanguage, args: []string{"--version"}},
		{name: "rustc", kind: KindLanguage, args: []string{"--version"}},
		{name: "gcc", kind: KindLanguage, args: []string{"--version"}},
		{name: "clang", kind: KindLanguage, args: []string{"--version"}},

		{name: "npm", kind: KindPackage, args: []string{"--version"}},
		{name: "pnpm", kind: KindPackage, args: []string{"--version"}},
		{name: "yarn", kind: KindPackage, args: []string{"--version"}},
		{name: "pip", kind: KindPackage, args: []string{"--version"}},
		{name: "cargo", kind: KindPackage, args: []string{"--version"}},
		{name: "composer", kind: KindPackage, args: []string{"--version"}},
		{name: "gem", kind: KindPackage, args: []string{"--version"}},

		{name: "make", kind: KindBuild, args: []string{"--version"}},
		{name: "cmake", kind: KindBuild, args: []string{"--version"}},
		{name: "gradle", kind: KindBuild, args: []string{"--version"}},
		{name: "maven", kind: KindBuild, executable: "mvn", args: []string{"--version"}},

		{name: "git", kind: KindVersion, args: []string{"--version"}},
		{name: "hg", kind: KindVersion, args: []string{"--version"}},

		{name: "docker", kind: KindContainer, args: []string{"--version"}},
		{name: "podman", kind: KindContainer, args: []string{"--version"}},
		{name: "kubectl", kind: KindContainer, args: []string{"version", "--client"}},

		{name: "terraform", kind: KindCloud, args: []string{"--version"}},
		{name: "aws", kind: KindCloud, args: []string{"--version"}},
		{name: "gcloud", kind: KindCloud, args: []string{"--version"}},
		{name: "az", kind: KindCloud, args: []string{"--version"}},
	}
}

// findToolchain returns the table entry for a name, if there is one. A tool
// nobody has described is still worth locating; it simply has no version
// command to run.
func findToolchain(name string) (toolchain, bool) {
	wanted := strings.ToLower(strings.TrimSpace(name))
	for _, tool := range toolchains() {
		if tool.name == wanted || tool.lookupName() == wanted {
			return tool, true
		}
	}
	return toolchain{}, false
}
