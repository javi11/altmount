// Package updater provides binary self-update capabilities for standalone
// (non-Docker) installs of altmount. It fetches release assets from GitHub,
// verifies their SHA-512 checksum, extracts the binary, and applies the
// update in-place using github.com/minio/selfupdate.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const (
	defaultGitHubAPIBase = "https://api.github.com"
	repoOwner            = "javi11"
	repoName             = "altmount"

	// Channel identifiers.
	ChannelLatest = "latest"
	ChannelDev    = "dev"

	downloadTimeout = 10 * time.Minute
)

// githubAsset mirrors the subset of the GitHub release asset schema used here.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// githubRelease mirrors the subset of the GitHub release schema used here.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// Updater applies binary updates in-place using assets from GitHub releases.
// It exists as an interface so callers (and tests) can swap in a fake
// implementation.
type Updater interface {
	CanSelfUpdate() bool
	ApplyBinaryUpdate(ctx context.Context, channel string) error
}

// Default returns a Updater backed by the real GitHub API and minio/selfupdate.
func Default() Updater {
	return &binaryUpdater{
		apiBase:    defaultGitHubAPIBase,
		httpClient: &http.Client{Timeout: downloadTimeout},
	}
}

// binaryUpdater is the production implementation of Updater.
type binaryUpdater struct {
	apiBase    string
	httpClient *http.Client
}

// CanSelfUpdate reports whether a binary self-update is feasible in the
// current runtime. It returns false when running inside a Docker container
// (the Docker path is preferred in that case), when os.Executable cannot be
// resolved, or when the current executable path is not writable.
func (u *binaryUpdater) CanSelfUpdate() bool {
	if insideContainer() {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return isWritable(exe)
}

// ApplyBinaryUpdate downloads the release asset for the current platform,
// verifies its checksum, extracts the binary and applies the update. The
// channel must be either "latest" or "dev".
func (u *binaryUpdater) ApplyBinaryUpdate(ctx context.Context, channel string) error {
	slog.InfoContext(ctx, "Starting binary self-update",
		"channel", channel,
		"goos", runtime.GOOS,
		"goarch", runtime.GOARCH)

	reader, cleanup, err := u.downloadAndExtract(ctx, channel)
	if err != nil {
		return fmt.Errorf("prepare binary update: %w", err)
	}
	defer cleanup()

	if err := selfupdate.Apply(reader, selfupdate.Options{}); err != nil {
		slog.ErrorContext(ctx, "selfupdate.Apply failed", "error", err)
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			slog.ErrorContext(ctx, "selfupdate rollback failed", "error", rerr)
		}
		return fmt.Errorf("apply binary update: %w", err)
	}

	slog.InfoContext(ctx, "Binary self-update applied successfully")
	return nil
}

// downloadAndExtract resolves the release for the given channel, downloads
// the matching archive and checksums, verifies the SHA-512 hash, and extracts
// the binary. It returns an io.Reader positioned at the start of the binary
// and a cleanup function the caller must invoke when done.
func (u *binaryUpdater) downloadAndExtract(ctx context.Context, channel string) (io.Reader, func(), error) {
	release, err := u.fetchRelease(ctx, channel)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch release: %w", err)
	}

	archiveAsset, checksumAsset, err := pickAssets(release.Assets, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, nil, err
	}

	slog.InfoContext(ctx, "Selected release assets",
		"archive", archiveAsset.Name,
		"checksum", checksumAsset.Name,
		"tag", release.TagName)

	archiveBytes, err := u.downloadBytes(ctx, archiveAsset.BrowserDownloadURL)
	if err != nil {
		return nil, nil, fmt.Errorf("download archive: %w", err)
	}

	checksumBytes, err := u.downloadBytes(ctx, checksumAsset.BrowserDownloadURL)
	if err != nil {
		return nil, nil, fmt.Errorf("download checksum file: %w", err)
	}

	if err := verifyChecksum(archiveAsset.Name, archiveBytes, checksumBytes); err != nil {
		return nil, nil, fmt.Errorf("verify checksum: %w", err)
	}

	binaryName := expectedBinaryName(runtime.GOOS, runtime.GOARCH)
	reader, err := extractBinary(archiveAsset.Name, archiveBytes, binaryName)
	if err != nil {
		return nil, nil, fmt.Errorf("extract binary %q: %w", binaryName, err)
	}

	return reader, func() {}, nil
}

