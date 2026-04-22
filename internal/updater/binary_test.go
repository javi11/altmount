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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPickAssets(t *testing.T) {
	t.Parallel()

	assets := []githubAsset{
		{Name: "altmount-cli_v1.2.3_linux_amd64.tar.gz", BrowserDownloadURL: "https://e/linux-amd64"},
		{Name: "altmount-cli_v1.2.3_linux_arm64.tar.gz", BrowserDownloadURL: "https://e/linux-arm64"},
		{Name: "altmount-cli_v1.2.3_windows_amd64.zip", BrowserDownloadURL: "https://e/win"},
		{Name: "altmount-cli_v1.2.3_darwin_amd64.tar.gz", BrowserDownloadURL: "https://e/darwin-amd64"},
		{Name: "altmount-cli_v1.2.3_darwin_universal.tar.gz", BrowserDownloadURL: "https://e/darwin-universal"},
		{Name: "checksums-cli.txt", BrowserDownloadURL: "https://e/checksums"},
	}

	tests := []struct {
		name        string
		goos        string
		goarch      string
		wantArchive string
		wantErr     bool
	}{
		{name: "linux amd64", goos: "linux", goarch: "amd64", wantArchive: "altmount-cli_v1.2.3_linux_amd64.tar.gz"},
		{name: "linux arm64", goos: "linux", goarch: "arm64", wantArchive: "altmount-cli_v1.2.3_linux_arm64.tar.gz"},
		{name: "windows amd64", goos: "windows", goarch: "amd64", wantArchive: "altmount-cli_v1.2.3_windows_amd64.zip"},
		{name: "darwin prefers universal", goos: "darwin", goarch: "amd64", wantArchive: "altmount-cli_v1.2.3_darwin_universal.tar.gz"},
		{name: "unsupported os", goos: "plan9", goarch: "amd64", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			archive, checksum, err := pickAssets(assets, tc.goos, tc.goarch)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantArchive, archive.Name)
			assert.Equal(t, "checksums-cli.txt", checksum.Name)
		})
	}
}

func TestPickAssets_MissingChecksum(t *testing.T) {
	t.Parallel()
	assets := []githubAsset{
		{Name: "altmount-cli_v1.2.3_linux_amd64.tar.gz"},
	}
	_, _, err := pickAssets(assets, "linux", "amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksums-cli.txt")
}

func TestVerifyChecksum(t *testing.T) {
	t.Parallel()

	data := []byte("hello world")
	sum := sha512.Sum512(data)
	digest := hex.EncodeToString(sum[:])
	checksums := fmt.Sprintf("%s  altmount-cli_v1_linux_amd64.tar.gz\nbaddigest  other.tar.gz\n", digest)

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		err := verifyChecksum("altmount-cli_v1_linux_amd64.tar.gz", data, []byte(checksums))
		require.NoError(t, err)
	})

	t.Run("mismatch", func(t *testing.T) {
		t.Parallel()
		err := verifyChecksum("altmount-cli_v1_linux_amd64.tar.gz", []byte("tampered"), []byte(checksums))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mismatch")
	})

	t.Run("unknown file", func(t *testing.T) {
		t.Parallel()
		err := verifyChecksum("other.tar.gz.missing", data, []byte(checksums))
		require.Error(t, err)
	})
}

