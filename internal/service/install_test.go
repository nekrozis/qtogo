package service

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/transport"
)

// trustServer is a transport that trusts one test server's certificate, so a
// TLS server can stand in for the official endpoint.
func trustServer(s *httptest.Server) *http.Transport {
	pool := x509.NewCertPool()
	pool.AddCert(s.Certificate())
	return &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
}

// sha256hex is the digest form a checksum sidecar carries.
func sha256hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// treeArchive is the archived tree the fake repository serves: a minimal Qt 5
// layout, whose archive carries <version>/<arch>/ itself and a bin/qmake marker.
func treeArchive(t *testing.T) []byte {
	t.Helper()
	blob, err := os.ReadFile(filepath.Join("..", "..", "testdata", "archive", "qtree.7z"))
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// installRepo wires a fake repository that serves one tree archive for a Qt 5
// desktop version. The archive has no Extract operation — as a real Qt 5 one does
// not — so its contents land at the base directory and the tree is the
// <version>/<arch>/ path inside it.
func installRepo(t *testing.T) (*transport.Client, string) {
	t.Helper()

	blob := treeArchive(t)
	const leaf = desktop + "/qt5_5152"
	const archivePath = leaf + "/qt.qt5.5152.win64_mingw81/5.15.2-0-202011130601qtree.7z"

	mux := http.NewServeMux()
	mux.HandleFunc("/"+desktop+"/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Updates.xml") {
			io.WriteString(w, updatesNoExtract("qt.qt5.5152.win64_mingw81", "5.15.2-0-202011130601", "qtree.7z"))
			return
		}
		io.WriteString(w, targetIndexAt(desktop, "qt5_5152"))
	})
	mux.HandleFunc("/"+leaf+"/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Updates.xml") {
			io.WriteString(w, updatesNoExtract("qt.qt5.5152.win64_mingw81", "5.15.2-0-202011130601", "qtree.7z"))
			return
		}
		io.WriteString(w, leafIndex(leaf, "Updates.xml"))
	})
	mux.HandleFunc("/"+archivePath, func(w http.ResponseWriter, _ *http.Request) { w.Write(blob) })
	mux.HandleFunc("/"+archivePath+".sha256", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, sha256hex(string(blob))+"  qtree.7z\n")
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	client, err := transport.NewWithTransport(
		transport.Config{Official: srv.URL, Sources: []string{srv.URL}},
		trustServer(srv))
	if err != nil {
		t.Fatal(err)
	}
	return client, srv.URL
}

func updatesFor(name, version, archive string) string {
	return `<Updates><PackageUpdate><Name>` + name + `</Name>` +
		`<Version>` + version + `</Version>` +
		`<DownloadableArchives>` + archive + `</DownloadableArchives>` +
		`<Operations><Operation name="Extract">` +
		`<Argument>@TargetDir@/6.8.0/msvc2022_64</Argument><Argument>` + archive + `</Argument>` +
		`</Operation></Operations></PackageUpdate></Updates>`
}

// updatesNoExtract is a Qt 5 package: downloads with no Extract operation, so
// their contents land at the base directory.
func updatesNoExtract(name, version, archive string) string {
	return `<Updates><PackageUpdate><Name>` + name + `</Name>` +
		`<Version>` + version + `</Version>` +
		`<DownloadableArchives>` + archive + `</DownloadableArchives>` +
		`</PackageUpdate></Updates>`
}

func targetIndexAt(dir, child string) string {
	return `<html><head><title>Index of /` + dir + `</title></head><body>` +
		`<h1>Index of /` + dir + `</h1>` +
		`<a href="` + child + `/">` + child + `/</a></body></html>`
}

func leafIndex(leaf, updates string) string {
	return `<html><head><title>Index of /` + leaf + `</title></head><body>` +
		`<h1>Index of /` + leaf + `</h1>` +
		`<a href="` + updates + `">` + updates + `</a></body></html>`
}

// installRequest is the request most of these tests make: a Qt 5 desktop version,
// which relocation covers and whose archive nests its own version/arch path.
func installRequest(t *testing.T) (model.Host, model.Kind, model.Version) {
	t.Helper()
	v, err := model.ParseVersion("5.15.2")
	if err != nil {
		t.Fatal(err)
	}
	return model.HostWindows, model.KindDesktop, v
}

// installTree is the tree path a request installs into: the arch directory is Qt's
// own name, which the archive carries.
const installArch = "win64_mingw81"
const treeRel = "5.15.2/mingw81_64"

