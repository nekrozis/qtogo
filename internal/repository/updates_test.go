package repository

import (
	"reflect"
	"testing"

	"github.com/nekrozis/qtogo/internal/model"
)

const updatesSource = "https://example.invalid/online/qtsdkrepository/windows_x86/desktop/qt6_680/qt6_680_msvc2022_64/Updates.xml"

func TestParseUpdatesXMLReadsTheDocument(t *testing.T) {
	packages, meta, err := ParseUpdatesXML(updatesSource, readFixture(t, "repository", "updates-basic", "Updates.xml"))
	if err != nil {
		t.Fatalf("ParseUpdatesXML = %v", err)
	}

	wantMeta := Metadata{
		ApplicationName:    "{AnyApplication}",
		ApplicationVersion: "1.0.0",
		Checksum:           true,
		MetadataName:       "2024-10-03-0758_meta.7z",
		SHA1:               "2222222222222222222222222222222222222222",
	}
	if meta != wantMeta {
		t.Errorf("metadata = %+v, want %+v", meta, wantMeta)
	}

	// Independent of ParseVersion: the numbers and the suffix are asserted as
	// literals so this checks the parse rather than the parser.
	build := model.Version{Raw: "6.8.0-0-202410030750", Major: 6, Minor: 8, Patch: 0, Suffix: "-0-202410030750"}

	want := []PackageUpdate{
		{
			Name:            "qt.qt6.680.win64_msvc2022_64",
			DisplayName:     "Qt 6.8.0 MSVC 2022 64-bit",
			Description:     "Qt 6.8.0 prebuilt components.<br><br>Hand-written fixture, no real URL.",
			Version:         build,
			ReleaseDate:     "2024-10-03",
			Default:         true,
			SortingPriority: 700,
			Dependencies:    []string{"qt.qt6.680.patcher", "qt.qt6.680.addons"},
			AutoDependOn:    []string{"qt.qt6.680.patcher"},
			Script:          "installscript.qs",
			Downloadables:   []string{"qtbase.7z", "qtsvg.7z", "qtdeclarative.7z"},
			UpdateFile:      UpdateFile{CompressedSize: 27666190, UncompressedSize: 208203936, OS: "Any"},
			SHA1:            "a5f32a66b1f20a74a3ee0838c6e27acc2c13f475",
			Operations: []Operation{
				{Name: "Extract", Arguments: []string{"@TargetDir@/6.8.0/msvc2022_64", "qtbase.7z"}},
				{Name: "Extract", Arguments: []string{"@TargetDir@/6.8.0/msvc2022_64", "qtsvg.7z"}},
			},
		},
		{
			Name:               "qt.qt6.680.addons.qtcharts",
			DisplayName:        "Qt Charts",
			Version:            build,
			ReleaseDate:        "2024-10-03",
			Virtual:            true,
			ForcedInstallation: true,
			Dependencies:       []string{"qt.qt6.680.doc.qtcharts", "qt.qt6.680.examples.qtcharts"},
			UserInterfaces:     []string{"readmecheckboxform.ui", "launchqtcreatorcheckboxform.ui"},
			Translations:       []string{"fr.qm", "de.qm"},
			Replaces:           []string{"qt.qt6.680.addons.qtcharts.legacy"},
			Licenses: []License{
				{File: "LICENSE.LGPL3", Name: "LGPL v3"},
				{File: "LICENSE.GPL3", Name: "GPL v3"},
			},
			UpdateFile: UpdateFile{OS: "Any"},
			SHA1:       "1111111111111111111111111111111111111111",
		},
	}

	if !reflect.DeepEqual(packages, want) {
		t.Errorf("packages =\n%+v\nwant\n%+v", packages, want)
	}
}

func TestParseUpdatesXMLKeepsExtractArgumentsUninterpreted(t *testing.T) {
	packages, _, err := ParseUpdatesXML(updatesSource, readFixture(t, "repository", "updates-basic", "Updates.xml"))
	if err != nil {
		t.Fatalf("ParseUpdatesXML = %v", err)
	}

	// @TargetDir@ is an install-time placeholder; the parse layer must not expand
	// it or guess the directory it stands for.
	if got := packages[0].Operations[0].Arguments[0]; got != "@TargetDir@/6.8.0/msvc2022_64" {
		t.Errorf("first extract argument = %q, want the unexpanded template", got)
	}
}

func TestParseUpdatesXMLReadsAMinimalDocument(t *testing.T) {
	packages, meta, err := ParseUpdatesXML("", readFixture(t, "repository", "updates-minimal", "Updates.xml"))
	if err != nil {
		t.Fatalf("ParseUpdatesXML = %v", err)
	}
	if meta.MetadataName != "" || meta.SHA1 != "" {
		t.Errorf("metadata = %+v, want no MetadataName or SHA1", meta)
	}
	if len(packages) != 1 {
		t.Fatalf("got %d packages, want 1", len(packages))
	}

	p := packages[0]
	if p.Name != "qt.qt5.5152.win64_msvc2019_64" {
		t.Errorf("name = %q", p.Name)
	}
	if got, want := p.Version, (model.Version{Raw: "5.15.2-0-202011130601", Major: 5, Minor: 15, Patch: 2, Suffix: "-0-202011130601"}); got != want {
		t.Errorf("version = %+v, want %+v", got, want)
	}
	for name, list := range map[string][]string{
		"Dependencies":   p.Dependencies,
		"AutoDependOn":   p.AutoDependOn,
		"Downloadables":  p.Downloadables,
		"UserInterfaces": p.UserInterfaces,
		"Translations":   p.Translations,
		"Replaces":       p.Replaces,
	} {
		if list != nil {
			t.Errorf("%s = %v, want nil when the element is absent or empty", name, list)
		}
	}
	if len(p.Licenses) != 0 {
		t.Errorf("licenses = %v, want none", p.Licenses)
	}
	if len(p.Operations) != 0 {
		t.Errorf("operations = %v, want none", p.Operations)
	}
}

func TestParseUpdatesXMLIgnoresUnknownElements(t *testing.T) {
	body := []byte(`<Updates>
	 <SomethingNew>ignored</SomethingNew>
	 <PackageUpdate><Name>a.b</Name><Version>1.0.0</Version><AlsoNew/></PackageUpdate>
	</Updates>`)

	packages, _, err := ParseUpdatesXML("", body)
	if err != nil {
		t.Fatalf("ParseUpdatesXML = %v", err)
	}
	if len(packages) != 1 || packages[0].Name != "a.b" {
		t.Errorf("packages = %+v, want one package named a.b", packages)
	}
}

func TestParseUpdatesXMLRejectsDocumentsThatAreNotUpdates(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"malformed", readFixture(t, "repository", "updates-malformed", "Updates.xml")},
		{"empty", nil},
		{"a listing page", []byte("<html><head><title>Index of /x</title></head></html>")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages, _, err := ParseUpdatesXML(updatesSource, tt.body)
			if err == nil {
				t.Fatalf("ParseUpdatesXML = %+v, want a failure", packages)
			}
		})
	}
}

func TestParseUpdatesXMLRejectsAnUnparsableVersion(t *testing.T) {
	body := []byte(`<Updates><PackageUpdate><Name>a.b</Name><Version>not-a-version</Version></PackageUpdate></Updates>`)
	if _, _, err := ParseUpdatesXML("", body); err == nil {
		t.Error("ParseUpdatesXML = nil error, want a failure for a bad version")
	}
}
