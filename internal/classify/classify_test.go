package classify

import "testing"

func TestOfNamesWhatAPathIs(t *testing.T) {
	tests := map[string]Category{
		"main.go":                        CategorySource,
		"src/app.ts":                     CategorySource,
		"internal/cli/root.go":           CategorySource,
		"main_test.go":                   CategoryTest,
		"src/app.spec.ts":                CategoryTest,
		"tests/helper.go":                CategoryTest,
		"testdata/access.log":            CategoryTest,
		"test_thing.py":                  CategoryTest,
		"api/service.pb.go":              CategoryGenerated,
		"web/bundle.min.js":              CategoryGenerated,
		"node_modules/left-pad/index.js": CategoryVendored,
		"vendor/github.com/x/y.go":       CategoryVendored,
		"dist/bundle.js":                 CategoryBuild,
		"target/debug/app":               CategoryBuild,
		"__pycache__/thing.pyc":          CategoryBuild,
		"README.md":                      CategoryDocs,
		"docs/guide.txt":                 CategoryDocs,
		"go.mod":                         CategoryConfig,
		".gitignore":                     CategoryConfig,
		"config/settings.toml":           CategoryConfig,
		"assets/logo.png":                CategoryAsset,
		"fonts/inter.woff2":              CategoryAsset,
		"unknown.qqq":                    CategoryOther,
	}

	for path, want := range tests {
		if got := Of(path); got != want {
			t.Errorf("Of(%q) = %q, want %q", path, got, want)
		}
	}
}

// Order is the whole design: a test file inside node_modules is vendored, not
// a test, because "whose code is this" is answered before "what is it for".
func TestOfAnswersOwnershipFirst(t *testing.T) {
	tests := map[string]Category{
		"node_modules/lib/index_test.js": CategoryVendored,
		"node_modules/lib/README.md":     CategoryVendored,
		"dist/app.spec.js":               CategoryBuild,
		"vendor/thing/dist/x.js":         CategoryVendored,
	}

	for path, want := range tests {
		if got := Of(path); got != want {
			t.Errorf("Of(%q) = %q, want %q", path, got, want)
		}
	}
}

// Callers pass slash-form paths (scan runs every path through filepath.ToSlash
// before classifying), and the match is case-insensitive so a Windows
// NODE_MODULES reads the same as a Linux node_modules. A backslash is not
// tested as a separator: it is a literal filename character everywhere except
// Windows, and classifying it as one would be wrong on the platforms where a
// file really can be named that.
func TestOfIsCaseInsensitive(t *testing.T) {
	for _, path := range []string{
		"node_modules/x/index.js",
		"NODE_MODULES/x/index.js",
		"Node_Modules/x/index.js",
	} {
		if got := Of(path); got != CategoryVendored {
			t.Errorf("Of(%q) = %q, want %q", path, got, CategoryVendored)
		}
	}
}

func TestLanguageOfIdentifiesFiles(t *testing.T) {
	tests := map[string]string{
		"main.go":        "Go",
		"app.tsx":        "TypeScript",
		"script.py":      "Python",
		"Makefile":       "Make",
		"makefile":       "Make",
		"Dockerfile":     "Docker",
		"query.sql":      "SQL",
		"style.scss":     "SCSS",
		"data.json":      "JSON",
		"README.md":      "Markdown",
		"deploy.ps1":     "PowerShell",
		"CMakeLists.txt": "CMake",
	}

	for path, want := range tests {
		language, known := LanguageOf(path)
		if !known {
			t.Errorf("LanguageOf(%q) reported unknown", path)
			continue
		}
		if language.Name != want {
			t.Errorf("LanguageOf(%q) = %q, want %q", path, language.Name, want)
		}
	}

	for _, path := range []string{"logo.png", "archive.tar.zst", "noextension"} {
		if _, known := LanguageOf(path); known {
			t.Errorf("LanguageOf(%q) claimed to know the language", path)
		}
	}
}

// A whole-name match beats an extension: go.sum is not Go, however much its
// name suggests otherwise.
func TestLanguageOfPrefersWholeNames(t *testing.T) {
	language, known := LanguageOf("go.sum")
	if !known || language.Name != "Go module" {
		t.Errorf("LanguageOf(go.sum) = %q, want \"Go module\"", language.Name)
	}
	if len(language.Line) != 0 {
		t.Error("go.sum has no comment syntax and should not claim one")
	}
}

// The comment syntax travels with the language, so the line counter cannot
// drift from this table.
func TestLanguagesCarryTheirCommentSyntax(t *testing.T) {
	tests := map[string]string{
		"main.go":   "//",
		"script.py": "#",
		"query.sql": "--",
		"page.html": "",
	}

	for path, want := range tests {
		language, _ := LanguageOf(path)
		got := ""
		if len(language.Line) > 0 {
			got = language.Line[0]
		}
		if got != want {
			t.Errorf("line comment for %q = %q, want %q", path, got, want)
		}
	}

	html, _ := LanguageOf("page.html")
	if len(html.Block) != 1 || html.Block[0][0] != "<!--" {
		t.Errorf("html block comment = %v, want <!-- -->", html.Block)
	}
}

func TestIsCodeSeparatesProgrammingFromData(t *testing.T) {
	code := []string{"main.go", "app.ts", "Makefile", "script.sh"}
	data := []string{"data.json", "README.md", "config.yml", "logo.png"}

	for _, path := range code {
		if !IsCode(path) {
			t.Errorf("IsCode(%q) = false, want true", path)
		}
	}
	for _, path := range data {
		if IsCode(path) {
			t.Errorf("IsCode(%q) = true, want false", path)
		}
	}
}

// The walker uses these to skip a subtree rather than classify every file in
// it afterwards, which is the difference between a scan that takes a second
// and one that takes a minute.
func TestDirectoryHelpers(t *testing.T) {
	if !IsVendoredDirectory("node_modules") || !IsVendoredDirectory("VENDOR") {
		t.Error("a vendored directory was not recognised")
	}
	if !IsBuildDirectory("dist") || !IsBuildDirectory("__pycache__") {
		t.Error("a build directory was not recognised")
	}
	if IsVendoredDirectory("internal") || IsBuildDirectory("src") {
		t.Error("an ordinary directory was treated as vendored or build output")
	}
}
