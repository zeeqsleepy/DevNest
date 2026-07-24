package file

import (
	"sort"
	"strings"
)

// Categories group extensions into the folders "organize" creates and the
// names "filter" accepts.
//
// The table is data, not logic: adding a format is one entry, which is what
// makes this the easiest part of DevNest to contribute to. Extensions are
// stored with their leading dot and in lower case, matching what
// normalizeExtension produces.
//
// When scan and clean arrive and need the same classification, this table
// moves into its own package below the module layer. Two consumers in one
// package do not justify that yet.
const (
	CategoryImages      = "Images"
	CategoryDocuments   = "Documents"
	CategoryVideos      = "Videos"
	CategoryAudio       = "Audio"
	CategoryArchives    = "Archives"
	CategoryCode        = "Code"
	CategoryData        = "Data"
	CategoryFonts       = "Fonts"
	CategoryExecutables = "Executables"
	CategoryOther       = "Other"
)

var categories = map[string][]string{
	CategoryImages: {
		".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg", ".tif", ".tiff",
		".ico", ".heic", ".heif", ".raw", ".cr2", ".nef", ".avif",
	},
	CategoryDocuments: {
		".pdf", ".doc", ".docx", ".odt", ".rtf", ".txt", ".md", ".tex",
		".xls", ".xlsx", ".ods", ".ppt", ".pptx", ".odp", ".epub", ".mobi", ".pages",
	},
	CategoryVideos: {
		// ".ts" is both a TypeScript source file and an MPEG transport
		// stream. An extension can only belong to one category, and on a
		// developer's machine the source file is the overwhelmingly more
		// likely meaning, so it lives in Code.
		".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".m4v",
		".mpg", ".mpeg", ".3gp", ".m2ts",
	},
	CategoryAudio: {
		".mp3", ".wav", ".flac", ".aac", ".ogg", ".oga", ".m4a", ".wma",
		".opus", ".aiff", ".mid", ".midi",
	},
	CategoryArchives: {
		".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz", ".zst",
		".tgz", ".iso", ".cab", ".dmg",
	},
	CategoryCode: {
		".go", ".rs", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".java", ".kt",
		".swift", ".py", ".rb", ".php", ".pl", ".lua", ".r", ".scala", ".dart",
		".js", ".jsx", ".ts", ".tsx", ".vue", ".svelte", ".html", ".htm", ".css",
		".scss", ".sass", ".less", ".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat",
		".cmd", ".sql", ".vim", ".el", ".ex", ".exs", ".erl", ".hs", ".clj", ".zig",
	},
	CategoryData: {
		".json", ".yaml", ".yml", ".toml", ".xml", ".csv", ".tsv", ".ini",
		".cfg", ".conf", ".env", ".parquet", ".db", ".sqlite", ".sqlite3", ".log",
	},
	CategoryFonts: {
		".ttf", ".otf", ".woff", ".woff2", ".eot", ".fon",
	},
	CategoryExecutables: {
		".exe", ".msi", ".appx", ".deb", ".rpm", ".apk", ".app", ".jar", ".bin",
	},
}

// extensionIndex is built once at first use and read afterwards. It is derived
// entirely from the table above, so it is immutable in practice.
var extensionIndex = buildExtensionIndex()

func buildExtensionIndex() map[string]string {
	index := make(map[string]string)
	for category, extensions := range categories {
		for _, extension := range extensions {
			index[extension] = category
		}
	}
	return index
}

// CategoryOf returns the category an extension belongs to. Anything unknown,
// including a file with no extension at all, is Other.
func CategoryOf(extension string) string {
	if category, known := extensionIndex[extension]; known {
		return category
	}
	return CategoryOther
}

// CategoryNames lists every category, in a stable order, for help text and for
// validating --category.
func CategoryNames() []string {
	names := make([]string, 0, len(categories)+1)
	for name := range categories {
		names = append(names, name)
	}
	sort.Strings(names)
	return append(names, CategoryOther)
}

// MatchCategory resolves a category name case-insensitively, so "images" and
// "Images" both work.
func MatchCategory(name string) (string, bool) {
	wanted := strings.ToLower(strings.TrimSpace(name))
	for _, candidate := range CategoryNames() {
		if strings.ToLower(candidate) == wanted {
			return candidate, true
		}
	}
	return "", false
}

// ExtensionsIn lists the extensions belonging to a category.
func ExtensionsIn(category string) []string {
	extensions := append([]string(nil), categories[category]...)
	sort.Strings(extensions)
	return extensions
}

// folderFor returns the directory name used for an extension inside a
// category folder. Files with no extension get a named folder rather than
// being dropped loose, so the layout stays predictable.
func folderFor(extension string) string {
	if extension == "" {
		return "no-extension"
	}
	return strings.TrimPrefix(extension, ".")
}
