package model

import (
	"encoding/json"
	"testing"
)

// assertJSON marshals got and compares it with want, so the document shape is
// pinned rather than described.
func assertJSON(t *testing.T, got any, want string) {
	t.Helper()

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(blob) != want {
		t.Errorf("JSON =\n  %s\nwant\n  %s", blob, want)
	}
}

func TestPackageJSONContract(t *testing.T) {
	pkg := Package{
		Name:    "qt.qt6.680.win64_msvc2022_64",
		Version: mustVersion(t, "6.8.0"),
		Dependencies: []Dependency{
			{Name: "qt.qt6.680", Version: mustVersion(t, "6.8.0")},
			{Name: "qt.tools.ifw"},
		},
		Downloadables: []string{"qtbase-Windows.7z"},
	}

	const want = `{"name":"qt.qt6.680.win64_msvc2022_64","version":"6.8.0",` +
		`"dependencies":[{"name":"qt.qt6.680","version":"6.8.0"},{"name":"qt.tools.ifw","version":""}],` +
		`"downloadables":["qtbase-Windows.7z"]}`

	assertJSON(t, pkg, want)
}

func TestArchiveJSONContract(t *testing.T) {
	archive := Archive{
		Name:        "qtbase-Windows.7z",
		URL:         "https://example.invalid/qtbase-Windows.7z",
		SHA256:      "abc123",
		Size:        42,
		InstallPath: "5.15.2/gcc_64",
	}

	const want = `{"name":"qtbase-Windows.7z","url":"https://example.invalid/qtbase-Windows.7z",` +
		`"sha256":"abc123","size":42,"installPath":"5.15.2/gcc_64"}`

	assertJSON(t, archive, want)
}

func TestArchiveJSONOmitsWhatIsUnknown(t *testing.T) {
	assertJSON(t, Archive{Name: "x.7z", URL: "https://example.invalid/x.7z"}, `{"name":"x.7z","url":"https://example.invalid/x.7z"}`)
}