// fetchRelease retrieves the release metadata for the requested channel.
func (u *binaryUpdater) fetchRelease(ctx context.Context, channel string) (*githubRelease, error) {
	var url string
	switch channel {
	case ChannelLatest:
		url = fmt.Sprintf("%s/repos/%s/%s/releases/latest", u.apiBase, repoOwner, repoName)
	case ChannelDev:
		url = fmt.Sprintf("%s/repos/%s/%s/releases/tags/dev", u.apiBase, repoOwner, repoName)
	default:
		return nil, fmt.Errorf("unknown channel %q", channel)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &rel, nil
}

// downloadBytes fetches a URL and returns the full response body.
func (u *binaryUpdater) downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := u.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %q returned status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

// pickAssets selects the archive + checksum assets that match the given
// GOOS/GOARCH. For darwin, a universal binary asset is preferred when
// available.
func pickAssets(assets []githubAsset, goos, goarch string) (archive githubAsset, checksum githubAsset, err error) {
	var checksumAsset *githubAsset
	for i, a := range assets {
		if a.Name == "checksums-cli.txt" {
			checksumAsset = &assets[i]
			break
		}
	}
	if checksumAsset == nil {
		return githubAsset{}, githubAsset{}, errors.New("release does not include checksums-cli.txt")
	}

	candidateNames := candidateArchiveNames(goos, goarch)
	for _, want := range candidateNames {
		for _, a := range assets {
			if strings.HasSuffix(a.Name, want) {
				return a, *checksumAsset, nil
			}
		}
	}
	return githubAsset{}, githubAsset{}, fmt.Errorf("no release asset matches %s/%s", goos, goarch)
}

// candidateArchiveNames returns the suffixes we accept for a given
// GOOS/GOARCH, in priority order. Asset names follow the release.yml pattern
// `altmount-cli_v<version>_<goos>_<goarch>.<ext>`.
func candidateArchiveNames(goos, goarch string) []string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	if goos == "darwin" {
		// Prefer the universal binary, fall back to arch-specific archives.
		return []string{
			fmt.Sprintf("_darwin_universal%s", ext),
			fmt.Sprintf("_%s_%s%s", goos, goarch, ext),
		}
	}
	return []string{fmt.Sprintf("_%s_%s%s", goos, goarch, ext)}
}

// expectedBinaryName returns the binary file name that is embedded in the
// archive for the given GOOS/GOARCH.
func expectedBinaryName(goos, goarch string) string {
	if goos == "darwin" {
		// If the universal binary is present, it uses this name; otherwise the
		// arch-specific binary name is the fallback. extractBinary tries both.
		return "altmount-cli-darwin-universal"
	}
	name := fmt.Sprintf("altmount-cli-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// verifyChecksum validates archiveBytes against the sha512 digest listed for
// archiveName in the provided checksum file contents.
func verifyChecksum(archiveName string, archiveBytes, checksumFile []byte) error {
	want, err := findChecksum(archiveName, checksumFile)
	if err != nil {
		return err
	}
	sum := sha512.Sum512(archiveBytes)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: got %s want %s", archiveName, got, want)
	}
	return nil
}

// findChecksum parses a `sha512sum` style checksum file and returns the digest
// for the named file.
func findChecksum(name string, checksumFile []byte) (string, error) {
	for line := range strings.SplitSeq(string(checksumFile), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "<hex>  <filename>" (two spaces) or "<hex> *<filename>".
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		fname := strings.TrimPrefix(fields[len(fields)-1], "*")
		if fname == name || path.Base(fname) == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %q not found", name)
}

// extractBinary pulls the expected binary out of the archive. For tar.gz it
// iterates entries; for zip it looks up by file name. If the expected name is
// not present, a secondary lookup by arch-specific name is attempted (this
// covers darwin_universal archives whose binary name differs).
func extractBinary(archiveName string, archiveBytes []byte, expectedName string) (io.Reader, error) {
	alt := fmt.Sprintf("altmount-cli-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		alt += ".exe"
	}
	candidates := []string{expectedName, alt}

	switch {
	case strings.HasSuffix(archiveName, ".tar.gz"):
		return extractFromTarGz(archiveBytes, candidates)
	case strings.HasSuffix(archiveName, ".zip"):
		return extractFromZip(archiveBytes, candidates)
	default:
		return nil, fmt.Errorf("unsupported archive format: %s", archiveName)
	}
}

func extractFromTarGz(data []byte, candidates []string) (io.Reader, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		base := path.Base(hdr.Name)
		if matchesAny(base, candidates) {
			buf, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read entry %q: %w", hdr.Name, err)
			}
			return bytes.NewReader(buf), nil
		}
	}
	return nil, fmt.Errorf("binary not found in tar.gz (looked for %v)", candidates)
}

func extractFromZip(data []byte, candidates []string) (io.Reader, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range zr.File {
		base := path.Base(f.Name)
		if !matchesAny(base, candidates) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %q: %w", f.Name, err)
		}
		buf, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %q: %w", f.Name, err)
		}
		return bytes.NewReader(buf), nil
	}
	return nil, fmt.Errorf("binary not found in zip (looked for %v)", candidates)
}

func matchesAny(name string, candidates []string) bool {
	return slices.Contains(candidates, name)
}

// insideContainer reports whether the current process appears to be running
// inside a Docker/Kubernetes container. It checks for the /.dockerenv marker
// file and the KUBERNETES_SERVICE_HOST env var.
func insideContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}
	return false
}

// isWritable returns true if the current process can open the given file for
// writing without truncating it.
func isWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
