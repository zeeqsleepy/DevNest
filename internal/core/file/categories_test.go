package file

import (
	"strings"
	"testing"
)

func TestCategoryOf(t *testing.T) {
	tests := []struct {
		extension string
		want      string
	}{
		{".jpg", CategoryImages},
		{".png", CategoryImages},
		{".pdf", CategoryDocuments},
		{".docx", CategoryDocuments},
		{".mp4", CategoryVideos},
		{".mkv", CategoryVideos},
		{".mp3", CategoryAudio},
		{".zip", CategoryArchives},
		{".go", CategoryCode},
		{".json", CategoryData},
		{".ttf", CategoryFonts},
		{".exe", CategoryExecutables},
		{".unheardof", CategoryOther},
		{"", CategoryOther},
	}

	for _, test := range tests {
		t.Run(test.extension, func(t *testing.T) {
			if got := CategoryOf(test.extension); got != test.want {
				t.Errorf("CategoryOf(%q) = %q, want %q", test.extension, got, test.want)
			}
		})
	}
}

// The table is the source of truth for both organize and filter, so an
// extension appearing twice would make a file's category depend on map
// iteration order.
func TestNoExtensionAppearsInTwoCategories(t *testing.T) {
	owner := make(map[string]string)

	for category, extensions := range categories {
		for _, extension := range extensions {
			if previous, seen := owner[extension]; seen {
				t.Errorf("%s is in both %s and %s", extension, previous, category)
			}
			owner[extension] = category
		}
	}
}

// Extensions have to be stored in the form normalizeExtension produces, or the
// lookup silently misses.
func TestEveryExtensionIsNormalised(t *testing.T) {
	for category, extensions := range categories {
		for _, extension := range extensions {
			if !strings.HasPrefix(extension, ".") {
				t.Errorf("%s in %s has no leading dot", extension, category)
			}
			if extension != strings.ToLower(extension) {
				t.Errorf("%s in %s is not lowercase", extension, category)
			}
			if extension != normalizeExtension(extension) {
				t.Errorf("%s in %s is not in normalised form", extension, category)
			}
		}
	}
}

func TestMatchCategoryIsCaseInsensitive(t *testing.T) {
	for _, spelling := range []string{"Images", "images", "IMAGES", " images "} {
		got, known := MatchCategory(spelling)
		if !known {
			t.Fatalf("MatchCategory(%q) did not match", spelling)
		}
		if got != CategoryImages {
			t.Errorf("MatchCategory(%q) = %q, want %q", spelling, got, CategoryImages)
		}
	}

	if _, known := MatchCategory("Photographs"); known {
		t.Error("MatchCategory matched a name that does not exist")
	}
}

func TestCategoryNamesIncludeOtherLast(t *testing.T) {
	names := CategoryNames()
	if len(names) == 0 {
		t.Fatal("no categories")
	}
	if names[len(names)-1] != CategoryOther {
		t.Errorf("last category = %q, want %q", names[len(names)-1], CategoryOther)
	}
	for index := 1; index < len(names)-1; index++ {
		if names[index-1] > names[index] {
			t.Errorf("categories are not sorted: %v", names)
		}
	}
}

func TestFolderFor(t *testing.T) {
	if got := folderFor(".jpg"); got != "jpg" {
		t.Errorf("folderFor(\".jpg\") = %q, want %q", got, "jpg")
	}
	if got := folderFor(""); got != "no-extension" {
		t.Errorf("folderFor(\"\") = %q, want %q", got, "no-extension")
	}
}

func TestNormalizeExtension(t *testing.T) {
	tests := map[string]string{
		".JPG": ".jpg",
		".jpg": ".jpg",
		"":     "",
		".":    "",
	}
	for input, want := range tests {
		if got := normalizeExtension(input); got != want {
			t.Errorf("normalizeExtension(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeExtensionArgument(t *testing.T) {
	tests := map[string]string{
		"pdf":   ".pdf",
		".pdf":  ".pdf",
		" PDF ": ".pdf",
		"":      "",
	}
	for input, want := range tests {
		if got := NormalizeExtensionArgument(input); got != want {
			t.Errorf("NormalizeExtensionArgument(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExtensionsInIsSortedAndCopied(t *testing.T) {
	first := ExtensionsIn(CategoryImages)
	if len(first) == 0 {
		t.Fatal("Images has no extensions")
	}
	for index := 1; index < len(first); index++ {
		if first[index-1] > first[index] {
			t.Fatalf("not sorted: %v", first)
		}
	}

	first[0] = ".mutated"
	if ExtensionsIn(CategoryImages)[0] == ".mutated" {
		t.Error("the caller can mutate the category table")
	}
}
