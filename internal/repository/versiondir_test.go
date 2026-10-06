package repository

import (
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/model"
)

func TestParseVersionDirectoryReadsTheName(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		major     int
		minor     int
		patch     int
		suffix    string
		extension string
	}{
		// The names below are the ones the repository actually uses.
		{name: "qt6_6120", raw: "6120", major: 6, minor: 12},
		{name: "qt6_6113", raw: "6113", major: 6, minor: 11, patch: 3},
		{name: "qt6_6110", raw: "6110", major: 6, minor: 11},
		{name: "qt6_6103", raw: "6103", major: 6, minor: 10, patch: 3},
		{name: "qt6_693", raw: "693", major: 6, minor: 9, patch: 3},
		{name: "qt6_680", raw: "680", major: 6, minor: 8},
		{name: "qt5_5152", raw: "5152", major: 5, minor: 15, patch: 2},
		{name: "qt5_59", raw: "59", major: 5, minor: 9},

		// An extension is taken whole, underscores and all.
		{name: "qt6_673_src_doc_examples", raw: "673", major: 6, minor: 7, patch: 3, extension: "src_doc_examples"},
		{name: "qt6_6110_msvc2022_64", raw: "6110", major: 6, minor: 11, extension: "msvc2022_64"},
		{name: "qt6_663_wasm_singlethread", raw: "663", major: 6, minor: 6, patch: 3, extension: "wasm_singlethread"},
		{name: "qt6_6110_msvc2022_arm64_cross_compiled", raw: "6110", major: 6, minor: 11, extension: "msvc2022_arm64_cross_compiled"},

		// A preview names only the major and minor.
		{name: "qt5_515_preview", raw: "515", major: 5, minor: 15, suffix: "-preview", extension: "preview"},
		{name: "qt5_514_preview", raw: "514", major: 5, minor: 14, suffix: "-preview", extension: "preview"},
		{name: "qt6_62_preview", raw: "62", major: 6, minor: 2, suffix: "-preview", extension: "preview"},
		{name: "qt6_62_wasm_preview", raw: "62", major: 6, minor: 2, suffix: "-preview", extension: "wasm_preview"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVersionDirectory(tt.name)
			if err != nil {
				t.Fatalf("ParseVersionDirectory(%q) = %v", tt.name, err)
			}
			if got.Name != tt.name {
				t.Errorf("name = %q, want %q", got.Name, tt.name)
			}
			want := model.Version{
				Raw:    tt.raw,
				Major:  tt.major,
				Minor:  tt.minor,
				Patch:  tt.patch,
				Suffix: tt.suffix,
			}
			if got.Version != want {
				t.Errorf("version = %+v, want %+v", got.Version, want)
			}
			if got.Extension != tt.extension {
				t.Errorf("extension = %q, want %q", got.Extension, tt.extension)
			}
		})
	}
}

// TestParseVersionDirectoryKeepsTheDirectorySpelling pins that Version.Raw is the
// representation the version was discovered in, not a rendered one, so a caller
// can still tell which repository entry a version came from.
func TestParseVersionDirectoryKeepsTheDirectorySpelling(t *testing.T) {
	tests := map[string]string{
		"qt6_6110":        "6110",
		"qt5_59":          "59",
		"qt5_515_preview": "515",
	}
	for name, want := range tests {
		got, err := ParseVersionDirectory(name)
		if err != nil {
			t.Fatalf("ParseVersionDirectory(%q) = %v", name, err)
		}
		if got.Version.Raw != want {
			t.Errorf("%s: Raw = %q, want %q", name, got.Version.Raw, want)
		}
		if got.Version.String() != want {
			t.Errorf("%s: String() = %q, want %q", name, got.Version.String(), want)
		}
	}
}

// TestParseVersionDirectoryOrdersAPreviewBeforeItsRelease shows why the preview
// suffix exists: the numbers alone cannot separate the two.
func TestParseVersionDirectoryOrdersAPreviewBeforeItsRelease(t *testing.T) {
	preview, err := ParseVersionDirectory("qt6_62_preview")
	if err != nil {
		t.Fatalf("ParseVersionDirectory(preview) = %v", err)
	}
	release, err := ParseVersionDirectory("qt6_620")
	if err != nil {
		t.Fatalf("ParseVersionDirectory(release) = %v", err)
	}

	if c := preview.Version.Compare(release.Version); c >= 0 {
		t.Errorf("preview.Compare(release) = %d, want < 0 (a preview is older)", c)
	}
}

func TestParseVersionDirectoryRejectsNamesThatDoNotFit(t *testing.T) {
	tests := []struct {
		name string
		why  string
	}{
		{"", "empty"},
		{"qt6", "no underscore"},
		{"qt6_", "no version digits"},
		{"qtX_6110", "major is not numeric"},
		{"qt_6110", "no major version digit"},
		{"tools_qtcreator", "not a qt version directory"},
		{"qt6_abcdef", "version is not numeric"},
		{"qt6_7_3_arm64_v8a", "an underscored version the repositories no longer use"},
		{"qt6_6120_preview", "a preview written with a patch digit"},
		{"qt6_611" + strings.Repeat("9", 27), "a patch too large for an int"},
	}

	for _, tt := range tests {
		t.Run(tt.why, func(t *testing.T) {
			got, err := ParseVersionDirectory(tt.name)
			if err == nil {
				t.Fatalf("ParseVersionDirectory(%q) = %+v, want a failure (%s)", tt.name, got, tt.why)
			}
			if !got.Version.IsZero() {
				t.Errorf("version = %+v, want the zero version on failure", got.Version)
			}
		})
	}
}
