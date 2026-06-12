package cli

// Self-update command (`gresbase update`), PocketBase-style.
//
// Release asset naming convention (release CI must follow this):
//
//	gresbase_{version}_{GOOS}_{GOARCH}.zip
//
// where {version} is the release tag without the leading "v"
// (e.g. gresbase_0.4.0_linux_amd64.zip). The zip contains the single
// binary "gresbase" ("gresbase.exe" on windows). Every release must also
// publish a "checksums.txt" asset with one line per asset:
//
//	<sha256-hex>  gresbase_0.4.0_linux_amd64.zip
//
// The updater downloads the platform zip, verifies its SHA-256 against
// checksums.txt (hard fail on mismatch or missing entry), then atomically
// replaces the running executable: the current binary is renamed aside to
// "{name}.bak" before the new one is moved into place, which also works on
// Windows where an in-use file cannot be overwritten but can be renamed.

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gresbase/gresbase"
	"github.com/spf13/cobra"
)

const (
	defaultUpdateRepo   = "gresbase/gresbase"
	defaultGitHubAPIURL = "https://api.github.com"
	updateRepoEnvVar    = "GRESBASE_UPDATE_REPO"

	// maxChecksumsSize caps how much of checksums.txt we read.
	maxChecksumsSize = 1 << 20 // 1 MiB
)

func updateCommand() *cobra.Command {
	var (
		repo      string
		checkOnly bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the gresbase binary to the latest GitHub release",
		RunE: func(cmd *cobra.Command, args []string) error {
			u := newUpdater()
			u.out = cmd.OutOrStdout()
			u.checkOnly = checkOnly
			if repo != "" {
				u.repo = repo
			}
			return u.run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository to update from (owner/name, default "+defaultUpdateRepo+", or env "+updateRepoEnvVar+")")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check whether an update is available, never download")
	return cmd
}

// updater performs the self-update. All external touch points (API base URL,
// HTTP client, executable path, platform) are injectable for tests.
type updater struct {
	apiBaseURL     string       // GitHub API base, e.g. https://api.github.com
	repo           string       // owner/name
	currentVersion string       // gresbase.Version
	exePath        string       // override for os.Executable (tests)
	httpClient     *http.Client // client used for API + downloads
	out            io.Writer    // progress output
	goos, goarch   string       // platform for the asset name
	checkOnly      bool         // --check
}

func newUpdater() *updater {
	repo := defaultUpdateRepo
	if env := os.Getenv(updateRepoEnvVar); env != "" {
		repo = env
	}
	return &updater{
		apiBaseURL:     defaultGitHubAPIURL,
		repo:           repo,
		currentVersion: gresbase.Version,
		httpClient:     &http.Client{Timeout: 5 * time.Minute},
		out:            os.Stdout,
		goos:           runtime.GOOS,
		goarch:         runtime.GOARCH,
	}
}

type releaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

type release struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

func (u *updater) run(ctx context.Context) error {
	fmt.Fprintf(u.out, "Checking for updates (repo %s)...\n", u.repo)

	rel, err := u.fetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch latest release: %w", err)
	}

	current := strings.TrimPrefix(u.currentVersion, "v")
	latest := strings.TrimPrefix(rel.TagName, "v")

	if compareSemver(latest, current) <= 0 {
		fmt.Fprintf(u.out, "Already up to date (current %s, latest %s).\n", current, latest)
		return nil
	}

	if u.checkOnly {
		fmt.Fprintf(u.out, "Update available: current %s, latest %s.\nRun \"gresbase update\" to install it.\n", current, latest)
		return nil
	}

	assetName := fmt.Sprintf("gresbase_%s_%s_%s.zip", latest, u.goos, u.goarch)
	asset := findAsset(rel.Assets, assetName)
	if asset == nil {
		return fmt.Errorf("release %s has no asset %q for this platform", rel.TagName, assetName)
	}
	checksums := findAsset(rel.Assets, "checksums.txt")
	if checksums == nil {
		return fmt.Errorf("release %s has no checksums.txt asset; refusing to update without checksum verification", rel.TagName)
	}

	exePath, err := u.resolveExecutable()
	if err != nil {
		return err
	}
	exeDir := filepath.Dir(exePath)

	// Download the zip into the binary's directory so the final rename is
	// guaranteed to stay on the same filesystem (atomic).
	fmt.Fprintf(u.out, "Downloading %s...\n", assetName)
	zipPath, zipSum, err := u.downloadToTemp(ctx, asset.DownloadURL, exeDir)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("no write permission for %s; re-run with elevated privileges (e.g. \"sudo gresbase update\"): %w", exeDir, err)
		}
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(zipPath)

	fmt.Fprintln(u.out, "Verifying checksum...")
	expected, err := u.fetchChecksum(ctx, checksums.DownloadURL, assetName)
	if err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}
	if !strings.EqualFold(expected, zipSum) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s — aborting, binary not replaced", assetName, expected, zipSum)
	}

	fmt.Fprintln(u.out, "Extracting...")
	binName := "gresbase"
	if u.goos == "windows" {
		binName = "gresbase.exe"
	}
	tmpBin, err := extractBinary(zipPath, binName, exeDir)
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	fmt.Fprintln(u.out, "Replacing current executable...")
	if err := replaceExecutable(exePath, tmpBin); err != nil {
		os.Remove(tmpBin)
		if os.IsPermission(err) {
			return fmt.Errorf("no write permission for %s; re-run with elevated privileges (e.g. \"sudo gresbase update\"): %w", exeDir, err)
		}
		return err
	}

	fmt.Fprintf(u.out, "Successfully updated %s -> %s, restart the server to apply.\n", current, latest)
	fmt.Fprintf(u.out, "The previous binary was kept as %s.bak\n", exePath)
	return nil
}

