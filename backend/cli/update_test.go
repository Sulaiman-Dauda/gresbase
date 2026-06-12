package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// buildZip returns a zip archive containing a single file binName with the
// given content.
func buildZip(t *testing.T, binName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(binName)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

type fakeRelease struct {
	server        *httptest.Server
	downloadCount atomic.Int64
}

// newFakeReleaseServer serves a GitHub-style latest-release endpoint for
// version (tag "v"+version) with the platform zip and a checksums.txt asset.
// checksumOverride, if non-empty, replaces the real sha256 in checksums.txt.
func newFakeReleaseServer(t *testing.T, repo, version string, zipBytes []byte, checksumOverride string) *fakeRelease {
	t.Helper()

	fr := &fakeRelease{}
	assetName := fmt.Sprintf("gresbase_%s_%s_%s.zip", version, runtime.GOOS, runtime.GOARCH)

	sum := checksumOverride
	if sum == "" {
		h := sha256.Sum256(zipBytes)
		sum = hex.EncodeToString(h[:])
	}
	checksums := fmt.Sprintf("%s  %s\n", sum, assetName)

	mux := http.NewServeMux()
	fr.server = httptest.NewServer(mux)
	t.Cleanup(fr.server.Close)

	mux.HandleFunc("/repos/"+repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		rel := map[string]any{
			"tag_name": "v" + version,
			"assets": []map[string]any{
				{"name": assetName, "browser_download_url": fr.server.URL + "/dl/" + assetName},
				{"name": "checksums.txt", "browser_download_url": fr.server.URL + "/dl/checksums.txt"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rel)
	})
	mux.HandleFunc("/dl/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		fr.downloadCount.Add(1)
		w.Write(zipBytes)
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fr.downloadCount.Add(1)
		w.Write([]byte(checksums))
	})

	return fr
}

// newTestUpdater wires an updater at the fake server with a fake "current
// binary" in a temp dir. Returns the updater, the binary path and the output
// buffer.
func newTestUpdater(t *testing.T, fr *fakeRelease, repo, currentVersion string, oldContent []byte) (*updater, string, *bytes.Buffer) {
	t.Helper()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "gresbase")
	if err := os.WriteFile(exePath, oldContent, 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	out := &bytes.Buffer{}
	u := &updater{
		apiBaseURL:     fr.server.URL,
		repo:           repo,
		currentVersion: currentVersion,
		exePath:        exePath,
		httpClient:     fr.server.Client(),
		out:            out,
		goos:           runtime.GOOS,
		goarch:         runtime.GOARCH,
	}
	return u, exePath, out
}

func TestUpdateHappyPath(t *testing.T) {
	const repo = "gresbase/gresbase"
	oldContent := []byte("old-binary-v0.3.0")
	newContent := []byte("new-binary-v9.9.9")

	binName := "gresbase"
	if runtime.GOOS == "windows" {
		binName = "gresbase.exe"
	}
	zipBytes := buildZip(t, binName, newContent)
	fr := newFakeReleaseServer(t, repo, "9.9.9", zipBytes, "")
	u, exePath, out := newTestUpdater(t, fr, repo, "0.3.0", oldContent)

	if err := u.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read updated binary: %v", err)
	}
	if !bytes.Equal(got, newContent) {
		t.Fatalf("binary not replaced: got %q, want %q", got, newContent)
	}

	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("binary mode = %v, want 0755", info.Mode().Perm())
	}

	bak, err := os.ReadFile(exePath + ".bak")
	if err != nil {
		t.Fatalf("expected .bak backup: %v", err)
	}
	if !bytes.Equal(bak, oldContent) {
		t.Fatalf(".bak content = %q, want old binary %q", bak, oldContent)
	}

	if !strings.Contains(out.String(), "updated 0.3.0 -> 9.9.9") {
		t.Fatalf("output missing update message, got:\n%s", out.String())
	}
}

func TestUpdateChecksumMismatch(t *testing.T) {
	const repo = "gresbase/gresbase"
	oldContent := []byte("old-binary")

	zipBytes := buildZip(t, "gresbase", []byte("evil-binary"))
	badSum := strings.Repeat("ab", 32) // wrong sha256
	fr := newFakeReleaseServer(t, repo, "9.9.9", zipBytes, badSum)
	u, exePath, _ := newTestUpdater(t, fr, repo, "0.3.0", oldContent)

	err := u.run(context.Background())
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}

	got, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatalf("original binary missing: %v", readErr)
	}
	if !bytes.Equal(got, oldContent) {
		t.Fatalf("original binary modified: got %q, want %q", got, oldContent)
	}
	if _, statErr := os.Stat(exePath + ".bak"); !os.IsNotExist(statErr) {
		t.Fatalf("no .bak should exist on failure, stat err = %v", statErr)
	}

	// No leftover temp files in the binary's directory.
	entries, _ := os.ReadDir(filepath.Dir(exePath))
	for _, e := range entries {
		if e.Name() != "gresbase" {
			t.Fatalf("leftover file after failed update: %s", e.Name())
		}
	}
}

func TestUpdateCheckOnly(t *testing.T) {
	const repo = "gresbase/gresbase"
	oldContent := []byte("old-binary")

	zipBytes := buildZip(t, "gresbase", []byte("new-binary"))
	fr := newFakeReleaseServer(t, repo, "9.9.9", zipBytes, "")
	u, exePath, out := newTestUpdater(t, fr, repo, "0.3.0", oldContent)
	u.checkOnly = true

	if err := u.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if n := fr.downloadCount.Load(); n != 0 {
		t.Fatalf("--check downloaded %d assets, want 0", n)
	}
	if !strings.Contains(out.String(), "current 0.3.0") || !strings.Contains(out.String(), "latest 9.9.9") {
		t.Fatalf("check output missing versions, got:\n%s", out.String())
	}

	got, _ := os.ReadFile(exePath)
	if !bytes.Equal(got, oldContent) {
		t.Fatal("--check must not modify the binary")
	}
}

func TestUpdateAlreadyUpToDate(t *testing.T) {
	const repo = "gresbase/gresbase"
	oldContent := []byte("current-binary")

	zipBytes := buildZip(t, "gresbase", []byte("same-binary"))
	fr := newFakeReleaseServer(t, repo, "0.3.0", zipBytes, "")
	u, exePath, out := newTestUpdater(t, fr, repo, "0.3.0", oldContent)

	if err := u.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.Contains(out.String(), "Already up to date") {
		t.Fatalf("output missing up-to-date message, got:\n%s", out.String())
	}
	if n := fr.downloadCount.Load(); n != 0 {
		t.Fatalf("up-to-date path downloaded %d assets, want 0", n)
	}
	got, _ := os.ReadFile(exePath)
	if !bytes.Equal(got, oldContent) {
		t.Fatal("up-to-date path must not modify the binary")
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.3.0", 0},
		{"0.4.0", "0.3.0", 1},
		{"0.3.0", "0.4.0", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.3.1", "0.3.0", 1},
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"10.0.0", "9.0.0", 1},
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
