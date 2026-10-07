package repository

import (
	"os"
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
		{"qt6_dev", "the dev channel, which is not a version"},
		{"qt6_dev_wasm_singlethread", "an archive of the dev channel"},
		{"qt6_7_3_arm64_v8a", "an underscored version the repositories no longer use"},
		{"qt6_6120_preview", "a preview token longer than three digits"},
		{"qt6_6_preview", "a preview token of one digit"},
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

// A token's digits often admit more than one split -- "qt5_5152" is 5.15.2 or
// 5.1.52, "qt6_6810" is 6.81.0 or 6.8.10 -- and no rule over the digits alone tells
// them apart. The corpus's per-major minor bound is what settles it, so the
// boundary is tested from both sides: the highest minor a major spells is read, one
// past it is refused, and a patch is never bounded.
func TestParseVersionDirectoryBoundsTheMinor(t *testing.T) {
	accepted := []struct {
		name string
		want model.Version
	}{
		{"qt5_5152", model.Version{Raw: "5152", Major: 5, Minor: 15, Patch: 2}},
		{"qt6_6120", model.Version{Raw: "6120", Major: 6, Minor: 12}},
		{"qt6_695", model.Version{Raw: "695", Major: 6, Minor: 9, Patch: 5}},
	}
	for _, tt := range accepted {
		got, err := ParseVersionDirectory(tt.name)
		if err != nil {
			t.Errorf("ParseVersionDirectory(%q) = %v, want %+v", tt.name, err, tt.want)
			continue
		}
		if got.Version != tt.want {
			t.Errorf("ParseVersionDirectory(%q) = %+v, want %+v", tt.name, got.Version, tt.want)
		}
	}

	refused := []struct {
		name string
		why  string
	}{
		{"qt6_6810", "minor 81, where Qt 6 spells up to 12: 6.81.0 and 6.8.10 cannot be told apart"},
		{"qt6_6130", "minor 13, one past what Qt 6 spells"},
		{"qt5_5160", "minor 16, one past what Qt 5 spells"},
		{"qt7_700", "a major the corpus does not cover"},
	}
	for _, tt := range refused {
		if got, err := ParseVersionDirectory(tt.name); err == nil {
			t.Errorf("ParseVersionDirectory(%q) = %+v, want a failure (%s)", tt.name, got, tt.why)
		}
	}
}

func TestEncodeVersionDirectorySpellsTheName(t *testing.T) {
	tests := []struct {
		version model.Version
		ext     string
		want    string
	}{
		{model.Version{Major: 6, Minor: 11}, "", "qt6_6110"},
		{model.Version{Major: 6, Minor: 8}, "", "qt6_680"},
		{model.Version{Major: 6, Minor: 11, Patch: 3}, "", "qt6_6113"},
		{model.Version{Major: 5, Minor: 15, Patch: 2}, "", "qt5_5152"},

		// Major 5 with a zero patch and a one-digit minor drops the patch.
		{model.Version{Major: 5, Minor: 9}, "", "qt5_59"},
		{model.Version{Major: 5, Minor: 6}, "src_doc_examples", "qt5_56_src_doc_examples"},

		// Two-digit minors are written as they are.
		{model.Version{Major: 5, Minor: 10}, "", "qt5_5100"},
		{model.Version{Major: 5, Minor: 15}, "", "qt5_5150"},

		// A preview carries only major and minor; the marker is in the extension.
		{model.Version{Major: 5, Minor: 15, Suffix: "-preview"}, "preview", "qt5_515_preview"},
		{model.Version{Major: 6, Minor: 2, Suffix: "-preview"}, "wasm_preview", "qt6_62_wasm_preview"},

		// The extension is taken whole.
		{model.Version{Major: 6, Minor: 11}, "msvc2022_64", "qt6_6110_msvc2022_64"},
		{model.Version{Major: 6, Minor: 11}, "msvc2022_arm64_cross_compiled", "qt6_6110_msvc2022_arm64_cross_compiled"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := EncodeVersionDirectory(tt.version, tt.ext)
			if err != nil {
				t.Fatalf("EncodeVersionDirectory(%v, %q) = %v", tt.version, tt.ext, err)
			}
			if got != tt.want {
				t.Errorf("name = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodeVersionDirectoryRejectsWhatTheSpellingCannotCarry(t *testing.T) {
	tests := []struct {
		why     string
		version model.Version
		ext     string
	}{
		{"a two-digit major", model.Version{Major: 10}, ""},
		{"the zero version", model.Version{}, ""},
		{"a negative component", model.Version{Major: 6, Minor: -1}, ""},
		{"a one-digit minor with a two-digit patch", model.Version{Major: 6, Minor: 8, Patch: 12}, ""},
		{"a preview whose extension does not carry the marker", model.Version{Major: 5, Minor: 15, Suffix: "-preview"}, ""},
		// The produced name parses as a preview and so cannot read back as the
		// release that was given.
		{"a release whose extension claims a preview", model.Version{Major: 6, Minor: 11}, "preview"},
		// A directory names no build stamp, so a version read from Updates.xml is
		// not something this function takes.
		{"a build stamp", model.Version{Major: 5, Minor: 15, Patch: 2, Suffix: "-0-202011130601"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.why, func(t *testing.T) {
			if got, err := EncodeVersionDirectory(tt.version, tt.ext); err == nil {
				t.Fatalf("EncodeVersionDirectory(%v, %q) = %q, want a failure", tt.version, tt.ext, got)
			}
		})
	}
}

// TestEncodeVersionDirectoryRoundTripsTheCorpus runs the ADR-004 acceptance
// criterion: every version directory name the repositories actually use must
// re-encode to itself.
func TestEncodeVersionDirectoryRoundTripsTheCorpus(t *testing.T) {
	body, err := os.ReadFile(fixturePath("repository", "version-directory-names.txt"))
	if err != nil {
		t.Fatalf("reading the name list: %v", err)
	}

	checked := 0
	for _, line := range strings.Split(string(body), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}

		vd, err := ParseVersionDirectory(name)
		if err != nil {
			continue // the dev-channel entries are not version directories
		}
		encoded, err := EncodeVersionDirectory(vd.Version, vd.Extension)
		if err != nil {
			t.Errorf("%s: EncodeVersionDirectory(%v, %q) = %v", name, vd.Version, vd.Extension, err)
			continue
		}
		if encoded != name {
			t.Errorf("%s re-encodes as %q", name, encoded)
		}
		checked++
	}

	// 262 of the 269 names in the list are version directories.
	if checked != 262 {
		t.Errorf("round-tripped %d names, want 262", checked)
	}
}

// FuzzVersionDirectoryRoundTrip asserts the two properties the spelling provides,
// both on versions rather than names: a name that decodes re-encodes to a name
// that decodes to the same version (the names can differ — "qt5_590" decodes to
// 5.9.0 but the only correct encoding is "qt5_59"), and encoding is idempotent
// through decode.
func FuzzVersionDirectoryRoundTrip(f *testing.F) {
	for _, name := range []string{
		"qt6_6110_msvc2022_64", "qt5_5152", "qt5_59_src_doc_examples",
		"qt6_62_wasm_preview", "qt6_6110", "qt5_590", "not a directory",
	} {
		f.Add(name)
	}

	f.Fuzz(func(t *testing.T, name string) {
		vd, err := ParseVersionDirectory(name)
		if err != nil {
			return // names the decoder rejects are not this contract's input
		}

		encoded, err := EncodeVersionDirectory(vd.Version, vd.Extension)
		if err != nil {
			return // a decoded shape the spelling cannot write back is allowed
		}
		again, err := ParseVersionDirectory(encoded)
		if err != nil {
			t.Fatalf("encode(decode(%q)) = %q, which does not parse: %v", name, encoded, err)
		}
		if again.Version.Major != vd.Version.Major || again.Version.Minor != vd.Version.Minor ||
			again.Version.Patch != vd.Version.Patch || again.Version.Suffix != vd.Version.Suffix ||
			again.Extension != vd.Extension {
			t.Fatalf("round trip %q -> %q changed the version: %+v/%q vs %+v/%q",
				name, encoded, again.Version, again.Extension, vd.Version, vd.Extension)
		}

		second, err := EncodeVersionDirectory(again.Version, again.Extension)
		if err != nil || second != encoded {
			t.Fatalf("encoding is not idempotent: %q encoded twice as %q (%v)", name, second, err)
		}
	})
}