func (u *updater) fetchLatestRelease(ctx context.Context) (*release, error) {
	url := strings.TrimSuffix(u.apiBaseURL, "/") + "/repos/" + u.repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	u.setUserAgent(req)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository %s has no published releases (HTTP 404)", u.repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d from %s", resp.StatusCode, url)
	}

	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("invalid release JSON: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("release JSON from %s has an empty tag_name", url)
	}
	return &rel, nil
}

// downloadToTemp streams url into a temp file in dir and returns the file
// path and its SHA-256 (hex).
func (u *updater) downloadToTemp(ctx context.Context, url, dir string) (path, sum string, err error) {
	tmp, err := os.CreateTemp(dir, ".gresbase-update-*.zip")
	if err != nil {
		return "", "", err
	}
	defer func() {
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	u.setUserAgent(req)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unexpected HTTP status %d from %s", resp.StatusCode, url)
	}

	hasher := sha256.New()
	if _, err = io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		return "", "", err
	}
	if err = tmp.Close(); err != nil {
		return "", "", err
	}
	return tmp.Name(), hex.EncodeToString(hasher.Sum(nil)), nil
}

// fetchChecksum downloads checksums.txt and returns the sha256 hex recorded
// for assetName. Lines have the sha256sum format: "<hex>  <filename>".
func (u *updater) fetchChecksum(ctx context.Context, url, assetName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	u.setUserAgent(req)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status %d downloading checksums.txt", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumsSize))
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// sha256sum may prefix binary-mode filenames with "*".
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", assetName)
}

func (u *updater) setUserAgent(req *http.Request) {
	req.Header.Set("User-Agent", "gresbase-updater/"+strings.TrimPrefix(u.currentVersion, "v"))
}

// resolveExecutable returns the path of the running binary with symlinks
// resolved (or the injected test override).
func (u *updater) resolveExecutable() (string, error) {
	path := u.exePath
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("cannot determine the current executable: %w", err)
		}
		path = exe
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("cannot resolve executable path %s: %w", path, err)
	}
	return resolved, nil
}

func findAsset(assets []releaseAsset, name string) *releaseAsset {
	for i := range assets {
		if assets[i].Name == name {
			return &assets[i]
		}
	}
	return nil
}

// extractBinary extracts binName from the zip at zipPath into a temp file in
// destDir (mode 0755) and returns the temp file path.
func extractBinary(zipPath, binName, destDir string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("invalid zip archive: %w", err)
	}
	defer zr.Close()

	var entry *zip.File
	for _, f := range zr.File {
		if f.Name == binName || filepath.Base(f.Name) == binName {
			entry = f
			break
		}
	}
	if entry == nil {
		return "", fmt.Errorf("zip archive does not contain %q", binName)
	}

	src, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	tmp, err := os.CreateTemp(destDir, ".gresbase-new-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// replaceExecutable atomically swaps exePath with newPath: the current binary
// is renamed aside to "{exePath}.bak" first (required on Windows, where an
// in-use file cannot be overwritten but can be renamed), then the new binary
// is renamed into place. On failure the .bak is rolled back.
func replaceExecutable(exePath, newPath string) error {
	bakPath := exePath + ".bak"

	// Drop any stale .bak from a previous update.
	if err := os.Remove(bakPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove stale backup %s: %w", bakPath, err)
	}

	if err := os.Rename(exePath, bakPath); err != nil {
		return fmt.Errorf("cannot move the current binary aside: %w", err)
	}

	if err := os.Rename(newPath, exePath); err != nil {
		// Roll the backup back so the original binary keeps working.
		if rbErr := os.Rename(bakPath, exePath); rbErr != nil {
			return fmt.Errorf("failed to install the new binary (%v) AND failed to roll back the backup (%v); your binary is at %s", err, rbErr, bakPath)
		}
		return fmt.Errorf("failed to install the new binary (rolled back): %w", err)
	}
	return nil
}

// compareSemver compares two semver strings (without leading "v") and
// returns -1, 0 or 1. Numeric major.minor.patch are compared first; if the
// cores are equal, a version without a pre-release suffix is newer than one
// with it (e.g. 1.0.0 > 1.0.0-rc.1).
func compareSemver(a, b string) int {
	aCore, aPre := splitPre(a)
	bCore, bPre := splitPre(b)

	an := parseVersionCore(aCore)
	bn := parseVersionCore(bCore)
	for i := 0; i < 3; i++ {
		if an[i] != bn[i] {
			if an[i] > bn[i] {
				return 1
			}
			return -1
		}
	}

	switch {
	case aPre == bPre:
		return 0
	case aPre == "": // a is the release, b is a pre-release
		return 1
	case bPre == "":
		return -1
	case aPre > bPre:
		return 1
	default:
		return -1
	}
}

func splitPre(v string) (core, pre string) {
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}

func parseVersionCore(core string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(core, ".", 3) {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			break
		}
		out[i] = n
	}
	return out
}