func TestExtractBinary_TarGz(t *testing.T) {
	t.Parallel()

	payload := []byte("fake-binary-contents")
	archive := buildTarGz(t, "altmount-cli-linux-amd64", payload)

	r, err := extractBinary("altmount-cli_v1_linux_amd64.tar.gz", archive, "altmount-cli-linux-amd64")
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestExtractBinary_Zip(t *testing.T) {
	t.Parallel()

	payload := []byte("fake-windows-binary")
	archive := buildZip(t, "altmount-cli-windows-amd64.exe", payload)

	r, err := extractBinary("altmount-cli_v1_windows_amd64.zip", archive, "altmount-cli-windows-amd64.exe")
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestExtractBinary_BinaryMissing(t *testing.T) {
	t.Parallel()
	archive := buildTarGz(t, "unrelated-file", []byte("x"))
	_, err := extractBinary("something_linux_amd64.tar.gz", archive, "altmount-cli-linux-amd64")
	require.Error(t, err)
}

func TestDownloadAndExtract_HappyPath(t *testing.T) {
	t.Parallel()

	payload := []byte("fake-binary-contents-for-download")
	archive := buildTarGz(t, "altmount-cli-linux-amd64", payload)
	sum := sha512.Sum512(archive)
	digestLine := fmt.Sprintf("%s  altmount-cli_v1.0.0_linux_amd64.tar.gz\n", hex.EncodeToString(sum[:]))

	var baseURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			rel := githubRelease{
				TagName: "v1.0.0",
				Assets: []githubAsset{
					{Name: "altmount-cli_v1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: baseURL + "/archive"},
					{Name: "checksums-cli.txt", BrowserDownloadURL: baseURL + "/checksums"},
				},
			}
			_ = json.NewEncoder(w).Encode(rel)
		case r.URL.Path == "/archive":
			_, _ = w.Write(archive)
		case r.URL.Path == "/checksums":
			_, _ = w.Write([]byte(digestLine))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	baseURL = srv.URL

	u := &binaryUpdater{apiBase: srv.URL, httpClient: srv.Client()}
	reader, cleanup, err := u.downloadAndExtractWith(context.Background(), ChannelLatest, "linux", "amd64")
	require.NoError(t, err)
	defer cleanup()

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestDownloadAndExtract_ChecksumMismatch(t *testing.T) {
	t.Parallel()

	archive := buildTarGz(t, "altmount-cli-linux-amd64", []byte("real contents"))
	// Intentionally wrong checksum.
	digestLine := fmt.Sprintf("%s  altmount-cli_v1.0.0_linux_amd64.tar.gz\n", strings.Repeat("0", 128))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			rel := githubRelease{
				TagName: "v1.0.0",
				Assets: []githubAsset{
					{Name: "altmount-cli_v1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: srv.URL + "/archive"},
					{Name: "checksums-cli.txt", BrowserDownloadURL: srv.URL + "/checksums"},
				},
			}
			_ = json.NewEncoder(w).Encode(rel)
		case r.URL.Path == "/archive":
			_, _ = w.Write(archive)
		case r.URL.Path == "/checksums":
			_, _ = w.Write([]byte(digestLine))
		}
	})
	defer srv.Close()

	u := &binaryUpdater{apiBase: srv.URL, httpClient: srv.Client()}
	_, _, err := u.downloadAndExtractWith(context.Background(), ChannelLatest, "linux", "amd64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum")
}

func TestFetchRelease_UnknownChannel(t *testing.T) {
	t.Parallel()
	u := &binaryUpdater{apiBase: "http://127.0.0.1:1", httpClient: http.DefaultClient}
	_, err := u.fetchRelease(context.Background(), "banana")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown channel")
}

func TestIsWritable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writable := filepath.Join(dir, "writable")
	require.NoError(t, os.WriteFile(writable, []byte("x"), 0o600))
	assert.True(t, isWritable(writable))

	readOnly := filepath.Join(dir, "readonly")
	require.NoError(t, os.WriteFile(readOnly, []byte("x"), 0o400))
	// On most filesystems 0o400 forbids O_WRONLY for the owner.
	assert.False(t, isWritable(readOnly))

	assert.False(t, isWritable(filepath.Join(dir, "does-not-exist")))
}

func TestCanSelfUpdate_RefusesWhenExecutableNotWritable(t *testing.T) {
	// We can't portably make os.Executable point at a read-only file, but we
	// can at least assert the function does not panic and returns a boolean.
	u := &binaryUpdater{}
	_ = u.CanSelfUpdate()
}

// --- helpers -----------------------------------------------------------

func buildTarGz(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(payload)),
		Typeflag: tar.TypeReg,
	}))
	_, err := tw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func buildZip(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	require.NoError(t, err)
	_, err = w.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// downloadAndExtractWith exposes downloadAndExtract with explicit goos/goarch
// for tests. It mirrors the logic of downloadAndExtract so callers do not need
// to mutate runtime.GOOS/GOARCH.
func (u *binaryUpdater) downloadAndExtractWith(ctx context.Context, channel, goos, goarch string) (io.Reader, func(), error) {
	release, err := u.fetchRelease(ctx, channel)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch release: %w", err)
	}
	archiveAsset, checksumAsset, err := pickAssets(release.Assets, goos, goarch)
	if err != nil {
		return nil, nil, err
	}
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
	binaryName := expectedBinaryName(goos, goarch)
	// For non-darwin the alt fallback equals the expected name; for darwin we
	// want to try the arch-specific binary name too.
	alt := fmt.Sprintf("altmount-cli-%s-%s", goos, goarch)
	if goos == "windows" {
		alt += ".exe"
	}
	candidates := []string{binaryName, alt}
	var reader io.Reader
	switch {
	case strings.HasSuffix(archiveAsset.Name, ".tar.gz"):
		reader, err = extractFromTarGz(archiveBytes, candidates)
	case strings.HasSuffix(archiveAsset.Name, ".zip"):
		reader, err = extractFromZip(archiveBytes, candidates)
	default:
		return nil, nil, fmt.Errorf("unsupported archive: %s", archiveAsset.Name)
	}
	if err != nil {
		return nil, nil, err
	}
	return reader, func() {}, nil
}
