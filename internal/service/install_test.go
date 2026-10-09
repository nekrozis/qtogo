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

// treeArchive is the archived tree the fake repository serves.
func treeArchive(t *testing.T) []byte {
	t.Helper()
	blob, err := os.ReadFile(filepath.Join("..", "..", "testdata", "archive", "tree.7z"))
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// installRepo wires a fake repository that serves one tree archive for a modern
// desktop version, and a transport client that reads it.
//
// The archive is the existing testdata/tree.7z, whose entries land under the
// version and architecture the metadata names, so an install of it has something
// real to extract and relocate.
func installRepo(t *testing.T) (*transport.Client, string) {
	t.Helper()

	blob := treeArchive(t)
	const leaf = desktop + "/qt6_680"
	const archivePath = leaf + "/qt.qt6.680.win64_msvc2022_64/6.8.0-0-202410030750tree.7z"

	mux := http.NewServeMux()
	mux.HandleFunc("/"+desktop+"/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Updates.xml") {
			io.WriteString(w, updatesFor("qt.qt6.680.win64_msvc2022_64", "6.8.0-0-202410030750", "tree.7z"))
			return
		}
		io.WriteString(w, targetIndexAt(desktop, "qt6_680"))
	})
	mux.HandleFunc("/"+leaf+"/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Updates.xml") {
			io.WriteString(w, updatesFor("qt.qt6.680.win64_msvc2022_64", "6.8.0-0-202410030750", "tree.7z"))
			return
		}
		io.WriteString(w, leafIndex(leaf, "Updates.xml"))
	})
	mux.HandleFunc("/"+archivePath, func(w http.ResponseWriter, _ *http.Request) { w.Write(blob) })
	mux.HandleFunc("/"+archivePath+".sha256", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, sha256hex(string(blob))+"  tree.7z\n")
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

// installRequest is the request most of these tests make: a modern desktop
// version, which relocation covers.
func installRequest(t *testing.T) (model.Host, model.Kind, model.Version) {
	t.Helper()
	v, err := model.ParseVersion("6.8.0")
	if err != nil {
		t.Fatal(err)
	}
	return model.HostWindows, model.KindDesktop, v
}

func TestInstallQtWritesATreeAndAManifest(t *testing.T) {
	client, _ := installRepo(t)
	out := t.TempDir()
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	got, err := s.InstallQt(context.Background(), host, kind, version, "win64_msvc2022_64", nil,
		InstallOptions{OutputDir: out})
	if err != nil {
		t.Fatal(err)
	}

	// The tree landed under <version>/<arch> and the files came out of the archive.
	root := filepath.Join(out, "6.8.0", "win64_msvc2022_64")
	if got.Path != root {
		t.Errorf("path = %q, want %q", got.Path, root)
	}
	if got.Policy != "modern-desktop" || !got.Relocatable {
		t.Errorf("policy = %q, relocatable = %t", got.Policy, got.Relocatable)
	}
	// The archive's contents landed where its Extract operation said — under
	// <tree>/6.8.0/msvc2022_64/ — and relocation wrote bin/qt.conf beside them.
	for _, rel := range []string{"6.8.0/msvc2022_64/dir/file.txt", "6.8.0/msvc2022_64/empty.txt"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("the archive's %s is not in the tree: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "qt.conf")); err != nil {
		t.Errorf("relocation did not write bin/qt.conf: %v", err)
	}

	// The manifest is there and names the archive and its digest.
	body, err := os.ReadFile(filepath.Join(root, "qtogo-manifest.json"))
	if err != nil {
		t.Fatalf("no manifest: %v", err)
	}
	for _, want := range []string{`"program"`, `"modern-desktop"`, `"tree.7z"`, `"digest"`} {
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

	got, err := s.InstallQt(context.Background(), host, kind, version, "win64_msvc2022_64", nil,
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

	got, err := s.InstallQt(context.Background(), host, kind, version, "win64_msvc2022_64", nil,
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
	root := filepath.Join(out, "6.8.0", "win64_msvc2022_64")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	_, err := s.InstallQt(context.Background(), host, kind, version, "win64_msvc2022_64", nil,
		InstallOptions{OutputDir: out})
	assertFailure(t, err, exitcode.Filesystem, errs.CodeFilesystem)

	// No staging directory was left beside the destination.
	entries, err := os.ReadDir(filepath.Join(out, "6.8.0"))
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
	root := filepath.Join(out, "6.8.0", "win64_msvc2022_64")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(client).WithDownloader(client)
	host, kind, version := installRequest(t)

	got, err := s.InstallQt(context.Background(), host, kind, version, "win64_msvc2022_64", nil,
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
	blob, err := os.ReadFile(filepath.Join("..", "..", "testdata", "archive", "tree.7z"))
	if err != nil {
		t.Fatal(err)
	}
	const leaf = desktop + "/qt5_590"
	mux := http.NewServeMux()
	mux.HandleFunc("/"+desktop+"/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Updates.xml") {
			io.WriteString(w, updatesFor("qt.qt5.590.win64_msvc2019_64", "5.9.0-0-20180101", "tree.7z"))
			return
		}
		io.WriteString(w, targetIndexAt(desktop, "qt5_590"))
	})
	mux.HandleFunc("/"+leaf+"/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Updates.xml") {
			io.WriteString(w, updatesFor("qt.qt5.590.win64_msvc2019_64", "5.9.0-0-20180101", "tree.7z"))
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
	_, err := s.InstallQt(ctx, host, kind, version, "win64_msvc2022_64", nil, InstallOptions{OutputDir: out})
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
