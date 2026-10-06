package repository

import (
	"os"
	"path/filepath"
	"testing"
)

// The fixtures live at the repository root so that every package can read the
// same pages.
func fixturePath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
}

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()

	body, err := os.ReadFile(fixturePath(parts...))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return body
}

const (
	archSplitPage = "https://example.invalid/online/qtsdkrepository/windows_x86/desktop/qt6_6110/"
	nestedPage    = "https://example.invalid/online/qtsdkrepository/windows_x86/desktop/qt6_680/"
	flatPage      = "https://example.invalid/online/qtsdkrepository/windows_x86/desktop/qt5_5152/"
	malformedPage = "https://example.invalid/online/qtsdkrepository/malformed"
)

// want is one expected child, in the order the page lists it.
type want struct {
	name  string
	isDir bool
}

func TestParseIndexListsTheChildrenOfAPage(t *testing.T) {
	tests := []struct {
		name     string
		page     string
		fixture  []string
		children []want
	}{
		{
			name:    "architecture directories",
			page:    archSplitPage,
			fixture: []string{"repository", "qt6-arch-split", "index.html"},
			children: []want{
				{"qt6_6110_msvc2022_arm64_cross_compiled", true},
				{"qt6_6110_msvc2022_64", true},
				{"qt6_6110_mingw", true},
			},
		},
		{
			name:    "a version directory that nests one more level",
			page:    nestedPage,
			fixture: []string{"repository", "qt6-nested", "index.html"},
			children: []want{
				{"qt6_680", true},
			},
		},
		{
			name:    "a version directory holding the metadata itself",
			page:    flatPage,
			fixture: []string{"repository", "qt5-flat", "index.html"},
			children: []want{
				{"2020-11-13-0724_meta.7z", false},
				{"2020-11-13-0724_meta.7z.mirrorlist", false},
				{"qt.qt5.5152.win64_msvc2017_64", true},
				{"Updates.xml", false},
				{"Updates.xml.mirrorlist", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := ParseIndex(tt.page, readFixture(t, tt.fixture...))
			if err != nil {
				t.Fatalf("ParseIndex = %v, want no error", err)
			}

			if len(entries) != len(tt.children) {
				t.Fatalf("got %d children, want %d: %+v", len(entries), len(tt.children), entries)
			}
			for i, w := range tt.children {
				if entries[i].Name != w.name || entries[i].IsDir != w.isDir {
					t.Errorf("child %d = %q (dir=%t), want %q (dir=%t)",
						i, entries[i].Name, entries[i].IsDir, w.name, w.isDir)
				}
			}
		})
	}
}

// TestParseIndexResolvesRelativeLinks pins the join: the page URL has to keep its
// trailing slash, or a relative link resolves one level too high.
func TestParseIndexResolvesRelativeLinks(t *testing.T) {
	entries, err := ParseIndex(archSplitPage, readFixture(t, "repository", "qt6-arch-split", "index.html"))
	if err != nil {
		t.Fatalf("ParseIndex = %v", err)
	}

	if got, want := entries[1].URL, archSplitPage+"qt6_6110_msvc2022_64/"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	if got, want := entries[1].Text, "qt6_6110_msvc2022_64/"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

func TestParseIndexRejectsAPageThatIsNotAListing(t *testing.T) {
	tests := []struct {
		name string
		page string
		body []byte
	}{
		{"not found", malformedPage, readFixture(t, "repository", "not-found", "index.html")},
		{"empty", malformedPage, nil},
		{"a fragment of html", malformedPage, []byte("<html><body>hello</body></html>")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseIndex(tt.page, tt.body); err == nil {
				t.Error("ParseIndex = nil error, want a failure")
			}
		})
	}
}

func TestParseIndexRejectsAListingForAnotherPath(t *testing.T) {
	_, err := ParseIndex(nestedPage, readFixture(t, "repository", "qt6-arch-split", "index.html"))
	if err == nil {
		t.Error("ParseIndex = nil error, want a failure for a page describing another path")
	}
}

func TestParseIndexToleratesBrokenMarkup(t *testing.T) {
	tests := []struct {
		name     string
		fixture  []string
		children []want
	}{
		{
			// Mixed case tags, single-quoted, unquoted and empty values, nested
			// markup inside the link text, an anchor with no href, links that
			// name something other than a child of this page, and a link whose
			// text is cut off.
			name:    "odd but readable",
			fixture: []string{"repository", "malformed", "index.html"},
			children: []want{
				{"qt6_680_msvc2022_64", true},
				{"qt6_680_mingw", true},
				{"qt6_680_wasm", true},
				{"qt6_680_llvm_mingw", true},
				{"tail.html", false},
			},
		},
		{
			// The page is cut off inside a start tag, so the last link never
			// completes and is dropped.
			name:    "truncated mid-tag",
			fixture: []string{"repository", "malformed", "truncated.html"},
			children: []want{
				{"whole.html", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := ParseIndex(malformedPage, readFixture(t, tt.fixture...))
			if err != nil {
				t.Fatalf("ParseIndex = %v, want no error", err)
			}

			if len(entries) != len(tt.children) {
				t.Fatalf("got %d children, want %d: %+v", len(entries), len(tt.children), entries)
			}
			for i, w := range tt.children {
				if entries[i].Name != w.name || entries[i].IsDir != w.isDir {
					t.Errorf("child %d = %q (dir=%t), want %q (dir=%t)",
						i, entries[i].Name, entries[i].IsDir, w.name, w.isDir)
				}
			}
		})
	}
}

func TestParseIndexRejectsAnUnusablePageURL(t *testing.T) {
	if _, err := ParseIndex("://not-a-url", []byte("Index of /x")); err == nil {
		t.Error("ParseIndex = nil error, want a failure for an unparsable URL")
	}
}