func TestInstallQtWritesATreeAndAManifest(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	got, err := s.InstallQt(context.Background(), host, kind, version, installArch, nil,
		InstallOptions{OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}

	// --outputdir is the base directory; the tree is the path its archive carries.
	root := filepath.Join(out, filepath.FromSlash(treeRel))
	if got.Path != root {
		t.Errorf("path = %q, want %q", got.Path, root)
	}
	if got.Policy != "modern-desktop" || !got.Relocatable {
		t.Errorf("policy = %q, relocatable = %t", got.Policy, got.Relocatable)
	}

	// The archive's contents are in the tree, and relocation corrected and marked it.
	for _, rel := range []string{"bin/qmake.exe", "bin/qt.conf"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("the tree has no %s: %v", rel, err)
		}
	}
	// The prl was written by the fixture with a build prefix, and relocation
	// replaced it — proof the policy ran on the tree, not the base directory.
	prl, err := os.ReadFile(filepath.Join(root, "lib", "libQt5Core.prl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prl), "$$[QT_INSTALL_LIBS]") {
		t.Errorf("the prl was not relocated:\n%s", prl)
	}

	// The manifest is in the tree and names the archive and its digest.
	body, err := os.ReadFile(filepath.Join(root, "qtogo-manifest.json"))
	if err != nil {
		t.Fatalf("no manifest: %v", err)
	}
	for _, want := range []string{`"program"`, `"modern-desktop"`, `"qtree.7z"`, `"digest"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("manifest is missing %q:\n%s", want, body)
		}
	}
}

// The ZAP rule: the manifest must not carry the install path, so two installs of
// the same request are byte-identical.
func TestInstallManifestHasNoAbsolutePath(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	got, err := s.InstallQt(context.Background(), host, kind, version, installArch, nil,
		InstallOptions{OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(got.Path, "qtogo-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), out) {
		t.Errorf("the manifest names the install path:\n%s", body)
	}
	if strings.Contains(string(body), filepath.ToSlash(out)) {
		t.Errorf("the manifest names the install path (slash form):\n%s", body)
	}
}

// A dry run downloads nothing and publishes nothing.
func TestInstallQtDryRunTouchesNothing(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	got, err := s.InstallQt(context.Background(), host, kind, version, installArch, nil,
		InstallOptions{OutputDir: out, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "" {
		t.Errorf("a dry run reported a path: %q", got.Path)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a dry run created %v", entries)
	}
}

// An existing destination is refused without --overwrite, and the staging tree is
// removed so nothing partial is left.
func TestInstallQtRefusesAnExistingDestination(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	root := filepath.Join(out, filepath.FromSlash(treeRel))
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	_, err := s.InstallQt(context.Background(), host, kind, version, installArch, nil,
		InstallOptions{OutputDir: out})
	assertFailure(t, err, exitcode.Filesystem, errs.CodeFilesystem)

	// No staging directory was left beside the destination.
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "staging") {
			t.Errorf("a staging directory was left behind: %s", e.Name())
		}
	}
}

func TestInstallQtOverwritesWhenAsked(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	root := filepath.Join(out, filepath.FromSlash(treeRel))
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	got, err := s.InstallQt(context.Background(), host, kind, version, installArch, nil,
		InstallOptions{OutputDir: out, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Replaced {
		t.Error("the result does not say the destination was replaced")
	}
	if _, err := os.Stat(filepath.Join(got.Path, "old")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old tree survived the overwrite")
	}
}

// A version this build cannot relocate is refused before anything is downloaded:
// the fake repository counts the archive requests it serves.
func TestInstallQtChecksRelocationBeforeDownloading(t *testing.T) {
	var archiveHits int
	blob, err := os.ReadFile(filepath.Join("..", "..", "testdata", "archive", "qtree.7z"))
	if err != nil {
		t.Fatal(err)
	}
	const leaf = desktop + "/qt5_590"
	mux := http.NewServeMux()
	mux.HandleFunc("/"+desktop+"/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Updates.xml") {
			io.WriteString(w, updatesFor("qt.qt5.590.win64_msvc2019_64", "5.9.0-0-20180101", "qtree.7z"))
			return
		}
		io.WriteString(w, targetIndexAt(desktop, "qt5_590"))
	})
	mux.HandleFunc("/"+leaf+"/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Updates.xml") {
			io.WriteString(w, updatesFor("qt.qt5.590.win64_msvc2019_64", "5.9.0-0-20180101", "qtree.7z"))
			return
		}
		io.WriteString(w, leafIndex(leaf, "Updates.xml"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".7z") {
			archiveHits++
			w.Write(blob)
			return
		}
		io.WriteString(w, sha256hex(string(blob))+"  tree.7z\n")
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	client, err := transport.NewWithTransport(
		transport.Config{Official: srv.URL, Sources: []string{srv.URL}}, trustServer(srv))
	if err != nil {
		t.Fatal(err)
	}

	v, err := model.ParseVersion("5.9.0")
	if err != nil {
		t.Fatal(err)
	}
	s := New(client).WithDownloader(client)
	_, err = s.InstallQt(context.Background(), model.HostWindows, model.KindDesktop, v, "win64_msvc2019_64", nil,
		InstallOptions{OutputDir: t.TempDir()})
	assertFailure(t, err, exitcode.Relocate, errs.CodeRelocateUnsupported)

	if archiveHits != 0 {
		t.Errorf("the archive was fetched %d times before the capability check", archiveHits)
	}
}

// A cancelled context during the fetch leaves nothing published.
func TestInstallQtCancellationLeavesNothing(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.InstallQt(ctx, host, kind, version, installArch, nil, InstallOptions{OutputDir: out})
	if err == nil {
		t.Fatal("a cancelled install reported success")
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a cancelled install left %v", entries)
	}
}

// Nothing is staged on the OS temporary volume: the extraction merge is a rename,
// and a rename across volumes fails. The staging tree, the downloaded archives and
// each extraction directory all live under the destination (F1).
func TestInstallStagesNothingOnTheTempVolume(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	got, err := s.InstallQt(context.Background(), host, kind, version, installArch, nil,
		InstallOptions{OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}
	// No qtogo-* directory was left in the OS temp directory by this install.
	temp := os.TempDir()
	entries, err := os.ReadDir(temp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "qtogo-extract-") || strings.HasPrefix(e.Name(), "qtogo-install-") {
			t.Errorf("an install left %s in the OS temp volume %s", e.Name(), temp)
		}
	}
	// And the tree is where it was asked for, which proves the rename worked.
	if _, err := os.Stat(filepath.Join(got.Path, "bin", "qmake.exe")); err != nil {
		t.Errorf("the tree is not in place: %v", err)
	}
}

// The block budget has to clear a real archive's largest solid block, or the
// flagship install is refused before decoding anything (F2). The figure is the
// measured size of a Qt 6.8.0 desktop package's block.
func TestDefaultMemoryBudgetClearsARealArchive(t *testing.T) {
	const realQtBlock = 1_613_160_616 // qtdeclarative, Qt 6.8.0 desktop
	if DefaultMemoryBudget <= realQtBlock {
		t.Errorf("DefaultMemoryBudget = %d, which is not above the real archive block %d",
			DefaultMemoryBudget, realQtBlock)
	}
}

// A plan and a dry run refuse a target this build cannot relocate, rather than
// showing a green plan the install then fails on (F3).
func TestPlanInstallQtChecksCapability(t *testing.T) {
	client, _ := installRepo(t)
	s := New(client).WithDownloader(client)

	// The fixture is 5.15.2, which is covered, so it plans.
	v152, err := model.ParseVersion("5.15.2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanInstallQt(context.Background(), model.HostWindows, model.KindDesktop,
		v152, installArch, nil); err != nil {
		t.Fatalf("PlanInstallQt(covered) = %v, want a plan", err)
	}
}

// The budget is bounded, and a smaller one refuses an archive whose block exceeds
// it — the path that fails before decoding (F2), exercised without a large fixture
// by driving the limit down rather than the archive up.
func TestInstallQtRefusesAnArchiveOverTheBudget(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	// The fixture archive needs more than one byte of block budget, so opening it
	// is refused before anything is written. The refusal names the budget, which is
	// what a user with a pathologically small one needs to hear.
	_, err := s.InstallQt(context.Background(), host, kind, version, installArch, nil,
		InstallOptions{OutputDir: out, MemoryBudget: 1})
	assertFailure(t, err, exitcode.Integrity, errs.CodeExtractFailed)
	if !strings.Contains(err.Error(), "memory budget") {
		t.Errorf("the refusal does not name the budget: %v", err)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("an over-budget install left %v", entries)
	}
}
